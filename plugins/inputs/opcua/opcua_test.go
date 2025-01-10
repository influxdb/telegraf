package opcua

import (
	"fmt"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/common/opcua"
	"github.com/influxdata/telegraf/plugins/common/opcua/input"
	"github.com/influxdata/telegraf/testutil"
)

const servicePort = "4840"

type opcTags struct {
	name           string
	namespace      string
	identifierType string
	identifier     string
	want           interface{}
}

func mapOPCTag(tags opcTags) (out input.NodeSettings) {
	out.FieldName = tags.name
	out.Namespace = tags.namespace
	out.IdentifierType = tags.identifierType
	out.Identifier = tags.identifier
	return out
}

func TestGetDataBadNodeContainerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := testutil.Container{
		Image:        "open62541/open62541",
		ExposedPorts: []string{servicePort},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort(nat.Port(servicePort)),
			wait.ForLog("TCP network layer listening on opc.tcp://"),
		),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")
	defer container.Terminate()

	testopctags := []opcTags{
		{"ProductName", "1", "i", "2261", "open62541 OPC UA Server"},
		{"ProductUri", "0", "i", "2262", "http://open62541.org"},
		{"ManufacturerName", "0", "i", "2263", "open62541"},
	}

	readConfig := readClientConfig{
		InputClientConfig: input.InputClientConfig{
			OpcUAClientConfig: opcua.OpcUAClientConfig{
				Endpoint:       fmt.Sprintf("opc.tcp://%s:%s", container.Address, container.Ports[servicePort]),
				SecurityPolicy: "None",
				SecurityMode:   "None",
				AuthMethod:     "Anonymous",
				ConnectTimeout: config.Duration(10 * time.Second),
				RequestTimeout: config.Duration(1 * time.Second),
				Workarounds:    opcua.OpcUAWorkarounds{},
			},
			MetricName: "testing",
			RootNodes:  make([]input.NodeSettings, 0),
			Groups:     make([]input.NodeGroupSettings, 0),
		},
	}

	g := input.NodeGroupSettings{
		MetricName: "anodic_current",
		TagsSlice: [][]string{
			{"pot", "2002"},
		},
	}

	for _, tags := range testopctags {
		g.Nodes = append(g.Nodes, mapOPCTag(tags))
	}
	readConfig.Groups = append(readConfig.Groups, g)

	logger := &testutil.CaptureLogger{}
	readClient, err := readConfig.createReadClient(logger)
	require.NoError(t, err)
	err = readClient.connect()
	require.NoError(t, err)
}

func TestReadClientIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := testutil.Container{
		Image:        "open62541/open62541",
		ExposedPorts: []string{servicePort},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort(nat.Port(servicePort)),
			wait.ForLog("TCP network layer listening on opc.tcp://"),
		),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")
	defer container.Terminate()

	testopctags := []opcTags{
		{"ProductName", "0", "i", "2261", "open62541 OPC UA Server"},
		{"ProductUri", "0", "i", "2262", "http://open62541.org"},
		{"ManufacturerName", "0", "i", "2263", "open62541"},
		{"badnode", "1", "i", "1337", nil},
		{"goodnode", "1", "s", "the.answer", int32(42)},
		{"DateTime", "1", "i", "51037", "0001-01-01T00:00:00Z"},
	}

	readConfig := readClientConfig{
		InputClientConfig: input.InputClientConfig{
			OpcUAClientConfig: opcua.OpcUAClientConfig{
				Endpoint:       fmt.Sprintf("opc.tcp://%s:%s", container.Address, container.Ports[servicePort]),
				SecurityPolicy: "None",
				SecurityMode:   "None",
				AuthMethod:     "Anonymous",
				ConnectTimeout: config.Duration(10 * time.Second),
				RequestTimeout: config.Duration(1 * time.Second),
				Workarounds:    opcua.OpcUAWorkarounds{},
			},
			MetricName: "testing",
			RootNodes:  make([]input.NodeSettings, 0),
			Groups:     make([]input.NodeGroupSettings, 0),
		},
	}

	for _, tags := range testopctags {
		readConfig.RootNodes = append(readConfig.RootNodes, mapOPCTag(tags))
	}

	client, err := readConfig.createReadClient(testutil.Logger{})
	require.NoError(t, err)

	err = client.connect()
	require.NoError(t, err)

	for i, v := range client.LastReceivedData {
		require.Equal(t, testopctags[i].want, v.Value)
	}
}

func TestReadClientIntegrationAdditionalFields(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := testutil.Container{
		Image:        "open62541/open62541",
		ExposedPorts: []string{servicePort},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort(nat.Port(servicePort)),
			wait.ForLog("TCP network layer listening on opc.tcp://"),
		),
	}
	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	testopctags := []opcTags{
		{"ProductName", "0", "i", "2261", "open62541 OPC UA Server"},
		{"ProductUri", "0", "i", "2262", "http://open62541.org"},
		{"ManufacturerName", "0", "i", "2263", "open62541"},
		{"badnode", "1", "i", "1337", nil},
		{"goodnode", "1", "s", "the.answer", int32(42)},
		{"DateTime", "1", "i", "51037", "0001-01-01T00:00:00Z"},
	}
	testopctypes := []string{
		"String",
		"String",
		"String",
		"Null",
		"Int32",
		"DateTime",
	}
	testopcquality := []string{
		"The operation succeeded. StatusGood (0x0)",
		"The operation succeeded. StatusGood (0x0)",
		"The operation succeeded. StatusGood (0x0)",
		"User does not have permission to perform the requested operation. StatusBadUserAccessDenied (0x801F0000)",
		"The operation succeeded. StatusGood (0x0)",
		"The operation succeeded. StatusGood (0x0)",
	}
	expectedopcmetrics := make([]telegraf.Metric, 0, len(testopctags))
	for i, x := range testopctags {
		now := time.Now()
		tags := map[string]string{
			"id": fmt.Sprintf("ns=%s;%s=%s", x.namespace, x.identifierType, x.identifier),
		}
		fields := map[string]interface{}{
			x.name:     x.want,
			"Quality":  testopcquality[i],
			"DataType": testopctypes[i],
		}
		expectedopcmetrics = append(expectedopcmetrics, metric.New("testing", tags, fields, now))
	}

	readConfig := readClientConfig{
		InputClientConfig: input.InputClientConfig{
			OpcUAClientConfig: opcua.OpcUAClientConfig{
				Endpoint:       fmt.Sprintf("opc.tcp://%s:%s", container.Address, container.Ports[servicePort]),
				SecurityPolicy: "None",
				SecurityMode:   "None",
				AuthMethod:     "Anonymous",
				ConnectTimeout: config.Duration(10 * time.Second),
				RequestTimeout: config.Duration(1 * time.Second),
				Workarounds:    opcua.OpcUAWorkarounds{},
				OptionalFields: []string{"DataType"},
			},
			MetricName: "testing",
			RootNodes:  make([]input.NodeSettings, 0),
			Groups:     make([]input.NodeGroupSettings, 0),
		},
	}

	for _, tags := range testopctags {
		readConfig.RootNodes = append(readConfig.RootNodes, mapOPCTag(tags))
	}

	client, err := readConfig.createReadClient(testutil.Logger{})
	require.NoError(t, err)

	require.NoError(t, client.connect())

	actualopcmetrics := make([]telegraf.Metric, 0, len(client.LastReceivedData))
	for i := range client.LastReceivedData {
		actualopcmetrics = append(actualopcmetrics, client.MetricForNode(i))
	}
	testutil.RequireMetricsEqual(t, expectedopcmetrics, actualopcmetrics, testutil.IgnoreTime())
}

func TestReadClientIntegrationWithPasswordAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := testutil.Container{
		Image:        "open62541/open62541",
		Entrypoint:   []string{"/opt/open62541/build/bin/examples/access_control_server"},
		ExposedPorts: []string{servicePort},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort(nat.Port(servicePort)),
			wait.ForLog("TCP network layer listening on opc.tcp://"),
		),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")
	defer container.Terminate()

	testopctags := []opcTags{
		{"ProductName", "0", "i", "2261", "open62541 OPC UA Server"},
		{"ProductUri", "0", "i", "2262", "http://open62541.org"},
		{"ManufacturerName", "0", "i", "2263", "open62541"},
	}

	readConfig := readClientConfig{
		InputClientConfig: input.InputClientConfig{
			OpcUAClientConfig: opcua.OpcUAClientConfig{
				Endpoint:       fmt.Sprintf("opc.tcp://%s:%s", container.Address, container.Ports[servicePort]),
				SecurityPolicy: "None",
				SecurityMode:   "None",
				Username:       config.NewSecret([]byte("peter")),
				Password:       config.NewSecret([]byte("peter123")),
				AuthMethod:     "UserName",
				ConnectTimeout: config.Duration(10 * time.Second),
				RequestTimeout: config.Duration(1 * time.Second),
				Workarounds:    opcua.OpcUAWorkarounds{},
			},
			MetricName: "testing",
			RootNodes:  make([]input.NodeSettings, 0),
			Groups:     make([]input.NodeGroupSettings, 0),
		},
	}

	for _, tags := range testopctags {
		readConfig.RootNodes = append(readConfig.RootNodes, mapOPCTag(tags))
	}

	client, err := readConfig.createReadClient(testutil.Logger{})
	require.NoError(t, err)

	err = client.connect()
	require.NoError(t, err)

	for i, v := range client.LastReceivedData {
		require.Equal(t, testopctags[i].want, v.Value)
	}
}

func TestReadClientConfig(t *testing.T) {
	toml := `
[[inputs.opcua]]
name = "localhost"
endpoint = "opc.tcp://localhost:4840"
connect_timeout = "10s"
request_timeout = "5s"
security_policy = "auto"
security_mode = "auto"
certificate = "/etc/telegraf/cert.pem"
private_key = "/etc/telegraf/key.pem"
auth_method = "Anonymous"
username = ""
password = ""

optional_fields = ["DataType"]

[[inputs.opcua.nodes]]
  name = "name"
  namespace = "1"
  identifier_type = "s"
  identifier="one"
  tags=[["tag0", "val0"]]

[[inputs.opcua.nodes]]
  name="name2"
  namespace="2"
  identifier_type="s"
  identifier="two"
  tags=[["tag0", "val0"], ["tag00", "val00"]]
  default_tags = {tag6 = "val6"}

[[inputs.opcua.group]]
name = "foo"
namespace = "3"
identifier_type = "i"
tags = [["tag1", "val1"], ["tag2", "val2"]]
nodes = [{name="name3", identifier="3000", tags=[["tag3", "val3"]]}]

[[inputs.opcua.group]]
name = "bar"
namespace = "0"
identifier_type = "i"
tags = [["tag1", "val1"], ["tag2", "val2"]]
[[inputs.opcua.group.nodes]]
  name = "name4"
  identifier = "4000"
  tags=[["tag4", "val4"]]
  default_tags = { tag1 = "override" }

[[inputs.opcua.group.nodes]]
  name = "name5"
  identifier = "4001"

[inputs.opcua.workarounds]
additional_valid_status_codes = ["0xC0"]

[inputs.opcua.request_workarounds]
use_unregistered_reads = true

`

	c := config.NewConfig()
	err := c.LoadConfigData([]byte(toml))
	require.NoError(t, err)

	require.Len(t, c.Inputs, 1)

	o, ok := c.Inputs[0].Input.(*OpcUA)
	require.True(t, ok)

	require.Equal(t, "localhost", o.readClientConfig.MetricName)
	require.Equal(t, "opc.tcp://localhost:4840", o.readClientConfig.Endpoint)
	require.Equal(t, config.Duration(10*time.Second), o.readClientConfig.ConnectTimeout)
	require.Equal(t, config.Duration(5*time.Second), o.readClientConfig.RequestTimeout)
	require.Equal(t, "auto", o.readClientConfig.SecurityPolicy)
	require.Equal(t, "auto", o.readClientConfig.SecurityMode)
	require.Equal(t, "/etc/telegraf/cert.pem", o.readClientConfig.Certificate)
	require.Equal(t, "/etc/telegraf/key.pem", o.readClientConfig.PrivateKey)
	require.Equal(t, "Anonymous", o.readClientConfig.AuthMethod)
	require.True(t, o.readClientConfig.Username.Empty())
	require.True(t, o.readClientConfig.Password.Empty())
	require.Equal(t, []input.NodeSettings{
		{
			FieldName:      "name",
			Namespace:      "1",
			IdentifierType: "s",
			Identifier:     "one",
			TagsSlice:      [][]string{{"tag0", "val0"}},
		},
		{
			FieldName:      "name2",
			Namespace:      "2",
			IdentifierType: "s",
			Identifier:     "two",
			TagsSlice:      [][]string{{"tag0", "val0"}, {"tag00", "val00"}},
			DefaultTags:    map[string]string{"tag6": "val6"},
		},
	}, o.readClientConfig.RootNodes)
	require.Equal(t, []input.NodeGroupSettings{
		{
			MetricName:     "foo",
			Namespace:      "3",
			IdentifierType: "i",
			TagsSlice:      [][]string{{"tag1", "val1"}, {"tag2", "val2"}},
			Nodes: []input.NodeSettings{{
				FieldName:  "name3",
				Identifier: "3000",
				TagsSlice:  [][]string{{"tag3", "val3"}},
			}},
		},
		{
			MetricName:     "bar",
			Namespace:      "0",
			IdentifierType: "i",
			TagsSlice:      [][]string{{"tag1", "val1"}, {"tag2", "val2"}},
			Nodes: []input.NodeSettings{{
				FieldName:   "name4",
				Identifier:  "4000",
				TagsSlice:   [][]string{{"tag4", "val4"}},
				DefaultTags: map[string]string{"tag1": "override"},
			}, {
				FieldName:  "name5",
				Identifier: "4001",
			}},
		},
	}, o.readClientConfig.Groups)
	require.Equal(t, opcua.OpcUAWorkarounds{AdditionalValidStatusCodes: []string{"0xC0"}}, o.readClientConfig.Workarounds)
	require.Equal(t, readClientWorkarounds{UseUnregisteredReads: true}, o.readClientConfig.ReadClientWorkarounds)
	require.Equal(t, []string{"DataType"}, o.readClientConfig.OptionalFields)
	err = o.Init()
	require.NoError(t, err)
	require.Len(t, o.client.NodeMetricMapping, 5, "incorrect number of nodes")
	require.EqualValues(t, map[string]string{"tag0": "val0"}, o.client.NodeMetricMapping[0].MetricTags)
	require.EqualValues(t, map[string]string{"tag6": "val6"}, o.client.NodeMetricMapping[1].MetricTags)
	require.EqualValues(t, map[string]string{"tag1": "val1", "tag2": "val2", "tag3": "val3"}, o.client.NodeMetricMapping[2].MetricTags)
	require.EqualValues(t, map[string]string{"tag1": "override", "tag2": "val2"}, o.client.NodeMetricMapping[3].MetricTags)
	require.EqualValues(t, map[string]string{"tag1": "val1", "tag2": "val2"}, o.client.NodeMetricMapping[4].MetricTags)
}
