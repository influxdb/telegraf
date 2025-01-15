package netflow

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/netsampler/goflow2/v2/decoders/sflow"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

// Decoder structure
type sflowv5Decoder struct {
	log telegraf.Logger

	warnedCounterRaw map[uint32]bool
	warnedFlowRaw    map[int64]bool
}

func (d *sflowv5Decoder) init() error {
	if err := initL4ProtoMapping(); err != nil {
		return fmt.Errorf("initializing layer 4 protocol mapping failed: %w", err)
	}
	d.warnedCounterRaw = make(map[uint32]bool)
	d.warnedFlowRaw = make(map[int64]bool)

	return nil
}

func (d *sflowv5Decoder) decode(srcIP net.IP, payload []byte) ([]telegraf.Metric, error) {
	t := time.Now()
	src := srcIP.String()

	// Decode the message
	var msg sflow.Packet
	buf := bytes.NewBuffer(payload)
	if err := sflow.DecodeMessageVersion(buf, &msg); err != nil {
		return nil, err
	}

	// Extract metrics
	metrics := make([]telegraf.Metric, 0, len(msg.Samples))
	for _, s := range msg.Samples {
		tags := map[string]string{
			"source":  src,
			"version": "sFlowV5",
		}

		switch sample := s.(type) {
		case sflow.FlowSample:
			fields := map[string]interface{}{
				"ip_version":        decodeSflowIPVersion(msg.IPVersion),
				"sys_uptime":        msg.Uptime,
				"agent_subid":       msg.SubAgentId,
				"seq_number":        sample.Header.SampleSequenceNumber,
				"sampling_interval": sample.SamplingRate,
				"in_total_packets":  sample.SamplePool,
				"sampling_drops":    sample.Drops,
				"in_snmp":           sample.Input,
			}

			var err error
			fields["agent_ip"], err = decodeIP(msg.AgentIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'agent_ip' failed: %w", err)
			}

			if sample.Output>>31 == 0 {
				fields["out_snmp"] = sample.Output & 0x7fffffff
			}
			// Decode the source information
			if name := decodeSflowSourceInterface(sample.Header.SourceIdType); name != "" {
				fields[name] = sample.Header.SourceIdValue
			}
			// Decode the sampling direction
			if sample.Header.SourceIdValue == sample.Input {
				fields["direction"] = "ingress"
			} else {
				fields["direction"] = "egress"
			}
			recordFields, err := d.decodeFlowRecords(sample.Records)
			if err != nil {
				return nil, err
			}
			for k, v := range recordFields {
				fields[k] = v
			}
			metrics = append(metrics, metric.New("netflow", tags, fields, t))
		case sflow.ExpandedFlowSample:
			fields := map[string]interface{}{
				"ip_version":        decodeSflowIPVersion(msg.IPVersion),
				"sys_uptime":        msg.Uptime,
				"agent_subid":       msg.SubAgentId,
				"seq_number":        sample.Header.SampleSequenceNumber,
				"sampling_interval": sample.SamplingRate,
				"in_total_packets":  sample.SamplePool,
				"sampling_drops":    sample.Drops,
				"in_snmp":           sample.InputIfValue,
			}

			var err error
			fields["agent_ip"], err = decodeIP(msg.AgentIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'agent_ip' failed: %w", err)
			}

			if sample.OutputIfFormat == 0 {
				fields["out_snmp"] = sample.OutputIfValue
			}
			// Decode the source information
			if name := decodeSflowSourceInterface(sample.Header.SourceIdType); name != "" {
				fields[name] = sample.Header.SourceIdValue
			}
			// Decode the sampling direction
			if sample.Header.SourceIdValue == sample.InputIfValue {
				fields["direction"] = "ingress"
			} else {
				fields["direction"] = "egress"
			}
			recordFields, err := d.decodeFlowRecords(sample.Records)
			if err != nil {
				return nil, err
			}
			for k, v := range recordFields {
				fields[k] = v
			}
			metrics = append(metrics, metric.New("netflow", tags, fields, t))
		case sflow.CounterSample:
			fields := map[string]interface{}{
				"ip_version":  decodeSflowIPVersion(msg.IPVersion),
				"sys_uptime":  msg.Uptime,
				"agent_subid": msg.SubAgentId,
				"seq_number":  sample.Header.SampleSequenceNumber,
			}

			var err error
			fields["agent_ip"], err = decodeIP(msg.AgentIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'agent_ip' failed: %w", err)
			}

			// Decode the source information
			if name := decodeSflowSourceInterface(sample.Header.SourceIdType); name != "" {
				fields[name] = sample.Header.SourceIdValue
			}
			recordFields, err := d.decodeCounterRecords(sample.Records)
			if err != nil {
				return nil, err
			}
			for k, v := range recordFields {
				fields[k] = v
			}
			metrics = append(metrics, metric.New("netflow", tags, fields, t))
		case sflow.DropSample:
			fields := map[string]interface{}{
				"ip_version":     decodeSflowIPVersion(msg.IPVersion),
				"sys_uptime":     msg.Uptime,
				"agent_subid":    msg.SubAgentId,
				"seq_number":     sample.Header.SampleSequenceNumber,
				"sampling_drops": sample.Drops,
				"in_snmp":        sample.Input,
				"out_snmp":       sample.Output,
				"reason":         sample.Reason,
			}

			var err error
			fields["agent_ip"], err = decodeIP(msg.AgentIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'agent_ip' failed: %w", err)
			}

			// Decode the source information
			if name := decodeSflowSourceInterface(sample.Header.SourceIdType); name != "" {
				fields[name] = sample.Header.SourceIdValue
			}
			// Decode the sampling direction
			if sample.Header.SourceIdValue == sample.Input {
				fields["direction"] = "ingress"
			} else {
				fields["direction"] = "egress"
			}
			recordFields, err := d.decodeFlowRecords(sample.Records)
			if err != nil {
				return nil, err
			}
			for k, v := range recordFields {
				fields[k] = v
			}
			metrics = append(metrics, metric.New("netflow", tags, fields, t))

		default:
			return nil, fmt.Errorf("unknown record type %T", s)
		}
	}

	return metrics, nil
}

func (d *sflowv5Decoder) decodeFlowRecords(records []sflow.FlowRecord) (map[string]interface{}, error) {
	fields := make(map[string]interface{})
	for _, r := range records {
		if r.Data == nil {
			continue
		}
		switch record := r.Data.(type) {
		case sflow.SampledHeader:
			fields["l2_protocol"] = decodeSflowHeaderProtocol(record.Protocol)
			fields["l2_bytes"] = record.FrameLength
			pktfields, err := d.decodeRawHeaderSample(&record)
			if err != nil {
				return nil, err
			}
			for k, v := range pktfields {
				fields[k] = v
			}
		case sflow.SampledEthernet:
			var err error
			fields["eth_total_len"] = record.Length
			fields["in_src_mac"], err = decodeMAC(record.SrcMac)
			if err != nil {
				return nil, fmt.Errorf("decoding 'in_src_mac' failed: %w", err)
			}
			fields["out_dst_mac"], err = decodeMAC(record.DstMac)
			if err != nil {
				return nil, fmt.Errorf("decoding 'out_dst_mac' failed: %w", err)
			}
			fields["datalink_frame_type"] = layers.EthernetType(record.EthType & 0x0000ffff).String()
		case sflow.SampledIPv4:
			var err error
			fields["ipv4_total_len"] = record.Length
			fields["protocol"] = mapL4Proto(uint8(record.Protocol & 0x000000ff))
			fields["src"], err = decodeIP(record.SrcIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'src' failed: %w", err)
			}
			fields["dst"], err = decodeIP(record.DstIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'dst' failed: %w", err)
			}
			fields["src_port"] = record.SrcPort
			fields["dst_port"] = record.DstPort
			fields["src_tos"] = record.Tos
			fields["tcp_flags"], err = decodeTCPFlags([]byte{byte(record.TcpFlags & 0x000000ff)})
			if err != nil {
				return nil, fmt.Errorf("decoding 'tcp_flags' failed: %w", err)
			}
		case sflow.SampledIPv6:
			var err error
			fields["ipv6_total_len"] = record.Length
			fields["protocol"] = mapL4Proto(uint8(record.Protocol & 0x000000ff))
			fields["src"], err = decodeIP(record.SrcIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'src' failed: %w", err)
			}
			fields["dst"], err = decodeIP(record.DstIP)
			if err != nil {
				return nil, fmt.Errorf("decoding 'dst' failed: %w", err)
			}
			fields["src_port"] = record.SrcPort
			fields["dst_port"] = record.DstPort
			fields["tcp_flags"], err = decodeTCPFlags([]byte{byte(record.TcpFlags & 0x000000ff)})
			if err != nil {
				return nil, fmt.Errorf("decoding 'tcp_flags' failed: %w", err)
			}
		case sflow.ExtendedSwitch:
			fields["vlan_src"] = record.SrcVlan
			fields["vlan_src_priority"] = record.SrcPriority
			fields["vlan_dst"] = record.DstVlan
			fields["vlan_dst_priority"] = record.DstPriority
		case sflow.ExtendedRouter:
			var err error
			fields["next_hop"], err = decodeIP(record.NextHop)
			if err != nil {
				return nil, fmt.Errorf("decoding 'next_hop' failed: %w", err)
			}
			fields["src_mask"] = record.SrcMaskLen
			fields["dst_mask"] = record.DstMaskLen
		case sflow.ExtendedGateway:
			var err error
			fields["next_hop_ip_version"] = record.NextHopIPVersion
			fields["next_hop"], err = decodeIP(record.NextHop)
			if err != nil {
				return nil, fmt.Errorf("decoding 'next_hop' failed: %w", err)
			}
			fields["bgp_as"] = record.AS
			fields["bgp_src_as"] = record.SrcAS
			fields["bgp_dst_as"] = record.ASDestinations
			fields["bgp_as_path_type"] = record.ASPathType
			fields["bgp_as_path_length"] = record.ASPathLength
			fields["bgp_next_hop"], err = decodeIP(record.NextHop)
			if err != nil {
				return nil, fmt.Errorf("decoding 'bgp_next_hop' failed: %w", err)
			}
			fields["bgp_prev_as"] = record.SrcPeerAS
			if len(record.ASPath) > 0 {
				fields["bgp_next_as"] = record.ASPath[0]
			}
			fields["community_length"] = record.CommunitiesLength
			parts := make([]string, 0, len(record.Communities))
			for _, c := range record.Communities {
				parts = append(parts, "0x"+strconv.FormatUint(uint64(c), 16))
			}
			fields["communities"] = strings.Join(parts, ",")
			fields["local_pref"] = record.LocalPref
		case sflow.EgressQueue:
			fields["out_queue"] = record.Queue
		case sflow.ExtendedACL:
			fields["acl_id"] = record.Number
			fields["acl_name"] = record.Name
			switch record.Direction {
			case 1:
				fields["direction"] = "ingress"
			case 2:
				fields["direction"] = "egress"
			default:
				fields["direction"] = "unknown"
			}
		case sflow.ExtendedFunction:
			fields["function"] = record.Symbol
		default:
			return nil, fmt.Errorf("unhandled flow record type %T", r.Data)
		}
	}
	return fields, nil
}

func (d *sflowv5Decoder) decodeRawHeaderSample(record *sflow.SampledHeader) (map[string]interface{}, error) {
	var packet gopacket.Packet
	switch record.Protocol {
	case 1: // ETHERNET-ISO8023
		packet = gopacket.NewPacket(record.HeaderData, layers.LayerTypeEthernet, gopacket.Default)
	case 2: // ISO88024-TOKENBUS
		fallthrough
	case 3: // ISO88025-TOKENRING
		fallthrough
	case 4: // FDDI
		fallthrough
	case 5: // FRAME-RELAY
		fallthrough
	case 6: // X25
		fallthrough
	case 7: // PPP
		fallthrough
	case 8: // SMDS
		fallthrough
	case 9: // AAL5
		fallthrough
	case 10: // AAL5-IP
		fallthrough
	case 11: // IPv4
		fallthrough
	case 12: // IPv6
		fallthrough
	case 13: // MPLS
		fallthrough
	default:
		return nil, fmt.Errorf("unhandled protocol %d", record.Protocol)
	}

	fields := make(map[string]interface{})
	for _, pkt := range packet.Layers() {
		switch l := pkt.(type) {
		case *layers.Ethernet:
			fields["in_src_mac"] = l.SrcMAC.String()
			fields["out_dst_mac"] = l.DstMAC.String()
			fields["datalink_frame_type"] = l.EthernetType.String()
			if l.Length > 0 {
				fields["eth_header_len"] = l.Length
			}
		case *layers.Dot1Q:
			fields["vlan_id"] = l.VLANIdentifier
			fields["vlan_priority"] = l.Priority
			fields["vlan_drop_eligible"] = l.DropEligible
		case *layers.IPv4:
			fields["ip_version"] = decodePacketIPVersion(l.Version)
			fields["ipv4_inet_header_len"] = l.IHL
			fields["src_tos"] = l.TOS
			fields["ipv4_total_len"] = l.Length
			fields["ipv4_id"] = l.Id // ?
			fields["ttl"] = l.TTL
			fields["protocol"] = mapL4Proto(uint8(l.Protocol))
			fields["src"] = l.SrcIP.String()
			fields["dst"] = l.DstIP.String()

			flags := []byte("........")
			if l.Flags&layers.IPv4EvilBit > 0 {
				flags[7] = byte('E')
			}
			if l.Flags&layers.IPv4DontFragment > 0 {
				flags[6] = byte('D')
			}
			if l.Flags&layers.IPv4MoreFragments > 0 {
				flags[5] = byte('M')
			}
			fields["fragment_flags"] = string(flags)
			fields["fragment_offset"] = l.FragOffset
			fields["ip_total_len"] = l.Length
		case *layers.IPv6:
			fields["ip_version"] = decodePacketIPVersion(l.Version)
			fields["ipv6_total_len"] = l.Length
			fields["ttl"] = l.HopLimit
			fields["protocol"] = mapL4Proto(uint8(l.NextHeader))
			fields["src"] = l.SrcIP.String()
			fields["dst"] = l.DstIP.String()
			fields["ip_total_len"] = l.Length
		case *layers.TCP:
			fields["src_port"] = uint16(l.SrcPort)
			fields["dst_port"] = uint16(l.DstPort)
			fields["tcp_seq_number"] = l.Seq
			fields["tcp_ack_number"] = l.Ack
			fields["tcp_window_size"] = l.Window
			fields["tcp_urgent_ptr"] = l.Urgent
			flags := []byte("........")
			if l.FIN {
				flags[7] = byte('F')
			}
			if l.SYN {
				flags[6] = byte('S')
			}
			if l.RST {
				flags[5] = byte('R')
			}
			if l.PSH {
				flags[4] = byte('P')
			}
			if l.ACK {
				flags[3] = byte('A')
			}
			if l.URG {
				flags[2] = byte('U')
			}
			if l.ECE {
				flags[1] = byte('E')
			}
			if l.CWR {
				flags[0] = byte('C')
			}
			fields["tcp_flags"] = string(flags)
		case *layers.UDP:
			fields["src_port"] = uint16(l.SrcPort)
			fields["dst_port"] = uint16(l.DstPort)
			fields["ip_total_len"] = l.Length
		case *gopacket.Payload:
			// Ignore the payload
		default:
			ltype := int64(pkt.LayerType())
			if !d.warnedFlowRaw[ltype] {
				contents := hex.EncodeToString(pkt.LayerContents())
				payload := hex.EncodeToString(pkt.LayerPayload())
				d.log.Warnf("Unknown flow raw flow message %s (%d):", pkt.LayerType().String(), pkt.LayerType())
				d.log.Warnf("  contents: %s", contents)
				d.log.Warnf("  payload:  %s", payload)

				d.log.Warn("This message is only printed once.")
			}
			d.warnedFlowRaw[ltype] = true
		}
	}
	return fields, nil
}

func (d *sflowv5Decoder) decodeCounterRecords(records []sflow.CounterRecord) (map[string]interface{}, error) {
	for _, r := range records {
		if r.Data == nil {
			continue
		}
		switch record := r.Data.(type) {
		case sflow.IfCounters:
			fields := map[string]interface{}{
				"interface":                   record.IfIndex,
				"interface_type":              record.IfType,
				"speed":                       record.IfSpeed,
				"in_bytes":                    record.IfInOctets,
				"in_unicast_packets_total":    record.IfInUcastPkts,
				"in_mcast_packets_total":      record.IfInMulticastPkts,
				"in_broadcast_packets_total":  record.IfInBroadcastPkts,
				"in_dropped_packets":          record.IfInDiscards,
				"in_errors":                   record.IfInErrors,
				"in_unknown_protocol":         record.IfInUnknownProtos,
				"out_bytes":                   record.IfOutOctets,
				"out_unicast_packets_total":   record.IfOutUcastPkts,
				"out_mcast_packets_total":     record.IfOutMulticastPkts,
				"out_broadcast_packets_total": record.IfOutBroadcastPkts,
				"out_dropped_packets":         record.IfOutDiscards,
				"out_errors":                  record.IfOutErrors,
				"promiscuous":                 record.IfPromiscuousMode,
			}
			if record.IfStatus == 0 {
				fields["status"] = "down"
			} else {
				fields["status"] = "up"
			}
			return fields, nil
		case sflow.EthernetCounters:
			fields := map[string]interface{}{
				"type":                    "IEEE 802.3",
				"collision_frames_single": record.Dot3StatsSingleCollisionFrames,
				"collision_frames_multi":  record.Dot3StatsMultipleCollisionFrames,
				"collisions_late":         record.Dot3StatsLateCollisions,
				"collisions_excessive":    record.Dot3StatsExcessiveCollisions,
				"deferred":                record.Dot3StatsDeferredTransmissions,
				"errors_alignment":        record.Dot3StatsAlignmentErrors,
				"errors_fcs":              record.Dot3StatsFCSErrors,
				"errors_sqetest":          record.Dot3StatsSQETestErrors,
				"errors_internal_mac_tx":  record.Dot3StatsInternalMacTransmitErrors,
				"errors_internal_mac_rx":  record.Dot3StatsInternalMacReceiveErrors,
				"errors_carrier_sense":    record.Dot3StatsCarrierSenseErrors,
				"errors_frame_too_long":   record.Dot3StatsFrameTooLongs,
				"errors_symbols":          record.Dot3StatsSymbolErrors,
			}
			return fields, nil
		case sflow.RawRecord:
			switch r.Header.DataFormat {
			case 1005:
				// Openflow port-name
				if len(record.Data) < 4 {
					return nil, fmt.Errorf("invalid data for raw counter %+v", r)
				}
				fields := map[string]interface{}{
					"port_name": string(record.Data[4:]),
				}
				return fields, nil
			default:
				if !d.warnedCounterRaw[r.Header.DataFormat] {
					data := hex.EncodeToString(record.Data)
					d.log.Warnf("Unknown counter raw flow message %d: %s", r.Header.DataFormat, data)
					d.log.Warn("This message is only printed once.")
				}
				d.warnedCounterRaw[r.Header.DataFormat] = true
			}
		default:
			return nil, fmt.Errorf("unhandled counter record type %T", r.Data)
		}
	}
	return nil, nil
}
