//go:generate ../../../tools/readme_config_includer/generator
//go:build windows

package win_perf_counters

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var defaultMaxBufferSize = config.Size(100 * 1024 * 1024)

type WinPerfCounters struct {
	PrintValid                 bool `toml:"PrintValid"`
	PreVistaSupport            bool `toml:"PreVistaSupport" deprecated:"1.7.0;1.35.0;determined dynamically"`
	UsePerfCounterTime         bool
	Object                     []perfObject
	CountersRefreshInterval    config.Duration
	UseWildcardsExpansion      bool
	LocalizeWildcardsExpansion bool
	IgnoredErrors              []string `toml:"IgnoredErrors"`
	MaxBufferSize              config.Size
	Sources                    []string

	Log telegraf.Logger

	lastRefreshed time.Time
	queryCreator  PerformanceQueryCreator
	hostCounters  map[string]*hostCountersInfo
	// cached os.Hostname()
	cachedHostname string
}

type hostCountersInfo struct {
	// computer name used as key and for printing
	computer string
	// computer name used in tag
	tag       string
	counters  []*counter
	query     PerformanceQuery
	timestamp time.Time
}

type perfObject struct {
	Sources       []string
	ObjectName    string
	Counters      []string
	Instances     []string
	Measurement   string
	WarnOnMissing bool
	FailOnMissing bool
	IncludeTotal  bool
	UseRawValues  bool
}

type counter struct {
	counterPath   string
	computer      string
	objectName    string
	counter       string
	instance      string
	measurement   string
	includeTotal  bool
	useRawValue   bool
	counterHandle pdhCounterHandle
}

type instanceGrouping struct {
	name       string
	instance   string
	objectName string
}

type fieldGrouping map[instanceGrouping]map[string]interface{}

var sanitizedChars = strings.NewReplacer("/sec", "_persec", "/Sec", "_persec",
	" ", "_", "%", "Percent", `\`, "")

// extractCounterInfoFromCounterPath gets object name, instance name (if available) and counter name from counter path
// General Counter path pattern is: \\computer\object(parent/instance#index)\counter
// parent/instance#index part is skipped in single instance objects (e.g. Memory): \\computer\object\counter
//
//nolint:revive //function-result-limit conditionally 5 return results allowed
func extractCounterInfoFromCounterPath(counterPath string) (computer string, object string, instance string, counter string, err error) {
	leftComputerBorderIndex := -1
	rightObjectBorderIndex := -1
	leftObjectBorderIndex := -1
	leftCounterBorderIndex := -1
	rightInstanceBorderIndex := -1
	leftInstanceBorderIndex := -1
	var bracketLevel int

	for i := len(counterPath) - 1; i >= 0; i-- {
		switch counterPath[i] {
		case '\\':
			if bracketLevel == 0 {
				if leftCounterBorderIndex == -1 {
					leftCounterBorderIndex = i
				} else if leftObjectBorderIndex == -1 {
					leftObjectBorderIndex = i
				} else if leftComputerBorderIndex == -1 {
					leftComputerBorderIndex = i
				}
			}
		case '(':
			bracketLevel--
			if leftInstanceBorderIndex == -1 && bracketLevel == 0 && leftObjectBorderIndex == -1 && leftCounterBorderIndex > -1 {
				leftInstanceBorderIndex = i
				rightObjectBorderIndex = i
			}
		case ')':
			if rightInstanceBorderIndex == -1 && bracketLevel == 0 && leftCounterBorderIndex > -1 {
				rightInstanceBorderIndex = i
			}
			bracketLevel++
		}
	}
	if rightObjectBorderIndex == -1 {
		rightObjectBorderIndex = leftCounterBorderIndex
	}
	if rightObjectBorderIndex == -1 || leftObjectBorderIndex == -1 {
		return "", "", "", "", errors.New("cannot parse object from: " + counterPath)
	}

	if leftComputerBorderIndex > -1 {
		// validate there is leading \\ and not empty computer (\\\O)
		if leftComputerBorderIndex != 1 || leftComputerBorderIndex == leftObjectBorderIndex-1 {
			return "", "", "", "", errors.New("cannot parse computer from: " + counterPath)
		}
		computer = counterPath[leftComputerBorderIndex+1 : leftObjectBorderIndex]
	}

	if leftInstanceBorderIndex > -1 && rightInstanceBorderIndex > -1 {
		instance = counterPath[leftInstanceBorderIndex+1 : rightInstanceBorderIndex]
	} else if (leftInstanceBorderIndex == -1 && rightInstanceBorderIndex > -1) || (leftInstanceBorderIndex > -1 && rightInstanceBorderIndex == -1) {
		return "", "", "", "", errors.New("cannot parse instance from: " + counterPath)
	}
	object = counterPath[leftObjectBorderIndex+1 : rightObjectBorderIndex]
	counter = counterPath[leftCounterBorderIndex+1:]
	return computer, object, instance, counter, nil
}

func (*WinPerfCounters) SampleConfig() string {
	return sampleConfig
}

func (m *WinPerfCounters) hostname() string {
	if m.cachedHostname != "" {
		return m.cachedHostname
	}
	hostname, err := os.Hostname()
	if err != nil {
		m.cachedHostname = "localhost"
	} else {
		m.cachedHostname = hostname
	}
	return m.cachedHostname
}

//nolint:revive //argument-limit conditionally more arguments allowed for helper function
func newCounter(
	counterHandle pdhCounterHandle,
	counterPath string,
	computer string,
	objectName string,
	instance string,
	counterName string,
	measurement string,
	includeTotal bool,
	useRawValue bool,
) *counter {
	measurementName := sanitizedChars.Replace(measurement)
	if measurementName == "" {
		measurementName = "win_perf_counters"
	}
	newCounterName := sanitizedChars.Replace(counterName)
	if useRawValue {
		newCounterName += "_Raw"
	}
	return &counter{counterPath, computer, objectName, newCounterName, instance, measurementName,
		includeTotal, useRawValue, counterHandle}
}

//nolint:revive //argument-limit conditionally more arguments allowed
func (m *WinPerfCounters) AddItem(counterPath, computer, objectName, instance, counterName, measurement string, includeTotal bool, useRawValue bool) error {
	origCounterPath := counterPath
	var err error
	var counterHandle pdhCounterHandle

	sourceTag := computer
	if computer == "localhost" {
		sourceTag = m.hostname()
	}
	if m.hostCounters == nil {
		m.hostCounters = make(map[string]*hostCountersInfo)
	}
	hostCounter, ok := m.hostCounters[computer]
	if !ok {
		hostCounter = &hostCountersInfo{computer: computer, tag: sourceTag}
		m.hostCounters[computer] = hostCounter
		hostCounter.query = m.queryCreator.NewPerformanceQuery(computer, uint32(m.MaxBufferSize))
		if err := hostCounter.query.Open(); err != nil {
			return err
		}
		hostCounter.counters = make([]*counter, 0)
	}

	if !hostCounter.query.IsVistaOrNewer() {
		counterHandle, err = hostCounter.query.AddCounterToQuery(counterPath)
		if err != nil {
			return err
		}
	} else {
		counterHandle, err = hostCounter.query.AddEnglishCounterToQuery(counterPath)
		if err != nil {
			return err
		}
	}

	if m.UseWildcardsExpansion {
		origInstance := instance
		counterPath, err = hostCounter.query.GetCounterPath(counterHandle)
		if err != nil {
			return err
		}
		counters, err := hostCounter.query.ExpandWildCardPath(counterPath)
		if err != nil {
			return err
		}

		_, origObjectName, _, origCounterName, err := extractCounterInfoFromCounterPath(origCounterPath)
		if err != nil {
			return err
		}

		for _, counterPath := range counters {
			_, err := hostCounter.query.AddCounterToQuery(counterPath)
			if err != nil {
				return err
			}

			computer, objectName, instance, counterName, err = extractCounterInfoFromCounterPath(counterPath)
			if err != nil {
				return err
			}

			var newItem *counter
			if !m.LocalizeWildcardsExpansion {
				// On localized installations of Windows, Telegraf
				// should return English metrics, but
				// ExpandWildCardPath returns localized counters. Undo
				// that by using the original object and counter
				// names, along with the expanded instance.

				var newInstance string
				if instance == "" {
					newInstance = emptyInstance
				} else {
					newInstance = instance
				}
				counterPath = formatPath(computer, origObjectName, newInstance, origCounterName)
				counterHandle, err = hostCounter.query.AddEnglishCounterToQuery(counterPath)
				if err != nil {
					return err
				}
				newItem = newCounter(
					counterHandle,
					counterPath,
					computer,
					origObjectName, instance,
					origCounterName,
					measurement,
					includeTotal,
					useRawValue,
				)
			} else {
				counterHandle, err = hostCounter.query.AddCounterToQuery(counterPath)
				if err != nil {
					return err
				}
				newItem = newCounter(
					counterHandle,
					counterPath,
					computer,
					objectName,
					instance,
					counterName,
					measurement,
					includeTotal,
					useRawValue,
				)
			}

			if instance == "_Total" && origInstance == "*" && !includeTotal {
				continue
			}

			hostCounter.counters = append(hostCounter.counters, newItem)

			if m.PrintValid {
				m.Log.Infof("Valid: %s", counterPath)
			}
		}
	} else {
		newItem := newCounter(
			counterHandle,
			counterPath,
			computer,
			objectName,
			instance,
			counterName,
			measurement,
			includeTotal,
			useRawValue,
		)
		hostCounter.counters = append(hostCounter.counters, newItem)
		if m.PrintValid {
			m.Log.Infof("Valid: %s", counterPath)
		}
	}

	return nil
}

const emptyInstance = "------"

func formatPath(computer, objectName, instance, counter string) string {
	path := ""
	if instance == emptyInstance {
		path = fmt.Sprintf(`\%s\%s`, objectName, counter)
	} else {
		path = fmt.Sprintf(`\%s(%s)\%s`, objectName, instance, counter)
	}
	if computer != "" && computer != "localhost" {
		path = fmt.Sprintf(`\\%s%s`, computer, path)
	}
	return path
}

func (m *WinPerfCounters) ParseConfig() error {
	var counterPath string

	if len(m.Sources) == 0 {
		m.Sources = []string{"localhost"}
	}

	if len(m.Object) == 0 {
		err := errors.New("no performance objects configured")
		return err
	}

	for _, PerfObject := range m.Object {
		computers := PerfObject.Sources
		if len(computers) == 0 {
			computers = m.Sources
		}
		for _, computer := range computers {
			if computer == "" {
				// localhost as a computer name in counter path doesn't work
				computer = "localhost"
			}
			for _, counter := range PerfObject.Counters {
				if len(PerfObject.Instances) == 0 {
					m.Log.Warnf("Missing 'Instances' param for object %q", PerfObject.ObjectName)
				}
				for _, instance := range PerfObject.Instances {
					objectName := PerfObject.ObjectName
					counterPath = formatPath(computer, objectName, instance, counter)

					err := m.AddItem(counterPath, computer, objectName, instance, counter,
						PerfObject.Measurement, PerfObject.IncludeTotal, PerfObject.UseRawValues)
					if err != nil {
						if PerfObject.FailOnMissing || PerfObject.WarnOnMissing {
							m.Log.Errorf("Invalid counterPath %q: %s", counterPath, err.Error())
						}
						if PerfObject.FailOnMissing {
							return err
						}
					}
				}
			}
		}
	}

	return nil
}

func (m *WinPerfCounters) checkError(err error) error {
	var pdhErr *pdhError
	if errors.As(err, &pdhErr) {
		for _, ignoredErrors := range m.IgnoredErrors {
			if PDHErrors[pdhErr.ErrorCode] == ignoredErrors {
				return nil
			}
		}

		return err
	}
	return err
}

func (m *WinPerfCounters) Gather(acc telegraf.Accumulator) error {
	// Parse the config once
	var err error

	if m.lastRefreshed.IsZero() || (m.CountersRefreshInterval > 0 && m.lastRefreshed.Add(time.Duration(m.CountersRefreshInterval)).Before(time.Now())) {
		if err := m.cleanQueries(); err != nil {
			return err
		}

		if err := m.ParseConfig(); err != nil {
			return err
		}
		for _, hostCounterSet := range m.hostCounters {
			// some counters need two data samples before computing a value
			if err = hostCounterSet.query.CollectData(); err != nil {
				return m.checkError(err)
			}
		}
		m.lastRefreshed = time.Now()
		// minimum time between collecting two samples
		time.Sleep(time.Second)
	}

	for _, hostCounterSet := range m.hostCounters {
		if m.UsePerfCounterTime && hostCounterSet.query.IsVistaOrNewer() {
			hostCounterSet.timestamp, err = hostCounterSet.query.CollectDataWithTime()
			if err != nil {
				return err
			}
		} else {
			hostCounterSet.timestamp = time.Now()
			if err := hostCounterSet.query.CollectData(); err != nil {
				return err
			}
		}
	}
	var wg sync.WaitGroup
	// iterate over computers
	for _, hostCounterInfo := range m.hostCounters {
		wg.Add(1)
		go func(hostInfo *hostCountersInfo) {
			m.Log.Debugf("Gathering from %s", hostInfo.computer)
			start := time.Now()
			err := m.gatherComputerCounters(hostInfo, acc)
			m.Log.Debugf("Gathering from %s finished in %v", hostInfo.computer, time.Since(start))
			if err != nil && m.checkError(err) != nil {
				acc.AddError(fmt.Errorf("error during collecting data on host %q: %w", hostInfo.computer, err))
			}
			wg.Done()
		}(hostCounterInfo)
	}

	wg.Wait()
	return nil
}

func (m *WinPerfCounters) gatherComputerCounters(hostCounterInfo *hostCountersInfo, acc telegraf.Accumulator) error {
	var value interface{}
	var err error
	collectedFields := make(fieldGrouping)
	// For iterate over the known metrics and get the samples.
	for _, metric := range hostCounterInfo.counters {
		// collect
		if m.UseWildcardsExpansion {
			if metric.useRawValue {
				value, err = hostCounterInfo.query.GetRawCounterValue(metric.counterHandle)
			} else {
				value, err = hostCounterInfo.query.GetFormattedCounterValueDouble(metric.counterHandle)
			}
			if err != nil {
				// ignore invalid data  as some counters from process instances returns this sometimes
				if !isKnownCounterDataError(err) {
					return fmt.Errorf("error while getting value for counter %q: %w", metric.counterPath, err)
				}
				m.Log.Warnf("Error while getting value for counter %q, instance: %s, will skip metric: %v", metric.counterPath, metric.instance, err)
				continue
			}
			addCounterMeasurement(metric, metric.instance, value, collectedFields)
		} else {
			var counterValues []counterValue
			if metric.useRawValue {
				counterValues, err = hostCounterInfo.query.GetRawCounterArray(metric.counterHandle)
			} else {
				counterValues, err = hostCounterInfo.query.GetFormattedCounterArrayDouble(metric.counterHandle)
			}
			if err != nil {
				// ignore invalid data  as some counters from process instances returns this sometimes
				if !isKnownCounterDataError(err) {
					return fmt.Errorf("error while getting value for counter %q: %w", metric.counterPath, err)
				}
				m.Log.Warnf("Error while getting value for counter %q, instance: %s, will skip metric: %v", metric.counterPath, metric.instance, err)
				continue
			}
			for _, cValue := range counterValues {
				if strings.Contains(metric.instance, "#") && strings.HasPrefix(metric.instance, cValue.InstanceName) {
					// If you are using a multiple instance identifier such as "w3wp#1"
					// phd.dll returns only the first 2 characters of the identifier.
					cValue.InstanceName = metric.instance
				}

				if shouldIncludeMetric(metric, cValue) {
					addCounterMeasurement(metric, cValue.InstanceName, cValue.Value, collectedFields)
				}
			}
		}
	}
	for instance, fields := range collectedFields {
		var tags = map[string]string{
			"objectname": instance.objectName,
		}
		if len(instance.instance) > 0 {
			tags["instance"] = instance.instance
		}
		if len(hostCounterInfo.tag) > 0 {
			tags["source"] = hostCounterInfo.tag
		}
		acc.AddFields(instance.name, fields, tags, hostCounterInfo.timestamp)
	}
	return nil
}

func (m *WinPerfCounters) cleanQueries() error {
	for _, hostCounterInfo := range m.hostCounters {
		if err := hostCounterInfo.query.Close(); err != nil {
			return err
		}
	}
	m.hostCounters = nil
	return nil
}

func shouldIncludeMetric(metric *counter, cValue counterValue) bool {
	if metric.includeTotal {
		// If IncludeTotal is set, include all.
		return true
	}
	if metric.instance == "*" && !strings.Contains(cValue.InstanceName, "_Total") {
		// Catch if set to * and that it is not a '*_Total*' instance.
		return true
	}
	if metric.instance == cValue.InstanceName {
		// Catch if we set it to total or some form of it
		return true
	}
	if metric.instance == emptyInstance {
		return true
	}
	return false
}

func addCounterMeasurement(metric *counter, instanceName string, value interface{}, collectFields fieldGrouping) {
	var instance = instanceGrouping{metric.measurement, instanceName, metric.objectName}
	if collectFields[instance] == nil {
		collectFields[instance] = make(map[string]interface{})
	}
	collectFields[instance][sanitizedChars.Replace(metric.counter)] = value
}

func isKnownCounterDataError(err error) bool {
	var pdhErr *pdhError
	if errors.As(err, &pdhErr) && (pdhErr.ErrorCode == PdhInvalidData ||
		pdhErr.ErrorCode == PdhCalcNegativeDenominator ||
		pdhErr.ErrorCode == PdhCalcNegativeValue ||
		pdhErr.ErrorCode == PdhCstatusInvalidData ||
		pdhErr.ErrorCode == PdhCstatusNoInstance ||
		pdhErr.ErrorCode == PdhNoData) {
		return true
	}
	return false
}

func (m *WinPerfCounters) Init() error {
	// Check the buffer size
	if m.MaxBufferSize < config.Size(initialBufferSize) {
		return fmt.Errorf("maximum buffer size should at least be %d", 2*initialBufferSize)
	}
	if m.MaxBufferSize > math.MaxUint32 {
		return fmt.Errorf("maximum buffer size should be smaller than %d", uint32(math.MaxUint32))
	}

	if m.UseWildcardsExpansion && !m.LocalizeWildcardsExpansion {
		// Counters must not have wildcards with this option
		found := false
		wildcards := []string{"*", "?"}

		for _, object := range m.Object {
			for _, wildcard := range wildcards {
				if strings.Contains(object.ObjectName, wildcard) {
					found = true
					m.Log.Errorf("Object: %s, contains wildcard %s", object.ObjectName, wildcard)
				}
			}
			for _, counter := range object.Counters {
				for _, wildcard := range wildcards {
					if strings.Contains(counter, wildcard) {
						found = true
						m.Log.Errorf("Object: %s, counter: %s contains wildcard %s", object.ObjectName, counter, wildcard)
					}
				}
			}
		}

		if found {
			return errors.New("wildcards can't be used with LocalizeWildcardsExpansion=false")
		}
	}
	return nil
}

func init() {
	inputs.Add("win_perf_counters", func() telegraf.Input {
		return &WinPerfCounters{
			CountersRefreshInterval:    config.Duration(time.Second * 60),
			LocalizeWildcardsExpansion: true,
			MaxBufferSize:              defaultMaxBufferSize,
			queryCreator:               &performanceQueryCreatorImpl{},
		}
	})
}
