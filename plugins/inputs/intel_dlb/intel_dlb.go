//go:generate ../../../tools/readme_config_includer/generator
//go:build linux
// +build linux

package intel_dlb

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var unreachableSocketBehaviors = []string{"error", "ignore"}

const (
	defaultSocketPath      = "/var/run/dpdk/rte/dpdk_telemetry.v2"
	pluginName             = "intel_dlb"
	eventdevListCommand    = "/eventdev/dev_list"
	dlbDeviceIDLocation    = "/sys/devices/*/*/device"
	aerCorrectableFileName = "aer_dev_correctable"
	aerFatalFileName       = "aer_dev_fatal"
	aerNonFatalFileName    = "aer_dev_nonfatal"
	defaultDLBDevice       = "0x2710"
)

type IntelDLB struct {
	SocketPath                string          `toml:"socket_path"`
	EventdevCommands          []string        `toml:"eventdev_commands"`
	DLBDeviceIDs              []string        `toml:"dlb_device_types"`
	UnreachableSocketBehavior string          `toml:"unreachable_socket_behavior"`
	Log                       telegraf.Logger `toml:"-"`

	connection           net.Conn
	devicesDir           []string
	rasReader            rasReader
	maxInitMessageLength uint32
}

func (*IntelDLB) SampleConfig() string {
	return sampleConfig
}

func (d *IntelDLB) Init() error {
	var err error

	if d.UnreachableSocketBehavior == "" {
		d.UnreachableSocketBehavior = "error"
	}

	if err = choice.Check(d.UnreachableSocketBehavior, unreachableSocketBehaviors); err != nil {
		return fmt.Errorf("unreachable_socket_behavior: %w", err)
	}

	if d.SocketPath == "" {
		d.SocketPath = defaultSocketPath
		d.Log.Debugf("Using default '%v' path for socket_path", defaultSocketPath)
	}

	err = checkSocketPath(d.SocketPath)
	if err != nil {
		if d.UnreachableSocketBehavior == "error" {
			return err
		}
		d.Log.Warn(err)
	}

	if len(d.EventdevCommands) == 0 {
		eventdevDefaultCommands := []string{"/eventdev/dev_xstats", "/eventdev/port_xstats", "/eventdev/queue_xstats", "/eventdev/queue_links"}
		d.EventdevCommands = eventdevDefaultCommands
		d.Log.Debugf("Using default eventdev commands '%v'", eventdevDefaultCommands)
	}

	if err := validateEventdevCommands(d.EventdevCommands); err != nil {
		return err
	}

	if len(d.DLBDeviceIDs) == 0 {
		d.DLBDeviceIDs = []string{defaultDLBDevice}
		d.Log.Debugf("Using default DLB Device ID '%v'", defaultDLBDevice)
	}

	err = d.checkAndAddDLBDevice()
	if err != nil {
		return err
	}

	d.maxInitMessageLength = 1024

	return nil
}

func (d *IntelDLB) Gather(acc telegraf.Accumulator) error {
	err := d.gatherMetricsFromSocket(acc)
	if err != nil {
		socketErr := fmt.Errorf("gathering metrics from socket by given commands failed: %w", err)
		if d.UnreachableSocketBehavior == "error" {
			return socketErr
		}
		d.Log.Debug(socketErr)
	}

	err = d.gatherRasMetrics(acc)
	if err != nil {
		return fmt.Errorf("gathering RAS metrics failed: %w", err)
	}

	return nil
}

func (d *IntelDLB) gatherRasMetrics(acc telegraf.Accumulator) error {
	for _, devicePath := range d.devicesDir {
		rasTags := map[string]string{
			"device": filepath.Base(filepath.Dir(devicePath)),
		}

		aerFilesName := []string{aerCorrectableFileName, aerFatalFileName, aerNonFatalFileName}
		for _, fileName := range aerFilesName {
			rasTags["metric_file"] = fileName
			rasMetrics, err := d.readRasMetrics(devicePath, fileName)
			if err != nil {
				return err
			}
			acc.AddFields("intel_dlb_ras", rasMetrics, rasTags)
		}
	}
	return nil
}

func (d *IntelDLB) readRasMetrics(devicePath, metricPath string) (map[string]interface{}, error) {
	deviceMetricPath := filepath.Join(devicePath, metricPath)

	data, err := d.rasReader.readFromFile(deviceMetricPath)
	if err != nil {
		return nil, err
	}

	metrics := strings.Split(strings.TrimSpace(string(data)), "\n")

	rasMetric := make(map[string]interface{})
	for _, metric := range metrics {
		metricPart := strings.Split(metric, " ")
		if len(metricPart) < 2 {
			return nil, fmt.Errorf("no value to parse: %+q", metricPart)
		}

		metricVal, err := strconv.ParseUint(metricPart[1], 10, 10)
		if err != nil {
			return nil, fmt.Errorf("failed to parse value %q: %w", metricPart[1], err)
		}
		rasMetric[metricPart[0]] = metricVal
	}

	return rasMetric, nil
}

func (d *IntelDLB) gatherMetricsFromSocket(acc telegraf.Accumulator) error {
	// Get device indexes and those indexes to available commands
	commandsWithIndex, err := d.gatherCommandsWithDeviceIndex()
	if err != nil {
		return err
	}

	for _, command := range commandsWithIndex {
		// Write message to socket, e.g.: "/eventdev/dev_xstats,0", then process result and parse it to variable.
		var parsedDeviceXstats map[string]map[string]int
		err := d.gatherCommandsResult(command, &parsedDeviceXstats)
		if err != nil {
			return err
		}
		var statsWithValue = make(map[string]interface{})
		for _, commandBody := range parsedDeviceXstats {
			for metricName, metricValue := range commandBody {
				statsWithValue[metricName] = metricValue
			}
		}

		var tags = map[string]string{
			"command": command,
		}
		acc.AddFields(pluginName, statsWithValue, tags)
	}

	return nil
}

func (d *IntelDLB) gatherCommandsWithDeviceIndex() ([]string, error) {
	// Parse message from JSON format to map e.g.: key = "/eventdev/dev_list", and value = [0, 1]
	var parsedDeviceIndexes map[string][]int
	err := d.gatherCommandsResult(eventdevListCommand, &parsedDeviceIndexes)
	if err != nil {
		return nil, err
	}
	var commandsWithIndex []string
	for _, deviceIndexes := range parsedDeviceIndexes {
		for _, index := range deviceIndexes {
			for _, command := range d.EventdevCommands {
				if !strings.Contains(command, "dev_") {
					secondDeviceIndexes, err := d.gatherSecondDeviceIndex(index, command)
					if err != nil {
						return nil, err
					}
					commandsWithIndex = append(commandsWithIndex, secondDeviceIndexes...)
				} else {
					// Append to "/eventdev/dev_xstats," device index eg.: "/eventdev/dev_xstats" + "," + "0"
					commandWithIndex := fmt.Sprintf("%s,%d", command, index)
					commandsWithIndex = append(commandsWithIndex, commandWithIndex)
				}
			}
		}
	}

	return commandsWithIndex, nil
}

func (d *IntelDLB) gatherCommandsResult(command string, deviceToParse interface{}) error {
	err := d.ensureConnected()
	if err != nil {
		return err
	}

	replyMsgLen, socketReply, err := d.writeReadSocketMessage(command)
	if err != nil {
		return err
	}

	err = d.parseJSON(replyMsgLen, socketReply, &deviceToParse)
	if err != nil {
		return err
	}

	return nil
}

func (d *IntelDLB) gatherSecondDeviceIndex(index int, command string) ([]string, error) {
	eventdevListWithSecondIndex := []string{"/eventdev/port_list", "/eventdev/queue_list"}
	var commandsWithIndex []string
	for _, commandToGatherSecondIndex := range eventdevListWithSecondIndex {
		// get command type e.g.: "port_xstat" gives "port"
		commandType := strings.Split(command, "_")
		if len(commandType) != 2 {
			return nil, d.closeSocketAndThrowError("custom", fmt.Errorf("cannot split command - %s", commandType))
		}

		if strings.Contains(commandToGatherSecondIndex, commandType[0]) {
			var parsedDeviceSecondIndexes map[string][]int
			commandToGatherWithIndex := fmt.Sprintf("%s,%d", commandToGatherSecondIndex, index)

			err := d.gatherCommandsResult(commandToGatherWithIndex, &parsedDeviceSecondIndexes)
			if err != nil {
				return nil, err
			}

			for _, indexArray := range parsedDeviceSecondIndexes {
				for _, secondIndex := range indexArray {
					commandWithIndex := fmt.Sprintf("%s,%d,%d", command, index, secondIndex)
					commandsWithIndex = append(commandsWithIndex, commandWithIndex)
				}
			}
		}
	}

	return commandsWithIndex, nil
}

func (d *IntelDLB) ensureConnected() error {
	var err error
	d.maxInitMessageLength = uint32(1024)
	if d.connection == nil {
		d.connection, err = net.Dial("unixpacket", d.SocketPath)
		if err != nil {
			return err
		}

		err = d.setInitMessageLength()
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *IntelDLB) setInitMessageLength() error {
	type initMessage struct {
		Version      string `json:"version"`
		Pid          int    `json:"pid"`
		MaxOutputLen uint32 `json:"max_output_len"`
	}
	buf := make([]byte, d.maxInitMessageLength)
	messageLength, err := d.connection.Read(buf)
	if err != nil {
		return d.closeSocketAndThrowError("custom", fmt.Errorf("failed to read InitMessage from socket: %w", err))
	}
	if messageLength > len(buf) {
		return d.closeSocketAndThrowError("custom", errors.New("socket reply length is bigger than default buffer length"))
	}

	var initMsg initMessage
	err = json.Unmarshal(buf[:messageLength], &initMsg)
	if err != nil {
		return d.closeSocketAndThrowError("json", err)
	}
	if initMsg.MaxOutputLen == 0 {
		return d.closeSocketAndThrowError("message", err)
	}

	d.maxInitMessageLength = initMsg.MaxOutputLen

	return nil
}

func (d *IntelDLB) writeReadSocketMessage(messageToWrite string) (int, []byte, error) {
	_, writeErr := d.connection.Write([]byte(messageToWrite))
	if writeErr != nil {
		return 0, nil, d.closeSocketAndThrowError("write", writeErr)
	}

	// Read reply, and obtain length of it.
	socketReply := make([]byte, d.maxInitMessageLength)
	replyMsgLen, readErr := d.connection.Read(socketReply)
	if readErr != nil {
		return 0, nil, d.closeSocketAndThrowError("read", readErr)
	}

	if replyMsgLen == 0 {
		return 0, nil, d.closeSocketAndThrowError("message", errors.New("message length is empty"))
	}

	return replyMsgLen, socketReply, nil
}

func (d *IntelDLB) parseJSON(replyMsgLen int, socketReply []byte, parsedDeviceInfo interface{}) error {
	if len(socketReply) == 0 {
		return d.closeSocketAndThrowError("json", errors.New("socket reply is empty"))
	}
	if replyMsgLen > len(socketReply) {
		return d.closeSocketAndThrowError("json", errors.New("socket reply length is bigger than it should be"))
	}
	if replyMsgLen == 0 {
		return d.closeSocketAndThrowError("json", errors.New("socket reply message is empty"))
	}
	// Assign reply to variable, e.g.:  {"/eventdev/dev_list": [0, 1]}
	jsonDeviceIndexes := socketReply[:replyMsgLen]

	// Parse message from JSON format to map, e.g.: map[string]int. Key = "/eventdev/dev_list" Value = Array[int] {0,1}
	jsonParseErr := json.Unmarshal(jsonDeviceIndexes, &parsedDeviceInfo)
	if jsonParseErr != nil {
		return d.closeSocketAndThrowError("json", jsonParseErr)
	}

	return nil
}

func (d *IntelDLB) closeSocketAndThrowError(errType string, err error) error {
	const (
		writeErrMsg  = "failed to send command to socket: '%v'"
		readErrMsg   = "failed to read response of from socket: '%v'"
		msgLenErr    = "got empty response from socket: '%v'"
		jsonParseErr = "failed to parse json: '%v'"
		failedConErr = " - and failed to close connection '%v'"
		customErr    = "error occurred: '%v'"
	)

	var errMsg string
	switch errType {
	case "write":
		errMsg = writeErrMsg
	case "read":
		errMsg = readErrMsg
	case "message":
		errMsg = msgLenErr
	case "json":
		errMsg = jsonParseErr
	case "custom":
		errMsg = customErr
	}

	if d.connection != nil {
		closeConnectionErr := d.connection.Close()
		d.connection = nil
		if closeConnectionErr != nil {
			errCloseMsg := errMsg + failedConErr
			return fmt.Errorf(errCloseMsg, err, closeConnectionErr)
		}
	}

	return fmt.Errorf(errMsg, err)
}

func (d *IntelDLB) checkAndAddDLBDevice() error {
	if d.rasReader == nil {
		return errors.New("rasreader was not initialized")
	}
	filePaths, err := d.rasReader.gatherPaths(dlbDeviceIDLocation)
	if err != nil {
		return err
	}

	deviceIDToDirs := make(map[string][]string)
	for _, path := range filePaths {
		fileData, err := d.rasReader.readFromFile(path)
		if err != nil {
			return err
		}

		// check if it is DLB device
		trimmedDeviceID := strings.TrimSpace(string(fileData))
		if !choice.Contains(trimmedDeviceID, d.DLBDeviceIDs) {
			continue
		}
		deviceDir := filepath.Dir(path)
		deviceIDToDirs[trimmedDeviceID] = append(deviceIDToDirs[trimmedDeviceID], deviceDir)
		d.devicesDir = append(d.devicesDir, deviceDir)
	}
	if len(d.devicesDir) == 0 {
		return fmt.Errorf("cannot find any of provided IDs on the system - %+q", d.DLBDeviceIDs)
	}
	for _, deviceID := range d.DLBDeviceIDs {
		if len(deviceIDToDirs[deviceID]) == 0 {
			d.Log.Debugf("Device %s was not found on system", deviceID)
		}
	}
	return nil
}

func checkSocketPath(path string) error {
	pathInfo, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("provided path does not exist: '%v'", path)
	}

	if err != nil {
		return fmt.Errorf("cannot get system information of %q file: %w", path, err)
	}

	if pathInfo.Mode()&os.ModeSocket != os.ModeSocket {
		return fmt.Errorf("provided path does not point to a socket file: '%v'", path)
	}

	return nil
}

func validateEventdevCommands(commands []string) error {
	eventdevCommandRegex := regexp.MustCompile("^/eventdev/[a-z_]+$")
	for _, command := range commands {
		if !eventdevCommandRegex.MatchString(command) {
			return fmt.Errorf("provided command is not valid - %v", command)
		}
	}

	return nil
}

func init() {
	inputs.Add(pluginName, func() telegraf.Input {
		return &IntelDLB{
			rasReader: rasReaderImpl{},
		}
	})
}
