package influxdb_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/outputs/influxdb"
	"github.com/influxdata/telegraf/testutil"
)

func getHTTPURL() *url.URL {
	u, err := url.Parse("http://localhost")
	if err != nil {
		panic(err)
	}
	return u
}

func TestHTTP_EmptyConfig(t *testing.T) {
	cfg := influxdb.HTTPConfig{}
	_, err := influxdb.NewHTTPClient(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), influxdb.ErrMissingURL.Error())
}

func TestHTTP_MinimalConfig(t *testing.T) {
	cfg := influxdb.HTTPConfig{
		URL: getHTTPURL(),
	}
	_, err := influxdb.NewHTTPClient(cfg)
	require.NoError(t, err)
}

func TestHTTP_UnsupportedScheme(t *testing.T) {
	cfg := influxdb.HTTPConfig{
		URL: &url.URL{
			Scheme: "foo",
			Host:   "localhost",
		},
	}
	_, err := influxdb.NewHTTPClient(cfg)
	require.Error(t, err)
}

func TestHTTP_CreateDatabase(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	successResponse := []byte(`{"results": [{"statement_id": 0}]}`)

	tests := []struct {
		name             string
		config           influxdb.HTTPConfig
		database         string
		queryHandlerFunc func(t *testing.T, w http.ResponseWriter, r *http.Request)
		errFunc          func(t *testing.T, err error)
	}{
		{
			name: "success",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "xyzzy",
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, `CREATE DATABASE "xyzzy"`, r.FormValue("q"))
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
		},
		{
			name: "send basic auth",
			config: influxdb.HTTPConfig{
				URL:      u,
				Username: config.NewSecret([]byte("guy")),
				Password: config.NewSecret([]byte("smiley")),
				Database: "telegraf",
			},
			database: "telegraf",
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				require.True(t, ok)
				require.Equal(t, "guy", username)
				require.Equal(t, "smiley", password)
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
		},
		{
			name: "send user agent",
			config: influxdb.HTTPConfig{
				URL: u,
				Headers: map[string]string{
					"A": "B",
					"C": "D",
				},
				Database: "telegraf",
			},
			database: `a " b`,
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "B", r.Header.Get("A"))
				require.Equal(t, "D", r.Header.Get("C"))
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
		},
		{
			name: "send headers",
			config: influxdb.HTTPConfig{
				URL: u,
				Headers: map[string]string{
					"A": "B",
					"C": "D",
				},
				Database: "telegraf",
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "B", r.Header.Get("A"))
				require.Equal(t, "D", r.Header.Get("C"))
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
		},
		{
			name: "database default",
			config: influxdb.HTTPConfig{
				URL: u,
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, `CREATE DATABASE "telegraf"`, r.FormValue("q"))
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
		},
		{
			name: "database name is escaped",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: `a " b`,
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, `CREATE DATABASE "a \" b"`, r.FormValue("q"))
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
		},
		{
			name: "invalid database name creates api error",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: `a \\ b`,
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				// Yes, 200 OK is the correct response...
				w.WriteHeader(http.StatusOK)
				_, err = w.Write([]byte(`{"results": [{"error": "invalid name", "statement_id": 0}]}`))
				require.NoError(t, err)
			},
			errFunc: func(t *testing.T, err error) {
				expected := &influxdb.APIError{
					StatusCode:  200,
					Title:       "200 OK",
					Description: "invalid name",
				}

				require.Equal(t, expected, err)
			},
		},
		{
			name: "error with no response body",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
			},
			queryHandlerFunc: func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			errFunc: func(t *testing.T, err error) {
				expected := &influxdb.APIError{
					StatusCode: 404,
					Title:      "404 Not Found",
				}

				require.Equal(t, expected, err)
			},
		},
		{
			name: "ok with no response body",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
			},
			queryHandlerFunc: func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "invalid json response is handled",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: `database`,
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, err = w.Write([]byte(`invalid response`))
				require.NoError(t, err)
			},
			errFunc: func(t *testing.T, err error) {
				expected := &influxdb.APIError{
					StatusCode:  400,
					Title:       "400 Bad Request",
					Description: "An error response was received while attempting to create the following database: database. Error: invalid response",
				}

				require.Equal(t, expected, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/query":
					tt.queryHandlerFunc(t, w, r)
					return
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
			})

			ctx := context.Background()

			client, err := influxdb.NewHTTPClient(tt.config)
			require.NoError(t, err)
			err = client.CreateDatabase(ctx, client.Database())
			if tt.errFunc != nil {
				tt.errFunc(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHTTP_Write(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name             string
		config           influxdb.HTTPConfig
		queryHandlerFunc func(t *testing.T, w http.ResponseWriter, r *http.Request)
		errFunc          func(t *testing.T, err error)
		logFunc          func(t *testing.T, str string)
	}{
		{
			name: "success",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "cpu value=42")
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "send basic auth",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Username: config.NewSecret([]byte("guy")),
				Password: config.NewSecret([]byte("smiley")),
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				require.True(t, ok)
				require.Equal(t, "guy", username)
				require.Equal(t, "smiley", password)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "send user agent",
			config: influxdb.HTTPConfig{
				URL:       u,
				Database:  "telegraf",
				UserAgent: "telegraf",
				Log:       testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.Header.Get("User-Agent"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "default user agent",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, internal.ProductToken(), r.Header.Get("User-Agent"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "default database",
			config: influxdb.HTTPConfig{
				URL: u,
				Log: testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "send headers",
			config: influxdb.HTTPConfig{
				URL: u,
				Headers: map[string]string{
					"A": "B",
					"C": "D",
				},
				Log: testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "B", r.Header.Get("A"))
				require.Equal(t, "D", r.Header.Get("C"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "send retention policy",
			config: influxdb.HTTPConfig{
				URL:             u,
				Database:        "telegraf",
				RetentionPolicy: "foo",
				Log:             testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "foo", r.FormValue("rp"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "send consistency",
			config: influxdb.HTTPConfig{
				URL:         u,
				Database:    "telegraf",
				Consistency: "all",
				Log:         testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "all", r.FormValue("consistency"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "hinted handoff not empty no error",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, err = w.Write([]byte(`{"error": "write failed: hinted handoff queue not empty"}`))
				require.NoError(t, err)
			},
			errFunc: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "partial write errors are logged no error",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, err = w.Write([]byte(`{"error": "partial write: field type conflict:"}`))
				require.NoError(t, err)
			},
			logFunc: func(t *testing.T, str string) {
				require.Contains(t, str, "partial write")
			},
		},
		{
			name: "parse errors are logged no error",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, err = w.Write([]byte(`{"error": "unable to parse 'cpu value': invalid field format"}`))
				require.NoError(t, err)
			},
			logFunc: func(t *testing.T, str string) {
				require.Contains(t, str, "unable to parse")
			},
		},
		{
			name: "http error",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			errFunc: func(t *testing.T, err error) {
				expected := &influxdb.APIError{
					StatusCode: 502,
					Title:      "502 Bad Gateway",
				}
				require.Equal(t, expected, err)
			},
		},
		{
			name: "http error with desc",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, err = w.Write([]byte(`{"error": "unknown error"}`))
				require.NoError(t, err)
			},
			errFunc: func(t *testing.T, err error) {
				expected := &influxdb.APIError{
					StatusCode:  503,
					Title:       "503 Service Unavailable",
					Description: "unknown error",
				}
				require.Equal(t, expected, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/write":
					tt.queryHandlerFunc(t, w, r)
					return
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
			})

			var b bytes.Buffer
			if tt.logFunc != nil {
				log.SetOutput(&b)
			}

			ctx := context.Background()

			m := metric.New(
				"cpu",
				map[string]string{},
				map[string]interface{}{
					"value": 42.0,
				},
				time.Unix(0, 0),
			)
			metrics := []telegraf.Metric{m}

			client, err := influxdb.NewHTTPClient(tt.config)
			require.NoError(t, err)
			err = client.Write(ctx, metrics)
			if tt.errFunc != nil {
				tt.errFunc(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.logFunc != nil {
				tt.logFunc(t, b.String())
			}
		})
	}
}

func TestHTTP_WritePathPrefix(t *testing.T) {
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/x/y/z/query":
				w.WriteHeader(http.StatusOK)
				return
			case "/x/y/z/write":
				w.WriteHeader(http.StatusNoContent)
				return
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
		},
		),
	)
	defer ts.Close()

	u, err := url.Parse(fmt.Sprintf("http://%s/x/y/z", ts.Listener.Addr().String()))
	require.NoError(t, err)

	ctx := context.Background()

	m := metric.New(
		"cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	metrics := []telegraf.Metric{m}

	cfg := influxdb.HTTPConfig{
		URL:      u,
		Database: "telegraf",
		Log:      testutil.Logger{},
	}

	client, err := influxdb.NewHTTPClient(cfg)
	require.NoError(t, err)
	err = client.CreateDatabase(ctx, cfg.Database)
	require.NoError(t, err)
	err = client.Write(ctx, metrics)
	require.NoError(t, err)
}

func TestHTTP_WriteContentEncodingGzip(t *testing.T) {
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/write":
				if contentHeader := r.Header.Get("Content-Encoding"); contentHeader != "gzip" {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", "gzip", contentHeader)
					return
				}

				gr, err := gzip.NewReader(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}

				body, err := io.ReadAll(gr)
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
		},
		),
	)
	defer ts.Close()

	u, err := url.Parse(fmt.Sprintf("http://%s/", ts.Listener.Addr().String()))
	require.NoError(t, err)

	ctx := context.Background()

	m := metric.New(
		"cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	require.NoError(t, err)
	metrics := []telegraf.Metric{m}

	cfg := influxdb.HTTPConfig{
		URL:             u,
		Database:        "telegraf",
		ContentEncoding: "gzip",
		Log:             testutil.Logger{},
	}

	client, err := influxdb.NewHTTPClient(cfg)
	require.NoError(t, err)
	err = client.Write(ctx, metrics)
	require.NoError(t, err)
}

func TestHTTP_UnixSocket(t *testing.T) {
	tmpdir := t.TempDir()

	sock := path.Join(tmpdir, "test.sock")
	listener, err := net.Listen("unix", sock)
	require.NoError(t, err)

	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	ts.Listener = listener
	ts.Start()
	defer ts.Close()

	successResponse := []byte(`{"results": [{"statement_id": 0}]}`)

	tests := []struct {
		name             string
		config           influxdb.HTTPConfig
		database         string
		queryHandlerFunc func(t *testing.T, w http.ResponseWriter, r *http.Request)
		writeHandlerFunc func(t *testing.T, w http.ResponseWriter, r *http.Request)
		errFunc          func(t *testing.T, err error)
	}{
		{
			name: "success",
			config: influxdb.HTTPConfig{
				URL:      &url.URL{Scheme: "unix", Path: sock},
				Database: "xyzzy",
				Log:      testutil.Logger{},
			},
			queryHandlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, `CREATE DATABASE "xyzzy"`, r.FormValue("q"))
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
			writeHandlerFunc: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
				_, err = w.Write(successResponse)
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/query":
					tt.queryHandlerFunc(t, w, r)
					return
				case "/write":
					tt.queryHandlerFunc(t, w, r)
					return
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
			})

			ctx := context.Background()

			client, err := influxdb.NewHTTPClient(tt.config)
			require.NoError(t, err)
			err = client.CreateDatabase(ctx, tt.config.Database)
			if tt.errFunc != nil {
				tt.errFunc(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHTTP_WriteDatabaseTagWorksOnRetry(t *testing.T) {
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/write":
				if err := r.ParseForm(); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(r.Form["db"], []string{"foo"}) {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("Not equal, expected: %q, actual: %q", []string{"foo"}, r.Form["db"])
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

	addr := &url.URL{
		Scheme: "http",
		Host:   ts.Listener.Addr().String(),
	}

	cfg := influxdb.HTTPConfig{
		URL:                addr,
		Database:           "telegraf",
		DatabaseTag:        "database",
		ExcludeDatabaseTag: true,
		Log:                testutil.Logger{},
	}

	client, err := influxdb.NewHTTPClient(cfg)
	require.NoError(t, err)

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

	ctx := context.Background()
	err = client.Write(ctx, metrics)
	require.NoError(t, err)
	err = client.Write(ctx, metrics)
	require.NoError(t, err)
}

func TestDBRPTags(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name        string
		config      influxdb.HTTPConfig
		metrics     []telegraf.Metric
		handlerFunc func(t *testing.T, w http.ResponseWriter, r *http.Request)
		url         string
	}{
		{
			name: "defaults",
			config: influxdb.HTTPConfig{
				URL:      u,
				Database: "telegraf",
			},
			metrics: []telegraf.Metric{
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
			},
			handlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				require.Equal(t, "", r.FormValue("rp"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "static retention policy",
			config: influxdb.HTTPConfig{
				URL:             u,
				Database:        "telegraf",
				RetentionPolicy: "foo",
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			handlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				require.Equal(t, "foo", r.FormValue("rp"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "retention policy tag",
			config: influxdb.HTTPConfig{
				URL:                  u,
				SkipDatabaseCreation: true,
				Database:             "telegraf",
				RetentionPolicyTag:   "rp",
				Log:                  testutil.Logger{},
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"rp": "foo",
					},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			handlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				require.Equal(t, "foo", r.FormValue("rp"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "cpu,rp=foo value=42")
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "retention policy tag fallback to static rp",
			config: influxdb.HTTPConfig{
				URL:                  u,
				SkipDatabaseCreation: true,
				Database:             "telegraf",
				RetentionPolicy:      "foo",
				RetentionPolicyTag:   "rp",
				Log:                  testutil.Logger{},
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			handlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				require.Equal(t, "foo", r.FormValue("rp"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "retention policy tag fallback to unset rp",
			config: influxdb.HTTPConfig{
				URL:                  u,
				SkipDatabaseCreation: true,
				Database:             "telegraf",
				RetentionPolicyTag:   "rp",
				Log:                  testutil.Logger{},
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			handlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				require.Equal(t, "", r.FormValue("rp"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "exclude retention policy tag",
			config: influxdb.HTTPConfig{
				URL:                       u,
				SkipDatabaseCreation:      true,
				Database:                  "telegraf",
				RetentionPolicyTag:        "rp",
				ExcludeRetentionPolicyTag: true,
				Log:                       testutil.Logger{},
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"rp": "foo",
					},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			handlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				require.Equal(t, "foo", r.FormValue("rp"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "cpu value=42")
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "exclude database tag keeps retention policy tag",
			config: influxdb.HTTPConfig{
				URL:                  u,
				SkipDatabaseCreation: true,
				Database:             "telegraf",
				RetentionPolicyTag:   "rp",
				ExcludeDatabaseTag:   true,
				Log:                  testutil.Logger{},
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"rp": "foo",
					},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			handlerFunc: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "telegraf", r.FormValue("db"))
				require.Equal(t, "foo", r.FormValue("rp"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "cpu,rp=foo value=42")
				w.WriteHeader(http.StatusNoContent)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/write":
					tt.handlerFunc(t, w, r)
					return
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
			})

			client, err := influxdb.NewHTTPClient(tt.config)
			require.NoError(t, err)

			ctx := context.Background()
			err = client.Write(ctx, tt.metrics)
			require.NoError(t, err)
		})
	}
}

type MockHandlerChain struct {
	handlers []http.HandlerFunc
}

func (h *MockHandlerChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(h.handlers) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	next, rest := h.handlers[0], h.handlers[1:]
	h.handlers = rest
	next(w, r)
}

func (h *MockHandlerChain) Done() bool {
	return len(h.handlers) == 0
}

func TestDBRPTagsCreateDatabaseNotCalledOnRetryAfterForbidden(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	handlers := &MockHandlerChain{
		handlers: []http.HandlerFunc{
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/query":
					if r.FormValue("q") != `CREATE DATABASE "telegraf"` {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusForbidden)
					if _, err = w.Write([]byte(`{"results": [{"error": "error authorizing query"}]}`)); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						t.Error(err)
						return
					}
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/write":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/write":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
		},
	}
	ts.Config.Handler = handlers

	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			time.Unix(0, 0),
		),
	}

	output := influxdb.InfluxDB{
		URLs:        []string{u.String()},
		Database:    "telegraf",
		DatabaseTag: "database",
		Log:         testutil.Logger{},
		CreateHTTPClientF: func(config *influxdb.HTTPConfig) (influxdb.Client, error) {
			return influxdb.NewHTTPClient(*config)
		},
	}
	err = output.Connect()
	require.NoError(t, err)
	err = output.Write(metrics)
	require.NoError(t, err)
	err = output.Write(metrics)
	require.NoError(t, err)

	require.True(t, handlers.Done(), "all handlers not called")
}

func TestDBRPTagsCreateDatabaseCalledOnDatabaseNotFound(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	handlers := &MockHandlerChain{
		handlers: []http.HandlerFunc{
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/query":
					if r.FormValue("q") != `CREATE DATABASE "telegraf"` {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusForbidden)
					if _, err = w.Write([]byte(`{"results": [{"error": "error authorizing query"}]}`)); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						t.Error(err)
						return
					}
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/write":
					w.WriteHeader(http.StatusNotFound)
					if _, err = w.Write([]byte(`{"error": "database not found: \"telegraf\""}`)); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						t.Error(err)
						return
					}
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/query":
					if r.FormValue("q") != `CREATE DATABASE "telegraf"` {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusForbidden)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/write":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
		},
	}
	ts.Config.Handler = handlers

	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			time.Unix(0, 0),
		),
	}

	output := influxdb.InfluxDB{
		URLs:        []string{u.String()},
		Database:    "telegraf",
		DatabaseTag: "database",
		Log:         testutil.Logger{},
		CreateHTTPClientF: func(config *influxdb.HTTPConfig) (influxdb.Client, error) {
			return influxdb.NewHTTPClient(*config)
		},
	}

	err = output.Connect()
	require.NoError(t, err)

	// this write fails, but we're expecting it to drop the metrics and not retry, so no error.
	err = output.Write(metrics)
	require.NoError(t, err)

	// expects write to succeed
	err = output.Write(metrics)
	require.NoError(t, err)

	require.True(t, handlers.Done(), "all handlers not called")
}

func TestDBNotFoundShouldDropMetricWhenSkipDatabaseCreateIsTrue(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)
	f := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/write":
			w.WriteHeader(http.StatusNotFound)
			if _, err = w.Write([]byte(`{"error": "database not found: \"telegraf\""}`)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}

	ts.Config.Handler = http.HandlerFunc(f)

	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			time.Unix(0, 0),
		),
	}

	logger := &testutil.CaptureLogger{}
	output := influxdb.InfluxDB{
		URLs:                 []string{u.String()},
		Database:             "telegraf",
		DatabaseTag:          "database",
		SkipDatabaseCreation: true,
		Log:                  logger,
		CreateHTTPClientF: func(config *influxdb.HTTPConfig) (influxdb.Client, error) {
			return influxdb.NewHTTPClient(*config)
		},
	}

	err = output.Connect()
	require.NoError(t, err)
	err = output.Write(metrics)
	require.Contains(t, logger.LastError(), "database not found")
	require.NoError(t, err)

	err = output.Write(metrics)
	require.Contains(t, logger.LastError(), "database not found")
	require.NoError(t, err)
}
