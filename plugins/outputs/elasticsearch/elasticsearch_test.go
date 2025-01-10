package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

const servicePort = "9200"

func launchTestContainer(t *testing.T) *testutil.Container {
	container := testutil.Container{
		Image:        "elasticsearch:6.8.23",
		ExposedPorts: []string{servicePort},
		Env: map[string]string{
			"discovery.type": "single-node",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("] mode [basic] - valid"),
			wait.ForListeningPort(nat.Port(servicePort)),
		),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")

	return &container
}

func TestConnectAndWriteIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	e := &Elasticsearch{
		URLs:                urls,
		IndexName:           "test-%Y.%m.%d",
		Timeout:             config.Duration(time.Second * 5),
		EnableGzip:          true,
		ManageTemplate:      true,
		TemplateName:        "telegraf",
		OverwriteTemplate:   false,
		HealthCheckInterval: config.Duration(time.Second * 10),
		HealthCheckTimeout:  config.Duration(time.Second * 1),
		Log:                 testutil.Logger{},
	}

	// Verify that we can connect to Elasticsearch
	err := e.Connect()
	require.NoError(t, err)

	// Verify that we can successfully write data to Elasticsearch
	err = e.Write(testutil.MockMetrics())
	require.NoError(t, err)
}

func TestConnectAndWriteMetricWithNaNValueEmptyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	e := &Elasticsearch{
		URLs:                urls,
		IndexName:           "test-%Y.%m.%d",
		Timeout:             config.Duration(time.Second * 5),
		ManageTemplate:      true,
		TemplateName:        "telegraf",
		OverwriteTemplate:   false,
		HealthCheckInterval: config.Duration(time.Second * 10),
		HealthCheckTimeout:  config.Duration(time.Second * 1),
		Log:                 testutil.Logger{},
	}

	metrics := []telegraf.Metric{
		testutil.TestMetric(math.NaN()),
		testutil.TestMetric(math.Inf(1)),
		testutil.TestMetric(math.Inf(-1)),
	}

	// Verify that we can connect to Elasticsearch
	err := e.Connect()
	require.NoError(t, err)

	// Verify that we can fail for metric with unhandled NaN/inf/-inf values
	for _, m := range metrics {
		err = e.Write([]telegraf.Metric{m})
		require.Error(t, err, "error sending bulk request to Elasticsearch: json: unsupported value: NaN")
	}
}

func TestConnectAndWriteMetricWithNaNValueNoneIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	e := &Elasticsearch{
		URLs:                urls,
		IndexName:           "test-%Y.%m.%d",
		Timeout:             config.Duration(time.Second * 5),
		ManageTemplate:      true,
		TemplateName:        "telegraf",
		OverwriteTemplate:   false,
		HealthCheckInterval: config.Duration(time.Second * 10),
		HealthCheckTimeout:  config.Duration(time.Second * 1),
		FloatHandling:       "none",
		Log:                 testutil.Logger{},
	}

	metrics := []telegraf.Metric{
		testutil.TestMetric(math.NaN()),
		testutil.TestMetric(math.Inf(1)),
		testutil.TestMetric(math.Inf(-1)),
	}

	// Verify that we can connect to Elasticsearch
	err := e.Connect()
	require.NoError(t, err)

	// Verify that we can fail for metric with unhandled NaN/inf/-inf values
	for _, m := range metrics {
		err = e.Write([]telegraf.Metric{m})
		require.Error(t, err, "error sending bulk request to Elasticsearch: json: unsupported value: NaN")
	}
}

func TestConnectAndWriteMetricWithNaNValueDropIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	e := &Elasticsearch{
		URLs:                urls,
		IndexName:           "test-%Y.%m.%d",
		Timeout:             config.Duration(time.Second * 5),
		ManageTemplate:      true,
		TemplateName:        "telegraf",
		OverwriteTemplate:   false,
		HealthCheckInterval: config.Duration(time.Second * 10),
		HealthCheckTimeout:  config.Duration(time.Second * 1),
		FloatHandling:       "drop",
		Log:                 testutil.Logger{},
	}

	metrics := []telegraf.Metric{
		testutil.TestMetric(math.NaN()),
		testutil.TestMetric(math.Inf(1)),
		testutil.TestMetric(math.Inf(-1)),
	}

	// Verify that we can connect to Elasticsearch
	err := e.Connect()
	require.NoError(t, err)

	// Verify that we can fail for metric with unhandled NaN/inf/-inf values
	for _, m := range metrics {
		err = e.Write([]telegraf.Metric{m})
		require.NoError(t, err)
	}
}

func TestConnectAndWriteMetricWithNaNValueReplacementIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		floatHandle      string
		floatReplacement float64
		expectError      bool
	}{
		{
			"none",
			0.0,
			true,
		},
		{
			"drop",
			0.0,
			false,
		},
		{
			"replace",
			0.0,
			false,
		},
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	for _, test := range tests {
		e := &Elasticsearch{
			URLs:                urls,
			IndexName:           "test-%Y.%m.%d",
			Timeout:             config.Duration(time.Second * 5),
			ManageTemplate:      true,
			TemplateName:        "telegraf",
			OverwriteTemplate:   false,
			HealthCheckInterval: config.Duration(time.Second * 10),
			HealthCheckTimeout:  config.Duration(time.Second * 1),
			FloatHandling:       test.floatHandle,
			FloatReplacement:    test.floatReplacement,
			Log:                 testutil.Logger{},
		}

		metrics := []telegraf.Metric{
			testutil.TestMetric(math.NaN()),
			testutil.TestMetric(math.Inf(1)),
			testutil.TestMetric(math.Inf(-1)),
		}

		err := e.Connect()
		require.NoError(t, err)

		for _, m := range metrics {
			err = e.Write([]telegraf.Metric{m})

			if test.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		}
	}
}

func TestTemplateManagementEmptyTemplateIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	ctx := context.Background()

	e := &Elasticsearch{
		URLs:              urls,
		IndexName:         "test-%Y.%m.%d",
		Timeout:           config.Duration(time.Second * 5),
		EnableGzip:        true,
		ManageTemplate:    true,
		TemplateName:      "",
		OverwriteTemplate: true,
		Log:               testutil.Logger{},
	}

	err := e.manageTemplate(ctx)
	require.Error(t, err)
}

func TestUseOpTypeCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	e := &Elasticsearch{
		URLs:              urls,
		IndexName:         "test-%Y.%m.%d",
		Timeout:           config.Duration(time.Second * 5),
		EnableGzip:        true,
		ManageTemplate:    true,
		TemplateName:      "telegraf",
		OverwriteTemplate: true,
		UseOpTypeCreate:   true,
		Log:               testutil.Logger{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	metrics := []telegraf.Metric{
		testutil.TestMetric(1),
	}

	err := e.Connect()
	require.NoError(t, err)

	err = e.manageTemplate(ctx)
	require.NoError(t, err)

	// Verify that we can fail for metric with unhandled NaN/inf/-inf values
	for _, m := range metrics {
		err = e.Write([]telegraf.Metric{m})
		require.NoError(t, err)
	}
}

func TestTemplateManagementIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	e := &Elasticsearch{
		URLs:              urls,
		IndexName:         "test-%Y.%m.%d",
		Timeout:           config.Duration(time.Second * 5),
		EnableGzip:        true,
		ManageTemplate:    true,
		TemplateName:      "telegraf",
		OverwriteTemplate: true,
		Log:               testutil.Logger{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	err := e.Connect()
	require.NoError(t, err)

	err = e.manageTemplate(ctx)
	require.NoError(t, err)
}

func TestTemplateInvalidIndexPatternIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := launchTestContainer(t)
	defer container.Terminate()

	urls := []string{
		fmt.Sprintf("http://%s:%s", container.Address, container.Ports[servicePort]),
	}

	e := &Elasticsearch{
		URLs:              urls,
		IndexName:         "{{host}}-%Y.%m.%d",
		Timeout:           config.Duration(time.Second * 5),
		EnableGzip:        true,
		ManageTemplate:    true,
		TemplateName:      "telegraf",
		OverwriteTemplate: true,
		Log:               testutil.Logger{},
	}

	err := e.Connect()
	require.Error(t, err)
}

func TestGetTagKeys(t *testing.T) {
	e := &Elasticsearch{
		DefaultTagValue: "none",
		Log:             testutil.Logger{},
	}

	tests := []struct {
		IndexName         string
		ExpectedIndexName string
		ExpectedTagKeys   []string
	}{
		{
			IndexName:         "indexname",
			ExpectedIndexName: "indexname",
			ExpectedTagKeys:   make([]string, 0),
		}, {
			IndexName:         "indexname-%Y",
			ExpectedIndexName: "indexname-%Y",
			ExpectedTagKeys:   make([]string, 0),
		}, {
			IndexName:         "indexname-%Y-%m",
			ExpectedIndexName: "indexname-%Y-%m",
			ExpectedTagKeys:   make([]string, 0),
		}, {
			IndexName:         "indexname-%Y-%m-%d",
			ExpectedIndexName: "indexname-%Y-%m-%d",
			ExpectedTagKeys:   make([]string, 0),
		}, {
			IndexName:         "indexname-%Y-%m-%d-%H",
			ExpectedIndexName: "indexname-%Y-%m-%d-%H",
			ExpectedTagKeys:   make([]string, 0),
		}, {
			IndexName:         "indexname-%y-%m",
			ExpectedIndexName: "indexname-%y-%m",
			ExpectedTagKeys:   make([]string, 0),
		}, {
			IndexName:         "indexname-{{tag1}}-%y-%m",
			ExpectedIndexName: "indexname-%s-%y-%m",
			ExpectedTagKeys:   []string{"tag1"},
		}, {
			IndexName:         "indexname-{{tag1}}-{{tag2}}-%y-%m",
			ExpectedIndexName: "indexname-%s-%s-%y-%m",
			ExpectedTagKeys:   []string{"tag1", "tag2"},
		}, {
			IndexName:         "indexname-{{tag1}}-{{tag2}}-{{tag3}}-%y-%m",
			ExpectedIndexName: "indexname-%s-%s-%s-%y-%m",
			ExpectedTagKeys:   []string{"tag1", "tag2", "tag3"},
		},
	}
	for _, test := range tests {
		indexName, tagKeys := e.GetTagKeys(test.IndexName)
		if indexName != test.ExpectedIndexName {
			t.Errorf("Expected indexname %s, got %s\n", test.ExpectedIndexName, indexName)
		}
		if !reflect.DeepEqual(tagKeys, test.ExpectedTagKeys) {
			t.Errorf("Expected tagKeys %s, got %s\n", test.ExpectedTagKeys, tagKeys)
		}
	}
}

func TestGetIndexName(t *testing.T) {
	e := &Elasticsearch{
		DefaultTagValue: "none",
		Log:             testutil.Logger{},
	}

	tests := []struct {
		EventTime time.Time
		Tags      map[string]string
		TagKeys   []string
		IndexName string
		Expected  string
	}{
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			IndexName: "indexname",
			Expected:  "indexname",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			IndexName: "indexname-%Y",
			Expected:  "indexname-2014",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			IndexName: "indexname-%Y-%m",
			Expected:  "indexname-2014-12",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			IndexName: "indexname-%Y-%m-%d",
			Expected:  "indexname-2014-12-01",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			IndexName: "indexname-%Y-%m-%d-%H",
			Expected:  "indexname-2014-12-01-23",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			IndexName: "indexname-%y-%m",
			Expected:  "indexname-14-12",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			IndexName: "indexname-%Y-%V",
			Expected:  "indexname-2014-49",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			TagKeys:   []string{"tag1"},
			IndexName: "indexname-%s-%y-%m",
			Expected:  "indexname-value1-14-12",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			TagKeys:   []string{"tag1", "tag2"},
			IndexName: "indexname-%s-%s-%y-%m",
			Expected:  "indexname-value1-value2-14-12",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			TagKeys:   []string{"tag1", "tag2", "tag3"},
			IndexName: "indexname-%s-%s-%s-%y-%m",
			Expected:  "indexname-value1-value2-none-14-12",
		},
	}
	for _, test := range tests {
		indexName := e.GetIndexName(test.IndexName, test.EventTime, test.TagKeys, test.Tags)
		if indexName != test.Expected {
			t.Errorf("Expected indexname %s, got %s\n", test.Expected, indexName)
		}
	}
}

func TestGetPipelineName(t *testing.T) {
	e := &Elasticsearch{
		UsePipeline:     "{{es-pipeline}}",
		DefaultPipeline: "myDefaultPipeline",
		Log:             testutil.Logger{},
	}
	e.pipelineName, e.pipelineTagKeys = e.GetTagKeys(e.UsePipeline)

	tests := []struct {
		EventTime       time.Time
		Tags            map[string]string
		PipelineTagKeys []string
		Expected        string
	}{
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			Expected:  "myDefaultPipeline",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			Expected:  "myDefaultPipeline",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "es-pipeline": "myOtherPipeline"},
			Expected:  "myOtherPipeline",
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "es-pipeline": "pipeline2"},
			Expected:  "pipeline2",
		},
	}
	for _, test := range tests {
		pipelineName := e.getPipelineName(e.pipelineName, e.pipelineTagKeys, test.Tags)
		require.Equal(t, test.Expected, pipelineName)
	}

	// Setup testing for testing no pipeline set. All the tests in this case should return "".
	e = &Elasticsearch{
		Log: testutil.Logger{},
	}
	e.pipelineName, e.pipelineTagKeys = e.GetTagKeys(e.UsePipeline)

	for _, test := range tests {
		pipelineName := e.getPipelineName(e.pipelineName, e.pipelineTagKeys, test.Tags)
		require.Equal(t, "", pipelineName)
	}
}

func TestPipelineConfigs(t *testing.T) {
	tests := []struct {
		EventTime       time.Time
		Tags            map[string]string
		PipelineTagKeys []string
		Expected        string
		Elastic         *Elasticsearch
	}{
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			Elastic: &Elasticsearch{
				Log: testutil.Logger{},
			},
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "tag2": "value2"},
			Elastic: &Elasticsearch{
				DefaultPipeline: "myDefaultPipeline",
				Log:             testutil.Logger{},
			},
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "es-pipeline": "myOtherPipeline"},
			Expected:  "myDefaultPipeline",
			Elastic: &Elasticsearch{
				UsePipeline: "myDefaultPipeline",
				Log:         testutil.Logger{},
			},
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "es-pipeline": "pipeline2"},
			Elastic: &Elasticsearch{
				DefaultPipeline: "myDefaultPipeline",
				Log:             testutil.Logger{},
			},
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "es-pipeline": "pipeline2"},
			Expected:  "pipeline2",
			Elastic: &Elasticsearch{
				UsePipeline: "{{es-pipeline}}",
				Log:         testutil.Logger{},
			},
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1", "es-pipeline": "pipeline2"},
			Expected:  "value1-pipeline2",
			Elastic: &Elasticsearch{
				UsePipeline: "{{tag1}}-{{es-pipeline}}",
				Log:         testutil.Logger{},
			},
		},
		{
			EventTime: time.Date(2014, 12, 01, 23, 30, 00, 00, time.UTC),
			Tags:      map[string]string{"tag1": "value1"},
			Elastic: &Elasticsearch{
				UsePipeline: "{{es-pipeline}}",
				Log:         testutil.Logger{},
			},
		},
	}

	for _, test := range tests {
		e := test.Elastic
		e.pipelineName, e.pipelineTagKeys = e.GetTagKeys(e.UsePipeline)
		pipelineName := e.getPipelineName(e.pipelineName, e.pipelineTagKeys, test.Tags)
		require.Equal(t, test.Expected, pipelineName)
	}
}

func TestRequestHeaderWhenGzipIsEnabled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_bulk":
			if contentHeader := r.Header.Get("Content-Encoding"); contentHeader != "gzip" {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", "gzip", contentHeader)
				return
			}
			if acceptHeader := r.Header.Get("Accept-Encoding"); acceptHeader != "gzip" {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", "gzip", acceptHeader)
				return
			}

			if _, err := w.Write([]byte("{}")); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
			return
		default:
			if _, err := w.Write([]byte(`{"version": {"number": "7.8"}}`)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
			return
		}
	}))
	defer ts.Close()

	urls := []string{"http://" + ts.Listener.Addr().String()}

	e := &Elasticsearch{
		URLs:           urls,
		IndexName:      "{{host}}-%Y.%m.%d",
		Timeout:        config.Duration(time.Second * 5),
		EnableGzip:     true,
		ManageTemplate: false,
		Log:            testutil.Logger{},
	}

	err := e.Connect()
	require.NoError(t, err)

	err = e.Write(testutil.MockMetrics())
	require.NoError(t, err)
}

func TestRequestHeaderWhenGzipIsDisabled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_bulk":
			if contentHeader := r.Header.Get("Content-Encoding"); contentHeader == "gzip" {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", "gzip", contentHeader)
				return
			}
			if _, err := w.Write([]byte("{}")); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
			return
		default:
			if _, err := w.Write([]byte(`{"version": {"number": "7.8"}}`)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
			return
		}
	}))
	defer ts.Close()

	urls := []string{"http://" + ts.Listener.Addr().String()}

	e := &Elasticsearch{
		URLs:           urls,
		IndexName:      "{{host}}-%Y.%m.%d",
		Timeout:        config.Duration(time.Second * 5),
		EnableGzip:     false,
		ManageTemplate: false,
		Log:            testutil.Logger{},
	}

	err := e.Connect()
	require.NoError(t, err)

	err = e.Write(testutil.MockMetrics())
	require.NoError(t, err)
}

func TestAuthorizationHeaderWhenBearerTokenIsPresent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_bulk":
			if authHeader := r.Header.Get("Authorization"); authHeader != "Bearer 0123456789abcdef" {
				w.WriteHeader(http.StatusInternalServerError)
				t.Errorf("Not equal, expected: %q, actual: %q", "Bearer 0123456789abcdef", authHeader)
				return
			}
			if _, err := w.Write([]byte("{}")); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
			return
		default:
			if _, err := w.Write([]byte(`{"version": {"number": "7.8"}}`)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
			}
			return
		}
	}))
	defer ts.Close()

	urls := []string{"http://" + ts.Listener.Addr().String()}

	e := &Elasticsearch{
		URLs:            urls,
		IndexName:       "{{host}}-%Y.%m.%d",
		Timeout:         config.Duration(time.Second * 5),
		EnableGzip:      false,
		ManageTemplate:  false,
		Log:             testutil.Logger{},
		AuthBearerToken: config.NewSecret([]byte("0123456789abcdef")),
	}

	err := e.Connect()
	require.NoError(t, err)

	err = e.Write(testutil.MockMetrics())
	require.NoError(t, err)
}

func TestStandardIndexSettings(t *testing.T) {
	e := &Elasticsearch{
		TemplateName: "test",
		IndexName:    "telegraf-%Y.%m.%d",
		Log:          testutil.Logger{},
	}
	buf, err := e.createNewTemplate("test")
	require.NoError(t, err)
	var jsonData esTemplate
	err = json.Unmarshal(buf.Bytes(), &jsonData)
	require.NoError(t, err)
	index := jsonData.Settings.Index
	require.Equal(t, "10s", index["refresh_interval"])
	require.InDelta(t, float64(5000), index["mapping.total_fields.limit"], testutil.DefaultDelta)
	require.Equal(t, "0-1", index["auto_expand_replicas"])
	require.Equal(t, "best_compression", index["codec"])
}

func TestDifferentIndexSettings(t *testing.T) {
	e := &Elasticsearch{
		TemplateName: "test",
		IndexName:    "telegraf-%Y.%m.%d",
		IndexTemplate: map[string]interface{}{
			"refresh_interval":           "20s",
			"mapping.total_fields.limit": 1000,
			"codec":                      "best_compression",
		},
		Log: testutil.Logger{},
	}
	buf, err := e.createNewTemplate("test")
	require.NoError(t, err)
	var jsonData esTemplate
	err = json.Unmarshal(buf.Bytes(), &jsonData)
	require.NoError(t, err)
	index := jsonData.Settings.Index
	require.Equal(t, "20s", index["refresh_interval"])
	require.InDelta(t, float64(1000), index["mapping.total_fields.limit"], testutil.DefaultDelta)
	require.Equal(t, "best_compression", index["codec"])
}

type esTemplate struct {
	Settings esSettings `json:"settings"`
}

type esSettings struct {
	Index map[string]interface{} `json:"index"`
}
