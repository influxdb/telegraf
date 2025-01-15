package sflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUDPHeader(t *testing.T) {
	octets := bytes.NewBuffer([]byte{
		0x00, 0x01, // src_port
		0x00, 0x02, // dst_port
		0x00, 0x03, // udp_length
		0x00, 0x00, // checksum
	})

	actual, err := decodeUDPHeader(octets)
	require.NoError(t, err)

	expected := udpHeader{
		sourcePort:      1,
		destinationPort: 2,
		udpLength:       3,
	}

	require.Equal(t, expected, actual)
}

func BenchmarkUDPHeader(b *testing.B) {
	octets := bytes.NewBuffer([]byte{
		0x00, 0x01, // src_port
		0x00, 0x02, // dst_port
		0x00, 0x03, // udp_length
		0x00, 0x00, // checksum
	})

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := decodeUDPHeader(octets)
		require.NoError(b, err)
	}
}

func TestIPv4Header(t *testing.T) {
	octets := bytes.NewBuffer(
		[]byte{
			0x45,       // version + IHL
			0x00,       // ip_dscp + ip_ecn
			0x00, 0x00, // total length
			0x00, 0x00, // identification
			0x00, 0x00, // flags + frag offset
			0x00,       // ttl
			0x11,       // protocol udp (0x11)
			0x00, 0x00, // header checksum
			0x7f, 0x00, 0x00, 0x01, // src ip
			0x7f, 0x00, 0x00, 0x02, // dst ip
			0x00, 0x01, // src_port
			0x00, 0x02, // dst_port
			0x00, 0x03, // udp_length
			0x00, 0x00, // checksum
		},
	)
	dc := newDecoder()
	actual, err := dc.decodeIPv4Header(octets)
	require.NoError(t, err)

	expected := ipV4Header{
		version:              0x40,
		internetHeaderLength: 0x05,
		dscp:                 0,
		ecn:                  0,
		totalLength:          0,
		identification:       0,
		flags:                0,
		fragmentOffset:       0,
		ttl:                  0,
		protocol:             0x11,
		headerChecksum:       0,
		sourceIP:             [4]byte{127, 0, 0, 1},
		destIP:               [4]byte{127, 0, 0, 2},
		protocolHeader: udpHeader{
			sourcePort:      1,
			destinationPort: 2,
			udpLength:       3,
			checksum:        0,
		},
	}

	require.Equal(t, expected, actual)
}

// Using the same Directive instance, prior paths through the parse tree should
// not affect the latest parse.
func TestIPv4HeaderSwitch(t *testing.T) {
	octets := bytes.NewBuffer(
		[]byte{
			0x45,       // version + IHL
			0x00,       // ip_dscp + ip_ecn
			0x00, 0x00, // total length
			0x00, 0x00, // identification
			0x00, 0x00, // flags + frag offset
			0x00,       // ttl
			0x11,       // protocol udp (0x11)
			0x00, 0x00, // header checksum
			0x7f, 0x00, 0x00, 0x01, // src ip
			0x7f, 0x00, 0x00, 0x02, // dst ip
			0x00, 0x01, // src_port
			0x00, 0x02, // dst_port
			0x00, 0x03, // udp_length
			0x00, 0x00, // checksum
		},
	)
	dc := newDecoder()
	_, err := dc.decodeIPv4Header(octets)
	require.NoError(t, err)

	octets = bytes.NewBuffer(
		[]byte{
			0x45,       // version + IHL
			0x00,       // ip_dscp + ip_ecn
			0x00, 0x00, // total length
			0x00, 0x00, // identification
			0x00, 0x00, // flags + frag offset
			0x00,       // ttl
			0x06,       // protocol tcp (0x06)
			0x00, 0x00, // header checksum
			0x7f, 0x00, 0x00, 0x01, // src ip
			0x7f, 0x00, 0x00, 0x02, // dst ip
			0x00, 0x01, // src_port
			0x00, 0x02, // dst_port
			0x00, 0x00, 0x00, 0x00, // sequence
			0x00, 0x00, 0x00, 0x00, // ack_number
			0x00, 0x00, // tcp_header_length
			0x00, 0x00, // tcp_window_size
			0x00, 0x00, // checksum
			0x00, 0x00, // tcp_urgent_pointer
		},
	)
	dc = newDecoder()
	actual, err := dc.decodeIPv4Header(octets)
	require.NoError(t, err)

	expected := ipV4Header{
		version:              64,
		internetHeaderLength: 5,
		protocol:             6,
		sourceIP:             [4]byte{127, 0, 0, 1},
		destIP:               [4]byte{127, 0, 0, 2},
		protocolHeader: tcpHeader{
			sourcePort:      1,
			destinationPort: 2,
		},
	}

	require.Equal(t, expected, actual)
}

func TestUnknownProtocol(t *testing.T) {
	octets := bytes.NewBuffer(
		[]byte{
			0x45,       // version + IHL
			0x00,       // ip_dscp + ip_ecn
			0x00, 0x00, // total length
			0x00, 0x00, // identification
			0x00, 0x00, // flags + frag offset
			0x00,       // ttl
			0x99,       // protocol
			0x00, 0x00, // header checksum
			0x7f, 0x00, 0x00, 0x01, // src ip
			0x7f, 0x00, 0x00, 0x02, // dst ip
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
		},
	)
	dc := newDecoder()
	actual, err := dc.decodeIPv4Header(octets)
	require.NoError(t, err)

	expected := ipV4Header{
		version:              64,
		internetHeaderLength: 5,
		protocol:             153,
		sourceIP:             [4]byte{127, 0, 0, 1},
		destIP:               [4]byte{127, 0, 0, 2},
	}

	require.Equal(t, expected, actual)
}
