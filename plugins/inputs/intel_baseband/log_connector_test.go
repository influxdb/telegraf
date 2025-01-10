//go:build linux && amd64

package intel_baseband

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadLogFile(t *testing.T) {
	testCases := []struct {
		name        string
		testLogPath string
		err         error
	}{
		{"when file doesn't exist return the error", "testdata/logfiles/doesntexist", errors.New("no such file or directory")},
		{"when the file is empty return the error", "testdata/logfiles/empty.log", errors.New("log file is empty")},
		{"when the log file is correct, error should be nil", "testdata/logfiles/example.log", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logConnector := prepareLogConnMock()
			require.NotNil(t, logConnector)
			logConnector.path = tc.testLogPath

			err := logConnector.readLogFile()
			if tc.err != nil {
				require.ErrorContains(t, err, tc.err.Error())
				return
			}

			require.NoError(t, err)
			data := logConnector.getLogLines()
			require.NotEmpty(t, data)
			require.NoError(t, err)
		})
	}
}

func TestGetMetric(t *testing.T) {
	testCases := []struct {
		name              string
		input             []string
		metricName        string
		expectedOperation string
		expectedData      []string
		err               error
	}{
		{"with correct string no error should be returned",
			[]string{"Thu May 18 08:45:15 2023:INFO:5GUL counters: Code Blocks", "Thu May 18 08:45:15 2023:INFO:0 0"},
			vfCodeBlocks, "5GUL", []string{"0", "0"}, nil},

		{"with correct string no error should be returned",
			[]string{"Thu May 18 08:45:15 2023:INFO:5GUL counters: Data (Bytes)", "Thu May 18 08:45:15 2023:INFO:0 0"},
			vfDataBlock, "5GUL", []string{"0", "0"}, nil},

		{"with correct string no error should be returned",
			[]string{"Thu May 18 08:45:15 2023:INFO:5GUL counters: Per Engine", "Thu May 18 08:45:15 2023:INFO:0 0 3 0 50 0 200 0"},
			engineBlock, "5GUL", []string{"0", "0", "3", "0", "50", "0", "200", "0"}, nil},

		{"when the incorrect number of lines provided, error should be returned",
			[]string{"Thu May 18 08:45:15 2023:INFO:5GUL counters: Per Engine"},
			engineBlock, "5GUL", []string{""}, errors.New("the content of the log file is incorrect")},

		{"when the incorrect number of lines provided, error should be returned",
			[]string{"Thu May 18 08:45:15 2023:INFO:5GUL counters: Per Engine", ""},
			engineBlock, "5GUL", []string{""}, errors.New("the content of the log file is incorrect")},

		{"when the incorrect line provided, error should be returned", []string{"Something different"},
			"", "5GUL", []string{""}, errors.New("substring is empty")},

		{"when the incorrect line provided error should be returned", []string{"Device Status:: 1 VFs", "INFO:00counters:", "INFO:0  0"},
			"I", "", nil, errors.New("metric's data is empty")},

		{"when the incorrect metric's line provided error should be returned", []string{"Device Status:: 1 VFs", "INFO:00counters:B", "INFO: "},
			"B", "", nil, errors.New("metric's data is incorrect")},

		{"when the operation name wasn't found, error should be returned", []string{"Device Status:: 1 VFs", "", "INFO:countersCode Blocks"},
			"B", "", nil, errors.New("valid operation name wasn't found in log")},

		{"when lines are empty, error should be returned", []string{""},
			"something", "5GUL", []string{""}, errors.New("couldn't find the substring")},

		{"when lines are empty, error should be returned", nil,
			"something", "5GUL", []string{""}, errors.New("couldn't find the substring")},
	}

	logConnector := prepareLogConnMock()
	require.NotNil(t, logConnector)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logConnector.lines = tc.input
			offset, metric, err := logConnector.getMetric(0, tc.metricName)
			if tc.err != nil {
				require.ErrorContains(t, err, tc.err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, 2, offset)
			require.Equal(t, tc.expectedOperation, metric.operationName)
			require.ElementsMatch(t, tc.expectedData, metric.data)
		})
	}
}

func TestReadAndGetMetrics(t *testing.T) {
	testCases := []struct {
		name               string
		filePath           string
		metricName         string
		expectedOperations []string
		expectedData       [][]string
	}{
		{"with correct values no error should be returned for Code Blocks",
			"testdata/logfiles/example.log",
			vfCodeBlocks, []string{"5GUL", "5GDL"}, [][]string{{"0", "0"}, {"1", "0"}}},
		{"with correct values no error should be returned for Data Blocks",
			"testdata/logfiles/example.log",
			vfDataBlock, []string{"5GUL", "5GDL"}, [][]string{{"0", "0"}, {"2699", "0"}}},
		{"with correct values no error should be returned for Per Engine Blocks",
			"testdata/logfiles/example.log",
			engineBlock, []string{"5GUL", "5GDL"}, [][]string{{"0", "0", "0", "0", "0", "0", "0", "0"}, {"1", "0", "0"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logConnector := prepareLogConnMock()
			require.NotNil(t, logConnector)
			logConnector.path = tc.filePath
			err := logConnector.readLogFile()
			require.NoError(t, err)
			metrics, err := logConnector.getMetrics(tc.metricName)
			require.NoError(t, err)
			require.Len(t, metrics, len(tc.expectedOperations))

			for i := range metrics {
				require.Equal(t, tc.expectedOperations[i], metrics[i].operationName)
				require.ElementsMatch(t, tc.expectedData[i], metrics[i].data)
			}
		})
	}
}

func TestGetMetrics(t *testing.T) {
	testCases := []struct {
		name               string
		input              []string
		metricName         string
		expectedOperations []string
		expectedData       [][]string
		err                error
	}{
		{"with correct values no error should be returned",
			[]string{"Thu May 18 08:45:15 2023:INFO:5GUL counters: Code Blocks", "Thu May 18 08:45:15 2023:INFO:0 0",
				"Thu May 18 08:45:15 2023:INFO:5GUL counters: XXXX XXXX", "Thu May 18 08:45:15 2023:INFO:0 1", "sdasadasdsa",
				"Thu May 18 08:45:15 2023:INFO:5GDL counters: Code Blocks", "Thu May 18 08:45:15 2023:INFO:1 1",
				"Thu May 18 08:45:15 2023:INFO:5GUL counters: XXXX XXXX", "Thu May 18 08:45:15 2023:INFO:0 1", "sdasadasdsa"},
			vfCodeBlocks, []string{"5GUL", "5GDL"}, [][]string{{"0", "0"}, {"1", "1"}}, nil},

		{"when lines are empty, error should be returned", []string{""},
			"something", nil, nil, errors.New("couldn't find the substring in the log file")},

		{"when lines are nil, error should be returned", nil,
			"something", nil, nil, errors.New("couldn't find the substring in the log file")},
	}

	logConnector := prepareLogConnMock()
	require.NotNil(t, logConnector)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logConnector.lines = tc.input
			metrics, err := logConnector.getMetrics(tc.metricName)
			if tc.err != nil {
				require.ErrorContains(t, err, tc.err.Error())
				return
			}

			require.NoError(t, err)
			require.Len(t, metrics, len(tc.expectedOperations))

			for i := range metrics {
				require.Equal(t, tc.expectedOperations[i], metrics[i].operationName)
				require.ElementsMatch(t, tc.expectedData[i], metrics[i].data)
			}
		})
	}
}

func TestGetNumVFs(t *testing.T) {
	testCases := []struct {
		name     string
		input    []string
		expected int
		err      error
	}{
		{"incorrect format of the line", []string{"Device Status::VFs"}, -1, errors.New("incorrect format of the line")},
		{"when the line is correct, no error should be returned", []string{"Device Status:: 0 VFs"}, 0, nil},
		{"when the line is correct, no error should be returned", []string{"Device Status:: 10 VFs"}, 10, nil},
		{"when the line is correct, no error should be returned", []string{"Device Status:: 5000 VFs"}, 5000, nil},
		{"when the value is not int, error should be returned", []string{"Device Status:: Nah VFs"}, -1, errors.New("invalid syntax")},
		{"when end prefix isn't found, error should be returned", []string{"Device Status:: Nah END"}, -1, errors.New("couldn't find device end prefix")},
		{"when the line is empty, error should be returned", []string{""}, -1, errors.New("numVFs data wasn't found")},
		{"when the line is empty, error should be returned", nil, -1, errors.New("numVFs data wasn't found")},
	}

	logConnector := prepareLogConnMock()
	require.NotNil(t, logConnector)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logConnector.lines = tc.input
			err := logConnector.readNumVFs()
			if tc.err != nil {
				require.ErrorContains(t, err, tc.err.Error())
				return
			}

			require.NoError(t, err)
			numVFs := logConnector.getNumVFs()
			require.Equal(t, tc.expected, numVFs)
			require.Equal(t, tc.expected, logConnector.numVFs)
		})
	}
}

func TestParseOperationName(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"Thu May 18 08:45:15 2023:INFO:5GUL counters: Code Blocks", "5GUL"},
		{"May 18 08:45:15 2023:INFO:5GUL counters: Per Engine", "5GUL"},
		{"023:INFO:3G counters: Per ", "3G"},
		{"Device Status:: Nah VFs", ""},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run("expected "+tc.expected, func(t *testing.T) {
			operationName := parseOperationName(tc.input)
			require.Equal(t, tc.expected, operationName)
		})
	}
}

func prepareLogConnMock() *logConnector {
	return &logConnector{
		path:        "",
		numVFs:      -1,
		lastModTime: time.Time{},
	}
}
