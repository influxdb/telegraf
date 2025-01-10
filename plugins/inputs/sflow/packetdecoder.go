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
	onPacket func(p *v5Format)
	Log      telegraf.Logger
}

func newDecoder() *packetDecoder {
	return &packetDecoder{}
}

func (d *packetDecoder) debug(args ...interface{}) {
	if d.Log != nil {
		d.Log.Debug(args...)
	}
}

func (d *packetDecoder) OnPacket(f func(p *v5Format)) {
	d.onPacket = f
}

func (d *packetDecoder) Decode(r io.Reader) error {
	var err error
	var packet *v5Format
	for err == nil {
		packet, err = d.DecodeOnePacket(r)
		if err != nil {
			break
		}
		d.onPacket(packet)
	}
	if err != nil && errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

type AddressType uint32 // must be uint32

const (
	AddressTypeUnknown AddressType = 0
	AddressTypeIPV4    AddressType = 1
	AddressTypeIPV6    AddressType = 2
)

func (d *packetDecoder) DecodeOnePacket(r io.Reader) (*v5Format, error) {
	p := &v5Format{}
	err := read(r, &p.Version, "version")
	if err != nil {
		return nil, err
	}
	if p.Version != 5 {
		return nil, fmt.Errorf("version %d not supported, only version 5", p.Version)
	}
	var addressIPType AddressType
	if err := read(r, &addressIPType, "address ip type"); err != nil {
		return nil, err
	}
	switch addressIPType {
	case AddressTypeUnknown:
		p.AgentAddress.IP = make([]byte, 0)
	case AddressTypeIPV4:
		p.AgentAddress.IP = make([]byte, 4)
	case AddressTypeIPV6:
		p.AgentAddress.IP = make([]byte, 16)
	default:
		return nil, fmt.Errorf("unknown address IP type %d", addressIPType)
	}
	if err := read(r, &p.AgentAddress.IP, "Agent Address IP"); err != nil {
		return nil, err
	}
	if err := read(r, &p.SubAgentID, "SubAgentID"); err != nil {
		return nil, err
	}
	if err := read(r, &p.SequenceNumber, "SequenceNumber"); err != nil {
		return nil, err
	}
	if err := read(r, &p.Uptime, "Uptime"); err != nil {
		return nil, err
	}

	p.Samples, err = d.decodeSamples(r)
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
	if err := read(r, &sam.SampleType, "sampleType"); err != nil {
		return sam, err
	}
	sampleDataLen := uint32(0)
	if err := read(r, &sampleDataLen, "Sample data length"); err != nil {
		return sam, err
	}
	mr := binaryio.MinReader(r, int64(sampleDataLen))
	defer mr.Close()

	switch sam.SampleType {
	case sampleTypeFlowSample:
		sam.SampleData, err = d.decodeFlowSample(mr)
	case sampleTypeFlowSampleExpanded:
		sam.SampleData, err = d.decodeFlowSampleExpanded(mr)
	default:
		d.debug("Unknown sample type: ", sam.SampleType)
	}
	return sam, err
}

type InterfaceFormatType uint8 // sflow_version_5.txt line 1497
const (
	InterfaceFormatTypeSingleInterface InterfaceFormatType = 0
	InterfaceFormatTypePacketDiscarded InterfaceFormatType = 1
)

func (d *packetDecoder) decodeFlowSample(r io.Reader) (t sampleDataFlowSampleExpanded, err error) {
	if err := read(r, &t.SequenceNumber, "SequenceNumber"); err != nil {
		return t, err
	}
	var sourceID uint32
	if err := read(r, &sourceID, "SourceID"); err != nil { // source_id sflow_version_5.txt line: 1622
		return t, err
	}
	// split source id to source id type and source id index
	t.SourceIDIndex = sourceID & 0x00ffffff // sflow_version_5.txt line: 1468
	t.SourceIDType = sourceID >> 24         // source_id_type sflow_version_5.txt Line 1465
	if err := read(r, &t.SamplingRate, "SamplingRate"); err != nil {
		return t, err
	}
	if err := read(r, &t.SamplePool, "SamplePool"); err != nil {
		return t, err
	}
	if err := read(r, &t.Drops, "Drops"); err != nil { // sflow_version_5.txt line 1636
		return t, err
	}
	if err := read(r, &t.InputIfIndex, "InputIfIndex"); err != nil {
		return t, err
	}
	t.InputIfFormat = t.InputIfIndex >> 30
	t.InputIfIndex = t.InputIfIndex & 0x3FFFFFFF

	if err := read(r, &t.OutputIfIndex, "OutputIfIndex"); err != nil {
		return t, err
	}
	t.OutputIfFormat = t.OutputIfIndex >> 30
	t.OutputIfIndex = t.OutputIfIndex & 0x3FFFFFFF

	switch t.SourceIDIndex {
	case t.OutputIfIndex:
		t.SampleDirection = "egress"
	case t.InputIfIndex:
		t.SampleDirection = "ingress"
	}

	t.FlowRecords, err = d.decodeFlowRecords(r, t.SamplingRate)
	return t, err
}

func (d *packetDecoder) decodeFlowSampleExpanded(r io.Reader) (t sampleDataFlowSampleExpanded, err error) {
	if err := read(r, &t.SequenceNumber, "SequenceNumber"); err != nil { // sflow_version_5.txt line 1701
		return t, err
	}
	if err := read(r, &t.SourceIDType, "SourceIDType"); err != nil { // sflow_version_5.txt line: 1706 + 16878
		return t, err
	}
	if err := read(r, &t.SourceIDIndex, "SourceIDIndex"); err != nil { // sflow_version_5.txt line: 1689
		return t, err
	}
	if err := read(r, &t.SamplingRate, "SamplingRate"); err != nil { // sflow_version_5.txt line: 1707
		return t, err
	}
	if err := read(r, &t.SamplePool, "SamplePool"); err != nil { // sflow_version_5.txt line: 1708
		return t, err
	}
	if err := read(r, &t.Drops, "Drops"); err != nil { // sflow_version_5.txt line: 1712
		return t, err
	}
	if err := read(r, &t.InputIfFormat, "InputIfFormat"); err != nil { // sflow_version_5.txt line: 1727
		return t, err
	}
	if err := read(r, &t.InputIfIndex, "InputIfIndex"); err != nil {
		return t, err
	}
	if err := read(r, &t.OutputIfFormat, "OutputIfFormat"); err != nil { // sflow_version_5.txt line: 1728
		return t, err
	}
	if err := read(r, &t.OutputIfIndex, "OutputIfIndex"); err != nil {
		return t, err
	}

	switch t.SourceIDIndex {
	case t.OutputIfIndex:
		t.SampleDirection = "egress"
	case t.InputIfIndex:
		t.SampleDirection = "ingress"
	}

	t.FlowRecords, err = d.decodeFlowRecords(r, t.SamplingRate)
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
		if err := read(r, &fr.FlowFormat, "FlowFormat"); err != nil { // sflow_version_5.txt line 1597
			return recs, err
		}
		if err := read(r, &flowDataLen, "Flow data length"); err != nil {
			return recs, err
		}

		mr := binaryio.MinReader(r, int64(flowDataLen))

		switch fr.FlowFormat {
		case flowFormatTypeRawPacketHeader: // sflow_version_5.txt line 1938
			fr.FlowData, err = d.decodeRawPacketHeaderFlowData(mr, samplingRate)
		default:
			d.debug("Unknown flow format: ", fr.FlowFormat)
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
	if err := read(r, &h.HeaderProtocol, "HeaderProtocol"); err != nil { // sflow_version_5.txt line 1940
		return h, err
	}
	if err := read(r, &h.FrameLength, "FrameLength"); err != nil { // sflow_version_5.txt line 1942
		return h, err
	}
	h.Bytes = h.FrameLength * samplingRate

	if err := read(r, &h.StrippedOctets, "StrippedOctets"); err != nil { // sflow_version_5.txt line 1967
		return h, err
	}
	if err := read(r, &h.HeaderLength, "HeaderLength"); err != nil {
		return h, err
	}

	mr := binaryio.MinReader(r, int64(h.HeaderLength))
	defer mr.Close()

	switch h.HeaderProtocol {
	case headerProtocolTypeEthernetISO88023:
		h.Header, err = d.decodeEthHeader(mr)
	default:
		d.debug("Unknown header protocol type: ", h.HeaderProtocol)
	}

	return h, err
}

// ethHeader answers a decode Directive that will decode an ethernet frame header
// according to https://en.wikipedia.org/wiki/Ethernet_frame
func (d *packetDecoder) decodeEthHeader(r io.Reader) (h ethHeader, err error) {
	// we may have to read out StrippedOctets bytes and throw them away first?
	if err := read(r, &h.DestinationMAC, "DestinationMAC"); err != nil {
		return h, err
	}
	if err := read(r, &h.SourceMAC, "SourceMAC"); err != nil {
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
		if err := read(r, &h.EtherTypeCode, "EtherTypeCode"); err != nil {
			return h, err
		}
	default:
		h.EtherTypeCode = tagOrEType
	}
	h.EtherType = eTypeMap[h.EtherTypeCode]
	switch h.EtherType {
	case "IPv4":
		h.IPHeader, err = d.decodeIPv4Header(r)
	case "IPv6":
		h.IPHeader, err = d.decodeIPv6Header(r)
	default:
	}
	if err != nil {
		return h, err
	}
	return h, err
}

// https://en.wikipedia.org/wiki/IPv4#Header
func (d *packetDecoder) decodeIPv4Header(r io.Reader) (h ipV4Header, err error) {
	if err := read(r, &h.Version, "Version"); err != nil {
		return h, err
	}
	h.InternetHeaderLength = h.Version & 0x0F
	h.Version = h.Version & 0xF0
	if err := read(r, &h.DSCP, "DSCP"); err != nil {
		return h, err
	}
	h.ECN = h.DSCP & 0x03
	h.DSCP = h.DSCP >> 2
	if err := read(r, &h.TotalLength, "TotalLength"); err != nil {
		return h, err
	}
	if err := read(r, &h.Identification, "Identification"); err != nil {
		return h, err
	}
	if err := read(r, &h.FragmentOffset, "FragmentOffset"); err != nil {
		return h, err
	}
	h.Flags = uint8(h.FragmentOffset >> 13)
	h.FragmentOffset = h.FragmentOffset & 0x1FFF
	if err := read(r, &h.TTL, "TTL"); err != nil {
		return h, err
	}
	if err := read(r, &h.Protocol, "Protocol"); err != nil {
		return h, err
	}
	if err := read(r, &h.HeaderChecksum, "HeaderChecksum"); err != nil {
		return h, err
	}
	if err := read(r, &h.SourceIP, "SourceIP"); err != nil {
		return h, err
	}
	if err := read(r, &h.DestIP, "DestIP"); err != nil {
		return h, err
	}
	switch h.Protocol {
	case ipProtocolTCP:
		h.ProtocolHeader, err = decodeTCPHeader(r)
	case ipProtocolUDP:
		h.ProtocolHeader, err = decodeUDPHeader(r)
	default:
		d.debug("Unknown IP protocol: ", h.Protocol)
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
	h.DSCP = uint8((fourByteBlock & 0xFC00000) >> 22)
	h.ECN = uint8((fourByteBlock & 0x300000) >> 20)

	// The flowLabel is available via fourByteBlock & 0xFFFFF
	if err := read(r, &h.PayloadLength, "PayloadLength"); err != nil {
		return h, err
	}
	if err := read(r, &h.NextHeaderProto, "NextHeaderProto"); err != nil {
		return h, err
	}
	if err := read(r, &h.HopLimit, "HopLimit"); err != nil {
		return h, err
	}
	if err := read(r, &h.SourceIP, "SourceIP"); err != nil {
		return h, err
	}
	if err := read(r, &h.DestIP, "DestIP"); err != nil {
		return h, err
	}
	switch h.NextHeaderProto {
	case ipProtocolTCP:
		h.ProtocolHeader, err = decodeTCPHeader(r)
	case ipProtocolUDP:
		h.ProtocolHeader, err = decodeUDPHeader(r)
	default:
		// not handled
		d.debug("Unknown IP protocol: ", h.NextHeaderProto)
	}
	return h, err
}

// https://en.wikipedia.org/wiki/Transmission_Control_Protocol#TCP_segment_structure
func decodeTCPHeader(r io.Reader) (h tcpHeader, err error) {
	if err := read(r, &h.SourcePort, "SourcePort"); err != nil {
		return h, err
	}
	if err := read(r, &h.DestinationPort, "DestinationPort"); err != nil {
		return h, err
	}
	if err := read(r, &h.Sequence, "Sequence"); err != nil {
		return h, err
	}
	if err := read(r, &h.AckNumber, "AckNumber"); err != nil {
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
	h.TCPHeaderLength = uint8((dataOffsetAndReservedAndFlags >> 12) * 4)
	h.Flags = dataOffsetAndReservedAndFlags & 0x1FF
	// done bit reading

	if err := read(r, &h.TCPWindowSize, "TCPWindowSize"); err != nil {
		return h, err
	}
	if err := read(r, &h.Checksum, "Checksum"); err != nil {
		return h, err
	}
	if err := read(r, &h.TCPUrgentPointer, "TCPUrgentPointer"); err != nil {
		return h, err
	}

	return h, err
}

func decodeUDPHeader(r io.Reader) (h udpHeader, err error) {
	if err := read(r, &h.SourcePort, "SourcePort"); err != nil {
		return h, err
	}
	if err := read(r, &h.DestinationPort, "DestinationPort"); err != nil {
		return h, err
	}
	if err := read(r, &h.UDPLength, "UDPLength"); err != nil {
		return h, err
	}
	if err := read(r, &h.Checksum, "Checksum"); err != nil {
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
