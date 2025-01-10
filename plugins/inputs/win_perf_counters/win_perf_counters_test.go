//go:build windows

package win_perf_counters

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

type testCounter struct {
	handle pdhCounterHandle
	path   string
	value  float64
	status uint32 // allows for tests against specific pdh_error codes, rather than assuming all cases of "value == 0" to indicate error conditions
}

type FakePerformanceQuery struct {
	counters      map[string]testCounter
	vistaAndNewer bool
	expandPaths   map[string][]string
	openCalled    bool
}

var MetricTime = time.Date(2018, 5, 28, 12, 0, 0, 0, time.UTC)

func (m *testCounter) ToCounterValue(raw bool) *counterValue {
	//nolint:dogsled,errcheck // only instance is needed for this helper function in tests
	_, _, inst, _, _ := extractCounterInfoFromCounterPath(m.path)
	if inst == "" {
		inst = "--"
	}
	var val interface{}
	if raw {
		val = int64(m.value)
	} else {
		val = m.value
	}

	return &counterValue{inst, val}
}

func (m *FakePerformanceQuery) Open() error {
	if m.openCalled {
		err := m.Close()
		if err != nil {
			return err
		}
	}
	m.openCalled = true
	return nil
}

func (m *FakePerformanceQuery) Close() error {
	if !m.openCalled {
		return errors.New("in Close: uninitialized query")
	}
	m.openCalled = false
	return nil
}

func (m *FakePerformanceQuery) AddCounterToQuery(counterPath string) (pdhCounterHandle, error) {
	if !m.openCalled {
		return 0, errors.New("in AddCounterToQuery: uninitialized query")
	}
	if c, ok := m.counters[counterPath]; ok {
		return c.handle, nil
	}
	return 0, fmt.Errorf("in AddCounterToQuery: invalid counter path: %q", counterPath)
}

func (m *FakePerformanceQuery) AddEnglishCounterToQuery(counterPath string) (pdhCounterHandle, error) {
	if !m.openCalled {
		return 0, errors.New("in AddEnglishCounterToQuery: uninitialized query")
	}
	if c, ok := m.counters[counterPath]; ok {
		return c.handle, nil
	}
	return 0, fmt.Errorf("in AddEnglishCounterToQuery: invalid counter path: %q", counterPath)
}

func (m *FakePerformanceQuery) GetCounterPath(counterHandle pdhCounterHandle) (string, error) {
	for _, counter := range m.counters {
		if counter.handle == counterHandle {
			return counter.path, nil
		}
	}
	return "", fmt.Errorf("in GetCounterPath: invalid handle: %q", counterHandle)
}

func (m *FakePerformanceQuery) ExpandWildCardPath(counterPath string) ([]string, error) {
	if e, ok := m.expandPaths[counterPath]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("in ExpandWildCardPath: invalid counter path: %q", counterPath)
}

func (m *FakePerformanceQuery) GetFormattedCounterValueDouble(counterHandle pdhCounterHandle) (float64, error) {
	if !m.openCalled {
		return 0, errors.New("in GetFormattedCounterValueDouble: uninitialized query")
	}
	for _, counter := range m.counters {
		if counter.handle == counterHandle {
			if counter.status > 0 {
				return 0, NewPdhError(counter.status)
			}
			return counter.value, nil
		}
	}
	return 0, fmt.Errorf("in GetFormattedCounterValueDouble: invalid handle: %q", counterHandle)
}

func (m *FakePerformanceQuery) GetRawCounterValue(counterHandle pdhCounterHandle) (int64, error) {
	if !m.openCalled {
		return 0, errors.New("in GetRawCounterValue: uninitialised query")
	}
	for _, counter := range m.counters {
		if counter.handle == counterHandle {
			if counter.status > 0 {
				return 0, NewPdhError(counter.status)
			}
			return int64(counter.value), nil
		}
	}
	return 0, fmt.Errorf("in GetRawCounterValue: invalid handle: %q", counterHandle)
}

func (m *FakePerformanceQuery) findCounterByPath(counterPath string) *testCounter {
	for _, c := range m.counters {
		if c.path == counterPath {
			return &c
		}
	}
	return nil
}

func (m *FakePerformanceQuery) GetFormattedCounterArrayDouble(hCounter pdhCounterHandle) ([]counterValue, error) {
	if !m.openCalled {
		return nil, errors.New("in GetFormattedCounterArrayDouble: uninitialized query")
	}
	for _, c := range m.counters {
		if c.handle == hCounter {
			if e, ok := m.expandPaths[c.path]; ok {
				counters := make([]counterValue, 0, len(e))
				for _, p := range e {
					counter := m.findCounterByPath(p)
					if counter == nil {
						return nil, fmt.Errorf("in GetFormattedCounterArrayDouble: invalid counter: %q", p)
					}
					if counter.status > 0 {
						return nil, NewPdhError(counter.status)
					}
					counters = append(counters, *counter.ToCounterValue(false))
				}
				return counters, nil
			}
			return nil, fmt.Errorf("in GetFormattedCounterArrayDouble: invalid counter: %q", hCounter)
		}
	}
	return nil, fmt.Errorf("in GetFormattedCounterArrayDouble: invalid counter: %q, no paths found", hCounter)
}

func (m *FakePerformanceQuery) GetRawCounterArray(hCounter pdhCounterHandle) ([]counterValue, error) {
	if !m.openCalled {
		return nil, errors.New("in GetRawCounterArray: uninitialised query")
	}
	for _, c := range m.counters {
		if c.handle == hCounter {
			if e, ok := m.expandPaths[c.path]; ok {
				counters := make([]counterValue, 0, len(e))
				for _, p := range e {
					counter := m.findCounterByPath(p)
					if counter == nil {
						return nil, fmt.Errorf("in GetRawCounterArray: invalid counter: %q", p)
					}
					if counter.status > 0 {
						return nil, NewPdhError(counter.status)
					}
					counters = append(counters, *counter.ToCounterValue(true))
				}
				return counters, nil
			}
			return nil, fmt.Errorf("in GetRawCounterArray: invalid counter: %q", hCounter)
		}
	}
	return nil, fmt.Errorf("in GetRawCounterArray: invalid counter: %q, no paths found", hCounter)
}

func (m *FakePerformanceQuery) CollectData() error {
	if !m.openCalled {
		return errors.New("in CollectData: uninitialized query")
	}
	return nil
}

func (m *FakePerformanceQuery) CollectDataWithTime() (time.Time, error) {
	if !m.openCalled {
		return time.Now(), errors.New("in CollectDataWithTime: uninitialized query")
	}
	return MetricTime, nil
}

func (m *FakePerformanceQuery) IsVistaOrNewer() bool {
	return m.vistaAndNewer
}

type FakePerformanceQueryCreator struct {
	fakeQueries map[string]*FakePerformanceQuery
}

func (m FakePerformanceQueryCreator) NewPerformanceQuery(computer string, _ uint32) PerformanceQuery {
	var ret PerformanceQuery
	var ok bool
	if ret, ok = m.fakeQueries[computer]; !ok {
		panic(fmt.Errorf("query for %q not found", computer))
	}
	return ret
}

//nolint:revive //argument-limit allowed for helper function
func createPerfObject(
	computer string,
	measurement string,
	object string,
	instances []string,
	counters []string,
	failOnMissing bool,
	includeTotal bool,
	useRawValues bool,
) []perfObject {
	perfObj := perfObject{
		ObjectName:    object,
		Instances:     instances,
		Counters:      counters,
		Measurement:   measurement,
		WarnOnMissing: false,
		FailOnMissing: failOnMissing,
		IncludeTotal:  includeTotal,
		UseRawValues:  useRawValues,
	}

	if computer != "" {
		perfObj.Sources = []string{computer}
	}
	return []perfObject{perfObj}
}

func createCounterMap(counterPaths []string, values []float64, status []uint32) map[string]testCounter {
	counters := make(map[string]testCounter)
	for i, cp := range counterPaths {
		counters[cp] = testCounter{
			pdhCounterHandle(i),
			cp,
			values[i],
			status[i],
		}
	}
	return counters
}

var cachedHostname string

func hostname() string {
	if cachedHostname == "" {
		var err error
		if cachedHostname, err = os.Hostname(); err != nil {
			cachedHostname = "localhost"
		}
	}
	return cachedHostname
}

var counterPathsAndRes = map[string][]string{
	"\\O\\CT":                           {"", "O", "", "CT"},
	"\\O\\CT(i)":                        {"", "O", "", "CT(i)"},
	"\\O\\CT(d:\\f\\i)":                 {"", "O", "", "CT(d:\\f\\i)"},
	"\\\\CM\\O\\CT":                     {"CM", "O", "", "CT"},
	"\\O(I)\\CT":                        {"", "O", "I", "CT"},
	"\\O(I)\\CT(i)":                     {"", "O", "I", "CT(i)"},
	"\\O(I)\\CT(i)x":                    {"", "O", "I", "CT(i)x"},
	"\\O(I)\\CT(d:\\f\\i)":              {"", "O", "I", "CT(d:\\f\\i)"},
	"\\\\CM\\O(I)\\CT":                  {"CM", "O", "I", "CT"},
	"\\O(d:\\f\\I)\\CT":                 {"", "O", "d:\\f\\I", "CT"},
	"\\O(d:\\f\\I(d))\\CT":              {"", "O", "d:\\f\\I(d)", "CT"},
	"\\O(d:\\f\\I(d)x)\\CT":             {"", "O", "d:\\f\\I(d)x", "CT"},
	"\\O(d:\\f\\I)\\CT(i)":              {"", "O", "d:\\f\\I", "CT(i)"},
	"\\O(d:\\f\\I)\\CT(d:\\f\\i)":       {"", "O", "d:\\f\\I", "CT(d:\\f\\i)"},
	"\\\\CM\\O(d:\\f\\I)\\CT":           {"CM", "O", "d:\\f\\I", "CT"},
	"\\\\CM\\O(d:\\f\\I)\\CT(d:\\f\\i)": {"CM", "O", "d:\\f\\I", "CT(d:\\f\\i)"},
	"\\O(I(info))\\CT":                  {"", "O", "I(info)", "CT"},
	"\\\\CM\\O(I(info))\\CT":            {"CM", "O", "I(info)", "CT"},
}

var invalidCounterPaths = []string{
	"\\O(I\\C",
	"\\OI)\\C",
	"\\O(I\\C",
	"\\O/C",
	"\\O(I/C",
	"\\O(I/C)",
	"\\O(I\\)C",
	"\\O(I\\C)",
	"\\CM\\O(I)\\C",
	"\\CM\\O\\C",
	"\\\\C\\O(I)\\C)",
	"\\\\C\\O\\C)",
}

func TestCounterPathParsing(t *testing.T) {
	for path, vals := range counterPathsAndRes {
		h, o, i, c, err := extractCounterInfoFromCounterPath(path)
		require.NoError(t, err)
		require.Equalf(t, []string{h, o, i, c}, vals, "arrays: %#v and %#v are not equal", vals, []string{o, i, c})
	}
	for _, path := range invalidCounterPaths {
		_, _, _, _, err := extractCounterInfoFromCounterPath(path)
		require.Error(t, err)
	}
}

func TestAddItemSimple(t *testing.T) {
	var err error
	cps1 := []string{"\\O(I)\\C"}
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     nil,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.1}, []uint32{0}),
				expandPaths: map[string][]string{
					cps1[0]: cps1,
				},
				vistaAndNewer: true,
			},
			},
		},
	}
	err = m.AddItem(cps1[0], "localhost", "O", "I", "c", "test", false, false)
	require.NoError(t, err)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 1)
	require.Equal(t, "localhost", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[0].objectName)
	require.Equal(t, "I", counters.counters[0].instance)
	require.Equal(t, "c", counters.counters[0].counter)
	require.Equal(t, "test", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
}

func TestAddItemInvalidCountPath(t *testing.T) {
	var err error
	cps1 := []string{"\\O\\C"}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		Object:                nil,
		UseWildcardsExpansion: true,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.1}, []uint32{0}),
				expandPaths: map[string][]string{
					cps1[0]: {"\\O/C"},
				},
				vistaAndNewer: true,
			},
			},
		},
	}
	require.NoError(t, err)
	err = m.AddItem("\\O\\C", "localhost", "O", "------", "C", "test", false, false)
	require.Error(t, err)
}

func TestParseConfigBasic(t *testing.T) {
	var err error
	perfObjects := createPerfObject("", "m", "O", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	m := WinPerfCounters{
		Sources:    []string{"localhost"},
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.1, 1.2, 1.3, 1.4}, []uint32{0, 0, 0, 0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
					cps1[1]: {cps1[1]},
					cps1[2]: {cps1[2]},
					cps1[3]: {cps1[3]},
				},
				vistaAndNewer: true,
			},
			},
		},
	}
	require.NoError(t, err)
	err = m.ParseConfig()
	require.NoError(t, err)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	err = m.cleanQueries()
	require.NoError(t, err)
	m.UseWildcardsExpansion = true

	err = m.ParseConfig()
	require.NoError(t, err)
	counters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
}

func TestParseConfigMultiComps(t *testing.T) {
	var err error
	perfObjects := []perfObject{
		createPerfObject("", "m", "O", []string{"I"}, []string{"C"}, false, false, false)[0],
		createPerfObject("", "m", "O1", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)[0],
		createPerfObject("", "m", "O2", []string{"I"}, []string{"C1", "C2", "C3"}, false, false, false)[0],
	}
	cps11 := []string{"\\O(I)\\C"}
	cps12 := []string{"\\\\cmp1\\O(I)\\C"}
	cps13 := []string{"\\\\cmp2\\O(I)\\C"}
	cps21 := []string{"\\O1(I1)\\C1", "\\O1(I1)\\C2", "\\O1(I2)\\C1", "\\O1(I2)\\C2"}
	cps22 := []string{"\\\\cmp1\\O1(I1)\\C1", "\\\\cmp1\\O1(I1)\\C2", "\\\\cmp1\\O1(I2)\\C1", "\\\\cmp1\\O1(I2)\\C2"}
	cps23 := []string{"\\\\cmp2\\O1(I1)\\C1", "\\\\cmp2\\O1(I1)\\C2", "\\\\cmp2\\O1(I2)\\C1", "\\\\cmp2\\O1(I2)\\C2"}
	cps31 := []string{"\\O2(I)\\C1", "\\O2(I)\\C2", "\\O2(I)\\C3"}
	cps32 := []string{"\\\\cmp1\\O2(I)\\C1", "\\\\cmp1\\O2(I)\\C2", "\\\\cmp1\\O2(I)\\C3"}
	cps33 := []string{"\\\\cmp2\\O2(I)\\C1", "\\\\cmp2\\O2(I)\\C2", "\\\\cmp2\\O2(I)\\C3"}
	m := WinPerfCounters{
		Sources:    []string{"localhost", "cmp1", "cmp2"},
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{
				"localhost": {
					counters: createCounterMap(append(append(cps11, cps21...), cps31...),
						[]float64{1.1, 1.1, 1.2, 2.1, 2.2, 1.1, 1.2, 1.3},
						[]uint32{0, 0, 0, 0, 0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps11[0]: {cps11[0]},
						cps21[0]: {cps21[0]},
						cps21[1]: {cps21[1]},
						cps21[2]: {cps21[2]},
						cps21[3]: {cps21[3]},
						cps31[0]: {cps31[0]},
						cps31[1]: {cps31[1]},
						cps31[2]: {cps31[2]},
					},
					vistaAndNewer: true,
				},
				"cmp1": {
					counters: createCounterMap(append(append(cps12, cps22...), cps32...),
						[]float64{1.1, 1.1, 1.2, 2.1, 2.2, 1.1, 1.2, 1.3},
						[]uint32{0, 0, 0, 0, 0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps12[0]: {cps12[0]},
						cps22[0]: {cps22[0]},
						cps22[1]: {cps22[1]},
						cps22[2]: {cps22[2]},
						cps22[3]: {cps22[3]},
						cps32[0]: {cps32[0]},
						cps32[1]: {cps32[1]},
						cps32[2]: {cps32[2]},
					},
					vistaAndNewer: true,
				},
				"cmp2": {
					counters: createCounterMap(append(append(cps13, cps23...), cps33...),
						[]float64{1.1, 1.1, 1.2, 2.1, 2.2, 1.1, 1.2, 1.3},
						[]uint32{0, 0, 0, 0, 0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps13[0]: {cps13[0]},
						cps23[0]: {cps23[0]},
						cps23[1]: {cps23[1]},
						cps23[2]: {cps23[2]},
						cps23[3]: {cps23[3]},
						cps33[0]: {cps33[0]},
						cps33[1]: {cps33[1]},
						cps33[2]: {cps33[2]},
					},
					vistaAndNewer: true,
				},
			},
		},
	}
	require.NoError(t, err)
	err = m.ParseConfig()
	require.NoError(t, err)
	require.Len(t, m.hostCounters, 3)

	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 8)
	require.Equal(t, counters.tag, hostname())
	require.Equal(t, "localhost", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[0].objectName)
	require.Equal(t, "I", counters.counters[0].instance)
	require.Equal(t, "C", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "localhost", counters.counters[1].computer)
	require.Equal(t, "O1", counters.counters[1].objectName)
	require.Equal(t, "I1", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "localhost", counters.counters[2].computer)
	require.Equal(t, "O1", counters.counters[2].objectName)
	require.Equal(t, "I2", counters.counters[2].instance)
	require.Equal(t, "C1", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "localhost", counters.counters[3].computer)
	require.Equal(t, "O1", counters.counters[3].objectName)
	require.Equal(t, "I1", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)
	require.Equal(t, "localhost", counters.counters[4].computer)
	require.Equal(t, "O1", counters.counters[4].objectName)
	require.Equal(t, "I2", counters.counters[4].instance)
	require.Equal(t, "C2", counters.counters[4].counter)
	require.Equal(t, "m", counters.counters[4].measurement)
	require.False(t, counters.counters[4].includeTotal)
	require.Equal(t, "localhost", counters.counters[5].computer)
	require.Equal(t, "O2", counters.counters[5].objectName)
	require.Equal(t, "I", counters.counters[5].instance)
	require.Equal(t, "C1", counters.counters[5].counter)
	require.Equal(t, "m", counters.counters[5].measurement)
	require.False(t, counters.counters[5].includeTotal)
	require.Equal(t, "localhost", counters.counters[6].computer)
	require.Equal(t, "O2", counters.counters[6].objectName)
	require.Equal(t, "I", counters.counters[6].instance)
	require.Equal(t, "C2", counters.counters[6].counter)
	require.Equal(t, "m", counters.counters[6].measurement)
	require.False(t, counters.counters[6].includeTotal)
	require.Equal(t, "localhost", counters.counters[7].computer)
	require.Equal(t, "O2", counters.counters[7].objectName)
	require.Equal(t, "I", counters.counters[7].instance)
	require.Equal(t, "C3", counters.counters[7].counter)
	require.Equal(t, "m", counters.counters[7].measurement)
	require.False(t, counters.counters[7].includeTotal)

	counters, ok = m.hostCounters["cmp1"]
	require.True(t, ok)
	require.Len(t, counters.counters, 8)
	require.Equal(t, "cmp1", counters.tag)
	require.Equal(t, "cmp1", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[0].objectName)
	require.Equal(t, "I", counters.counters[0].instance)
	require.Equal(t, "C", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "cmp1", counters.counters[1].computer)
	require.Equal(t, "O1", counters.counters[1].objectName)
	require.Equal(t, "I1", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "cmp1", counters.counters[2].computer)
	require.Equal(t, "O1", counters.counters[2].objectName)
	require.Equal(t, "I2", counters.counters[2].instance)
	require.Equal(t, "C1", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "cmp1", counters.counters[3].computer)
	require.Equal(t, "O1", counters.counters[3].objectName)
	require.Equal(t, "I1", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)
	require.Equal(t, "cmp1", counters.counters[4].computer)
	require.Equal(t, "O1", counters.counters[4].objectName)
	require.Equal(t, "I2", counters.counters[4].instance)
	require.Equal(t, "C2", counters.counters[4].counter)
	require.Equal(t, "m", counters.counters[4].measurement)
	require.False(t, counters.counters[4].includeTotal)
	require.Equal(t, "cmp1", counters.counters[5].computer)
	require.Equal(t, "O2", counters.counters[5].objectName)
	require.Equal(t, "I", counters.counters[5].instance)
	require.Equal(t, "C1", counters.counters[5].counter)
	require.Equal(t, "m", counters.counters[5].measurement)
	require.False(t, counters.counters[5].includeTotal)
	require.Equal(t, "cmp1", counters.counters[6].computer)
	require.Equal(t, "O2", counters.counters[6].objectName)
	require.Equal(t, "I", counters.counters[6].instance)
	require.Equal(t, "C2", counters.counters[6].counter)
	require.Equal(t, "m", counters.counters[6].measurement)
	require.False(t, counters.counters[6].includeTotal)
	require.Equal(t, "cmp1", counters.counters[7].computer)
	require.Equal(t, "O2", counters.counters[7].objectName)
	require.Equal(t, "I", counters.counters[7].instance)
	require.Equal(t, "C3", counters.counters[7].counter)
	require.Equal(t, "m", counters.counters[7].measurement)
	require.False(t, counters.counters[7].includeTotal)

	counters, ok = m.hostCounters["cmp2"]
	require.True(t, ok)
	require.Len(t, counters.counters, 8)
	require.Equal(t, "cmp2", counters.tag)
	require.Equal(t, "cmp2", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[0].objectName)
	require.Equal(t, "I", counters.counters[0].instance)
	require.Equal(t, "C", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "cmp2", counters.counters[1].computer)
	require.Equal(t, "O1", counters.counters[1].objectName)
	require.Equal(t, "I1", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "cmp2", counters.counters[2].computer)
	require.Equal(t, "O1", counters.counters[2].objectName)
	require.Equal(t, "I2", counters.counters[2].instance)
	require.Equal(t, "C1", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "cmp2", counters.counters[3].computer)
	require.Equal(t, "O1", counters.counters[3].objectName)
	require.Equal(t, "I1", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)
	require.Equal(t, "cmp2", counters.counters[4].computer)
	require.Equal(t, "O1", counters.counters[4].objectName)
	require.Equal(t, "I2", counters.counters[4].instance)
	require.Equal(t, "C2", counters.counters[4].counter)
	require.Equal(t, "m", counters.counters[4].measurement)
	require.False(t, counters.counters[4].includeTotal)
	require.Equal(t, "cmp2", counters.counters[5].computer)
	require.Equal(t, "O2", counters.counters[5].objectName)
	require.Equal(t, "I", counters.counters[5].instance)
	require.Equal(t, "C1", counters.counters[5].counter)
	require.Equal(t, "m", counters.counters[5].measurement)
	require.False(t, counters.counters[5].includeTotal)
	require.Equal(t, "cmp2", counters.counters[6].computer)
	require.Equal(t, "O2", counters.counters[6].objectName)
	require.Equal(t, "I", counters.counters[6].instance)
	require.Equal(t, "C2", counters.counters[6].counter)
	require.Equal(t, "m", counters.counters[6].measurement)
	require.False(t, counters.counters[6].includeTotal)
	require.Equal(t, "cmp2", counters.counters[7].computer)
	require.Equal(t, "O2", counters.counters[7].objectName)
	require.Equal(t, "I", counters.counters[7].instance)
	require.Equal(t, "C3", counters.counters[7].counter)
	require.Equal(t, "m", counters.counters[7].measurement)
	require.False(t, counters.counters[7].includeTotal)
}

func TestParseConfigMultiCompsOverrideMultiplePerfObjects(t *testing.T) {
	var err error
	perfObjects := []perfObject{
		createPerfObject("localhost", "m", "O", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)[0],
		createPerfObject("cmp1", "m", "O1", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)[0],
		createPerfObject("cmp2", "m", "O2", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)[0],
	}
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	cps2 := []string{"\\\\cmp1\\O1(I1)\\C1", "\\\\cmp1\\O1(I1)\\C2", "\\\\cmp1\\O1(I2)\\C1", "\\\\cmp1\\O1(I2)\\C2"}
	cps3 := []string{"\\\\cmp2\\O2(I1)\\C1", "\\\\cmp2\\O2(I1)\\C2", "\\\\cmp2\\O2(I2)\\C1", "\\\\cmp2\\O2(I2)\\C2"}
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1,
					[]float64{1.1, 1.2, 1.3, 1.4},
					[]uint32{0, 0, 0, 0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
					cps1[1]: {cps1[1]},
					cps1[2]: {cps1[2]},
					cps1[3]: {cps1[3]},
				},
				vistaAndNewer: true,
			},
				"cmp1": {
					counters: createCounterMap(cps2,
						[]float64{2.1, 2.2, 2.3, 2.4},
						[]uint32{0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps2[0]: {cps2[0]},
						cps2[1]: {cps2[1]},
						cps2[2]: {cps2[2]},
						cps2[3]: {cps2[3]},
					},
					vistaAndNewer: true,
				},
				"cmp2": {
					counters: createCounterMap(cps3,
						[]float64{3.1, 3.2, 3.3, 3.4},
						[]uint32{0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps3[0]: {cps3[0]},
						cps3[1]: {cps3[1]},
						cps3[2]: {cps3[2]},
						cps3[3]: {cps3[3]},
					},
					vistaAndNewer: true,
				},
			},
		},
	}
	require.NoError(t, err)
	err = m.ParseConfig()
	require.NoError(t, err)
	require.Len(t, m.hostCounters, 3)

	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	require.Equal(t, "localhost", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[0].objectName)
	require.Equal(t, "I1", counters.counters[0].instance)
	require.Equal(t, "C1", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "localhost", counters.counters[1].computer)
	require.Equal(t, "O", counters.counters[1].objectName)
	require.Equal(t, "I2", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "localhost", counters.counters[2].computer)
	require.Equal(t, "O", counters.counters[2].objectName)
	require.Equal(t, "I1", counters.counters[2].instance)
	require.Equal(t, "C2", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "localhost", counters.counters[3].computer)
	require.Equal(t, "O", counters.counters[3].objectName)
	require.Equal(t, "I2", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)

	counters, ok = m.hostCounters["cmp1"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	require.Equal(t, "cmp1", counters.counters[0].computer)
	require.Equal(t, "O1", counters.counters[0].objectName)
	require.Equal(t, "I1", counters.counters[0].instance)
	require.Equal(t, "C1", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "cmp1", counters.counters[1].computer)
	require.Equal(t, "O1", counters.counters[1].objectName)
	require.Equal(t, "I2", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "cmp1", counters.counters[2].computer)
	require.Equal(t, "O1", counters.counters[2].objectName)
	require.Equal(t, "I1", counters.counters[2].instance)
	require.Equal(t, "C2", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "cmp1", counters.counters[3].computer)
	require.Equal(t, "O1", counters.counters[3].objectName)
	require.Equal(t, "I2", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)

	counters, ok = m.hostCounters["cmp2"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	require.Equal(t, "cmp2", counters.counters[0].computer)
	require.Equal(t, "O2", counters.counters[0].objectName)
	require.Equal(t, "I1", counters.counters[0].instance)
	require.Equal(t, "C1", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "cmp2", counters.counters[1].computer)
	require.Equal(t, "O2", counters.counters[1].objectName)
	require.Equal(t, "I2", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "cmp2", counters.counters[2].computer)
	require.Equal(t, "O2", counters.counters[2].objectName)
	require.Equal(t, "I1", counters.counters[2].instance)
	require.Equal(t, "C2", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "cmp2", counters.counters[3].computer)
	require.Equal(t, "O2", counters.counters[3].objectName)
	require.Equal(t, "I2", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)
}

func TestParseConfigMultiCompsOverrideOnePerfObject(t *testing.T) {
	var err error

	perfObj := perfObject{
		Sources:       []string{"cmp1", "cmp2"},
		ObjectName:    "O",
		Instances:     []string{"I1", "I2"},
		Counters:      []string{"C1", "C2"},
		Measurement:   "m",
		WarnOnMissing: false,
		FailOnMissing: false,
		IncludeTotal:  false,
	}
	cps11 := []string{"\\\\cmp1\\O(I1)\\C1", "\\\\cmp1\\O(I1)\\C2", "\\\\cmp1\\O(I2)\\C1", "\\\\cmp1\\O(I2)\\C2"}
	cps12 := []string{"\\\\cmp2\\O(I1)\\C1", "\\\\cmp2\\O(I1)\\C2", "\\\\cmp2\\O(I2)\\C1", "\\\\cmp2\\O(I2)\\C2"}
	cps21 := []string{"\\O1(I)\\C"}
	cps22 := []string{"\\\\cmp1\\O1(I)\\C"}
	m := WinPerfCounters{
		Sources:    []string{"localhost", "cmp1"},
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     []perfObject{perfObj, createPerfObject("", "m", "O1", []string{"I"}, []string{"C"}, false, false, false)[0]},
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{
				"localhost": {
					counters: createCounterMap(cps21,
						[]float64{1.1},
						[]uint32{0}),
					expandPaths: map[string][]string{
						cps21[0]: {cps21[0]},
					},
					vistaAndNewer: true,
				},
				"cmp1": {
					counters: createCounterMap(append(cps11, cps22...),
						[]float64{2.1, 2.1, 2.2, 2.3, 2.4},
						[]uint32{0, 0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps11[0]: {cps11[0]},
						cps11[1]: {cps11[1]},
						cps11[2]: {cps11[2]},
						cps11[3]: {cps11[3]},
						cps22[0]: {cps22[0]},
					},
					vistaAndNewer: true,
				},
				"cmp2": {
					counters: createCounterMap(cps12,
						[]float64{3.1, 3.2, 3.3, 3.4},
						[]uint32{0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps12[0]: {cps12[0]},
						cps12[1]: {cps12[1]},
						cps12[2]: {cps12[2]},
						cps12[3]: {cps12[3]},
					},
					vistaAndNewer: true,
				},
			},
		},
	}
	require.NoError(t, err)
	err = m.ParseConfig()
	require.NoError(t, err)
	require.Len(t, m.hostCounters, 3)

	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 1)
	require.Equal(t, counters.tag, hostname())
	require.Equal(t, "localhost", counters.counters[0].computer)
	require.Equal(t, "O1", counters.counters[0].objectName)
	require.Equal(t, "I", counters.counters[0].instance)
	require.Equal(t, "C", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)

	counters, ok = m.hostCounters["cmp1"]
	require.True(t, ok)
	require.Len(t, counters.counters, 5)
	require.Equal(t, "cmp1", counters.tag)
	require.Equal(t, "cmp1", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[0].objectName)
	require.Equal(t, "I1", counters.counters[0].instance)
	require.Equal(t, "C1", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "cmp1", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[1].objectName)
	require.Equal(t, "I2", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "cmp1", counters.counters[2].computer)
	require.Equal(t, "O", counters.counters[2].objectName)
	require.Equal(t, "I1", counters.counters[2].instance)
	require.Equal(t, "C2", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "cmp1", counters.counters[3].computer)
	require.Equal(t, "O", counters.counters[3].objectName)
	require.Equal(t, "I2", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)
	require.Equal(t, "cmp1", counters.counters[4].computer)
	require.Equal(t, "O1", counters.counters[4].objectName)
	require.Equal(t, "I", counters.counters[4].instance)
	require.Equal(t, "C", counters.counters[4].counter)
	require.Equal(t, "m", counters.counters[4].measurement)
	require.False(t, counters.counters[4].includeTotal)

	counters, ok = m.hostCounters["cmp2"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	require.Equal(t, "cmp2", counters.tag)
	require.Equal(t, "cmp2", counters.counters[0].computer)
	require.Equal(t, "O", counters.counters[0].objectName)
	require.Equal(t, "I1", counters.counters[0].instance)
	require.Equal(t, "C1", counters.counters[0].counter)
	require.Equal(t, "m", counters.counters[0].measurement)
	require.False(t, counters.counters[0].includeTotal)
	require.Equal(t, "cmp2", counters.counters[1].computer)
	require.Equal(t, "O", counters.counters[1].objectName)
	require.Equal(t, "I2", counters.counters[1].instance)
	require.Equal(t, "C1", counters.counters[1].counter)
	require.Equal(t, "m", counters.counters[1].measurement)
	require.False(t, counters.counters[1].includeTotal)
	require.Equal(t, "cmp2", counters.counters[2].computer)
	require.Equal(t, "O", counters.counters[2].objectName)
	require.Equal(t, "I1", counters.counters[2].instance)
	require.Equal(t, "C2", counters.counters[2].counter)
	require.Equal(t, "m", counters.counters[2].measurement)
	require.False(t, counters.counters[2].includeTotal)
	require.Equal(t, "cmp2", counters.counters[3].computer)
	require.Equal(t, "O", counters.counters[3].objectName)
	require.Equal(t, "I2", counters.counters[3].instance)
	require.Equal(t, "C2", counters.counters[3].counter)
	require.Equal(t, "m", counters.counters[3].measurement)
	require.False(t, counters.counters[3].includeTotal)
}

func TestParseConfigLocalhost(t *testing.T) {
	var err error
	perfObjects := createPerfObject("localhost", "m", "O", []string{"------"}, []string{"C"}, false, false, false)
	cps1 := []string{"\\O\\C"}
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.1}, []uint32{0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
				},
				vistaAndNewer: true,
			},
			},
		},
	}

	err = m.ParseConfig()
	require.NoError(t, err)

	hostCounters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, hostCounters.counters, 1)
	require.Equal(t, "localhost", hostCounters.counters[0].computer)
	require.Equal(t, "localhost", hostCounters.computer, hostCounters.computer)
	require.Equal(t, hostname(), hostCounters.tag)

	err = m.cleanQueries()
	require.NoError(t, err)

	m.Object[0].Sources = []string{""}

	err = m.ParseConfig()
	require.NoError(t, err)

	hostCounters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, hostCounters.counters, 1)
	require.Equal(t, "localhost", hostCounters.counters[0].computer)
	require.Equal(t, "localhost", hostCounters.computer, hostCounters.computer)
	require.Equal(t, hostname(), hostCounters.tag)
}

func TestParseConfigNoInstance(t *testing.T) {
	var err error
	perfObjects := createPerfObject("", "m", "O", []string{"------"}, []string{"C1", "C2"}, false, false, false)
	cps1 := []string{"\\O\\C1", "\\O\\C2"}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		Object:                perfObjects,
		UseWildcardsExpansion: false,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.1, 1.2}, []uint32{0, 0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
					cps1[1]: {cps1[1]},
				},
				vistaAndNewer: true,
			},
			},
		},
	}

	err = m.ParseConfig()
	require.NoError(t, err)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)

	m.UseWildcardsExpansion = true
	err = m.cleanQueries()
	require.NoError(t, err)

	err = m.ParseConfig()
	require.NoError(t, err)
	counters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)
}

func TestParseConfigInvalidCounterError(t *testing.T) {
	var err error
	perfObjects := createPerfObject("", "m", "O", []string{"I1", "I2"}, []string{"C1", "C2"}, true, false, false)
	cps1 := []string{"\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.1, 1.2, 1.3}, []uint32{0, 0, 0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
					cps1[1]: {cps1[1]},
					cps1[2]: {cps1[2]},
				},
				vistaAndNewer: true,
			},
			},
		},
	}

	err = m.ParseConfig()
	require.Error(t, err)

	err = m.cleanQueries()
	require.NoError(t, err)
	m.UseWildcardsExpansion = true

	err = m.ParseConfig()
	require.Error(t, err)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestParseConfigInvalidCounterNoError(t *testing.T) {
	var err error
	perfObjects := createPerfObject("", "m", "O", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)
	cps1 := []string{"\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.1, 1.2, 1.3}, []uint32{0, 0, 0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
					cps1[1]: {cps1[1]},
					cps1[2]: {cps1[2]},
				},
				vistaAndNewer: true,
			},
			},
		},
	}

	err = m.ParseConfig()
	require.NoError(t, err)
	err = m.cleanQueries()
	require.NoError(t, err)

	m.UseWildcardsExpansion = true

	err = m.ParseConfig()
	require.NoError(t, err)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestParseConfigTotalExpansion(t *testing.T) {
	var err error
	perfObjects := createPerfObject("", "m", "O", []string{"*"}, []string{"*"}, true, true, false)
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(_Total)\\C1", "\\O(_Total)\\C2"}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		UseWildcardsExpansion: true,
		Object:                perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(append(cps1, "\\O(*)\\*"), []float64{1.1, 1.2, 1.3, 1.4, 0}, []uint32{0, 0, 0, 0, 0}),
				expandPaths: map[string][]string{
					"\\O(*)\\*": cps1,
				},
				vistaAndNewer: true,
			},
			},
		},
		LocalizeWildcardsExpansion: true,
	}
	err = m.ParseConfig()
	require.NoError(t, err)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	err = m.cleanQueries()
	require.NoError(t, err)

	perfObjects[0].IncludeTotal = false

	m = WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		UseWildcardsExpansion: true,
		Object:                perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(append(cps1, "\\O(*)\\*"), []float64{1.1, 1.2, 1.3, 1.4, 0}, []uint32{0, 0, 0, 0, 0}),
				expandPaths: map[string][]string{
					"\\O(*)\\*": cps1,
				},
				vistaAndNewer: true,
			},
			},
		},
		LocalizeWildcardsExpansion: true,
	}

	err = m.ParseConfig()
	require.NoError(t, err)
	counters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestParseConfigExpand(t *testing.T) {
	var err error
	perfObjects := createPerfObject("", "m", "O", []string{"*"}, []string{"*"}, false, false, false)
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		UseWildcardsExpansion: true,
		Object:                perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(append(cps1, "\\O(*)\\*"), []float64{1.1, 1.2, 1.3, 1.4, 0}, []uint32{0, 0, 0, 0, 0}),
				expandPaths: map[string][]string{
					"\\O(*)\\*": cps1,
				},
				vistaAndNewer: true,
			},
			},
		},
		LocalizeWildcardsExpansion: true,
	}
	err = m.ParseConfig()
	require.NoError(t, err)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestSimpleGather(t *testing.T) {
	var err error
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	measurement := "test"
	perfObjects := createPerfObject("", measurement, "O", []string{"I"}, []string{"C"}, false, false, false)
	cp1 := "\\O(I)\\C"
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap([]string{cp1}, []float64{1.2}, []uint32{0}),
				expandPaths: map[string][]string{
					cp1: {cp1},
				},
				vistaAndNewer: false,
			},
			},
		},
	}
	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	require.NoError(t, err)

	fields1 := map[string]interface{}{
		"C": 1.2,
	}
	tags1 := map[string]string{
		"instance":   "I",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)

	m.UseWildcardsExpansion = true
	err = m.cleanQueries()
	require.NoError(t, err)
	m.lastRefreshed = time.Time{}

	var acc2 testutil.Accumulator

	err = m.Gather(&acc2)
	require.NoError(t, err)
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestSimpleGatherNoData(t *testing.T) {
	var err error
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	measurement := "test"
	perfObjects := createPerfObject("", measurement, "O", []string{"I"}, []string{"C"}, false, false, false)
	cp1 := "\\O(I)\\C"
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap([]string{cp1}, []float64{1.2}, []uint32{PdhNoData}),
				expandPaths: map[string][]string{
					cp1: {cp1},
				},
				vistaAndNewer: false,
			},
			},
		},
	}
	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	// this "PDH_NO_DATA" error should not be returned to caller, but checked, and handled
	require.NoError(t, err)

	// fields would contain if the error was ignored, and we simply added garbage
	fields1 := map[string]interface{}{
		"C": 1.2,
	}
	// tags would contain if the error was ignored, and we simply added garbage
	tags1 := map[string]string{
		"instance":   "I",
		"objectname": "O",
	}
	acc1.AssertDoesNotContainsTaggedFields(t, measurement, fields1, tags1)

	m.UseWildcardsExpansion = true
	err = m.cleanQueries()
	require.NoError(t, err)
	m.lastRefreshed = time.Time{}

	var acc2 testutil.Accumulator

	err = m.Gather(&acc2)
	require.NoError(t, err)
	acc1.AssertDoesNotContainsTaggedFields(t, measurement, fields1, tags1)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestSimpleGatherWithTimestamp(t *testing.T) {
	var err error
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	measurement := "test"
	perfObjects := createPerfObject("", measurement, "O", []string{"I"}, []string{"C"}, false, false, false)
	cp1 := "\\O(I)\\C"
	m := WinPerfCounters{
		Log:                testutil.Logger{},
		PrintValid:         false,
		UsePerfCounterTime: true,
		Object:             perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap([]string{cp1}, []float64{1.2}, []uint32{0}),
				expandPaths: map[string][]string{
					cp1: {cp1},
				},
				vistaAndNewer: true,
			},
			},
		},
	}

	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	require.NoError(t, err)

	fields1 := map[string]interface{}{
		"C": 1.2,
	}
	tags1 := map[string]string{
		"instance":   "I",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	require.True(t, acc1.HasTimestamp(measurement, MetricTime))
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestGatherError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}

	measurement := "test"
	perfObjects := createPerfObject("", measurement, "O", []string{"I"}, []string{"C"}, false, false, false)
	cp1 := "\\O(I)\\C"
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap([]string{cp1}, []float64{-2}, []uint32{PdhPlaValidationWarning}),
				expandPaths: map[string][]string{
					cp1: {cp1},
				},
				vistaAndNewer: false,
			},
			},
		},
	}

	expectedError := fmt.Sprintf("error during collecting data on host %q: error while getting value for counter %q: "+
		"The information passed is not valid.\r\n", "localhost", "\\O(I)\\C")
	var acc1 testutil.Accumulator
	require.NoError(t, m.Gather(&acc1))
	require.Len(t, acc1.Errors, 1)
	require.Equal(t, expectedError, acc1.Errors[0].Error())

	m.UseWildcardsExpansion = true
	require.NoError(t, m.cleanQueries())

	m.lastRefreshed = time.Time{}
	var acc2 testutil.Accumulator
	require.NoError(t, m.Gather(&acc2))
	require.Len(t, acc2.Errors, 1)
	require.Equal(t, expectedError, acc2.Errors[0].Error())

	require.NoError(t, m.cleanQueries())
}

func TestGatherInvalidDataIgnore(t *testing.T) {
	var err error
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	measurement := "test"
	perfObjects := createPerfObject("", measurement, "O", []string{"I"}, []string{"C1", "C2", "C3"}, false, false, false)
	cps1 := []string{"\\O(I)\\C1", "\\O(I)\\C2", "\\O(I)\\C3"}
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1, []float64{1.2, 1, 0}, []uint32{0, PdhInvalidData, 0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
					cps1[1]: {cps1[1]},
					cps1[2]: {cps1[2]},
				},
				vistaAndNewer: false,
			},
			},
		},
	}
	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	require.NoError(t, err)

	fields1 := map[string]interface{}{
		"C1": 1.2,
		"C3": float64(0),
	}
	tags1 := map[string]string{
		"instance":   "I",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)

	m.UseWildcardsExpansion = true
	err = m.cleanQueries()
	require.NoError(t, err)
	m.lastRefreshed = time.Time{}

	var acc2 testutil.Accumulator
	err = m.Gather(&acc2)
	require.NoError(t, err)
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	err = m.cleanQueries()
	require.NoError(t, err)
}

// tests with expansion
func TestGatherRefreshingWithExpansion(t *testing.T) {
	var err error
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	measurement := "test"
	perfObjects := createPerfObject("", measurement, "O", []string{"*"}, []string{"*"}, true, false, false)
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	fpm := &FakePerformanceQuery{
		counters: createCounterMap(append(cps1, "\\O(*)\\*"), []float64{1.1, 1.2, 1.3, 1.4, 0}, []uint32{0, 0, 0, 0, 0}),
		expandPaths: map[string][]string{
			"\\O(*)\\*": cps1,
		},
		vistaAndNewer: true,
	}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		Object:                perfObjects,
		UseWildcardsExpansion: true,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": fpm},
		},
		CountersRefreshInterval:    config.Duration(time.Second * 10),
		LocalizeWildcardsExpansion: true,
	}
	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	require.NoError(t, err)

	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)

	require.Len(t, acc1.Metrics, 2)

	fields1 := map[string]interface{}{
		"C1": 1.1,
		"C2": 1.2,
	}
	tags1 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)

	fields2 := map[string]interface{}{
		"C1": 1.3,
		"C2": 1.4,
	}
	tags2 := map[string]string{
		"instance":   "I2",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields2, tags2)

	cps2 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2", "\\O(I3)\\C1", "\\O(I3)\\C2"}
	fpm = &FakePerformanceQuery{
		counters: createCounterMap(append(cps2, "\\O(*)\\*"), []float64{1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 0}, []uint32{0, 0, 0, 0, 0, 0, 0}),
		expandPaths: map[string][]string{
			"\\O(*)\\*": cps2,
		},
		vistaAndNewer: true,
	}

	m.queryCreator = &FakePerformanceQueryCreator{
		fakeQueries: map[string]*FakePerformanceQuery{"localhost": fpm},
	}
	var acc2 testutil.Accumulator

	fields3 := map[string]interface{}{
		"C1": 1.5,
		"C2": 1.6,
	}
	tags3 := map[string]string{
		"instance":   "I3",
		"objectname": "O",
		"source":     hostname(),
	}

	// test before elapsing CounterRefreshRate counters are not refreshed
	err = m.Gather(&acc2)
	require.NoError(t, err)
	counters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4)
	require.Len(t, acc2.Metrics, 2)

	acc2.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	acc2.AssertContainsTaggedFields(t, measurement, fields2, tags2)
	acc2.AssertDoesNotContainsTaggedFields(t, measurement, fields3, tags3)
	time.Sleep(time.Duration(m.CountersRefreshInterval))

	var acc3 testutil.Accumulator
	err = m.Gather(&acc3)
	require.NoError(t, err)
	require.Len(t, acc3.Metrics, 3)

	acc3.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	acc3.AssertContainsTaggedFields(t, measurement, fields2, tags2)

	acc3.AssertContainsTaggedFields(t, measurement, fields3, tags3)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestGatherRefreshingWithoutExpansion(t *testing.T) {
	var err error
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	measurement := "test"
	perfObjects := createPerfObject("", measurement, "O", []string{"*"}, []string{"C1", "C2"}, true, false, false)
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	fpm := &FakePerformanceQuery{
		counters: createCounterMap(append([]string{"\\O(*)\\C1", "\\O(*)\\C2"}, cps1...), []float64{0, 0, 1.1, 1.2, 1.3, 1.4}, []uint32{0, 0, 0, 0, 0, 0}),
		expandPaths: map[string][]string{
			"\\O(*)\\C1": {cps1[0], cps1[2]},
			"\\O(*)\\C2": {cps1[1], cps1[3]},
		},
		vistaAndNewer: true,
	}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		Object:                perfObjects,
		UseWildcardsExpansion: false,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": fpm},
		},
		CountersRefreshInterval: config.Duration(time.Second * 10)}
	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)
	require.NoError(t, err)
	require.Len(t, acc1.Metrics, 2)

	fields1 := map[string]interface{}{
		"C1": 1.1,
		"C2": 1.2,
	}
	tags1 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)

	fields2 := map[string]interface{}{
		"C1": 1.3,
		"C2": 1.4,
	}
	tags2 := map[string]string{
		"instance":   "I2",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields2, tags2)
	// test finding new instance
	cps2 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2", "\\O(I3)\\C1", "\\O(I3)\\C2"}
	fpm = &FakePerformanceQuery{
		counters: createCounterMap(
			append([]string{"\\O(*)\\C1", "\\O(*)\\C2"}, cps2...),
			[]float64{0, 0, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6},
			[]uint32{0, 0, 0, 0, 0, 0, 0, 0},
		),
		expandPaths: map[string][]string{
			"\\O(*)\\C1": {cps2[0], cps2[2], cps2[4]},
			"\\O(*)\\C2": {cps2[1], cps2[3], cps2[5]},
		},
		vistaAndNewer: true,
	}

	err = m.cleanQueries()
	require.NoError(t, err)
	m.lastRefreshed = time.Time{}
	m.queryCreator = &FakePerformanceQueryCreator{
		fakeQueries: map[string]*FakePerformanceQuery{"localhost": fpm},
	}

	var acc2 testutil.Accumulator

	fields3 := map[string]interface{}{
		"C1": 1.5,
		"C2": 1.6,
	}
	tags3 := map[string]string{
		"instance":   "I3",
		"objectname": "O",
		"source":     hostname(),
	}

	// test before elapsing CounterRefreshRate counters are not refreshed

	err = m.Gather(&acc2)
	require.NoError(t, err)
	counters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)
	require.Len(t, acc2.Metrics, 3)

	acc2.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	acc2.AssertContainsTaggedFields(t, measurement, fields2, tags2)
	acc2.AssertContainsTaggedFields(t, measurement, fields3, tags3)
	// test changed configuration
	perfObjects = createPerfObject("", measurement, "O", []string{"*"}, []string{"C1", "C2", "C3"}, true, false, false)
	cps3 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I1)\\C3", "\\O(I2)\\C1", "\\O(I2)\\C2", "\\O(I2)\\C3"}
	fpm = &FakePerformanceQuery{
		counters: createCounterMap(
			append([]string{"\\O(*)\\C1", "\\O(*)\\C2", "\\O(*)\\C3"}, cps3...),
			[]float64{0, 0, 0, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6},
			[]uint32{0, 0, 0, 0, 0, 0, 0, 0, 0},
		),
		expandPaths: map[string][]string{
			"\\O(*)\\C1": {cps3[0], cps3[3]},
			"\\O(*)\\C2": {cps3[1], cps3[4]},
			"\\O(*)\\C3": {cps3[2], cps3[5]},
		},
		vistaAndNewer: true,
	}
	err = m.cleanQueries()
	m.lastRefreshed = time.Time{}
	require.NoError(t, err)
	m.queryCreator = &FakePerformanceQueryCreator{
		fakeQueries: map[string]*FakePerformanceQuery{"localhost": fpm},
	}
	m.Object = perfObjects

	time.Sleep(time.Duration(m.CountersRefreshInterval))

	var acc3 testutil.Accumulator
	err = m.Gather(&acc3)
	require.NoError(t, err)
	require.Len(t, acc3.Metrics, 2)
	fields4 := map[string]interface{}{
		"C1": 1.1,
		"C2": 1.2,
		"C3": 1.3,
	}
	tags4 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     hostname(),
	}
	fields5 := map[string]interface{}{
		"C1": 1.4,
		"C2": 1.5,
		"C3": 1.6,
	}
	tags5 := map[string]string{
		"instance":   "I2",
		"objectname": "O",
		"source":     hostname(),
	}

	acc3.AssertContainsTaggedFields(t, measurement, fields4, tags4)
	acc3.AssertContainsTaggedFields(t, measurement, fields5, tags5)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestGatherTotalNoExpansion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	var err error
	measurement := "m"
	perfObjects := createPerfObject("", measurement, "O", []string{"*"}, []string{"C1", "C2"}, true, true, false)
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(_Total)\\C1", "\\O(_Total)\\C2"}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		UseWildcardsExpansion: false,
		Object:                perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(
					append([]string{"\\O(*)\\C1", "\\O(*)\\C2"}, cps1...),
					[]float64{0, 0, 1.1, 1.2, 1.3, 1.4},
					[]uint32{0, 0, 0, 0, 0, 0}),
				expandPaths: map[string][]string{
					"\\O(*)\\C1": {cps1[0], cps1[2]},
					"\\O(*)\\C2": {cps1[1], cps1[3]},
				},
				vistaAndNewer: true,
			},
			},
		},
	}
	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	require.NoError(t, err)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)
	require.Len(t, acc1.Metrics, 2)
	fields1 := map[string]interface{}{
		"C1": 1.1,
		"C2": 1.2,
	}
	tags1 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)

	fields2 := map[string]interface{}{
		"C1": 1.3,
		"C2": 1.4,
	}
	tags2 := map[string]string{
		"instance":   "_Total",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields2, tags2)

	perfObjects[0].IncludeTotal = false

	err = m.cleanQueries()
	require.NoError(t, err)
	m.UseWildcardsExpansion = true
	m.lastRefreshed = time.Time{}

	var acc2 testutil.Accumulator
	err = m.Gather(&acc2)
	require.NoError(t, err)
	counters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)
	require.Len(t, acc2.Metrics, 1)

	acc2.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	acc2.AssertDoesNotContainsTaggedFields(t, measurement, fields2, tags2)
	err = m.cleanQueries()
	require.NoError(t, err)
}

func TestGatherMultiComps(t *testing.T) {
	var err error
	perfObjects := []perfObject{
		createPerfObject("", "m", "O", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)[0],
		createPerfObject("cmp1", "m1", "O", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)[0],
		createPerfObject("cmp2", "m2", "O", []string{"I1", "I2"}, []string{"C1", "C2"}, false, false, false)[0],
	}
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(I2)\\C1", "\\O(I2)\\C2"}
	cps2 := []string{"\\\\cmp1\\O(I1)\\C1", "\\\\cmp1\\O(I1)\\C2", "\\\\cmp1\\O(I2)\\C1", "\\\\cmp1\\O(I2)\\C2"}
	cps3 := []string{"\\\\cmp2\\O(I1)\\C1", "\\\\cmp2\\O(I1)\\C2", "\\\\cmp2\\O(I2)\\C1", "\\\\cmp2\\O(I2)\\C2"}
	m := WinPerfCounters{
		Log:        testutil.Logger{},
		PrintValid: false,
		Object:     perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(cps1,
					[]float64{1.1, 1.2, 1.3, 1.4},
					[]uint32{0, 0, 0, 0}),
				expandPaths: map[string][]string{
					cps1[0]: {cps1[0]},
					cps1[1]: {cps1[1]},
					cps1[2]: {cps1[2]},
					cps1[3]: {cps1[3]},
				},
				vistaAndNewer: true,
			},
				"cmp1": {
					counters: createCounterMap(cps2,
						[]float64{2.1, 2.2, 2.3, 2.4},
						[]uint32{0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps2[0]: {cps2[0]},
						cps2[1]: {cps2[1]},
						cps2[2]: {cps2[2]},
						cps2[3]: {cps2[3]},
					},
					vistaAndNewer: true,
				},
				"cmp2": {
					counters: createCounterMap(cps3,
						[]float64{3.1, 3.2, 3.3, 3.4},
						[]uint32{0, 0, 0, 0}),
					expandPaths: map[string][]string{
						cps3[0]: {cps3[0]},
						cps3[1]: {cps3[1]},
						cps3[2]: {cps3[2]},
						cps3[3]: {cps3[3]},
					},
					vistaAndNewer: true,
				},
			},
		},
	}
	var acc testutil.Accumulator
	err = m.Gather(&acc)
	require.NoError(t, err)
	require.Len(t, acc.Metrics, 6)
	fields1 := map[string]interface{}{
		"C1": 1.1,
		"C2": 1.2,
	}
	tags1 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     hostname(),
	}
	fields2 := map[string]interface{}{
		"C1": 1.3,
		"C2": 1.4,
	}
	tags2 := map[string]string{
		"instance":   "I2",
		"objectname": "O",
		"source":     hostname(),
	}
	acc.AssertContainsTaggedFields(t, "m", fields1, tags1)
	acc.AssertContainsTaggedFields(t, "m", fields2, tags2)
	fields3 := map[string]interface{}{
		"C1": 2.1,
		"C2": 2.2,
	}
	tags3 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     "cmp1",
	}
	fields4 := map[string]interface{}{
		"C1": 2.3,
		"C2": 2.4,
	}
	tags4 := map[string]string{
		"instance":   "I2",
		"objectname": "O",
		"source":     "cmp1",
	}
	acc.AssertContainsTaggedFields(t, "m1", fields3, tags3)
	acc.AssertContainsTaggedFields(t, "m1", fields4, tags4)
	fields5 := map[string]interface{}{
		"C1": 3.1,
		"C2": 3.2,
	}
	tags5 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     "cmp2",
	}
	fields6 := map[string]interface{}{
		"C1": 3.3,
		"C2": 3.4,
	}
	tags6 := map[string]string{
		"instance":   "I2",
		"objectname": "O",
		"source":     "cmp2",
	}
	acc.AssertContainsTaggedFields(t, "m2", fields5, tags5)
	acc.AssertContainsTaggedFields(t, "m2", fields6, tags6)
}

func TestGatherRaw(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}
	var err error
	measurement := "m"
	perfObjects := createPerfObject("", measurement, "O", []string{"*"}, []string{"C1", "C2"}, true, true, true)
	cps1 := []string{"\\O(I1)\\C1", "\\O(I1)\\C2", "\\O(_Total)\\C1", "\\O(_Total)\\C2"}
	m := WinPerfCounters{
		Log:                   testutil.Logger{},
		PrintValid:            false,
		UseWildcardsExpansion: false,
		Object:                perfObjects,
		queryCreator: &FakePerformanceQueryCreator{
			fakeQueries: map[string]*FakePerformanceQuery{"localhost": {
				counters: createCounterMap(
					append([]string{"\\O(*)\\C1", "\\O(*)\\C2"}, cps1...),
					[]float64{0, 0, 1.1, 2.2, 3.3, 4.4},
					[]uint32{0, 0, 0, 0, 0, 0}),
				expandPaths: map[string][]string{
					"\\O(*)\\C1": {cps1[0], cps1[2]},
					"\\O(*)\\C2": {cps1[1], cps1[3]},
				},
				vistaAndNewer: true,
			},
			},
		},
	}
	var acc1 testutil.Accumulator
	err = m.Gather(&acc1)
	require.NoError(t, err)
	counters, ok := m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 2)
	require.Len(t, acc1.Metrics, 2)
	fields1 := map[string]interface{}{
		"C1_Raw": int64(1),
		"C2_Raw": int64(2),
	}
	tags1 := map[string]string{
		"instance":   "I1",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields1, tags1)

	fields2 := map[string]interface{}{
		"C1_Raw": int64(3),
		"C2_Raw": int64(4),
	}
	tags2 := map[string]string{
		"instance":   "_Total",
		"objectname": "O",
		"source":     hostname(),
	}
	acc1.AssertContainsTaggedFields(t, measurement, fields2, tags2)

	m.UseWildcardsExpansion = true
	err = m.cleanQueries()
	require.NoError(t, err)
	m.lastRefreshed = time.Time{}

	var acc2 testutil.Accumulator
	err = m.Gather(&acc2)
	require.NoError(t, err)

	counters, ok = m.hostCounters["localhost"]
	require.True(t, ok)
	require.Len(t, counters.counters, 4) // expanded counters
	require.Len(t, acc2.Metrics, 2)

	acc2.AssertContainsTaggedFields(t, measurement, fields1, tags1)
	acc2.AssertContainsTaggedFields(t, measurement, fields2, tags2)
}

// list of nul terminated strings from WinAPI
var unicodeStringListWithEnglishChars = []uint16{0x5c, 0x5c, 0x54, 0x34, 0x38, 0x30, 0x5c, 0x50, 0x68, 0x79, 0x73, 0x69, 0x63, 0x61, 0x6c, 0x44, 0x69, 0x73,
	0x6b, 0x28, 0x30, 0x20, 0x43, 0x3a, 0x29, 0x5c, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x20, 0x44, 0x69, 0x73, 0x6b, 0x20, 0x51, 0x75, 0x65, 0x75,
	0x65, 0x20, 0x4c, 0x65, 0x6e, 0x67, 0x74, 0x68, 0x0, 0x5c, 0x5c, 0x54, 0x34, 0x38, 0x30, 0x5c, 0x50, 0x68, 0x79, 0x73, 0x69, 0x63, 0x61, 0x6c, 0x44,
	0x69, 0x73, 0x6b, 0x28, 0x5f, 0x54, 0x6f, 0x74, 0x61, 0x6c, 0x29, 0x5c, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x20, 0x44, 0x69, 0x73, 0x6b, 0x20,
	0x51, 0x75, 0x65, 0x75, 0x65, 0x20, 0x4c, 0x65, 0x6e, 0x67, 0x74, 0x68, 0x0, 0x0}
var unicodeStringListWithCzechChars = []uint16{0x5c, 0x5c, 0x54, 0x34, 0x38, 0x30, 0x5c, 0x46, 0x79, 0x7a, 0x69, 0x63, 0x6b, 0xfd, 0x20, 0x64, 0x69, 0x73,
	0x6b, 0x28, 0x30, 0x20, 0x43, 0x3a, 0x29, 0x5c, 0x41, 0x6b, 0x74, 0x75, 0xe1, 0x6c, 0x6e, 0xed, 0x20, 0x64, 0xe9, 0x6c, 0x6b, 0x61, 0x20, 0x66, 0x72,
	0x6f, 0x6e, 0x74, 0x79, 0x20, 0x64, 0x69, 0x73, 0x6b, 0x75, 0x0, 0x5c, 0x5c, 0x54, 0x34, 0x38, 0x30, 0x5c, 0x46, 0x79, 0x7a, 0x69, 0x63, 0x6b, 0xfd,
	0x20, 0x64, 0x69, 0x73, 0x6b, 0x28, 0x5f, 0x54, 0x6f, 0x74, 0x61, 0x6c, 0x29, 0x5c, 0x41, 0x6b, 0x74, 0x75, 0xe1, 0x6c, 0x6e, 0xed, 0x20, 0x64, 0xe9,
	0x6c, 0x6b, 0x61, 0x20, 0x66, 0x72, 0x6f, 0x6e, 0x74, 0x79, 0x20, 0x64, 0x69, 0x73, 0x6b, 0x75, 0x0, 0x0}
var unicodeStringListSingleItem = []uint16{0x5c, 0x5c, 0x54, 0x34, 0x38, 0x30, 0x5c, 0x50, 0x68, 0x79, 0x73, 0x69, 0x63, 0x61, 0x6c, 0x44, 0x69, 0x73, 0x6b,
	0x28, 0x30, 0x20, 0x43, 0x3a, 0x29, 0x5c, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x20, 0x44, 0x69, 0x73, 0x6b, 0x20, 0x51, 0x75, 0x65, 0x75, 0x65, 0x20,
	0x4c, 0x65, 0x6e, 0x67, 0x74, 0x68, 0x0, 0x0}
var unicodeStringListNoItem = []uint16{0x0}

var stringArrayWithEnglishChars = []string{
	"\\\\T480\\PhysicalDisk(0 C:)\\Current Disk Queue Length",
	"\\\\T480\\PhysicalDisk(_Total)\\Current Disk Queue Length",
}
var stringArrayWithCzechChars = []string{
	"\\\\T480\\Fyzick\u00fd disk(0 C:)\\Aktu\u00e1ln\u00ed d\u00e9lka fronty disku",
	"\\\\T480\\Fyzick\u00fd disk(_Total)\\Aktu\u00e1ln\u00ed d\u00e9lka fronty disku",
}

var stringArraySingleItem = []string{
	"\\\\T480\\PhysicalDisk(0 C:)\\Current Disk Queue Length",
}

func TestUTF16ToStringArray(t *testing.T) {
	singleItem := UTF16ToStringArray(unicodeStringListSingleItem)
	require.Equal(t, singleItem, stringArraySingleItem, "Not equal single arrays")

	noItem := UTF16ToStringArray(unicodeStringListNoItem)
	require.Nil(t, noItem)

	engStrings := UTF16ToStringArray(unicodeStringListWithEnglishChars)
	require.Equal(t, engStrings, stringArrayWithEnglishChars, "Not equal eng arrays")

	czechStrings := UTF16ToStringArray(unicodeStringListWithCzechChars)
	require.Equal(t, czechStrings, stringArrayWithCzechChars, "Not equal czech arrays")
}

func TestNoWildcards(t *testing.T) {
	m := WinPerfCounters{
		Object:                     createPerfObject("", "measurement", "object", []string{"instance"}, []string{"counter*"}, false, false, false),
		UseWildcardsExpansion:      true,
		LocalizeWildcardsExpansion: false,
		Log:                        testutil.Logger{},
	}
	require.Error(t, m.Init())
	m = WinPerfCounters{
		Object:                     createPerfObject("", "measurement", "object?", []string{"instance"}, []string{"counter"}, false, false, false),
		UseWildcardsExpansion:      true,
		LocalizeWildcardsExpansion: false,
		Log:                        testutil.Logger{},
	}
	require.Error(t, m.Init())
}

func TestLocalizeWildcardsExpansion(t *testing.T) {
	// this test is valid only on localized windows
	if testing.Short() {
		t.Skip("Skipping long taking test in short mode")
	}

	const counter = "% Processor Time"
	m := WinPerfCounters{
		queryCreator:            &performanceQueryCreatorImpl{},
		CountersRefreshInterval: config.Duration(time.Second * 60),
		Object: createPerfObject("", "measurement", "Processor Information",
			[]string{"_Total"}, []string{counter}, true, false, false),
		LocalizeWildcardsExpansion: false,
		UseWildcardsExpansion:      true,
		MaxBufferSize:              defaultMaxBufferSize,
		Log:                        testutil.Logger{},
	}

	require.NoError(t, m.Init())
	var acc testutil.Accumulator
	require.NoError(t, m.Gather(&acc))
	require.Len(t, acc.Metrics, 1)

	// running on localized windows with UseWildcardsExpansion and
	// with LocalizeWildcardsExpansion, this will be localized. Using LocalizeWildcardsExpansion=false it will
	// be English.
	require.Contains(t, acc.Metrics[0].Fields, sanitizedChars.Replace(counter))
}

func TestCheckError(t *testing.T) {
	tests := []struct {
		Name          string
		Err           error
		IgnoredErrors []string
		ExpectedErr   error
	}{
		{
			Name: "Ignore PDH_NO_DATA",
			Err: &pdhError{
				ErrorCode: uint32(PdhNoData),
			},
			IgnoredErrors: []string{
				"PDH_NO_DATA",
			},
			ExpectedErr: nil,
		},
		{
			Name: "Don't ignore PDH_NO_DATA",
			Err: &pdhError{
				ErrorCode: uint32(PdhNoData),
			},
			ExpectedErr: &pdhError{
				ErrorCode: uint32(PdhNoData),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			m := WinPerfCounters{
				IgnoredErrors: tc.IgnoredErrors,
			}

			err := m.checkError(tc.Err)
			require.Equal(t, tc.ExpectedErr, err)
		})
	}
}
