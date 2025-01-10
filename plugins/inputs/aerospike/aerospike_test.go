package aerospike

import (
	"fmt"
	"strconv"
	"testing"

	as "github.com/aerospike/aerospike-client-go/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf/testutil"
)

const servicePort = "3000"

func launchTestServer(t *testing.T) *testutil.Container {
	container := testutil.Container{
		Image:        "aerospike:ce-6.0.0.1",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForLog("migrations: complete"),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")

	return &container
}

func TestAerospikeStatisticsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	a := &Aerospike{
		Servers: []string{fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort])},
	}

	var acc testutil.Accumulator

	err := acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.True(t, acc.HasMeasurement("aerospike_node"))
	require.True(t, acc.HasTag("aerospike_node", "node_name"))
	require.True(t, acc.HasMeasurement("aerospike_namespace"))
	require.True(t, acc.HasTag("aerospike_namespace", "node_name"))
	require.True(t, acc.HasInt64Field("aerospike_node", "batch_index_error"))

	namespaceName := acc.TagValue("aerospike_namespace", "namespace")
	require.Equal(t, "test", namespaceName)
}

func TestAerospikeStatisticsPartialErrIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	a := &Aerospike{
		Servers: []string{
			fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort]),
			testutil.GetLocalHost() + ":9999",
		},
	}

	var acc testutil.Accumulator
	err := acc.GatherError(a.Gather)

	require.Error(t, err)

	require.True(t, acc.HasMeasurement("aerospike_node"))
	require.True(t, acc.HasMeasurement("aerospike_namespace"))
	require.True(t, acc.HasInt64Field("aerospike_node", "batch_index_error"))
	namespaceName := acc.TagSetValue("aerospike_namespace", "namespace")
	require.Equal(t, "test", namespaceName)
}

func TestSelectNamespacesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	// Select nonexistent namespace
	a := &Aerospike{
		Servers:    []string{fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort])},
		Namespaces: []string{"notTest"},
	}

	var acc testutil.Accumulator

	err := acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.True(t, acc.HasMeasurement("aerospike_node"))
	require.True(t, acc.HasTag("aerospike_node", "node_name"))
	require.True(t, acc.HasMeasurement("aerospike_namespace"))
	require.True(t, acc.HasTag("aerospike_namespace", "node_name"))

	// Expect only 1 namespace
	count := 0
	for _, p := range acc.Metrics {
		if p.Measurement == "aerospike_namespace" {
			count++
		}
	}
	require.Equal(t, 1, count)

	// expect namespace to have no fields as nonexistent
	require.False(t, acc.HasInt64Field("aerospke_namespace", "appeals_tx_remaining"))
}

func TestDisableQueryNamespacesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	a := &Aerospike{
		Servers: []string{
			fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort]),
		},
		DisableQueryNamespaces: true,
	}

	var acc testutil.Accumulator
	err := acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.True(t, acc.HasMeasurement("aerospike_node"))
	require.False(t, acc.HasMeasurement("aerospike_namespace"))

	a.DisableQueryNamespaces = false
	err = acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.True(t, acc.HasMeasurement("aerospike_node"))
	require.True(t, acc.HasMeasurement("aerospike_namespace"))
}

func TestQuerySetsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	portInt, err := strconv.Atoi(container.Ports[servicePort])
	require.NoError(t, err)

	// create a set
	// test is the default namespace from aerospike
	policy := as.NewClientPolicy()
	client, errAs := as.NewClientWithPolicy(policy, container.Address, portInt)
	require.NoError(t, errAs)

	key, errAs := as.NewKey("test", "foo", 123)
	require.NoError(t, errAs)
	bins := as.BinMap{
		"e":  2,
		"pi": 3,
	}
	errAs = client.Add(nil, key, bins)
	require.NoError(t, errAs)

	key, errAs = as.NewKey("test", "bar", 1234)
	require.NoError(t, errAs)
	bins = as.BinMap{
		"e":  2,
		"pi": 3,
	}
	errAs = client.Add(nil, key, bins)
	require.NoError(t, errAs)

	a := &Aerospike{
		Servers: []string{
			fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort]),
		},
		QuerySets:              true,
		DisableQueryNamespaces: true,
	}

	var acc testutil.Accumulator
	err = acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.True(t, FindTagValue(&acc, "aerospike_set", "set", "test/foo"))
	require.True(t, FindTagValue(&acc, "aerospike_set", "set", "test/bar"))

	require.True(t, acc.HasMeasurement("aerospike_set"))
	require.True(t, acc.HasTag("aerospike_set", "set"))
	require.True(t, acc.HasInt64Field("aerospike_set", "memory_data_bytes"))
}

func TestSelectQuerySetsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	portInt, err := strconv.Atoi(container.Ports[servicePort])
	require.NoError(t, err)

	// create a set
	// test is the default namespace from aerospike
	policy := as.NewClientPolicy()
	client, errAs := as.NewClientWithPolicy(policy, container.Address, portInt)
	require.NoError(t, errAs)

	key, errAs := as.NewKey("test", "foo", 123)
	require.NoError(t, errAs)
	bins := as.BinMap{
		"e":  2,
		"pi": 3,
	}
	errAs = client.Add(nil, key, bins)
	require.NoError(t, errAs)

	key, errAs = as.NewKey("test", "bar", 1234)
	require.NoError(t, errAs)
	bins = as.BinMap{
		"e":  2,
		"pi": 3,
	}
	errAs = client.Add(nil, key, bins)
	require.NoError(t, errAs)

	a := &Aerospike{
		Servers: []string{
			fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort]),
		},
		QuerySets:              true,
		Sets:                   []string{"test/foo"},
		DisableQueryNamespaces: true,
	}

	var acc testutil.Accumulator
	err = acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.True(t, FindTagValue(&acc, "aerospike_set", "set", "test/foo"))
	require.False(t, FindTagValue(&acc, "aerospike_set", "set", "test/bar"))

	require.True(t, acc.HasMeasurement("aerospike_set"))
	require.True(t, acc.HasTag("aerospike_set", "set"))
	require.True(t, acc.HasInt64Field("aerospike_set", "memory_data_bytes"))
}

func TestDisableTTLHistogramIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	a := &Aerospike{
		Servers: []string{
			fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort]),
		},
		QuerySets:          true,
		EnableTTLHistogram: false,
	}
	/*
		No measurement exists
	*/
	var acc testutil.Accumulator
	err := acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.False(t, acc.HasMeasurement("aerospike_histogram_ttl"))
}

func TestDisableObjectSizeLinearHistogramIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aerospike integration tests.")
	}

	container := launchTestServer(t)
	defer container.Terminate()

	a := &Aerospike{
		Servers: []string{
			fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort]),
		},
		QuerySets:                       true,
		EnableObjectSizeLinearHistogram: false,
	}
	/*
		No Measurement
	*/
	var acc testutil.Accumulator
	err := acc.GatherError(a.Gather)
	require.NoError(t, err)

	require.False(t, acc.HasMeasurement("aerospike_histogram_object_size_linear"))
}

func TestParseNodeInfo(t *testing.T) {
	stats := map[string]string{
		"statistics": "early_tsvc_from_proxy_error=0;cluster_principal=BB9020012AC4202;cluster_is_member=true",
	}

	expectedFields := map[string]interface{}{
		"early_tsvc_from_proxy_error": int64(0),
		"cluster_principal":           "BB9020012AC4202",
		"cluster_is_member":           true,
	}

	expectedTags := map[string]string{
		"aerospike_host": "127.0.0.1:3000",
		"node_name":      "TestNodeName",
	}

	var acc testutil.Accumulator
	parseNodeInfo(&acc, stats, "127.0.0.1:3000", "TestNodeName")
	acc.AssertContainsTaggedFields(t, "aerospike_node", expectedFields, expectedTags)
}

func TestParseNamespaceInfo(t *testing.T) {
	stats := map[string]string{
		"namespace/test": "ns_cluster_size=1;effective_replication_factor=1;objects=2;tombstones=0;master_objects=2",
	}

	expectedFields := map[string]interface{}{
		"ns_cluster_size":              int64(1),
		"effective_replication_factor": int64(1),
		"tombstones":                   int64(0),
		"objects":                      int64(2),
		"master_objects":               int64(2),
	}

	expectedTags := map[string]string{
		"aerospike_host": "127.0.0.1:3000",
		"node_name":      "TestNodeName",
		"namespace":      "test",
	}

	var acc testutil.Accumulator
	parseNamespaceInfo(&acc, stats, "127.0.0.1:3000", "test", "TestNodeName")
	acc.AssertContainsTaggedFields(t, "aerospike_namespace", expectedFields, expectedTags)
}

func TestParseSetInfo(t *testing.T) {
	stats := map[string]string{
		"sets/test/foo": "objects=1:tombstones=0:memory_data_bytes=26;",
	}

	expectedFields := map[string]interface{}{
		"objects":           int64(1),
		"tombstones":        int64(0),
		"memory_data_bytes": int64(26),
	}

	expectedTags := map[string]string{
		"aerospike_host": "127.0.0.1:3000",
		"node_name":      "TestNodeName",
		"set":            "test/foo",
	}

	var acc testutil.Accumulator
	parseSetInfo(&acc, stats, "127.0.0.1:3000", "test/foo", "TestNodeName")
	acc.AssertContainsTaggedFields(t, "aerospike_set", expectedFields, expectedTags)
}

func TestParseHistogramSet(t *testing.T) {
	a := &Aerospike{
		NumberHistogramBuckets: 10,
	}

	var acc testutil.Accumulator

	stats := map[string]string{
		"histogram:type=object-size-linear;namespace=test;set=foo": "units=bytes:hist-width=1048576:bucket-width=1024:buckets=0,1,3,1,6,1,9,1,12,1,15,1,18",
	}

	expectedFields := map[string]interface{}{
		"0": int64(1),
		"1": int64(4),
		"2": int64(7),
		"3": int64(10),
		"4": int64(13),
		"5": int64(16),
		"6": int64(18),
	}

	expectedTags := map[string]string{
		"aerospike_host": "127.0.0.1:3000",
		"node_name":      "TestNodeName",
		"namespace":      "test",
		"set":            "foo",
	}

	nTags := createTags("127.0.0.1:3000", "TestNodeName", "test", "foo")
	a.parseHistogram(&acc, stats, nTags, "object-size-linear")
	acc.AssertContainsTaggedFields(t, "aerospike_histogram_object_size_linear", expectedFields, expectedTags)
}

func TestParseHistogramNamespace(t *testing.T) {
	a := &Aerospike{
		NumberHistogramBuckets: 10,
	}

	var acc testutil.Accumulator

	stats := map[string]string{
		"histogram:type=object-size-linear;namespace=test;set=foo": " units=bytes:hist-width=1048576:bucket-width=1024:buckets=0,1,3,1,6,1,9,1,12,1,15,1,18",
	}

	expectedFields := map[string]interface{}{
		"0": int64(1),
		"1": int64(4),
		"2": int64(7),
		"3": int64(10),
		"4": int64(13),
		"5": int64(16),
		"6": int64(18),
	}

	expectedTags := map[string]string{
		"aerospike_host": "127.0.0.1:3000",
		"node_name":      "TestNodeName",
		"namespace":      "test",
	}

	nTags := createTags("127.0.0.1:3000", "TestNodeName", "test", "")
	a.parseHistogram(&acc, stats, nTags, "object-size-linear")
	acc.AssertContainsTaggedFields(t, "aerospike_histogram_object_size_linear", expectedFields, expectedTags)
}

func TestAerospikeParseValue(t *testing.T) {
	// uint64 with value bigger than int64 max
	val := parseAerospikeValue("", "18446744041841121751")
	require.Equal(t, uint64(18446744041841121751), val)

	val = parseAerospikeValue("", "true")
	v, ok := val.(bool)
	require.Truef(t, ok, "bool type expected, got '%T' with '%v' value instead", val, val)
	require.True(t, v)

	// int values
	val = parseAerospikeValue("", "42")
	require.Equal(t, int64(42), val, "must be parsed as an int64")

	// string values
	val = parseAerospikeValue("", "BB977942A2CA502")
	require.Equal(t, `BB977942A2CA502`, val, "must be left as a string")

	// all digit hex values, unprotected
	val = parseAerospikeValue("", "1992929191")
	require.Equal(t, int64(1992929191), val, "must be parsed as an int64")

	// all digit hex values, protected
	val = parseAerospikeValue("node_name", "1992929191")
	require.Equal(t, `1992929191`, val, "must be left as a string")
}

func FindTagValue(acc *testutil.Accumulator, measurement, key, value string) bool {
	for _, p := range acc.Metrics {
		if p.Measurement == measurement {
			v, ok := p.Tags[key]
			if ok && v == value {
				return true
			}
		}
	}
	return false
}
