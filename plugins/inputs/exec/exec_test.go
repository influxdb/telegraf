//go:build !windows

// TODO: Windows - should be enabled for Windows when super asterisk is fixed on Windows
// https://github.com/influxdata/telegraf/issues/6248

package exec

import (
	"bytes"
	"errors"
	"strings"
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

type runnerMock struct {
	out    []byte
	errout []byte
	err    error
}

func (r runnerMock) run(string) ([]byte, []byte, error) {
	return r.out, r.errout, r.err
}

func TestExec(t *testing.T) {
	// Setup parser
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands: []string{"testcommand arg1"},
		Log:      testutil.Logger{},
	}
	plugin.SetParser(parser)
	require.NoError(t, plugin.Init())
	plugin.runner = &runnerMock{out: []byte(validJSON)}

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"num_processes": float64(82),
				"cpu_used":      float64(8234),
				"cpu_free":      float64(32),
				"percent":       float64(0.81),
				"users_0":       float64(0),
				"users_1":       float64(1),
				"users_2":       float64(2),
				"users_3":       float64(3),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestExecMalformed(t *testing.T) {
	// Setup parser
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands: []string{"badcommand arg1"},
		Log:      testutil.Logger{},
	}
	plugin.SetParser(parser)
	require.NoError(t, plugin.Init())
	plugin.runner = &runnerMock{out: []byte(malformedJSON)}

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.ErrorContains(t, acc.GatherError(plugin.Gather), "unexpected end of JSON input")
	require.Empty(t, acc.GetTelegrafMetrics())
}

func TestCommandError(t *testing.T) {
	// Setup parser
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands: []string{"badcommand"},
		Log:      testutil.Logger{},
	}
	plugin.SetParser(parser)
	require.NoError(t, plugin.Init())
	plugin.runner = &runnerMock{err: errors.New("exit status code 1")}

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.ErrorContains(t, acc.GatherError(plugin.Gather), "exit status code 1 for command")
	require.Equal(t, 0, acc.NFields(), "No new points should have been added")
}

func TestCommandIgnoreError(t *testing.T) {
	// Setup parser
	parser := &json.Parser{MetricName: "exec"}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands:    []string{"badcommand"},
		IgnoreError: true,
		Log:         testutil.Logger{},
	}
	plugin.SetParser(parser)
	require.NoError(t, plugin.Init())
	plugin.runner = &runnerMock{
		out:    []byte(validJSON),
		errout: []byte("error"),
		err:    errors.New("exit status code 1"),
	}

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"exec",
			map[string]string{},
			map[string]interface{}{
				"num_processes": float64(82),
				"cpu_used":      float64(8234),
				"cpu_free":      float64(32),
				"percent":       float64(0.81),
				"users_0":       float64(0),
				"users_1":       float64(1),
				"users_2":       float64(2),
				"users_3":       float64(3),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestExecCommandWithGlob(t *testing.T) {
	// Setup parser
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands: []string{"/bin/ech* metric_value"},
		Timeout:  config.Duration(5 * time.Second),
		Log:      testutil.Logger{},
	}
	plugin.SetParser(&parser)
	require.NoError(t, plugin.Init())

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"metric",
			map[string]string{},
			map[string]interface{}{
				"value": "metric_value",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestExecCommandWithoutGlob(t *testing.T) {
	// Setup parser
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands: []string{"/bin/echo metric_value"},
		Timeout:  config.Duration(5 * time.Second),
		Log:      testutil.Logger{},
	}
	plugin.SetParser(&parser)
	require.NoError(t, plugin.Init())

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"metric",
			map[string]string{},
			map[string]interface{}{
				"value": "metric_value",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestExecCommandWithoutGlobAndPath(t *testing.T) {
	// Setup parser
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands: []string{"echo metric_value"},
		Timeout:  config.Duration(5 * time.Second),
		Log:      testutil.Logger{},
	}
	plugin.SetParser(&parser)
	require.NoError(t, plugin.Init())

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"metric",
			map[string]string{},
			map[string]interface{}{
				"value": "metric_value",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestExecCommandWithEnv(t *testing.T) {
	// Setup parser
	parser := value.Parser{
		MetricName: "metric",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())

	// Setup plugin
	plugin := &Exec{
		Commands:    []string{"/bin/sh -c 'echo ${METRIC_NAME}'"},
		Environment: []string{"METRIC_NAME=metric_value"},
		Timeout:     config.Duration(5 * time.Second),
		Log:         testutil.Logger{},
	}
	plugin.SetParser(&parser)
	require.NoError(t, plugin.Init())

	// Gather the metrics and check the result
	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"metric",
			map[string]string{},
			map[string]interface{}{
				"value": "metric_value",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		bufF     func() *bytes.Buffer
		expected string
	}{
		{
			name: "should not truncate",
			bufF: func() *bytes.Buffer {
				return bytes.NewBufferString("hello world")
			},
			expected: "hello world",
		},
		{
			name: "should truncate up to the new line",
			bufF: func() *bytes.Buffer {
				return bytes.NewBufferString("hello world\nand all the people")
			},
			expected: "hello world...",
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
			expected: strings.Repeat("b", maxStderrBytes) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := tt.bufF()
			truncate(buf)
			require.Equal(t, tt.expected, buf.String())
		})
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
	plugin := &Exec{
		Commands: []string{"echo \"a,b\n1,2\n3,4\""},
		Timeout:  config.Duration(5 * time.Second),
		Log:      testutil.Logger{},
	}
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
		return &Exec{
			Timeout: config.Duration(5 * time.Second),
			Log:     testutil.Logger{},
		}
	})

	// Setup the plugin
	cfg := config.NewConfig()
	require.NoError(t, cfg.LoadConfigData([]byte(`
	[[inputs.exec]]
	commands = [ "echo \"a,b\n1,2\n3,4\"" ]
	data_format = "csv"
	csv_header_row_count = 1
`), config.EmptySourcePath))
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
