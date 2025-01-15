package netflow

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
)

// From https://www.iana.org/assignments/protocol-numbers/protocol-numbers.xhtml
//
//go:embed layer4_protocol_numbers.csv
var l4ProtoFile []byte

// From https://www.iana.org/assignments/ip-parameters/ip-parameters.xhtml
//
//go:embed ipv4_options.csv
var ip4OptionFile []byte

var l4ProtoMapping map[uint8]string
var ipv4OptionMapping []string

func initL4ProtoMapping() error {
	buf := bytes.NewBuffer(l4ProtoFile)
	reader := csv.NewReader(buf)
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}
	if len(records) < 2 {
		return errors.New("empty file")
	}

	l4ProtoMapping = make(map[uint8]string)
	for _, r := range records[1:] {
		if len(r) != 2 {
			return fmt.Errorf("invalid record: %v", r)
		}
		name := strings.ToLower(r[1])
		if name == "" {
			continue
		}
		id, err := strconv.ParseUint(r[0], 10, 8)
		if err != nil {
			return fmt.Errorf("%w: %v", err, r)
		}
		l4ProtoMapping[uint8(id)] = name
	}

	return nil
}

func initIPv4OptionMapping() error {
	buf := bytes.NewBuffer(ip4OptionFile)
	reader := csv.NewReader(buf)
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}
	if len(records) < 2 {
		return errors.New("empty file")
	}

	ipv4OptionMapping = make([]string, 32)
	for _, r := range records[1:] {
		if len(r) != 2 {
			return fmt.Errorf("invalid record: %v", r)
		}
		idx, err := strconv.ParseUint(r[0], 10, 8)
		if err != nil {
			return fmt.Errorf("%w: %v", err, r)
		}
		ipv4OptionMapping[idx] = r[1]
	}

	return nil
}

func decodeInt(b []byte) (interface{}, error) {
	switch len(b) {
	case 0:
		return int64(0), nil
	case 1:
		return int64(int8(b[0])), nil
	case 2:
		return int64(int16(binary.BigEndian.Uint16(b))), nil
	case 4:
		return int64(int32(binary.BigEndian.Uint32(b))), nil
	case 8:
		return int64(binary.BigEndian.Uint64(b)), nil
	}
	return nil, fmt.Errorf("invalid length for int buffer %v", b)
}

func decodeUint(b []byte) (interface{}, error) {
	switch len(b) {
	case 0:
		return uint64(0), nil
	case 1:
		return uint64(b[0]), nil
	case 2:
		return uint64(binary.BigEndian.Uint16(b)), nil
	case 4:
		return uint64(binary.BigEndian.Uint32(b)), nil
	case 8:
		return binary.BigEndian.Uint64(b), nil
	}
	return nil, fmt.Errorf("invalid length for uint buffer %v", b)
}

func decodeFloat64(b []byte) (interface{}, error) {
	raw := binary.BigEndian.Uint64(b)
	return math.Float64frombits(raw), nil
}

// According to https://www.rfc-editor.org/rfc/rfc5101#section-6.1.5
func decodeBool(b []byte) (interface{}, error) {
	if len(b) == 0 {
		return nil, errors.New("empty data")
	}
	if b[0] == 1 {
		return true, nil
	}
	if b[0] == 2 {
		return false, nil
	}
	return b[0], nil
}

func decodeHex(b []byte) (interface{}, error) {
	if len(b) == 0 {
		return "", nil
	}
	return "0x" + hex.EncodeToString(b), nil
}

func decodeString(b []byte) (interface{}, error) {
	return strings.TrimRight(string(b), "\x00"), nil
}

func decodeMAC(b []byte) (interface{}, error) {
	mac := net.HardwareAddr(b)
	return mac.String(), nil
}

func decodeIP(b []byte) (interface{}, error) {
	ip := net.IP(b)
	return ip.String(), nil
}

func decodeIPFromUint32(a uint32) (interface{}, error) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, a)
	return decodeIP(b)
}

func decodeL4Proto(b []byte) (interface{}, error) {
	return mapL4Proto(b[0]), nil
}

func mapL4Proto(id uint8) string {
	name, found := l4ProtoMapping[id]
	if found {
		return name
	}
	return strconv.FormatUint(uint64(id), 10)
}

func decodeIPv4Options(b []byte) (interface{}, error) {
	flags := binary.BigEndian.Uint32(b)

	var result []string
	for i := 0; i < 32; i++ {
		name := ipv4OptionMapping[i]
		if name == "" {
			name = fmt.Sprintf("UA%d", i)
		}
		if (flags>>i)&0x01 != 0 {
			result = append(result, name)
		}
	}

	return strings.Join(result, ","), nil
}

func decodeTCPFlags(b []byte) (interface{}, error) {
	if len(b) == 0 {
		return "", nil
	}

	if len(b) == 1 {
		return mapTCPFlags(b[0]), nil
	}

	// IPFIX has more flags
	results := make([]string, 0, 8)
	for i := 7; i >= 0; i-- {
		if (b[0]>>i)&0x01 != 0 {
			// Currently all flags are reserved so denote the bit set
			results = append(results, "*")
		} else {
			results = append(results, ".")
		}
	}

	//nolint:gosec // False positive (b[1] is not out of range - it is ensured by above checks)
	return strings.Join(results, "") + mapTCPFlags(b[1]), nil
}

func mapTCPFlags(flags uint8) string {
	flagMapping := []string{
		"F", // FIN
		"S", // SYN
		"R", // RST
		"P", // PSH
		"A", // ACK
		"U", // URG
		"E", // ECE
		"C", // CWR
	}

	result := make([]string, 0, 8)

	for i := 7; i >= 0; i-- {
		if (flags>>i)&0x01 != 0 {
			result = append(result, flagMapping[i])
		} else {
			result = append(result, ".")
		}
	}

	return strings.Join(result, "")
}

func decodeFragmentFlags(b []byte) (interface{}, error) {
	flagMapping := []string{
		"*", // do not care
		"*", // do not care
		"*", // do not care
		"*", // do not care
		"*", // do not care
		"M", // MF -- more fragments
		"D", // DF -- don't fragment
		"R", // RS -- reserved
	}

	flags := b[0]
	result := make([]string, 0, 8)
	for i := 7; i >= 0; i-- {
		if (flags>>i)&0x01 != 0 {
			result = append(result, flagMapping[i])
		} else {
			result = append(result, ".")
		}
	}

	return strings.Join(result, ""), nil
}

func decodeSampleAlgo(b []byte) (interface{}, error) {
	switch b[0] {
	case 1:
		return "deterministic", nil
	case 2:
		return "random", nil
	}
	return strconv.FormatUint(uint64(b[0]), 10), nil
}

func decodeEngineType(b []byte) (interface{}, error) {
	return mapEngineType(b[0]), nil
}

func mapEngineType(b uint8) string {
	switch b {
	case 0:
		return "RP"
	case 1:
		return "VIP/linecard"
	case 2:
		return "PFC/DFC"
	}
	return strconv.FormatUint(uint64(b), 10)
}

func decodeMPLSType(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "unknown", nil
	case 1:
		return "TE-MIDPT", nil
	case 2:
		return "Pseudowire", nil
	case 3:
		return "VPN", nil
	case 4:
		return "BGP", nil
	case 5:
		return "LDP", nil
	case 6:
		return "Path computation element", nil
	case 7:
		return "OSPFv2", nil
	case 8:
		return "OSPFv3", nil
	case 9:
		return "IS-IS", nil
	case 10:
		return "BGP segment routing Prefix-SID", nil
	}
	return strconv.FormatUint(uint64(b[0]), 10), nil
}

func decodeIPVersion(b []byte) (interface{}, error) {
	switch b[0] {
	case 4:
		return "IPv4", nil
	case 6:
		return "IPv6", nil
	}
	return strconv.FormatUint(uint64(b[0]), 10), nil
}

func decodePacketIPVersion(v uint8) string {
	switch v {
	case 4:
		return "IPv4"
	case 6:
		return "IPv6"
	default:
		return "unknown"
	}
}

func decodeDirection(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "ingress", nil
	case 1:
		return "egress", nil
	}
	return strconv.FormatUint(uint64(b[0]), 10), nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-forwarding-status
func decodeFwdStatus(b []byte) (interface{}, error) {
	switch b[0] >> 6 {
	case 0:
		return "unknown", nil
	case 1:
		return "forwarded", nil
	case 2:
		return "dropped", nil
	case 3:
		return "consumed", nil
	}
	return "invalid", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-forwarding-status
func decodeFwdReason(b []byte) (interface{}, error) {
	switch b[0] {
	// unknown
	case 0:
		return "unknown", nil
	// forwarded
	case 64:
		return "unknown", nil
	case 65:
		return "fragmented", nil
	case 66:
		return "not fragmented", nil
	// dropped
	case 128:
		return "unknown", nil
	case 129:
		return "ACL deny", nil
	case 130:
		return "ACL drop", nil
	case 131:
		return "unroutable", nil
	case 132:
		return "adjacency", nil
	case 133:
		return "fragmentation and DF set", nil
	case 134:
		return "bad header checksum", nil
	case 135:
		return "bad total length", nil
	case 136:
		return "bad header length", nil
	case 137:
		return "bad TTL", nil
	case 138:
		return "policer", nil
	case 139:
		return "WRED", nil
	case 140:
		return "RPF", nil
	case 141:
		return "for us", nil
	case 142:
		return "bad output interface", nil
	case 143:
		return "hardware", nil
	// consumed
	case 192:
		return "unknown", nil
	case 193:
		return "terminate punt adjacency", nil
	case 194:
		return "terminate incomplete adjacency", nil
	case 195:
		return "terminate for us", nil
	case 14:
		return "", nil
	}
	return "invalid", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-firewall-event
func decodeFWEvent(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "ignore", nil
	case 1:
		return "flow created", nil
	case 2:
		return "flow deleted", nil
	case 3:
		return "flow denied", nil
	case 4:
		return "flow alert", nil
	case 5:
		return "flow update", nil
	}
	return strconv.FormatUint(uint64(b[0]), 10), nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-flow-end-reason
func decodeFlowEndReason(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "reserved", nil
	case 1:
		return "idle timeout", nil
	case 2:
		return "active timeout", nil
	case 3:
		return "end of flow", nil
	case 4:
		return "forced end", nil
	case 5:
		return "lack of resources", nil
	}
	return "unassigned", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-biflow-direction
func decodeBiflowDirection(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "arbitrary", nil
	case 1:
		return "initiator", nil
	case 2:
		return "reverse initiator", nil
	case 3:
		return "perimeter", nil
	}
	return "unassigned", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-observation-point-type
func decodeOpsPointType(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "invalid", nil
	case 1:
		return "physical port", nil
	case 2:
		return "port channel", nil
	case 3:
		return "vlan", nil
	}
	return "unassigned", nil
}

func decodeAnonStabilityClass(b []byte) (interface{}, error) {
	switch b[1] & 0x03 {
	case 1:
		return "session", nil
	case 2:
		return "exporter-collector", nil
	case 3:
		return "stable", nil
	}
	return "undefined", nil
}

func decodeAnonFlags(b []byte) (interface{}, error) {
	var result []string
	if b[0]&(1<<2) != 0 {
		result = append(result, "PmA")
	}

	if b[0]&(1<<3) != 0 {
		result = append(result, "LOR")
	}

	return strings.Join(result, ","), nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-anonymization-technique
func decodeAnonTechnique(b []byte) (interface{}, error) {
	tech := binary.BigEndian.Uint16(b)
	switch tech {
	case 0:
		return "undefined", nil
	case 1:
		return "none", nil
	case 2:
		return "precision degradation", nil
	case 3:
		return "binning", nil
	case 4:
		return "enumeration", nil
	case 5:
		return "permutation", nil
	case 6:
		return "structure permutation", nil
	case 7:
		return "reverse truncation", nil
	case 8:
		return "noise", nil
	case 9:
		return "offset", nil
	}
	return "unassigned", nil
}

func decodeTechnology(b []byte) (interface{}, error) {
	switch string(b) {
	case "yes", "y", "1":
		return "yes", nil
	case "no", "n", "2":
		return "no", nil
	case "unassigned", "u", "0":
		return "unassigned", nil
	}
	switch b[0] {
	case 0:
		return "unassigned", nil
	case 1:
		return "yes", nil
	case 2:
		return "no", nil
	}
	return "undefined", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-nat-type
func decodeIPNatType(b []byte) (interface{}, error) {
	tech := binary.BigEndian.Uint16(b)
	switch tech {
	case 0:
		return "unknown", nil
	case 1:
		return "NAT44", nil
	case 2:
		return "NAT64", nil
	case 3:
		return "NAT46", nil
	case 4:
		return "IPv4 no NAT", nil
	case 5:
		return "NAT66", nil
	case 6:
		return "IPv6 no NAT", nil
	}
	return "unassigned", nil
}

// https://www.iana.org/assignments/psamp-parameters/psamp-parameters.xhtml
func decodeSelectorAlgorithm(b []byte) (interface{}, error) {
	tech := binary.BigEndian.Uint16(b)
	switch tech {
	case 0:
		return "reserved", nil
	case 1:
		return "systematic count-based sampling", nil
	case 2:
		return "systematic time-based sampling", nil
	case 3:
		return "random n-out-of-N sampling", nil
	case 4:
		return "uniform probabilistic sampling", nil
	case 5:
		return "property match filtering", nil
	case 6:
		return "hash based filtering using BOB", nil
	case 7:
		return "hash based filtering using IPSX", nil
	case 8:
		return "hash based filtering using CRC", nil
	case 9:
		return "flow-state dependent", nil
	}
	return "unassigned", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-value-distribution-method
func decodeValueDistMethod(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "unspecified", nil
	case 1:
		return "start interval", nil
	case 2:
		return "end interval", nil
	case 3:
		return "mid interval", nil
	case 4:
		return "simple uniform distribution", nil
	case 5:
		return "proportional uniform distribution", nil
	case 6:
		return "simulated process", nil
	case 7:
		return "direct", nil
	}
	return "unassigned", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-data-link-frame-type
func decodeDataLinkFrameType(b []byte) (interface{}, error) {
	switch binary.BigEndian.Uint16(b) {
	case 0x0001:
		return "IEEE802.3 ethernet", nil
	case 0x0002:
		return "IEEE802.11 MAC", nil
	}
	return "unassigned", nil
}

// https://www.iana.org/assignments/ipfix/ipfix.xhtml#ipfix-mib-capture-time-semantics
func decodeCaptureTimeSemantics(b []byte) (interface{}, error) {
	switch b[0] {
	case 0:
		return "undefined", nil
	case 1:
		return "begin", nil
	case 2:
		return "end", nil
	case 3:
		return "export", nil
	case 4:
		return "average", nil
	}
	return "unassigned", nil
}

func decodeSflowIPVersion(v uint32) string {
	switch v {
	case 0:
		return "unknown"
	case 1:
		return "IPv4"
	case 2:
		return "IPv6"
	}
	return strconv.FormatUint(uint64(v), 10)
}

func decodeSflowSourceInterface(t uint32) string {
	switch t {
	case 0:
		return "in_snmp"
	case 1:
		return "in_vlan_id"
	case 2:
		return "in_phy_interface"
	}
	return ""
}

func decodeSflowHeaderProtocol(t uint32) string {
	switch t {
	case 1:
		return "ETHERNET-ISO8023"
	case 2:
		return "ISO88024-TOKENBUS"
	case 3:
		return "ISO88025-TOKENRING"
	case 4:
		return "FDDI"
	case 5:
		return "FRAME-RELAY"
	case 6:
		return "X25"
	case 7:
		return "PPP"
	case 8:
		return "SMDS"
	case 9:
		return "AAL5"
	case 10:
		return "AAL5-IP"
	case 11:
		return "IPv4"
	case 12:
		return "IPv6"
	case 13:
		return "MPLS"
	}
	return "unassigned"
}

func decodeByteFunc(idx int) decoderFunc {
	return func(b []byte) (interface{}, error) { return b[idx], nil }
}
