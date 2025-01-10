package nebius_cloud_monitoring

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

func readBody(r *http.Request) (nebiusCloudMonitoringMessage, error) {
	decoder := json.NewDecoder(r.Body)
	var message nebiusCloudMonitoringMessage
	err := decoder.Decode(&message)
	return message, err
}

func TestWrite(t *testing.T) {
	testMetadataHTTPServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/token") {
				token := metadataIamToken{
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
		plugin  *NebiusCloudMonitoring
		metrics []telegraf.Metric
		handler func(t *testing.T, w http.ResponseWriter, r *http.Request)
	}{
		{
			name:   "metric is converted to json value",
			plugin: &NebiusCloudMonitoring{},
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
				message, err := readBody(r)
				require.NoError(t, err)
				require.Len(t, message.Metrics, 1)
				require.Equal(t, "cluster_cpu", message.Metrics[0].Name)
				require.InDelta(t, 42.0, message.Metrics[0].Value, testutil.DefaultDelta)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:   "int64 metric is converted to json value",
			plugin: &NebiusCloudMonitoring{},
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
				message, err := readBody(r)
				require.NoError(t, err)
				require.Len(t, message.Metrics, 1)
				require.Equal(t, "cluster_value", message.Metrics[0].Name)
				require.InDelta(t, float64(9.223372036854776e+18), message.Metrics[0].Value, testutil.DefaultDelta)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:   "int metric is converted to json value",
			plugin: &NebiusCloudMonitoring{},
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
				message, err := readBody(r)
				require.NoError(t, err)
				require.Len(t, message.Metrics, 1)
				require.Equal(t, "cluster_value", message.Metrics[0].Name)
				require.InDelta(t, float64(9226), message.Metrics[0].Value, testutil.DefaultDelta)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:   "label with name 'name' is replaced with '_name'",
			plugin: &NebiusCloudMonitoring{},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"cluster",
					map[string]string{
						"name": "accounts-daemon.service",
					},
					map[string]interface{}{
						"value": 9226,
					},
					time.Unix(0, 0),
				),
			},
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				message, err := readBody(r)
				require.NoError(t, err)
				require.Len(t, message.Metrics, 1)
				require.Equal(t, "cluster_value", message.Metrics[0].Name)
				require.Contains(t, message.Metrics[0].Labels, "_name")
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
			tt.plugin = &NebiusCloudMonitoring{
				Endpoint:          url,
				metadataTokenURL:  metadataTokenURL,
				metadataFolderURL: metadataFolderURL,
				Log:               testutil.Logger{},
			}
			require.NoError(t, tt.plugin.Init())
			require.NoError(t, tt.plugin.Connect())
			require.NoError(t, tt.plugin.Write(tt.metrics))
		})
	}
}

func TestReplaceReservedTagNames(t *testing.T) {
	tagMap := map[string]string{
		"name":  "value",
		"other": "value",
	}
	wantTagMap := map[string]string{
		"_name": "value",
		"other": "value",
	}

	type args struct {
		tagNames map[string]string
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "tagReplacement",
			args: args{
				tagNames: tagMap,
			},
			want: wantTagMap,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceReservedTagNames(tt.args.tagNames)
			require.EqualValues(t, tt.want, got)
		})
	}
}
