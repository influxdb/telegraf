package http

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	common_aws "github.com/influxdata/telegraf/plugins/common/aws"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/common/oauth"
	"github.com/influxdata/telegraf/plugins/serializers/influx"
	"github.com/influxdata/telegraf/plugins/serializers/json"
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

func getMetrics(n int) []telegraf.Metric {
	m := make([]telegraf.Metric, n)
	for n > 0 {
		n--
		m[n] = getMetric()
	}
	return m
}

func TestInvalidMethod(t *testing.T) {
	plugin := &HTTP{
		URL:    "",
		Method: http.MethodGet,
	}

	err := plugin.Connect()
	require.Error(t, err)
}

func TestMethod(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name           string
		plugin         *HTTP
		expectedMethod string
		connectError   bool
	}{
		{
			name: "default method is POST",
			plugin: &HTTP{
				URL:    u.String(),
				Method: defaultMethod,
			},
			expectedMethod: http.MethodPost,
		},
		{
			name: "put is okay",
			plugin: &HTTP{
				URL:    u.String(),
				Method: http.MethodPut,
			},
			expectedMethod: http.MethodPut,
		},
		{
			name: "get is invalid",
			plugin: &HTTP{
				URL:    u.String(),
				Method: http.MethodGet,
			},
			connectError: true,
		},
		{
			name: "method is case insensitive",
			plugin: &HTTP{
				URL:    u.String(),
				Method: "poST",
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

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			if tt.connectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}

func TestHTTPClientConfig(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name                        string
		plugin                      *HTTP
		connectError                bool
		expectedMaxIdleConns        int
		expectedMaxIdleConnsPerHost int
	}{
		{
			name: "With default client Config",
			plugin: &HTTP{
				URL:    u.String(),
				Method: defaultMethod,
				HTTPClientConfig: common_http.HTTPClientConfig{
					IdleConnTimeout: config.Duration(5 * time.Second),
				},
			},
			expectedMaxIdleConns:        0,
			expectedMaxIdleConnsPerHost: 0,
		},
		{
			name: "With MaxIdleConns client Config",
			plugin: &HTTP{
				URL:    u.String(),
				Method: defaultMethod,
				HTTPClientConfig: common_http.HTTPClientConfig{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 100,
					IdleConnTimeout:     config.Duration(5 * time.Second),
				},
			},
			expectedMaxIdleConns:        100,
			expectedMaxIdleConnsPerHost: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			if tt.connectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			tr := tt.plugin.client.Transport.(*http.Transport)
			maxIdleConns, maxIdleConnsPerHost := tr.MaxIdleConns, tr.MaxIdleConnsPerHost
			require.Equal(t, tt.expectedMaxIdleConns, maxIdleConns)
			require.Equal(t, tt.expectedMaxIdleConnsPerHost, maxIdleConnsPerHost)

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}

func TestStatusCode(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name       string
		plugin     *HTTP
		statusCode int
		errFunc    func(t *testing.T, err error)
	}{
		{
			name: "success",
			plugin: &HTTP{
				URL: u.String(),
			},
			statusCode: http.StatusOK,
			errFunc: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "1xx status is an error",
			plugin: &HTTP{
				URL: u.String(),
			},
			statusCode: http.StatusSwitchingProtocols,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "3xx status is an error",
			plugin: &HTTP{
				URL: u.String(),
			},
			statusCode: http.StatusMultipleChoices,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "4xx status is an error",
			plugin: &HTTP{
				URL: u.String(),
			},
			statusCode: http.StatusBadRequest,
			errFunc: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "Do not retry on configured non-retryable statuscode",
			plugin: &HTTP{
				URL:                     u.String(),
				NonRetryableStatusCodes: []int{409},
			},
			statusCode: http.StatusConflict,
			errFunc: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			require.NoError(t, err)

			tt.plugin.Log = testutil.Logger{}

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

	headerSecret := config.NewSecret([]byte("application/json"))
	tests := []struct {
		name     string
		plugin   *HTTP
		expected string
	}{
		{
			name: "default is text plain",
			plugin: &HTTP{
				URL: u.String(),
			},
			expected: defaultContentType,
		},
		{
			name: "overwrite content_type",
			plugin: &HTTP{
				URL:     u.String(),
				Headers: map[string]*config.Secret{"Content-Type": &headerSecret},
			},
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

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			require.NoError(t, err)

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
		plugin   *HTTP
		payload  string
		expected string
	}{
		{
			name: "default is no content encoding",
			plugin: &HTTP{
				URL: u.String(),
			},
			expected: "",
		},
		{
			name: "overwrite content_encoding",
			plugin: &HTTP{
				URL:             u.String(),
				ContentEncoding: "gzip",
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
				if !strings.Contains(string(payload), "cpu value=42") {
					w.WriteHeader(http.StatusInternalServerError)
					t.Errorf("'payload' should contain %q", "cpu value=42")
					return
				}

				w.WriteHeader(http.StatusNoContent)
			})

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			require.NoError(t, err)

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
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
			name:     "username only",
			username: "username",
		},
		{
			name:     "password only",
			password: "pa$$word",
		},
		{
			name:     "username and password",
			username: "username",
			password: "pa$$word",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &HTTP{
				URL:      u.String(),
				Username: config.NewSecret([]byte(tt.username)),
				Password: config.NewSecret([]byte(tt.password)),
			}
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

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			plugin.SetSerializer(serializer)
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
		plugin       *HTTP
		tokenHandler TestHandlerFunc
		handler      TestHandlerFunc
	}{
		{
			name: "no credentials",
			plugin: &HTTP{
				URL: u.String(),
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Empty(t, r.Header["Authorization"])
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "success",
			plugin: &HTTP{
				URL: u.String() + "/write",
				HTTPClientConfig: common_http.HTTPClientConfig{
					OAuth2Config: oauth.OAuth2Config{
						ClientID:     "howdy",
						ClientSecret: "secret",
						TokenURL:     u.String() + "/token",
						Scopes:       []string{"urn:opc:idm:__myscopes__"},
					},
				},
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
		{
			name: "audience",
			plugin: &HTTP{
				URL: u.String() + "/write",
				HTTPClientConfig: common_http.HTTPClientConfig{
					OAuth2Config: oauth.OAuth2Config{
						ClientID:     "howdy",
						ClientSecret: "secret",
						TokenURL:     u.String() + "/token",
						Scopes:       []string{"urn:opc:idm:__myscopes__"},
						Audience:     "audience",
					},
				},
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
				case "/write":
					tt.handler(t, w, r)
				case "/token":
					tt.tokenHandler(t, w, r)
				}
			})

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			require.NoError(t, err)

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}

func TestOAuthAuthorizationCodeGrant(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tmpDir := t.TempDir()
	tmpFile, err := os.CreateTemp(tmpDir, "test_key_file")
	require.NoError(t, err)

	tmpTokenURI := u.String() + "/token"
	data := []byte(
		fmt.Sprintf(
			"{\n    \"type\": \"service_account\",\n    \"project_id\": \"my-project\",\n    "+
				"\"private_key_id\": \"223423436436453645363456\",\n    \"private_key\": "+
				"\"-----BEGIN PRIVATE KEY-----\\nMIICXAIBAAKBgQDX7Plvu0MJtA9TrusYtQnAogsdiYJZd9wfFIjH5FxE3SWJ4KAIE+yRWRqcqX8XnpieQLaNsfXhDPWLkWngTDydk4NO/"+
				"jlAQk0e6+9+NeiZ2ViIHmtXERb9CyiiWUmo+YCd69lhzSEIMK9EPBSDHQTgQMtEfGak03G5rx3MCakE1QIDAQABAoGAOjRU4Lt3zKvO3d3u3ZAfet+zY1jn3DolCfO9EzUJcj6ymc"+
				"IFIWhNgrikJcrCyZkkxrPnAbcQ8oNNxTuDcMTcKZbnyUnlQj5NtVuty5Q+zgf3/Q2pRhaE+TwrpOJ+ETtVp9R/PrPN2NC5wPo289fPNWFYkd4DPbdWZp5AJHz1XYECQQD3kKpinJx"+
				"MYp9FQ1Qj1OkxGln0KPgdqRYjjW/rXI4/hUodfg+xXWHPFSGj3AgEjQIvuengbOAeH3qowF1uxVTlAkEA30hXM3EbboMCDQzNRNkkV9EiZ0MZXhj1aIGl+sQZOmOeFdcdjGkDdsA4"+
				"2nmaYqXCD9KAvc+S/tGJaa0Qg0VhMQJAb2+TAqh0Qn3yK39PFIH2JcAy1ZDLfq5p5L75rfwPm9AnuHbSIYhjSo+8gMG+ai3+2fTZrcfUajrJP8S3SfFRcQJBANQQPOHatxcKzlPeq"+
				"MaPBXlyY553mAxK4CnVmPLGdL+EBYzwtlu5EVUj09uMSxkOHXYxk5yzHQVvtXbsrBZBOsECQBJLlkMjJmXrIIdLPmHQWL3bm9MMg1PqzupSEwz6cyrGuIIm/X91pDyxCHaKYWp38F"+
				"XBkYAgohI8ow5/sgRvU5w=\\n-----END PRIVATE KEY-----\\n\",\n    "+
				"\"client_email\": \"test-service-account-email@example.iam.gserviceaccount.com\",\n    \"client_id\": \"110300009813738675309\",\n    "+
				"\"auth_uri\": \"https://accounts.google.com/o/oauth2/auth\",\n    \"token_uri\": \"%s\",\n    "+
				"\"auth_provider_x509_cert_url\": \"https://www.googleapis.com/oauth2/v1/certs\",\n    "+
				"\"client_x509_cert_url\": \"https://www.googleapis.com/robot/v1/metadata/x509/test-service-account-email@example.iam.gserviceaccount.com\"\n}",
			tmpTokenURI,
		),
	)
	_, err = tmpFile.Write(data)
	require.NoError(t, err)

	require.NoError(t, tmpFile.Close())

	const token = "eyJhbGciOiJSUzI1NiIsImtpZCI6Ijg2NzUzMDliMjJiMDFiZTU2YzIxM2M5ODU0MGFiNTYzYmZmNWE1OGMiLCJ0eXAiOiJKV1QifQ.eyJhdWQiOiJodHRwOi8vMTI3LjAuMC4x" +
		"OjU4MDI1LyIsImF6cCI6InRlc3Qtc2VydmljZS1hY2NvdW50LWVtYWlsQGV4YW1wbGUuY29tIiwiZW1haWwiOiJ0ZXN0LXNlcnZpY2UtYWNjb3VudC1lbWFpbEBleGFtcGxlLmNvbSIsImVtY" +
		"WlsX3ZlcmlmaWVkIjp0cnVlLCJleHAiOjk0NjY4NDgwMCwiaWF0Ijo5NDY2ODEyMDAsImlzcyI6Imh0dHBzOi8vYWNjb3VudHMudGVzdC5jb20iLCJzdWIiOiIxMTAzMDAwMDk4MTM3Mzg2Nz" +
		"UzMDkifQ.qi2LsXP2o6nl-rbYKUlHAgTBY0QoU7Nhty5NGR4GMdc8OoGEPW-vlD0WBSaKSr11vyFcIO4ftFDWXElo9Ut-AIQPKVxinsjHIU2-LoIATgI1kyifFLyU_pBecwcI4CIXEcDK5wEk" +
		"fonWFSkyDZHBeZFKbJXlQXtxj0OHvQ-DEEepXLuKY6v3s4U6GyD9_ppYUy6gzDZPYUbfPfgxCj_Jbv6qkLU0DiZ7F5-do6X6n-qkpgCRLTGHcY__rn8oe8_pSimsyJEeY49ZQ5lj4mXkVCwgL" +
		"9bvL1_eW1p6sgbHaBnPKVPbM7S1_cBmzgSonm__qWyZUxfDgNdigtNsvzBQTg"

	tests := []struct {
		name         string
		plugin       *HTTP
		handler      TestHandlerFunc
		tokenHandler TestHandlerFunc
	}{
		{
			name: "no credentials file",
			plugin: &HTTP{
				URL: u.String(),
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Empty(t, r.Header["Authorization"])
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "success",
			plugin: &HTTP{
				URL:             u.String() + "/write",
				CredentialsFile: tmpFile.Name(),
			},
			tokenHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				authHeader := fmt.Sprintf(`{"id_token":%q}`, token)
				_, err = w.Write([]byte(authHeader))
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
				case "/write":
					tt.handler(t, w, r)
				case "/token":
					tt.tokenHandler(t, w, r)
				}
			})

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)

			require.NoError(t, tt.plugin.Connect())
			require.NoError(t, tt.plugin.Write([]telegraf.Metric{getMetric()}))
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

		client := &HTTP{
			URL:    u.String(),
			Method: defaultMethod,
		}

		serializer := &influx.Serializer{}
		require.NoError(t, serializer.Init())
		client.SetSerializer(serializer)
		err = client.Connect()
		require.NoError(t, err)

		err = client.Write([]telegraf.Metric{getMetric()})
		require.NoError(t, err)
	})
}

func TestBatchedUnbatched(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	client := &HTTP{
		URL:    u.String(),
		Method: defaultMethod,
	}

	influxSerializer := &influx.Serializer{}
	require.NoError(t, influxSerializer.Init())

	jsonSerializer := &json.Serializer{}
	require.NoError(t, jsonSerializer.Init())

	s := map[string]telegraf.Serializer{
		"influx": influxSerializer,
		"json":   jsonSerializer,
	}

	for name, serializer := range s {
		var requests int
		ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.WriteHeader(http.StatusOK)
		})

		t.Run(name, func(t *testing.T) {
			for _, mode := range [...]bool{false, true} {
				requests = 0
				client.UseBatchFormat = mode
				client.SetSerializer(serializer)

				err = client.Connect()
				require.NoError(t, err)
				err = client.Write(getMetrics(3))
				require.NoError(t, err)

				if client.UseBatchFormat {
					require.Equal(t, 1, requests, "batched")
				} else {
					require.Equal(t, 3, requests, "unbatched")
				}
			}
		})
	}
}

func TestAwsCredentials(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse("http://" + ts.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name         string
		plugin       *HTTP
		tokenHandler TestHandlerFunc
		handler      TestHandlerFunc
	}{
		{
			name: "simple credentials",
			plugin: &HTTP{
				URL:        u.String(),
				AwsService: "aps",
				CredentialConfig: common_aws.CredentialConfig{
					Region:    "us-east-1",
					AccessKey: "dummy",
					SecretKey: "dummy",
				},
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				require.Contains(t, r.Header["Authorization"][0], "AWS4-HMAC-SHA256")
				require.Contains(t, r.Header["Authorization"][0], "=dummy/")
				require.Contains(t, r.Header["Authorization"][0], "/us-east-1/")
				w.WriteHeader(http.StatusOK)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.handler(t, w, r)
			})

			serializer := &influx.Serializer{}
			require.NoError(t, serializer.Init())
			tt.plugin.SetSerializer(serializer)
			err = tt.plugin.Connect()
			require.NoError(t, err)

			err = tt.plugin.Write([]telegraf.Metric{getMetric()})
			require.NoError(t, err)
		})
	}
}
