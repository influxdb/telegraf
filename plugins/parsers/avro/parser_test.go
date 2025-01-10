package avro

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/linkedin/goavro/v2"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/file"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

func TestCases(t *testing.T) {
	// Get all test-case directories
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)
	// Make sure testdata contains data
	require.NotEmpty(t, folders)

	// Set up for file inputs
	inputs.Add("file", func() telegraf.Input {
		return &file.File{}
	})

	for _, f := range folders {
		fname := f.Name()
		testdataPath := filepath.Join("testcases", fname)
		configFilename := filepath.Join(testdataPath, "telegraf.conf")
		expectedFilename := filepath.Join(testdataPath, "expected.out")
		expectedErrorFilename := filepath.Join(testdataPath, "expected.err")

		t.Run(fname, func(t *testing.T) {
			// Get parser to parse expected output
			testdataParser := &influx.Parser{}
			require.NoError(t, testdataParser.Init())

			var expected []telegraf.Metric
			if _, err := os.Stat(expectedFilename); err == nil {
				var err error
				expected, err = testutil.ParseMetricsFromFile(expectedFilename, testdataParser)
				require.NoError(t, err)
			}

			// Read the expected errors if any
			var expectedErrors []string

			if _, err := os.Stat(expectedErrorFilename); err == nil {
				var err error
				expectedErrors, err = testutil.ParseLinesFromFile(expectedErrorFilename)
				require.NoError(t, err)
				require.NotEmpty(t, expectedErrors)
			}

			// Set up error catching
			var acc testutil.Accumulator
			var actualErrors []string
			var actual []telegraf.Metric

			// Configure the plugin
			cfg := config.NewConfig()
			err := cfg.LoadConfig(configFilename)
			require.NoError(t, err)

			for _, input := range cfg.Inputs {
				require.NoError(t, input.Init())

				if err := input.Gather(&acc); err != nil {
					actualErrors = append(actualErrors, err.Error())
				}
			}
			require.ElementsMatch(t, actualErrors, expectedErrors)
			actual = acc.GetTelegrafMetrics()
			// Process expected metrics and compare with resulting metrics
			testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
		})
	}
}

const benchmarkSchema = `
{
	"namespace": "com.benchmark",
	"name": "benchmark",
	"type": "record",
	"version": "1",
	"fields": [
			{"name": "value", "type": "float", "doc": ""},
			{"name": "timestamp", "type": "long", "doc": ""},
			{"name": "tags_platform", "type": "string", "doc": ""},
			{"name": "tags_sdkver", "type": "string", "default": "", "doc": ""},
			{"name": "source", "type": "string", "default": "", "doc": ""}
	]
}
`

func BenchmarkParsing(b *testing.B) {
	plugin := &Parser{
		Format:          "json",
		Measurement:     "benchmark",
		Tags:            []string{"tags_platform", "tags_sdkver", "source"},
		Fields:          []string{"value"},
		Timestamp:       "timestamp",
		TimestampFormat: "unix",
		Schema:          benchmarkSchema,
	}
	require.NoError(b, plugin.Init())

	benchmarkData, err := os.ReadFile(filepath.Join("testcases", "benchmark", "message.json"))
	require.NoError(b, err)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		//nolint:errcheck // Benchmarking so skip the error check to avoid the unnecessary operations
		plugin.Parse(benchmarkData)
	}
}

func TestBenchmarkDataBinary(t *testing.T) {
	plugin := &Parser{
		Measurement:     "benchmark",
		Tags:            []string{"tags_platform", "tags_sdkver", "source"},
		Fields:          []string{"value"},
		Timestamp:       "timestamp",
		TimestampFormat: "unix",
		Schema:          benchmarkSchema,
	}
	require.NoError(t, plugin.Init())

	benchmarkDir := filepath.Join("testcases", "benchmark")

	// Read the expected valued from file
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	expected, err := testutil.ParseMetricsFromFile(filepath.Join(benchmarkDir, "expected.out"), parser)
	require.NoError(t, err)

	// Re-encode the benchmark data from JSON to binary format
	jsonData, err := os.ReadFile(filepath.Join(benchmarkDir, "message.json"))
	require.NoError(t, err)
	codec, err := goavro.NewCodec(benchmarkSchema)
	require.NoError(t, err)
	native, _, err := codec.NativeFromTextual(jsonData)
	require.NoError(t, err)
	benchmarkData, err := codec.BinaryFromNative(nil, native)
	require.NoError(t, err)

	// Do the actual testing
	actual, err := plugin.Parse(benchmarkData)
	require.NoError(t, err)
	testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics())
}

func BenchmarkParsingBinary(b *testing.B) {
	plugin := &Parser{
		Measurement:     "benchmark",
		Tags:            []string{"tags_platform", "tags_sdkver", "source"},
		Fields:          []string{"value"},
		Timestamp:       "timestamp",
		TimestampFormat: "unix",
		Schema:          benchmarkSchema,
	}
	require.NoError(b, plugin.Init())

	// Re-encode the benchmark data from JSON to binary format
	jsonData, err := os.ReadFile(filepath.Join("testcases", "benchmark", "message.json"))
	require.NoError(b, err)
	codec, err := goavro.NewCodec(benchmarkSchema)
	require.NoError(b, err)
	native, _, err := codec.NativeFromTextual(jsonData)
	require.NoError(b, err)
	benchmarkData, err := codec.BinaryFromNative(nil, native)
	require.NoError(b, err)

	for n := 0; n < b.N; n++ {
		//nolint:errcheck // Benchmarking so skip the error check to avoid the unnecessary operations
		plugin.Parse(benchmarkData)
	}
}
