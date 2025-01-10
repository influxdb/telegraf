package execd

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/agent"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/plugins/parsers/prometheus"
	serializers_influx "github.com/influxdata/telegraf/plugins/serializers/influx"
	"github.com/influxdata/telegraf/testutil"
)

func TestSettingConfigWorks(t *testing.T) {
	cfg := `
	[[inputs.execd]]
		command = ["a", "b", "c"]
		environment = ["d=e", "f=1"]
		restart_delay = "1m"
		signal = "SIGHUP"
	`
	conf := config.NewConfig()
	require.NoError(t, conf.LoadConfigData([]byte(cfg)))

	require.Len(t, conf.Inputs, 1)
	inp, ok := conf.Inputs[0].Input.(*Execd)
	require.True(t, ok)
	require.EqualValues(t, []string{"a", "b", "c"}, inp.Command)
	require.EqualValues(t, []string{"d=e", "f=1"}, inp.Environment)
	require.EqualValues(t, 1*time.Minute, inp.RestartDelay)
	require.EqualValues(t, "SIGHUP", inp.Signal)
}

func TestExternalInputWorks(t *testing.T) {
	influxParser := models.NewRunningParser(&influx.Parser{}, &models.ParserConfig{})
	require.NoError(t, influxParser.Init())

	exe, err := os.Executable()
	require.NoError(t, err)

	e := &Execd{
		Command:      []string{exe, "-mode", "counter"},
		Environment:  []string{"PLUGINS_INPUTS_EXECD_MODE=application", "METRIC_NAME=counter"},
		RestartDelay: config.Duration(5 * time.Second),
		Signal:       "STDIN",
		Log:          testutil.Logger{},
	}
	e.SetParser(influxParser)

	metrics := make(chan telegraf.Metric, 10)
	defer close(metrics)
	acc := agent.NewAccumulator(&TestMetricMaker{}, metrics)

	require.NoError(t, e.Start(acc))
	require.NoError(t, e.Gather(acc))

	// grab a metric and make sure it's a thing
	m := readChanWithTimeout(t, metrics, 10*time.Second)

	e.Stop()

	require.Equal(t, "counter", m.Name())
	val, ok := m.GetField("count")
	require.True(t, ok)
	require.EqualValues(t, 0, val)
}

func TestParsesLinesContainingNewline(t *testing.T) {
	parser := models.NewRunningParser(&influx.Parser{}, &models.ParserConfig{})
	require.NoError(t, parser.Init())

	metrics := make(chan telegraf.Metric, 10)
	defer close(metrics)
	acc := agent.NewAccumulator(&TestMetricMaker{}, metrics)

	e := &Execd{
		RestartDelay: config.Duration(5 * time.Second),
		Signal:       "STDIN",
		acc:          acc,
		Log:          testutil.Logger{},
	}
	e.SetParser(parser)

	cases := []struct {
		Name  string
		Value string
	}{
		{
			Name:  "no-newline",
			Value: "my message",
		}, {
			Name:  "newline",
			Value: "my\nmessage",
		},
	}

	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			line := fmt.Sprintf("event message=\"%v\" 1587128639239000000", test.Value)

			e.outputReader(strings.NewReader(line))

			m := readChanWithTimeout(t, metrics, 1*time.Second)

			require.Equal(t, "event", m.Name())
			val, ok := m.GetField("message")
			require.True(t, ok)
			require.Equal(t, test.Value, val)
		})
	}
}

func TestParsesPrometheus(t *testing.T) {
	parser := models.NewRunningParser(&prometheus.Parser{}, &models.ParserConfig{})
	require.NoError(t, parser.Init())

	metrics := make(chan telegraf.Metric, 10)
	defer close(metrics)

	var acc testutil.Accumulator

	e := &Execd{
		RestartDelay: config.Duration(5 * time.Second),
		Signal:       "STDIN",
		acc:          &acc,
		Log:          testutil.Logger{},
	}
	e.SetParser(parser)

	lines := `# HELP This is just a test metric.
# TYPE test summary
test{handler="execd",quantile="0.5"} 42.0
`
	expected := []telegraf.Metric{
		testutil.MustMetric(
			"prometheus",
			map[string]string{"handler": "execd", "quantile": "0.5"},
			map[string]interface{}{"test": float64(42.0)},
			time.Unix(0, 0),
		),
	}

	e.outputReader(strings.NewReader(lines))
	check := func() bool { return acc.NMetrics() == uint64(len(expected)) }
	require.Eventually(t, check, 1*time.Second, 100*time.Millisecond)
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func TestStopOnError(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	plugin := &Execd{
		Command:      []string{exe, "-mode", "fail"},
		Environment:  []string{"PLUGINS_INPUTS_EXECD_MODE=application"},
		StopOnError:  true,
		RestartDelay: config.Duration(5 * time.Second),
		Log:          testutil.Logger{},
	}

	parser := models.NewRunningParser(&influx.Parser{}, &models.ParserConfig{})
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	require.Eventually(t, func() bool {
		_, running := plugin.process.State()
		return !running
	}, 3*time.Second, 100*time.Millisecond)

	state, running := plugin.process.State()
	require.False(t, running)
	require.Equal(t, 42, state.ExitCode())
}

func TestStopOnErrorSuccess(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	plugin := &Execd{
		Command:      []string{exe, "-mode", "success"},
		Environment:  []string{"PLUGINS_INPUTS_EXECD_MODE=application"},
		StopOnError:  true,
		RestartDelay: config.Duration(100 * time.Millisecond),
		Log:          testutil.Logger{},
	}

	parser := models.NewRunningParser(&influx.Parser{}, &models.ParserConfig{})
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	// Wait for at least two metric as this indicates the process was restarted
	require.Eventually(t, func() bool {
		return acc.NMetrics() > 1
	}, 3*time.Second, 100*time.Millisecond)
}

func TestLoggingNoPrefix(t *testing.T) {
	// Use own test as mocking executable
	exe, err := os.Executable()
	require.NoError(t, err)

	// Setup the plugin with a capturing logger
	var l testutil.CaptureLogger
	plugin := &Execd{
		Command: []string{exe, "-mode", "logging"},
		Environment: []string{
			"PLUGINS_INPUTS_EXECD_MODE=application",
			"MESSAGE=this is an error",
		},
		Signal:       "STDIN",
		StopOnError:  true,
		RestartDelay: config.Duration(100 * time.Millisecond),
		Log:          &l,
	}

	parser := models.NewRunningParser(&influx.Parser{}, &models.ParserConfig{})
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Run the plugin and trigger a report
	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()
	require.NoError(t, plugin.Gather(&acc))
	plugin.Stop()

	// Wait for at least two metric as this indicates the process was restarted
	require.Eventually(t, func() bool {
		return acc.NMetrics() > 0 && l.NMessages() > 0
	}, 3*time.Second, 100*time.Millisecond)

	// Check the metric
	expected := []telegraf.Metric{
		metric.New("test", map[string]string{}, map[string]interface{}{"value": int64(0)}, time.Unix(0, 0)),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())

	// Check the error message type
	expectedLevel := byte(testutil.LevelError)
	levels := make(map[byte]int, 0)
	for _, m := range l.Messages() {
		if strings.HasPrefix(m.Text, "Starting process") || strings.HasSuffix(m.Text, "shut down") {
			continue
		}
		if m.Level != expectedLevel {
			t.Logf("received msg %q (%s)", m.Text, string(m.Level))
		} else {
			require.Equal(t, "stderr: \"this is an error\"", m.Text)
		}
		levels[m.Level]++
	}
	require.Equal(t, 1, levels[testutil.LevelError])
	require.Len(t, levels, 1)
}

func TestLoggingWithPrefix(t *testing.T) {
	// Use own test as mocking executable
	exe, err := os.Executable()
	require.NoError(t, err)

	tests := []struct {
		name  string
		level byte
	}{
		{"error", testutil.LevelError},
		{"warn", testutil.LevelWarn},
		{"info", testutil.LevelInfo},
		{"debug", testutil.LevelDebug},
		{"trace", testutil.LevelTrace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the plugin with a capturing logger
			var l testutil.CaptureLogger
			plugin := &Execd{
				Command: []string{exe, "-mode", "logging"},
				Environment: []string{
					"PLUGINS_INPUTS_EXECD_MODE=application",
					fmt.Sprintf("MESSAGE=%s! a log message", string(tt.level)),
				},
				Signal:       "STDIN",
				StopOnError:  true,
				RestartDelay: config.Duration(100 * time.Millisecond),
				Log:          &l,
			}

			parser := models.NewRunningParser(&influx.Parser{}, &models.ParserConfig{})
			require.NoError(t, parser.Init())
			plugin.SetParser(parser)

			// Run the plugin and trigger a report
			var acc testutil.Accumulator
			require.NoError(t, plugin.Start(&acc))
			defer plugin.Stop()
			require.NoError(t, plugin.Gather(&acc))
			plugin.Stop()

			// Wait for at least two metric as this indicates the process was restarted
			require.Eventually(t, func() bool {
				return acc.NMetrics() > 0 && l.NMessages() > 0
			}, 3*time.Second, 100*time.Millisecond)

			// Check the metric
			expected := []telegraf.Metric{
				metric.New("test", map[string]string{}, map[string]interface{}{"value": int64(0)}, time.Unix(0, 0)),
			}
			testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())

			// Check the error message type
			expectedLevel := tt.level
			levels := make(map[byte]int, 0)
			for _, m := range l.Messages() {
				if strings.HasPrefix(m.Text, "Starting process") || strings.HasSuffix(m.Text, "shut down") {
					continue
				}
				if m.Level != expectedLevel {
					t.Logf("received msg %q (%s)", m.Text, string(m.Level))
				} else {
					require.Equal(t, "a log message", m.Text)
				}
				levels[m.Level]++
			}
			require.Equal(t, 1, levels[tt.level])
			require.Len(t, levels, 1)
		})
	}
}

func readChanWithTimeout(t *testing.T, metrics chan telegraf.Metric, timeout time.Duration) telegraf.Metric {
	to := time.NewTimer(timeout)
	defer to.Stop()
	select {
	case m := <-metrics:
		return m
	case <-to.C:
		require.FailNow(t, "timeout waiting for metric")
	}
	return nil
}

type TestMetricMaker struct{}

func (*TestMetricMaker) Name() string {
	return "TestPlugin"
}

func (tm *TestMetricMaker) LogName() string {
	return tm.Name()
}

func (*TestMetricMaker) MakeMetric(aMetric telegraf.Metric) telegraf.Metric {
	return aMetric
}

func (*TestMetricMaker) Log() telegraf.Logger {
	return logger.New("TestPlugin", "test", "")
}

func TestMain(m *testing.M) {
	var mode string

	flag.StringVar(&mode, "mode", "counter", "determines the output when run as mockup program")
	flag.Parse()

	operationMode := os.Getenv("PLUGINS_INPUTS_EXECD_MODE")
	if operationMode != "application" {
		// Run the normal test mode
		os.Exit(m.Run())
	}

	// Run as a mock program
	switch mode {
	case "counter":
		if err := runCounterProgram(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "fail":
		os.Exit(42)
	case "success":
		fmt.Println("test value=42i")
		os.Exit(0)
	case "logging":
		if err := runLoggingProgram(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(23)
}

func runCounterProgram() error {
	envMetricName := os.Getenv("METRIC_NAME")
	serializer := &serializers_influx.Serializer{}
	if err := serializer.Init(); err != nil {
		return err
	}

	i := 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		m := metric.New(envMetricName,
			map[string]string{},
			map[string]interface{}{"count": i},
			time.Now(),
		)
		i++

		b, err := serializer.Serialize(m)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR %v\n", err)
			return err
		}
		if _, err := fmt.Fprint(os.Stdout, string(b)); err != nil {
			return err
		}
	}
	return nil
}

func runLoggingProgram() error {
	msg := os.Getenv("MESSAGE")

	i := 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if _, err := fmt.Fprintf(os.Stdout, "test value=%di\n", i); err != nil {
			return err
		}
		if msg != "" {
			if _, err := fmt.Fprintln(os.Stderr, msg); err != nil {
				return err
			}
		}
	}
	return nil
}
