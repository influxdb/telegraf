package s7comm

import (
	_ "embed"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robinson/gos7"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/testutil"
)

func TestSampleConfig(t *testing.T) {
	plugin := &S7comm{}
	require.NotEmpty(t, plugin.SampleConfig())
}

func TestInitFail(t *testing.T) {
	tests := []struct {
		name          string
		server        string
		rack          int
		slot          int
		configs       []metricDefinition
		expectedError string
	}{
		{
			name:          "empty settings",
			rack:          -1, // This is the default in `init()`
			slot:          -1, // This is the default in `init()`
			expectedError: "'server' has to be specified",
		},
		{
			name:          "missing rack",
			server:        "127.0.0.1:102",
			rack:          -1, // This is the default in `init()`
			slot:          -1, // This is the default in `init()`
			expectedError: "'rack' has to be specified",
		},
		{
			name:          "missing slot",
			server:        "127.0.0.1:102",
			rack:          0,
			slot:          -1, // This is the default in `init()`
			expectedError: "'slot' has to be specified",
		},
		{
			name:          "missing configs",
			server:        "127.0.0.1:102",
			expectedError: "no metric defined",
		},
		{
			name:          "single empty metric",
			server:        "127.0.0.1:102",
			configs:       []metricDefinition{{}},
			expectedError: "no fields defined for metric",
		},
		{
			name:   "single empty metric field",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{{}},
				},
			},
			expectedError: "unnamed field in metric",
		},
		{
			name:   "no address",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name: "foo",
						},
					},
				},
			},
			expectedError: "invalid address",
		},
		{
			name:   "invalid address pattern",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "FOO",
						},
					},
				},
			},
			expectedError: "invalid address",
		},
		{
			name:   "invalid address area",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "FOO1.W2",
						},
					},
				},
			},
			expectedError: "invalid area",
		},
		{
			name:   "invalid address area index",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB.W2",
						},
					},
				},
			},
			expectedError: "invalid address",
		},
		{
			name:   "invalid address type",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.A2",
						},
					},
				},
			},
			expectedError: "unknown data type",
		},
		{
			name:   "invalid address start",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.A",
						},
					},
				},
			},
			expectedError: "invalid address",
		},
		{
			name:   "missing extra parameter bit",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.X1",
						},
					},
				},
			},
			expectedError: "extra parameter required",
		},
		{
			name:   "missing extra parameter string",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.S1",
						},
					},
				},
			},
			expectedError: "extra parameter required",
		},
		{
			name:   "invalid address extra parameter",
			server: "127.0.0.1:102",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.W1.23",
						},
					},
				},
			},
			expectedError: "extra parameter specified but not used",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &S7comm{
				Server:  tt.server,
				Rack:    tt.rack,
				Slot:    tt.slot,
				Configs: tt.configs,
				Log:     &testutil.Logger{},
			}
			require.ErrorContains(t, plugin.Init(), tt.expectedError)
		})
	}
}

func TestInit(t *testing.T) {
	plugin := &S7comm{
		Server: "127.0.0.1:102",
		Rack:   0,
		Slot:   0,
		Configs: []metricDefinition{
			{
				Fields: []metricFieldDefinition{
					{
						Name:    "foo",
						Address: "DB1.W2",
					},
				},
			},
		},
		Log: &testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
}

func TestFieldMappings(t *testing.T) {
	tests := []struct {
		name     string
		configs  []metricDefinition
		expected []batch
	}{
		{
			name: "single field bit",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.X3.2",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x01,
							Bit:      2,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 1),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return false },
						},
					},
				},
			},
		},
		{
			name: "single field byte",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.B3",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x02,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 1),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return byte(0) },
						},
					},
				},
			},
		},
		{
			name: "single field char",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.C3",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x03,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 1),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return string([]byte{0}) },
						},
					},
				},
			},
		},
		{
			name: "single field string",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.S3.10",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x03,
							DBNumber: 5,
							Start:    3,
							Amount:   10,
							Data:     make([]byte, 12),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return "" },
						},
					},
				},
			},
		},
		{
			name: "single field word",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.W3",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x04,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 2),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return uint16(0) },
						},
					},
				},
			},
		},
		{
			name: "single field integer",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.I3",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x05,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 2),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return int16(0) },
						},
					},
				},
			},
		},
		{
			name: "single field double word",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.DW3",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x06,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 4),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return uint32(0) },
						},
					},
				},
			},
		},
		{
			name: "single field double integer",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.DI3",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x07,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 4),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return int32(0) },
						},
					},
				},
			},
		},
		{
			name: "single field float",
			configs: []metricDefinition{
				{
					Name: "test",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB5.R3",
						},
					},
				},
			},
			expected: []batch{
				{
					items: []gos7.S7DataItem{
						{
							Area:     0x84,
							WordLen:  0x08,
							DBNumber: 5,
							Start:    3,
							Amount:   1,
							Data:     make([]byte, 4),
						},
					},
					mappings: []fieldMapping{
						{
							measurement: "test",
							field:       "foo",
							convert:     func([]byte) interface{} { return float32(0) },
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &S7comm{
				Server:  "127.0.0.1:102",
				Rack:    0,
				Slot:    2,
				Configs: tt.configs,
				Log:     &testutil.Logger{},
			}
			require.NoError(t, plugin.Init())

			// Check the length
			require.Len(t, plugin.batches, len(tt.expected))
			// Check the actual content
			for i, eb := range tt.expected {
				ab := plugin.batches[i]
				require.Len(t, ab.items, len(eb.items))
				require.Len(t, ab.mappings, len(eb.mappings))
				require.EqualValues(t, eb.items, plugin.batches[i].items, "different items")
				for j, em := range eb.mappings {
					am := ab.mappings[j]
					require.Equal(t, em.measurement, am.measurement)
					require.Equal(t, em.field, am.field)
					buf := ab.items[j].Data
					require.Equal(t, em.convert(buf), am.convert(buf))
				}
			}
		})
	}
}

func TestMetricCollisions(t *testing.T) {
	tests := []struct {
		name          string
		configs       []metricDefinition
		expectedError string
	}{
		{
			name: "duplicate fields same config",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.W1",
						},
						{
							Name:    "foo",
							Address: "DB1.B1",
						},
					},
				},
			},
			expectedError: "duplicate field definition",
		},
		{
			name: "duplicate fields different config",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.B1",
						},
					},
				},
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.B1",
						},
					},
				},
			},
			expectedError: "duplicate field definition",
		},
		{
			name: "same fields different name",
			configs: []metricDefinition{
				{
					Name: "foo",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.B1",
						},
					},
				},
				{
					Name: "bar",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.B1",
						},
					},
				},
			},
		},
		{
			name: "same fields different tags",
			configs: []metricDefinition{
				{
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.B1",
						},
					},
					Tags: map[string]string{"device": "foo"},
				},
				{
					Name: "bar",
					Fields: []metricFieldDefinition{
						{
							Name:    "foo",
							Address: "DB1.B1",
						},
					},
					Tags: map[string]string{"device": "bar"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &S7comm{
				Server:  "127.0.0.1:102",
				Rack:    0,
				Slot:    2,
				Configs: tt.configs,
				Log:     &testutil.Logger{},
			}
			err := plugin.Init()
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConnectionLoss(t *testing.T) {
	// Create fake S7 comm server that can accept connects
	server, err := newMockServer()
	require.NoError(t, err)
	defer server.close()
	server.start()

	// Create the plugin and attempt a connection
	plugin := &S7comm{
		Server:          server.addr(),
		Rack:            0,
		Slot:            2,
		DebugConnection: true,
		Timeout:         config.Duration(100 * time.Millisecond),
		Configs: []metricDefinition{
			{
				Fields: []metricFieldDefinition{
					{
						Name:    "foo",
						Address: "DB1.W2",
					},
				},
			},
		},
		Log: &testutil.Logger{},
	}
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	require.NoError(t, plugin.Gather(&acc))
	require.NoError(t, plugin.Gather(&acc))
	plugin.Stop()
	server.close()

	require.Equal(t, uint32(3), server.connectionAttempts.Load())
}

func TestStartupErrorBehaviorError(t *testing.T) {
	// Create fake S7 comm server that can accept connects
	server, err := newMockServer()
	require.NoError(t, err)
	defer server.close()

	// Setup the plugin and the model to be able to use the startup retry strategy
	plugin := &S7comm{
		Server:          server.addr(),
		Rack:            0,
		Slot:            2,
		DebugConnection: true,
		Timeout:         config.Duration(100 * time.Millisecond),
		Configs: []metricDefinition{
			{
				Fields: []metricFieldDefinition{
					{
						Name:    "foo",
						Address: "DB1.W2",
					},
				},
			},
		},
		Log: &testutil.Logger{},
	}
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:  "s7comm",
			Alias: "error-test", // required to get a unique error stats instance
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Starting the plugin will fail with an error because the server does not listen
	var acc testutil.Accumulator
	require.ErrorContains(t, model.Start(&acc), "connecting to \""+server.addr()+"\" failed")
}

func TestStartupErrorBehaviorIgnore(t *testing.T) {
	// Create fake S7 comm server that can accept connects
	server, err := newMockServer()
	require.NoError(t, err)
	defer server.close()

	// Setup the plugin and the model to be able to use the startup retry strategy
	plugin := &S7comm{
		Server:          server.addr(),
		Rack:            0,
		Slot:            2,
		DebugConnection: true,
		Timeout:         config.Duration(100 * time.Millisecond),
		Configs: []metricDefinition{
			{
				Fields: []metricFieldDefinition{
					{
						Name:    "foo",
						Address: "DB1.W2",
					},
				},
			},
		},
		Log: &testutil.Logger{},
	}
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:                 "s7comm",
			Alias:                "ignore-test", // required to get a unique error stats instance
			StartupErrorBehavior: "ignore",
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Starting the plugin will fail because the server does not accept connections.
	// The model code should convert it to a fatal error for the agent to remove
	// the plugin.
	var acc testutil.Accumulator
	err = model.Start(&acc)
	require.ErrorContains(t, err, "connecting to \""+server.addr()+"\" failed")
	var fatalErr *internal.FatalError
	require.ErrorAs(t, err, &fatalErr)
}

func TestStartupErrorBehaviorRetry(t *testing.T) {
	// Create fake S7 comm server that can accept connects
	server, err := newMockServer()
	require.NoError(t, err)
	defer server.close()

	// Setup the plugin and the model to be able to use the startup retry strategy
	plugin := &S7comm{
		Server:          server.addr(),
		Rack:            0,
		Slot:            2,
		DebugConnection: true,
		Timeout:         config.Duration(100 * time.Millisecond),
		Configs: []metricDefinition{
			{
				Fields: []metricFieldDefinition{
					{
						Name:    "foo",
						Address: "DB1.W2",
					},
				},
			},
		},
		Log: &testutil.Logger{},
	}
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:                 "s7comm",
			Alias:                "retry-test", // required to get a unique error stats instance
			StartupErrorBehavior: "retry",
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Starting the plugin will return no error because the plugin will
	// retry to connect in every gather cycle.
	var acc testutil.Accumulator
	require.NoError(t, model.Start(&acc))

	// The gather should fail as the server does not accept connections (yet)
	require.Empty(t, acc.GetTelegrafMetrics())
	require.ErrorIs(t, model.Gather(&acc), internal.ErrNotConnected)
	require.Equal(t, int64(2), model.StartupErrors.Get())

	// Allow connection in the server, now the connection should succeed
	server.start()
	defer model.Stop()
	require.NoError(t, model.Gather(&acc))
}

type mockServer struct {
	connectionAttempts atomic.Uint32
	listener           net.Listener
}

func newMockServer() (*mockServer, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return &mockServer{listener: l}, nil
}

func (s *mockServer) addr() string {
	return s.listener.Addr().String()
}

func (s *mockServer) close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *mockServer) start() {
	go func() {
		defer s.listener.Close()
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				return
			}
			if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
				conn.Close()
				return
			}

			// Count the number of connection attempts
			s.connectionAttempts.Add(1)

			buf := make([]byte, 4096)

			// Wait for ISO connection telegram
			if _, err := io.ReadAtLeast(conn, buf, 22); err != nil {
				conn.Close()
				return
			}

			// Send fake response
			response := make([]byte, 22)
			response[5] = 0xD0
			binary.BigEndian.PutUint16(response[2:4], uint16(len(response)))
			if _, err := conn.Write(response); err != nil {
				conn.Close()
				return
			}

			// Wait for PDU negotiation telegram
			if _, err := io.ReadAtLeast(conn, buf, 25); err != nil {
				conn.Close()
				return
			}

			// Send fake response
			response = make([]byte, 27)
			binary.BigEndian.PutUint16(response[2:4], uint16(len(response)))
			binary.BigEndian.PutUint16(response[25:27], uint16(480))
			if _, err := conn.Write(response); err != nil {
				return
			}

			// Always close after connection is established
			conn.Close()
		}
	}()
}
