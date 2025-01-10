package loki

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/testutil"
)

func getMetric() telegraf.Metric {
	return testutil.MustMetric(
		"log",
		map[string]string{
			"key1": "value1",
		},
		map[string]interface{}{
			"line":  "my log",
			"field": 3.14,
		},
		time.Unix(123, 0),
	)
}

func getOutOfOrderMetrics() []telegraf.Metric {
	return []telegraf.Metric{
		testutil.MustMetric(
			"log",
			map[string]string{
				"key1": "value1",
			},
			map[string]interface{}{
				"line":  "newer log",
				"field": 3.14,
			},
			time.Unix(1230, 0),
		),
		testutil.MustMetric(
			"log",
			map[string]string{
				"key1": "value1",
			},
			map[string]interface{}{
				"line":  "older log",
				"field": 3.14,
			},
			time.Unix(456, 0),
		),
	}
}

func TestStatusCode(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name       string
		plugin     *Loki
		statusCode int
		errFunc    func(t *testing.T, err error)
	}{
		{
			name: "success",
			plugin: &Loki{
				Domain: u.String(),
			},
			statusCode: http.StatusNoContent,
			errFunc: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "1xx status is an error",
			plugin: &Loki{
				Domain: u.String(),
			},
			statusCode: http.StatusSwitchingProtocols,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "3xx status is an error",
			plugin: &Loki{
				Domain: u.String(),
			},
			statusCode: http.StatusMultipleChoices,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "4xx status is an error",
			plugin: &Loki{
				Domain: u.String(),
			},
			statusCode: http.StatusMultipleChoices,
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

			require.NoError(t, tt.plugin.Connect())

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			tt.errFunc(t, err)
		})
	}
}

func TestContentType(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name     string
		plugin   *Loki
		expected string
	}{
		{
			name: "default is application/json",
			plugin: &Loki{
				Domain: u.String(),
			},
			expected: "application/json",
		},
		{
			name: "overwrite content_type",
			plugin: &Loki{
				Domain:  u.String(),
				Headers: map[string]string{"Content-Type": "plain/text"},
			},
			// plugin force content-type
			expected: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if contentHeader := r.Header.Get("Content-Type"); contentHeader != tt.expected {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", tt.expected, contentHeader)
					return
				}
				w.WriteHeader(http.StatusOK)
			})

			require.NoError(t, tt.plugin.Connect())

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}

func TestContentEncodingGzip(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name     string
		plugin   *Loki
		expected string
	}{
		{
			name: "default is no content encoding",
			plugin: &Loki{
				Domain: u.String(),
			},
			expected: "",
		},
		{
			name: "overwrite content_encoding",
			plugin: &Loki{
				Domain:      u.String(),
				GZipRequest: true,
			},
			expected: "gzip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if contentHeader := r.Header.Get("Content-Encoding"); contentHeader != tt.expected {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", tt.expected, contentHeader)
					return
				}

				body := r.Body
				var err error
				if r.Header.Get("Content-Encoding") == "gzip" {
					body, err = gzip.NewReader(r.Body)
					if err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						t.Error(err)
						return
					}
				}

				payload, err := io.ReadAll(body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				var s Request
				if err = json.Unmarshal(payload, &s); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if len(s.Streams) != 1 {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'s.Streams' should have %d item(s), but has %d", 1, len(s.Streams))
					return
				}
				if len(s.Streams[0].Logs) != 1 {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'s.Streams[0].Logs' should have %d item(s), but has %d", 1, len(s.Streams[0].Logs))
					return
				}
				if len(s.Streams[0].Logs[0]) != 2 {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'s.Streams[0].Logs[0]' should have %d item(s), but has %d", 2, len(s.Streams[0].Logs[0]))
					return
				}
				if !reflect.DeepEqual(s.Streams[0].Labels, map[string]string{"key1": "value1"}) {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", map[string]string{"key1": "value1"}, s.Streams[0].Labels)
					return
				}
				if s.Streams[0].Logs[0][0] != "123000000000" {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", "123000000000", s.Streams[0].Logs[0][0])
					return
				}
				if !strings.Contains(s.Streams[0].Logs[0][1], `line="my log"`) {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'s.Streams[0].Logs[0][1]' should contain %q", `line="my log"`)
					return
				}
				if !strings.Contains(s.Streams[0].Logs[0][1], `field="3.14"`) {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'s.Streams[0].Logs[0][1]' should contain %q", `field="3.14"`)
					return
				}

				w.WriteHeader(http.StatusNoContent)
			})

			require.NoError(t, tt.plugin.Connect())

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}

func TestMetricNameLabel(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name            string
		metricNameLabel string
	}{
		{
			name:            "no label",
			metricNameLabel: "",
		},
		{
			name:            "custom label",
			metricNameLabel: "foobar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				var s Request
				if err := json.Unmarshal(payload, &s); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				switch tt.metricNameLabel {
				case "":
					if !reflect.DeepEqual(s.Streams[0].Labels, map[string]string{"key1": "value1"}) {
						w.WriteHeader(http.StatusInternalServerError)
						t.Errorf("Not equal, expected: %q, actual: %q", map[string]string{"key1": "value1"}, s.Streams[0].Labels)
						return
					}
				case "foobar":
					if !reflect.DeepEqual(s.Streams[0].Labels, map[string]string{"foobar": "log", "key1": "value1"}) {
						w.WriteHeader(http.StatusInternalServerError)
						t.Errorf("Not equal, expected: %q, actual: %q", map[string]string{"foobar": "log", "key1": "value1"}, s.Streams[0].Labels)
						return
					}
				}

				w.WriteHeader(http.StatusNoContent)
			})

			l := Loki{
				Domain:          u.String(),
				MetricNameLabel: tt.metricNameLabel,
			}
			require.NoError(t, l.Connect())
			require.NoError(t, l.Write([]telegraf.Metric{getMetric()}))
		})
	}
}

func TestBasicAuth(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name     string
		username string
		password string
	}{
		{
			name: "default",
		},
		{
			name:     "username and password",
			username: "username",
			password: "pa$$word",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				username, password, _ := r.BasicAuth()
				if username != tt.username {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", tt.username, username)
					return
				}
				if password != tt.password {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", tt.password, password)
					return
				}
				w.WriteHeader(http.StatusOK)
			})

			plugin := &Loki{
				Domain:   u.String(),
				Username: config.NewSecret([]byte(tt.username)),
				Password: config.NewSecret([]byte(tt.password)),
			}
			require.NoError(t, plugin.Connect())

			require.NoError(t, plugin.Write([]telegraf.Metric{getMetric()}))
		})
	}
}

type TestHandlerFunc func(t *testing.T, w http.ResponseWriter, r *http.Request)

func TestOAuthClientCredentialsGrant(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	var token = "2YotnFZFEjr1zCsicMWpAA"

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name         string
		plugin       *Loki
		tokenHandler TestHandlerFunc
		handler      TestHandlerFunc
	}{
		{
			name: "no credentials",
			plugin: &Loki{
				Domain: u.String(),
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Empty(t, r.Header["Authorization"])
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "success",
			plugin: &Loki{
				Domain:       u.String(),
				ClientID:     "howdy",
				ClientSecret: "secret",
				TokenURL:     u.String() + "/token",
				Scopes:       []string{"urn:opc:idm:__myscopes__"},
			},
			tokenHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				values := url.Values{}
				values.Add("access_token", token)
				values.Add("token_type", "bearer")
				values.Add("expires_in", "3600")
				_, err = w.Write([]byte(values.Encode()))
				require.NoError(t, err)
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, []string{"Bearer " + token}, r.Header["Authorization"])
				w.WriteHeader(http.StatusOK)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case defaultEndpoint:
					tt.handler(t, w, r)
				case "/token":
					tt.tokenHandler(t, w, r)
				}
			})

			require.NoError(t, tt.plugin.Connect())

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
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
			if userHeader := r.Header.Get("User-Agent"); userHeader != internal.ProductToken() {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", internal.ProductToken(), userHeader)
				return
			}
			w.WriteHeader(http.StatusOK)
		})

		client := &Loki{
			Domain: u.String(),
		}

		require.NoError(t, client.Connect())

		err = client.Write([]telegraf.Metric{getMetric()})
		require.NoError(t, err)
	})
}

func TestMetricSorting(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	t.Run("out of order metrics", func(t *testing.T) {
		ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := r.Body
			var err error

			payload, err := io.ReadAll(body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}

			var s Request
			if err = json.Unmarshal(payload, &s); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
			if len(s.Streams) != 1 {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("'s.Streams' should have %d item(s), but has %d", 1, len(s.Streams))
				return
			}
			if len(s.Streams[0].Logs) != 2 {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("'s.Streams[0].Logs' should have %d item(s), but has %d", 2, len(s.Streams[0].Logs))
				return
			}
			if len(s.Streams[0].Logs[0]) != 2 {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("'s.Streams[0].Logs[0]' should have %d item(s), but has %d", 2, len(s.Streams[0].Logs[0]))
				return
			}
			if !reflect.DeepEqual(s.Streams[0].Labels, map[string]string{"key1": "value1"}) {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", map[string]string{"key1": "value1"}, s.Streams[0].Labels)
				return
			}
			if s.Streams[0].Logs[0][0] != "456000000000" {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", "456000000000", s.Streams[0].Logs[0][0])
				return
			}
			if !strings.Contains(s.Streams[0].Logs[0][1], `line="older log"`) {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("'s.Streams[0].Logs[0][1]' should contain %q", `line="older log"`)
				return
			}
			if !strings.Contains(s.Streams[0].Logs[0][1], `field="3.14"`) {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("'s.Streams[0].Logs[0][1]' should contain %q", `field="3.14"`)
				return
			}
			if s.Streams[0].Logs[1][0] != "1230000000000" {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", "1230000000000", s.Streams[0].Logs[1][0])
				return
			}
			if !strings.Contains(s.Streams[0].Logs[1][1], `line="newer log"`) {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("'s.Streams[0].Logs[1][1]' should contain %q", `line="newer log"`)
				return
			}
			if !strings.Contains(s.Streams[0].Logs[1][1], `field="3.14"`) {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("'s.Streams[0].Logs[1][1]' should contain %q", `field="3.14"`)
				return
			}

			w.WriteHeader(http.StatusNoContent)
		})

		client := &Loki{
			Domain: u.String(),
		}

		require.NoError(t, client.Connect())

		err = client.Write(getOutOfOrderMetrics())
		require.NoError(t, err)
	})
}

func TestSanitizeLabelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no change",
			input:    "foobar",
			expected: "foobar",
		},
		{
			name:     "replace invalid first character",
			input:    "3foobar",
			expected: "_foobar",
		},
		{
			name:     "replace invalid later character",
			input:    "foobar.foobar",
			expected: "foobar_foobar",
		},
		{
			name:     "numbers allowed later",
			input:    "foobar.foobar.2002",
			expected: "foobar_foobar_2002",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, sanitizeLabelName(tt.input))
		})
	}
}
