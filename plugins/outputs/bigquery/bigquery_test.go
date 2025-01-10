package bigquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"

	"github.com/influxdata/telegraf/testutil"
)

const (
	successfulResponse = `{"kind": "bigquery#tableDataInsertAllResponse"}`
)

var receivedBody map[string]json.RawMessage

type Row struct {
	Tag1      string  `json:"tag1"`
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

func TestInit(t *testing.T) {
	tests := []struct {
		name        string
		errorString string
		plugin      *BigQuery
	}{
		{
			name:        "dataset is not set",
			errorString: `"dataset" is required`,
			plugin:      &BigQuery{},
		},
		{
			name: "valid config",
			plugin: &BigQuery{
				Dataset: "test-dataset",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errorString != "" {
				require.EqualError(t, tt.plugin.Init(), tt.errorString)
			} else {
				require.NoError(t, tt.plugin.Init())
			}
		})
	}
}

func TestMetricToTable(t *testing.T) {
	tests := []struct {
		name            string
		replaceHyphenTo string
		metricName      string
		expectedTable   string
	}{
		{
			name:            "no rename",
			replaceHyphenTo: "_",
			metricName:      "test",
			expectedTable:   "test",
		},
		{
			name:            "default config",
			replaceHyphenTo: "_",
			metricName:      "table-with-hyphens",
			expectedTable:   "table_with_hyphens",
		},
		{
			name:            "custom hyphens",
			replaceHyphenTo: "*",
			metricName:      "table-with-hyphens",
			expectedTable:   "table*with*hyphens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &BigQuery{
				Dataset:         "test-dataset",
				ReplaceHyphenTo: tt.replaceHyphenTo,
				Log:             testutil.Logger{},
			}
			require.NoError(t, b.Init())

			require.Equal(t, tt.expectedTable, b.metricToTable(tt.metricName))
			if tt.metricName != tt.expectedTable {
				require.Contains(t, b.warnedOnHyphens, tt.metricName)
				require.True(t, b.warnedOnHyphens[tt.metricName])
			} else {
				require.NotContains(t, b.warnedOnHyphens, tt.metricName)
			}
		})
	}
}

func TestConnect(t *testing.T) {
	srv := localBigQueryServer(t)
	defer srv.Close()

	tests := []struct {
		name         string
		compactTable string
		errorString  string
	}{
		{name: "normal"},
		{
			name:         "compact table existing",
			compactTable: "test-metrics",
		},
		{
			name:         "compact table not existing",
			compactTable: "foobar",
			errorString:  "compact table: googleapi: got HTTP response code 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &BigQuery{
				Project:      "test-project",
				Dataset:      "test-dataset",
				Timeout:      defaultTimeout,
				CompactTable: tt.compactTable,
			}

			require.NoError(t, b.Init())
			require.NoError(t, b.setUpTestClient(srv.URL))

			if tt.errorString != "" {
				require.ErrorContains(t, b.Connect(), tt.errorString)
			} else {
				require.NoError(t, b.Connect())
			}
		})
	}
}

func TestWrite(t *testing.T) {
	srv := localBigQueryServer(t)
	defer srv.Close()

	b := &BigQuery{
		Project: "test-project",
		Dataset: "test-dataset",
		Timeout: defaultTimeout,
	}

	mockMetrics := testutil.MockMetrics()

	require.NoError(t, b.Init())
	require.NoError(t, b.setUpTestClient(srv.URL))
	require.NoError(t, b.Connect())

	require.NoError(t, b.Write(mockMetrics))

	var rows []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(receivedBody["rows"], &rows))

	var row Row
	require.NoError(t, json.Unmarshal(rows[0]["json"], &row))

	pt, err := time.Parse(time.RFC3339, row.Timestamp)
	require.NoError(t, err)
	require.Equal(t, mockMetrics[0].Tags()["tag1"], row.Tag1)
	require.Equal(t, mockMetrics[0].Time(), pt)
	require.InDelta(t, mockMetrics[0].Fields()["value"], row.Value, testutil.DefaultDelta)
}

func TestWriteCompact(t *testing.T) {
	srv := localBigQueryServer(t)
	defer srv.Close()

	b := &BigQuery{
		Project:      "test-project",
		Dataset:      "test-dataset",
		Timeout:      defaultTimeout,
		CompactTable: "test-metrics",
	}

	mockMetrics := testutil.MockMetrics()

	require.NoError(t, b.Init())
	require.NoError(t, b.setUpTestClient(srv.URL))
	require.NoError(t, b.Connect())

	require.NoError(t, b.Write(mockMetrics))

	var rows []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(receivedBody["rows"], &rows))
	require.Len(t, rows, 1)
	require.Contains(t, rows[0], "json")

	var row interface{}
	require.NoError(t, json.Unmarshal(rows[0]["json"], &row))
	require.Equal(t, map[string]interface{}{
		"timestamp": "2009-11-10T23:00:00Z",
		"name":      "test1",
		"tags":      `{"tag1":"value1"}`,
		"fields":    `{"value":1}`,
	}, row)

	require.NoError(t, b.Close())
}

func TestAutoDetect(t *testing.T) {
	srv := localBigQueryServer(t)
	defer srv.Close()

	b := &BigQuery{
		Dataset:      "test-dataset",
		Timeout:      defaultTimeout,
		CompactTable: "test-metrics",
	}

	credentialsJSON := []byte(`{"type": "service_account", "project_id": "test-project"}`)

	require.NoError(t, b.Init())
	require.NoError(t, b.setUpTestClientWithJSON(srv.URL, credentialsJSON))
	require.NoError(t, b.Connect())
	require.NoError(t, b.Close())
}

func (b *BigQuery) setUpTestClient(endpointURL string) error {
	noAuth := option.WithoutAuthentication()
	endpoint := option.WithEndpoint(endpointURL)

	ctx := context.Background()

	c, err := bigquery.NewClient(ctx, b.Project, noAuth, endpoint)

	if err != nil {
		return err
	}

	b.client = c

	return nil
}

func (b *BigQuery) setUpTestClientWithJSON(endpointURL string, credentialsJSON []byte) error {
	noAuth := option.WithoutAuthentication()
	endpoint := option.WithEndpoint(endpointURL)
	credentials := option.WithCredentialsJSON(credentialsJSON)
	skipValidate := internaloption.SkipDialSettingsValidation()

	ctx := context.Background()

	c, err := bigquery.NewClient(ctx, b.Project, credentials, noAuth, endpoint, skipValidate)

	b.client = c
	return err
}

func localBigQueryServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.NotFoundHandler())

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/test-project/datasets/test-dataset/tables/test1/insertAll",
			"/projects/test-project/datasets/test-dataset/tables/test-metrics/insertAll":
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&receivedBody); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}

			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(successfulResponse)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		case "/projects/test-project/datasets/test-dataset/tables/test-metrics":
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("{}")); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			if _, err := w.Write([]byte(r.URL.String())); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		}
	})

	return srv
}
