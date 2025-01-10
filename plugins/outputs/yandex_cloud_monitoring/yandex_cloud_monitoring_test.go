package yandex_cloud_monitoring

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestWrite(t *testing.T) {
	readBody := func(r *http.Request) yandexCloudMonitoringMessage {
		decoder := json.NewDecoder(r.Body)
		var message yandexCloudMonitoringMessage
		err := decoder.Decode(&message)
		require.NoError(t, err)
		return message
	}

	testMetadataHTTPServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/token") {
				token := MetadataIamToken{
					AccessToken: "token1",
					ExpiresIn:   123,
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				if err := json.NewEncoder(w).Encode(token); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			} else if strings.HasSuffix(r.URL.Path, "/folder") {
				if _, err := io.WriteString(w, "folder1"); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer testMetadataHTTPServer.Close()
	metadataTokenURL := "http://" + testMetadataHTTPServer.Listener.Addr().String() + "/token"
	metadataFolderURL := "http://" + testMetadataHTTPServer.Listener.Addr().String() + "/folder"

	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	url := "http://" + ts.Listener.Addr().String() + "/metrics"

	tests := []struct {
		name    string
		plugin  *YandexCloudMonitoring
		metrics []telegraf.Metric
		handler func(t *testing.T, w http.ResponseWriter, r *http.Request)
	}{
		{
			name:   "metric is converted to json value",
			plugin: &YandexCloudMonitoring{},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cluster",
					map[string]string{},
					map[string]interface{}{
						"cpu": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				message := readBody(r)
				require.Len(t, message.Metrics, 1)
				require.Equal(t, "cpu", message.Metrics[0].Name)
				require.InDelta(t, 42.0, message.Metrics[0].Value, testutil.DefaultDelta)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:   "int64 metric is converted to json value",
			plugin: &YandexCloudMonitoring{},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cluster",
					map[string]string{},
					map[string]interface{}{
						"value": int64(9223372036854775806),
					},
					time.Unix(0, 0),
				),
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				message := readBody(r)
				require.Len(t, message.Metrics, 1)
				require.Equal(t, "value", message.Metrics[0].Name)
				require.InDelta(t, float64(9.223372036854776e+18), message.Metrics[0].Value, testutil.DefaultDelta)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:   "int metric is converted to json value",
			plugin: &YandexCloudMonitoring{},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cluster",
					map[string]string{},
					map[string]interface{}{
						"value": 9226,
					},
					time.Unix(0, 0),
				),
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				message := readBody(r)
				require.Len(t, message.Metrics, 1)
				require.Equal(t, "value", message.Metrics[0].Name)
				require.InDelta(t, float64(9226), message.Metrics[0].Value, testutil.DefaultDelta)
				w.WriteHeader(http.StatusOK)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.handler(t, w, r)
			})
			tt.plugin.Log = testutil.Logger{}
			tt.plugin.EndpointURL = url
			tt.plugin.MetadataTokenURL = metadataTokenURL
			tt.plugin.MetadataFolderURL = metadataFolderURL
			err := tt.plugin.Connect()
			require.NoError(t, err)

			err = tt.plugin.Write(tt.metrics)

			require.NoError(t, err)
		})
	}
}
