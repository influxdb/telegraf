package huebridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
	"github.com/influxdata/toml"
	"github.com/tdrn-org/go-hue/mock"

	"github.com/stretchr/testify/require"
)

func TestInitDefaults(t *testing.T) {
	plugin := defaultHueBridge()
	err := plugin.Init()
	require.NoError(t, err)
	require.NotNil(t, plugin.Bridges)
	require.Equal(t, 0, len(plugin.Bridges))
	require.False(t, plugin.CloudInsecureSkipVerify)
	require.Empty(t, plugin.RemoteClientId)
	require.Empty(t, plugin.RemoteClientSecret)
	require.Empty(t, plugin.RemoteCallbackUrl)
	require.Empty(t, plugin.RemoteTokenDir)
	require.False(t, plugin.RemoteInsecureSkipVerify)
	require.NotNil(t, plugin.RoomAssignments)
	require.Equal(t, 0, len(plugin.RoomAssignments))
	require.Equal(t, defaultTimeout, plugin.Timeout)
	require.False(t, plugin.Debug)
	require.NotNil(t, plugin.Log)
}

func TestConfig(t *testing.T) {
	conf, err := os.ReadFile("testdata/huebridge.conf")
	require.NoError(t, err)
	var plugin = &HueBridge{}
	err = toml.Unmarshal(conf, plugin)
	require.NoError(t, err)
	require.NotNil(t, plugin.Bridges)
	require.Equal(t, 4, len(plugin.Bridges))
	require.True(t, plugin.CloudInsecureSkipVerify)
	require.NotEmpty(t, plugin.RemoteClientId)
	require.NotEmpty(t, plugin.RemoteClientSecret)
	require.NotEmpty(t, plugin.RemoteCallbackUrl)
	require.NotEmpty(t, plugin.RemoteTokenDir)
	require.True(t, plugin.RemoteInsecureSkipVerify)
	require.NotNil(t, plugin.RoomAssignments)
	require.Equal(t, 2, len(plugin.RoomAssignments))
	require.Equal(t, config.Duration(60*time.Second), plugin.Timeout)
	require.True(t, plugin.Debug)
}

func TestInitBridges(t *testing.T) {
	// Start mock server
	bridgeMock := mock.Start()
	require.NotNil(t, bridgeMock)
	defer bridgeMock.Shutdown()
	// Prepare token dir
	tokenDir, err := os.MkdirTemp("", "TestInitBridges")
	require.NoError(t, err)
	tokenFile := filepath.Join(tokenDir, mock.MockClientId, strings.ToUpper(mock.MockBridgeId)+".json")
	bridgeMock.WriteTokenFile(tokenFile)
	// Actual test
	plugin := defaultHueBridge()
	plugin.Bridges = append(plugin.Bridges, fmt.Sprintf("address://%s:%s@%s/", mock.MockBridgeId, mock.MockBridgeUsername, bridgeMock.Server().Host))
	plugin.Bridges = append(plugin.Bridges, fmt.Sprintf("cloud://%s:%s@%s/discovery", mock.MockBridgeId, mock.MockBridgeUsername, bridgeMock.Server().Host))
	plugin.Bridges = append(plugin.Bridges, fmt.Sprintf("mdns://%s:%s@/", mock.MockBridgeId, mock.MockBridgeUsername))
	plugin.Bridges = append(plugin.Bridges, fmt.Sprintf("remote://%s:%s@%s/", mock.MockBridgeId, mock.MockBridgeUsername, bridgeMock.Server().Host))
	plugin.CloudInsecureSkipVerify = true
	plugin.RemoteClientId = mock.MockClientId
	plugin.RemoteClientSecret = mock.MockClientSecret
	plugin.RemoteTokenDir = tokenDir
	plugin.RemoteInsecureSkipVerify = true
	plugin.Debug = true
	err = plugin.Init()
	require.NoError(t, err)
}

func TestGatherLocal(t *testing.T) {
	// Start mock server
	bridgeMock := mock.Start()
	require.NotNil(t, bridgeMock)
	defer bridgeMock.Shutdown()
	// Actual test
	plugin := defaultHueBridge()
	plugin.Bridges = append(plugin.Bridges, fmt.Sprintf("address://%s:%s@%s/", mock.MockBridgeId, mock.MockBridgeUsername, bridgeMock.Server().Host))
	plugin.RoomAssignments = [][]string{{"Name#7", "Name#15"}}
	plugin.Debug = true
	err := plugin.Init()
	require.NoError(t, err)
	var acc testutil.Accumulator
	err = acc.GatherError(plugin.Gather)
	require.NoError(t, err)
	testMeasurement(t, &acc, "huebridge_light", []string{"huebridge_bridge_id", "huebridge_room", "huebridge_device"}, []string{"on"})
	testMeasurement(t, &acc, "huebridge_temperature", []string{"huebridge_bridge_id", "huebridge_room", "huebridge_device", "huebridge_device_enabled"}, []string{"temperature"})
	testMeasurement(t, &acc, "huebridge_light_level", []string{"huebridge_bridge_id", "huebridge_room", "huebridge_device", "huebridge_device_enabled"}, []string{"light_level", "light_level_lux"})
	testMeasurement(t, &acc, "huebridge_motion_sensor", []string{"huebridge_bridge_id", "huebridge_room", "huebridge_device", "huebridge_device_enabled"}, []string{"motion"})
	testMeasurement(t, &acc, "huebridge_device_power", []string{"huebridge_bridge_id", "huebridge_room", "huebridge_device"}, []string{"battery_level", "battery_state"})
}

func testMeasurement(t *testing.T, acc *testutil.Accumulator, measurement string, tags []string, fields []string) {
	require.Truef(t, acc.HasMeasurement(measurement), "measurement: %s", measurement)
	for _, tag := range tags {
		require.Truef(t, acc.HasTag(measurement, tag), "measurement: %s tag: %s", measurement, tag)
	}
	for _, field := range fields {
		require.True(t, acc.HasField(measurement, field), "measurement: %s field: %s", measurement, field)
	}
}
