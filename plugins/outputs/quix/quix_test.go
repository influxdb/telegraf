package quix

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	kafkacontainer "github.com/testcontainers/testcontainers-go/modules/kafka"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

func TestMissingTopic(t *testing.T) {
	plugin := &Quix{}
	require.ErrorContains(t, plugin.Init(), "option 'topic' must be set")
}

func TestMissingWorkspace(t *testing.T) {
	plugin := &Quix{Topic: "foo"}
	require.ErrorContains(t, plugin.Init(), "option 'workspace' must be set")
}

func TestMissingToken(t *testing.T) {
	plugin := &Quix{Topic: "foo", Workspace: "bar"}
	require.ErrorContains(t, plugin.Init(), "option 'token' must be set")
}

func TestDefaultURL(t *testing.T) {
	plugin := &Quix{
		Topic:     "foo",
		Workspace: "bar",
		Token:     config.NewSecret([]byte("secret")),
	}
	require.NoError(t, plugin.Init())
	require.Equal(t, "https://portal-api.platform.quix.io", plugin.APIURL)
}

func TestFetchingConfig(t *testing.T) {
	// Setup HTTP test-server for providing the broker config
	brokerCfg := []byte(`
	{
		"bootstrap.servers":"servers",
		"sasl.mechanism":"mechanism",
		"sasl.username":"user",
		"sasl.password":"password",
		"security.protocol":"protocol",
		"ssl.ca.cert":"Y2VydA=="
	}
	`)
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/workspaces/bar/broker/librdkafka" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Header.Get("Authorization") != "Bearer bXkgc2VjcmV0" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Accept") != "application/json" {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			if _, err := w.Write(brokerCfg); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
		}),
	)
	defer server.Close()

	// Setup the plugin and fetch the config
	plugin := &Quix{
		APIURL:    server.URL,
		Topic:     "foo",
		Workspace: "bar",
		Token:     config.NewSecret([]byte("bXkgc2VjcmV0")),
	}
	require.NoError(t, plugin.Init())

	// Check the config
	expected := &brokerConfig{
		BootstrapServers: "servers",
		SaslMechanism:    "mechanism",
		SaslUsername:     "user",
		SaslPassword:     "password",
		SecurityProtocol: "protocol",
		SSLCertBase64:    "Y2VydA==",
		cert:             []byte("cert"),
	}
	cfg, err := plugin.fetchBrokerConfig()
	require.NoError(t, err)
	require.Equal(t, expected, cfg)
}

func TestConnectAndWriteIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup common config params
	workspace := "test"
	topic := "telegraf"

	// Setup a kafka container
	ctx := context.Background()
	kafkaContainer, err := kafkacontainer.Run(ctx, "confluentinc/confluent-local:7.5.0")
	require.NoError(t, err)
	defer kafkaContainer.Terminate(ctx) //nolint:errcheck // ignored

	brokers, err := kafkaContainer.Brokers(ctx)
	require.NoError(t, err)

	// Setup broker config distributed via HTTP
	brokerCfg := &brokerConfig{
		BootstrapServers: strings.Join(brokers, ","),
		SecurityProtocol: "PLAINTEXT",
	}
	response, err := json.Marshal(brokerCfg)
	require.NoError(t, err)

	// Setup authentication
	signingKey := make([]byte, 64)
	_, err = rand.Read(signingKey)
	require.NoError(t, err)

	tokenRaw := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
		Issuer:    "quix test",
	})
	token, err := tokenRaw.SignedString(signingKey)
	require.NoError(t, err)

	// Setup HTTP test-server for providing the broker config
	path := "/workspaces/" + workspace + "/broker/librdkafka"
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != path {
				w.WriteHeader(http.StatusNotFound)
				t.Logf("invalid path %q", r.URL.Path)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Accept") != "application/json" {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			if _, err := w.Write(response); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
		}),
	)
	defer server.Close()

	// Setup the plugin and establish connection
	plugin := &Quix{
		APIURL:    server.URL,
		Workspace: workspace,
		Topic:     topic,
		Token:     config.NewSecret([]byte(token)),
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Connect())
	defer plugin.Close()

	// Verify that we can successfully write data to the kafka broker
	require.NoError(t, plugin.Write(testutil.MockMetrics()))
}
