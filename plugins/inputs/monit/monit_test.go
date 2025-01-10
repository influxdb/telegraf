package monit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

type transportMock struct {
}

func (*transportMock) RoundTrip(*http.Request) (*http.Response, error) {
	errorString := "get http://127.0.0.1:2812/_status?format=xml: " +
		"read tcp 192.168.10.2:55610->127.0.0.1:2812: " +
		"read: connection reset by peer"
	return nil, errors.New(errorString)
}

func TestServiceType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected []telegraf.Metric
	}{
		{
			name:     "check filesystem service type",
			filename: "testdata/response_servicetype_0.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_filesystem",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"mode":                   555,
						"block_percent":          29.5,
						"block_usage":            4424.0,
						"block_total":            14990.0,
						"inode_percent":          0.8,
						"inode_usage":            59674.0,
						"inode_total":            7680000.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check directory service type",
			filename: "testdata/response_servicetype_1.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_directory",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"mode":                   755,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check file service type",
			filename: "testdata/response_servicetype_2.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_file",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"mode":                   644,
						"size":                   1565,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check process service type",
			filename: "testdata/response_servicetype_3.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_process",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"cpu_percent":            0.0,
						"cpu_percent_total":      0.0,
						"mem_kb":                 22892,
						"mem_kb_total":           22892,
						"mem_percent":            0.1,
						"mem_percent_total":      0.1,
						"pid":                    5959,
						"parent_pid":             1,
						"threads":                31,
						"children":               0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check remote host service type",
			filename: "testdata/response_servicetype_4.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_remote_host",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"remote_hostname":        "192.168.1.10",
						"port_number":            2812,
						"request":                "",
						"protocol":               "DEFAULT",
						"type":                   "TCP",
						"response_time":          0.000145,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check system service type",
			filename: "testdata/response_servicetype_5.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_system",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"cpu_system":             0.1,
						"cpu_user":               0.0,
						"cpu_wait":               0.0,
						"cpu_load_avg_1m":        0.00,
						"cpu_load_avg_5m":        0.00,
						"cpu_load_avg_15m":       0.00,
						"mem_kb":                 259668,
						"mem_percent":            1.5,
						"swap_kb":                0.0,
						"swap_percent":           0.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check fifo service type",
			filename: "testdata/response_servicetype_6.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_fifo",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"mode":                   664,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check program service type",
			filename: "testdata/response_servicetype_7.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_program",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"program_status":         0,
						"program_started":        int64(15728504980000000),
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check network service type",
			filename: "testdata/response_servicetype_8.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_network",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"link_speed":             1000000000,
						"link_mode":              "duplex",
						"link_state":             1,
						"download_packets_now":   0,
						"download_packets_total": 15243,
						"download_bytes_now":     0,
						"download_bytes_total":   5506778,
						"download_errors_now":    0,
						"download_errors_total":  0,
						"upload_packets_now":     0,
						"upload_packets_total":   8822,
						"upload_bytes_now":       0,
						"upload_bytes_total":     1287240,
						"upload_errors_now":      0,
						"upload_errors_total":    0,
					},
					time.Unix(0, 0),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/_status":
					http.ServeFile(w, r, tt.filename)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()

			plugin := &Monit{
				Address: ts.URL,
			}

			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator
			require.NoError(t, plugin.Gather(&acc))

			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}

func TestMonitFailure(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected []telegraf.Metric
	}{
		{
			name:     "check monit failure status",
			filename: "testdata/response_servicetype_8_failure.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_network",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "failure",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            8388608,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"link_speed":             -1,
						"link_mode":              "unknown",
						"link_state":             0,
						"download_packets_now":   0,
						"download_packets_total": 0,
						"download_bytes_now":     0,
						"download_bytes_total":   0,
						"download_errors_now":    0,
						"download_errors_total":  0,
						"upload_packets_now":     0,
						"upload_packets_total":   0,
						"upload_bytes_now":       0,
						"upload_bytes_total":     0,
						"upload_errors_now":      0,
						"upload_errors_total":    0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check passive mode",
			filename: "testdata/response_servicetype_8_passivemode.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_network",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "passive",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   1,
						"pending_action_code":    0,
						"link_speed":             1000000000,
						"link_mode":              "duplex",
						"link_state":             1,
						"download_packets_now":   0,
						"download_packets_total": 15243,
						"download_bytes_now":     0,
						"download_bytes_total":   5506778,
						"download_errors_now":    0,
						"download_errors_total":  0,
						"upload_packets_now":     0,
						"upload_packets_total":   8822,
						"upload_bytes_now":       0,
						"upload_bytes_total":     1287240,
						"upload_errors_now":      0,
						"upload_errors_total":    0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check initializing status",
			filename: "testdata/response_servicetype_8_initializingmode.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_network",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "initializing",
						"monitoring_mode":   "active",
						"pending_action":    "none",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 2,
						"monitoring_mode_code":   0,
						"pending_action_code":    0,
						"link_speed":             1000000000,
						"link_mode":              "duplex",
						"link_state":             1,
						"download_packets_now":   0,
						"download_packets_total": 15243,
						"download_bytes_now":     0,
						"download_bytes_total":   5506778,
						"download_errors_now":    0,
						"download_errors_total":  0,
						"upload_packets_now":     0,
						"upload_packets_total":   8822,
						"upload_bytes_now":       0,
						"upload_bytes_total":     1287240,
						"upload_errors_now":      0,
						"upload_errors_total":    0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:     "check pending action",
			filename: "testdata/response_servicetype_8_pendingaction.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"monit_network",
					map[string]string{
						"version":           "5.17.1",
						"source":            "localhost",
						"platform_name":     "Linux",
						"service":           "test",
						"status":            "running",
						"monitoring_status": "monitored",
						"monitoring_mode":   "active",
						"pending_action":    "exec",
					},
					map[string]interface{}{
						"status_code":            0,
						"monitoring_status_code": 1,
						"monitoring_mode_code":   0,
						"pending_action_code":    5,
						"link_speed":             1000000000,
						"link_mode":              "duplex",
						"link_state":             1,
						"download_packets_now":   0,
						"download_packets_total": 15243,
						"download_bytes_now":     0,
						"download_bytes_total":   5506778,
						"download_errors_now":    0,
						"download_errors_total":  0,
						"upload_packets_now":     0,
						"upload_packets_total":   8822,
						"upload_bytes_now":       0,
						"upload_bytes_total":     1287240,
						"upload_errors_now":      0,
						"upload_errors_total":    0,
					},
					time.Unix(0, 0),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/_status":
					http.ServeFile(w, r, tt.filename)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()

			plugin := &Monit{
				Address: ts.URL,
			}

			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator
			require.NoError(t, plugin.Gather(&acc))

			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}

func checkAuth(r *http.Request, username, password string) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return user == username && pass == password
}

func TestAllowHosts(t *testing.T) {
	r := &Monit{
		Address:  "http://127.0.0.1:2812",
		Username: "test",
		Password: "test",
	}

	var acc testutil.Accumulator

	r.client.Transport = &transportMock{}

	err := r.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read: connection reset by peer")
}

func TestConnection(t *testing.T) {
	r := &Monit{
		Address:  "http://127.0.0.1:2812",
		Username: "test",
		Password: "test",
	}

	require.NoError(t, r.Init())

	var acc testutil.Accumulator

	err := r.Gather(&acc)
	require.Error(t, err)

	var urlErr *url.Error
	require.ErrorAs(t, err, &urlErr)
}

func TestInvalidUsernameOrPassword(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, "testing", "testing") {
			http.Error(w, "Unauthorized.", 401)
			return
		}

		if r.URL.Path != "/_status" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "/_status", r.URL.Path)
			return
		}
		http.ServeFile(w, r, "testdata/response_servicetype_0.xml")
	}))

	defer ts.Close()

	r := &Monit{
		Address:  ts.URL,
		Username: "test",
		Password: "test",
	}

	var acc testutil.Accumulator

	require.NoError(t, r.Init())

	err := r.Gather(&acc)
	require.EqualError(t, err, "received status code 401 (Unauthorized), expected 200")
}

func TestNoUsernameOrPasswordConfiguration(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, "testing", "testing") {
			http.Error(w, "Unauthorized.", 401)
			return
		}

		if r.URL.Path != "/_status" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "/_status", r.URL.Path)
			return
		}
		http.ServeFile(w, r, "testdata/response_servicetype_0.xml")
	}))

	defer ts.Close()

	r := &Monit{
		Address: ts.URL,
	}

	var acc testutil.Accumulator

	require.NoError(t, r.Init())

	err := r.Gather(&acc)
	require.EqualError(t, err, "received status code 401 (Unauthorized), expected 200")
}

func TestInvalidXMLAndInvalidTypes(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{
			name:     "check filesystem service type",
			filename: "testdata/response_invalidxml_1.xml",
		},
		{
			name:     "check filesystem service type",
			filename: "testdata/response_invalidxml_2.xml",
		},
		{
			name:     "check filesystem service type",
			filename: "testdata/response_invalidxml_3.xml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/_status":
					http.ServeFile(w, r, tt.filename)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()

			plugin := &Monit{
				Address: ts.URL,
			}

			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator

			err := plugin.Gather(&acc)
			require.Error(t, err)
			require.Contains(t, err.Error(), "error parsing input:")
		})
	}
}
