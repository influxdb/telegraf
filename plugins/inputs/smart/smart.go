//go:generate ../../../tools/readme_config_includer/generator
package smart

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const intelVID = "0x8086"

var (
	// Device Model:     APPLE SSD SM256E
	// Product:              HUH721212AL5204
	// Model Number: TS128GMTE850
	modelInfo = regexp.MustCompile(`^(Device Model|Product|Model Number):\s+(.*)$`)
	// Serial Number:    S0X5NZBC422720
	serialInfo = regexp.MustCompile(`(?i)^Serial Number:\s+(.*)$`)
	// LU WWN Device Id: 5 002538 655584d30
	wwnInfo = regexp.MustCompile(`^LU WWN Device Id:\s+(.*)$`)
	// User Capacity:    251,000,193,024 bytes [251 GB]
	userCapacityInfo = regexp.MustCompile(`^User Capacity:\s+([0-9,]+)\s+bytes.*$`)
	// SMART support is: Enabled
	smartEnabledInfo = regexp.MustCompile(`^SMART support is:\s+(\w+)$`)
	// Power mode is:    ACTIVE or IDLE or Power mode was:   STANDBY
	powermodeInfo = regexp.MustCompile(`^Power mode \w+:\s+(\w+)`)
	// Device is in STANDBY mode
	standbyInfo = regexp.MustCompile(`^Device is in\s+(\w+)`)
	// SMART overall-health self-assessment test result: PASSED
	// SMART Health Status: OK
	// PASSED, FAILED, UNKNOWN
	smartOverallHealth = regexp.MustCompile(`^(SMART overall-health self-assessment test result|SMART Health Status):\s+(\w+).*$`)

	// sasNVMeAttr is a SAS or NVMe SMART attribute
	sasNVMeAttr = regexp.MustCompile(`^([^:]+):\s+(.+)$`)

	// ID# ATTRIBUTE_NAME          FLAGS    VALUE WORST THRESH FAIL RAW_VALUE
	//   1 Raw_Read_Error_Rate     -O-RC-   200   200   000    -    0
	//   5 Reallocated_Sector_Ct   PO--CK   100   100   000    -    0
	// 192 Power-Off_Retract_Count -O--C-   097   097   000    -    14716

	// ID# ATTRIBUTE_NAME          FLAGS    VALUE WORST THRESH FAIL RAW_VALUE
	//   1 Raw_Read_Error_Rate     PO-RC-+  200   200   051    -    30
	//   5 Reallocated_Sector_Ct   POS-C-+  200   200   140    -    0
	// 192 Power-Off_Retract_Count -O-RCK+  200   200   000    -    4
	attribute = regexp.MustCompile(`^\s*([0-9]+)\s(\S+)\s+([-P][-O][-S][-R][-C][-K])[\+]?\s+([0-9]+)\s+([0-9]+)\s+([0-9-]+)\s+([-\w]+)\s+([\w\+\.]+).*$`)

	//  Additional Smart Log for NVME device:nvme0 namespace-id:ffffffff
	// nvme version 1.14+ metrics:
	// ID             KEY                                 Normalized     Raw
	// 0xab    program_fail_count                             100         0

	// nvme deprecated metric format:
	//	key                               normalized raw
	//	program_fail_count              : 100%       0

	// REGEX pattern supports deprecated metrics (nvme-cli version below 1.14) and metrics from nvme-cli 1.14 (and above).
	intelExpressionPattern = regexp.MustCompile(`^([A-Za-z0-9_\s]+)[:|\s]+(\d+)[%|\s]+(.+)`)

	//	vid     : 0x8086
	//	sn      : CFGT53260XSP8011P
	nvmeIDCtrlExpressionPattern = regexp.MustCompile(`^([\w\s]+):([\s\w]+)`)

	// Format from nvme-cli 1.14 (and above) gives ID and KEY, this regex is for separating id from key.
	//  ID			  KEY
	// 0xab    program_fail_count
	nvmeIDSeparatePattern = regexp.MustCompile(`^([A-Za-z0-9_]+)(.+)`)

	deviceFieldIDs = map[string]string{
		"1":   "read_error_rate",
		"5":   "reallocated_sectors_count",
		"7":   "seek_error_rate",
		"9":   "power_on_hours",
		"12":  "power_cycle_count",
		"10":  "spin_retry_count",
		"184": "end_to_end_error",
		"187": "uncorrectable_errors",
		"188": "command_timeout",
		"190": "temp_c",
		"194": "temp_c",
		"196": "realloc_event_count",
		"197": "pending_sector_count",
		"198": "uncorrectable_sector_count",
		"199": "udma_crc_errors",
		"201": "soft_read_error_rate",
	}

	// There are some fields we're interested in which use the vendor specific device ids
	// so we need to be able to match on name instead
	deviceFieldNames = map[string]string{
		"Percent_Lifetime_Remain": "percent_lifetime_remain",
		"Wear_Leveling_Count":     "wear_leveling_count",
		"Media_Wearout_Indicator": "media_wearout_indicator",
	}

	// to obtain metrics from smartctl
	sasNVMeAttributes = map[string]struct {
		ID    string
		Name  string
		Parse func(fields, deviceFields map[string]interface{}, str string) error
	}{
		"Accumulated start-stop cycles": {
			ID:   "4",
			Name: "Start_Stop_Count",
		},
		"Accumulated load-unload cycles": {
			ID:   "193",
			Name: "Load_Cycle_Count",
		},
		"Current Drive Temperature": {
			ID:    "194",
			Name:  "Temperature_Celsius",
			Parse: parseTemperature,
		},
		"Temperature": {
			ID:    "194",
			Name:  "Temperature_Celsius",
			Parse: parseTemperature,
		},
		"Power Cycles": {
			ID:   "12",
			Name: "Power_Cycle_Count",
		},
		"Power On Hours": {
			ID:   "9",
			Name: "Power_On_Hours",
		},
		"Media and Data Integrity Errors": {
			Name: "Media_and_Data_Integrity_Errors",
		},
		"Error Information Log Entries": {
			Name: "Error_Information_Log_Entries",
		},
		"Critical Warning": {
			Name: "Critical_Warning",
			Parse: func(fields, _ map[string]interface{}, str string) error {
				var value int64
				if _, err := fmt.Sscanf(str, "0x%x", &value); err != nil {
					return err
				}

				fields["raw_value"] = value

				return nil
			},
		},
		"Available Spare": {
			Name:  "Available_Spare",
			Parse: parsePercentageInt,
		},
		"Available Spare Threshold": {
			Name:  "Available_Spare_Threshold",
			Parse: parsePercentageInt,
		},
		"Percentage Used": {
			Name:  "Percentage_Used",
			Parse: parsePercentageInt,
		},
		"Percentage used endurance indicator": {
			Name:  "Percentage_Used",
			Parse: parsePercentageInt,
		},
		"Data Units Read": {
			Name:  "Data_Units_Read",
			Parse: parseDataUnits,
		},
		"Data Units Written": {
			Name:  "Data_Units_Written",
			Parse: parseDataUnits,
		},
		"Host Read Commands": {
			Name:  "Host_Read_Commands",
			Parse: parseCommaSeparatedInt,
		},
		"Host Write Commands": {
			Name:  "Host_Write_Commands",
			Parse: parseCommaSeparatedInt,
		},
		"Controller Busy Time": {
			Name:  "Controller_Busy_Time",
			Parse: parseCommaSeparatedInt,
		},
		"Unsafe Shutdowns": {
			Name:  "Unsafe_Shutdowns",
			Parse: parseCommaSeparatedInt,
		},
		"Warning  Comp. Temperature Time": {
			Name:  "Warning_Temperature_Time",
			Parse: parseCommaSeparatedInt,
		},
		"Critical Comp. Temperature Time": {
			Name:  "Critical_Temperature_Time",
			Parse: parseCommaSeparatedInt,
		},
		"Thermal Temp. 1 Transition Count": {
			Name:  "Thermal_Management_T1_Trans_Count",
			Parse: parseCommaSeparatedInt,
		},
		"Thermal Temp. 2 Transition Count": {
			Name:  "Thermal_Management_T2_Trans_Count",
			Parse: parseCommaSeparatedInt,
		},
		"Thermal Temp. 1 Total Time": {
			Name:  "Thermal_Management_T1_Total_Time",
			Parse: parseCommaSeparatedInt,
		},
		"Thermal Temp. 2 Total Time": {
			Name:  "Thermal_Management_T2_Total_Time",
			Parse: parseCommaSeparatedInt,
		},
		"Temperature Sensor 1": {
			Name:  "Temperature_Sensor_1",
			Parse: parseTemperatureSensor,
		},
		"Temperature Sensor 2": {
			Name:  "Temperature_Sensor_2",
			Parse: parseTemperatureSensor,
		},
		"Temperature Sensor 3": {
			Name:  "Temperature_Sensor_3",
			Parse: parseTemperatureSensor,
		},
		"Temperature Sensor 4": {
			Name:  "Temperature_Sensor_4",
			Parse: parseTemperatureSensor,
		},
		"Temperature Sensor 5": {
			Name:  "Temperature_Sensor_5",
			Parse: parseTemperatureSensor,
		},
		"Temperature Sensor 6": {
			Name:  "Temperature_Sensor_6",
			Parse: parseTemperatureSensor,
		},
		"Temperature Sensor 7": {
			Name:  "Temperature_Sensor_7",
			Parse: parseTemperatureSensor,
		},
		"Temperature Sensor 8": {
			Name:  "Temperature_Sensor_8",
			Parse: parseTemperatureSensor,
		},
	}
	// To obtain Intel specific metrics from nvme-cli version 1.14 and above.
	intelAttributes = map[string]struct {
		ID    string
		Name  string
		Parse func(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error
	}{
		"program_fail_count": {
			Name: "Program_Fail_Count",
		},
		"erase_fail_count": {
			Name: "Erase_Fail_Count",
		},
		"wear_leveling_count": { // previously: "wear_leveling"
			Name: "Wear_Leveling_Count",
		},
		"e2e_error_detect_count": { // previously: "end_to_end_error_detection_count"
			Name: "End_To_End_Error_Detection_Count",
		},
		"crc_error_count": {
			Name: "Crc_Error_Count",
		},
		"media_wear_percentage": { // previously: "timed_workload_media_wear"
			Name: "Media_Wear_Percentage",
		},
		"host_reads": {
			Name: "Host_Reads",
		},
		"timed_work_load": { // previously: "timed_workload_timer"
			Name: "Timed_Workload_Timer",
		},
		"thermal_throttle_status": {
			Name: "Thermal_Throttle_Status",
		},
		"retry_buff_overflow_count": { // previously: "retry_buffer_overflow_count"
			Name: "Retry_Buffer_Overflow_Count",
		},
		"pll_lock_loss_counter": { // previously: "pll_lock_loss_count"
			Name: "Pll_Lock_Loss_Count",
		},
	}
	// to obtain Intel specific metrics from nvme-cli
	intelAttributesDeprecatedFormat = map[string]struct {
		ID    string
		Name  string
		Parse func(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error
	}{
		"program_fail_count": {
			Name: "Program_Fail_Count",
		},
		"erase_fail_count": {
			Name: "Erase_Fail_Count",
		},
		"end_to_end_error_detection_count": {
			Name: "End_To_End_Error_Detection_Count",
		},
		"crc_error_count": {
			Name: "Crc_Error_Count",
		},
		"retry_buffer_overflow_count": {
			Name: "Retry_Buffer_Overflow_Count",
		},
		"wear_leveling": {
			Name:  "Wear_Leveling",
			Parse: parseWearLeveling,
		},
		"timed_workload_media_wear": {
			Name:  "Timed_Workload_Media_Wear",
			Parse: parseTimedWorkload,
		},
		"timed_workload_host_reads": {
			Name:  "Timed_Workload_Host_Reads",
			Parse: parseTimedWorkload,
		},
		"timed_workload_timer": {
			Name: "Timed_Workload_Timer",
			Parse: func(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error {
				return parseCommaSeparatedIntWithAccumulator(acc, fields, tags, strings.TrimSuffix(str, " min"))
			},
		},
		"thermal_throttle_status": {
			Name:  "Thermal_Throttle_Status",
			Parse: parseThermalThrottle,
		},
		"pll_lock_loss_count": {
			Name: "Pll_Lock_Loss_Count",
		},
		"nand_bytes_written": {
			Name:  "Nand_Bytes_Written",
			Parse: parseBytesWritten,
		},
		"host_bytes_written": {
			Name:  "Host_Bytes_Written",
			Parse: parseBytesWritten,
		},
	}

	knownReadMethods = []string{"concurrent", "sequential"}
)

// Smart plugin reads metrics from storage devices supporting S.M.A.R.T.
type Smart struct {
	Path              string          `toml:"path" deprecated:"1.16.0;1.35.0;use 'path_smartctl' instead"`
	PathSmartctl      string          `toml:"path_smartctl"`
	PathNVMe          string          `toml:"path_nvme"`
	Nocheck           string          `toml:"nocheck"`
	EnableExtensions  []string        `toml:"enable_extensions"`
	Attributes        bool            `toml:"attributes"`
	Excludes          []string        `toml:"excludes"`
	Devices           []string        `toml:"devices"`
	UseSudo           bool            `toml:"use_sudo"`
	TagWithDeviceType bool            `toml:"tag_with_device_type"`
	Timeout           config.Duration `toml:"timeout"`
	ReadMethod        string          `toml:"read_method"`
	Log               telegraf.Logger `toml:"-"`
}

type nvmeDevice struct {
	name         string
	vendorID     string
	model        string
	serialNumber string
}

func newSmart() *Smart {
	return &Smart{
		Timeout:    config.Duration(time.Second * 30),
		ReadMethod: "concurrent",
	}
}

func (*Smart) SampleConfig() string {
	return sampleConfig
}

// Init performs one time setup of the plugin and returns an error if the configuration is invalid.
func (m *Smart) Init() error {
	// if deprecated `path` (to smartctl binary) is provided in config and `path_smartctl` override does not exist
	if len(m.Path) > 0 && len(m.PathSmartctl) == 0 {
		m.PathSmartctl = m.Path
	}

	// if `path_smartctl` is not provided in config, try to find smartctl binary in PATH
	if len(m.PathSmartctl) == 0 {
		//nolint:errcheck // error handled later
		m.PathSmartctl, _ = exec.LookPath("smartctl")
	}

	// if `path_nvme` is not provided in config, try to find nvme binary in PATH
	if len(m.PathNVMe) == 0 {
		//nolint:errcheck // error handled later
		m.PathNVMe, _ = exec.LookPath("nvme")
	}

	if !contains(knownReadMethods, m.ReadMethod) {
		return fmt.Errorf("provided read method %q is not valid", m.ReadMethod)
	}

	err := validatePath(m.PathSmartctl)
	if err != nil {
		m.PathSmartctl = ""
		// without smartctl, plugin will not be able to gather basic metrics
		return fmt.Errorf("smartctl not found: verify that smartctl is installed and it is in your PATH (or specified in config): %w", err)
	}

	err = validatePath(m.PathNVMe)
	if err != nil {
		m.PathNVMe = ""
		// without nvme, plugin will not be able to gather vendor specific attributes (but it can work without it)
		m.Log.Warnf(
			"nvme not found: verify that nvme is installed and it is in your PATH (or specified in config) to gather vendor specific attributes: %s",
			err.Error(),
		)
	}

	return nil
}

// Gather takes in an accumulator and adds the metrics that the SMART tools gather.
func (m *Smart) Gather(acc telegraf.Accumulator) error {
	var err error
	var scannedNVMeDevices []string
	var scannedNonNVMeDevices []string

	devicesFromConfig := m.Devices
	isNVMe := len(m.PathNVMe) != 0
	isVendorExtension := len(m.EnableExtensions) != 0

	if len(m.Devices) != 0 {
		m.addAttributes(acc, devicesFromConfig)

		// if nvme-cli is present, vendor specific attributes can be gathered
		if isVendorExtension && isNVMe {
			scannedNVMeDevices, _, err = m.scanAllDevices(true)
			if err != nil {
				return err
			}
			nvmeDevices := distinguishNVMeDevices(devicesFromConfig, scannedNVMeDevices)

			m.addVendorNVMeAttributes(acc, nvmeDevices)
		}
		return nil
	}
	scannedNVMeDevices, scannedNonNVMeDevices, err = m.scanAllDevices(false)
	if err != nil {
		return err
	}
	var devicesFromScan []string
	devicesFromScan = append(devicesFromScan, scannedNVMeDevices...)
	devicesFromScan = append(devicesFromScan, scannedNonNVMeDevices...)

	m.addAttributes(acc, devicesFromScan)
	if isVendorExtension && isNVMe {
		m.addVendorNVMeAttributes(acc, scannedNVMeDevices)
	}
	return nil
}

func (m *Smart) scanAllDevices(ignoreExcludes bool) ([]string, []string, error) {
	// this will return all devices (including NVMe devices) for smartctl version >= 7.0
	// for older versions this will return non NVMe devices
	devices, err := m.scanDevices(ignoreExcludes, "--scan")
	if err != nil {
		return nil, nil, err
	}

	// this will return only NVMe devices
	nvmeDevices, err := m.scanDevices(ignoreExcludes, "--scan", "--device=nvme")
	if err != nil {
		return nil, nil, err
	}

	// to handle all versions of smartctl this will return only non NVMe devices
	nonNVMeDevices := difference(devices, nvmeDevices)
	return nvmeDevices, nonNVMeDevices, nil
}

func distinguishNVMeDevices(userDevices, availableNVMeDevices []string) []string {
	var nvmeDevices []string

	for _, userDevice := range userDevices {
		for _, availableNVMeDevice := range availableNVMeDevices {
			// double check. E.g. in case when nvme0 is equal nvme0n1, will check if "nvme0" part is present.
			if strings.Contains(availableNVMeDevice, userDevice) || strings.Contains(userDevice, availableNVMeDevice) {
				nvmeDevices = append(nvmeDevices, userDevice)
			}
		}
	}
	return nvmeDevices
}

// Scan for S.M.A.R.T. devices from smartctl
func (m *Smart) scanDevices(ignoreExcludes bool, scanArgs ...string) ([]string, error) {
	out, err := runCmd(m.Timeout, m.UseSudo, m.PathSmartctl, scanArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to run command '%s %s': %w - %s", m.PathSmartctl, scanArgs, err, string(out))
	}
	var devices []string
	for _, line := range strings.Split(string(out), "\n") {
		dev := strings.Split(line, " ")
		if len(dev) <= 1 {
			continue
		}
		if !ignoreExcludes {
			if !excludedDev(m.Excludes, strings.TrimSpace(dev[0])) {
				devices = append(devices, strings.TrimSpace(dev[0]))
			}
		} else {
			devices = append(devices, strings.TrimSpace(dev[0]))
		}
	}
	return devices, nil
}

// Wrap with sudo
var runCmd = func(timeout config.Duration, sudo bool, command string, args ...string) ([]byte, error) {
	cmd := exec.Command(command, args...)
	if sudo {
		cmd = exec.Command("sudo", append([]string{"-n", command}, args...)...)
	}
	return internal.CombinedOutputTimeout(cmd, time.Duration(timeout))
}

func excludedDev(excludes []string, deviceLine string) bool {
	device := strings.Split(deviceLine, " ")
	if len(device) != 0 {
		for _, exclude := range excludes {
			if device[0] == exclude {
				return true
			}
		}
	}
	return false
}

// Add info and attributes for each S.M.A.R.T. device
func (m *Smart) addAttributes(acc telegraf.Accumulator, devices []string) {
	var wg sync.WaitGroup
	wg.Add(len(devices))
	for _, device := range devices {
		switch m.ReadMethod {
		case "concurrent":
			go m.gatherDisk(acc, device, &wg)
		case "sequential":
			m.gatherDisk(acc, device, &wg)
		default:
			wg.Done()
		}
	}

	wg.Wait()
}

func (m *Smart) addVendorNVMeAttributes(acc telegraf.Accumulator, devices []string) {
	nvmeDevices := getDeviceInfoForNVMeDisks(acc, devices, m.PathNVMe, m.Timeout, m.UseSudo)

	var wg sync.WaitGroup

	for _, device := range nvmeDevices {
		if contains(m.EnableExtensions, "auto-on") {
			//nolint:revive // one case switch on purpose to demonstrate potential extensions
			switch device.vendorID {
			case intelVID:
				wg.Add(1)
				switch m.ReadMethod {
				case "concurrent":
					go gatherIntelNVMeDisk(acc, m.Timeout, m.UseSudo, m.PathNVMe, device, &wg)
				case "sequential":
					gatherIntelNVMeDisk(acc, m.Timeout, m.UseSudo, m.PathNVMe, device, &wg)
				default:
					wg.Done()
				}
			}
		} else if contains(m.EnableExtensions, "Intel") && device.vendorID == intelVID {
			wg.Add(1)
			switch m.ReadMethod {
			case "concurrent":
				go gatherIntelNVMeDisk(acc, m.Timeout, m.UseSudo, m.PathNVMe, device, &wg)
			case "sequential":
				gatherIntelNVMeDisk(acc, m.Timeout, m.UseSudo, m.PathNVMe, device, &wg)
			default:
				wg.Done()
			}
		}
	}
	wg.Wait()
}

func getDeviceInfoForNVMeDisks(acc telegraf.Accumulator, devices []string, nvme string, timeout config.Duration, useSudo bool) []nvmeDevice {
	nvmeDevices := make([]nvmeDevice, 0, len(devices))
	for _, device := range devices {
		newDevice, err := gatherNVMeDeviceInfo(nvme, device, timeout, useSudo)
		if err != nil {
			acc.AddError(fmt.Errorf("cannot find device info for %s device", device))
			continue
		}
		nvmeDevices = append(nvmeDevices, newDevice)
	}
	return nvmeDevices
}

func gatherNVMeDeviceInfo(nvme, deviceName string, timeout config.Duration, useSudo bool) (device nvmeDevice, err error) {
	args := []string{"id-ctrl"}
	args = append(args, strings.Split(deviceName, " ")...)
	out, err := runCmd(timeout, useSudo, nvme, args...)
	if err != nil {
		return device, err
	}
	outStr := string(out)
	device, err = findNVMeDeviceInfo(outStr)
	if err != nil {
		return device, err
	}
	device.name = deviceName
	return device, nil
}

func findNVMeDeviceInfo(output string) (nvmeDevice, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var vid, sn, mn string

	for scanner.Scan() {
		line := scanner.Text()

		if matches := nvmeIDCtrlExpressionPattern.FindStringSubmatch(line); len(matches) > 2 {
			matches[1] = strings.TrimSpace(matches[1])
			matches[2] = strings.TrimSpace(matches[2])
			if matches[1] == "vid" {
				if _, err := fmt.Sscanf(matches[2], "%s", &vid); err != nil {
					return nvmeDevice{}, err
				}
			}
			if matches[1] == "sn" {
				sn = matches[2]
			}
			if matches[1] == "mn" {
				mn = matches[2]
			}
		}
	}

	newDevice := nvmeDevice{
		vendorID:     vid,
		model:        mn,
		serialNumber: sn,
	}
	return newDevice, nil
}

func gatherIntelNVMeDisk(acc telegraf.Accumulator, timeout config.Duration, usesudo bool, nvme string, device nvmeDevice, wg *sync.WaitGroup) {
	defer wg.Done()

	args := []string{"intel", "smart-log-add"}
	args = append(args, strings.Split(device.name, " ")...)
	out, e := runCmd(timeout, usesudo, nvme, args...)
	outStr := string(out)

	_, er := exitStatus(e)
	if er != nil {
		acc.AddError(fmt.Errorf("failed to run command '%s %s': %w - %s", nvme, strings.Join(args, " "), e, outStr))
		return
	}

	scanner := bufio.NewScanner(strings.NewReader(outStr))

	for scanner.Scan() {
		line := scanner.Text()
		fields := make(map[string]interface{})
		tags := map[string]string{
			"device":    path.Base(device.name),
			"model":     device.model,
			"serial_no": device.serialNumber,
		}

		// Create struct to initialize later with intel attributes.
		var (
			attr = struct {
				ID    string
				Name  string
				Parse func(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error
			}{}
			attrExists bool
		)

		if matches := intelExpressionPattern.FindStringSubmatch(line); len(matches) > 3 && len(matches[1]) > 1 {
			// Check if nvme shows metrics in deprecated format or in format with ID.
			// Based on that, an attribute map with metrics is chosen.
			// If string has more than one character it means it has KEY there, otherwise it's empty string ("").
			if separatedIDAndKey := nvmeIDSeparatePattern.FindStringSubmatch(matches[1]); len(strings.TrimSpace(separatedIDAndKey[2])) > 1 {
				matches[1] = strings.TrimSpace(separatedIDAndKey[2])
				attr, attrExists = intelAttributes[matches[1]]
			} else {
				matches[1] = strings.TrimSpace(matches[1])
				attr, attrExists = intelAttributesDeprecatedFormat[matches[1]]
			}

			matches[3] = strings.TrimSpace(matches[3])

			if attrExists {
				tags["name"] = attr.Name
				if attr.ID != "" {
					tags["id"] = attr.ID
				}

				parse := parseCommaSeparatedIntWithAccumulator
				if attr.Parse != nil {
					parse = attr.Parse
				}

				if err := parse(acc, fields, tags, matches[3]); err != nil {
					continue
				}
			}
		}
	}
}

func (m *Smart) gatherDisk(acc telegraf.Accumulator, device string, wg *sync.WaitGroup) {
	defer wg.Done()
	// smartctl 5.41 & 5.42 have are broken regarding handling of --nocheck/-n
	args := []string{"--info", "--health", "--attributes", "--tolerance=verypermissive", "-n", m.Nocheck, "--format=brief"}
	args = append(args, strings.Split(device, " ")...)
	out, e := runCmd(m.Timeout, m.UseSudo, m.PathSmartctl, args...)
	outStr := string(out)

	// Ignore all exit statuses except if it is a command line parse error
	exitStatus, er := exitStatus(e)
	if er != nil {
		acc.AddError(fmt.Errorf("failed to run command '%s %s': %w - %s", m.PathSmartctl, strings.Join(args, " "), e, outStr))
		return
	}

	deviceTags := make(map[string]string)
	if m.TagWithDeviceType {
		deviceNode := strings.SplitN(device, " ", 2)
		deviceTags["device"] = path.Base(deviceNode[0])
		if len(deviceNode) == 2 && deviceNode[1] != "" {
			deviceTags["device_type"] = strings.TrimPrefix(deviceNode[1], "-d ")
		}
	} else {
		deviceNode := strings.Split(device, " ")[0]
		deviceTags["device"] = path.Base(deviceNode)
	}
	deviceFields := make(map[string]interface{})
	deviceFields["exit_status"] = exitStatus

	scanner := bufio.NewScanner(strings.NewReader(outStr))

	for scanner.Scan() {
		line := scanner.Text()

		model := modelInfo.FindStringSubmatch(line)
		if len(model) > 2 {
			deviceTags["model"] = model[2]
		}

		serial := serialInfo.FindStringSubmatch(line)
		if len(serial) > 1 {
			deviceTags["serial_no"] = serial[1]
		}

		wwn := wwnInfo.FindStringSubmatch(line)
		if len(wwn) > 1 {
			deviceTags["wwn"] = strings.ReplaceAll(wwn[1], " ", "")
		}

		capacity := userCapacityInfo.FindStringSubmatch(line)
		if len(capacity) > 1 {
			deviceTags["capacity"] = strings.ReplaceAll(capacity[1], ",", "")
		}

		enabled := smartEnabledInfo.FindStringSubmatch(line)
		if len(enabled) > 1 {
			deviceTags["enabled"] = enabled[1]
		}

		health := smartOverallHealth.FindStringSubmatch(line)
		if len(health) > 2 {
			deviceFields["health_ok"] = health[2] == "PASSED" || health[2] == "OK"
		}

		// checks to see if there is a power mode to print to user
		// if not look for Device is in STANDBY which happens when
		// nocheck is set to standby (will exit to not spin up the disk)
		// otherwise nothing is found so nothing is printed (NVMe does not show power)
		if power := powermodeInfo.FindStringSubmatch(line); len(power) > 1 {
			deviceTags["power"] = power[1]
		} else {
			if power := standbyInfo.FindStringSubmatch(line); len(power) > 1 {
				deviceTags["power"] = power[1]
			}
		}

		tags := make(map[string]string)
		fields := make(map[string]interface{})

		if m.Attributes {
			// add power mode
			keys := [...]string{"device", "device_type", "model", "serial_no", "wwn", "capacity", "enabled", "power"}
			for _, key := range keys {
				if value, ok := deviceTags[key]; ok {
					tags[key] = value
				}
			}
		}

		attr := attribute.FindStringSubmatch(line)
		if len(attr) > 1 {
			// attribute has been found, add it only if m.Attributes is true
			if m.Attributes {
				tags["id"] = attr[1]
				tags["name"] = attr[2]
				tags["flags"] = attr[3]

				fields["exit_status"] = exitStatus
				if i, err := strconv.ParseInt(attr[4], 10, 64); err == nil {
					fields["value"] = i
				}
				if i, err := strconv.ParseInt(attr[5], 10, 64); err == nil {
					fields["worst"] = i
				}
				if i, err := strconv.ParseInt(attr[6], 10, 64); err == nil {
					fields["threshold"] = i
				}

				tags["fail"] = attr[7]
				if val, err := parseRawValue(attr[8]); err == nil {
					fields["raw_value"] = val
				}

				acc.AddFields("smart_attribute", fields, tags)
			}

			// If the attribute matches on the one in deviceFieldIDs
			// save the raw value to a field.
			if field, ok := deviceFieldIDs[attr[1]]; ok {
				if val, err := parseRawValue(attr[8]); err == nil {
					deviceFields[field] = val
				}
			}

			if len(attr) > 4 {
				// If the attribute name matches on in deviceFieldNames
				// save the value to a field
				if field, ok := deviceFieldNames[attr[2]]; ok {
					if val, err := parseRawValue(attr[4]); err == nil {
						deviceFields[field] = val
					}
				}
			}
		} else {
			// what was found is not a vendor attribute
			if matches := sasNVMeAttr.FindStringSubmatch(line); len(matches) > 2 {
				if attr, ok := sasNVMeAttributes[matches[1]]; ok {
					tags["name"] = attr.Name
					if attr.ID != "" {
						tags["id"] = attr.ID
					}

					parse := parseCommaSeparatedInt
					if attr.Parse != nil {
						parse = attr.Parse
					}

					if err := parse(fields, deviceFields, matches[2]); err != nil {
						continue
					}
					// if the field is classified as an attribute, only add it
					// if m.Attributes is true
					if m.Attributes {
						acc.AddFields("smart_attribute", fields, tags)
					}
				}
			}
		}
	}
	acc.AddFields("smart_device", deviceFields, deviceTags)
}

// Command line parse errors are denoted by the exit code having the 0 bit set.
// All other errors are drive/communication errors and should be ignored.
func exitStatus(err error) (int, error) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus(), nil
		}
	}
	return 0, err
}

func contains(args []string, element string) bool {
	for _, arg := range args {
		if arg == element {
			return true
		}
	}
	return false
}

func difference(a, b []string) []string {
	mb := make(map[string]struct{}, len(b))
	for _, x := range b {
		mb[x] = struct{}{}
	}
	var diff []string
	for _, x := range a {
		if _, found := mb[x]; !found {
			diff = append(diff, x)
		}
	}
	return diff
}

func parseRawValue(rawVal string) (int64, error) {
	// Integer
	if i, err := strconv.ParseInt(rawVal, 10, 64); err == nil {
		return i, nil
	}

	// Duration: 65h+33m+09.259s
	unit := regexp.MustCompile("^(.*)([hms])$")
	parts := strings.Split(rawVal, "+")
	if len(parts) == 0 {
		return 0, fmt.Errorf("couldn't parse RAW_VALUE %q", rawVal)
	}

	duration := int64(0)
	for _, part := range parts {
		timePart := unit.FindStringSubmatch(part)
		if len(timePart) == 0 {
			continue
		}
		switch timePart[2] {
		case "h":
			duration += parseInt(timePart[1]) * int64(3600)
		case "m":
			duration += parseInt(timePart[1]) * int64(60)
		case "s":
			// drop fractions of seconds
			duration += parseInt(strings.Split(timePart[1], ".")[0])
		default:
			// Unknown, ignore
		}
	}
	return duration, nil
}

func parseBytesWritten(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error {
	var value int64

	if _, err := fmt.Sscanf(str, "sectors: %d", &value); err != nil {
		return err
	}
	fields["raw_value"] = value
	acc.AddFields("smart_attribute", fields, tags)
	return nil
}

func parseThermalThrottle(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error {
	var percentage float64
	var count int64

	if _, err := fmt.Sscanf(str, "%f%%, cnt: %d", &percentage, &count); err != nil {
		return err
	}

	fields["raw_value"] = percentage
	tags["name"] = "Thermal_Throttle_Status_Prc"
	acc.AddFields("smart_attribute", fields, tags)

	fields["raw_value"] = count
	tags["name"] = "Thermal_Throttle_Status_Cnt"
	acc.AddFields("smart_attribute", fields, tags)

	return nil
}

func parseWearLeveling(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error {
	var vmin, vmax, avg int64

	if _, err := fmt.Sscanf(str, "min: %d, max: %d, avg: %d", &vmin, &vmax, &avg); err != nil {
		return err
	}
	values := []int64{vmin, vmax, avg}
	for i, submetricName := range []string{"Min", "Max", "Avg"} {
		fields["raw_value"] = values[i]
		tags["name"] = "Wear_Leveling_" + submetricName
		acc.AddFields("smart_attribute", fields, tags)
	}

	return nil
}

func parseTimedWorkload(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error {
	var value float64

	if _, err := fmt.Sscanf(str, "%f", &value); err != nil {
		return err
	}
	fields["raw_value"] = value
	acc.AddFields("smart_attribute", fields, tags)
	return nil
}

func parseInt(str string) int64 {
	if i, err := strconv.ParseInt(str, 10, 64); err == nil {
		return i
	}
	return 0
}

func parseCommaSeparatedInt(fields, _ map[string]interface{}, str string) error {
	// remove any non-utf8 values
	// '1\xa0292' --> 1292
	value := strings.ToValidUTF8(strings.Join(strings.Fields(str), ""), "")

	// remove any non-alphanumeric values
	// '16,626,888' --> 16626888
	// '16 829 004' --> 16829004
	numRegex, err := regexp.Compile(`[^0-9\-]+`)
	if err != nil {
		return errors.New("failed to compile numeric regex")
	}
	value = numRegex.ReplaceAllString(value, "")

	i, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return err
	}

	fields["raw_value"] = i

	return nil
}

func parsePercentageInt(fields, deviceFields map[string]interface{}, str string) error {
	return parseCommaSeparatedInt(fields, deviceFields, strings.TrimSuffix(str, "%"))
}

func parseDataUnits(fields, deviceFields map[string]interface{}, str string) error {
	// Remove everything after '['
	units := strings.Split(str, "[")[0]
	return parseCommaSeparatedInt(fields, deviceFields, units)
}

func parseCommaSeparatedIntWithAccumulator(acc telegraf.Accumulator, fields map[string]interface{}, tags map[string]string, str string) error {
	i, err := strconv.ParseInt(strings.ReplaceAll(str, ",", ""), 10, 64)
	if err != nil {
		return err
	}

	fields["raw_value"] = i
	acc.AddFields("smart_attribute", fields, tags)
	return nil
}

func parseTemperature(fields, deviceFields map[string]interface{}, str string) error {
	var temp int64
	if _, err := fmt.Sscanf(str, "%d C", &temp); err != nil {
		return err
	}

	fields["raw_value"] = temp
	deviceFields["temp_c"] = temp

	return nil
}

func parseTemperatureSensor(fields, _ map[string]interface{}, str string) error {
	var temp int64
	if _, err := fmt.Sscanf(str, "%d C", &temp); err != nil {
		return err
	}

	fields["raw_value"] = temp

	return nil
}

func validatePath(filePath string) error {
	pathInfo, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("provided path does not exist: [%s]", filePath)
	}
	if mode := pathInfo.Mode(); !mode.IsRegular() {
		return fmt.Errorf("provided path does not point to a regular file: [%s]", filePath)
	}
	return nil
}

func init() {
	// Set LC_NUMERIC to uniform numeric output from cli tools
	_ = os.Setenv("LC_NUMERIC", "en_US.UTF-8")

	inputs.Add("smart", func() telegraf.Input {
		m := newSmart()
		m.Nocheck = "standby"
		return m
	})
}
