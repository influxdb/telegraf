package sflow

import (
	"net"
	"strconv"
)

const (
	ipProtocolTCP uint8 = 6
	ipProtocolUDP uint8 = 17
)

var eTypeMap = map[uint16]string{
	0x0800: "IPv4",
	0x86DD: "IPv6",
}

type containsMetricData interface {
	getTags() map[string]string
	getFields() map[string]interface{}
}

// v5Format answers and decoder.Directive capable of decoding sFlow v5 packets in accordance
// with SFlow v5 specification at https://sflow.org/sflow_version_5.txt
type v5Format struct {
	version        uint32
	agentAddress   net.IPAddr
	subAgentID     uint32
	sequenceNumber uint32
	uptime         uint32
	samples        []sample
}

type sampleType uint32

const (
	sampleTypeFlowSample         sampleType = 1 // sflow_version_5.txt line: 1614
	sampleTypeFlowSampleExpanded sampleType = 3 // sflow_version_5.txt line: 1698
)

type sample struct {
	smplType sampleType
	smplData sampleDataFlowSampleExpanded
}

type sampleDataFlowSampleExpanded struct {
	sequenceNumber  uint32
	sourceIDType    uint32
	sourceIDIndex   uint32
	samplingRate    uint32
	samplePool      uint32
	drops           uint32
	sampleDirection string // ingress/egress
	inputIfFormat   uint32
	inputIfIndex    uint32
	outputIfFormat  uint32
	outputIfIndex   uint32
	flowRecords     []flowRecord
}

type flowFormatType uint32

const (
	flowFormatTypeRawPacketHeader flowFormatType = 1 // sflow_version_5.txt line: 1938
)

type flowData containsMetricData

type flowRecord struct {
	flowFormat flowFormatType
	flowData   flowData
}

type headerProtocolType uint32

const (
	headerProtocolTypeEthernetISO88023  headerProtocolType = 1
	headerProtocolTypeISO88024TokenBus  headerProtocolType = 2
	headerProtocolTypeISO88025TokenRing headerProtocolType = 3
	headerProtocolTypeFDDI              headerProtocolType = 4
	headerProtocolTypeFrameRelay        headerProtocolType = 5
	headerProtocolTypeX25               headerProtocolType = 6
	headerProtocolTypePPP               headerProtocolType = 7
	headerProtocolTypeSMDS              headerProtocolType = 8
	headerProtocolTypeAAL5              headerProtocolType = 9
	headerProtocolTypeAAL5IP            headerProtocolType = 10 /* e.g. Cisco AAL5 mux */
	headerProtocolTypeIPv4              headerProtocolType = 11
	headerProtocolTypeIPv6              headerProtocolType = 12
	headerProtocolTypeMPLS              headerProtocolType = 13
	headerProtocolTypePOS               headerProtocolType = 14 /* RFC 1662, 2615 */
)

var headerProtocolMap = map[headerProtocolType]string{
	headerProtocolTypeEthernetISO88023: "ETHERNET-ISO88023", // sflow_version_5.txt line: 1920
}

type header containsMetricData

type rawPacketHeaderFlowData struct {
	headerProtocol headerProtocolType
	frameLength    uint32
	bytes          uint32
	strippedOctets uint32
	headerLength   uint32
	header         header
}

func (h rawPacketHeaderFlowData) getTags() map[string]string {
	var t map[string]string
	if h.header != nil {
		t = h.header.getTags()
	} else {
		t = make(map[string]string, 1)
	}
	t["header_protocol"] = headerProtocolMap[h.headerProtocol]
	return t
}

func (h rawPacketHeaderFlowData) getFields() map[string]interface{} {
	var f map[string]interface{}
	if h.header != nil {
		f = h.header.getFields()
	} else {
		f = make(map[string]interface{}, 3)
	}
	f["bytes"] = h.bytes
	f["frame_length"] = h.frameLength
	f["header_length"] = h.headerLength
	return f
}

type ipHeader containsMetricData

type ethHeader struct {
	destinationMAC        [6]byte
	sourceMAC             [6]byte
	tagProtocolIdentifier uint16
	tagControlInformation uint16
	etherTypeCode         uint16
	etherType             string
	ipHeader              ipHeader
}

func (h ethHeader) getTags() map[string]string {
	var t map[string]string
	if h.ipHeader != nil {
		t = h.ipHeader.getTags()
	} else {
		t = make(map[string]string, 3)
	}
	t["src_mac"] = net.HardwareAddr(h.sourceMAC[:]).String()
	t["dst_mac"] = net.HardwareAddr(h.destinationMAC[:]).String()
	t["ether_type"] = h.etherType
	return t
}

func (h ethHeader) getFields() map[string]interface{} {
	if h.ipHeader != nil {
		return h.ipHeader.getFields()
	}
	return make(map[string]interface{})
}

type protocolHeader containsMetricData

// https://en.wikipedia.org/wiki/IPv4#Header
type ipV4Header struct {
	version              uint8 // 4 bit
	internetHeaderLength uint8 // 4 bit
	dscp                 uint8
	ecn                  uint8
	totalLength          uint16
	identification       uint16
	flags                uint8
	fragmentOffset       uint16
	ttl                  uint8
	protocol             uint8 // https://en.wikipedia.org/wiki/List_of_IP_protocol_numbers
	headerChecksum       uint16
	sourceIP             [4]byte
	destIP               [4]byte
	protocolHeader       protocolHeader
}

func (h ipV4Header) getTags() map[string]string {
	var t map[string]string
	if h.protocolHeader != nil {
		t = h.protocolHeader.getTags()
	} else {
		t = make(map[string]string, 2)
	}
	t["src_ip"] = net.IP(h.sourceIP[:]).String()
	t["dst_ip"] = net.IP(h.destIP[:]).String()
	return t
}

func (h ipV4Header) getFields() map[string]interface{} {
	var f map[string]interface{}
	if h.protocolHeader != nil {
		f = h.protocolHeader.getFields()
	} else {
		f = make(map[string]interface{}, 6)
	}
	f["ip_dscp"] = strconv.FormatUint(uint64(h.dscp), 10)
	f["ip_ecn"] = strconv.FormatUint(uint64(h.ecn), 10)
	f["ip_flags"] = h.flags
	f["ip_fragment_offset"] = h.fragmentOffset
	f["ip_total_length"] = h.totalLength
	f["ip_ttl"] = h.ttl
	return f
}

// https://en.wikipedia.org/wiki/IPv6_packet
type ipV6Header struct {
	dscp            uint8
	ecn             uint8
	payloadLength   uint16
	nextHeaderProto uint8 // tcp/udp?
	hopLimit        uint8
	sourceIP        [16]byte
	destIP          [16]byte
	protocolHeader  protocolHeader
}

func (h ipV6Header) getTags() map[string]string {
	var t map[string]string
	if h.protocolHeader != nil {
		t = h.protocolHeader.getTags()
	} else {
		t = make(map[string]string, 2)
	}
	t["src_ip"] = net.IP(h.sourceIP[:]).String()
	t["dst_ip"] = net.IP(h.destIP[:]).String()
	return t
}

func (h ipV6Header) getFields() map[string]interface{} {
	var f map[string]interface{}
	if h.protocolHeader != nil {
		f = h.protocolHeader.getFields()
	} else {
		f = make(map[string]interface{}, 3)
	}
	f["ip_dscp"] = strconv.FormatUint(uint64(h.dscp), 10)
	f["ip_ecn"] = strconv.FormatUint(uint64(h.ecn), 10)
	f["payload_length"] = h.payloadLength
	return f
}

// https://en.wikipedia.org/wiki/Transmission_Control_Protocol
type tcpHeader struct {
	sourcePort       uint16
	destinationPort  uint16
	sequence         uint32
	ackNumber        uint32
	tcpHeaderLength  uint8
	flags            uint16
	tcpWindowSize    uint16
	checksum         uint16
	tcpUrgentPointer uint16
}

func (h tcpHeader) getTags() map[string]string {
	t := map[string]string{
		"dst_port": strconv.FormatUint(uint64(h.destinationPort), 10),
		"src_port": strconv.FormatUint(uint64(h.sourcePort), 10),
	}
	return t
}

func (h tcpHeader) getFields() map[string]interface{} {
	return map[string]interface{}{
		"tcp_header_length":  h.tcpHeaderLength,
		"tcp_urgent_pointer": h.tcpUrgentPointer,
		"tcp_window_size":    h.tcpWindowSize,
	}
}

type udpHeader struct {
	sourcePort      uint16
	destinationPort uint16
	udpLength       uint16
	checksum        uint16
}

func (h udpHeader) getTags() map[string]string {
	t := map[string]string{
		"dst_port": strconv.FormatUint(uint64(h.destinationPort), 10),
		"src_port": strconv.FormatUint(uint64(h.sourcePort), 10),
	}
	return t
}

func (h udpHeader) getFields() map[string]interface{} {
	return map[string]interface{}{
		"udp_length": h.udpLength,
	}
}
