package snmp

import (
	"testing"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"
)

func TestConvertDefault(t *testing.T) {
	tests := []struct {
		name     string
		ent      gosnmp.SnmpPDU
		expected interface{}
		errmsg   string
	}{
		{
			name: "integer",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.Integer,
				Value: int(2),
			},
			expected: 2,
		},
		{
			name: "octet string with valid bytes",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0x77, 0x6F, 0x72, 0x6C, 0x64},
			},
			expected: "Hello world",
		},
		{
			name: "octet string with invalid bytes",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8, 0x7, 0xff, 0xfd, 0x38, 0x54, 0xc1},
			},
			expected: "84c807fffd3854c1",
		},
	}

	f := Field{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := f.Convert(tt.ent)

			if tt.errmsg != "" {
				require.ErrorContains(t, err, tt.errmsg)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expected, actual)
		})
	}

	t.Run("invalid", func(t *testing.T) {
		f.Conversion = "invalid"
		actual, err := f.Convert(gosnmp.SnmpPDU{})

		require.Nil(t, actual)
		require.ErrorContains(t, err, "invalid conversion type")
	})
}

func TestConvertHex(t *testing.T) {
	tests := []struct {
		name     string
		ent      gosnmp.SnmpPDU
		expected interface{}
		errmsg   string
	}{
		{
			name: "octet string with valid bytes",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0x77, 0x6F, 0x72, 0x6C, 0x64},
			},
			expected: "48656c6c6f20776f726c64",
		},
		{
			name: "octet string with invalid bytes",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8, 0x7, 0xff, 0xfd, 0x38, 0x54, 0xc1},
			},
			expected: "84c807fffd3854c1",
		},
		{
			name: "IPv4",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.IPAddress,
				Value: "192.0.2.1",
			},
			expected: "c0000201",
		},
		{
			name: "IPv6",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.IPAddress,
				Value: "2001:db8::1",
			},
			expected: "20010db8000000000000000000000001",
		},
		{
			name: "oid",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.ObjectIdentifier,
				Value: ".1.2.3",
			},
			errmsg: "unsupported Asn1BER (0x6) for hex conversion",
		},
		{
			name: "integer",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.Integer,
				Value: int(2),
			},
			errmsg: "unsupported type (int) for hex conversion",
		},
	}

	f := Field{Conversion: "hex"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := f.Convert(tt.ent)

			if tt.errmsg != "" {
				require.ErrorContains(t, err, tt.errmsg)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestConvertHextoint(t *testing.T) {
	tests := []struct {
		name       string
		conversion string
		ent        gosnmp.SnmpPDU
		expected   interface{}
		errmsg     string
	}{
		{
			name:       "empty",
			conversion: "hextoint:BigEndian:uint64",
			ent:        gosnmp.SnmpPDU{},
			expected:   nil,
		},
		{
			name:       "big endian uint64",
			conversion: "hextoint:BigEndian:uint64",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8, 0x7, 0xff, 0xfd, 0x38, 0x54, 0xc1},
			},
			expected: uint64(0x84c807fffd3854c1),
		},
		{
			name:       "big endian uint32",
			conversion: "hextoint:BigEndian:uint32",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8, 0x7, 0xff},
			},
			expected: uint32(0x84c807ff),
		},
		{
			name:       "big endian uint16",
			conversion: "hextoint:BigEndian:uint16",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8},
			},
			expected: uint16(0x84c8),
		},
		{
			name:       "big endian invalid",
			conversion: "hextoint:BigEndian:invalid",
			ent:        gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: make([]uint8, 0)},
			errmsg:     "invalid bit value",
		},
		{
			name:       "little endian uint64",
			conversion: "hextoint:LittleEndian:uint64",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8, 0x7, 0xff, 0xfd, 0x38, 0x54, 0xc1},
			},
			expected: uint64(0xc15438fdff07c884),
		},
		{
			name:       "little endian uint32",
			conversion: "hextoint:LittleEndian:uint32",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8, 0x7, 0xff},
			},
			expected: uint32(0xff07c884),
		},
		{
			name:       "little endian uint16",
			conversion: "hextoint:LittleEndian:uint16",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84, 0xc8},
			},
			expected: uint16(0xc884),
		},
		{
			name:       "little endian single byte",
			conversion: "hextoint:LittleEndian:uint16",
			ent: gosnmp.SnmpPDU{
				Type:  gosnmp.OctetString,
				Value: []byte{0x84},
			},
			expected: uint16(0x84),
		},
		{
			name:       "little endian invalid",
			conversion: "hextoint:LittleEndian:invalid",
			ent:        gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: make([]byte, 0)},
			errmsg:     "invalid bit value",
		},
		{
			name:       "invalid",
			conversion: "hextoint:invalid:uint64",
			ent:        gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: make([]byte, 0)},
			errmsg:     "invalid Endian value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Field{Conversion: tt.conversion}

			actual, err := f.Convert(tt.ent)

			if tt.errmsg != "" {
				require.ErrorContains(t, err, tt.errmsg)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expected, actual)
		})
	}
}
