package jti_openconfig_telemetry

import (
	"context"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/influxdata/telegraf/config"
	telemetry "github.com/influxdata/telegraf/plugins/inputs/jti_openconfig_telemetry/oc"
	"github.com/influxdata/telegraf/testutil"
)

var cfg = &OpenConfigTelemetry{
	Log:             testutil.Logger{},
	Servers:         []string{"127.0.0.1:50051"},
	SampleFrequency: config.Duration(time.Millisecond * 10),
}

var data = &telemetry.OpenConfigData{
	Path: "/sensor",
	Kv:   []*telemetry.KeyValue{{Key: "/sensor[tag='tagValue']/intKey", Value: &telemetry.KeyValue_IntValue{IntValue: 10}}},
}

var dataWithPrefix = &telemetry.OpenConfigData{
	Path: "/sensor_with_prefix",
	Kv: []*telemetry.KeyValue{{Key: "__prefix__", Value: &telemetry.KeyValue_StrValue{StrValue: "/sensor/prefix/"}},
		{Key: "intKey", Value: &telemetry.KeyValue_IntValue{IntValue: 10}}},
}

var dataWithMultipleTags = &telemetry.OpenConfigData{
	Path: "/sensor_with_multiple_tags",
	Kv: []*telemetry.KeyValue{{Key: "__prefix__", Value: &telemetry.KeyValue_StrValue{StrValue: "/sensor/prefix/"}},
		{Key: "tagKey[tag='tagValue']/boolKey", Value: &telemetry.KeyValue_BoolValue{BoolValue: false}},
		{Key: "intKey", Value: &telemetry.KeyValue_IntValue{IntValue: 10}}},
}

var dataWithStringValues = &telemetry.OpenConfigData{
	Path: "/sensor_with_string_values",
	Kv: []*telemetry.KeyValue{{Key: "__prefix__", Value: &telemetry.KeyValue_StrValue{StrValue: "/sensor/prefix/"}},
		{Key: "strKey[tag='tagValue']/strValue", Value: &telemetry.KeyValue_StrValue{StrValue: "10"}}},
}

var dataWithTimestamp = &telemetry.OpenConfigData{
	Path: "/sensor_with_timestamp",
	Kv: []*telemetry.KeyValue{
		{Key: "/sensor[tag='tagValue']/intKey", Value: &telemetry.KeyValue_IntValue{IntValue: 10}},
	},
	Timestamp: 1676560510002,
}

type openConfigTelemetryServer struct {
	telemetry.UnimplementedOpenConfigTelemetryServer
}

func (*openConfigTelemetryServer) TelemetrySubscribe(req *telemetry.SubscriptionRequest, stream telemetry.OpenConfigTelemetry_TelemetrySubscribeServer) error {
	path := req.PathList[0].Path
	switch path {
	case "/sensor":
		return stream.Send(data)
	case "/sensor_with_prefix":
		return stream.Send(dataWithPrefix)
	case "/sensor_with_multiple_tags":
		return stream.Send(dataWithMultipleTags)
	case "/sensor_with_string_values":
		return stream.Send(dataWithStringValues)
	case "/sensor_with_timestamp":
		return stream.Send(dataWithTimestamp)
	}
	return nil
}

func (*openConfigTelemetryServer) CancelTelemetrySubscription(
	context.Context,
	*telemetry.CancelSubscriptionRequest,
) (*telemetry.CancelSubscriptionReply, error) {
	return nil, nil
}

func (*openConfigTelemetryServer) GetTelemetrySubscriptions(
	context.Context,
	*telemetry.GetSubscriptionsRequest,
) (*telemetry.GetSubscriptionsReply, error) {
	return nil, nil
}

func (*openConfigTelemetryServer) GetTelemetryOperationalState(
	context.Context,
	*telemetry.GetOperationalStateRequest,
) (*telemetry.GetOperationalStateReply, error) {
	return nil, nil
}

func (*openConfigTelemetryServer) GetDataEncodings(context.Context, *telemetry.DataEncodingRequest) (*telemetry.DataEncodingReply, error) {
	return nil, nil
}

func newServer() *openConfigTelemetryServer {
	s := new(openConfigTelemetryServer)
	return s
}

func TestOpenConfigTelemetryData(t *testing.T) {
	var acc testutil.Accumulator

	cfg.Sensors = []string{"/sensor"}
	err := cfg.Start(&acc)
	require.NoError(t, err)

	tags := map[string]string{
		"device":       "127.0.0.1",
		"/sensor/@tag": "tagValue",
		"system_id":    "",
		"path":         "/sensor",
	}

	fields := map[string]interface{}{
		"/sensor/intKey":   int64(10),
		"_sequence":        uint64(0),
		"_timestamp":       uint64(0),
		"_component_id":    uint32(0),
		"_subcomponent_id": uint32(0),
	}

	require.Eventually(t, func() bool { return acc.HasMeasurement("/sensor") }, 5*time.Second, 10*time.Millisecond)
	acc.AssertContainsTaggedFields(t, "/sensor", fields, tags)
}

func TestOpenConfigTelemetryData_timestamp(t *testing.T) {
	var acc testutil.Accumulator
	cfg.Sensors = []string{"/sensor_with_timestamp"}
	require.NoError(t, cfg.Start(&acc))

	timestamp := int64(1676560510002)
	tags := map[string]string{
		"device":       "127.0.0.1",
		"/sensor/@tag": "tagValue",
		"system_id":    "",
		"path":         "/sensor_with_timestamp",
	}
	fields := map[string]interface{}{
		"/sensor/intKey":   int64(10),
		"_sequence":        uint64(0),
		"_timestamp":       uint64(timestamp),
		"_component_id":    uint32(0),
		"_subcomponent_id": uint32(0),
	}

	require.Eventually(t, func() bool { return acc.HasMeasurement("/sensor_with_timestamp") }, 5*time.Second, 10*time.Millisecond)
	acc.AssertContainsTaggedFields(t, "/sensor_with_timestamp", fields, tags)
}

func TestOpenConfigTelemetryDataWithPrefix(t *testing.T) {
	var acc testutil.Accumulator
	cfg.Sensors = []string{"/sensor_with_prefix"}
	err := cfg.Start(&acc)
	require.NoError(t, err)

	tags := map[string]string{
		"device":    "127.0.0.1",
		"system_id": "",
		"path":      "/sensor_with_prefix",
	}

	fields := map[string]interface{}{
		"/sensor/prefix/intKey": int64(10),
		"_sequence":             uint64(0),
		"_timestamp":            uint64(0),
		"_component_id":         uint32(0),
		"_subcomponent_id":      uint32(0),
	}

	require.Eventually(t, func() bool { return acc.HasMeasurement("/sensor_with_prefix") }, 5*time.Second, 10*time.Millisecond)
	acc.AssertContainsTaggedFields(t, "/sensor_with_prefix", fields, tags)
}

func TestOpenConfigTelemetryDataWithMultipleTags(t *testing.T) {
	var acc testutil.Accumulator
	cfg.Sensors = []string{"/sensor_with_multiple_tags"}
	err := cfg.Start(&acc)
	require.NoError(t, err)

	tags1 := map[string]string{
		"/sensor/prefix/tagKey/@tag": "tagValue",
		"device":                     "127.0.0.1",
		"system_id":                  "",
		"path":                       "/sensor_with_multiple_tags",
	}

	fields1 := map[string]interface{}{
		"/sensor/prefix/tagKey/boolKey": false,
		"_sequence":                     uint64(0),
		"_timestamp":                    uint64(0),
		"_component_id":                 uint32(0),
		"_subcomponent_id":              uint32(0),
	}

	tags2 := map[string]string{
		"device":    "127.0.0.1",
		"system_id": "",
		"path":      "/sensor_with_multiple_tags",
	}

	fields2 := map[string]interface{}{
		"/sensor/prefix/intKey": int64(10),
		"_sequence":             uint64(0),
		"_timestamp":            uint64(0),
		"_component_id":         uint32(0),
		"_subcomponent_id":      uint32(0),
	}

	require.Eventually(t, func() bool { return acc.HasMeasurement("/sensor_with_multiple_tags") }, 5*time.Second, 10*time.Millisecond)
	acc.AssertContainsTaggedFields(t, "/sensor_with_multiple_tags", fields1, tags1)
	acc.AssertContainsTaggedFields(t, "/sensor_with_multiple_tags", fields2, tags2)
}

func TestOpenConfigTelemetryDataWithStringValues(t *testing.T) {
	var acc testutil.Accumulator
	cfg.Sensors = []string{"/sensor_with_string_values"}
	err := cfg.Start(&acc)
	require.NoError(t, err)

	tags := map[string]string{
		"/sensor/prefix/strKey/@tag": "tagValue",
		"device":                     "127.0.0.1",
		"system_id":                  "",
		"path":                       "/sensor_with_string_values",
	}

	fields := map[string]interface{}{
		"/sensor/prefix/strKey/strValue": "10",
		"_sequence":                      uint64(0),
		"_timestamp":                     uint64(0),
		"_component_id":                  uint32(0),
		"_subcomponent_id":               uint32(0),
	}

	require.Eventually(t, func() bool { return acc.HasMeasurement("/sensor_with_string_values") }, 5*time.Second, 10*time.Millisecond)
	acc.AssertContainsTaggedFields(t, "/sensor_with_string_values", fields, tags)
}

func TestMain(m *testing.M) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	cfg.Servers = []string{lis.Addr().String()}

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	telemetry.RegisterOpenConfigTelemetryServer(grpcServer, newServer())
	go func() {
		grpcServer.Serve(lis) //nolint:errcheck // ignore the returned error as the tests will fail anyway
	}()
	defer grpcServer.Stop()

	exitCode = m.Run()
}
