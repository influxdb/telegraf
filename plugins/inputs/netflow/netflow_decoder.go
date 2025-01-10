package netflow

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

var regexpIPFIXPENMapping = regexp.MustCompile(`\d+\.\d+`)

type decoderFunc func([]byte) (interface{}, error)

type fieldMapping struct {
	name    string
	decoder decoderFunc
}

// Default field mappings common for Netflow version 9 and IPFIX
// From documentations at
// - https://www.cisco.com/en/US/technologies/tk648/tk362/technologies_white_paper09186a00800a3db9.html
// - https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-information-elements
var fieldMappingsNetflowCommon = map[uint16][]fieldMapping{
	// 0: reserved
	1:  {{"in_bytes", decodeUint}},          // IN_BYTES / octetDeltaCount
	2:  {{"in_packets", decodeUint}},        // IN_PKTS / packetDeltaCount
	3:  {{"flows", decodeUint}},             // FLOWS / deltaFlowCount
	4:  {{"protocol", decodeL4Proto}},       // PROTOCOL / protocolIdentifier
	5:  {{"src_tos", decodeHex}},            // SRC_TOS / ipClassOfService
	6:  {{"tcp_flags", decodeTCPFlags}},     // TCP_FLAGS / tcpControlBits
	7:  {{"src_port", decodeUint}},          // L4_SRC_PORT / sourceTransportPort
	8:  {{"src", decodeIP}},                 // IPV4_SRC_ADDR / sourceIPv4Address
	9:  {{"src_mask", decodeUint}},          // SRC_MASK / sourceIPv4PrefixLength
	10: {{"in_snmp", decodeUint}},           // INPUT_SNMP / ingressInterface
	11: {{"dst_port", decodeUint}},          // L4_DST_PORT / destinationTransportPort
	12: {{"dst", decodeIP}},                 // IPV4_DST_ADDR / destinationIPv4Address
	13: {{"dst_mask", decodeUint}},          // DST_MASK / destinationIPv4PrefixLength
	14: {{"out_snmp", decodeUint}},          // OUTPUT_SNMP / egressInterface
	15: {{"next_hop", decodeIP}},            // IPV4_NEXT_HOP / ipNextHopIPv4Address
	16: {{"bgp_src_as", decodeUint}},        // SRC_AS / bgpSourceAsNumber
	17: {{"bgp_dst_as", decodeUint}},        // DST_AS / bgpDestinationAsNumber
	18: {{"bgp_next_hop", decodeIP}},        // BGP_IPV4_NEXT_HOP / bgpNextHopIPv4Address
	19: {{"out_mcast_packets", decodeUint}}, // MUL_DST_PKTS / postMCastPacketDeltaCount
	20: {{"out_mcast_bytes", decodeUint}},   // MUL_DST_BYTES / postMCastOctetDeltaCount
	21: {{"last_switched", decodeUint}},     // LAST_SWITCHED / flowEndSysUpTime
	22: {{"first_switched", decodeUint}},    // FIRST_SWITCHED / flowStartSysUpTime
	23: {{"out_bytes", decodeUint}},         // OUT_BYTES / postOctetDeltaCount
	24: {{"out_packets", decodeUint}},       // OUT_PKTS / postPacketDeltaCount
	25: {{"min_packet_len", decodeUint}},    // MIN_PKT_LNGTH / minimumIpTotalLength
	26: {{"max_packet_len", decodeUint}},    // MAX_PKT_LNGTH / maximumIpTotalLength
	27: {{"src", decodeIP}},                 // IPV6_SRC_ADDR / sourceIPv6Address
	28: {{"dst", decodeIP}},                 // IPV6_DST_ADDR / destinationIPv6Address
	29: {{"src_mask", decodeUint}},          // IPV6_SRC_MASK / sourceIPv6PrefixLength
	30: {{"dst_mask", decodeUint}},          // IPV6_DST_MASK / destinationIPv6PrefixLength
	31: {{"flow_label", decodeHex}},         // IPV6_FLOW_LABEL / flowLabelIPv6
	32: {
		{"icmp_type", decodeByteFunc(0)}, // ICMP_TYPE / icmpTypeCodeIPv4
		{"icmp_code", decodeByteFunc(1)},
	},
	33: {{"igmp_type", decodeUint}},               // MUL_IGMP_TYPE / igmpType
	34: {{"sampling_interval", decodeUint}},       // SAMPLING_INTERVAL / samplingInterval (deprecated)
	35: {{"sampling_algo", decodeSampleAlgo}},     // SAMPLING_ALGORITHM / samplingAlgorithm (deprecated)
	36: {{"flow_active_timeout", decodeUint}},     // FLOW_ACTIVE_TIMEOUT / flowActiveTimeout
	37: {{"flow_inactive_timeout", decodeUint}},   // FLOW_INACTIVE_TIMEOUT / flowIdleTimeout
	38: {{"engine_type", decodeEngineType}},       // ENGINE_TYPE / engineType (deprecated)
	39: {{"engine_id", decodeHex}},                // ENGINE_ID / engineId (deprecated)
	40: {{"total_bytes_exported", decodeUint}},    // TOTAL_BYTES_EXP / exportedOctetTotalCount
	41: {{"total_messages_exported", decodeUint}}, // TOTAL_PKTS_EXP / exportedMessageTotalCount
	42: {{"total_flows_exported", decodeUint}},    // TOTAL_FLOWS_EXP / exportedFlowRecordTotalCount
	// 43: vendor proprietary / deprecated
	44: {{"ipv4_src_prefix", decodeIP}},           // IPV4_SRC_PREFIX / sourceIPv4Prefix
	45: {{"ipv4_dst_prefix", decodeIP}},           // IPV4_DST_PREFIX / destinationIPv4Prefix
	46: {{"mpls_top_label_type", decodeMPLSType}}, // MPLS_TOP_LABEL_TYPE / mplsTopLabelType
	47: {{"mpls_top_label_ip", decodeIP}},         // MPLS_TOP_LABEL_IP_ADDR / mplsTopLabelIPv4Address
	48: {{"flow_sampler_id", decodeUint}},         // FLOW_SAMPLER_ID / samplerId (deprecated)
	49: {{"flow_sampler_mode", decodeSampleAlgo}}, // FLOW_SAMPLER_MODE / samplerMode (deprecated)
	50: {{"flow_sampler_interval", decodeUint}},   // FLOW_SAMPLER_RANDOM_INTERVAL / samplerRandomInterval (deprecated)
	// 51: vendor proprietary / deprecated
	52: {{"min_ttl", decodeUint}},         // MIN_TTL / minimumTTL
	53: {{"max_ttl", decodeUint}},         // MAX_TTL / maximumTTL
	54: {{"fragment_id", decodeHex}},      // IPV4_IDENT / fragmentIdentification
	55: {{"dst_tos", decodeHex}},          // DST_TOS / postIpClassOfService
	56: {{"in_src_mac", decodeMAC}},       // IN_SRC_MAC / sourceMacAddress
	57: {{"out_dst_mac", decodeMAC}},      // OUT_DST_MAC / postDestinationMacAddress
	58: {{"vlan_src", decodeUint}},        // SRC_VLAN / vlanId
	59: {{"vlan_dst", decodeUint}},        // DST_VLAN / postVlanId
	60: {{"ip_version", decodeIPVersion}}, // IP_PROTOCOL_VERSION / ipVersion
	61: {{"direction", decodeDirection}},  // DIRECTION / flowDirection
	62: {{"next_hop", decodeIP}},          // IPV6_NEXT_HOP / ipNextHopIPv6Address
	63: {{"bgp_next_hop", decodeIP}},      // BPG_IPV6_NEXT_HOP / bgpNextHopIPv6Address
	64: {{"ipv6_extensions", decodeHex}},  // IPV6_OPTION_HEADERS / ipv6ExtensionHeaders
	// 65 - 69: vendor proprietary
	70: {{"mpls_label_1", decodeHex}},      // MPLS_LABEL_1 / mplsTopLabelStackSection
	71: {{"mpls_label_2", decodeHex}},      // MPLS_LABEL_2 / mplsLabelStackSection2
	72: {{"mpls_label_3", decodeHex}},      // MPLS_LABEL_3 / mplsLabelStackSection3
	73: {{"mpls_label_4", decodeHex}},      // MPLS_LABEL_4 / mplsLabelStackSection4
	74: {{"mpls_label_5", decodeHex}},      // MPLS_LABEL_5 / mplsLabelStackSection5
	75: {{"mpls_label_6", decodeHex}},      // MPLS_LABEL_6 / mplsLabelStackSection6
	76: {{"mpls_label_7", decodeHex}},      // MPLS_LABEL_7 / mplsLabelStackSection7
	77: {{"mpls_label_8", decodeHex}},      // MPLS_LABEL_8 / mplsLabelStackSection8
	78: {{"mpls_label_9", decodeHex}},      // MPLS_LABEL_9 / mplsLabelStackSection9
	79: {{"mpls_label_10", decodeHex}},     // MPLS_LABEL_10 / mplsLabelStackSection10
	80: {{"in_dst_mac", decodeMAC}},        // IN_DST_MAC / destinationMacAddress
	81: {{"out_src_mac", decodeMAC}},       // OUT_SRC_MAC / postSourceMacAddress
	82: {{"interface", decodeString}},      // IF_NAME / interfaceName
	83: {{"interface_desc", decodeString}}, // IF_DESC / interfaceDescription
	84: {{"sampler_name", decodeString}},   // SAMPLER_NAME / samplerName
	85: {{"in_total_bytes", decodeUint}},   // IN_PERMANENT_BYTES / octetTotalCount
	86: {{"in_total_packets", decodeUint}}, // IN_PERMANENT_PKTS / packetTotalCount
	// 87: vendor proprietary
	88: {{"fragment_offset", decodeUint}}, // FRAGMENT_OFFSET / fragmentOffset
	89: {
		{"fwd_status", decodeFwdStatus}, // FORWARDING STATUS / forwardingStatus
		{"fwd_reason", decodeFwdReason},
	},
	90: {{"mpls_vpn_rd", decodeHex}},        // MPLS PAL RD / mplsVpnRouteDistinguisher
	91: {{"mpls_prefix_len", decodeUint}},   // MPLS PREFIX LEN / mplsTopLabelPrefixLength
	92: {{"src_traffic_index", decodeUint}}, // SRC TRAFFIC INDEX / srcTrafficIndex
	93: {{"dst_traffic_index", decodeUint}}, // DST TRAFFIC INDEX / dstTrafficIndex
	94: {{"app_desc", decodeString}},        // APPLICATION DESCRIPTION / applicationDescription
	95: {{"app_id", decodeHex}},             // APPLICATION TAG / applicationId
	96: {{"app_name", decodeString}},        // APPLICATION NAME / applicationName
	// 97: undefined
	98: {{"out_dscp", decodeUint}},           // postipDiffServCodePoint / postIpDiffServCodePoint
	99: {{"replication_factor", decodeUint}}, // replication factor / multicastReplicationFactor
	// 100: deprecated / className
	101: {{"classification_engine_id", decodeUint}}, // undefined / classificationEngineId
	102: {{"l2_packet_section_offset", decodeUint}}, // layer2packetSectionOffset
	103: {{"l2_packet_section_size", decodeUint}},   // layer2packetSectionSize
	104: {{"l2_packet_section_data", decodeHex}},    // layer2packetSectionData
	// 105 - 127:  reserved

	// Common between Netflow v9 ASA extension
	// https://www.cisco.com/c/en/us/td/docs/security/asa/special/netflow/asa_netflow.html
	// and IPFIX.
	148: {{"flow_id", decodeUint}},         // flowId
	152: {{"flow_start_ms", decodeUint}},   // NF_F_FLOW_CREATE_TIME_MSEC / flowStartMilliseconds
	153: {{"flow_end_ms", decodeUint}},     // NF_F_FLOW_END_TIME_MSEC / flowEndMilliseconds
	176: {{"icmp_type", decodeUint}},       // NF_F_ICMP_TYPE / icmpTypeIPv4
	177: {{"icmp_code", decodeUint}},       // NF_F_ICMP_CODE / icmpCodeIPv4
	178: {{"icmp_type", decodeUint}},       // NF_F_ICMP_TYPE_IPV6 / icmpTypeIPv6
	179: {{"icmp_code", decodeUint}},       // NF_F_ICMP_CODE_IPV6 / icmpCodeIPv6
	225: {{"xlat_src", decodeIP}},          // NF_F_XLATE_SRC_ADDR_IPV4 / postNATSourceIPv4Address
	226: {{"xlat_dst", decodeIP}},          // NF_F_XLATE_DST_ADDR_IPV4 / postNATDestinationIPv4Address
	227: {{"xlat_src_port", decodeUint}},   // NF_F_XLATE_SRC_PORT / postNAPTSourceTransportPort
	228: {{"xlat_dst_port", decodeUint}},   // NF_F_XLATE_DST_PORT / postNAPTDestinationTransportPort
	231: {{"initiator_bytes", decodeUint}}, // NF_F_FWD_FLOW_DELTA_BYTES / initiatorOctets
	232: {{"responder_bytes", decodeUint}}, // NF_F_REV_FLOW_DELTA_BYTES / responderOctets
	233: {{"fw_event", decodeFWEvent}},     // NF_F_FW_EVENT / firewallEvent
	281: {{"xlat_src", decodeIP}},          // NF_F_XLATE_SRC_ADDR_IPV6 / postNATSourceIPv6Address
	282: {{"xlat_dst", decodeIP}},          // NF_F_XLATE_DST_ADDR_IPV6 / postNATDestinationIPv6Address
	323: {{"event_time_ms", decodeUint}},   // NF_F_EVENT_TIME_MSEC / observationTimeMilliseconds
	324: {{"event_time_us", decodeUint}},   // NF_F_EVENT_TIME_USEC / observationTimeMicroseconds
	325: {{"event_time_ns", decodeUint}},   // NF_F_EVENT_TIME_NSEC / observationTimeNanoseconds
}

// Default field mappings specific to Netflow version 9
// From documentation at https://www.cisco.com/c/en/us/td/docs/security/asa/special/netflow/asa_netflow.html
var fieldMappingsNetflowV9 = map[uint16][]fieldMapping{
	33000: {{"in_acl_id", decodeHex}},    // NF_F_INGRESS_ACL_ID
	33001: {{"out_acl_id", decodeHex}},   // NF_F_EGRESS_ACL_ID
	33002: {{"fw_event_ext", decodeHex}}, // NF_F_FW_EXT_EVENT
	40000: {{"username", decodeString}},  // NF_F_USERNAME
}

// Default field mappings specific to Netflow version 9
// From documentation at
// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-information-elements
var fieldMappingsIPFIX = map[uint16][]fieldMapping{
	128: {{"bgp_next_as", decodeUint}},              // bgpNextAdjacentAsNumber
	129: {{"bgp_prev_as", decodeUint}},              // bgpPrevAdjacentAsNumber
	130: {{"exporter", decodeIP}},                   // exporterIPv4Address
	131: {{"exporter", decodeIP}},                   // exporterIPv6Address
	132: {{"dropped_bytes", decodeUint}},            // droppedOctetDeltaCount
	133: {{"dropped_packets", decodeUint}},          // droppedPacketDeltaCount
	134: {{"dropped_bytes_total", decodeUint}},      // droppedOctetTotalCount
	135: {{"dropped_packets_total", decodeUint}},    // droppedPacketTotalCount
	136: {{"flow_end_reason", decodeFlowEndReason}}, // flowEndReason
	137: {{"common_properties_id", decodeUint}},     // commonPropertiesId
	138: {{"observation_point_id", decodeUint}},     // observationPointId
	139: {
		{"icmp_type", decodeByteFunc(0)}, // icmpTypeCodeIPv6
		{"icmp_code", decodeByteFunc(1)},
	},
	140: {{"mpls_top_label_ip", decodeIP}}, // mplsTopLabelIPv6Address
	141: {{"linecard_id", decodeUint}},     // lineCardId
	142: {{"port_id", decodeUint}},         // portId
	143: {{"metering_pid", decodeUint}},    // meteringProcessId
	144: {{"exporting_pid", decodeUint}},   // exportingProcessId
	145: {{"template_id", decodeUint}},     // templateId
	146: {{"wlan_channel", decodeUint}},    // wlanChannelId
	147: {{"wlan_ssid", decodeString}},     // wlanSSID
	// 148: common
	149: {{"observation_domain_id", decodeUint}}, // observationDomainId
	150: {{"flow_start", decodeUint}},            // flowStartSeconds
	151: {{"flow_end", decodeUint}},              // flowEndSeconds
	// 152 - 153: common
	154: {{"flow_start_us", decodeUint}},            // flowStartMicroseconds
	155: {{"flow_end_us", decodeUint}},              // flowEndMicroseconds
	156: {{"flow_start_ns", decodeUint}},            // flowStartNanoseconds
	157: {{"flow_end_ns", decodeUint}},              // flowEndNanoseconds
	158: {{"flow_start_delta_us", decodeUint}},      // flowStartDeltaMicroseconds
	159: {{"flow_end_delta_us", decodeUint}},        // flowEndDeltaMicroseconds
	160: {{"system_init_ms", decodeUint}},           // systemInitTimeMilliseconds
	161: {{"flow_duration_ms", decodeUint}},         // flowDurationMilliseconds
	162: {{"flow_duration_us", decodeUint}},         // flowDurationMicroseconds
	163: {{"flow_count_total", decodeUint}},         // observedFlowTotalCount
	164: {{"ignored_packet_total", decodeUint}},     // ignoredPacketTotalCount
	165: {{"ignored_bytes_total", decodeUint}},      // ignoredOctetTotalCount
	166: {{"notsent_flow_count_total", decodeUint}}, // notSentFlowTotalCount
	167: {{"notsent_packet_total", decodeUint}},     // notSentPacketTotalCount
	168: {{"notsent_bytes_total", decodeUint}},      // notSentOctetTotalCount
	169: {{"ipv6_dst_prefix", decodeIP}},            // destinationIPv6Prefix
	170: {{"ipv6_src_prefix", decodeIP}},            // sourceIPv6Prefix
	171: {{"out_bytes_total", decodeUint}},          // postOctetTotalCount
	172: {{"out_packets_total", decodeUint}},        // postPacketTotalCount
	173: {{"flow_key_indicator", decodeHex}},        // flowKeyIndicator
	174: {{"out_mcast_packets_total", decodeUint}},  // postMCastPacketTotalCount
	175: {{"out_mcast_bytes_total", decodeUint}},    // postMCastOctetTotalCount
	// 176 - 179: common
	180: {{"udp_src_port", decodeUint}},             // udpSourcePort
	181: {{"udp_dst_port", decodeUint}},             // udpDestinationPort
	182: {{"tcp_src_port", decodeUint}},             // tcpSourcePort
	183: {{"tcp_dst_port", decodeUint}},             // tcpDestinationPort
	184: {{"tcp_seq_number", decodeUint}},           // tcpSequenceNumber
	185: {{"tcp_ack_number", decodeUint}},           // tcpAcknowledgementNumber
	186: {{"tcp_window_size", decodeUint}},          // tcpWindowSize
	187: {{"tcp_urgent_ptr", decodeUint}},           // tcpUrgentPointer
	188: {{"tcp_header_len", decodeUint}},           // tcpHeaderLength
	189: {{"ip_header_len", decodeUint}},            // ipHeaderLength
	190: {{"ipv4_total_len", decodeUint}},           // totalLengthIPv4
	191: {{"ipv6_payload_len", decodeUint}},         // payloadLengthIPv6
	192: {{"ttl", decodeUint}},                      // ipTTL
	193: {{"ipv6_next_header", decodeUint}},         // nextHeaderIPv6
	194: {{"mpls_payload_len", decodeUint}},         // mplsPayloadLength
	195: {{"dscp", decodeUint}},                     // ipDiffServCodePoint
	196: {{"precedence", decodeUint}},               // ipPrecedence
	197: {{"fragment_flags", decodeFragmentFlags}},  // fragmentFlags
	198: {{"bytes_sqr_sum", decodeUint}},            // octetDeltaSumOfSquares
	199: {{"bytes_sqr_sum_total", decodeUint}},      // octetTotalSumOfSquares
	200: {{"mpls_top_label_ttl", decodeUint}},       // mplsTopLabelTTL
	201: {{"mpls_stack_len", decodeUint}},           // mplsLabelStackLength
	202: {{"mpls_stack_depth", decodeUint}},         // mplsLabelStackDepth
	203: {{"mpls_top_label_exp", decodeUint}},       // mplsTopLabelExp
	204: {{"ip_payload_len", decodeUint}},           // ipPayloadLength
	205: {{"udp_msg_len", decodeUint}},              // udpMessageLength
	206: {{"mcast", decodeUint}},                    // isMulticast
	207: {{"ipv4_inet_header_len", decodeUint}},     // ipv4IHL
	208: {{"ipv4_options", decodeIPv4Options}},      // ipv4Options
	209: {{"tcp_options", decodeHex}},               // tcpOptions
	210: {{"padding", decodeHex}},                   // paddingOctets
	211: {{"collector", decodeIP}},                  // collectorIPv4Address
	212: {{"collector", decodeIP}},                  // collectorIPv6Address
	213: {{"export_interface", decodeUint}},         // exportInterface
	214: {{"export_proto_version", decodeUint}},     // exportProtocolVersion
	215: {{"export_transport_proto", decodeUint}},   // exportTransportProtocol
	216: {{"collector_transport_port", decodeUint}}, // collectorTransportPort
	217: {{"exporter_transport_port", decodeUint}},  // exporterTransportPort
	218: {{"tcp_syn_total", decodeUint}},            // tcpSynTotalCount
	219: {{"tcp_fin_total", decodeUint}},            // tcpFinTotalCount
	220: {{"tcp_rst_total", decodeUint}},            // tcpRstTotalCount
	221: {{"tcp_psh_total", decodeUint}},            // tcpPshTotalCount
	222: {{"tcp_ack_total", decodeUint}},            // tcpAckTotalCount
	223: {{"tcp_urg_total", decodeUint}},            // tcpUrgTotalCount
	224: {{"ip_total_len", decodeUint}},             // ipTotalLength
	// 225 - 228: common
	229: {{"nat_origin_addr_realm", decodeUint}}, // natOriginatingAddressRealm
	230: {{"nat_event", decodeUint}},             // natEvent
	// 231 - 233: common
	234: {{"in_vrf_id", decodeUint}},                      // ingressVRFID
	235: {{"out_vrf_id", decodeUint}},                     // egressVRFID
	236: {{"vrf_name", decodeString}},                     // VRFname
	237: {{"out_mpls_top_label_exp", decodeUint}},         // postMplsTopLabelExp
	238: {{"tcp_window_scale", decodeUint}},               // tcpWindowScale
	239: {{"biflow_direction", decodeBiflowDirection}},    // biflowDirection
	240: {{"eth_header_len", decodeUint}},                 // ethernetHeaderLength
	241: {{"eth_payload_len", decodeUint}},                // ethernetPayloadLength
	242: {{"eth_total_len", decodeUint}},                  // ethernetTotalLength
	243: {{"vlan_id", decodeUint}},                        // dot1qVlanId
	244: {{"vlan_priority", decodeUint}},                  // dot1qPriority
	245: {{"vlan_customer_id", decodeUint}},               // dot1qCustomerVlanId
	246: {{"vlan_customer_priority", decodeUint}},         // dot1qCustomerPriority
	247: {{"metro_evc_id", decodeString}},                 // metroEvcId
	248: {{"metro_evc_type", decodeUint}},                 // metroEvcType
	249: {{"pseudo_wire_id", decodeUint}},                 // pseudoWireId
	250: {{"pseudo_wire_type", decodeHex}},                // pseudoWireType
	251: {{"pseudo_wire_ctrl_word", decodeHex}},           // pseudoWireControlWord
	252: {{"in_phy_interface", decodeUint}},               // ingressPhysicalInterface
	253: {{"out_phy_interface", decodeUint}},              // egressPhysicalInterface
	254: {{"out_vlan_id", decodeUint}},                    // postDot1qVlanId
	255: {{"out_vlan_customer_id", decodeUint}},           // postDot1qCustomerVlanId
	256: {{"eth_type", decodeHex}},                        // ethernetType
	257: {{"out_precedence", decodeUint}},                 // postIpPrecedence
	258: {{"collection_time_ms", decodeUint}},             // collectionTimeMilliseconds
	259: {{"export_sctp_stream_id", decodeUint}},          // exportSctpStreamId
	260: {{"max_export_time", decodeUint}},                // maxExportSeconds
	261: {{"max_flow_end_time", decodeUint}},              // maxFlowEndSeconds
	262: {{"msg_md5", decodeHex}},                         // messageMD5Checksum
	263: {{"msg_scope", decodeUint}},                      // messageScope
	264: {{"min_export_time", decodeUint}},                // minExportSeconds
	265: {{"min_flow_start_time", decodeUint}},            // minFlowStartSeconds
	266: {{"opaque_bytes", decodeUint}},                   // opaqueOctets
	267: { /* MUST BE IGNORED according to standard */ },  // sessionScope
	268: {{"max_flow_end_time_us", decodeUint}},           // maxFlowEndMicroseconds
	269: {{"max_flow_end_time_ms", decodeUint}},           // maxFlowEndMilliseconds
	270: {{"max_flow_end_time_ns", decodeUint}},           // maxFlowEndNanoseconds
	271: {{"min_flow_start_time_us", decodeUint}},         // minFlowStartMicroseconds
	272: {{"min_flow_start_time_ms", decodeUint}},         // minFlowStartMilliseconds
	273: {{"min_flow_start_time_ns", decodeUint}},         // minFlowStartNanoseconds
	274: {{"collector_cert", decodeString}},               // collectorCertificate
	275: {{"exporter_cert", decodeString}},                // exporterCertificate
	276: {{"data_records_reliability", decodeBool}},       // dataRecordsReliability
	277: {{"observation_point_type", decodeOpsPointType}}, // observationPointType
	278: {{"connection_new_count", decodeUint}},           // newConnectionDeltaCount
	279: {{"connection_duration_sum", decodeUint}},        // connectionSumDurationSeconds
	280: {{"connection_transaction_id", decodeUint}},      // connectionTransactionId
	// 281 - 282: common
	283: {{"nat_pool_id", decodeUint}},     // natPoolId
	284: {{"nat_pool_name", decodeString}}, // natPoolName
	285: {
		{"anon_stability_class", decodeAnonStabilityClass}, // anonymizationFlags
		{"anon_flags", decodeAnonFlags},
	},
	286: {{"anon_technique", decodeAnonTechnique}},         // anonymizationTechnique
	287: {{"information_element", decodeUint}},             // informationElementIndex
	288: {{"p2p", decodeTechnology}},                       // p2pTechnology
	289: {{"tunnel", decodeTechnology}},                    // tunnelTechnology
	290: {{"encryption", decodeTechnology}},                // encryptedTechnology
	291: { /* IGNORED for parse-ability */ },               // basicList
	292: { /* IGNORED for parse-ability */ },               // subTemplateList
	293: { /* IGNORED for parse-ability */ },               // subTemplateMultiList
	294: {{"bgp_validity_state", decodeUint}},              // bgpValidityState
	295: {{"ipsec_spi", decodeUint}},                       // IPSecSPI
	296: {{"gre_key", decodeUint}},                         // greKey
	297: {{"nat_type", decodeIPNatType}},                   // natType
	298: {{"initiator_packets", decodeUint}},               // initiatorPackets
	299: {{"responder_packets", decodeUint}},               // responderPackets
	300: {{"observation_domain_name", decodeString}},       // observationDomainName
	301: {{"observation_seq_id", decodeUint}},              // selectionSequenceId
	302: {{"selector_id", decodeUint}},                     // selectorId
	303: {{"information_elem_id", decodeUint}},             // informationElementId
	304: {{"selector_algo", decodeSelectorAlgorithm}},      // selectorAlgorithm
	305: {{"sampling_packet_interval", decodeUint}},        // samplingPacketInterval
	306: {{"sampling_packet_space", decodeUint}},           // samplingPacketSpace
	307: {{"sampling_time_interval_us", decodeUint}},       // samplingTimeInterval
	308: {{"sampling_time_space_us", decodeUint}},          // samplingTimeSpace
	309: {{"sampling_size", decodeUint}},                   // samplingSize
	310: {{"sampling_population", decodeUint}},             // samplingPopulation
	311: {{"sampling_probability", decodeFloat64}},         // samplingProbability
	312: {{"datalink_frame_size", decodeUint}},             // dataLinkFrameSize
	313: {{"ip_header_packet_section", decodeHex}},         // ipHeaderPacketSection
	314: {{"ip_payload_packet_section", decodeHex}},        // ipPayloadPacketSection
	315: {{"datalink_frame_section", decodeHex}},           // dataLinkFrameSection
	316: {{"mpls_label_stack_section", decodeHex}},         // mplsLabelStackSection
	317: {{"mpls_payload_packet_section", decodeHex}},      // mplsPayloadPacketSection
	318: {{"selector_total_packets_observed", decodeUint}}, // selectorIdTotalPktsObserved
	319: {{"selector_total_packets_selected", decodeUint}}, // selectorIdTotalPktsSelected
	320: {{"absolute_error", decodeFloat64}},               // absoluteError
	321: {{"relative_error", decodeFloat64}},               // relativeError
	322: {{"event_time", decodeUint}},                      // observationTimeSeconds
	// 323 - 325: common
	326: {{"hash_digest", decodeHex}},                         // digestHashValue
	327: {{"hash_ip_payload_offset", decodeUint}},             // hashIPPayloadOffset
	328: {{"hash_ip_payload_size", decodeUint}},               // hashIPPayloadSize
	329: {{"hash_out_range_min", decodeUint}},                 // hashOutputRangeMin
	330: {{"hash_out_range_max", decodeUint}},                 // hashOutputRangeMax
	331: {{"hash_selected_range_min", decodeUint}},            // hashSelectedRangeMin
	332: {{"hash_selected_range_max", decodeUint}},            // hashSelectedRangeMax
	333: {{"hash_digest_out", decodeBool}},                    // hashDigestOutput
	334: {{"hash_init_val", decodeUint}},                      // hashInitialiserValue
	335: {{"selector_name", decodeString}},                    // selectorName
	336: {{"upper_confidence_interval_limit", decodeFloat64}}, // upperCILimit
	337: {{"lower_confidence_interval_limit", decodeFloat64}}, // upperCILimit
	338: {{"confidence_level", decodeFloat64}},                // confidenceLevel
	// 339 - 346: information element fields, do not map for now
	347: {{"virtual_station_interface_id", decodeHex}},      // virtualStationInterfaceId
	348: {{"virtual_station_interface_name", decodeString}}, // virtualStationInterfaceName
	349: {{"virtual_station_uuid", decodeHex}},              // virtualStationUUID
	350: {{"virtual_station_name", decodeString}},           // virtualStationName
	351: {{"l2_segment_id", decodeUint}},                    // layer2SegmentId
	352: {{"l2_bytes", decodeUint}},                         // layer2OctetDeltaCount
	353: {{"l2_bytes_total", decodeUint}},                   // layer2OctetTotalCount
	354: {{"in_unicast_packets_total", decodeUint}},         // ingressUnicastPacketTotalCount
	355: {{"in_mcast_packets_total", decodeUint}},           // ingressMulticastPacketTotalCount
	356: {{"in_broadcast_packets_total", decodeUint}},       // ingressBroadcastPacketTotalCount
	357: {{"out_unicast_packets_total", decodeUint}},        // egressUnicastPacketTotalCount
	358: {{"out_broadcast_packets_total", decodeUint}},      // egressBroadcastPacketTotalCount
	359: {{"monitoring_interval_start_ms", decodeUint}},     // monitoringIntervalStartMilliSeconds
	360: {{"monitoring_interval_end_ms", decodeUint}},       // monitoringIntervalEndMilliSeconds
	361: {{"port_range_start", decodeUint}},                 // portRangeStart
	362: {{"port_range_end", decodeUint}},                   // portRangeEnd
	363: {{"port_range_step_size", decodeUint}},             // portRangeStepSize
	364: {{"port_range_ports", decodeUint}},                 // portRangeNumPorts
	365: {{"station_mac", decodeMAC}},                       // staMacAddress
	366: {{"station", decodeIP}},                            // staIPv4Address
	367: {{"wtp_mac", decodeMAC}},                           // wtpMacAddress
	368: {{"in_interface_type", decodeUint}},                // ingressInterfaceType
	369: {{"out_interface_type", decodeUint}},               // egressInterfaceType
	370: {{"rtp_seq_number", decodeUint}},                   // rtpSequenceNumber
	371: {{"username", decodeString}},                       // userName
	372: {{"app_category", decodeString}},                   // applicationCategoryName
	373: {{"app_subcategory", decodeHex}},                   // applicationSubCategoryName
	374: {{"app_group", decodeString}},                      // applicationGroupName
	375: {{"flows_original_present", decodeUint}},           // originalFlowsPresent
	376: {{"flows_original_initiated", decodeUint}},         // originalFlowsInitiated
	377: {{"flows_original_completed", decodeUint}},         // originalFlowsCompleted
	378: {{"flow_src_ip_count", decodeUint}},                // distinctCountOfSourceIPAddress
	379: {{"flow_dst_ip_count", decodeUint}},                // distinctCountOfDestinationIPAddress
	380: {{"flow_src_ipv4_count", decodeUint}},              // distinctCountOfSourceIPv4Address
	381: {{"flow_dst_ipv4_count", decodeUint}},              // distinctCountOfDestinationIPv4Address
	382: {{"flow_src_ipv6_count", decodeUint}},              // distinctCountOfSourceIPv6Address
	383: {{"flow_dst_ipv6_count", decodeUint}},              // distinctCountOfDestinationIPv6Address
	384: {{"value_dist_method", decodeValueDistMethod}},     // valueDistributionMethod
	385: {{"rfc3550_jitter_ms", decodeUint}},                // rfc3550JitterMilliseconds
	386: {{"rfc3550_jitter_us", decodeUint}},                // rfc3550JitterMicroseconds
	387: {{"rfc3550_jitter_ns", decodeUint}},                // rfc3550JitterNanoseconds
	388: {{"vlan_dei", decodeBool}},                         // dot1qDEI
	389: {{"vlan_customer_dei", decodeUint}},                // dot1qCustomerDEI
	390: {{"flow_selector_algo", decodeSelectorAlgorithm}},  // flowSelectorAlgorithm
	391: {{"flow_selected_byte_count", decodeUint}},         // flowSelectedOctetDeltaCount
	392: {{"flow_selected_packet_count", decodeUint}},       // flowSelectedOctetDeltaCount
	393: {{"flow_selected_count", decodeUint}},              // flowSelectedFlowDeltaCount
	394: {{"selector_id_flows_observed_total", decodeUint}}, // selectorIDTotalFlowsObserved
	395: {{"selector_id_flows_selected_total", decodeUint}}, // selectorIDTotalFlowsSelected
	396: {{"sampling_flow_interval_count", decodeUint}},     // samplingFlowInterval
	397: {{"sampling_flow_spacing_count", decodeUint}},      // samplingFlowSpacing
	398: {{"sampling_flow_interval_ms", decodeUint}},        // flowSamplingTimeInterval
	399: {{"sampling_flow_spacing_ms", decodeUint}},         // flowSamplingTimeSpacing
	400: {{"flow_domain_hash_element_id", decodeUint}},      // hashFlowDomain
	401: {{"transport_byte_count", decodeUint}},             // transportOctetDeltaCount
	402: {{"transport_packet_count", decodeUint}},           // transportPacketDeltaCount
	403: {{"exporter_original_ip", decodeUint}},             // originalExporterIPv4Address
	404: {{"exporter_original_ip", decodeUint}},             // originalExporterIPv6Address
	405: {{"exporter_original_domain", decodeHex}},          // originalObservationDomainId
	406: {{"intermediate_process_id", decodeHex}},           // intermediateProcessId
	407: {{"ignored_data_records_total", decodeUint}},       // ignoredDataRecordTotalCount
	408: {{"datalink_frame_type", decodeDataLinkFrameType}}, // dataLinkFrameType
	409: {{"section_offset", decodeUint}},                   // sectionOffset
	410: {{"section_exported_bytes", decodeUint}},           // sectionExportedOctets
	411: {{"vlan_service_instance_tag", decodeHex}},         // dot1qServiceInstanceTag
	412: {{"vlan_service_instance_id", decodeUint}},         // dot1qServiceInstanceId
	413: {{"vlan_service_instance_priority", decodeUint}},   // dot1qServiceInstancePriority
	414: {{"vlan_customer_src_mac", decodeMAC}},             // dot1qCustomerSourceMacAddress
	415: {{"vlan_customer_dst_mac", decodeMAC}},             // dot1qCustomerDestinationMacAddress
	// 416: deprecated
	417: {{"post_layer2_bytes", decodeUint}},       // postLayer2OctetDeltaCount
	418: {{"post_mcast_layer2_bytes", decodeUint}}, // postMCastLayer2OctetDeltaCount
	// 419: deprecated
	420: {{"post_layer2_bytes_total", decodeUint}},                    // postLayer2OctetTotalCount
	421: {{"post_mcast_layer2_bytes_total", decodeUint}},              // postMCastLayer2OctetTotalCount
	422: {{"min_layer2_total_length", decodeUint}},                    // minimumLayer2TotalLength
	423: {{"max_layer2_total_length", decodeUint}},                    // maximumLayer2TotalLength
	424: {{"dropped_layer2_bytes", decodeUint}},                       // droppedLayer2OctetDeltaCount
	425: {{"dropped_layer2_bytes_total", decodeUint}},                 // droppedLayer2OctetTotalCount
	426: {{"ignored_layer2_bytes_total", decodeUint}},                 // ignoredLayer2OctetTotalCount
	427: {{"not_sent_layer2_bytes_total", decodeUint}},                // notSentLayer2OctetTotalCount
	428: {{"layer2_bytes_sumsqr", decodeUint}},                        // layer2OctetDeltaSumOfSquares
	429: {{"layer2_bytes_total_sumsqr", decodeUint}},                  // layer2OctetTotalSumOfSquares
	430: {{"layer2_frames", decodeUint}},                              // layer2FrameDeltaCount
	431: {{"layer2_frames_total", decodeUint}},                        // layer2FrameTotalCount
	432: {{"pseudo_wire_dst", decodeIP}},                              // pseudoWireDestinationIPv4Address
	433: {{"ignored_layer2_frames_total", decodeUint}},                // ignoredLayer2FrameTotalCount
	434: {{"mib_obj_value_int", decodeInt}},                           // mibObjectValueInteger
	435: {{"mib_obj_value_str", decodeString}},                        // mibObjectValueOctetString
	436: {{"mib_obj_value_oid", decodeHex}},                           // mibObjectValueOID
	437: {{"mib_obj_value_bits", decodeHex}},                          // mibObjectValueBits
	438: {{"mib_obj_value_ip", decodeIP}},                             // mibObjectValueIPAddress
	439: {{"mib_obj_value_counter", decodeUint}},                      // mibObjectValueCounter
	440: {{"mib_obj_value_gauge", decodeUint}},                        // mibObjectValueGauge
	441: {{"mib_obj_value_time", decodeUint}},                         // mibObjectValueTimeTicks
	442: {{"mib_obj_value_uint", decodeUint}},                         // mibObjectValueUnsigned
	443: {{"mib_obj_value_table", decodeHex}},                         // mibObjectValueTable
	444: {{"mib_obj_value_row", decodeHex}},                           // mibObjectValueRow
	445: {{"mib_oid", decodeHex}},                                     // mibObjectIdentifier
	446: {{"mib_sub_id", decodeUint}},                                 // mibSubIdentifier
	447: {{"mib_index_indicator", decodeHex}},                         // mibIndexIndicator
	448: {{"mib_capture_time_semantics", decodeCaptureTimeSemantics}}, // mibCaptureTimeSemantics
	449: {{"mib_context_engine_id", decodeHex}},                       // mibContextEngineID
	450: {{"mib_context_name", decodeString}},                         // mibContextName
	451: {{"mib_obj_name", decodeString}},                             // mibObjectName
	452: {{"mib_obj_desc", decodeString}},                             // mibObjectDescription
	453: {{"mib_obj_syntax", decodeString}},                           // mibObjectSyntax
	454: {{"mib_module_name", decodeString}},                          // mibModuleName
	455: {{"imsi", decodeString}},                                     // mobileIMSI
	456: {{"msisdn", decodeString}},                                   // mobileMSISDN
	457: {{"http_status_code", decodeUint}},                           // httpStatusCode
	458: {{"src_transport_port_limit", decodeUint}},                   // sourceTransportPortsLimit
	459: {{"http_request_method", decodeString}},                      // httpRequestMethod
	460: {{"http_request_host", decodeString}},                        // httpRequestHost
	461: {{"http_request_target", decodeString}},                      // httpRequestTarget
	462: {{"http_msg_version", decodeString}},                         // httpMessageVersion
	463: {{"nat_instance_id", decodeUint}},                            // natInstanceID
	464: {{"internal_addr_realm", decodeHex}},                         // internalAddressRealm
	465: {{"external_addr_realm", decodeHex}},                         // externalAddressRealm
	466: {{"nat_quota_exceeded_event", decodeUint}},                   // natQuotaExceededEvent
	467: {{"nat_threshold_event", decodeUint}},                        // natThresholdEvent
	468: {{"http_user_agent", decodeString}},                          // httpUserAgent
	469: {{"http_content_type", decodeString}},                        // httpContentType
	470: {{"http_reason_phrase", decodeString}},                       // httpReasonPhrase
	471: {{"max_session_entries", decodeUint}},                        // maxSessionEntries
	472: {{"max_bib_entries", decodeUint}},                            // maxBIBEntries
	473: {{"max_entries_per_user", decodeUint}},                       // maxEntriesPerUser
	474: {{"max_subscribers", decodeUint}},                            // maxSubscribers
	475: {{"max_fragments_pending_reassembly", decodeUint}},           // maxFragmentsPendingReassembly
	476: {{"addr_pool_threshold_high", decodeUint}},                   // addressPoolHighThreshold
	477: {{"addr_pool_threshold_low", decodeUint}},                    // addressPoolLowThreshold
	478: {{"addr_port_mapping_threshold_high", decodeUint}},           // addressPortMappingHighThreshold
	479: {{"addr_port_mapping_threshold_low", decodeUint}},            // addressPortMappingLowThreshold
	480: {{"addr_port_mapping_per_user_threshold_high", decodeUint}},  // addressPortMappingPerUserHighThreshold
	481: {{"global_addr_mapping_threshold_high", decodeUint}},         // globalAddressMappingHighThreshold
	482: {{"vpn_identifier", decodeIP}},                               // vpnIdentifier
	483: {{"bgp_community", decodeUint}},                              // bgpCommunity
	484: {{"bgp_src_community_list", decodeHex}},                      // bgpSourceCommunityList
	485: {{"bgp_dst_community_list", decodeHex}},                      // bgpDestinationCommunityList
	486: {{"bgp_extended_community", decodeHex}},                      // bgpExtendedCommunity
	487: {{"bgp_src_extended_community_list", decodeHex}},             // bgpSourceExtendedCommunityList
	488: {{"bgp_dst_extended_community_list", decodeHex}},             // bgpDestinationExtendedCommunityList
	489: {{"bgp_large_community", decodeHex}},                         // bgpLargeCommunity
	490: {{"bgp_src_large_community_list", decodeHex}},                // bgpSourceLargeCommunityList
	491: {{"bgp_dst_large_community_list", decodeHex}},                // bgpDestinationLargeCommunityList
}

// Decoder structure
type netflowDecoder struct {
	penFiles []string
	log      telegraf.Logger

	templates     map[string]netflow.NetFlowTemplateSystem
	mappingsV9    map[uint16]fieldMapping
	mappingsIPFIX map[uint16]fieldMapping
	mappingsPEN   map[string]fieldMapping

	logged map[string]bool
	sync.Mutex
}

func (d *netflowDecoder) decode(srcIP net.IP, payload []byte) ([]telegraf.Metric, error) {
	var metrics []telegraf.Metric

	t := time.Now()
	src := srcIP.String()

	// Prepare the templates used to decode the messages
	d.Lock()
	if _, ok := d.templates[src]; !ok {
		d.templates[src] = netflow.CreateTemplateSystem()
	}
	templates := d.templates[src]
	d.Unlock()

	// Decode the overall message
	var msg9 netflow.NFv9Packet
	var msg10 netflow.IPFIXPacket
	buf := bytes.NewBuffer(payload)
	if err := netflow.DecodeMessageVersion(buf, templates, &msg9, &msg10); err != nil {
		if errors.Is(err, netflow.ErrorTemplateNotFound) {
			msg := "Skipping packet until the device resends the required template..."
			d.log.Warnf("%v. %s", err, msg)
			return nil, nil
		}
		return nil, fmt.Errorf("decoding message failed: %w", err)
	}

	// Extract metrics
	switch {
	case msg9.Version == 9:
		msg := msg9
		for _, flowsets := range msg.FlowSets {
			switch fs := flowsets.(type) {
			case netflow.TemplateFlowSet:
			case netflow.NFv9OptionsTemplateFlowSet:
			case netflow.OptionsDataFlowSet:
				for _, record := range fs.Records {
					tags := map[string]string{
						"source":  src,
						"version": "NetFlowV9",
					}
					fields := make(map[string]interface{})
					for _, value := range record.ScopesValues {
						decodedFields, err := d.decodeValueV9(value)
						if err != nil {
							d.log.Errorf("decoding option record %+v failed: %v", record, err)
							continue
						}
						for _, field := range decodedFields {
							fields[field.Key] = field.Value
						}
					}
					for _, value := range record.OptionsValues {
						decodedFields, err := d.decodeValueV9(value)
						if err != nil {
							d.log.Errorf("decoding option record %+v failed: %v", record, err)
							continue
						}
						for _, field := range decodedFields {
							fields[field.Key] = field.Value
						}
					}
					metrics = append(metrics, metric.New("netflow_options", tags, fields, t))
				}
			case netflow.DataFlowSet:
				for _, record := range fs.Records {
					tags := map[string]string{
						"source":  src,
						"version": "NetFlowV9",
					}
					fields := make(map[string]interface{})
					for _, value := range record.Values {
						decodedFields, err := d.decodeValueV9(value)
						if err != nil {
							d.log.Errorf("decoding record %+v failed: %v", record, err)
							continue
						}
						for _, field := range decodedFields {
							fields[field.Key] = field.Value
						}
					}
					metrics = append(metrics, metric.New("netflow", tags, fields, t))
				}
			}
		}
	case msg10.Version == 10:
		msg := msg10
		for _, flowsets := range msg.FlowSets {
			switch fs := flowsets.(type) {
			case netflow.TemplateFlowSet:
			case netflow.IPFIXOptionsTemplateFlowSet:
			case netflow.OptionsDataFlowSet:
				for _, record := range fs.Records {
					tags := map[string]string{
						"source":  src,
						"version": "IPFIX",
					}
					fields := make(map[string]interface{})
					for _, value := range record.ScopesValues {
						decodedFields, err := d.decodeValueIPFIX(value)
						if err != nil {
							d.log.Errorf("decoding option record %+v failed: %v", record, err)
							continue
						}
						for _, field := range decodedFields {
							fields[field.Key] = field.Value
						}
					}
					for _, value := range record.OptionsValues {
						decodedFields, err := d.decodeValueIPFIX(value)
						if err != nil {
							d.log.Errorf("decoding option record %+v failed: %v", record, err)
							continue
						}
						for _, field := range decodedFields {
							fields[field.Key] = field.Value
						}
					}
					metrics = append(metrics, metric.New("netflow_options", tags, fields, t))
				}
			case netflow.DataFlowSet:
				for _, record := range fs.Records {
					tags := map[string]string{
						"source":  srcIP.String(),
						"version": "IPFIX",
					}
					fields := make(map[string]interface{})
					t := time.Now()
					for _, value := range record.Values {
						decodedFields, err := d.decodeValueIPFIX(value)
						if err != nil {
							d.log.Errorf("decoding value %+v failed: %v", value, err)
							continue
						}
						for _, field := range decodedFields {
							fields[field.Key] = field.Value
						}
					}
					metrics = append(metrics, metric.New("netflow", tags, fields, t))
				}
			}
		}
	default:
		return nil, errors.New("invalid message of type")
	}

	return metrics, nil
}

func (d *netflowDecoder) init() error {
	if err := initL4ProtoMapping(); err != nil {
		return fmt.Errorf("initializing layer 4 protocol mapping failed: %w", err)
	}
	if err := initIPv4OptionMapping(); err != nil {
		return fmt.Errorf("initializing IPv4 options mapping failed: %w", err)
	}

	d.templates = make(map[string]netflow.NetFlowTemplateSystem)
	d.mappingsV9 = make(map[uint16]fieldMapping)
	d.mappingsIPFIX = make(map[uint16]fieldMapping)
	d.mappingsPEN = make(map[string]fieldMapping)
	for _, fn := range d.penFiles {
		d.log.Debugf("Loading PEN mapping file %q...", fn)
		mappings, err := loadMapping(fn)
		if err != nil {
			return err
		}
		for k, v := range mappings {
			if !regexpIPFIXPENMapping.MatchString(k) {
				return fmt.Errorf("key %q in file %q does not match pattern <PEN>.<element-id>; maybe wrong file", k, fn)
			}
			if _, found := d.mappingsPEN[k]; found {
				return fmt.Errorf("duplicate entries for ID %q", k)
			}
			d.mappingsPEN[k] = v
		}
	}
	d.log.Infof("Loaded %d PEN mappings...", len(d.mappingsPEN))

	d.logged = make(map[string]bool)

	return nil
}

func (d *netflowDecoder) decodeValueV9(field netflow.DataField) ([]telegraf.Field, error) {
	raw := field.Value.([]byte)
	elementID := field.Type

	// Check the user-specified mapping
	if m, found := d.mappingsV9[elementID]; found {
		v, err := m.decoder(raw)
		if err != nil {
			return nil, err
		}
		return []telegraf.Field{{Key: m.name, Value: v}}, nil
	}

	// Check the version specific default field mappings
	if mappings, found := fieldMappingsNetflowV9[elementID]; found {
		var fields []telegraf.Field
		for _, m := range mappings {
			v, err := m.decoder(raw)
			if err != nil {
				return nil, err
			}
			fields = append(fields, telegraf.Field{Key: m.name, Value: v})
		}
		return fields, nil
	}

	// Check the common default field mappings
	if mappings, found := fieldMappingsNetflowCommon[elementID]; found {
		var fields []telegraf.Field
		for _, m := range mappings {
			v, err := m.decoder(raw)
			if err != nil {
				return nil, err
			}
			fields = append(fields, telegraf.Field{Key: m.name, Value: v})
		}
		return fields, nil
	}

	// Fallback to IPFIX mappings as some devices seem to send IPFIX elements in
	// Netflow v9 packets. See https://github.com/influxdata/telegraf/issues/14902
	// and https://github.com/influxdata/telegraf/issues/14903.
	if mappings, found := fieldMappingsIPFIX[elementID]; found {
		var fields []telegraf.Field
		for _, m := range mappings {
			v, err := m.decoder(raw)
			if err != nil {
				return nil, err
			}
			fields = append(fields, telegraf.Field{Key: m.name, Value: v})
		}
		return fields, nil
	}

	// Return the raw data if no mapping was found
	key := fmt.Sprintf("type_%d", elementID)
	if !d.logged[key] {
		d.log.Debugf("unknown Netflow v9 data field %v", field)
		d.logged[key] = true
	}
	v, err := decodeHex(raw)
	if err != nil {
		return nil, err
	}

	return []telegraf.Field{{Key: key, Value: v}}, nil
}

func (d *netflowDecoder) decodeValueIPFIX(field netflow.DataField) ([]telegraf.Field, error) {
	raw := field.Value.([]byte)

	// Checking for reverse elements according to RFC5103
	var prefix string
	elementID := field.Type
	if field.Type&0x4000 != 0 {
		prefix = "rev_"
		elementID = field.Type & (0x4000 ^ 0xffff)
	}

	// Handle messages with Private Enterprise Numbers (PENs)
	if field.PenProvided {
		key := fmt.Sprintf("%d.%d", field.Pen, elementID)
		if m, found := d.mappingsPEN[key]; found {
			name := prefix + m.name
			v, err := m.decoder(raw)
			if err != nil {
				return nil, err
			}
			return []telegraf.Field{{Key: name, Value: v}}, nil
		}
		if !d.logged[key] {
			d.log.Debugf("unknown IPFIX PEN data field %v", field)
			d.logged[key] = true
		}
		name := fmt.Sprintf("type_%d_%s%d", field.Pen, prefix, elementID)
		v, err := decodeHex(raw)
		if err != nil {
			return nil, err
		}
		return []telegraf.Field{{Key: name, Value: v}}, nil
	}

	// Check the user-specified mapping
	if m, found := d.mappingsIPFIX[elementID]; found {
		v, err := m.decoder(raw)
		if err != nil {
			return nil, err
		}
		return []telegraf.Field{{Key: prefix + m.name, Value: v}}, nil
	}

	// Check the version specific default field mappings
	if mappings, found := fieldMappingsIPFIX[elementID]; found {
		var fields []telegraf.Field
		for _, m := range mappings {
			v, err := m.decoder(raw)
			if err != nil {
				return nil, err
			}
			fields = append(fields, telegraf.Field{Key: prefix + m.name, Value: v})
		}
		return fields, nil
	}

	// Check the common default field mappings
	if mappings, found := fieldMappingsNetflowCommon[elementID]; found {
		var fields []telegraf.Field
		for _, m := range mappings {
			v, err := m.decoder(raw)
			if err != nil {
				return nil, err
			}
			fields = append(fields, telegraf.Field{Key: prefix + m.name, Value: v})
		}
		return fields, nil
	}

	// Return the raw data if no mapping was found
	key := fmt.Sprintf("type_%d", elementID)
	if !d.logged[key] {
		d.log.Debugf("unknown IPFIX data field %v", field)
		d.logged[key] = true
	}
	v, err := decodeHex(raw)
	if err != nil {
		return nil, err
	}
	return []telegraf.Field{{Key: key, Value: v}}, nil
}
