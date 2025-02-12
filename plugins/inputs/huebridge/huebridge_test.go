package huebridge

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tdrn-org/go-hue/mock"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

func TestConfig(t *testing.T) {
	// Verify plugin can be loaded from config
	conf := config.NewConfig()
	require.NoError(t, conf.LoadConfig("testdata/conf/huebridge.conf"))
	require.Len(t, conf.Inputs, 1)
	h, ok := conf.Inputs[0].Input.(*HueBridge)
	require.True(t, ok)

	// Verify successful Init
	require.NoError(t, h.Init())

	// Verify everything is setup according to config file
	require.Len(t, h.BridgeUrls, 4)
	require.Equal(t, "client", h.RemoteClientId)
	require.Equal(t, "secret", h.RemoteClientSecret)
	require.Equal(t, "url", h.RemoteCallbackUrl)
	require.Equal(t, "dir", h.RemoteTokenDir)
	require.Len(t, h.RoomAssignments, 2)
	require.Equal(t, config.Duration(60*time.Second), h.Timeout)
	require.Equal(t, "secret", h.TLSKeyPwd)
	require.True(t, h.InsecureSkipVerify)
}

func TestInitSuccess(t *testing.T) {
	// Create plugin instance with all types of URL schemes
	h := &HueBridge{
		BridgeUrls: []string{
			"address://12345678:secret@localhost/",
			"cloud://12345678:secret@localhost/discovery/",
			"mdns://12345678:secret@/",
			"remote://12345678:secret@localhost/",
		},
		RemoteClientConfig: RemoteClientConfig{
			RemoteClientId:     mock.MockClientId,
			RemoteClientSecret: mock.MockClientSecret,
			RemoteTokenDir:     ".",
		},
		ClientConfig: tls.ClientConfig{
			InsecureSkipVerify: true,
		},
		Timeout: config.Duration(10 * time.Second),
		Log:     &testutil.Logger{Name: "huebridge"},
	}

	// Verify successful Init
	require.NoError(t, h.Init())

	// Verify successful configuration of all bridge URLs
	require.Len(t, h.bridges, len(h.BridgeUrls))
}

func TestInitIgnoreInvalidUrls(t *testing.T) {
	// The following URLs are all invalid must all be ignored during Init
	h := &HueBridge{
		BridgeUrls: []string{
			"invalid://12345678:secret@invalid-scheme.net/",
			"address://12345678@missing-password.net/",
			"cloud://12345678@missing-password.net/",
			"mdns://12345678@missing-password.net/",
			"remote://12345678@missing-password.net/",
			"remote://12345678:secret@missing-remote-config.net/",
		},
		Timeout: config.Duration(10 * time.Second),
		Log:     &testutil.Logger{Name: "huebridge"},
	}

	// Verify successful Init
	require.NoError(t, h.Init())

	// Verify no bridge have been configured
	require.Len(t, h.bridges, 0)
}

func TestGatherLocal(t *testing.T) {
	// Start mock server and make plugin targing it
	bridgeMock := mock.Start()
	require.NotNil(t, bridgeMock)
	defer bridgeMock.Shutdown()
	h := &HueBridge{
		BridgeUrls: []string{
			fmt.Sprintf("address://%s:%s@%s/", mock.MockBridgeId, mock.MockBridgeUsername, bridgeMock.Server().Host),
		},
		RoomAssignments: map[string]string{"Name#7": "Name#15"},
		Timeout:         config.Duration(10 * time.Second),
		Log:             &testutil.Logger{Name: "huebridge"},
	}

	// Verify successful Init
	require.NoError(t, h.Init())

	// Verify successfull Gather
	acc := &testutil.Accumulator{}
	require.NoError(t, acc.GatherError(h.Gather))

	// Verify collected metrics are as expected
	expectedMetrics := loadExpectedMetrics(t, "testdata/metrics/huebridge.txt", telegraf.Gauge)
	testutil.RequireMetricsEqual(t, expectedMetrics, acc.GetTelegrafMetrics(), testutil.IgnoreTime(), testutil.SortMetrics())
}

func loadExpectedMetrics(t *testing.T, file string, vt telegraf.ValueType) []telegraf.Metric {
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	expectedMetrics, err := testutil.ParseMetricsFromFile(file, parser)
	require.NoError(t, err)
	for index := range expectedMetrics {
		expectedMetrics[index].SetType(vt)
	}
	return expectedMetrics
}
