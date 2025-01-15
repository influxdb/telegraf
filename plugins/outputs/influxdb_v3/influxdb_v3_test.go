package influxdb_v3_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/common/ratelimiter"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/outputs"
	influxdb "github.com/influxdata/telegraf/plugins/outputs/influxdb_v3"
	"github.com/influxdata/telegraf/testutil"
)

func TestSampleConfig(t *testing.T) {
	plugin := influxdb.InfluxDB{}
	require.NotEmpty(t, plugin.SampleConfig())
}

func TestPluginRegistered(t *testing.T) {
	require.Contains(t, outputs.Outputs, "influxdb_v3")
}

func TestCloseWithoutConnect(t *testing.T) {
	plugin := influxdb.InfluxDB{}
	require.NoError(t, plugin.Close())
}

func TestDefaultURL(t *testing.T) {
	plugin := influxdb.InfluxDB{}
	require.NoError(t, plugin.Init())
	require.Len(t, plugin.URLs, 1)
	require.Equal(t, "http://localhost:8181", plugin.URLs[0])
}

func TestInit(t *testing.T) {
	tests := []*influxdb.InfluxDB{
		{
			URLs: []string{"https://localhost:8181"},
			ClientConfig: tls.ClientConfig{
				TLSCA: "thing",
			},
		},
	}

	for _, plugin := range tests {
		t.Run(plugin.URLs[0], func(t *testing.T) {
			require.Error(t, plugin.Init())
		})
	}
}

func TestConnectFail(t *testing.T) {
	tests := []*influxdb.InfluxDB{
		{
			URLs:      []string{"!@#$qwert"},
			HTTPProxy: "http://localhost:8181",
			HTTPHeaders: map[string]string{
				"x": "y",
			},
		},

		{

			URLs:      []string{"http://localhost:1234"},
			HTTPProxy: "!@#$%^&*()_+",
			HTTPHeaders: map[string]string{
				"x": "y",
			},
		},

		{

			URLs:      []string{"!@#$%^&*()_+"},
			HTTPProxy: "http://localhost:8181",
			HTTPHeaders: map[string]string{
				"x": "y",
			},
		},

		{

			URLs:      []string{":::@#$qwert"},
			HTTPProxy: "http://localhost:8181",
			HTTPHeaders: map[string]string{
				"x": "y",
			},
		},
	}

	for _, plugin := range tests {
		t.Run(plugin.URLs[0], func(t *testing.T) {
			require.NoError(t, plugin.Init())
			require.Error(t, plugin.Connect())
		})
	}
}

func TestConnect(t *testing.T) {
	tests := []*influxdb.InfluxDB{
		{
			URLs:      []string{"http://localhost:1234"},
			HTTPProxy: "http://localhost:8181",
			HTTPHeaders: map[string]string{
				"x": "y",
			},
		},
	}

	for _, plugin := range tests {
		t.Run(plugin.URLs[0], func(t *testing.T) {
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Connect())
		})
	}
}

func TestInfluxDBLocalAddress(t *testing.T) {
	t.Log("Starting server")
	server, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer server.Close()

	output := influxdb.InfluxDB{LocalAddr: "localhost"}
	require.NoError(t, output.Connect())
	require.NoError(t, output.Close())
}

func TestWrite(t *testing.T) {
	// Setup a test server
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v3/write_lp":
				if err := r.ParseForm(); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(r.Form["db"], []string{"foobar"}) {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", []string{"foobar"}, r.Form["database"])
					return
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if !strings.Contains(string(body), "cpu value=42.123") {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'body' should contain %q", "cpu value=42.123")
					return
				}

				w.WriteHeader(http.StatusNoContent)
				return
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}),
	)
	defer ts.Close()

	// Setup plugin and connect
	plugin := &influxdb.InfluxDB{
		URLs:               []string{"http://" + ts.Listener.Addr().String()},
		Database:           "telegraf",
		DatabaseTag:        "database",
		ExcludeDatabaseTag: true,
		ContentEncoding:    "identity",
		PingTimeout:        config.Duration(15 * time.Second),
		ReadIdleTimeout:    config.Duration(30 * time.Second),
		Log:                &testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Connect())
	defer plugin.Close()

	// Test writing
	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{
				"database": "foobar",
			},
			map[string]interface{}{
				"value": 42.123,
			},
			time.Unix(0, 0),
		),
	}
	require.NoError(t, plugin.Write(metrics))
	require.NoError(t, plugin.Write(metrics))
}

func TestWriteDatabaseTagWorksOnRetry(t *testing.T) {
	// Setup a test server
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v3/write_lp":
				if err := r.ParseForm(); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(r.Form["db"], []string{"foo"}) {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", []string{"foo"}, r.Form["database"])
					return
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if !strings.Contains(string(body), "cpu value=42") {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'body' should contain %q", "cpu value=42")
					return
				}

				w.WriteHeader(http.StatusNoContent)
				return
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}),
	)
	defer ts.Close()

	// Setup plugin and connect
	plugin := &influxdb.InfluxDB{
		URLs:               []string{"http://" + ts.Listener.Addr().String()},
		Database:           "telegraf",
		DatabaseTag:        "database",
		ExcludeDatabaseTag: true,
		ContentEncoding:    "identity",
		Log:                &testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Connect())
	defer plugin.Close()

	// Send the metrics which should be succeed if sent twice
	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{
				"database": "foo",
			},
			map[string]interface{}{
				"value": 42.0,
			},
			time.Unix(0, 0),
		),
	}
	require.NoError(t, plugin.Write(metrics))
	require.NoError(t, plugin.Write(metrics))
}

func TestTooLargeWriteRetry(t *testing.T) {
	// Setup a test server
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v3/write_lp":
				if err := r.ParseForm(); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				// Ensure metric body size is small
				if len(body) > 16 {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
				} else {
					w.WriteHeader(http.StatusNoContent)
				}

				return
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}),
	)
	defer ts.Close()

	// Setup plugin and connect
	plugin := &influxdb.InfluxDB{
		URLs:               []string{"http://" + ts.Listener.Addr().String()},
		Database:           "telegraf",
		DatabaseTag:        "database",
		ExcludeDatabaseTag: true,
		ContentEncoding:    "identity",
		Log:                &testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Connect())
	defer plugin.Close()

	// Together the metric batch size is too big, split up, we get success
	metrics := []telegraf.Metric{
		metric.New(
			"cpu",
			map[string]string{
				"database": "foo",
			},
			map[string]interface{}{
				"value": 42.0,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"cpu",
			map[string]string{
				"database": "bar",
			},
			map[string]interface{}{
				"value": 99.0,
			},
			time.Unix(0, 0),
		),
	}
	require.NoError(t, plugin.Write(metrics))

	// These metrics are too big, even after splitting in half, expect error
	hugeMetrics := []telegraf.Metric{
		metric.New(
			"reallyLargeMetric",
			map[string]string{
				"database": "foobar",
			},
			map[string]interface{}{
				"value": 123.456,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"evenBiggerMetric",
			map[string]string{
				"database": "fizzbuzzbang",
			},
			map[string]interface{}{
				"value": 999.999,
			},
			time.Unix(0, 0),
		),
	}
	require.Error(t, plugin.Write(hugeMetrics))
}

func TestRateLimit(t *testing.T) {
	// Setup a test server
	var received atomic.Uint64
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v3/write_lp":
				if err := r.ParseForm(); err != nil {
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				received.Add(uint64(len(body)))

				w.WriteHeader(http.StatusNoContent)

				return
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}),
	)
	defer ts.Close()

	// Setup plugin and connect
	plugin := &influxdb.InfluxDB{
		URLs:            []string{"http://" + ts.Listener.Addr().String()},
		Database:        "telegraf",
		ContentEncoding: "identity",
		RateLimitConfig: ratelimiter.RateLimitConfig{
			Limit:  50,
			Period: config.Duration(time.Second),
		},
		Log: &testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Connect())
	defer plugin.Close()

	// Together the metric batch size is too big, split up, we get success
	metrics := []telegraf.Metric{
		metric.New(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"value": 42.0,
			},
			time.Unix(0, 1),
		),
		metric.New(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"value": 99.0,
			},
			time.Unix(0, 2),
		),
		metric.New(
			"operating_hours",
			map[string]string{
				"machine": "A",
			},
			map[string]interface{}{
				"value": 123.456,
			},
			time.Unix(0, 3),
		),
		metric.New(
			"status",
			map[string]string{
				"machine": "B",
			},
			map[string]interface{}{
				"temp":      48.235,
				"remaining": 999.999,
			},
			time.Unix(0, 4),
		),
	}

	// Write the metrics the first time. Only the first two metrics should be
	// received by the server due to the rate limit.
	require.ErrorIs(t, plugin.Write(metrics), internal.ErrSizeLimitReached)
	require.LessOrEqual(t, received.Load(), uint64(30))

	// A direct follow-up write attempt with the remaining metrics should fail
	// due to the rate limit being reached
	require.ErrorIs(t, plugin.Write(metrics[2:]), internal.ErrSizeLimitReached)
	require.LessOrEqual(t, received.Load(), uint64(30))

	// Wait for at least the period (plus some safety margin) to write the third metric
	time.Sleep(time.Duration(plugin.RateLimitConfig.Period) + 100*time.Millisecond)
	require.ErrorIs(t, plugin.Write(metrics[2:]), internal.ErrSizeLimitReached)
	require.Greater(t, received.Load(), uint64(30))
	require.LessOrEqual(t, received.Load(), uint64(72))

	// Wait again for the period for at least the period (plus some safety margin)
	// to write the last metric. This should finally succeed as all metrics
	// are written.
	time.Sleep(time.Duration(plugin.RateLimitConfig.Period) + 100*time.Millisecond)
	require.NoError(t, plugin.Write(metrics[3:]))
	require.Equal(t, uint64(121), received.Load())
}

func TestStatusCodeNonRetryable4xx(t *testing.T) {
	codes := []int{
		// Explicitly checked non-retryable status codes
		http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusNotAcceptable,
		// Other non-retryable 4xx status codes not explicitly checked
		http.StatusNotFound, http.StatusExpectationFailed,
	}

	for _, code := range codes {
		t.Run(fmt.Sprintf("code %d", code), func(t *testing.T) {
			// Setup a test server
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if strings.Contains(string(body), "database=foo") {
						w.WriteHeader(http.StatusOK)
						return
					}
					w.WriteHeader(code)
				}),
			)
			defer ts.Close()

			// Setup plugin and connect
			plugin := &influxdb.InfluxDB{
				URLs:        []string{"http://" + ts.Listener.Addr().String()},
				DatabaseTag: "database",
				Log:         &testutil.Logger{},
			}
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Connect())
			defer plugin.Close()

			// Together the metric batch size is too big, split up, we get success
			metrics := []telegraf.Metric{
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 0),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 1),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 43.0,
					},
					time.Unix(0, 2),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 3),
				),
			}

			// Write the metrics the first time and check for the expected errors
			err := plugin.Write(metrics)
			require.ErrorContains(t, err, "failed to write metric to my_database (will be dropped:")

			var apiErr *influxdb.APIError
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, code, apiErr.StatusCode)

			var writeErr *internal.PartialWriteError
			require.ErrorAs(t, err, &writeErr)
			require.Len(t, writeErr.MetricsReject, 2, "rejected metrics")
		})
	}
}

func TestStatusCodeInvalidAuthentication(t *testing.T) {
	codes := []int{http.StatusUnauthorized, http.StatusForbidden}

	for _, code := range codes {
		t.Run(fmt.Sprintf("code %d", code), func(t *testing.T) {
			// Setup a test server
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if strings.Contains(string(body), "database=foo") {
						w.WriteHeader(http.StatusOK)
						return
					}
					w.WriteHeader(code)
				}),
			)
			defer ts.Close()

			// Setup plugin and connect
			plugin := &influxdb.InfluxDB{
				URLs:        []string{"http://" + ts.Listener.Addr().String()},
				DatabaseTag: "database",
				Log:         &testutil.Logger{},
			}
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Connect())
			defer plugin.Close()

			// Together the metric batch size is too big, split up, we get success
			metrics := []telegraf.Metric{
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 0),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 1),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 43.0,
					},
					time.Unix(0, 2),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 3),
				),
			}

			// Write the metrics the first time and check for the expected errors
			err := plugin.Write(metrics)
			require.ErrorContains(t, err, "failed to write metric to my_database")
			require.ErrorContains(t, err, strconv.Itoa(code))

			var writeErr *internal.PartialWriteError
			require.ErrorAs(t, err, &writeErr)
			require.Empty(t, writeErr.MetricsReject, "rejected metrics")
			require.LessOrEqual(t, len(writeErr.MetricsAccept), 2, "accepted metrics")
		})
	}
}

func TestStatusCodeServiceUnavailable(t *testing.T) {
	codes := []int{
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
		http.StatusBadGateway,
		http.StatusGatewayTimeout,
	}

	for _, code := range codes {
		t.Run(fmt.Sprintf("code %d", code), func(t *testing.T) {
			// Setup a test server
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if strings.Contains(string(body), "database=foo") {
						w.WriteHeader(http.StatusOK)
						return
					}
					w.WriteHeader(code)
				}),
			)
			defer ts.Close()

			// Setup plugin and connect
			plugin := &influxdb.InfluxDB{
				URLs:        []string{"http://" + ts.Listener.Addr().String()},
				DatabaseTag: "database",
				Log:         &testutil.Logger{},
			}
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Connect())
			defer plugin.Close()

			// Together the metric batch size is too big, split up, we get success
			metrics := []telegraf.Metric{
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 0),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 1),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 43.0,
					},
					time.Unix(0, 2),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 3),
				),
			}

			// Write the metrics the first time and check for the expected errors
			err := plugin.Write(metrics)
			require.ErrorContains(t, err, "waiting 25ms for server (my_database) before sending metric again")

			var writeErr *internal.PartialWriteError
			require.ErrorAs(t, err, &writeErr)
			require.Empty(t, writeErr.MetricsReject, "rejected metrics")
			require.LessOrEqual(t, len(writeErr.MetricsAccept), 2, "accepted metrics")
		})
	}
}

func TestStatusCodeUnexpected(t *testing.T) {
	codes := []int{http.StatusInternalServerError}

	for _, code := range codes {
		t.Run(fmt.Sprintf("code %d", code), func(t *testing.T) {
			// Setup a test server
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if strings.Contains(string(body), "database=foo") {
						w.WriteHeader(http.StatusOK)
						return
					}
					w.WriteHeader(code)
				}),
			)
			defer ts.Close()

			// Setup plugin and connect
			plugin := &influxdb.InfluxDB{
				URLs:        []string{"http://" + ts.Listener.Addr().String()},
				DatabaseTag: "database",
				Log:         &testutil.Logger{},
			}
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Connect())
			defer plugin.Close()

			// Together the metric batch size is too big, split up, we get success
			metrics := []telegraf.Metric{
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 0),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 1),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "my_database",
					},
					map[string]interface{}{
						"value": 43.0,
					},
					time.Unix(0, 2),
				),
				metric.New(
					"cpu",
					map[string]string{
						"database": "foo",
					},
					map[string]interface{}{
						"value": 0.0,
					},
					time.Unix(0, 3),
				),
			}

			// Write the metrics the first time and check for the expected errors
			err := plugin.Write(metrics)
			require.ErrorContains(t, err, "failed to write metric to database \"my_database\"")
			require.ErrorContains(t, err, strconv.Itoa(code))

			var writeErr *internal.PartialWriteError
			require.ErrorAs(t, err, &writeErr)
			require.Empty(t, writeErr.MetricsReject, "rejected metrics")
			require.LessOrEqual(t, len(writeErr.MetricsAccept), 2, "accepted metrics")
		})
	}
}
