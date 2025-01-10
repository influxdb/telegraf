//go:build !windows

// TODO: Windows - should be enabled for Windows when super asterisk is fixed on Windows
// https://github.com/influxdata/telegraf/issues/6248

package exec

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/csv"
	"github.com/influxdata/telegraf/plugins/parsers/json"
	"github.com/influxdata/telegraf/plugins/parsers/value"
	"github.com/influxdata/telegraf/testutil"
)

const validJSON = `
{
    "status": "green",
    "num_processes": 82,
    "cpu": {
        "status": "red",
        "nil_status": null,
        "used": 8234,
        "free": 32
    },
    "percent": 0.81,
    "users": [0, 1, 2, 3]
}`

const malformedJSON = `
{
    "status": "green",
`

type carriageReturnTest struct {
	input  []byte
	output []byte
}

var crTests = []carriageReturnTest{
	{[]byte{0x4c, 0x69, 0x6e, 0x65, 0x20, 0x31, 0x0d, 0x0a, 0x4c, 0x69,
		0x6e, 0x65, 0x20, 0x32, 0x0d, 0x0a, 0x4c, 0x69, 0x6e, 0x65,
		0x20, 0x33},
		[]byte{0x4c, 0x69, 0x6e, 0x65, 0x20, 0x31, 0x0a, 0x4c, 0x69, 0x6e,
			0x65, 0x20, 0x32, 0x0a, 0x4c, 0x69, 0x6e, 0x65, 0x20, 0x33}},
	{[]byte{0x4c, 0x69, 0x6e, 0x65, 0x20, 0x31, 0x0a, 0x4c, 0x69, 0x6e,
		0x65, 0x20, 0x32, 0x0a, 0x4c, 0x69, 0x6e, 0x65, 0x20, 0x33},
		[]byte{0x4c, 0x69, 0x6e, 0x65, 0x20, 0x31, 0x0a, 0x4c, 0x69, 0x6e,
			0x65, 0x20, 0x32, 0x0a, 0x4c, 0x69, 0x6e, 0x65, 0x20, 0x33}},
	{[]byte{0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x61, 0x6c,
		0x6c, 0x20, 0x6f, 0x6e, 0x65, 0x20, 0x62, 0x69, 0x67, 0x20,
		0x6c, 0x69, 0x6e, 0x65},
		[]byte{0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x61, 0x6c,
			0x6c, 0x20, 0x6f, 0x6e, 0x65, 0x20, 0x62, 0x69, 0x67, 0x20,
			0x6c, 0x69, 0x6e, 0x65}},
}

type runnerMock struct {
	out    []byte
	errout []byte
	err    error
}

func newRunnerMock(out, errout []byte, err error) runner {
	return &runnerMock{
		out:    out,
		errout: errout,
		err:    err,
	}
}

func (r runnerMock) run(_ string, _ []string, _ time.Duration) ([]byte, []byte, error) {
	return r.out, r.errout, r.err
}

func TestExec(t *testing.T) {
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())
	e := &Exec{
		Log:      testutil.Logger{},
		runner:   newRunnerMock([]byte(validJSON), nil, nil),
		Commands: []string{"testcommand arg1"},
		parser:   parser,
	}

	var acc testutil.Accumulator
	err := acc.GatherError(e.Gather)
	require.NoError(t, err)
	require.Equal(t, 8, acc.NFields(), "non-numeric measurements should be ignored")

	fields := map[string]interface{}{
		"num_processes": float64(82),
		"cpu_used":      float64(8234),
		"cpu_free":      float64(32),
		"percent":       float64(0.81),
		"users_0":       float64(0),
		"users_1":       float64(1),
		"users_2":       float64(2),
		"users_3":       float64(3),
	}
	acc.AssertContainsFields(t, "exec", fields)
}

func TestExecMalformed(t *testing.T) {
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())
	e := &Exec{
		Log:      testutil.Logger{},
		runner:   newRunnerMock([]byte(malformedJSON), nil, nil),
		Commands: []string{"badcommand arg1"},
		parser:   parser,
	}

	var acc testutil.Accumulator
	require.Error(t, acc.GatherError(e.Gather))
	require.Equal(t, 0, acc.NFields(), "No new points should have been added")
}

func TestCommandError(t *testing.T) {
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())
	e := &Exec{
		Log:      testutil.Logger{},
		runner:   newRunnerMock(nil, nil, errors.New("exit status code 1")),
		Commands: []string{"badcommand"},
		parser:   parser,
	}

	var acc testutil.Accumulator
	require.Error(t, acc.GatherError(e.Gather))
	require.Equal(t, 0, acc.NFields(), "No new points should have been added")
}

func TestCommandIgnoreError(t *testing.T) {
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())
	e := &Exec{
		Log:         testutil.Logger{},
		runner:      newRunnerMock([]byte(validJSON), []byte("error"), errors.New("exit status code 1")),
		Commands:    []string{"badcommand"},
		IgnoreError: true,
		parser:      parser,
	}

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(e.Gather))
	require.Equal(t, 8, acc.NFields(), "non-numeric measurements should be ignored")

	fields := map[string]interface{}{
		"num_processes": float64(82),
		"cpu_used":      float64(8234),
		"cpu_free":      float64(32),
		"percent":       float64(0.81),
		"users_0":       float64(0),
		"users_1":       float64(1),
		"users_2":       float64(2),
		"users_3":       float64(3),
	}
	acc.AssertContainsFields(t, "exec", fields)
}

func TestExecCommandWithGlob(t *testing.T) {
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())

	e := newExec()
	e.Commands = []string{"/bin/ech* metric_value"}
	e.SetParser(&parser)

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(e.Gather))

	fields := map[string]interface{}{
		"value": "metric_value",
	}
	acc.AssertContainsFields(t, "metric", fields)
}

func TestExecCommandWithoutGlob(t *testing.T) {
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())

	e := newExec()
	e.Commands = []string{"/bin/echo metric_value"}
	e.SetParser(&parser)

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(e.Gather))

	fields := map[string]interface{}{
		"value": "metric_value",
	}
	acc.AssertContainsFields(t, "metric", fields)
}

func TestExecCommandWithoutGlobAndPath(t *testing.T) {
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())
	e := newExec()
	e.Commands = []string{"echo metric_value"}
	e.SetParser(&parser)

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(e.Gather))

	fields := map[string]interface{}{
		"value": "metric_value",
	}
	acc.AssertContainsFields(t, "metric", fields)
}

func TestExecCommandWithEnv(t *testing.T) {
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())
	e := newExec()
	e.Commands = []string{"/bin/sh -c 'echo ${METRIC_NAME}'"}
	e.Environment = []string{"METRIC_NAME=metric_value"}
	e.SetParser(&parser)

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(e.Gather))

	fields := map[string]interface{}{
		"value": "metric_value",
	}
	acc.AssertContainsFields(t, "metric", fields)
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		bufF func() *bytes.Buffer
		expF func() *bytes.Buffer
	}{
		{
			name: "should not truncate",
			bufF: func() *bytes.Buffer {
				var b bytes.Buffer
				b.WriteString("hello world")
				return &b
			},
			expF: func() *bytes.Buffer {
				var b bytes.Buffer
				b.WriteString("hello world")
				return &b
			},
		},
		{
			name: "should truncate up to the new line",
			bufF: func() *bytes.Buffer {
				var b bytes.Buffer
				b.WriteString("hello world\nand all the people")
				return &b
			},
			expF: func() *bytes.Buffer {
				var b bytes.Buffer
				b.WriteString("hello world...")
				return &b
			},
		},
		{
			name: "should truncate to the maxStderrBytes",
			bufF: func() *bytes.Buffer {
				var b bytes.Buffer
				for i := 0; i < 2*maxStderrBytes; i++ {
					b.WriteByte('b')
				}
				return &b
			},
			expF: func() *bytes.Buffer {
				var b bytes.Buffer
				for i := 0; i < maxStderrBytes; i++ {
					b.WriteByte('b')
				}
				b.WriteString("...")
				return &b
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := truncate(*tt.bufF())
			require.Equal(t, tt.expF().Bytes(), res.Bytes())
		})
	}
}

func TestRemoveCarriageReturns(t *testing.T) {
	//nolint:staticcheck // Silence linter for now as we plan to reenable tests for Windows later
	if runtime.GOOS == "windows" {
		// Test that all carriage returns are removed
		for _, test := range crTests {
			b := bytes.NewBuffer(test.input)
			out := removeWindowsCarriageReturns(*b)
			require.True(t, bytes.Equal(test.output, out.Bytes()))
		}
	} else {
		// Test that the buffer is returned unaltered
		for _, test := range crTests {
			b := bytes.NewBuffer(test.input)
			out := removeWindowsCarriageReturns(*b)
			require.True(t, bytes.Equal(test.input, out.Bytes()))
		}
	}
}

func TestCSVBehavior(t *testing.T) {
	// Setup the CSV parser
	parser := &csv.Parser{
		MetricName:     "exec",
		HeaderRowCount: 1,
		ResetMode:      "always",
	}
	require.NoError(t, parser.Init())

	// Setup the plugin
	plugin := newExec()
	plugin.Commands = []string{"echo \"a,b\n1,2\n3,4\""}
	plugin.Log = testutil.Logger{}
	plugin.SetParser(parser)
	require.NoError(t, plugin.Init())

	expected := []telegraf.Metric{
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(1),
				"b": int64(2),
			},
			time.Unix(0, 1),
		),
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(3),
				"b": int64(4),
			},
			time.Unix(0, 2),
		),
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(1),
				"b": int64(2),
			},
			time.Unix(0, 3),
		),
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(3),
				"b": int64(4),
			},
			time.Unix(0, 4),
		),
	}

	var acc testutil.Accumulator
	// Run gather once
	require.NoError(t, plugin.Gather(&acc))
	// Run gather a second time
	require.NoError(t, plugin.Gather(&acc))
	require.Eventuallyf(t, func() bool {
		acc.Lock()
		defer acc.Unlock()
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(expected), acc.NMetrics())

	// Check the result
	options := []cmp.Option{
		testutil.SortMetrics(),
		testutil.IgnoreTime(),
	}
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, options...)
}

func TestCases(t *testing.T) {
	// Register the plugin
	inputs.Add("exec", func() telegraf.Input {
		return newExec()
	})

	// Setup the plugin
	cfg := config.NewConfig()
	require.NoError(t, cfg.LoadConfigData([]byte(`
	[[inputs.exec]]
	commands = [ "echo \"a,b\n1,2\n3,4\"" ]
	data_format = "csv"
	csv_header_row_count = 1
`)))
	require.Len(t, cfg.Inputs, 1)
	plugin := cfg.Inputs[0]
	require.NoError(t, plugin.Init())

	expected := []telegraf.Metric{
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(1),
				"b": int64(2),
			},
			time.Unix(0, 1),
		),
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(3),
				"b": int64(4),
			},
			time.Unix(0, 2),
		),
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(1),
				"b": int64(2),
			},
			time.Unix(0, 3),
		),
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"a": int64(3),
				"b": int64(4),
			},
			time.Unix(0, 4),
		),
	}

	var acc testutil.Accumulator
	// Run gather once
	require.NoError(t, plugin.Gather(&acc))
	// Run gather a second time
	require.NoError(t, plugin.Gather(&acc))
	require.Eventuallyf(t, func() bool {
		acc.Lock()
		defer acc.Unlock()
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(expected), acc.NMetrics())

	// Check the result
	options := []cmp.Option{
		testutil.SortMetrics(),
		testutil.IgnoreTime(),
	}
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, options...)
}
