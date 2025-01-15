package sflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRawPacketHeaderFlowData(t *testing.T) {
	h := rawPacketHeaderFlowData{
		headerProtocol: headerProtocolTypeEthernetISO88023,
		frameLength:    64,
		bytes:          64,
		strippedOctets: 0,
		headerLength:   0,
		header:         nil,
	}
	tags := h.getTags()
	fields := h.getFields()

	require.NotNil(t, fields)
	require.NotNil(t, tags)
	require.Contains(t, tags, "header_protocol")
	require.Len(t, tags, 1)
}

// process a raw ethernet packet without any encapsulated protocol
func TestEthHeader(t *testing.T) {
	h := ethHeader{
		destinationMAC:        [6]byte{0xca, 0xff, 0xee, 0xff, 0xe, 0x0},
		sourceMAC:             [6]byte{0xde, 0xad, 0xbe, 0xef, 0x0, 0x0},
		tagProtocolIdentifier: 0x88B5, // IEEE Std 802 - Local Experimental Ethertype
		tagControlInformation: 0,
		etherTypeCode:         0,
		etherType:             "",
		ipHeader:              nil,
	}
	tags := h.getTags()
	fields := h.getFields()

	require.NotNil(t, fields)
	require.NotNil(t, tags)
}
