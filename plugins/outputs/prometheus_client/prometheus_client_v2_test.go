package prometheus_client

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	inputs "github.com/influxdata/telegraf/plugins/inputs/prometheus"
	"github.com/influxdata/telegraf/plugins/serializers/prometheus"
	"github.com/influxdata/telegraf/testutil"
)

func TestMetricVersion2(t *testing.T) {
	logger := testutil.Logger{Name: "outputs.prometheus_client"}
	tests := []struct {
		name     string
		output   *PrometheusClient
		metrics  []telegraf.Metric
		expected []byte
	}{
		{
			name: "untyped telegraf metric",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle untyped
cpu_time_idle{host="example.org"} 42
`),
		},
		{
			name: "summary no quantiles",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"prometheus",
					map[string]string{},
					map[string]interface{}{
						"rpc_duration_seconds_sum":   1.7560473e+07,
						"rpc_duration_seconds_count": 2693,
					},
					time.Unix(0, 0),
					telegraf.Summary,
				),
			},
			expected: []byte(`
# HELP rpc_duration_seconds Telegraf collected metric
# TYPE rpc_duration_seconds summary
rpc_duration_seconds_sum 1.7560473e+07
rpc_duration_seconds_count 2693
`),
		},
		{
			name: "when export timestamp is true timestamp is present in the metric",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				ExportTimestamp:   true,
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle untyped
cpu_time_idle{host="example.org"} 42 0
`),
		},
		{
			name: "strings as labels",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				StringAsLabel:     true,
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
						"host":      "example.org",
					},
					time.Unix(0, 0),
				),
			},
			expected: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle untyped
cpu_time_idle{host="example.org"} 42
`),
		},
		{
			name: "when strings as labels is false string fields are discarded",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				StringAsLabel:     false,
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
						"host":      "example.org",
					},
					time.Unix(0, 0),
				),
			},
			expected: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle untyped
cpu_time_idle 42
`),
		},
		{
			name: "untype prometheus metric",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"prometheus",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{
						"cpu_time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle untyped
cpu_time_idle{host="example.org"} 42
`),
		},
		{
			name: "telegraf histogram",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"cpu": "cpu1",
					},
					map[string]interface{}{
						"usage_idle_sum":   2000.0,
						"usage_idle_count": 20.0,
					},
					time.Unix(0, 0),
					telegraf.Histogram,
				),
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"cpu": "cpu1",
						"le":  "0.0",
					},
					map[string]interface{}{
						"usage_idle_bucket": 0.0,
					},
					time.Unix(0, 0),
					telegraf.Histogram,
				),
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"cpu": "cpu1",
						"le":  "50.0",
					},
					map[string]interface{}{
						"usage_idle_bucket": 7.0,
					},
					time.Unix(0, 0),
					telegraf.Histogram,
				),
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"cpu": "cpu1",
						"le":  "100.0",
					},
					map[string]interface{}{
						"usage_idle_bucket": 20.0,
					},
					time.Unix(0, 0),
					telegraf.Histogram,
				),
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"cpu": "cpu1",
						"le":  "+Inf",
					},
					map[string]interface{}{
						"usage_idle_bucket": 20.0,
					},
					time.Unix(0, 0),
					telegraf.Histogram,
				),
			},
			expected: []byte(`
# HELP cpu_usage_idle Telegraf collected metric
# TYPE cpu_usage_idle histogram
cpu_usage_idle_bucket{cpu="cpu1",le="0"} 0
cpu_usage_idle_bucket{cpu="cpu1",le="50"} 7
cpu_usage_idle_bucket{cpu="cpu1",le="100"} 20
cpu_usage_idle_bucket{cpu="cpu1",le="+Inf"} 20
cpu_usage_idle_sum{cpu="cpu1"} 2000
cpu_usage_idle_count{cpu="cpu1"} 20
`),
		},
		{
			name: "histogram no buckets",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"cpu": "cpu1",
					},
					map[string]interface{}{
						"usage_idle_sum":   2000.0,
						"usage_idle_count": 20.0,
					},
					time.Unix(0, 0),
					telegraf.Histogram,
				),
			},
			expected: []byte(`
# HELP cpu_usage_idle Telegraf collected metric
# TYPE cpu_usage_idle histogram
cpu_usage_idle_bucket{cpu="cpu1",le="+Inf"} 20
cpu_usage_idle_sum{cpu="cpu1"} 2000
cpu_usage_idle_count{cpu="cpu1"} 20
`),
		},
		{
			name: "untyped forced to counter",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				TypeMappings:      prometheus.MetricTypes{Counter: []string{"cpu_time_idle"}},
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"prometheus",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{
						"cpu_time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
			expected: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle counter
cpu_time_idle{host="example.org"} 42
`),
		},
		{
			name: "untyped forced to gauge",
			output: &PrometheusClient{
				Listen:            ":0",
				MetricVersion:     2,
				CollectorsExclude: []string{"gocollector", "process"},
				Path:              "/metrics",
				TypeMappings:      prometheus.MetricTypes{Gauge: []string{"cpu_time_idle"}},
				Log:               logger,
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"prometheus",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{
						"cpu_time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle gauge
cpu_time_idle{host="example.org"} 42
`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.output.Init())
			require.NoError(t, tt.output.Connect())

			defer func() {
				require.NoError(t, tt.output.Close())
			}()

			require.NoError(t, tt.output.Write(tt.metrics))

			resp, err := http.Get(tt.output.URL())
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			require.Equal(t,
				strings.TrimSpace(string(tt.expected)),
				strings.TrimSpace(string(body)))
		})
	}
}

func TestRoundTripMetricVersion2(t *testing.T) {
	logger := testutil.Logger{Name: "outputs.prometheus_client"}
	regxPattern := regexp.MustCompile(`.*prometheus_request_.*`)
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "untyped",
			data: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle untyped
cpu_time_idle{host="example.org"} 42
`),
		},
		{
			name: "counter",
			data: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle counter
cpu_time_idle{host="example.org"} 42
`),
		},
		{
			name: "gauge",
			data: []byte(`
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle gauge
cpu_time_idle{host="example.org"} 42
`),
		},
		{
			name: "multi",
			data: []byte(`
# HELP cpu_time_guest Telegraf collected metric
# TYPE cpu_time_guest gauge
cpu_time_guest{host="one.example.org"} 42
cpu_time_guest{host="two.example.org"} 42
# HELP cpu_time_idle Telegraf collected metric
# TYPE cpu_time_idle gauge
cpu_time_idle{host="one.example.org"} 42
cpu_time_idle{host="two.example.org"} 42
`),
		},
		{
			name: "histogram",
			data: []byte(`
# HELP http_request_duration_seconds Telegraf collected metric
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.05"} 24054
http_request_duration_seconds_bucket{le="0.1"} 33444
http_request_duration_seconds_bucket{le="0.2"} 100392
http_request_duration_seconds_bucket{le="0.5"} 129389
http_request_duration_seconds_bucket{le="1"} 133988
http_request_duration_seconds_bucket{le="+Inf"} 144320
http_request_duration_seconds_sum 53423
http_request_duration_seconds_count 144320
`),
		},
		{
			name: "summary",
			data: []byte(`
# HELP rpc_duration_seconds Telegraf collected metric
# TYPE rpc_duration_seconds summary
rpc_duration_seconds{quantile="0.01"} 3102
rpc_duration_seconds{quantile="0.05"} 3272
rpc_duration_seconds{quantile="0.5"} 4773
rpc_duration_seconds{quantile="0.9"} 9001
rpc_duration_seconds{quantile="0.99"} 76656
rpc_duration_seconds_sum 1.7560473e+07
rpc_duration_seconds_count 2693
`),
		},
	}

	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	url := fmt.Sprintf("http://%s", ts.Listener.Addr())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write(tt.data); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			})

			input := &inputs.Prometheus{
				Log:           logger,
				URLs:          []string{url},
				URLTag:        "",
				MetricVersion: 2,
			}
			err := input.Init()
			require.NoError(t, err)

			var acc testutil.Accumulator
			err = input.Start(&acc)
			require.NoError(t, err)
			err = input.Gather(&acc)
			require.NoError(t, err)
			input.Stop()

			metrics := acc.GetTelegrafMetrics()

			output := &PrometheusClient{
				Listen:            "127.0.0.1:0",
				Path:              defaultPath,
				MetricVersion:     2,
				Log:               logger,
				CollectorsExclude: []string{"gocollector", "process"},
			}
			err = output.Init()
			require.NoError(t, err)
			err = output.Connect()
			require.NoError(t, err)
			defer func() {
				err = output.Close()
				require.NoError(t, err)
			}()
			err = output.Write(metrics)
			require.NoError(t, err)

			resp, err := http.Get(output.URL())
			require.NoError(t, err)
			defer resp.Body.Close()

			actual, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			current := regxPattern.ReplaceAllLiteralString(string(actual), "")
			require.Equal(t,
				strings.TrimSpace(string(tt.data)),
				strings.TrimSpace(current))
		})
	}
}
