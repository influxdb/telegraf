package microsoft_fabric

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	adx_commons "github.com/influxdata/telegraf/plugins/common/adx"
	eh_commons "github.com/influxdata/telegraf/plugins/common/eventhub"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockOutput struct {
	mock.Mock
}

func (m *MockOutput) Connect() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockOutput) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockOutput) Write(metrics []telegraf.Metric) error {
	args := m.Called(metrics)
	return args.Error(0)
}

func (m *MockOutput) SampleConfig() string {
	args := m.Called()
	return args.String(0)
}

func TestMicrosoftFabric_Connect(t *testing.T) {
	mockOutput := new(MockOutput)
	mockOutput.On("Connect").Return(nil)

	plugin := MicrosoftFabric{
		FabricSinkService: mockOutput,
	}

	err := plugin.Connect()
	require.NoError(t, err)
	mockOutput.AssertExpectations(t)
}

func TestMicrosoftFabric_Close(t *testing.T) {
	mockOutput := new(MockOutput)
	mockOutput.On("Close").Return(nil)

	plugin := MicrosoftFabric{
		FabricSinkService: mockOutput,
	}

	err := plugin.Close()
	require.NoError(t, err)
	mockOutput.AssertExpectations(t)
}

func TestMicrosoftFabric_Write(t *testing.T) {
	mockOutput := new(MockOutput)
	mockOutput.On("Write", mock.Anything).Return(nil)

	plugin := MicrosoftFabric{
		FabricSinkService: mockOutput,
	}

	metrics := []telegraf.Metric{
		testutil.TestMetric(1.0, "test_metric"),
	}

	err := plugin.Write(metrics)
	require.NoError(t, err)
	mockOutput.AssertExpectations(t)
}

func TestIsKustoEndpoint(t *testing.T) {
	testCases := []struct {
		name     string
		endpoint string
		expected bool
	}{
		{
			name:     "Valid address prefix",
			endpoint: "address=https://example.com",
			expected: true,
		},
		{
			name:     "Valid network address prefix",
			endpoint: "network address=https://example.com",
			expected: true,
		},
		{
			name:     "Valid server prefix",
			endpoint: "server=https://example.com",
			expected: true,
		},
		{
			name:     "Invalid prefix",
			endpoint: "https://example.com",
			expected: false,
		},
		{
			name:     "Empty endpoint",
			endpoint: "",
			expected: false,
		},
	}

	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			result := isKustoEndpoint(tC.endpoint)
			require.Equal(t, tC.expected, result)
		})
	}
}

func TestMicrosoftFabric_Init(t *testing.T) {
	tests := []struct {
		name             string
		connectionString string
		expectedError    string
	}{
		{
			name:             "Empty connection string",
			connectionString: "",
			expectedError: "endpoint must not be empty. For EventHouse refer : " +
				"https://learn.microsoft.com/kusto/api/connection-strings/kusto?view=microsoft-fabric" +
				" for EventStream refer : " +
				"https://learn.microsoft.com/fabric/real-time-intelligence/event-streams/add-manage-eventstream-sources?pivots=enhanced-capabilities",
		},
		{
			name:             "Invalid connection string",
			connectionString: "invalid_connection_string",
			expectedError: "invalid connection string. For EventHouse refer : " +
				"https://learn.microsoft.com/kusto/api/connection-strings/kusto?view=microsoft-fabric" +
				" for EventStream refer : " +
				"https://learn.microsoft.com/fabric/real-time-intelligence/event-streams/add-manage-eventstream-sources?pivots=enhanced-capabilities",
		},
		{
			name: "Valid EventHouse connection string",
			connectionString: "Endpoint=sb://namespace.servicebus.windows.net/;" +
				"SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=superSecret1234;EntityPath=hubName",
			expectedError: "",
		},
		{
			name:             "Valid Kusto connection string",
			connectionString: "data source=https://example.kusto.windows.net;Database=e2e",
			expectedError:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mf := &MicrosoftFabric{
				ConnectionString: tt.connectionString,
				Log:              testutil.Logger{},
				EventHouseConf: &adx_commons.AzureDataExplorer{
					Database: "database",
				},
				EventStreamConf: &eh_commons.EventHubs{
					Hub:     &eh_commons.EventHub{},
					Timeout: config.Duration(30 * time.Second),
				},
			}
			err := mf.Init()
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Equal(t, tt.expectedError, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
