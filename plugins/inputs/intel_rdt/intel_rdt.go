//go:generate ../../../tools/readme_config_includer/generator
//go:build !windows

package intel_rdt

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var pqosMetricOrder = map[int]string{
	0: "IPC",        // Instructions Per Cycle
	1: "LLC_Misses", // Cache Misses
	2: "LLC",        // L3 Cache Occupancy
	3: "MBL",        // Memory Bandwidth on Local NUMA Node
	4: "MBR",        // Memory Bandwidth on Remote NUMA Node
	5: "MBT",        // Total Memory Bandwidth
}

const (
	timestampFormat           = "2006-01-02 15:04:05"
	defaultSamplingInterval   = 10
	pqosInitOutputLinesNumber = 4
	numberOfMetrics           = 6
	secondsDenominator        = 10
)

type IntelRDT struct {
	PqosPath         string          `toml:"pqos_path"`
	Cores            []string        `toml:"cores"`
	Processes        []string        `toml:"processes"`
	SamplingInterval int32           `toml:"sampling_interval"`
	ShortenedMetrics bool            `toml:"shortened_metrics"`
	UseSudo          bool            `toml:"use_sudo"`
	Log              telegraf.Logger `toml:"-"`

	publisher        publisher
	processor        processesHandler
	stopPQOSChan     chan bool
	quitChan         chan struct{}
	errorChan        chan error
	parsedCores      []string
	processesPIDsMap map[string]string
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

type processMeasurement struct {
	name        string
	measurement string
}

type splitCSVLine struct {
	timeValue        string
	metricsValues    []string
	coreOrPIDsValues []string
}

func (*IntelRDT) SampleConfig() string {
	return sampleConfig
}

func (r *IntelRDT) Start(acc telegraf.Accumulator) error {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	r.processor = newProcessor()
	r.publisher = newPublisher(acc, r.Log, r.ShortenedMetrics)

	err := r.initialize()
	if err != nil {
		return err
	}

	r.publisher.publish(ctx)
	go r.errorHandler(ctx)
	go r.scheduler(ctx)

	return nil
}

func (*IntelRDT) Gather(telegraf.Accumulator) error {
	return nil
}

func (r *IntelRDT) Stop() {
	r.cancel()
	r.wg.Wait()
}

func (r *IntelRDT) initialize() error {
	r.stopPQOSChan = make(chan bool)
	r.quitChan = make(chan struct{})
	r.errorChan = make(chan error)

	err := validatePqosPath(r.PqosPath)
	if err != nil {
		return err
	}
	if len(r.Cores) != 0 && len(r.Processes) != 0 {
		return errors.New("monitoring start error, process and core tracking can not be done simultaneously")
	}
	if len(r.Cores) == 0 && len(r.Processes) == 0 {
		return errors.New("monitoring start error, at least one of cores or processes must be provided in config")
	}
	if r.SamplingInterval == 0 {
		r.SamplingInterval = defaultSamplingInterval
	}
	if err := validateInterval(r.SamplingInterval); err != nil {
		return err
	}
	r.parsedCores, err = parseCoresConfig(r.Cores)
	if err != nil {
		return err
	}
	r.processesPIDsMap, err = r.associateProcessesWithPIDs(r.Processes)
	if err != nil {
		return err
	}
	return nil
}

func (r *IntelRDT) errorHandler(ctx context.Context) {
	r.wg.Add(1)
	defer r.wg.Done()
	for {
		select {
		case err := <-r.errorChan:
			if err != nil {
				r.Log.Error(fmt.Sprintf("Error: %v", err))
				r.quitChan <- struct{}{}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *IntelRDT) scheduler(ctx context.Context) {
	r.wg.Add(1)
	defer r.wg.Done()
	interval := time.Duration(r.SamplingInterval)
	ticker := time.NewTicker(interval * time.Second / secondsDenominator)

	r.createArgsAndStartPQOS(ctx)

	for {
		select {
		case <-ticker.C:
			if len(r.Processes) != 0 {
				err := r.checkPIDsAssociation(ctx)
				if err != nil {
					r.errorChan <- err
				}
			}
		case <-r.quitChan:
			r.cancel()
			return
		case <-ctx.Done():
			return
		}
	}
}

func (r *IntelRDT) checkPIDsAssociation(ctx context.Context) error {
	newProcessesPIDsMap, err := r.associateProcessesWithPIDs(r.Processes)
	if err != nil {
		return err
	}
	// change in PIDs association appears
	if !cmp.Equal(newProcessesPIDsMap, r.processesPIDsMap) {
		r.Log.Warnf("PIDs association has changed. Refreshing...")
		if len(r.processesPIDsMap) != 0 {
			r.stopPQOSChan <- true
		}
		r.processesPIDsMap = newProcessesPIDsMap
		r.createArgsAndStartPQOS(ctx)
	}
	return nil
}

func (r *IntelRDT) associateProcessesWithPIDs(providedProcesses []string) (map[string]string, error) {
	availableProcesses, err := r.processor.getAllProcesses()
	if err != nil {
		return nil, errors.New("cannot gather information of all available processes")
	}

	mapProcessPIDs := make(map[string]string, len(availableProcesses))
	for _, availableProcess := range availableProcesses {
		if choice.Contains(availableProcess.Name, providedProcesses) {
			pid := availableProcess.PID
			mapProcessPIDs[availableProcess.Name] = mapProcessPIDs[availableProcess.Name] + strconv.Itoa(pid) + ","
		}
	}
	for key := range mapProcessPIDs {
		mapProcessPIDs[key] = strings.TrimSuffix(mapProcessPIDs[key], ",")
	}
	return mapProcessPIDs, nil
}

func (r *IntelRDT) createArgsAndStartPQOS(ctx context.Context) {
	args := []string{"-r", "--iface-os", "--mon-file-type=csv", fmt.Sprintf("--mon-interval=%d", r.SamplingInterval)}

	if len(r.parsedCores) != 0 {
		coresArg := createArgCores(r.parsedCores)
		args = append(args, coresArg)
		go r.readData(ctx, args, nil)
	} else if len(r.processesPIDsMap) != 0 {
		processArg := createArgProcess(r.processesPIDsMap)
		args = append(args, processArg)
		go r.readData(ctx, args, r.processesPIDsMap)
	}
}

func (r *IntelRDT) readData(ctx context.Context, args []string, processesPIDsAssociation map[string]string) {
	r.wg.Add(1)
	defer r.wg.Done()

	cmd := exec.Command(r.PqosPath, args...)

	if r.UseSudo {
		// run pqos with `/bin/sh -c "sudo /path/to/pqos ..."`
		args = []string{"-c", fmt.Sprintf("sudo %s %s", r.PqosPath, strings.ReplaceAll(strings.Join(args, " "), ";", "\\;"))}
		cmd = exec.Command("/bin/sh", args...)
	}

	cmdReader, err := cmd.StdoutPipe()
	if err != nil {
		r.errorChan <- err
	}
	go r.processOutput(cmdReader, processesPIDsAssociation)

	go func() {
		for {
			select {
			case <-r.stopPQOSChan:
				if err := shutDownPqos(cmd); err != nil {
					r.Log.Error(err)
				}
				return
			case <-ctx.Done():
				if err := shutDownPqos(cmd); err != nil {
					r.Log.Error(err)
				}
				return
			}
		}
	}()
	err = cmd.Start()
	if err != nil {
		r.Log.Errorf("pqos: %v", err)
		return
	}
	err = cmd.Wait()
	if err != nil {
		r.Log.Errorf("pqos: %v", err)
	}
}

func (r *IntelRDT) processOutput(cmdReader io.ReadCloser, processesPIDsAssociation map[string]string) {
	reader := bufio.NewScanner(cmdReader)
	/*
		Omit constant, first 4 lines :
		"NOTE:  Mixed use of MSR and kernel interfaces to manage
				CAT or CMT & MBM may lead to unexpected behavior.\n"
		CMT/MBM reset successful
		"Time,Core,IPC,LLC Misses,LLC[KB],MBL[MB/s],MBR[MB/s],MBT[MB/s]\n"
	*/
	toOmit := pqosInitOutputLinesNumber

	if len(r.parsedCores) != 0 { // omit first measurements which are zeroes
		toOmit = toOmit + len(r.parsedCores)
	} else if len(processesPIDsAssociation) != 0 { // specify how many lines should pass before stopping
		toOmit = toOmit + len(processesPIDsAssociation)
	}
	for omitCounter := 0; omitCounter < toOmit; omitCounter++ {
		reader.Scan()
	}
	for reader.Scan() {
		out := reader.Text()
		// to handle situation when monitored PID disappear and "err" is shown in output
		if strings.Contains(out, "err") {
			continue
		}
		if len(r.Processes) != 0 {
			newMetric := processMeasurement{}

			pids, err := findPIDsInMeasurement(out)
			if err != nil {
				r.Log.Warnf("Skipping measurement: %v", err)
				continue
			}
			for processName, PIDsProcess := range processesPIDsAssociation {
				if pids == PIDsProcess {
					newMetric.name = processName
					newMetric.measurement = out
				}
			}
			r.publisher.bufferChanProcess <- newMetric
		} else {
			r.publisher.bufferChanCores <- out
		}
	}
}

func shutDownPqos(pqos *exec.Cmd) error {
	timeout := time.Second * 2

	if pqos.Process != nil {
		//nolint:errcheck // try to send interrupt signal, ignore err for now
		pqos.Process.Signal(os.Interrupt)

		// wait and constantly check if pqos is still running
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		for {
			if err := pqos.Process.Signal(syscall.Signal(0)); errors.Is(err, os.ErrProcessDone) {
				return nil
			} else if ctx.Err() != nil {
				break
			}
		}

		// if pqos is still running after some period, try to kill it
		// this will send SIGTERM to pqos, and leave garbage in `/sys/fs/resctrl/mon_groups`
		// fixed in https://github.com/intel/intel-cmt-cat/issues/197
		err := pqos.Process.Kill()
		if err != nil {
			return fmt.Errorf("failed to shut down pqos: %w", err)
		}
	}
	return nil
}

func createArgCores(cores []string) string {
	allGroupsArg := "--mon-core="
	for _, coreGroup := range cores {
		argGroup := createArgsForGroups(strings.Split(coreGroup, ","))
		allGroupsArg = allGroupsArg + argGroup
	}
	return allGroupsArg
}

func createArgProcess(processPIDs map[string]string) string {
	allPIDsArg := "--mon-pid="
	for _, PIDs := range processPIDs {
		argPIDs := createArgsForGroups(strings.Split(PIDs, ","))
		allPIDsArg = allPIDsArg + argPIDs
	}
	return allPIDsArg
}

func createArgsForGroups(coresOrPIDs []string) string {
	template := "all:[%s];mbt:[%s];"
	group := ""

	for _, coreOrPID := range coresOrPIDs {
		group = group + coreOrPID + ","
	}
	if group != "" {
		group = strings.TrimSuffix(group, ",")
		return fmt.Sprintf(template, group, group)
	}
	return ""
}

func validatePqosPath(pqosPath string) error {
	if len(pqosPath) == 0 {
		return errors.New("monitoring start error, can not find pqos executable")
	}
	pathInfo, err := os.Stat(pqosPath)
	if os.IsNotExist(err) {
		return errors.New("monitoring start error, provided pqos path not exist")
	}
	if mode := pathInfo.Mode(); !mode.IsRegular() {
		return errors.New("monitoring start error, provided pqos path does not point to a regular file")
	}
	return nil
}

func parseCoresConfig(cores []string) ([]string, error) {
	var allCores []int

	parsedCores := make([]string, 0, len(cores))
	for _, singleCoreGroup := range cores {
		var actualGroupOfCores []int
		separatedCores := strings.Split(singleCoreGroup, ",")

		for _, coreStr := range separatedCores {
			actualCores, err := validateAndParseCores(coreStr)
			if err != nil {
				return nil, fmt.Errorf("wrong cores input config data format: %w", err)
			}
			if checkForDuplicates(allCores, actualCores) {
				return nil, errors.New("wrong cores input config data format: core value cannot be duplicated")
			}
			actualGroupOfCores = append(actualGroupOfCores, actualCores...)
			allCores = append(allCores, actualGroupOfCores...)
		}
		parsedCores = append(parsedCores, arrayToString(actualGroupOfCores))
	}

	return parsedCores, nil
}

func validateAndParseCores(coreStr string) ([]int, error) {
	var processedCores []int
	if strings.Contains(coreStr, "-") {
		rangeValues := strings.Split(coreStr, "-")

		if len(rangeValues) != 2 {
			return nil, errors.New("more than two values in range")
		}

		startValue, err := strconv.Atoi(rangeValues[0])
		if err != nil {
			return nil, err
		}
		stopValue, err := strconv.Atoi(rangeValues[1])
		if err != nil {
			return nil, err
		}

		if startValue > stopValue {
			return nil, errors.New("first value cannot be higher than second")
		}

		rangeOfCores := makeRange(startValue, stopValue)
		processedCores = append(processedCores, rangeOfCores...)
	} else {
		newCore, err := strconv.Atoi(coreStr)
		if err != nil {
			return nil, err
		}
		processedCores = append(processedCores, newCore)
	}
	return processedCores, nil
}

func findPIDsInMeasurement(measurements string) (string, error) {
	// to distinguish PIDs from Cores (PIDs should be in quotes)
	var insideQuoteRegex = regexp.MustCompile(`"(.*?)"`)
	pidsMatch := insideQuoteRegex.FindStringSubmatch(measurements)
	if len(pidsMatch) < 2 {
		return "", errors.New("cannot find PIDs in measurement line")
	}
	pids := pidsMatch[1]
	return pids, nil
}

func splitCSVLineIntoValues(line string) (splitCSVLine, error) {
	values, err := splitMeasurementLine(line)
	if err != nil {
		return splitCSVLine{}, err
	}

	timeValue := values[0]
	// Because pqos csv format is broken when many cores are involved in PID or
	// group of PIDs, there is need to work around it. E.g.:
	// Time,PID,Core,IPC,LLC Misses,LLC[KB],MBL[MB/s],MBR[MB/s],MBT[MB/s]
	// 2020-08-12 13:34:36,"45417,29170,",37,44,0.00,0,0.0,0.0,0.0,0.0
	metricsValues := values[len(values)-numberOfMetrics:]
	coreOrPIDsValues := values[1 : len(values)-numberOfMetrics]

	return splitCSVLine{timeValue, metricsValues, coreOrPIDsValues}, nil
}

func validateInterval(interval int32) error {
	if interval < 0 {
		return errors.New("interval cannot be lower than 0")
	}
	return nil
}

func splitMeasurementLine(line string) ([]string, error) {
	values := strings.Split(line, ",")
	if len(values) < 8 {
		return nil, fmt.Errorf("not valid line format from pqos: %s", values)
	}
	return values, nil
}

func parseTime(value string) (time.Time, error) {
	timestamp, err := time.ParseInLocation(timestampFormat, value, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	return timestamp, nil
}

func parseFloat(value string) (float64, error) {
	result, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return result, err
	}
	return result, nil
}

func arrayToString(array []int) string {
	result := ""
	for _, value := range array {
		result = fmt.Sprintf("%s%d,", result, value)
	}
	return strings.TrimSuffix(result, ",")
}

func checkForDuplicates(values, valuesToCheck []int) bool {
	for _, value := range values {
		for _, valueToCheck := range valuesToCheck {
			if value == valueToCheck {
				return true
			}
		}
	}
	return false
}

func makeRange(low, high int) []int {
	a := make([]int, high-low+1)
	for i := range a {
		a[i] = low + i
	}
	return a
}

func init() {
	inputs.Add("intel_rdt", func() telegraf.Input {
		rdt := IntelRDT{}
		pathPqos, err := exec.LookPath("pqos")
		if len(pathPqos) > 0 && err != nil {
			rdt.PqosPath = pathPqos
		}
		return &rdt
	})
}
