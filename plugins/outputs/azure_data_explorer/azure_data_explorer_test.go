package azure_data_explorer

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	adx_commons "github.com/influxdata/telegraf/plugins/common/adx"
	"github.com/influxdata/telegraf/plugins/serializers/json"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestConnect(t *testing.T) {
	testCases := []struct {
		name          string
		endpoint      string
		expectedError string
		expectedPanic bool
	}{
		{
			name:          "Valid connection",
			endpoint:      "https://valid.endpoint",
			expectedError: "",
			expectedPanic: false,
		},
	}

	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			plugin := AzureDataExplorer{
				AzureDataExplorer: adx_commons.AzureDataExplorer{
					Endpoint: tC.endpoint,
					Log:      testutil.Logger{},
				},
			}

			if tC.expectedPanic {
				require.PanicsWithValue(t, tC.expectedError, func() {
					err := plugin.Connect()
					require.NoError(t, err)
				})
			} else {
				require.NotPanics(t, func() {
					err := plugin.Connect()
					require.NoError(t, err)
				})
			}
		})
	}
}

func TestWrite(t *testing.T) {
	plugin := AzureDataExplorer{
		AzureDataExplorer: adx_commons.AzureDataExplorer{
			Endpoint: "https://valid.endpoint",
			Database: "database",
			Log:      testutil.Logger{},
		},
	}
	serializer := &json.Serializer{
		TimestampUnits:  config.Duration(time.Nanosecond),
		TimestampFormat: time.RFC3339Nano,
	}
	plugin.SetSerializer(serializer)
	perr := plugin.Init()
	require.NoError(t, perr)
	err := plugin.Connect()
	require.NoError(t, err)

	metrics := []telegraf.Metric{
		testutil.TestMetric(1.0, "test_metric"),
	}

	err = plugin.Write(metrics)
	require.NoError(t, err)
}

func TestClose(t *testing.T) {
	plugin := AzureDataExplorer{
		AzureDataExplorer: adx_commons.AzureDataExplorer{
			Endpoint: "https://valid.endpoint",
			Log:      testutil.Logger{},
		},
	}

	err := plugin.Connect()
	require.NoError(t, err)

	err = plugin.Close()
	require.NoError(t, err)
}
