package http

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/secretstores"
	"github.com/influxdata/telegraf/testutil"
)

func TestCases(t *testing.T) {
	// Get all directories in testcases
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	// Make sure tests contains data
	require.NotEmpty(t, folders)

	// Set up for file inputs
	secretstores.Add("http", func(string) telegraf.SecretStore {
		return &HTTP{Log: testutil.Logger{}}
	})

	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}

		fname := f.Name()
		t.Run(fname, func(t *testing.T) {
			testdataPath := filepath.Join("testcases", fname)
			configFilename := filepath.Join(testdataPath, "telegraf.conf")
			inputFilename := filepath.Join(testdataPath, "secrets.json")
			expectedFilename := filepath.Join(testdataPath, "expected.json")

			// Read the input data
			input, err := os.ReadFile(inputFilename)
			require.NoError(t, err)

			// Read the expected output data
			buf, err := os.ReadFile(expectedFilename)
			require.NoError(t, err)
			var expected map[string]string
			require.NoError(t, json.Unmarshal(buf, &expected))

			// Configure the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFilename))
			require.NotEmpty(t, cfg.SecretStores)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/secrets" {
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
			us, err := url.Parse(server.URL)
			require.NoError(t, err)

			var id string
			var plugin telegraf.SecretStore
			actual := make(map[string]string, len(expected))
			for id, plugin = range cfg.SecretStores {
				// Setup dummy server and redirect the plugin's URL to that dummy
				httpPlugin, ok := plugin.(*HTTP)
				require.True(t, ok)

				u, err := url.Parse(httpPlugin.URL)
				require.NoError(t, err)
				u.Host = us.Host
				httpPlugin.URL = u.String()
				require.NoError(t, httpPlugin.download())

				// Retrieve the secrets from the plugin
				keys, err := plugin.List()
				require.NoError(t, err)

				for _, k := range keys {
					v, err := plugin.Get(k)
					require.NoError(t, err)
					actual[id+"."+k] = string(v)
				}
			}
			require.EqualValues(t, expected, actual)
		})
	}
}

func TestSampleConfig(t *testing.T) {
	plugin := &HTTP{}
	require.NotEmpty(t, plugin.SampleConfig())
}

func TestInit(t *testing.T) {
	plugin := &HTTP{
		DecryptionConfig: DecryptionConfig{
			Cipher: "AES128/CBC/PKCS#5",
			Aes: AesEncryptor{
				Key: config.NewSecret([]byte("7465737474657374657374746573740a")),
				Vec: config.NewSecret([]byte("7465737474657374657374746573740a")),
			},
		},
	}
	require.NoError(t, plugin.Init())
}

func TestInitErrors(t *testing.T) {
	plugin := &HTTP{Transformation: "{some: malformed"}
	require.ErrorContains(t, plugin.Init(), "setting up data transformation failed")

	plugin = &HTTP{DecryptionConfig: DecryptionConfig{Cipher: "non-existing/CBC/lala"}}
	require.ErrorContains(t, plugin.Init(), "creating decryptor failed: unknown cipher")
}

func TestSetNotSupported(t *testing.T) {
	plugin := &HTTP{}
	require.NoError(t, plugin.Init())

	require.ErrorContains(t, plugin.Set("key", "value"), "setting secrets not supported")
}

func TestGetErrors(t *testing.T) {
	plugin := &HTTP{
		DecryptionConfig: DecryptionConfig{
			Cipher: "AES256/CBC/PKCS#5",
			Aes: AesEncryptor{
				Key: config.NewSecret([]byte("63238c069e3c5d6aaa20048c43ce4ed0a910eef95f22f55bacdddacafa06b656")),
				Vec: config.NewSecret([]byte("61737570657273656372657469763432")),
			},
		},
	}
	require.NoError(t, plugin.Init())

	_, err := plugin.Get("OMG")
	require.ErrorContains(t, err, "not found")

	plugin.cache = map[string]string{"test": "aedMZXaLR246OHHjVtJKXQ=X"}
	_, err = plugin.Get("test")
	require.ErrorContains(t, err, "base64 decoding failed")
}

func TestResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{"test": "aedMZXaLR246OHHjVtJKXQ=="}`)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	plugin := &HTTP{
		URL: server.URL,
		DecryptionConfig: DecryptionConfig{
			Cipher: "AES256/CBC/PKCS#5",
			Aes: AesEncryptor{
				Key: config.NewSecret([]byte("63238c069e3c5d6aaa20048c43ce4ed0a910eef95f22f55bacdddacafa06b656")),
				Vec: config.NewSecret([]byte("61737570657273656372657469763432")),
			},
		},
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())

	resolver, err := plugin.GetResolver("test")
	require.NoError(t, err)

	s, _, err := resolver()
	require.NoError(t, err)
	require.Equal(t, "password-B", string(s))
}

func TestGetResolverErrors(t *testing.T) {
	dummy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummy.Close()

	plugin := &HTTP{
		URL: "http://" + dummy.Addr().String(),
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())

	_, err = plugin.GetResolver("test")
	require.ErrorContains(t, err, "context deadline exceeded")
	dummy.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err = w.Write([]byte(`[{"test": "aedMZXaLR246OHHjVtJKXQ=="}]`)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	plugin = &HTTP{
		URL: server.URL,
		DecryptionConfig: DecryptionConfig{
			Cipher: "AES256/CBC/PKCS#5",
			Aes: AesEncryptor{
				Key: config.NewSecret([]byte("63238c069e3c5d6aaa20048c43ce4ed0a910eef95f22f55bacdddacafa06b656")),
				Vec: config.NewSecret([]byte("61737570657273656372657469763432")),
			},
		},
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())

	_, err = plugin.GetResolver("test")
	require.ErrorContains(t, err, "maybe missing or wrong data transformation")

	plugin.Transformation = "{awe:skds}"
	require.NoError(t, plugin.Init())

	_, err = plugin.GetResolver("test")
	require.ErrorContains(t, err, "transforming data failed")
}

func TestInvalidServerResponse(t *testing.T) {
	dummy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummy.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err = w.Write([]byte(`[somerandomebytes`)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	plugin := &HTTP{
		URL: server.URL,
		DecryptionConfig: DecryptionConfig{
			Cipher: "AES256/CBC/PKCS#5",
			Aes: AesEncryptor{
				Key: config.NewSecret([]byte("63238c069e3c5d6aaa20048c43ce4ed0a910eef95f22f55bacdddacafa06b656")),
				Vec: config.NewSecret([]byte("61737570657273656372657469763432")),
			},
		},
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())

	_, err = plugin.GetResolver("test")
	require.Error(t, err)
	var expectedErr *json.SyntaxError
	require.ErrorAs(t, err, &expectedErr)
}

func TestAdditionalHeaders(t *testing.T) {
	dummy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummy.Close()

	var actual http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual = r.Header.Clone()
		if r.Host != "" {
			actual.Add("host", r.Host)
		}
		if _, err = w.Write([]byte(`{"test": "aedMZXaLR246OHHjVtJKXQ=="}`)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	plugin := &HTTP{
		URL: server.URL,
		Headers: map[string]string{
			"host": "a.host.com",
			"foo":  "bar",
		},
		DecryptionConfig: DecryptionConfig{
			Cipher: "AES256/CBC/PKCS#5",
			Aes: AesEncryptor{
				Key: config.NewSecret([]byte("63238c069e3c5d6aaa20048c43ce4ed0a910eef95f22f55bacdddacafa06b656")),
				Vec: config.NewSecret([]byte("61737570657273656372657469763432")),
			},
		},
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())

	require.NoError(t, plugin.download())

	secret, err := plugin.Get("test")
	require.NoError(t, err)
	require.Equal(t, "password-B", string(secret))

	for k, v := range plugin.Headers {
		av := actual.Get(k)
		require.NotEmptyf(t, av, "header %q not found", k)
		require.Equal(t, v, av, "mismatch for header %q", k)
	}
}

func TestServerReturnCodes(t *testing.T) {
	dummy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummy.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/200":
			if _, err = w.Write([]byte(`{}`)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		case "/201":
			w.WriteHeader(201)
		case "/300":
			w.WriteHeader(300)
			if _, err = w.Write([]byte(`{}`)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		case "/401":
			w.WriteHeader(401)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	plugin := &HTTP{
		URL:                server.URL,
		SuccessStatusCodes: []int{200, 300},
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())

	// 200 and 300 should not return an error
	require.NoError(t, plugin.download())
	plugin.URL = server.URL + "/200"
	require.NoError(t, plugin.download())
	plugin.URL = server.URL + "/300"
	require.NoError(t, plugin.download())

	// other error codes should cause errors
	plugin.URL = server.URL + "/201"
	require.ErrorContains(t, plugin.download(), "received status code 201")
	plugin.URL = server.URL + "/401"
	require.ErrorContains(t, plugin.download(), "received status code 401")
	plugin.URL = server.URL + "/somewhere"
	require.ErrorContains(t, plugin.download(), "received status code 404")
}

func TestAuthenticationBasic(t *testing.T) {
	dummy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummy.Close()

	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header
		if _, err = w.Write([]byte(`{}`)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	plugin := &HTTP{
		URL:                server.URL,
		Username:           config.NewSecret([]byte("myuser")),
		Password:           config.NewSecret([]byte("mypass")),
		SuccessStatusCodes: []int{200, 300},
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.download())

	auth := header.Get("Authorization")
	require.NotEmpty(t, auth)
	require.Equal(t, "Basic bXl1c2VyOm15cGFzcw==", auth)
}

func TestAuthenticationToken(t *testing.T) {
	dummy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummy.Close()

	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header
		if _, err = w.Write([]byte(`{}`)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	token := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJUaWdlciIsImlhdCI6M..."
	plugin := &HTTP{
		URL:                server.URL,
		Token:              config.NewSecret([]byte(token)),
		SuccessStatusCodes: []int{200, 300},
	}
	plugin.Timeout = config.Duration(200 * time.Millisecond)
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.download())

	auth := header.Get("Authorization")
	require.NotEmpty(t, auth)
	require.Equal(t, "Bearer "+token, auth)
}
