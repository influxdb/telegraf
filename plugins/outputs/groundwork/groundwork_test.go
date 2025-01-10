package groundwork

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gwos/tcg/sdk/clients"
	"github.com/gwos/tcg/sdk/transit"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/testutil"
)

const (
	defaultTestAgentID = "ec1676cc-583d-48ee-b035-7fb5ed0fcf88"
	defaultHost        = "telegraf"
	defaultAppType     = "TELEGRAF"
	customAppType      = "SYSLOG"
)

func TestWriteWithDebug(t *testing.T) {
	// Generate test metric with default name to test Write logic
	intMetric := testutil.TestMetric(42, "IntMetric")
	srvTok := "88fcf0de5bf7-530b-ee84-d385-cc6761ce"

	// Simulate Groundwork server that should receive custom metrics
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Decode body to use in assertions below
		var obj transit.ResourcesWithServicesRequest
		if err = json.Unmarshal(body, &obj); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Check if server gets proper data
		if obj.Resources[0].Services[0].Name != "IntMetric" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "IntMetric", obj.Resources[0].Services[0].Name)
			return
		}
		if *obj.Resources[0].Services[0].Metrics[0].Value.IntegerValue != int64(42) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %v, actual: %v", int64(42), *obj.Resources[0].Services[0].Metrics[0].Value.IntegerValue)
			return
		}

		// Send back details
		ans := "Content-type: application/json\n\n" + `{"message":"` + srvTok + `"}`
		if _, err = fmt.Fprintln(w, ans); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))

	i := Groundwork{
		Server:              server.URL,
		AgentID:             defaultTestAgentID,
		Username:            config.NewSecret([]byte(`tu ser`)),
		Password:            config.NewSecret([]byte(`pu ser`)),
		DefaultAppType:      defaultAppType,
		DefaultHost:         defaultHost,
		DefaultServiceState: string(transit.ServiceOk),
		ResourceTag:         "host",
		Log:                 testutil.Logger{},
	}

	buf := new(bytes.Buffer)
	require.NoError(t, logger.SetupLogging(&logger.Config{Debug: true}))
	logger.RedirectLogging(buf)

	require.NoError(t, i.Init())
	require.NoError(t, i.Write([]telegraf.Metric{intMetric}))

	require.NoError(t, logger.CloseLogging())
	require.Contains(t, buf.String(), defaultTestAgentID)
	require.Contains(t, buf.String(), srvTok)

	server.Close()
}

func TestWriteWithDefaults(t *testing.T) {
	// Generate test metric with default name to test Write logic
	intMetric := testutil.TestMetric(42, "IntMetric")

	// Simulate Groundwork server that should receive custom metrics
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Decode body to use in assertions below
		var obj transit.ResourcesWithServicesRequest
		if err = json.Unmarshal(body, &obj); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Check if server gets proper data
		if obj.Context.AgentID != defaultTestAgentID {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", defaultTestAgentID, obj.Context.AgentID)
			return
		}
		if obj.Context.AppType != customAppType {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", customAppType, obj.Context.AppType)
			return
		}
		if obj.Resources[0].Name != defaultHost {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", defaultHost, obj.Resources[0].Name)
			return
		}
		if obj.Resources[0].Services[0].Status != transit.MonitorStatus("SERVICE_OK") {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", transit.MonitorStatus("SERVICE_OK"), obj.Resources[0].Services[0].Status)
			return
		}
		if obj.Resources[0].Services[0].Name != "IntMetric" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "IntMetric", obj.Resources[0].Services[0].Name)
			return
		}
		if *obj.Resources[0].Services[0].Metrics[0].Value.IntegerValue != int64(42) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %v, actual: %v", int64(42), *obj.Resources[0].Services[0].Metrics[0].Value.IntegerValue)
			return
		}
		if len(obj.Groups) != 0 {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("'obj.Groups' should not be empty")
			return
		}

		if _, err = fmt.Fprintln(w, "OK"); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))

	i := Groundwork{
		Log:            testutil.Logger{},
		Server:         server.URL,
		AgentID:        defaultTestAgentID,
		DefaultHost:    defaultHost,
		DefaultAppType: customAppType,
		client: clients.GWClient{
			AppName: "telegraf",
			AppType: customAppType,
			GWConnection: &clients.GWConnection{
				HostName: server.URL,
			},
		},
	}

	err := i.Write([]telegraf.Metric{intMetric})
	require.NoError(t, err)

	server.Close()
}

func TestWriteWithFields(t *testing.T) {
	// Generate test metric with fields to test Write logic
	floatMetric := testutil.TestMetric(1.0, "FloatMetric")
	floatMetric.AddField("value_cr", 3.0)
	floatMetric.AddField("value_wn", 2.0)
	floatMetric.AddField("message", "Test Message")
	floatMetric.AddField("status", "SERVICE_WARNING")

	// Simulate Groundwork server that should receive custom metrics
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Decode body to use in assertions below
		var obj transit.ResourcesWithServicesRequest
		if err = json.Unmarshal(body, &obj); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Check if server gets proper data
		if obj.Resources[0].Services[0].LastPluginOutput != "Test Message" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "Test Message", obj.Resources[0].Services[0].LastPluginOutput)
			return
		}
		if obj.Resources[0].Services[0].Status != transit.MonitorStatus("SERVICE_WARNING") {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", transit.MonitorStatus("SERVICE_WARNING"), obj.Resources[0].Services[0].Status)
			return
		}
		if dt := float64(1.0) - *obj.Resources[0].Services[0].Metrics[0].Value.DoubleValue; !testutil.WithinDefaultDelta(dt) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Max difference between %v and %v allowed is %v, but difference was %v",
				float64(1.0), *obj.Resources[0].Services[0].Metrics[0].Value.DoubleValue, testutil.DefaultDelta, dt)
			return
		}
		if dt := float64(3.0) - *obj.Resources[0].Services[0].Metrics[0].Thresholds[0].Value.DoubleValue; !testutil.WithinDefaultDelta(dt) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Max difference between %v and %v allowed is %v, but difference was %v",
				float64(3.0), *obj.Resources[0].Services[0].Metrics[0].Thresholds[0].Value.DoubleValue, testutil.DefaultDelta, dt)
			return
		}
		if dt := float64(2.0) - *obj.Resources[0].Services[0].Metrics[0].Thresholds[1].Value.DoubleValue; !testutil.WithinDefaultDelta(dt) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Max difference between %v and %v allowed is %v, but difference was %v",
				float64(2.0), *obj.Resources[0].Services[0].Metrics[0].Thresholds[1].Value.DoubleValue, testutil.DefaultDelta, dt)
			return
		}

		if _, err = fmt.Fprintln(w, "OK"); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))

	i := Groundwork{
		Log:            testutil.Logger{},
		Server:         server.URL,
		AgentID:        defaultTestAgentID,
		DefaultHost:    defaultHost,
		DefaultAppType: defaultAppType,
		GroupTag:       "group",
		ResourceTag:    "host",
		client: clients.GWClient{
			AppName: "telegraf",
			AppType: defaultAppType,
			GWConnection: &clients.GWConnection{
				HostName: server.URL,
			},
		},
	}

	err := i.Write([]telegraf.Metric{floatMetric})
	require.NoError(t, err)

	server.Close()
}

func TestWriteWithTags(t *testing.T) {
	// Generate test metric with tags to test Write logic
	floatMetric := testutil.TestMetric(1.0, "FloatMetric")
	floatMetric.AddField("value_cr", 3.0)
	floatMetric.AddField("value_wn", 2.0)
	floatMetric.AddField("message", "Test Message")
	floatMetric.AddField("status", "SERVICE_WARNING")
	floatMetric.AddTag("value_cr", "9.0")
	floatMetric.AddTag("value_wn", "6.0")
	floatMetric.AddTag("message", "Test Tag")
	floatMetric.AddTag("status", "SERVICE_PENDING")
	floatMetric.AddTag("group-tag", "Group01")
	floatMetric.AddTag("resource-tag", "Host01")
	floatMetric.AddTag("service", "Service01")
	floatMetric.AddTag("facility", "FACILITY")
	floatMetric.AddTag("severity", "SEVERITY")

	// Simulate Groundwork server that should receive custom metrics
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Decode body to use in assertions below
		var obj transit.ResourcesWithServicesRequest
		if err = json.Unmarshal(body, &obj); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		// Check if server gets proper data
		if obj.Context.AgentID != defaultTestAgentID {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", defaultTestAgentID, obj.Context.AgentID)
			return
		}
		if obj.Context.AppType != defaultAppType {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", defaultAppType, obj.Context.AppType)
			return
		}
		if obj.Resources[0].Name != "Host01" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "Host01", obj.Resources[0].Name)
			return
		}
		if obj.Resources[0].Services[0].Name != "Service01" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "Service01", obj.Resources[0].Services[0].Name)
			return
		}
		if *obj.Resources[0].Services[0].Properties["facility"].StringValue != "FACILITY" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "FACILITY", *obj.Resources[0].Services[0].Properties["facility"].StringValue)
			return
		}
		if *obj.Resources[0].Services[0].Properties["severity"].StringValue != "SEVERITY" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "SEVERITY", *obj.Resources[0].Services[0].Properties["severity"].StringValue)
			return
		}
		if obj.Groups[0].GroupName != "Group01" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "Group01", obj.Groups[0].GroupName)
			return
		}
		if obj.Groups[0].Resources[0].Name != "Host01" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "Host01", obj.Groups[0].Resources[0].Name)
			return
		}
		if obj.Resources[0].Services[0].LastPluginOutput != "Test Tag" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "Test Tag", obj.Resources[0].Services[0].LastPluginOutput)
			return
		}
		if obj.Resources[0].Services[0].Status != transit.MonitorStatus("SERVICE_PENDING") {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", transit.MonitorStatus("SERVICE_PENDING"), obj.Resources[0].Services[0].Status)
			return
		}
		if dt := float64(1.0) - *obj.Resources[0].Services[0].Metrics[0].Value.DoubleValue; !testutil.WithinDefaultDelta(dt) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Max difference between %v and %v allowed is %v, but difference was %v",
				float64(1.0), *obj.Resources[0].Services[0].Metrics[0].Value.DoubleValue, testutil.DefaultDelta, dt)
			return
		}
		if dt := float64(9.0) - *obj.Resources[0].Services[0].Metrics[0].Thresholds[0].Value.DoubleValue; !testutil.WithinDefaultDelta(dt) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Max difference between %v and %v allowed is %v, but difference was %v",
				float64(9.0), *obj.Resources[0].Services[0].Metrics[0].Thresholds[0].Value.DoubleValue, testutil.DefaultDelta, dt)
			return
		}
		if dt := float64(6.0) - *obj.Resources[0].Services[0].Metrics[0].Thresholds[1].Value.DoubleValue; !testutil.WithinDefaultDelta(dt) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Max difference between %v and %v allowed is %v, but difference was %v",
				float64(6.0), *obj.Resources[0].Services[0].Metrics[0].Thresholds[1].Value.DoubleValue, testutil.DefaultDelta, dt)
			return
		}

		if _, err = fmt.Fprintln(w, "OK"); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))

	i := Groundwork{
		Log:            testutil.Logger{},
		Server:         server.URL,
		AgentID:        defaultTestAgentID,
		DefaultHost:    defaultHost,
		DefaultAppType: defaultAppType,
		GroupTag:       "group-tag",
		ResourceTag:    "resource-tag",
		client: clients.GWClient{
			AppName: "telegraf",
			AppType: defaultAppType,
			GWConnection: &clients.GWConnection{
				HostName: server.URL,
			},
		},
	}

	err := i.Write([]telegraf.Metric{floatMetric})
	require.NoError(t, err)

	server.Close()
}
