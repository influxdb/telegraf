package inputs_httpjson_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	_ "github.com/influxdata/telegraf/migrations/inputs_httpjson"   // register migration
	httpplugin "github.com/influxdata/telegraf/plugins/inputs/http" // register plugin
	_ "github.com/influxdata/telegraf/plugins/parsers/all"          // register parsers
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

func TestCases(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}

		t.Run(f.Name(), func(t *testing.T) {
			testcasePath := filepath.Join("testcases", f.Name())
			inputFile := filepath.Join(testcasePath, "telegraf.conf")
			expectedFile := filepath.Join(testcasePath, "expected.conf")

			// Read the expected output
			expected := config.NewConfig()
			require.NoError(t, expected.LoadConfig(expectedFile))
			require.NotEmpty(t, expected.Inputs)

			// Read the input data
			input, remote, err := config.LoadConfigFile(inputFile)
			require.NoError(t, err)
			require.False(t, remote)
			require.NotEmpty(t, input)

			// Migrate
			output, n, err := config.ApplyMigrations(input)
			require.NoError(t, err)
			require.NotEmpty(t, output)
			require.GreaterOrEqual(t, n, uint64(1))
			actual := config.NewConfig()
			require.NoError(t, actual.LoadConfigData(output))

			// Test the output
			require.Len(t, actual.Inputs, len(expected.Inputs))
			actualIDs := make([]string, 0, len(expected.Inputs))
			expectedIDs := make([]string, 0, len(expected.Inputs))
			for i := range actual.Inputs {
				actualIDs = append(actualIDs, actual.Inputs[i].ID())
				expectedIDs = append(expectedIDs, expected.Inputs[i].ID())
			}
			require.ElementsMatchf(t, expectedIDs, actualIDs, "generated config: %s", string(output))
		})
	}
}

func TestParsing(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}

		testcasePath := filepath.Join("testcases", f.Name())
		configFile := filepath.Join(testcasePath, "expected.conf")
		inputFile := filepath.Join(testcasePath, "input.json")
		expectedFile := filepath.Join(testcasePath, "output.influx")

		// Skip the testcase if it doesn't provide data
		if _, err := os.Stat(inputFile); errors.Is(err, os.ErrNotExist) {
			continue
		}

		t.Run(f.Name(), func(t *testing.T) {
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())

			// Configure the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFile))
			require.Len(t, cfg.Inputs, 1)
			plugin := cfg.Inputs[0].Input.(*httpplugin.HTTP)

			// Read the input data
			input, err := os.ReadFile(inputFile)
			require.NoError(t, err)
			require.NotEmpty(t, input)

			// Read the expected output
			expected, err := testutil.ParseMetricsFromFile(expectedFile, parser)
			require.NoError(t, err)
			require.NotEmpty(t, expected)

			// Start the test-server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/stats" {
					if _, err = w.Write(input); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						t.Error(err)
						return
					}
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			// Point the plugin to the test-server and start the game
			addr := server.URL + "/stats"
			plugin.URLs = []string{addr}
			require.NoError(t, plugin.Init())
			var acc testutil.Accumulator
			require.NoError(t, plugin.Gather(&acc))

			// Prepare metrics for comparison
			for i := range expected {
				expected[i].AddTag("url", addr)
			}
			raw := acc.GetTelegrafMetrics()
			actual := make([]telegraf.Metric, 0, len(raw))
			for _, m := range raw {
				actual = append(actual, cfg.Inputs[0].MakeMetric(m))
			}

			// Compare
			options := []cmp.Option{
				testutil.IgnoreTime(),
				testutil.IgnoreTags("host"),
			}
			testutil.RequireMetricsEqual(t, expected, actual, options...)
		})
	}
}
