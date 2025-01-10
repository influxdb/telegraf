package opentelemetry

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/influxdata/influxdb-observability/otel2influx"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	otlplogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlpprofiles "go.opentelemetry.io/proto/otlp/collector/profiles/v1experimental"
	otlptrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

func TestOpenTelemetry(t *testing.T) {
	// Setup and start the plugin
	plugin := &OpenTelemetry{
		MetricsSchema: "prometheus-v1",
	}
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	// Setup the OpenTelemetry exporter
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithDialOption(
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return net.Dial("tcp", plugin.listener.Addr().String())
			})),
	)
	require.NoError(t, err)
	defer exporter.Shutdown(ctx) //nolint:errcheck // We cannot do anything if the shutdown fails

	// Setup the metric to send
	reader := metric.NewManualReader()
	defer reader.Shutdown(ctx) //nolint:errcheck // We cannot do anything if the shutdown fails

	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("library-name")
	counter, err := meter.Int64Counter("measurement-counter")
	require.NoError(t, err)
	counter.Add(ctx, 7)

	// Write the OpenTelemetry metrics
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	require.NoError(t, exporter.Export(ctx, &rm))

	// Shutdown
	require.NoError(t, reader.Shutdown(ctx))
	require.NoError(t, exporter.Shutdown(ctx))
	plugin.Stop()

	// Check
	require.Empty(t, acc.Errors)

	var exesuffix string
	if runtime.GOOS == "windows" {
		exesuffix = ".exe"
	}
	expected := []telegraf.Metric{
		testutil.MustMetric(
			"measurement-counter",
			map[string]string{
				"otel.library.name":      "library-name",
				"service.name":           "unknown_service:opentelemetry.test" + exesuffix,
				"telemetry.sdk.language": "go",
				"telemetry.sdk.name":     "opentelemetry",
				"telemetry.sdk.version":  "1.27.0",
			},
			map[string]interface{}{
				"counter": 7,
			},
			time.Unix(0, 0),
			telegraf.Counter,
		),
	}
	options := []cmp.Option{
		testutil.IgnoreTime(),
		testutil.IgnoreFields("start_time_unix_nano"),
		testutil.IgnoreTags("telemetry.sdk.version"),
	}
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, options...)
}

func TestCases(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	// Register the plugin
	inputs.Add("opentelemetry", func() telegraf.Input {
		return &OpenTelemetry{
			ServiceAddress:      "127.0.0.1:0",
			SpanDimensions:      otel2influx.DefaultOtelTracesToLineProtocolConfig().SpanDimensions,
			LogRecordDimensions: otel2influx.DefaultOtelLogsToLineProtocolConfig().LogRecordDimensions,
			ProfileDimensions:   []string{"host.name"},
			Timeout:             config.Duration(5 * time.Second),
		}
	})

	// Prepare the influx parser for expectations
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())

	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}
		testcasePath := filepath.Join("testcases", f.Name())
		configFilename := filepath.Join(testcasePath, "telegraf.conf")
		inputFiles := filepath.Join(testcasePath, "*.json")
		expectedFilename := filepath.Join(testcasePath, "expected.out")
		expectedErrorFilename := filepath.Join(testcasePath, "expected.err")

		// Compare options
		options := []cmp.Option{
			testutil.IgnoreTime(),
			testutil.SortMetrics(),
			testutil.IgnoreFields("start_time_unix_nano"),
		}

		t.Run(f.Name(), func(t *testing.T) {
			// Read the input data
			inputs := make(map[string][][]byte)
			matches, err := filepath.Glob(inputFiles)
			require.NoError(t, err)
			require.NotEmpty(t, matches)
			sort.Strings(matches)
			for _, fn := range matches {
				buf, err := os.ReadFile(fn)
				require.NoError(t, err)

				key := strings.TrimSuffix(filepath.Base(fn), ".json")
				key, _, _ = strings.Cut(key, "_")
				inputs[key] = append(inputs[key], buf)
			}

			// Read the expected output if any
			var expected []telegraf.Metric
			if _, err := os.Stat(expectedFilename); err == nil {
				var err error
				expected, err = testutil.ParseMetricsFromFile(expectedFilename, parser)
				require.NoError(t, err)
			}

			// Read the expected output if any
			var expectedErrors []string
			if _, err := os.Stat(expectedErrorFilename); err == nil {
				var err error
				expectedErrors, err = testutil.ParseLinesFromFile(expectedErrorFilename)
				require.NoError(t, err)
				require.NotEmpty(t, expectedErrors)
			}

			// Configure the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFilename))
			require.Len(t, cfg.Inputs, 1)

			// Setup and start the plugin
			plugin := cfg.Inputs[0].Input.(*OpenTelemetry)
			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator
			require.NoError(t, plugin.Start(&acc))
			defer plugin.Stop()

			// Send all data to the plugin
			addr := plugin.listener.Addr().String()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			grpcClient, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			defer grpcClient.Close()
			for msgtype, messages := range inputs {
				switch msgtype {
				case "logs":
					client := otlplogs.NewLogsServiceClient(grpcClient)
					for _, buf := range messages {
						var msg otlplogs.ExportLogsServiceRequest
						require.NoError(t, protojson.Unmarshal(buf, &msg))
						_, err := client.Export(ctx, &msg)
						require.NoError(t, err)
					}
				case "metrics":
					client := otlpmetrics.NewMetricsServiceClient(grpcClient)
					for _, buf := range messages {
						var msg otlpmetrics.ExportMetricsServiceRequest
						require.NoError(t, protojson.Unmarshal(buf, &msg))
						_, err := client.Export(ctx, &msg)
						require.NoError(t, err)
					}
				case "profiles":
					client := otlpprofiles.NewProfilesServiceClient(grpcClient)
					for _, buf := range messages {
						var msg otlpprofiles.ExportProfilesServiceRequest
						require.NoError(t, protojson.Unmarshal(buf, &msg))
						_, err := client.Export(ctx, &msg)
						require.NoError(t, err)
					}
				case "traces":
					client := otlptrace.NewTraceServiceClient(grpcClient)
					for _, buf := range messages {
						var msg otlptrace.ExportTraceServiceRequest
						require.NoError(t, protojson.Unmarshal(buf, &msg))
						_, err := client.Export(ctx, &msg)
						require.NoError(t, err)
					}
				}
			}

			// Close the plugin to make sure all data is flushed
			require.NoError(t, grpcClient.Close())
			plugin.Stop()

			// Check the metrics
			require.Eventually(t, func() bool {
				return acc.NMetrics() >= uint64(len(expected))
			}, 3*time.Second, 100*time.Millisecond)
			require.Empty(t, acc.Errors)

			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, options...)
		})
	}
}
