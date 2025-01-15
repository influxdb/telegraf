package sflow

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs/sflow/binaryio"
)

type packetDecoder struct {
	onPacketF func(p *v5Format)
	Log       telegraf.Logger
}

func newDecoder() *packetDecoder {
	return &packetDecoder{}
}

func (d *packetDecoder) debug(args ...interface{}) {
	if d.Log != nil {
		d.Log.Debug(args...)
	}
}

func (d *packetDecoder) onPacket(f func(p *v5Format)) {
	d.onPacketF = f
}

func (d *packetDecoder) decode(r io.Reader) error {
	var err error
	var packet *v5Format
	for err == nil {
		packet, err = d.decodeOnePacket(r)
		if err != nil {
			break
		}
		d.onPacketF(packet)
	}
	if err != nil && errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

type addressType uint32 // must be uint32

const (
	addressTypeUnknown addressType = 0
	addressTypeIPV4    addressType = 1
	addressTypeIPV6    addressType = 2
)

func (d *packetDecoder) decodeOnePacket(r io.Reader) (*v5Format, error) {
	p := &v5Format{}
	err := read(r, &p.version, "version")
	if err != nil {
		return nil, err
	}
	if p.version != 5 {
		return nil, fmt.Errorf("version %d not supported, only version 5", p.version)
	}
	var addressIPType addressType
	if err := read(r, &addressIPType, "address ip type"); err != nil {
		return nil, err
	}
	switch addressIPType {
	case addressTypeUnknown:
		p.agentAddress.IP = make([]byte, 0)
	case addressTypeIPV4:
		p.agentAddress.IP = make([]byte, 4)
	case addressTypeIPV6:
		p.agentAddress.IP = make([]byte, 16)
	default:
		return nil, fmt.Errorf("unknown address IP type %d", addressIPType)
	}
	if err := read(r, &p.agentAddress.IP, "Agent Address IP"); err != nil {
		return nil, err
	}
	if err := read(r, &p.subAgentID, "SubAgentID"); err != nil {
		return nil, err
	}
	if err := read(r, &p.sequenceNumber, "SequenceNumber"); err != nil {
		return nil, err
	}
	if err := read(r, &p.uptime, "Uptime"); err != nil {
		return nil, err
	}

	p.samples, err = d.decodeSamples(r)
	return p, err
}

func (d *packetDecoder) decodeSamples(r io.Reader) ([]sample, error) {
	// # of samples
	var numOfSamples uint32
	if err := read(r, &numOfSamples, "sample count"); err != nil {
		return nil, err
	}

	result := make([]sample, 0, numOfSamples)
	for i := 0; i < int(numOfSamples); i++ {
		sam, err := d.decodeSample(r)
		if err != nil {
			return result, err
		}
		result = append(result, sam)
	}

	return result, nil
}

func (d *packetDecoder) decodeSample(r io.Reader) (sample, error) {
	var err error
	sam := sample{}
	if err := read(r, &sam.smplType, "sampleType"); err != nil {
		return sam, err
	}
	sampleDataLen := uint32(0)
	if err := read(r, &sampleDataLen, "Sample data length"); err != nil {
		return sam, err
	}
	mr := binaryio.MinReader(r, int64(sampleDataLen))
	defer mr.Close()

	switch sam.smplType {
	case sampleTypeFlowSample:
		sam.smplData, err = d.decodeFlowSample(mr)
	case sampleTypeFlowSampleExpanded:
		sam.smplData, err = d.decodeFlowSampleExpanded(mr)
	default:
		d.debug("Unknown sample type: ", sam.smplType)
	}
	return sam, err
}

func (d *packetDecoder) decodeFlowSample(r io.Reader) (t sampleDataFlowSampleExpanded, err error) {
	if err := read(r, &t.sequenceNumber, "SequenceNumber"); err != nil {
		return t, err
	}
	var sourceID uint32
	if err := read(r, &sourceID, "SourceID"); err != nil { // source_id sflow_version_5.txt line: 1622
		return t, err
	}
	// split source id to source id type and source id index
	t.sourceIDIndex = sourceID & 0x00ffffff // sflow_version_5.txt line: 1468
	t.sourceIDType = sourceID >> 24         // source_id_type sflow_version_5.txt Line 1465
	if err := read(r, &t.samplingRate, "SamplingRate"); err != nil {
		return t, err
	}
	if err := read(r, &t.samplePool, "SamplePool"); err != nil {
		return t, err
	}
	if err := read(r, &t.drops, "Drops"); err != nil { // sflow_version_5.txt line 1636
		return t, err
	}
	if err := read(r, &t.inputIfIndex, "InputIfIndex"); err != nil {
		return t, err
	}
	t.inputIfFormat = t.inputIfIndex >> 30
	t.inputIfIndex = t.inputIfIndex & 0x3FFFFFFF

	if err := read(r, &t.outputIfIndex, "OutputIfIndex"); err != nil {
		return t, err
	}
	t.outputIfFormat = t.outputIfIndex >> 30
	t.outputIfIndex = t.outputIfIndex & 0x3FFFFFFF

	switch t.sourceIDIndex {
	case t.outputIfIndex:
		t.sampleDirection = "egress"
	case t.inputIfIndex:
		t.sampleDirection = "ingress"
	}

	t.flowRecords, err = d.decodeFlowRecords(r, t.samplingRate)
	return t, err
}

func (d *packetDecoder) decodeFlowSampleExpanded(r io.Reader) (t sampleDataFlowSampleExpanded, err error) {
	if err := read(r, &t.sequenceNumber, "SequenceNumber"); err != nil { // sflow_version_5.txt line 1701
		return t, err
	}
	if err := read(r, &t.sourceIDType, "SourceIDType"); err != nil { // sflow_version_5.txt line: 1706 + 16878
		return t, err
	}
	if err := read(r, &t.sourceIDIndex, "SourceIDIndex"); err != nil { // sflow_version_5.txt line: 1689
		return t, err
	}
	if err := read(r, &t.samplingRate, "SamplingRate"); err != nil { // sflow_version_5.txt line: 1707
		return t, err
	}
	if err := read(r, &t.samplePool, "SamplePool"); err != nil { // sflow_version_5.txt line: 1708
		return t, err
	}
	if err := read(r, &t.drops, "Drops"); err != nil { // sflow_version_5.txt line: 1712
		return t, err
	}
	if err := read(r, &t.inputIfFormat, "InputIfFormat"); err != nil { // sflow_version_5.txt line: 1727
		return t, err
	}
	if err := read(r, &t.inputIfIndex, "InputIfIndex"); err != nil {
		return t, err
	}
	if err := read(r, &t.outputIfFormat, "OutputIfFormat"); err != nil { // sflow_version_5.txt line: 1728
		return t, err
	}
	if err := read(r, &t.outputIfIndex, "OutputIfIndex"); err != nil {
		return t, err
	}

	switch t.sourceIDIndex {
	case t.outputIfIndex:
		t.sampleDirection = "egress"
	case t.inputIfIndex:
		t.sampleDirection = "ingress"
	}

	t.flowRecords, err = d.decodeFlowRecords(r, t.samplingRate)
	return t, err
}

func (d *packetDecoder) decodeFlowRecords(r io.Reader, samplingRate uint32) (recs []flowRecord, err error) {
	var flowDataLen uint32
	var count uint32
	if err := read(r, &count, "FlowRecord count"); err != nil {
		return recs, err
	}
	for i := uint32(0); i < count; i++ {
		fr := flowRecord{}
		if err := read(r, &fr.flowFormat, "FlowFormat"); err != nil { // sflow_version_5.txt line 1597
			return recs, err
		}
		if err := read(r, &flowDataLen, "Flow data length"); err != nil {
			return recs, err
		}

		mr := binaryio.MinReader(r, int64(flowDataLen))

		switch fr.flowFormat {
		case flowFormatTypeRawPacketHeader: // sflow_version_5.txt line 1938
			fr.flowData, err = d.decodeRawPacketHeaderFlowData(mr, samplingRate)
		default:
			d.debug("Unknown flow format: ", fr.flowFormat)
		}
		if err != nil {
			mr.Close()
			return recs, err
		}

		recs = append(recs, fr)
		mr.Close()
	}

	return recs, err
}

func (d *packetDecoder) decodeRawPacketHeaderFlowData(r io.Reader, samplingRate uint32) (h rawPacketHeaderFlowData, err error) {
	if err := read(r, &h.headerProtocol, "HeaderProtocol"); err != nil { // sflow_version_5.txt line 1940
		return h, err
	}
	if err := read(r, &h.frameLength, "FrameLength"); err != nil { // sflow_version_5.txt line 1942
		return h, err
	}
	h.bytes = h.frameLength * samplingRate

	if err := read(r, &h.strippedOctets, "StrippedOctets"); err != nil { // sflow_version_5.txt line 1967
		return h, err
	}
	if err := read(r, &h.headerLength, "HeaderLength"); err != nil {
		return h, err
	}

	mr := binaryio.MinReader(r, int64(h.headerLength))
	defer mr.Close()

	switch h.headerProtocol {
	case headerProtocolTypeEthernetISO88023:
		h.header, err = d.decodeEthHeader(mr)
	default:
		d.debug("Unknown header protocol type: ", h.headerProtocol)
	}

	return h, err
}

// ethHeader answers a decode Directive that will decode an ethernet frame header
// according to https://en.wikipedia.org/wiki/Ethernet_frame
func (d *packetDecoder) decodeEthHeader(r io.Reader) (h ethHeader, err error) {
	// we may have to read out StrippedOctets bytes and throw them away first?
	if err := read(r, &h.destinationMAC, "DestinationMAC"); err != nil {
		return h, err
	}
	if err := read(r, &h.sourceMAC, "SourceMAC"); err != nil {
		return h, err
	}
	var tagOrEType uint16
	if err := read(r, &tagOrEType, "tagOrEtype"); err != nil {
		return h, err
	}
	switch tagOrEType {
	case 0x8100: // could be?
		var discard uint16
		if err := read(r, &discard, "unknown"); err != nil {
			return h, err
		}
		if err := read(r, &h.etherTypeCode, "EtherTypeCode"); err != nil {
			return h, err
		}
	default:
		h.etherTypeCode = tagOrEType
	}
	h.etherType = eTypeMap[h.etherTypeCode]
	switch h.etherType {
	case "IPv4":
		h.ipHeader, err = d.decodeIPv4Header(r)
	case "IPv6":
		h.ipHeader, err = d.decodeIPv6Header(r)
	default:
	}
	if err != nil {
		return h, err
	}
	return h, err
}

// https://en.wikipedia.org/wiki/IPv4#Header
func (d *packetDecoder) decodeIPv4Header(r io.Reader) (h ipV4Header, err error) {
	if err := read(r, &h.version, "Version"); err != nil {
		return h, err
	}
	h.internetHeaderLength = h.version & 0x0F
	h.version = h.version & 0xF0
	if err := read(r, &h.dscp, "DSCP"); err != nil {
		return h, err
	}
	h.ecn = h.dscp & 0x03
	h.dscp = h.dscp >> 2
	if err := read(r, &h.totalLength, "TotalLength"); err != nil {
		return h, err
	}
	if err := read(r, &h.identification, "Identification"); err != nil {
		return h, err
	}
	if err := read(r, &h.fragmentOffset, "FragmentOffset"); err != nil {
		return h, err
	}
	h.flags = uint8(h.fragmentOffset >> 13)
	h.fragmentOffset = h.fragmentOffset & 0x1FFF
	if err := read(r, &h.ttl, "TTL"); err != nil {
		return h, err
	}
	if err := read(r, &h.protocol, "Protocol"); err != nil {
		return h, err
	}
	if err := read(r, &h.headerChecksum, "HeaderChecksum"); err != nil {
		return h, err
	}
	if err := read(r, &h.sourceIP, "SourceIP"); err != nil {
		return h, err
	}
	if err := read(r, &h.destIP, "DestIP"); err != nil {
		return h, err
	}
	switch h.protocol {
	case ipProtocolTCP:
		h.protocolHeader, err = decodeTCPHeader(r)
	case ipProtocolUDP:
		h.protocolHeader, err = decodeUDPHeader(r)
	default:
		d.debug("Unknown IP protocol: ", h.protocol)
	}
	return h, err
}

// https://en.wikipedia.org/wiki/IPv6_packet
func (d *packetDecoder) decodeIPv6Header(r io.Reader) (h ipV6Header, err error) {
	var fourByteBlock uint32
	if err := read(r, &fourByteBlock, "IPv6 header octet 0"); err != nil {
		return h, err
	}
	version := fourByteBlock >> 28
	if version != 0x6 {
		return h, fmt.Errorf("unexpected IPv6 header version 0x%x", version)
	}
	h.dscp = uint8((fourByteBlock & 0xFC00000) >> 22)
	h.ecn = uint8((fourByteBlock & 0x300000) >> 20)

	// The flowLabel is available via fourByteBlock & 0xFFFFF
	if err := read(r, &h.payloadLength, "PayloadLength"); err != nil {
		return h, err
	}
	if err := read(r, &h.nextHeaderProto, "NextHeaderProto"); err != nil {
		return h, err
	}
	if err := read(r, &h.hopLimit, "HopLimit"); err != nil {
		return h, err
	}
	if err := read(r, &h.sourceIP, "SourceIP"); err != nil {
		return h, err
	}
	if err := read(r, &h.destIP, "DestIP"); err != nil {
		return h, err
	}
	switch h.nextHeaderProto {
	case ipProtocolTCP:
		h.protocolHeader, err = decodeTCPHeader(r)
	case ipProtocolUDP:
		h.protocolHeader, err = decodeUDPHeader(r)
	default:
		// not handled
		d.debug("Unknown IP protocol: ", h.nextHeaderProto)
	}
	return h, err
}

// https://en.wikipedia.org/wiki/Transmission_Control_Protocol#TCP_segment_structure
func decodeTCPHeader(r io.Reader) (h tcpHeader, err error) {
	if err := read(r, &h.sourcePort, "SourcePort"); err != nil {
		return h, err
	}
	if err := read(r, &h.destinationPort, "DestinationPort"); err != nil {
		return h, err
	}
	if err := read(r, &h.sequence, "Sequence"); err != nil {
		return h, err
	}
	if err := read(r, &h.ackNumber, "AckNumber"); err != nil {
		return h, err
	}
	// Next up: bit reading!
	// 	 data offset 4 bits
	// 	 reserved 3 bits
	// 	 flags 9 bits
	var dataOffsetAndReservedAndFlags uint16
	if err := read(r, &dataOffsetAndReservedAndFlags, "TCP Header Octet offset 12"); err != nil {
		return h, err
	}
	h.tcpHeaderLength = uint8((dataOffsetAndReservedAndFlags >> 12) * 4)
	h.flags = dataOffsetAndReservedAndFlags & 0x1FF
	// done bit reading

	if err := read(r, &h.tcpWindowSize, "TCPWindowSize"); err != nil {
		return h, err
	}
	if err := read(r, &h.checksum, "Checksum"); err != nil {
		return h, err
	}
	if err := read(r, &h.tcpUrgentPointer, "TCPUrgentPointer"); err != nil {
		return h, err
	}

	return h, err
}

func decodeUDPHeader(r io.Reader) (h udpHeader, err error) {
	if err := read(r, &h.sourcePort, "SourcePort"); err != nil {
		return h, err
	}
	if err := read(r, &h.destinationPort, "DestinationPort"); err != nil {
		return h, err
	}
	if err := read(r, &h.udpLength, "UDPLength"); err != nil {
		return h, err
	}
	if err := read(r, &h.checksum, "Checksum"); err != nil {
		return h, err
	}
	return h, err
}

func read(r io.Reader, data interface{}, name string) error {
	err := binary.Read(r, binary.BigEndian, data)
	if err != nil {
		return fmt.Errorf("failed to read %q: %w", name, err)
	}
	return nil
}
