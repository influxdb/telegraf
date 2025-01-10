package modbus

import (
	"strconv"
	"strings"
	"testing"
	"time"

	mb "github.com/grid-x/modbus"
	"github.com/stretchr/testify/require"
	"github.com/tbrandon/mbserver"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestRequest(t *testing.T) {
	modbus := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
	}
	modbus.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "coil",
			Fields: []requestFieldDefinition{
				{
					Name:    "coil-0",
					Address: uint16(0),
				},
				{
					Name:    "coil-1",
					Address: uint16(1),
					Omit:    true,
				},
				{
					Name:        "coil-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "UINT16",
					Measurement: "modbus",
				},
				{
					Name:        "coil-3",
					Address:     uint16(3),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "BOOL",
					Measurement: "modbus",
				},
			},
		},
		{
			SlaveID:      1,
			RegisterType: "coil",
			Fields: []requestFieldDefinition{
				{
					Name:    "coil-4",
					Address: uint16(6),
				},
				{
					Name:    "coil-5",
					Address: uint16(7),
					Omit:    true,
				},
				{
					Name:        "coil-6",
					Address:     uint16(8),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "UINT16",
					Measurement: "modbus",
				},
				{
					Name:        "coil-7",
					Address:     uint16(9),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "BOOL",
					Measurement: "modbus",
				},
			},
		},
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "discrete",
			Fields: []requestFieldDefinition{
				{
					Name:    "discrete-0",
					Address: uint16(0),
				},
				{
					Name:    "discrete-1",
					Address: uint16(1),
					Omit:    true,
				},
				{
					Name:        "discrete-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "UINT16",
					Measurement: "modbus",
				},
				{
					Name:        "discrete-3",
					Address:     uint16(3),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "BOOL",
					Measurement: "modbus",
				},
			},
		},
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
				{
					Name:      "holding-1",
					Address:   uint16(1),
					InputType: "UINT16",
					Omit:      true,
				},
				{
					Name:        "holding-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "FLOAT64",
					Measurement: "modbus",
				},
			},
		},
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "input",
			Fields: []requestFieldDefinition{
				{
					Name:      "input-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
				{
					Name:      "input-1",
					Address:   uint16(1),
					InputType: "UINT16",
					Omit:      true,
				},
				{
					Name:        "input-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "FLOAT64",
					Measurement: "modbus",
				},
			},
		},
	}

	require.NoError(t, modbus.Init())
	require.NotEmpty(t, modbus.requests)
	require.NotNil(t, modbus.requests[1])
	require.Len(t, modbus.requests[1].coil, 2)
	require.Len(t, modbus.requests[1].discrete, 1)
	require.Len(t, modbus.requests[1].holding, 1)
	require.Len(t, modbus.requests[1].input, 1)
}

func TestRequestWithTags(t *testing.T) {
	modbus := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
	}
	modbus.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "coil",
			Fields: []requestFieldDefinition{
				{
					Name:    "coil-0",
					Address: uint16(0),
				},
				{
					Name:    "coil-1",
					Address: uint16(1),
					Omit:    true,
				},
				{
					Name:        "coil-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "UINT16",
					Measurement: "modbus",
				},
			},
			Tags: map[string]string{
				"first":  "a",
				"second": "bb",
				"third":  "ccc",
			},
		},
		{
			SlaveID:      1,
			RegisterType: "coil",
			Fields: []requestFieldDefinition{
				{
					Name:    "coil-3",
					Address: uint16(6),
				},
				{
					Name:    "coil-4",
					Address: uint16(7),
					Omit:    true,
				},
				{
					Name:        "coil-5",
					Address:     uint16(8),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "UINT16",
					Measurement: "modbus",
				},
			},
			Tags: map[string]string{
				"first":  "a",
				"second": "bb",
				"third":  "ccc",
			},
		},
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "discrete",
			Fields: []requestFieldDefinition{
				{
					Name:    "discrete-0",
					Address: uint16(0),
				},
				{
					Name:    "discrete-1",
					Address: uint16(1),
					Omit:    true,
				},
				{
					Name:        "discrete-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "UINT16",
					Measurement: "modbus",
				},
			},
			Tags: map[string]string{
				"first":  "a",
				"second": "bb",
				"third":  "ccc",
			},
		},
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
				{
					Name:      "holding-1",
					Address:   uint16(1),
					InputType: "UINT16",
					Omit:      true,
				},
				{
					Name:        "holding-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "FLOAT64",
					Measurement: "modbus",
				},
			},
			Tags: map[string]string{
				"first":  "a",
				"second": "bb",
				"third":  "ccc",
			},
		},
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "input",
			Fields: []requestFieldDefinition{
				{
					Name:      "input-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
				{
					Name:      "input-1",
					Address:   uint16(1),
					InputType: "UINT16",
					Omit:      true,
				},
				{
					Name:        "input-2",
					Address:     uint16(2),
					InputType:   "INT64",
					Scale:       1.2,
					OutputType:  "FLOAT64",
					Measurement: "modbus",
				},
			},
			Tags: map[string]string{
				"first":  "a",
				"second": "bb",
				"third":  "ccc",
			},
		},
	}

	require.NoError(t, modbus.Init())
	require.NotEmpty(t, modbus.requests)
	require.NotNil(t, modbus.requests[1])
	require.Len(t, modbus.requests[1].coil, 2)
	require.Len(t, modbus.requests[1].discrete, 1)
	require.Len(t, modbus.requests[1].holding, 1)
	require.Len(t, modbus.requests[1].input, 1)

	expectedTags := map[string]string{
		"first":  "a",
		"second": "bb",
		"third":  "ccc",
	}
	require.Equal(t, expectedTags, modbus.requests[1].coil[0].fields[0].tags)
	require.Equal(t, expectedTags, modbus.requests[1].coil[1].fields[0].tags)
	require.Equal(t, expectedTags, modbus.requests[1].discrete[0].fields[0].tags)
	require.Equal(t, expectedTags, modbus.requests[1].holding[0].fields[0].tags)
	require.Equal(t, expectedTags, modbus.requests[1].input[0].fields[0].tags)
}

func TestRequestTypesCoil(t *testing.T) {
	tests := []struct {
		name        string
		address     uint16
		dataTypeOut string
		write       uint16
		read        interface{}
	}{
		{
			name:    "coil-1-off",
			address: 1,
			write:   0,
			read:    uint16(0),
		},
		{
			name:    "coil-2-on",
			address: 2,
			write:   0xFF00,
			read:    uint16(1),
		},
		{
			name:        "coil-3-false",
			address:     3,
			dataTypeOut: "BOOL",
			write:       0,
			read:        false,
		},
		{
			name:        "coil-4-true",
			address:     4,
			dataTypeOut: "BOOL",
			write:       0xFF00,
			read:        true,
		},
	}

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	handler := mb.NewTCPClientHandler("localhost:1502")
	require.NoError(t, handler.Connect())
	defer handler.Close()
	client := mb.NewClient(handler)

	for _, hrt := range tests {
		t.Run(hrt.name, func(t *testing.T) {
			_, err := client.WriteSingleCoil(hrt.address, hrt.write)
			require.NoError(t, err)

			modbus := Modbus{
				Name:              "TestRequestTypesCoil",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			modbus.Requests = []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "coil",
					Fields: []requestFieldDefinition{
						{
							Name:       hrt.name,
							OutputType: hrt.dataTypeOut,
							Address:    hrt.address,
						},
					},
				},
			}

			expected := []telegraf.Metric{
				testutil.MustMetric(
					"modbus",
					map[string]string{
						"type":     cCoils,
						"slave_id": "1",
						"name":     modbus.Name,
					},
					map[string]interface{}{hrt.name: hrt.read},
					time.Unix(0, 0),
				),
			}

			var acc testutil.Accumulator
			require.NoError(t, modbus.Init())
			require.NotEmpty(t, modbus.requests)
			require.NoError(t, modbus.Gather(&acc))
			acc.Wait(len(expected))

			testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}

func TestRequestTypesHoldingABCD(t *testing.T) {
	byteOrder := "ABCD"
	tests := []struct {
		name        string
		address     uint16
		bit         uint8
		length      uint16
		byteOrder   string
		dataTypeIn  string
		dataTypeOut string
		scale       float64
		write       []byte
		read        interface{}
	}{
		{
			name:       "register5_bit3",
			address:    5,
			dataTypeIn: "BIT",
			bit:        3,
			write:      []byte{0x18, 0x0d},
			read:       uint8(1),
		},
		{
			name:       "register5_bit14",
			address:    5,
			dataTypeIn: "BIT",
			bit:        14,
			write:      []byte{0x18, 0x0d},
			read:       uint8(0),
		},
		{
			name:       "register10_uint8L",
			address:    10,
			dataTypeIn: "UINT8L",
			write:      []byte{0x18, 0x0d},
			read:       uint8(13),
		},
		{
			name:       "register10_uint8L-scale_.1",
			address:    10,
			dataTypeIn: "UINT8L",
			scale:      .1,
			write:      []byte{0x18, 0x0d},
			read:       float64(1.3),
		},
		{
			name:       "register10_uint8L_scale_10",
			address:    10,
			dataTypeIn: "UINT8L",
			scale:      10,
			write:      []byte{0x18, 0x0d},
			read:       float64(130),
		},
		{
			name:        "register10_uint8L_uint64",
			address:     10,
			dataTypeIn:  "UINT8L",
			dataTypeOut: "UINT64",
			write:       []byte{0x18, 0x0d},
			read:        uint64(13),
		},
		{
			name:        "register10_uint8L_int64",
			address:     10,
			dataTypeIn:  "UINT8L",
			dataTypeOut: "INT64",
			write:       []byte{0x18, 0x0d},
			read:        int64(13),
		},
		{
			name:        "register10_uint8L_float64",
			address:     10,
			dataTypeIn:  "UINT8L",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x18, 0x0d},
			read:        float64(13),
		},
		{
			name:       "register10_uint8L_float64_scale",
			address:    10,
			dataTypeIn: "UINT8L",
			scale:      1.0,
			write:      []byte{0x18, 0x0d},
			read:       float64(13),
		},
		{
			name:       "register15_int8L",
			address:    15,
			dataTypeIn: "UINT8L",
			write:      []byte{0x18, 0x0d},
			read:       uint8(13),
		},
		{
			name:       "register15_int8L-scale_.1",
			address:    15,
			dataTypeIn: "INT8L",
			scale:      .1,
			write:      []byte{0x18, 0x0d},
			read:       float64(1.3),
		},
		{
			name:       "register15_int8L_scale_10",
			address:    15,
			dataTypeIn: "INT8L",
			scale:      10,
			write:      []byte{0x18, 0x0d},
			read:       float64(130),
		},
		{
			name:        "register15_int8L_uint64",
			address:     15,
			dataTypeIn:  "INT8L",
			dataTypeOut: "UINT64",
			write:       []byte{0x18, 0x0d},
			read:        uint64(13),
		},
		{
			name:        "register15_int8L_int64",
			address:     15,
			dataTypeIn:  "INT8L",
			dataTypeOut: "INT64",
			write:       []byte{0x18, 0x0d},
			read:        int64(13),
		},
		{
			name:        "register15_int8L_float64",
			address:     15,
			dataTypeIn:  "INT8L",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x18, 0x0d},
			read:        float64(13),
		},
		{
			name:       "register15_int8L_float64_scale",
			address:    15,
			dataTypeIn: "INT8L",
			scale:      1.0,
			write:      []byte{0x18, 0x0d},
			read:       float64(13),
		},
		{
			name:       "register20_uint16",
			address:    20,
			dataTypeIn: "UINT16",
			write:      []byte{0x08, 0x98},
			read:       uint16(2200),
		},
		{
			name:       "register20_uint16-scale_.1",
			address:    20,
			dataTypeIn: "UINT16",
			scale:      .1,
			write:      []byte{0x08, 0x98},
			read:       float64(220),
		},
		{
			name:       "register20_uint16_scale_10",
			address:    20,
			dataTypeIn: "UINT16",
			scale:      10,
			write:      []byte{0x08, 0x98},
			read:       float64(22000),
		},
		{
			name:        "register20_uint16_uint64",
			address:     20,
			dataTypeIn:  "UINT16",
			dataTypeOut: "UINT64",
			write:       []byte{0x08, 0x98},
			read:        uint64(2200),
		},
		{
			name:        "register20_uint16_int64",
			address:     20,
			dataTypeIn:  "UINT16",
			dataTypeOut: "INT64",
			write:       []byte{0x08, 0x98},
			read:        int64(2200),
		},
		{
			name:        "register20_uint16_float64",
			address:     20,
			dataTypeIn:  "UINT16",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x08, 0x98},
			read:        float64(2200),
		},
		{
			name:       "register20_uint16_float64_scale",
			address:    20,
			dataTypeIn: "UINT16",
			scale:      1.0,
			write:      []byte{0x08, 0x98},
			read:       float64(2200),
		},
		{
			name:       "register30_int16",
			address:    30,
			dataTypeIn: "INT16",
			write:      []byte{0xf8, 0x98},
			read:       int16(-1896),
		},
		{
			name:       "register30_int16-scale_.1",
			address:    30,
			dataTypeIn: "INT16",
			scale:      .1,
			write:      []byte{0xf8, 0x98},
			read:       float64(-189.60000000000002),
		},
		{
			name:       "register30_int16_scale_10",
			address:    30,
			dataTypeIn: "INT16",
			scale:      10,
			write:      []byte{0xf8, 0x98},
			read:       float64(-18960),
		},
		{
			name:        "register30_int16_uint64",
			address:     30,
			dataTypeIn:  "INT16",
			dataTypeOut: "UINT64",
			write:       []byte{0xf8, 0x98},
			read:        uint64(18446744073709549720),
		},
		{
			name:        "register30_int16_int64",
			address:     30,
			dataTypeIn:  "INT16",
			dataTypeOut: "INT64",
			write:       []byte{0xf8, 0x98},
			read:        int64(-1896),
		},
		{
			name:        "register30_int16_float64",
			address:     30,
			dataTypeIn:  "INT16",
			dataTypeOut: "FLOAT64",
			write:       []byte{0xf8, 0x98},
			read:        float64(-1896),
		},
		{
			name:       "register30_int16_float64_scale",
			address:    30,
			dataTypeIn: "INT16",
			scale:      1.0,
			write:      []byte{0xf8, 0x98},
			read:       float64(-1896),
		},
		{
			name:       "register40_uint32",
			address:    40,
			dataTypeIn: "UINT32",
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       uint32(168496141),
		},
		{
			name:       "register40_uint32-scale_.1",
			address:    40,
			dataTypeIn: "UINT32",
			scale:      .1,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       float64(16849614.1),
		},
		{
			name:       "register40_uint32_scale_10",
			address:    40,
			dataTypeIn: "UINT32",
			scale:      10,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       float64(1684961410),
		},
		{
			name:        "register40_uint32_uint64",
			address:     40,
			dataTypeIn:  "UINT32",
			dataTypeOut: "UINT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:        uint64(168496141),
		},
		{
			name:        "register40_uint32_int64",
			address:     40,
			dataTypeIn:  "UINT32",
			dataTypeOut: "INT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:        int64(168496141),
		},
		{
			name:        "register40_uint32_float64",
			address:     40,
			dataTypeIn:  "UINT32",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:        float64(168496141),
		},
		{
			name:       "register40_uint32_float64_scale",
			address:    40,
			dataTypeIn: "UINT32",
			scale:      1.0,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       float64(168496141),
		},
		{
			name:       "register50_int32",
			address:    50,
			dataTypeIn: "INT32",
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       int32(-99939315),
		},
		{
			name:       "register50_int32-scale_.1",
			address:    50,
			dataTypeIn: "INT32",
			scale:      .1,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       float64(-9993931.5),
		},
		{
			name:       "register50_int32_scale_10",
			address:    50,
			dataTypeIn: "INT32",
			scale:      10,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       float64(-999393150),
		},
		{
			name:        "register50_int32_uint64",
			address:     50,
			dataTypeIn:  "INT32",
			dataTypeOut: "UINT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:        uint64(18446744073609612301),
		},
		{
			name:        "register50_int32_int64",
			address:     50,
			dataTypeIn:  "INT32",
			dataTypeOut: "INT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:        int64(-99939315),
		},
		{
			name:        "register50_int32_float64",
			address:     50,
			dataTypeIn:  "INT32",
			dataTypeOut: "FLOAT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:        float64(-99939315),
		},
		{
			name:       "register50_int32_float64_scale",
			address:    50,
			dataTypeIn: "INT32",
			scale:      1.0,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       float64(-99939315),
		},
		{
			name:       "register60_uint64",
			address:    60,
			dataTypeIn: "UINT64",
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       uint64(723685415333069058),
		},
		{
			name:       "register60_uint64-scale_.1",
			address:    60,
			dataTypeIn: "UINT64",
			scale:      .1,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(72368541533306905.8),
		},
		{
			name:       "register60_uint64_scale_10",
			address:    60,
			dataTypeIn: "UINT64",
			scale:      10,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(7236854153330690000), // quantization error
		},
		{
			name:        "register60_uint64_int64",
			address:     60,
			dataTypeIn:  "UINT64",
			dataTypeOut: "INT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        int64(723685415333069058),
		},
		{
			name:        "register60_uint64_float64",
			address:     60,
			dataTypeIn:  "UINT64",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        float64(723685415333069058),
		},
		{
			name:       "register60_uint64_float64_scale",
			address:    60,
			dataTypeIn: "UINT64",
			scale:      1.0,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(723685415333069058),
		},
		{
			name:       "register70_int64",
			address:    70,
			dataTypeIn: "INT64",
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       int64(-429236089273777918),
		},
		{
			name:       "register70_int64-scale_.1",
			address:    70,
			dataTypeIn: "INT64",
			scale:      .1,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(-42923608927377791.8),
		},
		{
			name:       "register70_int64_scale_10",
			address:    70,
			dataTypeIn: "INT64",
			scale:      10,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(-4292360892737779180),
		},
		{
			name:        "register70_int64_uint64",
			address:     70,
			dataTypeIn:  "INT64",
			dataTypeOut: "UINT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        uint64(18017507984435773698),
		},
		{
			name:        "register70_int64_float64",
			address:     70,
			dataTypeIn:  "INT64",
			dataTypeOut: "FLOAT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        float64(-429236089273777918),
		},
		{
			name:       "register70_int64_float64_scale",
			address:    70,
			dataTypeIn: "INT64",
			scale:      1.0,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(-429236089273777918),
		},
		{
			name:       "register80_float32",
			address:    80,
			dataTypeIn: "FLOAT32",
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float32(3.1415927410125732421875),
		},
		{
			name:       "register80_float32-scale_.1",
			address:    80,
			dataTypeIn: "FLOAT32",
			scale:      .1,
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float64(0.31415927410125732421875),
		},
		{
			name:       "register80_float32_scale_10",
			address:    80,
			dataTypeIn: "FLOAT32",
			scale:      10,
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float64(31.415927410125732421875),
		},
		{
			name:        "register80_float32_float64",
			address:     80,
			dataTypeIn:  "FLOAT32",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x40, 0x49, 0x0f, 0xdb},
			read:        float64(3.1415927410125732421875),
		},
		{
			name:       "register80_float32_float64_scale",
			address:    80,
			dataTypeIn: "FLOAT32",
			scale:      1.0,
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float64(3.1415927410125732421875),
		},
		{
			name:       "register90_float64",
			address:    90,
			dataTypeIn: "FLOAT64",
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(3.14159265359000006156975359772),
		},
		{
			name:       "register90_float64-scale_.1",
			address:    90,
			dataTypeIn: "FLOAT64",
			scale:      .1,
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(0.314159265359000006156975359772),
		},
		{
			name:       "register90_float64_scale_10",
			address:    90,
			dataTypeIn: "FLOAT64",
			scale:      10,
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(31.4159265359000006156975359772),
		},
		{
			name:       "register90_float64_float64_scale",
			address:    90,
			dataTypeIn: "FLOAT64",
			scale:      1.0,
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(3.14159265359000006156975359772),
		},
		{
			name:       "register100_float16",
			address:    100,
			dataTypeIn: "FLOAT16",
			write:      []byte{0xb8, 0x14},
			read:       float64(-0.509765625),
		},
		{
			name:       "register100_float16-scale_.1",
			address:    100,
			dataTypeIn: "FLOAT16",
			scale:      .1,
			write:      []byte{0xb8, 0x14},
			read:       float64(-0.0509765625),
		},
		{
			name:       "register100_float16_scale_10",
			address:    100,
			dataTypeIn: "FLOAT16",
			scale:      10,
			write:      []byte{0xb8, 0x14},
			read:       float64(-5.09765625),
		},
		{
			name:       "register100_float16_float64_scale",
			address:    100,
			dataTypeIn: "FLOAT16",
			scale:      1.0,
			write:      []byte{0xb8, 0x14},
			read:       float64(-0.509765625),
		},
		{
			name:       "register110_string",
			address:    110,
			dataTypeIn: "STRING",
			length:     7,
			write:      []byte{0x4d, 0x6f, 0x64, 0x62, 0x75, 0x73, 0x20, 0x53, 0x74, 0x72, 0x69, 0x6e, 0x67, 0x00},
			read:       "Modbus String",
		},
	}

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	handler := mb.NewTCPClientHandler("localhost:1502")
	require.NoError(t, handler.Connect())
	defer handler.Close()
	client := mb.NewClient(handler)

	for _, hrt := range tests {
		t.Run(hrt.name, func(t *testing.T) {
			quantity := uint16(len(hrt.write) / 2)
			_, err := client.WriteMultipleRegisters(hrt.address, quantity, hrt.write)
			require.NoError(t, err)

			modbus := Modbus{
				Name:              "TestRequestTypesHoldingABCD",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			modbus.Requests = []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    byteOrder,
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Name:       hrt.name,
							InputType:  hrt.dataTypeIn,
							OutputType: hrt.dataTypeOut,
							Scale:      hrt.scale,
							Address:    hrt.address,
							Length:     hrt.length,
							Bit:        hrt.bit,
						},
					},
				},
			}

			expected := []telegraf.Metric{
				testutil.MustMetric(
					"modbus",
					map[string]string{
						"type":     cHoldingRegisters,
						"slave_id": "1",
						"name":     modbus.Name,
					},
					map[string]interface{}{hrt.name: hrt.read},
					time.Unix(0, 0),
				),
			}

			var acc testutil.Accumulator
			require.NoError(t, modbus.Init())
			require.NotEmpty(t, modbus.requests)
			require.NoError(t, modbus.Gather(&acc))
			acc.Wait(len(expected))

			testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}

func TestRequestTypesHoldingDCBA(t *testing.T) {
	byteOrder := "DCBA"
	tests := []struct {
		name        string
		address     uint16
		length      uint16
		byteOrder   string
		dataTypeIn  string
		dataTypeOut string
		scale       float64
		write       []byte
		read        interface{}
	}{
		{
			name:       "register10_uint8L",
			address:    10,
			dataTypeIn: "UINT8L",
			write:      []byte{0x18, 0x0d},
			read:       uint8(13),
		},
		{
			name:       "register10_uint8L-scale_.1",
			address:    10,
			dataTypeIn: "UINT8L",
			scale:      .1,
			write:      []byte{0x18, 0x0d},
			read:       float64(1.3),
		},
		{
			name:       "register10_uint8L_scale_10",
			address:    10,
			dataTypeIn: "UINT8L",
			scale:      10,
			write:      []byte{0x18, 0x0d},
			read:       float64(130),
		},
		{
			name:        "register10_uint8L_uint64",
			address:     10,
			dataTypeIn:  "UINT8L",
			dataTypeOut: "UINT64",
			write:       []byte{0x18, 0x0d},
			read:        uint64(13),
		},
		{
			name:        "register10_uint8L_int64",
			address:     10,
			dataTypeIn:  "UINT8L",
			dataTypeOut: "INT64",
			write:       []byte{0x18, 0x0d},
			read:        int64(13),
		},
		{
			name:        "register10_uint8L_float64",
			address:     10,
			dataTypeIn:  "UINT8L",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x18, 0x0d},
			read:        float64(13),
		},
		{
			name:       "register10_uint8L_float64_scale",
			address:    10,
			dataTypeIn: "UINT8L",
			scale:      1.0,
			write:      []byte{0x18, 0x0d},
			read:       float64(13),
		},
		{
			name:       "register15_int8L",
			address:    15,
			dataTypeIn: "UINT8L",
			write:      []byte{0x18, 0x0d},
			read:       uint8(13),
		},
		{
			name:       "register15_int8L-scale_.1",
			address:    15,
			dataTypeIn: "INT8L",
			scale:      .1,
			write:      []byte{0x18, 0x0d},
			read:       float64(1.3),
		},
		{
			name:       "register15_int8L_scale_10",
			address:    15,
			dataTypeIn: "INT8L",
			scale:      10,
			write:      []byte{0x18, 0x0d},
			read:       float64(130),
		},
		{
			name:        "register15_int8L_uint64",
			address:     15,
			dataTypeIn:  "INT8L",
			dataTypeOut: "UINT64",
			write:       []byte{0x18, 0x0d},
			read:        uint64(13),
		},
		{
			name:        "register15_int8L_int64",
			address:     15,
			dataTypeIn:  "INT8L",
			dataTypeOut: "INT64",
			write:       []byte{0x18, 0x0d},
			read:        int64(13),
		},
		{
			name:        "register15_int8L_float64",
			address:     15,
			dataTypeIn:  "INT8L",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x18, 0x0d},
			read:        float64(13),
		},
		{
			name:       "register15_int8L_float64_scale",
			address:    15,
			dataTypeIn: "INT8L",
			scale:      1.0,
			write:      []byte{0x18, 0x0d},
			read:       float64(13),
		},
		{
			name:       "register20_uint16",
			address:    20,
			dataTypeIn: "UINT16",
			write:      []byte{0x08, 0x98},
			read:       uint16(2200),
		},
		{
			name:       "register20_uint16-scale_.1",
			address:    20,
			dataTypeIn: "UINT16",
			scale:      .1,
			write:      []byte{0x08, 0x98},
			read:       float64(220),
		},
		{
			name:       "register20_uint16_scale_10",
			address:    20,
			dataTypeIn: "UINT16",
			scale:      10,
			write:      []byte{0x08, 0x98},
			read:       float64(22000),
		},
		{
			name:        "register20_uint16_uint64",
			address:     20,
			dataTypeIn:  "UINT16",
			dataTypeOut: "UINT64",
			write:       []byte{0x08, 0x98},
			read:        uint64(2200),
		},
		{
			name:        "register20_uint16_int64",
			address:     20,
			dataTypeIn:  "UINT16",
			dataTypeOut: "INT64",
			write:       []byte{0x08, 0x98},
			read:        int64(2200),
		},
		{
			name:        "register20_uint16_float64",
			address:     20,
			dataTypeIn:  "UINT16",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x08, 0x98},
			read:        float64(2200),
		},
		{
			name:       "register20_uint16_float64_scale",
			address:    20,
			dataTypeIn: "UINT16",
			scale:      1.0,
			write:      []byte{0x08, 0x98},
			read:       float64(2200),
		},
		{
			name:       "register30_int16",
			address:    30,
			dataTypeIn: "INT16",
			write:      []byte{0xf8, 0x98},
			read:       int16(-1896),
		},
		{
			name:       "register30_int16-scale_.1",
			address:    30,
			dataTypeIn: "INT16",
			scale:      .1,
			write:      []byte{0xf8, 0x98},
			read:       float64(-189.60000000000002),
		},
		{
			name:       "register30_int16_scale_10",
			address:    30,
			dataTypeIn: "INT16",
			scale:      10,
			write:      []byte{0xf8, 0x98},
			read:       float64(-18960),
		},
		{
			name:        "register30_int16_uint64",
			address:     30,
			dataTypeIn:  "INT16",
			dataTypeOut: "UINT64",
			write:       []byte{0xf8, 0x98},
			read:        uint64(18446744073709549720),
		},
		{
			name:        "register30_int16_int64",
			address:     30,
			dataTypeIn:  "INT16",
			dataTypeOut: "INT64",
			write:       []byte{0xf8, 0x98},
			read:        int64(-1896),
		},
		{
			name:        "register30_int16_float64",
			address:     30,
			dataTypeIn:  "INT16",
			dataTypeOut: "FLOAT64",
			write:       []byte{0xf8, 0x98},
			read:        float64(-1896),
		},
		{
			name:       "register30_int16_float64_scale",
			address:    30,
			dataTypeIn: "INT16",
			scale:      1.0,
			write:      []byte{0xf8, 0x98},
			read:       float64(-1896),
		},
		{
			name:       "register40_uint32",
			address:    40,
			dataTypeIn: "UINT32",
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       uint32(168496141),
		},
		{
			name:       "register40_uint32-scale_.1",
			address:    40,
			dataTypeIn: "UINT32",
			scale:      .1,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       float64(16849614.1),
		},
		{
			name:       "register40_uint32_scale_10",
			address:    40,
			dataTypeIn: "UINT32",
			scale:      10,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       float64(1684961410),
		},
		{
			name:        "register40_uint32_uint64",
			address:     40,
			dataTypeIn:  "UINT32",
			dataTypeOut: "UINT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:        uint64(168496141),
		},
		{
			name:        "register40_uint32_int64",
			address:     40,
			dataTypeIn:  "UINT32",
			dataTypeOut: "INT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:        int64(168496141),
		},
		{
			name:        "register40_uint32_float64",
			address:     40,
			dataTypeIn:  "UINT32",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:        float64(168496141),
		},
		{
			name:       "register40_uint32_float64_scale",
			address:    40,
			dataTypeIn: "UINT32",
			scale:      1.0,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d},
			read:       float64(168496141),
		},
		{
			name:       "register50_int32",
			address:    50,
			dataTypeIn: "INT32",
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       int32(-99939315),
		},
		{
			name:       "register50_int32-scale_.1",
			address:    50,
			dataTypeIn: "INT32",
			scale:      .1,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       float64(-9993931.5),
		},
		{
			name:       "register50_int32_scale_10",
			address:    50,
			dataTypeIn: "INT32",
			scale:      10,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       float64(-999393150),
		},
		{
			name:        "register50_int32_uint64",
			address:     50,
			dataTypeIn:  "INT32",
			dataTypeOut: "UINT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:        uint64(18446744073609612301),
		},
		{
			name:        "register50_int32_int64",
			address:     50,
			dataTypeIn:  "INT32",
			dataTypeOut: "INT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:        int64(-99939315),
		},
		{
			name:        "register50_int32_float64",
			address:     50,
			dataTypeIn:  "INT32",
			dataTypeOut: "FLOAT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:        float64(-99939315),
		},
		{
			name:       "register50_int32_float64_scale",
			address:    50,
			dataTypeIn: "INT32",
			scale:      1.0,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d},
			read:       float64(-99939315),
		},
		{
			name:       "register60_uint64",
			address:    60,
			dataTypeIn: "UINT64",
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       uint64(723685415333069058),
		},
		{
			name:       "register60_uint64-scale_.1",
			address:    60,
			dataTypeIn: "UINT64",
			scale:      .1,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(72368541533306905.8),
		},
		{
			name:       "register60_uint64_scale_10",
			address:    60,
			dataTypeIn: "UINT64",
			scale:      10,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(7236854153330690000), // quantization error
		},
		{
			name:        "register60_uint64_int64",
			address:     60,
			dataTypeIn:  "UINT64",
			dataTypeOut: "INT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        int64(723685415333069058),
		},
		{
			name:        "register60_uint64_float64",
			address:     60,
			dataTypeIn:  "UINT64",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        float64(723685415333069058),
		},
		{
			name:       "register60_uint64_float64_scale",
			address:    60,
			dataTypeIn: "UINT64",
			scale:      1.0,
			write:      []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(723685415333069058),
		},
		{
			name:       "register70_int64",
			address:    70,
			dataTypeIn: "INT64",
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       int64(-429236089273777918),
		},
		{
			name:       "register70_int64-scale_.1",
			address:    70,
			dataTypeIn: "INT64",
			scale:      .1,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(-42923608927377791.8),
		},
		{
			name:       "register70_int64_scale_10",
			address:    70,
			dataTypeIn: "INT64",
			scale:      10,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(-4292360892737779180),
		},
		{
			name:        "register70_int64_uint64",
			address:     70,
			dataTypeIn:  "INT64",
			dataTypeOut: "UINT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        uint64(18017507984435773698),
		},
		{
			name:        "register70_int64_float64",
			address:     70,
			dataTypeIn:  "INT64",
			dataTypeOut: "FLOAT64",
			write:       []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:        float64(-429236089273777918),
		},
		{
			name:       "register70_int64_float64_scale",
			address:    70,
			dataTypeIn: "INT64",
			scale:      1.0,
			write:      []byte{0xfa, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02},
			read:       float64(-429236089273777918),
		},
		{
			name:       "register80_float32",
			address:    80,
			dataTypeIn: "FLOAT32",
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float32(3.1415927410125732421875),
		},
		{
			name:       "register80_float32-scale_.1",
			address:    80,
			dataTypeIn: "FLOAT32",
			scale:      .1,
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float64(0.31415927410125732421875),
		},
		{
			name:       "register80_float32_scale_10",
			address:    80,
			dataTypeIn: "FLOAT32",
			scale:      10,
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float64(31.415927410125732421875),
		},
		{
			name:        "register80_float32_float64",
			address:     80,
			dataTypeIn:  "FLOAT32",
			dataTypeOut: "FLOAT64",
			write:       []byte{0x40, 0x49, 0x0f, 0xdb},
			read:        float64(3.1415927410125732421875),
		},
		{
			name:       "register80_float32_float64_scale",
			address:    80,
			dataTypeIn: "FLOAT32",
			scale:      1.0,
			write:      []byte{0x40, 0x49, 0x0f, 0xdb},
			read:       float64(3.1415927410125732421875),
		},
		{
			name:       "register90_float64",
			address:    90,
			dataTypeIn: "FLOAT64",
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(3.14159265359000006156975359772),
		},
		{
			name:       "register90_float64-scale_.1",
			address:    90,
			dataTypeIn: "FLOAT64",
			scale:      .1,
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(0.314159265359000006156975359772),
		},
		{
			name:       "register90_float64_scale_10",
			address:    90,
			dataTypeIn: "FLOAT64",
			scale:      10,
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(31.4159265359000006156975359772),
		},
		{
			name:       "register90_float64_float64_scale",
			address:    90,
			dataTypeIn: "FLOAT64",
			scale:      1.0,
			write:      []byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2e, 0xea},
			read:       float64(3.14159265359000006156975359772),
		},
		{
			name:       "register100_float16",
			address:    100,
			dataTypeIn: "FLOAT16",
			write:      []byte{0xb8, 0x14},
			read:       float64(-0.509765625),
		},
		{
			name:       "register100_float16-scale_.1",
			address:    100,
			dataTypeIn: "FLOAT16",
			scale:      .1,
			write:      []byte{0xb8, 0x14},
			read:       float64(-0.0509765625),
		},
		{
			name:       "register100_float16_scale_10",
			address:    100,
			dataTypeIn: "FLOAT16",
			scale:      10,
			write:      []byte{0xb8, 0x14},
			read:       float64(-5.09765625),
		},
		{
			name:       "register100_float16_float64_scale",
			address:    100,
			dataTypeIn: "FLOAT16",
			scale:      1.0,
			write:      []byte{0xb8, 0x14},
			read:       float64(-0.509765625),
		},
		{
			name:       "register110_string",
			address:    110,
			dataTypeIn: "STRING",
			length:     7,
			write:      []byte{0x6f, 0x4d, 0x62, 0x64, 0x73, 0x75, 0x53, 0x20, 0x72, 0x74, 0x6e, 0x69, 0x00, 0x67},
			read:       "Modbus String",
		},
	}

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	handler := mb.NewTCPClientHandler("localhost:1502")
	require.NoError(t, handler.Connect())
	defer handler.Close()
	client := mb.NewClient(handler)

	for _, hrt := range tests {
		t.Run(hrt.name, func(t *testing.T) {
			quantity := uint16(len(hrt.write) / 2)
			invert := make([]byte, 0, len(hrt.write))
			if hrt.dataTypeIn != "STRING" {
				for i := len(hrt.write) - 1; i >= 0; i-- {
					invert = append(invert, hrt.write[i])
				}
			} else {
				// Put in raw data for strings
				invert = append(invert, hrt.write...)
			}
			_, err := client.WriteMultipleRegisters(hrt.address, quantity, invert)
			require.NoError(t, err)

			modbus := Modbus{
				Name:              "TestRequestTypesHoldingDCBA",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			modbus.Requests = []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    byteOrder,
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Name:       hrt.name,
							InputType:  hrt.dataTypeIn,
							OutputType: hrt.dataTypeOut,
							Scale:      hrt.scale,
							Address:    hrt.address,
							Length:     hrt.length,
						},
					},
				},
			}

			expected := []telegraf.Metric{
				testutil.MustMetric(
					"modbus",
					map[string]string{
						"type":     cHoldingRegisters,
						"slave_id": "1",
						"name":     modbus.Name,
					},
					map[string]interface{}{hrt.name: hrt.read},
					time.Unix(0, 0),
				),
			}

			var acc testutil.Accumulator
			require.NoError(t, modbus.Init())
			require.NotEmpty(t, modbus.requests)
			require.NoError(t, modbus.Gather(&acc))
			acc.Wait(len(expected))

			testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}

func TestRequestFail(t *testing.T) {
	tests := []struct {
		name     string
		requests []requestDefinition
		errormsg string
	}{
		{
			name: "empty field name (coil)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "coil",
					Fields: []requestFieldDefinition{
						{
							Address: uint16(15),
						},
					},
				},
			},
			errormsg: "empty field name in request for slave 1",
		},
		{
			name: "invalid byte-order (coil)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "AB",
					RegisterType: "coil",
				},
			},
			errormsg: "unknown byte-order \"AB\"",
		},
		{
			name: "duplicate fields (coil)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "coil",
					Fields: []requestFieldDefinition{
						{
							Name:    "coil-0",
							Address: uint16(0),
						},
						{
							Name:    "coil-0",
							Address: uint16(1),
						},
					},
				},
			},
			errormsg: "field \"coil-0\" duplicated in measurement \"modbus\" (slave 1/\"coil\")",
		},
		{
			name: "duplicate fields multiple requests (coil)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "coil",
					Fields: []requestFieldDefinition{
						{
							Name:        "coil-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "coil",
					Fields: []requestFieldDefinition{
						{
							Name:        "coil-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
			},
			errormsg: "field \"coil-0\" duplicated in measurement \"foo\" (slave 1/\"coil\")",
		},
		{
			name: "invalid byte-order (discrete)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "AB",
					RegisterType: "discrete",
				},
			},
			errormsg: "unknown byte-order \"AB\"",
		},
		{
			name: "duplicate fields (discrete)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "discrete",
					Fields: []requestFieldDefinition{
						{
							Name:    "discrete-0",
							Address: uint16(0),
						},
						{
							Name:    "discrete-0",
							Address: uint16(1),
						},
					},
				},
			},
			errormsg: "field \"discrete-0\" duplicated in measurement \"modbus\" (slave 1/\"discrete\")",
		},
		{
			name: "duplicate fields multiple requests (discrete)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "discrete",
					Fields: []requestFieldDefinition{
						{
							Name:        "discrete-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "discrete",
					Fields: []requestFieldDefinition{
						{
							Name:        "discrete-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
			},
			errormsg: "field \"discrete-0\" duplicated in measurement \"foo\" (slave 1/\"discrete\")",
		},
		{
			name: "invalid byte-order (holding)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "AB",
					RegisterType: "holding",
				},
			},
			errormsg: "unknown byte-order \"AB\"",
		},
		{
			name: "invalid field name (holding)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Address: uint16(0),
						},
					},
				},
			},
			errormsg: "empty field name in request for slave 1",
		},
		{
			name: "invalid field input type (holding)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Name:    "holding-0",
							Address: uint16(0),
						},
					},
				},
			},
			errormsg: "initializing field \"holding-0\" failed: invalid input datatype \"\" for determining field length",
		},
		{
			name: "invalid field output type (holding)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Name:       "holding-0",
							Address:    uint16(0),
							InputType:  "UINT16",
							OutputType: "UINT8",
						},
					},
				},
			},
			errormsg: `unknown output data-type "UINT8" for field "holding-0"`,
		},
		{
			name: "duplicate fields (holding)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Name:    "holding-0",
							Address: uint16(0),
						},
						{
							Name:    "holding-0",
							Address: uint16(1),
						},
					},
				},
			},
			errormsg: "field \"holding-0\" duplicated in measurement \"modbus\" (slave 1/\"holding\")",
		},
		{
			name: "duplicate fields multiple requests (holding)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Name:        "holding-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "holding",
					Fields: []requestFieldDefinition{
						{
							Name:        "holding-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
			},
			errormsg: "field \"holding-0\" duplicated in measurement \"foo\" (slave 1/\"holding\")",
		},
		{
			name: "invalid byte-order (input)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "AB",
					RegisterType: "input",
				},
			},
			errormsg: "unknown byte-order \"AB\"",
		},
		{
			name: "invalid field name (input)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					RegisterType: "input",
					Fields: []requestFieldDefinition{
						{
							Address: uint16(0),
						},
					},
				},
			},
			errormsg: "empty field name in request for slave 1",
		},
		{
			name: "invalid field input type (input)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					RegisterType: "input",
					Fields: []requestFieldDefinition{
						{
							Name:    "input-0",
							Address: uint16(0),
						},
					},
				},
			},
			errormsg: "initializing field \"input-0\" failed: invalid input datatype \"\" for determining field length",
		},
		{
			name: "invalid field output type (input)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					RegisterType: "input",
					Fields: []requestFieldDefinition{
						{
							Name:       "input-0",
							Address:    uint16(0),
							InputType:  "UINT16",
							OutputType: "UINT8",
						},
					},
				},
			},
			errormsg: `unknown output data-type "UINT8" for field "input-0"`,
		},
		{
			name: "duplicate fields (input)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "input",
					Fields: []requestFieldDefinition{
						{
							Name:    "input-0",
							Address: uint16(0),
						},
						{
							Name:    "input-0",
							Address: uint16(1),
						},
					},
				},
			},
			errormsg: "field \"input-0\" duplicated in measurement \"modbus\" (slave 1/\"input\")",
		},
		{
			name: "duplicate fields multiple requests (input)",
			requests: []requestDefinition{
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "input",
					Fields: []requestFieldDefinition{
						{
							Name:        "input-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
				{
					SlaveID:      1,
					ByteOrder:    "ABCD",
					RegisterType: "input",
					Fields: []requestFieldDefinition{
						{
							Name:        "input-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
			},
			errormsg: "field \"input-0\" duplicated in measurement \"foo\" (slave 1/\"input\")",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := Modbus{
				Name:              "Test",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			plugin.Requests = tt.requests

			require.ErrorContains(t, plugin.Init(), tt.errormsg)
			require.Empty(t, plugin.requests)
		})
	}
}

func TestRequestStartingWithOmits(t *testing.T) {
	modbus := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
	}
	modbus.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
					Omit:      true,
				},
				{
					Name:      "holding-1",
					Address:   uint16(1),
					InputType: "UINT16",
					Omit:      true,
				},
				{
					Name:      "holding-2",
					Address:   uint16(2),
					InputType: "INT16",
				},
			},
		},
	}
	require.NoError(t, modbus.Init())
	require.NotEmpty(t, modbus.requests)
	require.NotNil(t, modbus.requests[1])
	require.Equal(t, uint16(0), modbus.requests[1].holding[0].address)

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	handler := mb.NewTCPClientHandler("localhost:1502")
	require.NoError(t, handler.Connect())
	defer handler.Close()
	client := mb.NewClient(handler)
	_, err := client.WriteMultipleRegisters(uint16(0), 3, []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x03})
	require.NoError(t, err)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"modbus",
			map[string]string{
				"type":     cHoldingRegisters,
				"slave_id": strconv.Itoa(int(modbus.Requests[0].SlaveID)),
				"name":     modbus.Name,
			},
			map[string]interface{}{"holding-2": int16(3)},
			time.Unix(0, 0),
		),
	}

	var acc testutil.Accumulator
	require.NoError(t, modbus.Gather(&acc))
	acc.Wait(len(expected))
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestRequestWithOmittedFieldsOnly(t *testing.T) {
	modbus := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
	}
	modbus.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
					Omit:      true,
				},
				{
					Name:      "holding-1",
					Address:   uint16(1),
					InputType: "UINT16",
					Omit:      true,
				},
				{
					Name:      "holding-2",
					Address:   uint16(2),
					InputType: "INT16",
					Omit:      true,
				},
			},
		},
	}
	require.NoError(t, modbus.Init())
	require.Empty(t, modbus.requests)
}

func TestRequestGroupWithOmittedFieldsOnly(t *testing.T) {
	modbus := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
	}
	modbus.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
					Omit:      true,
				},
				{
					Name:      "holding-1",
					Address:   uint16(1),
					InputType: "UINT16",
					Omit:      true,
				},
				{
					Name:      "holding-2",
					Address:   uint16(2),
					InputType: "INT16",
					Omit:      true,
				},
				{
					Name:      "holding-8",
					Address:   uint16(8),
					InputType: "INT16",
				},
			},
		},
	}
	require.NoError(t, modbus.Init())
	require.Len(t, modbus.requests, 1)
	require.NotNil(t, modbus.requests[1])
	require.Len(t, modbus.requests[1].holding, 1)
	require.Equal(t, uint16(8), modbus.requests[1].holding[0].address)
	require.Equal(t, uint16(1), modbus.requests[1].holding[0].length)
}

func TestRequestEmptyFields(t *testing.T) {
	modbus := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
	}
	modbus.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
		},
	}
	err := modbus.Init()
	require.ErrorContains(t, err, `found request section without fields`)
}

func TestRequestMultipleSlavesOneFail(t *testing.T) {
	modbus := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		Retries:           1,
		ConfigurationType: "request",
		Log:               testutil.Logger{},
	}
	modbus.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
			},
		},
		{
			SlaveID:      2,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
			},
		},
		{
			SlaveID:      3,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-0",
					Address:   uint16(0),
					InputType: "INT16",
				},
			},
		},
	}
	require.NoError(t, modbus.Init())

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	serv.RegisterFunctionHandler(3,
		func(_ *mbserver.Server, frame mbserver.Framer) ([]byte, *mbserver.Exception) {
			tcpframe, ok := frame.(*mbserver.TCPFrame)
			if !ok {
				return nil, &mbserver.IllegalFunction
			}

			if tcpframe.Device == 2 {
				// Simulate device 2 being unavailable
				return nil, &mbserver.GatewayTargetDeviceFailedtoRespond
			}
			return []byte{0x02, 0x00, 0x42}, &mbserver.Success
		},
	)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"modbus",
			map[string]string{
				"type":     cHoldingRegisters,
				"slave_id": "1",
				"name":     modbus.Name,
			},
			map[string]interface{}{"holding-0": int16(0x42)},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"modbus",
			map[string]string{
				"type":     cHoldingRegisters,
				"slave_id": "3",
				"name":     modbus.Name,
			},
			map[string]interface{}{"holding-0": int16(0x42)},
			time.Unix(0, 0),
		),
	}

	var acc testutil.Accumulator
	require.NoError(t, modbus.Gather(&acc))
	acc.Wait(len(expected))
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime(), testutil.SortMetrics())
	require.Len(t, acc.Errors, 1)
	require.ErrorContains(t, acc.FirstError(), `slave 2 on controller "tcp://localhost:1502": modbus: exception '11' (gateway target device failed to respond)`)
}

func TestRequestOptimizationShrink(t *testing.T) {
	maxsize := maxQuantityHoldingRegisters
	tests := []struct {
		name     string
		inputs   []rangeDefinition
		expected []requestExpectation
	}{
		{
			name: "no omit",
			inputs: []rangeDefinition{
				{0, 2 * maxQuantityHoldingRegisters, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 0, count: maxsize, length: 1}},
					req:    request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{{start: maxsize, count: maxsize, length: 1}},
					req:    request{address: maxsize, length: maxsize},
				},
			},
		},
		{
			name: "borders",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, maxsize - 2, 1, 1, "INT16", true},
				{maxsize - 1, 2, 1, 1, "INT16", false},
				{maxsize + 1, maxsize - 2, 1, 1, "INT16", true},
				{2*maxsize - 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: maxsize - 1, count: 1, length: 1},
					},
					req: request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize, count: 1, length: 1},
						{start: 2*maxsize - 1, count: 1, length: 1},
					},
					req: request{address: maxsize, length: maxsize},
				},
			},
		},
		{
			name: "borders with gap",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, maxsize - 2, 1, 1, "INT16", true},
				{maxsize - 1, 2, 1, 1, "INT16", false},
				{maxsize + 1, 4, 1, 1, "INT16", true},
				{2*maxsize - 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: maxsize - 1, count: 1, length: 1},
					},
					req: request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{{start: maxsize, count: 1, length: 1}},
					req:    request{address: maxsize, length: 1},
				},
				{
					fields: []rangeDefinition{{start: 2*maxsize - 1, count: 1, length: 1}},
					req:    request{address: 2*maxsize - 1, length: 1},
				},
			},
		},
		{
			name: "large gaps",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 18, count: 3, length: 1}},
					req:    request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{{start: maxsize - 2, count: 5, length: 1}},
					req:    request{address: maxsize - 2, length: 5},
				},
				{
					fields: []rangeDefinition{{start: maxsize + 42, count: 2, length: 1}},
					req:    request{address: maxsize + 42, length: 2},
				},
			},
		},
		{
			name: "large gaps filled",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, 17, 1, 1, "INT16", true},
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: 18, count: 3, length: 1},
						{start: maxsize - 2, count: 2, length: 1},
					},
					req: request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize, count: 3, length: 1},
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize, length: 44},
				},
			},
		},
		{
			name: "large gaps filled with offset",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 18, count: 3, length: 1},
						{start: maxsize - 2, count: 5, length: 1},
					},
					req: request{address: 18, length: 110},
				},
				{
					fields: []rangeDefinition{{start: maxsize + 42, count: 2, length: 1}},
					req:    request{address: maxsize + 42, length: 2},
				},
			},
		},
		{
			name: "worst case",
			inputs: []rangeDefinition{
				{0, maxsize, 2, 1, "INT16", false},
				{1, maxsize, 2, 1, "INT16", true},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 0, count: maxsize/2 + 1, increment: 2, length: 1}},
					req:    request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{{start: maxsize + 1, count: maxsize / 2, increment: 2, length: 1}},
					req:    request{address: maxsize + 1, length: maxsize - 2},
				},
			},
		},
		{
			name: "from PR #11106",
			inputs: []rangeDefinition{
				{0, 2, 1, 1, "INT16", true},
				{2, 1, 1, 1, "INT16", false},
				{3, 2*maxsize + 1, 1, 1, "INT16", true},
				{2*maxsize + 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 2, count: 1, length: 1}},
					req:    request{address: 2, length: 1},
				},
				{
					fields: []rangeDefinition{{start: 2*maxsize + 1, count: 1, length: 1}},
					req:    request{address: 2*maxsize + 1, length: 1},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate the input structure and the expectation
			requestFields := generateRequestDefinitions(tt.inputs)
			expected := generateExpectation(tt.expected)

			// Setup the plugin
			slaveID := byte(1)
			plugin := Modbus{
				Name:              "Test",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			plugin.Requests = []requestDefinition{
				{
					SlaveID:      slaveID,
					ByteOrder:    "ABCD",
					RegisterType: "holding",
					Optimization: "shrink",
					Fields:       requestFields,
				},
			}
			require.NoError(t, plugin.Init())
			require.NotEmpty(t, plugin.requests)
			require.Contains(t, plugin.requests, slaveID)
			requireEqualRequests(t, expected, plugin.requests[slaveID].holding)
		})
	}
}

func TestRequestOptimizationRearrange(t *testing.T) {
	maxsize := maxQuantityHoldingRegisters
	tests := []struct {
		name     string
		inputs   []rangeDefinition
		expected []requestExpectation
	}{
		{
			name: "no omit",
			inputs: []rangeDefinition{
				{0, 2 * maxQuantityHoldingRegisters, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 0, count: maxsize, length: 1}},
					req:    request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{{start: maxsize, count: maxsize, length: 1}},
					req:    request{address: maxsize, length: maxsize},
				},
			},
		},
		{
			name: "borders",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, maxsize - 2, 1, 1, "INT16", true},
				{maxsize - 1, 2, 1, 1, "INT16", false},
				{maxsize + 1, maxsize - 2, 1, 1, "INT16", true},
				{2*maxsize - 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: maxsize - 1, count: 1, length: 1},
					},
					req: request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize, count: 1, length: 1},
						{start: 2*maxsize - 1, count: 1, length: 1},
					},
					req: request{address: maxsize, length: maxsize},
				},
			},
		},
		{
			name: "borders with gap",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, maxsize - 2, 1, 1, "INT16", true},
				{maxsize - 1, 2, 1, 1, "INT16", false},
				{maxsize + 1, 4, 1, 1, "INT16", true},
				{2*maxsize - 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 0, count: 1, length: 1}},
					req:    request{address: 0, length: 1},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 1, count: 1, length: 1},
						{start: maxsize, count: 1, length: 1},
					},
					req: request{address: maxsize - 1, length: 2},
				},
				{
					fields: []rangeDefinition{{start: 2*maxsize - 1, count: 1, length: 1}},
					req:    request{address: 2*maxsize - 1, length: 1},
				},
			},
		},
		{
			name: "large gaps",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 18, count: 3, length: 1}},
					req:    request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{{start: maxsize - 2, count: 5, length: 1}},
					req:    request{address: maxsize - 2, length: 5},
				},
				{
					fields: []rangeDefinition{{start: maxsize + 42, count: 2, length: 1}},
					req:    request{address: maxsize + 42, length: 2},
				},
			},
		},
		{
			name: "large gaps filled",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, 17, 1, 1, "INT16", true},
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: 18, count: 3, length: 1},
					},
					req: request{address: 0, length: 21},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize - 2, length: 46},
				},
			},
		},
		{
			name: "large gaps filled with offset",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 18, count: 3, length: 1}},
					req:    request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize - 2, length: 46},
				},
			},
		},
		{
			name: "from PR #11106",
			inputs: []rangeDefinition{
				{0, 2, 1, 1, "INT16", true},
				{2, 1, 1, 1, "INT16", false},
				{3, 2*maxsize + 1, 1, 1, "INT16", true},
				{2*maxsize + 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 2, count: 1, length: 1}},
					req:    request{address: 2, length: 1},
				},
				{
					fields: []rangeDefinition{{start: 2*maxsize + 1, count: 1, length: 1}},
					req:    request{address: 2*maxsize + 1, length: 1},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate the input structure and the expectation
			requestFields := generateRequestDefinitions(tt.inputs)
			expected := generateExpectation(tt.expected)

			// Setup the plugin
			slaveID := byte(1)
			plugin := Modbus{
				Name:              "Test",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			plugin.Requests = []requestDefinition{
				{
					SlaveID:      slaveID,
					ByteOrder:    "ABCD",
					RegisterType: "holding",
					Optimization: "rearrange",
					Fields:       requestFields,
				},
			}
			require.NoError(t, plugin.Init())
			require.NotEmpty(t, plugin.requests)
			require.Contains(t, plugin.requests, slaveID)
			requireEqualRequests(t, expected, plugin.requests[slaveID].holding)
		})
	}
}

func TestRequestOptimizationAggressive(t *testing.T) {
	maxsize := maxQuantityHoldingRegisters
	tests := []struct {
		name     string
		inputs   []rangeDefinition
		expected []requestExpectation
	}{
		{
			name: "no omit",
			inputs: []rangeDefinition{
				{0, 2 * maxQuantityHoldingRegisters, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 0, count: maxsize, length: 1}},
					req:    request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{{start: maxsize, count: maxsize, length: 1}},
					req:    request{address: maxsize, length: maxsize},
				},
			},
		},
		{
			name: "borders",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, maxsize - 2, 1, 1, "INT16", true},
				{maxsize - 1, 2, 1, 1, "INT16", false},
				{maxsize + 1, maxsize - 2, 1, 1, "INT16", true},
				{2*maxsize - 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: maxsize - 1, count: 1, length: 1},
					},
					req: request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize, count: 1, length: 1},
						{start: 2*maxsize - 1, count: 1, length: 1},
					},
					req: request{address: maxsize, length: maxsize},
				},
			},
		},
		{
			name: "borders with gap",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, maxsize - 2, 1, 1, "INT16", true},
				{maxsize - 1, 2, 1, 1, "INT16", false},
				{maxsize + 1, 4, 1, 1, "INT16", true},
				{2*maxsize - 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: maxsize - 1, count: 1, length: 1},
					},
					req: request{address: 0, length: maxsize},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize, count: 1, length: 1},
						{start: 2*maxsize - 1, count: 1, length: 1},
					},
					req: request{address: maxsize, length: maxsize},
				},
			},
		},
		{
			name: "large gaps",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 18, count: 3, length: 1}},
					req:    request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize - 2, length: 46},
				},
			},
		},
		{
			name: "large gaps filled",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, 17, 1, 1, "INT16", true},
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
						{start: 18, count: 3, length: 1},
					},
					req: request{address: 0, length: 21},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize - 2, length: 46},
				},
			},
		},
		{
			name: "large gaps filled with offset",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 18, count: 3, length: 1}},
					req:    request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize - 2, length: 46},
				},
			},
		},
		{
			name: "from PR #11106",
			inputs: []rangeDefinition{
				{0, 2, 1, 1, "INT16", true},
				{2, 1, 1, 1, "INT16", false},
				{3, 2*maxsize + 1, 1, 1, "INT16", true},
				{2*maxsize + 1, 1, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 2, count: 1, length: 1}},
					req:    request{address: 2, length: 1},
				},
				{
					fields: []rangeDefinition{{start: 2*maxsize + 1, count: 1, length: 1}},
					req:    request{address: 2*maxsize + 1, length: 1},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate the input structure and the expectation
			requestFields := generateRequestDefinitions(tt.inputs)
			expected := generateExpectation(tt.expected)

			// Setup the plugin
			slaveID := byte(1)
			plugin := Modbus{
				Name:              "Test",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			plugin.Requests = []requestDefinition{
				{
					SlaveID:      slaveID,
					ByteOrder:    "ABCD",
					RegisterType: "holding",
					Optimization: "aggressive",
					Fields:       requestFields,
				},
			}
			require.NoError(t, plugin.Init())
			require.NotEmpty(t, plugin.requests)
			require.Contains(t, plugin.requests, slaveID)
			requireEqualRequests(t, expected, plugin.requests[slaveID].holding)
		})
	}
}

func TestRequestOptimizationMaxExtraRegisterFail(t *testing.T) {
	tests := []struct {
		name     string
		requests []requestDefinition
		errormsg string
	}{{
		name: "MaxExtraRegister too large",
		requests: []requestDefinition{
			{
				SlaveID:           1,
				ByteOrder:         "ABCD",
				RegisterType:      "input",
				Optimization:      "max_insert",
				MaxExtraRegisters: 5000,
				Fields: []requestFieldDefinition{
					{
						Name:        "input-0",
						Address:     uint16(0),
						Measurement: "foo",
					},
				},
			},
		},
		errormsg: "optimization_max_register_fill has to be between 1 and 125",
	},
		{
			name: "MaxExtraRegister too small",
			requests: []requestDefinition{
				{
					SlaveID:           1,
					ByteOrder:         "ABCD",
					RegisterType:      "input",
					Optimization:      "max_insert",
					MaxExtraRegisters: 0,
					Fields: []requestFieldDefinition{
						{
							Name:        "input-0",
							Address:     uint16(0),
							Measurement: "foo",
						},
					},
				},
			},
			errormsg: "optimization_max_register_fill has to be between 1 and 125",
		}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := Modbus{
				Name:              "Test",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			plugin.Requests = tt.requests

			require.ErrorContains(t, plugin.Init(), tt.errormsg)
			require.Empty(t, plugin.requests)
		})
	}
}

func TestRequestOptimizationMaxInsertSmall(t *testing.T) {
	maxsize := maxQuantityHoldingRegisters
	maxExtraRegisters := uint16(5)
	tests := []struct {
		name     string
		inputs   []rangeDefinition
		expected []requestExpectation
	}{
		{
			name: "large gaps",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 18, count: 3, length: 1}},
					req:    request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
					},
					req: request{address: maxsize - 2, length: 5},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize + 42, length: 2},
				},
			},
		},
		{
			name: "large gaps filled",
			inputs: []rangeDefinition{
				{0, 1, 1, 1, "INT16", false},
				{1, 17, 1, 1, "INT16", true},
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{
						{start: 0, count: 1, length: 1},
					},
					req: request{address: 0, length: 1},
				},
				{
					fields: []rangeDefinition{
						{start: 18, count: 3, length: 1},
					},
					req: request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
					},
					req: request{address: maxsize - 2, length: 5},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize + 42, length: 2},
				},
			},
		},
		{
			name: "large gaps filled with offset",
			inputs: []rangeDefinition{
				{18, 3, 1, 1, "INT16", false},
				{21, maxsize - 23, 1, 1, "INT16", true},
				{maxsize - 2, 5, 1, 1, "INT16", false},
				{maxsize + 3, 39, 1, 1, "INT16", true},
				{maxsize + 42, 2, 1, 1, "INT16", false},
			},
			expected: []requestExpectation{
				{
					fields: []rangeDefinition{{start: 18, count: 3, length: 1}},
					req:    request{address: 18, length: 3},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize - 2, count: 5, length: 1},
					},
					req: request{address: maxsize - 2, length: 5},
				},
				{
					fields: []rangeDefinition{
						{start: maxsize + 42, count: 2, length: 1},
					},
					req: request{address: maxsize + 42, length: 2},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate the input structure and the expectation
			requestFields := generateRequestDefinitions(tt.inputs)
			expected := generateExpectation(tt.expected)

			// Setup the plugin
			slaveID := byte(1)
			plugin := Modbus{
				Name:              "Test",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{},
			}
			plugin.Requests = []requestDefinition{
				{
					SlaveID:           slaveID,
					ByteOrder:         "ABCD",
					RegisterType:      "holding",
					Optimization:      "max_insert",
					MaxExtraRegisters: maxExtraRegisters,
					Fields:            requestFields,
				},
			}
			require.NoError(t, plugin.Init())
			require.NotEmpty(t, plugin.requests)
			require.Contains(t, plugin.requests, slaveID)
			requireEqualRequests(t, expected, plugin.requests[slaveID].holding)
		})
	}
}
func TestRequestWorkaroundsOneRequestPerField(t *testing.T) {
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
		Workarounds:       workarounds{OnRequestPerField: true},
	}
	plugin.Requests = []requestDefinition{
		{
			SlaveID:      1,
			ByteOrder:    "ABCD",
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "holding-1",
					Address:   uint16(1),
					InputType: "INT16",
				},
				{
					Name:      "holding-2",
					Address:   uint16(2),
					InputType: "INT16",
				},
				{
					Name:      "holding-3",
					Address:   uint16(3),
					InputType: "INT16",
				},
				{
					Name:      "holding-4",
					Address:   uint16(4),
					InputType: "INT16",
				},
				{
					Name:      "holding-5",
					Address:   uint16(5),
					InputType: "INT16",
				},
			},
		},
	}
	require.NoError(t, plugin.Init())
	require.Len(t, plugin.requests[1].holding, len(plugin.Requests[0].Fields))
}

func TestRequestWorkaroundsReadCoilsStartingAtZeroRequest(t *testing.T) {
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{},
		Workarounds:       workarounds{ReadCoilsStartingAtZero: true},
	}
	plugin.SlaveID = 1
	plugin.Requests = []requestDefinition{
		{
			SlaveID:      1,
			RegisterType: "coil",
			Fields: []requestFieldDefinition{
				{
					Name:    "coil-8",
					Address: uint16(8),
				},
				{
					Name:    "coil-new-group",
					Address: maxQuantityCoils,
				},
			},
		},
	}
	require.NoError(t, plugin.Init())
	require.Len(t, plugin.requests[1].coil, 2)

	// First group should now start at zero and have the cumulated length
	require.Equal(t, uint16(0), plugin.requests[1].coil[0].address)
	require.Equal(t, uint16(9), plugin.requests[1].coil[0].length)

	// The second field should form a new group as the previous request
	// is now too large (beyond max-coils-per-read) after zero enforcement.
	require.Equal(t, maxQuantityCoils, plugin.requests[1].coil[1].address)
	require.Equal(t, uint16(1), plugin.requests[1].coil[1].length)
}

func TestRequestOverlap(t *testing.T) {
	logger := &testutil.CaptureLogger{}
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               logger,
		Workarounds:       workarounds{ReadCoilsStartingAtZero: true},
	}
	plugin.Requests = []requestDefinition{
		{
			SlaveID:           1,
			RegisterType:      "holding",
			Optimization:      "max_insert",
			MaxExtraRegisters: 16,
			Fields: []requestFieldDefinition{
				{
					Name:      "field-1",
					InputType: "UINT32",
					Address:   uint16(1),
				},
				{
					Name:      "field-2",
					InputType: "UINT64",
					Address:   uint16(3),
				},
				{
					Name:      "field-3",
					InputType: "UINT32",
					Address:   uint16(5),
				},
				{
					Name:      "field-4",
					InputType: "UINT32",
					Address:   uint16(7),
				},
			},
		},
	}
	require.NoError(t, plugin.Init())

	require.Eventually(t, func() bool {
		return len(logger.Warnings()) > 0
	}, 3*time.Second, 100*time.Millisecond)

	var found bool
	for _, w := range logger.Warnings() {
		if strings.Contains(w, "Request at 3 with length 4 overlaps with next request at 5") {
			found = true
			break
		}
	}
	require.True(t, found, "Overlap warning not found!")

	require.Len(t, plugin.requests, 1)
	require.Len(t, plugin.requests[1].holding, 1)
}

func TestRequestAddressOverflow(t *testing.T) {
	logger := &testutil.CaptureLogger{}
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               logger,
		Workarounds:       workarounds{ReadCoilsStartingAtZero: true},
	}
	plugin.Requests = []requestDefinition{
		{
			SlaveID:      1,
			RegisterType: "holding",
			Fields: []requestFieldDefinition{
				{
					Name:      "field",
					InputType: "UINT64",
					Address:   uint16(65534),
				},
			},
		},
	}
	require.ErrorIs(t, plugin.Init(), errAddressOverflow)
}
