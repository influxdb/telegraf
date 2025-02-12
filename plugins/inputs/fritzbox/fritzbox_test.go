package fritzbox

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
	"github.com/tdrn-org/go-tr064/mock"
)

func TestInitDefaults(t *testing.T) {
	plugin := defaultFritzbox()
	err := plugin.Init()
	require.NoError(t, err)
	require.Len(t, plugin.URLs, 0)
	require.Equal(t, []string{"device", "wan", "ppp", "dsl", "wlan"}, plugin.Collect)
	require.Equal(t, defaultTimeout, plugin.Timeout)
	require.Empty(t, plugin.TLSCA)
	require.Empty(t, plugin.TLSCert)
	require.Empty(t, plugin.TLSKey)
	require.Empty(t, plugin.TLSKeyPwd)
	require.False(t, plugin.InsecureSkipVerify)
}

func TestConfig(t *testing.T) {
	conf := config.NewConfig()
	err := conf.LoadConfig("testdata/conf/fritzbox.conf")
	require.NoError(t, err)
	require.Len(t, conf.Inputs, 1)
	plugin, ok := conf.Inputs[0].Input.(*Fritzbox)
	require.True(t, ok)
	require.Len(t, plugin.URLs, 2)
	require.Equal(t, []string{"device", "wan", "ppp", "dsl", "wlan", "hosts"}, plugin.Collect)
	require.Equal(t, config.Duration(60*time.Second), plugin.Timeout)
	require.Equal(t, "secret", plugin.TLSKeyPwd)
	require.True(t, plugin.InsecureSkipVerify)
}

const mockDocsDir = "testdata/mock"

var testMocks = []*mock.ServiceMock{
	mock.ServiceMockFromFile("/deviceinfo", "testdata/mock/DeviceInfo.xml"),
	mock.ServiceMockFromFile("/wancommonifconfig", "testdata/mock/WANCommonInterfaceConfig1.xml"),
	mock.ServiceMockFromFile("/WANCommonIFC1", "testdata/mock/WANCommonInterfaceConfig2.xml"),
	mock.ServiceMockFromFile("/wanpppconn", "testdata/mock/WANPPPConnection.xml"),
	mock.ServiceMockFromFile("/wandslifconfig", "testdata/mock/WANDSLInterfaceConfig.xml"),
	mock.ServiceMockFromFile("/wlanconfig", "testdata/mock/WLANConfiguration.xml"),
	mock.ServiceMockFromFile("/hosts", "testdata/mock/Hosts.xml"),
}

func TestGatherDeviceInfo(t *testing.T) {
	// Start mock server
	tr064Server := mock.Start(mockDocsDir, testMocks...)
	defer tr064Server.Shutdown()
	// Actual test
	plugin := defaultFritzbox()
	plugin.URLs = append(plugin.URLs, tr064Server.Server().String())
	plugin.Collect = []string{"device"}
	plugin.Log = testutil.Logger{Name: pluginName}
	err := plugin.Init()
	require.NoError(t, err)
	acc := &testutil.Accumulator{}
	err = acc.GatherError(plugin.Gather)
	require.NoError(t, err)
	testMetric(t, acc, "testdata/metrics/fritzbox_device.txt", telegraf.Untyped)
}

func TestGatherWanInfo(t *testing.T) {
	// Start mock server
	tr064Server := mock.Start(mockDocsDir, testMocks...)
	defer tr064Server.Shutdown()
	// Actual test
	plugin := defaultFritzbox()
	plugin.URLs = append(plugin.URLs, tr064Server.Server().String())
	plugin.Collect = []string{"wan"}
	plugin.Log = testutil.Logger{Name: pluginName}
	err := plugin.Init()
	require.NoError(t, err)
	acc := &testutil.Accumulator{}
	err = acc.GatherError(plugin.Gather)
	require.NoError(t, err)
	testMetric(t, acc, "testdata/metrics/fritzbox_wan.txt", telegraf.Untyped)
}

func TestGatherPppInfo(t *testing.T) {
	// Start mock server
	tr064Server := mock.Start(mockDocsDir, testMocks...)
	defer tr064Server.Shutdown()
	// Actual test
	plugin := defaultFritzbox()
	plugin.URLs = append(plugin.URLs, tr064Server.Server().String())
	plugin.Collect = []string{"ppp"}
	plugin.Log = testutil.Logger{Name: pluginName}
	err := plugin.Init()
	require.NoError(t, err)
	acc := &testutil.Accumulator{}
	err = acc.GatherError(plugin.Gather)
	require.NoError(t, err)
	testMetric(t, acc, "testdata/metrics/fritzbox_ppp.txt", telegraf.Untyped)
}

func TestGatherDslInfo(t *testing.T) {
	// Start mock server
	tr064Server := mock.Start(mockDocsDir, testMocks...)
	defer tr064Server.Shutdown()
	// Actual test
	plugin := defaultFritzbox()
	plugin.URLs = append(plugin.URLs, tr064Server.Server().String())
	plugin.Collect = []string{"dsl"}
	plugin.Log = testutil.Logger{Name: pluginName}
	err := plugin.Init()
	require.NoError(t, err)
	acc := &testutil.Accumulator{}
	err = acc.GatherError(plugin.Gather)
	require.NoError(t, err)
	testMetric(t, acc, "testdata/metrics/fritzbox_dsl.txt", telegraf.Untyped)
}

func TestGatherWlanInfo(t *testing.T) {
	// Start mock server
	tr064Server := mock.Start(mockDocsDir, testMocks...)
	defer tr064Server.Shutdown()
	// Actual test
	plugin := defaultFritzbox()
	plugin.URLs = append(plugin.URLs, tr064Server.Server().String())
	plugin.Collect = []string{"wlan"}
	plugin.Log = testutil.Logger{Name: pluginName}
	err := plugin.Init()
	require.NoError(t, err)
	acc := &testutil.Accumulator{}
	err = acc.GatherError(plugin.Gather)
	require.NoError(t, err)
	testMetric(t, acc, "testdata/metrics/fritzbox_wlan.txt", telegraf.Gauge)
}

func TestGatherHostsInfo(t *testing.T) {
	// Start mock server
	tr064Server := mock.Start(mockDocsDir, testMocks...)
	defer tr064Server.Shutdown()
	// Actual test
	plugin := defaultFritzbox()
	plugin.URLs = append(plugin.URLs, tr064Server.Server().String())
	plugin.Collect = []string{"hosts"}
	plugin.Log = testutil.Logger{Name: pluginName}
	err := plugin.Init()
	require.NoError(t, err)
	acc := &testutil.Accumulator{}
	err = acc.GatherError(plugin.Gather)
	require.NoError(t, err)
	testMetric(t, acc, "testdata/metrics/fritzbox_hosts.txt", telegraf.Gauge)
}

func testMetric(t *testing.T, acc *testutil.Accumulator, file string, vt telegraf.ValueType) {
	parser := &influx.Parser{}
	err := parser.Init()
	require.NoError(t, err)
	expectedMetrics, err := testutil.ParseMetricsFromFile(file, parser)
	for index := range expectedMetrics {
		expectedMetrics[index].SetType(vt)
	}
	require.NoError(t, err)
	testutil.RequireMetricsEqual(t, expectedMetrics, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}
