package modbus

import (
	"testing"
	"time"

	mb "github.com/grid-x/modbus"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
	"github.com/tbrandon/mbserver"
)

func TestMetric(t *testing.T) {
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "metric",
		Log:               testutil.Logger{},
	}
	plugin.Metrics = []metricDefinition{
		{
			SlaveID:     1,
			ByteOrder:   "ABCD",
			Measurement: "test",
			Fields: []metricFieldDefinition{
				{
					Name:         "coil-0",
					Address:      uint16(0),
					RegisterType: "coil",
				},
				{
					Name:         "coil-1",
					Address:      uint16(1),
					RegisterType: "coil",
				},
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
				{
					Name:         "holding-1",
					Address:      uint16(1),
					InputType:    "UINT16",
					RegisterType: "holding",
				},
			},
		},
		{
			SlaveID:   1,
			ByteOrder: "ABCD",
			Fields: []metricFieldDefinition{
				{
					Name:         "coil-0",
					Address:      uint16(2),
					RegisterType: "coil",
				},
				{
					Name:         "coil-1",
					Address:      uint16(3),
					RegisterType: "coil",
				},
				{
					Name:       "holding-0",
					Address:    uint16(2),
					InputType:  "INT64",
					Scale:      1.2,
					OutputType: "FLOAT64",
				},
			},
			Tags: map[string]string{
				"location": "main building",
				"device":   "mydevice",
			},
		},
		{
			SlaveID: 2,
			Fields: []metricFieldDefinition{
				{
					Name:         "coil-6",
					Address:      uint16(6),
					RegisterType: "coil",
				},
				{
					Name:         "coil-7",
					Address:      uint16(7),
					RegisterType: "coil",
				},
				{
					Name:         "discrete-0",
					Address:      uint16(0),
					RegisterType: "discrete",
				},
				{
					Name:      "holding-99",
					Address:   uint16(99),
					InputType: "INT16",
				},
			},
		},
		{
			SlaveID: 2,
			Fields: []metricFieldDefinition{
				{
					Name:         "coil-4",
					Address:      uint16(4),
					RegisterType: "coil",
				},
				{
					Name:         "coil-5",
					Address:      uint16(5),
					RegisterType: "coil",
				},
				{
					Name:         "input-0",
					Address:      uint16(0),
					RegisterType: "input",
					InputType:    "UINT16",
				},
				{
					Name:         "input-1",
					Address:      uint16(2),
					RegisterType: "input",
					InputType:    "UINT16",
				},
				{
					Name:      "holding-9",
					Address:   uint16(9),
					InputType: "INT16",
				},
			},
			Tags: map[string]string{
				"location": "main building",
				"device":   "mydevice",
			},
		},
	}

	require.NoError(t, plugin.Init())
	require.NotEmpty(t, plugin.requests)

	require.NotNil(t, plugin.requests[1])
	require.Len(t, plugin.requests[1].coil, 1, "coil 1")
	require.Len(t, plugin.requests[1].holding, 1, "holding 1")
	require.Empty(t, plugin.requests[1].discrete)
	require.Empty(t, plugin.requests[1].input)

	require.NotNil(t, plugin.requests[2])
	require.Len(t, plugin.requests[2].coil, 1, "coil 2")
	require.Len(t, plugin.requests[2].holding, 2, "holding 2")
	require.Len(t, plugin.requests[2].discrete, 1, "discrete 2")
	require.Len(t, plugin.requests[2].input, 2, "input 2")
}

func TestMetricResult(t *testing.T) {
	data := []byte{
		0x00, 0x0A, // 10
		0x00, 0x2A, // 42
		0x00, 0x00, 0x08, 0x98, // 2200
		0x00, 0x00, 0x08, 0x99, // 2201
		0x00, 0x00, 0x08, 0x9A, // 2202
		0x40, 0x49, 0x0f, 0xdb, // float32 of 3.1415927410125732421875
		0x4d, 0x6f, 0x64, 0x62, 0x75, 0x73, 0x20, 0x53, 0x74, 0x72, 0x69, 0x6e, 0x67, 0x00, // String "Modbus String"
	}

	// Write the data to a fake server
	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	handler := mb.NewTCPClientHandler("localhost:1502")
	require.NoError(t, handler.Connect())
	defer handler.Close()
	client := mb.NewClient(handler)

	quantity := uint16(len(data) / 2)
	_, err := client.WriteMultipleRegisters(1, quantity, data)
	require.NoError(t, err)

	// Setup the plugin
	plugin := Modbus{
		Name:              "FAKEMETER",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "metric",
		Log:               testutil.Logger{},
	}
	plugin.Metrics = []metricDefinition{
		{
			SlaveID:     1,
			ByteOrder:   "ABCD",
			Measurement: "machine",
			Fields: []metricFieldDefinition{
				{
					Name:         "hours",
					Address:      uint16(1),
					InputType:    "UINT16",
					RegisterType: "holding",
				},
				{
					Name:         "temperature",
					Address:      uint16(2),
					InputType:    "INT16",
					RegisterType: "holding",
				},
				{
					Name:         "comment",
					Address:      uint16(11),
					Length:       7,
					InputType:    "STRING",
					RegisterType: "holding",
				},
			},
			Tags: map[string]string{
				"location": "main building",
				"device":   "machine A",
			},
		},
		{
			SlaveID:     1,
			ByteOrder:   "ABCD",
			Measurement: "machine",
			Fields: []metricFieldDefinition{
				{
					Name:      "hours",
					Address:   uint16(3),
					InputType: "UINT32",
					Scale:     0.01,
				},
				{
					Name:      "temperature",
					Address:   uint16(5),
					InputType: "INT32",
					Scale:     0.02,
				},
				{
					Name:      "output",
					Address:   uint16(7),
					InputType: "UINT32",
				},
			},
			Tags: map[string]string{
				"location": "main building",
				"device":   "machine B",
			},
		},
		{
			SlaveID: 1,
			Fields: []metricFieldDefinition{
				{
					Name:      "pi",
					Address:   uint16(9),
					InputType: "FLOAT32",
				},
			},
		},
		{
			SlaveID:     1,
			Measurement: "bitvalues",
			Fields: []metricFieldDefinition{
				{
					Name:      "bit 0",
					Address:   uint16(1),
					InputType: "BIT",
					Bit:       0,
				},
				{
					Name:      "bit 1",
					Address:   uint16(1),
					InputType: "BIT",
					Bit:       1,
				},
				{
					Name:      "bit 2",
					Address:   uint16(1),
					InputType: "BIT",
					Bit:       2,
				},
				{
					Name:      "bit 3",
					Address:   uint16(1),
					InputType: "BIT",
					Bit:       3,
				},
			},
		},
	}
	require.NoError(t, plugin.Init())

	// Check the generated requests
	require.Len(t, plugin.requests, 1)
	require.NotNil(t, plugin.requests[1])
	require.Len(t, plugin.requests[1].holding, 5)
	require.Empty(t, plugin.requests[1].coil)
	require.Empty(t, plugin.requests[1].discrete)
	require.Empty(t, plugin.requests[1].input)

	// Gather the data and verify the resulting metrics
	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))

	expected := []telegraf.Metric{
		metric.New(
			"machine",
			map[string]string{
				"name":     "FAKEMETER",
				"location": "main building",
				"device":   "machine A",
				"slave_id": "1",
				"type":     "holding_register",
			},
			map[string]interface{}{
				"hours":       uint64(10),
				"temperature": int64(42),
				"comment":     "Modbus String",
			},
			time.Unix(0, 0),
		),
		metric.New(
			"machine",
			map[string]string{
				"name":     "FAKEMETER",
				"location": "main building",
				"device":   "machine B",
				"slave_id": "1",
				"type":     "holding_register",
			},
			map[string]interface{}{
				"hours":       float64(22.0),
				"temperature": float64(44.02),
				"output":      uint64(2202),
			},
			time.Unix(0, 0),
		),
		metric.New(
			"modbus",
			map[string]string{
				"name":     "FAKEMETER",
				"slave_id": "1",
				"type":     "holding_register",
			},
			map[string]interface{}{"pi": float64(3.1415927410125732421875)},
			time.Unix(0, 0),
		),
		metric.New(
			"bitvalues",
			map[string]string{
				"name":     "FAKEMETER",
				"slave_id": "1",
				"type":     "holding_register",
			},
			map[string]interface{}{
				"bit 0": uint64(0),
				"bit 1": uint64(1),
				"bit 2": uint64(0),
				"bit 3": uint64(1),
			},
			time.Unix(0, 0),
		),
	}

	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime(), testutil.SortMetrics())
}

func TestMetricAddressOverflow(t *testing.T) {
	logger := &testutil.CaptureLogger{}
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "metric",
		Log:               logger,
		Workarounds:       workarounds{ReadCoilsStartingAtZero: true},
	}
	plugin.Metrics = []metricDefinition{
		{
			SlaveID:     1,
			ByteOrder:   "ABCD",
			Measurement: "test",
			Fields: []metricFieldDefinition{
				{
					Name:         "field",
					Address:      uint16(65534),
					InputType:    "UINT64",
					RegisterType: "holding",
				},
			},
		},
	}
	require.ErrorIs(t, plugin.Init(), errAddressOverflow)
}
