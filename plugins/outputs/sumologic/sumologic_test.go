package sumologic

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/serializers/carbon2"
	"github.com/influxdata/telegraf/plugins/serializers/graphite"
	"github.com/influxdata/telegraf/plugins/serializers/prometheus"
	"github.com/influxdata/telegraf/testutil"
)

func getMetric() telegraf.Metric {
	m := metric.New(
		"cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	return m
}

func getMetrics() []telegraf.Metric {
	const count = 100
	var metrics = make([]telegraf.Metric, count)

	for i := 0; i < count; i++ {
		m := metric.New(
			fmt.Sprintf("cpu-%d", i),
			map[string]string{
				"ec2_instance": "aws-129038123",
				"image":        "aws-ami-1234567890",
			},
			map[string]interface{}{
				"idle":   5876876,
				"steal":  5876876,
				"system": 5876876,
				"user":   5876876,
				"temp":   70.0,
			},
			time.Unix(0, 0),
		)
		metrics[i] = m
	}
	return metrics
}

func TestMethod(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name           string
		plugin         func() *SumoLogic
		expectedMethod string
		connectError   bool
	}{
		{
			name: "default method is POST",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				return s
			},
			expectedMethod: http.MethodPost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.expectedMethod {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", tt.expectedMethod, r.Method)
					return
				}
				w.WriteHeader(http.StatusOK)
			})

			serializer := &carbon2.Serializer{
				Format: "field_separate",
			}
			require.NoError(t, serializer.Init())

			plugin := tt.plugin()
			plugin.SetSerializer(serializer)
			err = plugin.Connect()
			if tt.connectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			err = plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}

func TestStatusCode(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	pluginFn := func() *SumoLogic {
		s := Default()
		s.URL = u.String()
		return s
	}

	tests := []struct {
		name       string
		plugin     *SumoLogic
		statusCode int
		errFunc    func(t *testing.T, err error)
	}{
		{
			name:       "success",
			plugin:     pluginFn(),
			statusCode: http.StatusOK,
			errFunc: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:       "1xx status is an error",
			plugin:     pluginFn(),
			statusCode: http.StatusSwitchingProtocols,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name:       "3xx status is an error",
			plugin:     pluginFn(),
			statusCode: http.StatusMultipleChoices,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name:       "4xx status is an error",
			plugin:     pluginFn(),
			statusCode: http.StatusBadRequest,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			serializer := &carbon2.Serializer{
				Format: "field_separate",
			}
			require.NoError(t, serializer.Init())

			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			require.NoError(t, err)

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			tt.errFunc(t, err)
		})
	}
}

func TestContentType(t *testing.T) {
	tests := []struct {
		name         string
		plugin       func() *SumoLogic
		expectedBody []byte
	}{
		{
			name: "carbon2 (data format = field separate) is supported",
			plugin: func() *SumoLogic {
				s := Default()
				s.headers = map[string]string{
					contentTypeHeader: carbon2ContentType,
				}
				serializer := &carbon2.Serializer{
					Format: "field_separate",
				}
				require.NoError(t, serializer.Init())
				s.SetSerializer(serializer)
				return s
			},
			expectedBody: []byte("metric=cpu field=value  42 0\n"),
		},
		{
			name: "carbon2 (data format = metric includes field) is supported",
			plugin: func() *SumoLogic {
				s := Default()
				s.headers = map[string]string{
					contentTypeHeader: carbon2ContentType,
				}
				serializer := &carbon2.Serializer{
					Format: "metric_includes_field",
				}
				require.NoError(t, serializer.Init())
				s.SetSerializer(serializer)
				return s
			},
			expectedBody: []byte("metric=cpu_value  42 0\n"),
		},
		{
			name: "graphite is supported",
			plugin: func() *SumoLogic {
				s := Default()
				s.headers = map[string]string{
					contentTypeHeader: graphiteContentType,
				}
				serializer := &graphite.GraphiteSerializer{}
				require.NoError(t, serializer.Init())
				s.SetSerializer(serializer)
				return s
			},
		},
		{
			name: "prometheus is supported",
			plugin: func() *SumoLogic {
				s := Default()
				s.headers = map[string]string{
					contentTypeHeader: prometheusContentType,
				}
				s.SetSerializer(&prometheus.Serializer{})
				return s
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gz, err := gzip.NewReader(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				var maxDecompressionSize int64 = 500 * 1024 * 1024
				n, err := io.CopyN(&body, gz, maxDecompressionSize)
				if errors.Is(err, io.EOF) {
					err = nil
				}
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if n > maxDecompressionSize {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Size of decoded data exceeds (%v) allowed size (%v)", n, maxDecompressionSize)
					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			u, err := url.Parse("http://" + ts.Listener.Addr().String())
			require.NoError(t, err)

			plugin := tt.plugin()
			plugin.URL = u.String()

			require.NoError(t, plugin.Connect())

			err = plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)

			if tt.expectedBody != nil {
				require.Equal(t, string(tt.expectedBody), body.String())
			}
		})
	}
}

func TestContentEncodingGzip(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name   string
		plugin func() *SumoLogic
	}{
		{
			name: "default content_encoding=gzip works",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				return s
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Content-Encoding") != "gzip" {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", "gzip", r.Header.Get("Content-Encoding"))
					return
				}

				body, err := gzip.NewReader(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				payload, err := io.ReadAll(body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if "metric=cpu field=value  42 0\n" != string(payload) {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", "metric=cpu field=value  42 0\n", string(payload))
					return
				}

				w.WriteHeader(http.StatusNoContent)
			})

			serializer := &carbon2.Serializer{
				Format: "field_separate",
			}
			require.NoError(t, serializer.Init())

			plugin := tt.plugin()

			plugin.SetSerializer(serializer)
			err = plugin.Connect()
			require.NoError(t, err)

			err = plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}

func TestDefaultUserAgent(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	t.Run("default-user-agent", func(t *testing.T) {
		ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") != internal.ProductToken() {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", internal.ProductToken(), r.Header.Get("User-Agent"))
				return
			}
			w.WriteHeader(http.StatusOK)
		})

		plugin := &SumoLogic{
			URL:                u.String(),
			MaxRequestBodySize: Default().MaxRequestBodySize,
		}

		serializer := &carbon2.Serializer{
			Format: "field_separate",
		}
		require.NoError(t, serializer.Init())

		plugin.SetSerializer(serializer)
		err = plugin.Connect()
		require.NoError(t, err)

		err = plugin.Write([]telegraf.Metric{getMetric()})
		require.NoError(t, err)
	})
}

func TestTOMLConfig(t *testing.T) {
	testcases := []struct {
		name          string
		configBytes   []byte
		expectedError bool
	}{
		{
			name: "carbon2 content type is supported",
			configBytes: []byte(`
[[outputs.sumologic]]
  url = "https://localhost:3000"
  data_format = "carbon2"
            `),
			expectedError: false,
		},
		{
			name: "graphite content type is supported",
			configBytes: []byte(`
[[outputs.sumologic]]
  url = "https://localhost:3000"
  data_format = "graphite"
            `),
			expectedError: false,
		},
		{
			name: "prometheus content type is supported",
			configBytes: []byte(`
[[outputs.sumologic]]
  url = "https://localhost:3000"
  data_format = "prometheus"
            `),
			expectedError: false,
		},
		{
			name: "setting extra headers is not possible",
			configBytes: []byte(`
[[outputs.sumologic]]
  url = "https://localhost:3000"
  data_format = "carbon2"
  [outputs.sumologic.headers]
    X-Sumo-Name = "dummy"
    X-Sumo-Host = "dummy"
    X-Sumo-Category  = "dummy"
    X-Sumo-Dimensions = "dummy"
            `),
			expectedError: true,
		},
		{
			name: "full example from sample config is correct",
			configBytes: []byte(`
[[outputs.sumologic]]
  url = "https://localhost:3000"
  data_format = "carbon2"
  timeout = "5s"
  source_name = "name"
  source_host = "hosta"
  source_category = "category"
  dimensions = "dimensions"
            `),
			expectedError: false,
		},
		{
			name: "unknown key - sumo_metadata - in config fails",
			configBytes: []byte(`
[[outputs.sumologic]]
  url = "https://localhost:3000"
  data_format = "carbon2"
  timeout = "5s"
  source_name = "name"
  sumo_metadata = "metadata"
            `),
			expectedError: true,
		},
	}
	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			c := config.NewConfig()

			if tt.expectedError {
				require.Error(t, c.LoadConfigData(tt.configBytes))
			} else {
				require.NoError(t, c.LoadConfigData(tt.configBytes))
			}
		})
	}
}

func TestMaxRequestBodySize(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	testcases := []struct {
		name                     string
		plugin                   func() *SumoLogic
		metrics                  []telegraf.Metric
		expectedError            bool
		expectedRequestCount     int32
		expectedMetricLinesCount int32
	}{
		{
			name: "default max request body size is 1MB and doesn't split small enough metric slices",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				return s
			},
			metrics:                  []telegraf.Metric{getMetric()},
			expectedError:            false,
			expectedRequestCount:     1,
			expectedMetricLinesCount: 1,
		},
		{
			name: "default max request body size is 1MB and doesn't split small even medium sized metrics",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     1,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
		{
			name: "when short by at least 1B the request is split",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				// getMetrics returns metrics that serialized (using carbon2),
				// uncompressed size is 43750B
				s.MaxRequestBodySize = 43_749
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     2,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
		{
			name: "max request body size properly splits requests - max 10_000",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				s.MaxRequestBodySize = 10_000
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     5,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
		{
			name: "max request body size properly splits requests - max 5_000",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				s.MaxRequestBodySize = 5_000
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     10,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
		{
			name: "max request body size properly splits requests - max 2_500",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				s.MaxRequestBodySize = 2_500
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     20,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
		{
			name: "max request body size properly splits requests - max 1_000",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				s.MaxRequestBodySize = 1_000
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     50,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
		{
			name: "max request body size properly splits requests - max 500",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				s.MaxRequestBodySize = 500
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     100,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
		{
			name: "max request body size properly splits requests - max 300",
			plugin: func() *SumoLogic {
				s := Default()
				s.URL = u.String()
				s.MaxRequestBodySize = 300
				return s
			},
			metrics:                  getMetrics(),
			expectedError:            false,
			expectedRequestCount:     100,
			expectedMetricLinesCount: 500, // count (100) metrics, 5 lines per each (steal, idle, system, user, temp) = 500
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			var (
				requestCount int32
				linesCount   int32
			)
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)

				if tt.expectedMetricLinesCount != 0 {
					atomic.AddInt32(&linesCount, int32(countLines(t, r.Body)))
				}

				w.WriteHeader(http.StatusOK)
			})

			serializer := &carbon2.Serializer{
				Format: "field_separate",
			}
			require.NoError(t, serializer.Init())

			plugin := tt.plugin()
			plugin.SetSerializer(serializer)
			plugin.Log = testutil.Logger{}

			err = plugin.Connect()
			require.NoError(t, err)

			err = plugin.Write(tt.metrics)
			if tt.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedRequestCount, atomic.LoadInt32(&requestCount))
				require.Equal(t, tt.expectedMetricLinesCount, atomic.LoadInt32(&linesCount))
			}
		})
	}
}

func TestTryingToSendEmptyMetricsDoesntFail(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	metrics := make([]telegraf.Metric, 0)
	plugin := Default()
	plugin.URL = u.String()

	serializer := &carbon2.Serializer{
		Format: "field_separate",
	}
	require.NoError(t, serializer.Init())
	plugin.SetSerializer(serializer)

	err = plugin.Connect()
	require.NoError(t, err)

	err = plugin.Write(metrics)
	require.NoError(t, err)
}

func countLines(t *testing.T, body io.Reader) int {
	// All requests coming from Sumo Logic output plugin are gzipped.
	gz, err := gzip.NewReader(body)
	require.NoError(t, err)

	var linesCount int
	for s := bufio.NewScanner(gz); s.Scan(); {
		linesCount++
	}

	return linesCount
}
