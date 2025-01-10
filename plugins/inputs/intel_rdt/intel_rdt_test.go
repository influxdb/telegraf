//go:build !windows

package intel_rdt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

type mockProc struct{}

func (*mockProc) getAllProcesses() ([]process, error) {
	procs := []process{
		{Name: "process", PID: 1000},
		{Name: "process2", PID: 1002},
		{Name: "process2", PID: 1003},
	}
	return procs, nil
}

func TestAssociateProcessesWithPIDs(t *testing.T) {
	log := testutil.Logger{}
	proc := &mockProc{}
	rdt := IntelRDT{
		Log:       log,
		processor: proc,
	}
	processes := []string{"process"}
	expectedPID := "1000"
	result, err := rdt.associateProcessesWithPIDs(processes)
	require.NoError(t, err)
	require.Equal(t, expectedPID, result[processes[0]])

	processes = []string{"process2"}
	expectedPID = "1002,1003"
	result, err = rdt.associateProcessesWithPIDs(processes)
	require.NoError(t, err)
	require.Equal(t, expectedPID, result[processes[0]])

	processes = []string{"process1"}
	result, err = rdt.associateProcessesWithPIDs(processes)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestSplitCSVLineIntoValues(t *testing.T) {
	line := "2020-08-12 13:34:36,\"45417,29170\",37,44,0.00,0,0.0,0.0,0.0,0.0"
	expectedTimeValue := "2020-08-12 13:34:36"
	expectedMetricsValue := []string{"0.00", "0", "0.0", "0.0", "0.0", "0.0"}
	expectedCoreOrPidsValue := []string{"\"45417", "29170\"", "37", "44"}

	splitCSV, err := splitCSVLineIntoValues(line)
	require.NoError(t, err)
	require.Equal(t, expectedTimeValue, splitCSV.timeValue)
	require.Equal(t, expectedMetricsValue, splitCSV.metricsValues)
	require.Equal(t, expectedCoreOrPidsValue, splitCSV.coreOrPIDsValues)

	wrongLine := "2020-08-12 13:34:36,37,44,0.00,0,0.0"
	splitCSV, err = splitCSVLineIntoValues(wrongLine)
	require.Error(t, err)
	require.Equal(t, "", splitCSV.timeValue)
	require.Nil(t, splitCSV.metricsValues)
	require.Nil(t, splitCSV.coreOrPIDsValues)
}

func TestFindPIDsInMeasurement(t *testing.T) {
	line := "2020-08-12 13:34:36,\"45417,29170\""
	expected := "45417,29170"
	result, err := findPIDsInMeasurement(line)
	require.NoError(t, err)
	require.Equal(t, expected, result)

	line = "pids not included"
	result, err = findPIDsInMeasurement(line)
	require.Error(t, err)
	require.Equal(t, "", result)
}

func TestCreateArgsProcesses(t *testing.T) {
	processesPIDs := map[string]string{
		"process": "12345, 99999",
	}
	expected := "--mon-pid=all:[12345, 99999];mbt:[12345, 99999];"
	result := createArgProcess(processesPIDs)
	require.EqualValues(t, expected, result)

	processesPIDs = map[string]string{
		"process":  "12345, 99999",
		"process2": "44444, 11111",
	}
	expectedPrefix := "--mon-pid="
	expectedSubstring := "all:[12345, 99999];mbt:[12345, 99999];"
	expectedSubstring2 := "all:[44444, 11111];mbt:[44444, 11111];"
	result = createArgProcess(processesPIDs)
	require.Contains(t, result, expectedPrefix)
	require.Contains(t, result, expectedSubstring)
	require.Contains(t, result, expectedSubstring2)
}

func TestCreateArgsCores(t *testing.T) {
	cores := []string{"1,2,3"}
	expected := "--mon-core=all:[1,2,3];mbt:[1,2,3];"
	result := createArgCores(cores)
	require.EqualValues(t, expected, result)

	cores = []string{"1,2,3", "4,5,6"}
	expectedPrefix := "--mon-core="
	expectedSubstring := "all:[1,2,3];mbt:[1,2,3];"
	expectedSubstring2 := "all:[4,5,6];mbt:[4,5,6];"
	result = createArgCores(cores)
	require.Contains(t, result, expectedPrefix)
	require.Contains(t, result, expectedSubstring)
	require.Contains(t, result, expectedSubstring2)
}

func TestParseCoresConfig(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		var configCores []string
		result, err := parseCoresConfig(configCores)
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("empty string in slice", func(t *testing.T) {
		configCores := []string{""}
		result, err := parseCoresConfig(configCores)
		require.Error(t, err)
		require.Empty(t, result)
	})

	t.Run("not correct string", func(t *testing.T) {
		configCores := []string{"wrong string"}
		result, err := parseCoresConfig(configCores)
		require.Error(t, err)
		require.Empty(t, result)
	})

	t.Run("not correct string", func(t *testing.T) {
		configCores := []string{"1,2", "wasd:#$!;"}
		result, err := parseCoresConfig(configCores)
		require.Error(t, err)
		require.Empty(t, result)
	})

	t.Run("not correct string", func(t *testing.T) {
		configCores := []string{"1,2,2"}
		result, err := parseCoresConfig(configCores)
		require.Error(t, err)
		require.Empty(t, result)
	})

	t.Run("coma separated cores - positive", func(t *testing.T) {
		configCores := []string{"0,1,2,3,4,5"}
		expected := []string{"0,1,2,3,4,5"}
		result, err := parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)

		configCores = []string{"0,1,2", "3,4,5"}
		expected = []string{"0,1,2", "3,4,5"}
		result, err = parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)

		configCores = []string{"0,4,1", "2,3,5", "9"}
		expected = []string{"0,4,1", "2,3,5", "9"}
		result, err = parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)
	})

	t.Run("coma separated cores - negative", func(t *testing.T) {
		// cannot monitor same cores in different groups
		configCores := []string{"0,1,2", "2"}
		result, err := parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)

		configCores = []string{"0,1,2", "2,3,4"}
		result, err = parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)

		configCores = []string{"0,-1,2", "2,3,4"}
		result, err = parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("dash separated cores - positive", func(t *testing.T) {
		configCores := []string{"0-5"}
		expected := []string{"0,1,2,3,4,5"}
		result, err := parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)

		configCores = []string{"0-5", "7-10"}
		expected = []string{"0,1,2,3,4,5", "7,8,9,10"}
		result, err = parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)

		configCores = []string{"5-5"}
		expected = []string{"5"}
		result, err = parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)
	})

	t.Run("dash separated cores - negative", func(t *testing.T) {
		// cannot monitor same cores in different groups
		configCores := []string{"0-5", "2-7"}
		result, err := parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)

		// more than two values in range
		configCores = []string{"0-5-10"}
		result, err = parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)

		// first value cannot be higher than second
		configCores = []string{"12-5"}
		result, err = parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)

		configCores = []string{"0-"}
		result, err = parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("mixed separator - positive", func(t *testing.T) {
		configCores := []string{"0-5,6,7"}
		expected := []string{"0,1,2,3,4,5,6,7"}
		result, err := parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)

		configCores = []string{"0-5,6,7", "8,9,10"}
		expected = []string{"0,1,2,3,4,5,6,7", "8,9,10"}
		result, err = parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)

		configCores = []string{"0-7", "8-10"}
		expected = []string{"0,1,2,3,4,5,6,7", "8,9,10"}
		result, err = parseCoresConfig(configCores)
		require.NoError(t, err)
		require.EqualValues(t, expected, result)
	})

	t.Run("mixed separator - negative", func(t *testing.T) {
		// cannot monitor same cores in different groups
		configCores := []string{"0-5,", "2-7"}
		result, err := parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)

		// cores cannot be duplicated
		configCores = []string{"0-5,5"}
		result, err = parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)

		// more than two values in range
		configCores = []string{"0-5-6,9"}
		result, err = parseCoresConfig(configCores)
		require.Error(t, err)
		require.Nil(t, result)
	})
}
