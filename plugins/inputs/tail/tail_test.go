package tail

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/parsers/csv"
	"github.com/influxdata/telegraf/plugins/parsers/grok"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/plugins/parsers/json"
	"github.com/influxdata/telegraf/testutil"
)

func newInfluxParser() (telegraf.Parser, error) {
	parser := &influx.Parser{}
	if err := parser.Init(); err != nil {
		return nil, err
	}
	return parser, nil
}

func NewTestTail() *Tail {
	offsetsMutex.Lock()
	offsetsCopy := make(map[string]int64, len(offsets))
	for k, v := range offsets {
		offsetsCopy[k] = v
	}
	offsetsMutex.Unlock()

	watchMethod := "inotify"
	if runtime.GOOS == "windows" {
		watchMethod = "poll"
	}

	return &Tail{
		FromBeginning:       false,
		MaxUndeliveredLines: 1000,
		offsets:             offsetsCopy,
		WatchMethod:         watchMethod,
		PathTag:             "path",
	}
}

func TestTailBadLine(t *testing.T) {
	content := `
cpu mytag= foo usage_idle= 100
cpu usage_idle=100
`

	tmpfile := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(tmpfile, []byte(content), 0600))

	logger := &testutil.CaptureLogger{}

	tt := NewTestTail()
	tt.Log = logger
	tt.FromBeginning = true
	tt.Files = []string{tmpfile}
	tt.SetParserFunc(newInfluxParser)
	require.NoError(t, tt.Init())

	var acc testutil.Accumulator
	require.NoError(t, tt.Start(&acc))
	require.NoError(t, acc.GatherError(tt.Gather))

	acc.Wait(1)

	tt.Stop()
	require.Len(t, logger.Errors(), 1)
	require.Contains(t, logger.Errors()[0], "Malformed log line")
}

func TestColoredLine(t *testing.T) {
	content := "cpu usage_idle=\033[4A\033[4A100\ncpu2 usage_idle=200\n"

	tmpfile := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(tmpfile, []byte(content), 0600))

	tt := NewTestTail()
	tt.Log = testutil.Logger{}
	tt.FromBeginning = true
	tt.Filters = []string{"ansi_color"}
	tt.Files = []string{tmpfile}
	tt.SetParserFunc(newInfluxParser)
	require.NoError(t, tt.Init())

	var acc testutil.Accumulator
	require.NoError(t, tt.Start(&acc))
	defer tt.Stop()
	require.NoError(t, acc.GatherError(tt.Gather))

	acc.Wait(2)
	acc.AssertContainsFields(t, "cpu",
		map[string]interface{}{
			"usage_idle": float64(100),
		})
	acc.AssertContainsFields(t, "cpu2",
		map[string]interface{}{
			"usage_idle": float64(200),
		})
}

func TestTailDosLineEndings(t *testing.T) {
	content := "cpu usage_idle=100\r\ncpu2 usage_idle=200\r\n"

	tmpfile := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(tmpfile, []byte(content), 0600))

	tt := NewTestTail()
	tt.Log = testutil.Logger{}
	tt.FromBeginning = true
	tt.Files = []string{tmpfile}
	tt.SetParserFunc(newInfluxParser)
	require.NoError(t, tt.Init())

	var acc testutil.Accumulator
	require.NoError(t, tt.Start(&acc))
	defer tt.Stop()
	require.NoError(t, acc.GatherError(tt.Gather))

	acc.Wait(2)
	acc.AssertContainsFields(t, "cpu",
		map[string]interface{}{
			"usage_idle": float64(100),
		})
	acc.AssertContainsFields(t, "cpu2",
		map[string]interface{}{
			"usage_idle": float64(200),
		})
}

func TestGrokParseLogFilesWithMultiline(t *testing.T) {
	// we make sure the timeout won't kick in
	d, err := time.ParseDuration("100s")
	require.NoError(t, err)
	duration := config.Duration(d)
	tt := NewTail()
	tt.Log = testutil.Logger{}
	tt.FromBeginning = true
	tt.Files = []string{filepath.Join("testdata", "test_multiline.log")}
	tt.MultilineConfig = MultilineConfig{
		Pattern:        `^[^\[]`,
		MatchWhichLine: Previous,
		InvertMatch:    false,
		Timeout:        &duration,
	}
	tt.SetParserFunc(createGrokParser)
	require.NoError(t, tt.Init())

	var acc testutil.Accumulator
	require.NoError(t, tt.Start(&acc))
	defer tt.Stop()

	acc.Wait(3)

	expectedPath := filepath.Join("testdata", "test_multiline.log")
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is debug",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "DEBUG",
		})
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is info",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "INFO",
		})
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: Sorry, something wrong! java.lang.ArithmeticException: / by zero\t" +
				"at com.foo.HelloExample2.divide(HelloExample2.java:24)\tat com.foo.HelloExample2.main(HelloExample2.java:14)",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "ERROR",
		})

	require.Equal(t, uint64(3), acc.NMetrics())
}

func TestGrokParseLogFilesWithMultilineTimeout(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "")
	require.NoError(t, err)
	defer tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	// This seems necessary in order to get the test to read the following lines.
	_, err = tmpfile.WriteString("[04/Jun/2016:12:41:48 +0100] INFO HelloExample: This is fluff\r\n")
	require.NoError(t, err)
	require.NoError(t, tmpfile.Sync())

	// set tight timeout for tests
	d := 10 * time.Millisecond
	duration := config.Duration(d)
	tt := NewTail()

	tt.Log = testutil.Logger{}
	tt.FromBeginning = true
	tt.Files = []string{tmpfile.Name()}
	tt.MultilineConfig = MultilineConfig{
		Pattern:        `^[^\[]`,
		MatchWhichLine: Previous,
		InvertMatch:    false,
		Timeout:        &duration,
	}
	tt.SetParserFunc(createGrokParser)
	require.NoError(t, tt.Init())

	var acc testutil.Accumulator
	require.NoError(t, tt.Start(&acc))
	time.Sleep(11 * time.Millisecond) // will force timeout
	_, err = tmpfile.WriteString("[04/Jun/2016:12:41:48 +0100] INFO HelloExample: This is info\r\n")
	require.NoError(t, err)
	require.NoError(t, tmpfile.Sync())
	acc.Wait(2)
	time.Sleep(11 * time.Millisecond) // will force timeout
	_, err = tmpfile.WriteString("[04/Jun/2016:12:41:48 +0100] WARN HelloExample: This is warn\r\n")
	require.NoError(t, err)
	require.NoError(t, tmpfile.Sync())
	acc.Wait(3)
	tt.Stop()
	require.Equal(t, uint64(3), acc.NMetrics())
	expectedPath := tmpfile.Name()

	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is info",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "INFO",
		})
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is warn",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "WARN",
		})
}

func TestGrokParseLogFilesWithMultilineTailerCloseFlushesMultilineBuffer(t *testing.T) {
	// we make sure the timeout won't kick in
	duration := config.Duration(100 * time.Second)

	tt := NewTestTail()
	tt.Log = testutil.Logger{}
	tt.FromBeginning = true
	tt.Files = []string{filepath.Join("testdata", "test_multiline.log")}
	tt.MultilineConfig = MultilineConfig{
		Pattern:        `^[^\[]`,
		MatchWhichLine: Previous,
		InvertMatch:    false,
		Timeout:        &duration,
	}
	tt.SetParserFunc(createGrokParser)
	require.NoError(t, tt.Init())

	var acc testutil.Accumulator
	require.NoError(t, tt.Start(&acc))
	acc.Wait(3)
	require.Equal(t, uint64(3), acc.NMetrics())
	// Close tailer, so multiline buffer is flushed
	tt.Stop()
	acc.Wait(4)

	expectedPath := filepath.Join("testdata", "test_multiline.log")
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is warn",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "WARN",
		})
}

func createGrokParser() (telegraf.Parser, error) {
	parser := &grok.Parser{
		Measurement:        "tail_grok",
		Patterns:           []string{"%{TEST_LOG_MULTILINE}"},
		CustomPatternFiles: []string{filepath.Join("testdata", "test-patterns")},
		Log:                testutil.Logger{},
	}
	err := parser.Init()
	return parser, err
}

// The csv parser should only parse the header line once per file.
func TestCSVHeadersParsedOnce(t *testing.T) {
	content := `
measurement,time_idle
cpu,42
cpu,42
`
	tmpfile := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(tmpfile, []byte(content), 0600))

	plugin := NewTestTail()
	plugin.Log = testutil.Logger{}
	plugin.FromBeginning = true
	plugin.Files = []string{tmpfile}
	plugin.SetParserFunc(func() (telegraf.Parser, error) {
		parser := csv.Parser{
			MeasurementColumn: "measurement",
			HeaderRowCount:    1,
			TimeFunc:          func() time.Time { return time.Unix(0, 0) },
		}
		err := parser.Init()
		return &parser, err
	})
	require.NoError(t, plugin.Init())

	expected := []telegraf.Metric{
		testutil.MustMetric("cpu",
			map[string]string{
				"path": tmpfile,
			},
			map[string]interface{}{
				"time_idle": 42,
			},
			time.Unix(0, 0)),
		testutil.MustMetric("cpu",
			map[string]string{
				"path": tmpfile,
			},
			map[string]interface{}{
				"time_idle": 42,
			},
			time.Unix(0, 0)),
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	require.NoError(t, plugin.Gather(&acc))
	require.Eventuallyf(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(expected), acc.NMetrics())
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics())
}

func TestCSVMultiHeaderWithSkipRowANDColumn(t *testing.T) {
	content := `garbage nonsense
skip,measurement,value
row,1,2
skip1,cpu,42
skip2,mem,100
`
	tmpfile := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(tmpfile, []byte(content), 0600))

	expected := []telegraf.Metric{
		testutil.MustMetric("cpu",
			map[string]string{
				"path": tmpfile,
			},
			map[string]interface{}{
				"value2": 42,
			},
			time.Unix(0, 0)),
		testutil.MustMetric("mem",
			map[string]string{
				"path": tmpfile,
			},
			map[string]interface{}{
				"value2": 100,
			},
			time.Unix(0, 0)),
	}

	plugin := NewTestTail()
	plugin.Log = testutil.Logger{}
	plugin.FromBeginning = true
	plugin.Files = []string{tmpfile}
	plugin.SetParserFunc(func() (telegraf.Parser, error) {
		parser := csv.Parser{
			MeasurementColumn: "measurement1",
			HeaderRowCount:    2,
			SkipRows:          1,
			SkipColumns:       1,
			TimeFunc:          func() time.Time { return time.Unix(0, 0) },
		}
		err := parser.Init()
		return &parser, err
	})
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	require.NoError(t, plugin.Gather(&acc))
	require.Eventuallyf(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(expected), acc.NMetrics())
	plugin.Stop()

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics())
}

// Ensure that the first line can produce multiple metrics (#6138)
func TestMultipleMetricsOnFirstLine(t *testing.T) {
	content := `
[{"time_idle": 42}, {"time_idle": 42}]
`

	tmpfile := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(tmpfile, []byte(content), 0600))

	expected := []telegraf.Metric{
		testutil.MustMetric("cpu",
			map[string]string{
				"customPathTagMyFile": tmpfile,
			},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			time.Unix(0, 0)),
		testutil.MustMetric("cpu",
			map[string]string{
				"customPathTagMyFile": tmpfile,
			},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			time.Unix(0, 0)),
	}

	plugin := NewTestTail()
	plugin.Log = testutil.Logger{}
	plugin.FromBeginning = true
	plugin.Files = []string{tmpfile}
	plugin.PathTag = "customPathTagMyFile"
	plugin.SetParserFunc(func() (telegraf.Parser, error) {
		p := &json.Parser{MetricName: "cpu"}
		err := p.Init()
		return p, err
	})
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	require.NoError(t, plugin.Gather(&acc))
	require.Eventuallyf(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(expected), acc.NMetrics())
	plugin.Stop()

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestCharacterEncoding(t *testing.T) {
	full := []telegraf.Metric{
		testutil.MustMetric("cpu",
			map[string]string{
				"cpu": "cpu0",
			},
			map[string]interface{}{
				"usage_active": 11.9,
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric("cpu",
			map[string]string{
				"cpu": "cpu1",
			},
			map[string]interface{}{
				"usage_active": 26.0,
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric("cpu",
			map[string]string{
				"cpu": "cpu2",
			},
			map[string]interface{}{
				"usage_active": 14.0,
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric("cpu",
			map[string]string{
				"cpu": "cpu3",
			},
			map[string]interface{}{
				"usage_active": 20.4,
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric("cpu",
			map[string]string{
				"cpu": "cpu-total",
			},
			map[string]interface{}{
				"usage_active": 18.4,
			},
			time.Unix(0, 0),
		),
	}

	watchMethod := "inotify"
	if runtime.GOOS == "windows" {
		watchMethod = "poll"
	}

	tests := []struct {
		name              string
		testfiles         string
		fromBeginning     bool
		characterEncoding string
		offset            int64
		expected          []telegraf.Metric
	}{
		{
			name:              "utf-8",
			testfiles:         "cpu-utf-8.influx",
			fromBeginning:     true,
			characterEncoding: "utf-8",
			expected:          full,
		},
		{
			name:              "utf-8 seek",
			testfiles:         "cpu-utf-8.influx",
			characterEncoding: "utf-8",
			offset:            0x33,
			expected:          full[1:],
		},
		{
			name:              "utf-16le",
			testfiles:         "cpu-utf-16le.influx",
			fromBeginning:     true,
			characterEncoding: "utf-16le",
			expected:          full,
		},
		{
			name:              "utf-16le seek",
			testfiles:         "cpu-utf-16le.influx",
			characterEncoding: "utf-16le",
			offset:            0x68,
			expected:          full[1:],
		},
		{
			name:              "utf-16be",
			testfiles:         "cpu-utf-16be.influx",
			fromBeginning:     true,
			characterEncoding: "utf-16be",
			expected:          full,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Tail{
				Files:               []string{filepath.Join("testdata", tt.testfiles)},
				FromBeginning:       tt.fromBeginning,
				MaxUndeliveredLines: 1000,
				Log:                 testutil.Logger{},
				CharacterEncoding:   tt.characterEncoding,
				WatchMethod:         watchMethod,
			}

			plugin.SetParserFunc(newInfluxParser)
			require.NoError(t, plugin.Init())

			if tt.offset != 0 {
				plugin.offsets = map[string]int64{
					plugin.Files[0]: tt.offset,
				}
			}

			var acc testutil.Accumulator
			require.NoError(t, plugin.Start(&acc))
			require.Eventuallyf(t, func() bool {
				return acc.NMetrics() >= uint64(len(tt.expected))
			}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(tt.expected), acc.NMetrics())
			plugin.Stop()

			actual := acc.GetTelegrafMetrics()
			for _, m := range actual {
				m.RemoveTag("path")
			}

			testutil.RequireMetricsEqual(t, tt.expected, actual, testutil.IgnoreTime())
		})
	}
}

func TestTailEOF(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "")
	require.NoError(t, err)
	defer tmpfile.Close()
	defer os.Remove(tmpfile.Name())
	_, err = tmpfile.WriteString("cpu usage_idle=100\r\n")
	require.NoError(t, err)
	require.NoError(t, tmpfile.Sync())

	tt := NewTestTail()
	tt.Log = testutil.Logger{}
	tt.FromBeginning = true
	tt.Files = []string{tmpfile.Name()}
	tt.SetParserFunc(newInfluxParser)
	require.NoError(t, tt.Init())

	var acc testutil.Accumulator
	require.NoError(t, tt.Start(&acc))
	defer tt.Stop()
	require.NoError(t, acc.GatherError(tt.Gather))
	acc.Wait(1) // input hits eof

	_, err = tmpfile.WriteString("cpu2 usage_idle=200\r\n")
	require.NoError(t, err)
	require.NoError(t, tmpfile.Sync())

	acc.Wait(2)
	require.NoError(t, acc.GatherError(tt.Gather))
	acc.AssertContainsFields(t, "cpu",
		map[string]interface{}{
			"usage_idle": float64(100),
		})
	acc.AssertContainsFields(t, "cpu2",
		map[string]interface{}{
			"usage_idle": float64(200),
		})
	require.NoError(t, tmpfile.Close())
}

func TestCSVBehavior(t *testing.T) {
	// Prepare the input file
	input, err := os.CreateTemp("", "")
	require.NoError(t, err)
	defer input.Close()
	defer os.Remove(input.Name())
	// Write header
	_, err = input.WriteString("a,b\n")
	require.NoError(t, err)
	require.NoError(t, input.Sync())

	// Setup the CSV parser creator function
	parserFunc := func() (telegraf.Parser, error) {
		parser := &csv.Parser{
			MetricName:     "tail",
			HeaderRowCount: 1,
		}
		err := parser.Init()
		return parser, err
	}

	// Setup the plugin
	plugin := &Tail{
		Files:               []string{input.Name()},
		FromBeginning:       true,
		MaxUndeliveredLines: 1000,
		offsets:             make(map[string]int64, 0),
		PathTag:             "path",
		Log:                 testutil.Logger{},
	}
	plugin.SetParserFunc(parserFunc)
	require.NoError(t, plugin.Init())

	expected := []telegraf.Metric{
		metric.New(
			"tail",
			map[string]string{
				"path": input.Name(),
			},
			map[string]interface{}{
				"a": int64(1),
				"b": int64(2),
			},
			time.Unix(0, 0),
		),
		metric.New(
			"tail",
			map[string]string{
				"path": input.Name(),
			},
			map[string]interface{}{
				"a": int64(3),
				"b": int64(4),
			},
			time.Unix(0, 0),
		),
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	// Write the first line of data
	_, err = input.WriteString("1,2\n")
	require.NoError(t, err)
	require.NoError(t, input.Sync())
	require.NoError(t, plugin.Gather(&acc))

	// Write another line of data
	_, err = input.WriteString("3,4\n")
	require.NoError(t, err)
	require.NoError(t, input.Sync())
	require.NoError(t, plugin.Gather(&acc))
	require.Eventuallyf(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(expected), acc.NMetrics())

	// Check the result
	options := []cmp.Option{
		testutil.SortMetrics(),
		testutil.IgnoreTime(),
	}
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, options...)

	// Close the input file
	require.NoError(t, input.Close())
}

func TestStatePersistence(t *testing.T) {
	// Prepare the input file
	lines := []string{
		"metric,tag=value foo=1i 1730478201000000000\n",
		"metric,tag=value foo=2i 1730478211000000000\n",
		"metric,tag=value foo=3i 1730478221000000000\n",
	}
	content := []byte(strings.Join(lines, ""))

	inputFilename := filepath.Join(t.TempDir(), "input.influx")
	require.NoError(t, os.WriteFile(inputFilename, content, 0600))

	// Define the metrics and state to skip the first metric
	state := map[string]int64{inputFilename: int64(len(lines[0]))}
	expectedState := map[string]int64{inputFilename: int64(len(content))}
	expected := []telegraf.Metric{
		metric.New("metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 2},
			time.Unix(1730478211, 0),
		),
		metric.New("metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 3},
			time.Unix(1730478221, 0),
		),
	}

	// Configure the plugin
	plugin := &Tail{
		Files:               []string{inputFilename},
		MaxUndeliveredLines: 1000,
		offsets:             make(map[string]int64, 0),
		Log:                 testutil.Logger{},
	}
	plugin.SetParserFunc(newInfluxParser)
	require.NoError(t, plugin.Init())
	require.Empty(t, plugin.offsets)

	// Setup the "persisted" state
	var pi telegraf.StatefulPlugin = plugin
	require.NoError(t, pi.SetState(state))
	require.Len(t, plugin.offsets, 1)

	// Run the plugin
	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	require.NoError(t, plugin.Gather(&acc))
	require.Eventuallyf(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "Expected %d metrics found %d", len(expected), acc.NMetrics())
	plugin.Stop()

	// Check the result
	options := []cmp.Option{
		testutil.SortMetrics(),
		testutil.IgnoreTime(),
	}
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, options...)

	// Check getting the persisted state
	actualState, ok := pi.GetState().(map[string]int64)
	require.True(t, ok, "state is not a map[string]int64")
	require.Equal(t, expectedState, actualState)
}
