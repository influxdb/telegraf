package oauth2

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

func TestSampleConfig(t *testing.T) {
	plugin := &OAuth2{}
	require.NotEmpty(t, plugin.SampleConfig())
}

func TestEndpointParams(t *testing.T) {
	plugin := &OAuth2{
		Endpoint: "http://localhost:8080/token",
		Tenant:   "tenantID",
		TokenConfigs: []TokenConfig{
			{
				ClientID:     config.NewSecret([]byte("clientID")),
				ClientSecret: config.NewSecret([]byte("clientSecret")),
				Key:          "test",
				Params: map[string]string{
					"foo": "bar",
				},
			},
		},
		Log: testutil.Logger{},
	}

	require.NoError(t, plugin.Init())
}

func TestInitFail(t *testing.T) {
	tests := []struct {
		name     string
		plugin   *OAuth2
		expected string
	}{
		{
			name:     "no service",
			plugin:   &OAuth2{},
			expected: "'token_endpoint' required for custom service",
		},
		{
			name:     "custom service no URL",
			plugin:   &OAuth2{},
			expected: "'token_endpoint' required for custom service",
		},
		{
			name:     "invalid service",
			plugin:   &OAuth2{Service: "foo"},
			expected: `service "foo" not supported`,
		},
		{
			name:     "AzureAD without tenant",
			plugin:   &OAuth2{Service: "AzureAD"},
			expected: "'tenant_id' required for AzureAD",
		},
		{
			name: "token without key",
			plugin: &OAuth2{
				Service:      "custom",
				Endpoint:     "http://localhost:8080",
				TokenConfigs: []TokenConfig{{}}},
			expected: "'key' not specified",
		},
		{
			name: "token without client ID",
			plugin: &OAuth2{
				Service:  "custom",
				Endpoint: "http://localhost:8080",
				TokenConfigs: []TokenConfig{
					{
						Key: "test",
					},
				},
			},
			expected: "'client_id' not specified",
		},
		{
			name: "token without client secret",
			plugin: &OAuth2{
				Service:  "custom",
				Endpoint: "http://localhost:8080",
				TokenConfigs: []TokenConfig{
					{
						Key:      "test",
						ClientID: config.NewSecret([]byte("someone")),
					},
				},
			},
			expected: "'client_secret' not specified",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.Init()
			require.ErrorContains(t, err, tt.expected)
		})
	}
}

func TestSetUnsupported(t *testing.T) {
	plugin := &OAuth2{
		Service:  "custom",
		Endpoint: "http://localhost:8080",
		TokenConfigs: []TokenConfig{
			{
				Key:          "test",
				ClientID:     config.NewSecret([]byte("someone")),
				ClientSecret: config.NewSecret([]byte("s3cr3t")),
			},
		},
	}
	require.NoError(t, plugin.Init())
	require.ErrorContains(t, plugin.Set("foo", "bar"), "not supported")
}

func TestGetNonExisting(t *testing.T) {
	plugin := &OAuth2{
		Service:  "custom",
		Endpoint: "http://localhost:8080",
		TokenConfigs: []TokenConfig{
			{
				Key:          "test",
				ClientID:     config.NewSecret([]byte("someone")),
				ClientSecret: config.NewSecret([]byte("s3cr3t")),
			},
		},
	}
	require.NoError(t, plugin.Init())

	// Make sure the key does not exist and try to read that key
	_, err := plugin.Get("foo")
	require.EqualError(t, err, `token "foo" not found`)
}

func TestResolver404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
	defer server.Close()

	plugin := &OAuth2{
		Service:  "custom",
		Endpoint: server.URL + "/token",
		TokenConfigs: []TokenConfig{
			{
				Key:          "test",
				ClientID:     config.NewSecret([]byte("someone")),
				ClientSecret: config.NewSecret([]byte("s3cr3t")),
			},
		},
	}
	require.NoError(t, plugin.Init())

	// Get the resolver
	resolver, err := plugin.GetResolver("test")
	require.NoError(t, err)
	require.NotNil(t, resolver)
	_, _, err = resolver()
	require.ErrorContains(t, err, "404 Not Found")
}

func TestGet(t *testing.T) {
	expected := "MTQ0NjJkZmQ5OTM2NDE1ZTZjNGZmZjI3"
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				if _, err := w.Write([]byte(err.Error())); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			creds := "client_id=someone&client_secret=s3cr3t&grant_type=client_credentials"
			if !strings.Contains(string(body), creds) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"%s","scope":"read write","token_type":"bearer","expires_in":299}`, expected)
		}))
	defer server.Close()

	plugin := &OAuth2{
		Service:  "custom",
		Endpoint: server.URL + "/token",
		TokenConfigs: []TokenConfig{
			{
				Key:          "test",
				ClientID:     config.NewSecret([]byte("someone")),
				ClientSecret: config.NewSecret([]byte("s3cr3t")),
			},
		},
	}
	require.NoError(t, plugin.Init())

	// Get the resolver
	token, err := plugin.Get("test")
	require.NoError(t, err)
	require.Equal(t, expected, string(token))
}

func TestGetMultipleTimes(t *testing.T) {
	expected := []string{"MTQ0NjJkZmQ5OTM2NDE1ZTZjNGZmZjI3", "03807CB390319329BDF6C777D4DFAE9C0D3B3C35"}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				if _, err := w.Write([]byte(err.Error())); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			creds := "client_id=someone&client_secret=s3cr3t&grant_type=client_credentials"
			if !strings.Contains(string(body), creds) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"%s","scope":"read write","token_type":"bearer","expires_in":60}`, expected[index])
			index++
		}))
	defer server.Close()

	plugin := &OAuth2{
		Service:  "custom",
		Endpoint: server.URL + "/token",
		TokenConfigs: []TokenConfig{
			{
				Key:          "test",
				ClientID:     config.NewSecret([]byte("someone")),
				ClientSecret: config.NewSecret([]byte("s3cr3t")),
			},
		},
	}
	require.NoError(t, plugin.Init())

	// Get the secret
	token, err := plugin.Get("test")
	require.NoError(t, err)
	require.Equal(t, expected[0], string(token))

	// Get the token another time and it should still be the same as it didn't
	// expire yet.
	token, err = plugin.Get("test")
	require.NoError(t, err)
	require.Equal(t, expected[0], string(token))
}

func TestGetExpired(t *testing.T) {
	expected := "MTQ0NjJkZmQ5OTM2NDE1ZTZjNGZmZjI3"
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				if _, err := w.Write([]byte(err.Error())); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			creds := "client_id=someone&client_secret=s3cr3t&grant_type=client_credentials"
			if !strings.Contains(string(body), creds) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"%s","scope":"read write","token_type":"bearer","expires_in":3}`, expected)
		}))
	defer server.Close()

	plugin := &OAuth2{
		Service:      "custom",
		Endpoint:     server.URL + "/token",
		ExpiryMargin: config.Duration(5 * time.Second),
		TokenConfigs: []TokenConfig{
			{
				Key:          "test",
				ClientID:     config.NewSecret([]byte("someone")),
				ClientSecret: config.NewSecret([]byte("s3cr3t")),
			},
		},
	}
	require.NoError(t, plugin.Init())

	// Get the secret
	token, err := plugin.Get("test")
	require.ErrorContains(t, err, "token invalid")
	require.Nil(t, token)
}

func TestGetRefresh(t *testing.T) {
	expected := []string{"MTQ0NjJkZmQ5OTM2NDE1ZTZjNGZmZjI3", "03807CB390319329BDF6C777D4DFAE9C0D3B3C35"}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				if _, err := w.Write([]byte(err.Error())); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			creds := "client_id=someone&client_secret=s3cr3t&grant_type=client_credentials"
			if !strings.Contains(string(body), creds) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"%s","scope":"read write","token_type":"bearer","expires_in":6}`, expected[index])
			index++
		}))
	defer server.Close()

	plugin := &OAuth2{
		Service:      "custom",
		Endpoint:     server.URL + "/token",
		ExpiryMargin: config.Duration(5 * time.Second),
		TokenConfigs: []TokenConfig{
			{
				Key:          "test",
				ClientID:     config.NewSecret([]byte("someone")),
				ClientSecret: config.NewSecret([]byte("s3cr3t")),
			},
		},
	}
	require.NoError(t, plugin.Init())

	// Get the secret
	token, err := plugin.Get("test")
	require.NoError(t, err)
	require.Equal(t, expected[0], string(token))

	// Wait until the secret expired and get the secret again
	time.Sleep(2 * time.Second)
	token, err = plugin.Get("test")
	require.NoError(t, err)
	require.Equal(t, expected[1], string(token))
}
