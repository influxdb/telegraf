package sflow

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestIPv4SW(t *testing.T) {
	str := `00000005` + // version
		`00000001` + // address type
		`c0a80102` + // ip address
		`00000010` + // sub agent id
		`0000f3d4` + // sequence number
		`0bfa047f` + // uptime
		`00000002` + // sample count
		`00000001` + // sample type
		`000000d0` + // sample data length
		`0001210a` + // sequence number
		`000001fe` + // source id 00 = source id type, 0001fe = source id index
		`00000400` + // sampling rate.. apparently this should be input if index????
		`04842400` + // sample pool
		`00000000` + // drops
		`000001fe` + // input if index
		`00000200` + // output if index
		`00000002` + // flow records count
		`00000001` + // FlowFormat
		`00000090` + // flow length
		`00000001` + // header protocol
		`0000010b` + // Frame length
		`00000004` + // stripped octets
		`00000080` + // header length
		`000c2936d3d6` + // dest mac
		`94c691aa9760` + // source mac
		`0800` + // etype code: ipv4
		`4500` + // dscp + ecn
		`00f9` + // total length
		`f190` + // identification
		`4000` + // fragment offset + flags
		`40` + // ttl
		`11` + // protocol
		`b4f5` + // header checksum
		`c0a80913` + // source ip
		`c0a8090a` + // dest ip
		`00a1` + // source port
		`ba05` + // dest port
		`00e5` + // udp length
		// rest of header/flowSample we ignore
		`641f3081da02010104066d6f746f6770a281cc02047b46462e0201000201003081bd3012060d2b06010201190501010281dc710201003013060d2b060` +
		`10201190501010281e66802025acc3012060d2b0601020119050101` +
		// next flow record - ignored
		`000003e90000001000000009000000000000000900000000` +
		// next sample
		`00000001000000d00000e3cc000002100000400048eb74000000000000000210000002000000000200000001000000900000000100000097000000040` +
		`0000080000c2936d3d6fcecda44008f81000009080045000081186440003f119098c0a80815c0a8090a9a690202006d23083c33303e41707220313120` +
		`30393a33333a3031206b6e6f64653120736e6d70645b313039385d3a20436f6e6e656374696f6e2066726f6d205544503a205b3139322e3136382e392` +
		`e31305d3a34393233362d000003e90000001000000009000000000000000900000000`
	packet, err := hex.DecodeString(str)
	require.NoError(t, err)

	actual := make([]telegraf.Metric, 0)
	dc := newDecoder()
	dc.onPacket(func(p *v5Format) {
		metrics := makeMetrics(p)
		actual = append(actual, metrics...)
	})
	buf := bytes.NewReader(packet)
	err = dc.decode(buf)
	require.NoError(t, err)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "192.168.1.2",
				"dst_ip":           "192.168.9.10",
				"dst_mac":          "00:0c:29:36:d3:d6",
				"dst_port":         "47621",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "510",
				"output_ifindex":   "512",
				"sample_direction": "ingress",
				"source_id_index":  "510",
				"source_id_type":   "0",
				"src_ip":           "192.168.9.19",
				"src_mac":          "94:c6:91:aa:97:60",
				"src_port":         "161",
			},
			map[string]interface{}{
				"bytes":              uint64(0x042c00),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x010b),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0xf9),
				"ip_ttl":             uint64(0x40),
				"sampling_rate":      uint64(0x0400),
				"udp_length":         uint64(0xe5),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "192.168.1.2",
				"dst_ip":           "192.168.9.10",
				"dst_mac":          "00:0c:29:36:d3:d6",
				"dst_port":         "514",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "528",
				"output_ifindex":   "512",
				"sample_direction": "ingress",
				"source_id_index":  "528",
				"source_id_type":   "0",
				"src_ip":           "192.168.8.21",
				"src_mac":          "fc:ec:da:44:00:8f",
				"src_port":         "39529",
			},
			map[string]interface{}{
				"bytes":              uint64(0x25c000),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x97),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x81),
				"ip_ttl":             uint64(0x3f),
				"sampling_rate":      uint64(0x4000),
				"udp_length":         uint64(0x6d),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func BenchmarkDecodeIPv4SW(b *testing.B) {
	packet, err := hex.DecodeString(
		"0000000500000001c0a80102000000100000f3d40bfa047f0000000200000001000000d00001210a000001fe000004000484240000000000000001fe0" +
			"0000200000000020000000100000090000000010000010b0000000400000080000c2936d3d694c691aa97600800450000f9f19040004011b4f5c0a80" +
			"913c0a8090a00a1ba0500e5641f3081da02010104066d6f746f6770a281cc02047b46462e0201000201003081bd3012060d2b0601020119050101028" +
			"1dc710201003013060d2b06010201190501010281e66802025acc3012060d2b0601020119050101000003e9000000100000000900000000000000090" +
			"000000000000001000000d00000e3cc000002100000400048eb740000000000000002100000020000000002000000010000009000000001000000970" +
			"000000400000080000c2936d3d6fcecda44008f81000009080045000081186440003f119098c0a80815c0a8090a9a690202006d23083c33303e41707" +
			"22031312030393a33333a3031206b6e6f64653120736e6d70645b313039385d3a20436f6e6e656374696f6e2066726f6d205544503a205b3139322e3" +
			"136382e392e31305d3a34393233362d000003e90000001000000009000000000000000900000000",
	)
	require.NoError(b, err)

	dc := newDecoder()
	require.NoError(b, err)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err = dc.decodeOnePacket(bytes.NewBuffer(packet))
		if err != nil {
			panic(err)
		}
	}
}

func TestExpandFlow(t *testing.T) {
	packet, err := hex.DecodeString(
		"00000005000000010a00015000000000000f58998ae119780000000300000003000000c4000b62a90000000000100c840000040024fb7e1e000000000" +
			"0000000001017840000000000100c8400000001000000010000009000000001000005bc0000000400000080001b17000130001201f58d44810023710" +
			"800450205a6305440007e06ee92ac100016d94d52f505997e701fa1e17aff62574a50100200355f000000ffff00000b004175746f72697a7a6174610" +
			"400008040ffff000400008040050031303030320500313030302004000000000868a200000000000000000860a200000000000000000003000000c40" +
			"003cecf000000000010170400004000a168ac1c000000000000000000101784000000000010170400000001000000010000009000000001000005f20" +
			"0000004000000800024e8324338d4ae52aa0b54810020060800450005dc5420400080061397c0a8060cc0a806080050efcfbb25bad9a21c839a50100" +
			"0fff54000008a55f70975a0ff88b05735597ae274bd81fcba17e6e9206b8ea0fb07d05fc27dad06cfe3fdba5d2fc4d057b0add711e596cbe5e9b4bbe" +
			"8be59cd77537b7a89f7414a628b736d00000003000000c0000c547a0000000000100c04000004005bc3c3b5000000000000000000101784000000000" +
			"0100c0400000001000000010000008c000000010000007e000000040000007a001b17000130001201f58d448100237108004500006824ea4000ff32c" +
			"326d94d5105501018f02e88d003000001dd39b1d025d1c68689583b2ab21522d5b5a959642243804f6d51e63323091cc04544285433eb3f6b29e1046" +
			"a6a2fa7806319d62041d8fa4bd25b7cd85b8db54202054a077ac11de84acbe37a550004",
	)
	require.NoError(t, err)

	dc := newDecoder()
	p, err := dc.decodeOnePacket(bytes.NewBuffer(packet))
	require.NoError(t, err)
	actual := makeMetrics(p)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "217.77.82.245",
				"dst_mac":          "00:1b:17:00:01:30",
				"dst_port":         "32368",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1054596",
				"output_ifindex":   "1051780",
				"sample_direction": "egress",
				"source_id_index":  "1051780",
				"source_id_type":   "0",
				"src_ip":           "172.16.0.22",
				"src_mac":          "00:12:01:f5:8d:44",
				"src_port":         "1433",
			},
			map[string]interface{}{
				"bytes":              uint64(0x16f000),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x05bc),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x05a6),
				"ip_ttl":             uint64(0x7e),
				"sampling_rate":      uint64(0x0400),
				"tcp_header_length":  uint64(0x14),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0x0200),
				"ip_dscp":            "0",
				"ip_ecn":             "2",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "192.168.6.8",
				"dst_mac":          "00:24:e8:32:43:38",
				"dst_port":         "61391",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1054596",
				"output_ifindex":   "1054468",
				"sample_direction": "egress",
				"source_id_index":  "1054468",
				"source_id_type":   "0",
				"src_ip":           "192.168.6.12",
				"src_mac":          "d4:ae:52:aa:0b:54",
				"src_port":         "80",
			},
			map[string]interface{}{
				"bytes":              uint64(0x017c8000),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x05f2),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x05dc),
				"ip_ttl":             uint64(0x80),
				"sampling_rate":      uint64(0x4000),
				"tcp_header_length":  uint64(0x14),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0xff),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "80.16.24.240",
				"dst_mac":          "00:1b:17:00:01:30",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1054596",
				"output_ifindex":   "1051652",
				"sample_direction": "egress",
				"source_id_index":  "1051652",
				"source_id_type":   "0",
				"src_ip":           "217.77.81.5",
				"src_mac":          "00:12:01:f5:8d:44",
			},
			map[string]interface{}{
				"bytes":              uint64(0x01f800),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x7e),
				"header_length":      uint64(0x7a),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x68),
				"ip_ttl":             uint64(0xff),
				"sampling_rate":      uint64(0x0400),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func TestIPv4SWRT(t *testing.T) {
	packet, err := hex.DecodeString(
		"000000050000000189dd4f010000000000003d4f21151ad40000000600000001000000bc354b97090000020c000013b175792bea000000000000028f0" +
			"000020c0000000300000001000000640000000100000058000000040000005408b2587a57624c16fc0b61a5080045000046c3e440003a1118a0052aa" +
			"da7569e5ab367a6e35b0032d7bbf1f2fb2eb2490a97f87abc31e135834be367000002590000ffffffffffffffff02add830d51e0aec14cf000003e90" +
			"000001000000000000000000000000000000000000003ea0000001000000001c342e32a000000160000000b00000001000000a88b8ffb57000002a20" +
			"00013b12e344fd800000000000002a20000028f0000000300000001000000500000000100000042000000040000003e4c16fc0b6202c03e0fdecafe0" +
			"80045000030108000007d11fe45575185a718693996f0570e8c001c20614ad602003fd6d4afa6a6d18207324000271169b00000000003e9000000100" +
			"0000000000000000000000000000000000003ea000000100000000189dd4f210000000f0000001800000001000000e8354b970a0000020c000013b17" +
			"5793f9b000000000000028f0000020c00000003000000010000009000000001000001a500000004000000800231466d0b2c4c16fc0b61a5080045000" +
			"193198f40003a114b75052aae1f5f94c778678ef24d017f50ea7622287c30799e1f7d45932d01ca92c46d930000927c0000ffffffffffffffff02ad0" +
			"eea6498953d1c7ebb6dbdf0525c80e1a9a62bacfea92f69b7336c2f2f60eba0593509e14eef167eb37449f05ad70b8241c1a46d000003e9000000100" +
			"0000000000000000000000000000000000003ea0000001000000001c342e1fd000000160000001000000001000000e8354b970b0000020c000013b17" +
			"579534c000000000000028f0000020c00000003000000010000009000000001000000b500000004000000800231466d0b2c4c16fc0b61a5080045000" +
			"0a327c240003606fd67b93c706a021ff365045fe8a0976d624df8207083501800edb31b0000485454502f312e3120323030204f4b0d0a53657276657" +
			"23a2050726f746f636f6c20485454500d0a436f6e74656e742d4c656e6774683a20313430340d0a436f6e6e656374696f6e3a20000003e9000000100" +
			"0000000000000000000000000000000000003ea0000001000000001c342e1fd000000170000001000000001000000e8354b970c0000020c000013b17" +
			"57966fd000000000000028f0000020c000000030000000100000090000000010000018e00000004000000800231466d0b2c4c16fc0b61a5080045000" +
			"17c7d2c40003a116963052abd8d021c940e67e7e0d501682342dbe7936bd47ef487dee5591ec1b24d83622e000072250000ffffffffffffffff02ad0" +
			"039d8ba86a90017071d76b177de4d8c4e23bcaaaf4d795f77b032f959e0fb70234d4c28922d4e08dd3330c66e34bff51cc8ade5000003e9000000100" +
			"0000000000000000000000000000000000003ea0000001000000001c342e1fd000000160000001000000001000000e80d6146ac000002a1000013b17" +
			"880b49d00000000000002a10000028f00000003000000010000009000000001000005ee00000004000000804c16fc0b6201d8b122766a2c080045000" +
			"5dc04574000770623a11fcd80a218691d4cf2fe01bbd4f47482065fd63a5010fabd7987000052a20002c8c43ea91ca1eaa115663f5218a37fbb409df" +
			"bbedff54731ef41199b35535905ac2366a05a803146ced544abf45597f3714327d59f99e30c899c39fc5a4b67d12087bf8db2bc000003e9000000100" +
			"0000000000000000000000000000000000003ea000000100000000189dd4f210000001000000018",
	)
	require.NoError(t, err)

	dc := newDecoder()
	p, err := dc.decodeOnePacket(bytes.NewBuffer(packet))
	require.NoError(t, err)
	actual := makeMetrics(p)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "137.221.79.1",
				"dst_ip":           "86.158.90.179",
				"dst_mac":          "08:b2:58:7a:57:62",
				"dst_port":         "58203",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "655",
				"output_ifindex":   "524",
				"sample_direction": "egress",
				"source_id_index":  "524",
				"source_id_type":   "0",
				"src_ip":           "5.42.173.167",
				"src_mac":          "4c:16:fc:0b:61:a5",
				"src_port":         "26534",
			},
			map[string]interface{}{
				"bytes":              uint64(0x06c4d8),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x58),
				"header_length":      uint64(0x54),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x46),
				"ip_ttl":             uint64(0x3a),
				"sampling_rate":      uint64(0x13b1),
				"udp_length":         uint64(0x32),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "137.221.79.1",
				"dst_ip":           "24.105.57.150",
				"dst_mac":          "4c:16:fc:0b:62:02",
				"dst_port":         "3724",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "674",
				"output_ifindex":   "655",
				"sample_direction": "ingress",
				"source_id_index":  "674",
				"source_id_type":   "0",
				"src_ip":           "87.81.133.167",
				"src_mac":          "c0:3e:0f:de:ca:fe",
				"src_port":         "61527",
			},
			map[string]interface{}{
				"bytes":              uint64(0x0513a2),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x42),
				"header_length":      uint64(0x3e),
				"ip_flags":           uint64(0x00),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x30),
				"ip_ttl":             uint64(0x7d),
				"sampling_rate":      uint64(0x13b1),
				"udp_length":         uint64(0x1c),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "137.221.79.1",
				"dst_ip":           "95.148.199.120",
				"dst_mac":          "02:31:46:6d:0b:2c",
				"dst_port":         "62029",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "655",
				"output_ifindex":   "524",
				"sample_direction": "egress",
				"source_id_index":  "524",
				"source_id_type":   "0",
				"src_ip":           "5.42.174.31",
				"src_mac":          "4c:16:fc:0b:61:a5",
				"src_port":         "26510",
			},
			map[string]interface{}{
				"bytes":              uint64(0x206215),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x01a5),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x0193),
				"ip_ttl":             uint64(0x3a),
				"sampling_rate":      uint64(0x13b1),
				"udp_length":         uint64(0x017f),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "137.221.79.1",
				"dst_ip":           "2.31.243.101",
				"dst_mac":          "02:31:46:6d:0b:2c",
				"dst_port":         "59552",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "655",
				"output_ifindex":   "524",
				"sample_direction": "egress",
				"source_id_index":  "524",
				"source_id_type":   "0",
				"src_ip":           "185.60.112.106",
				"src_mac":          "4c:16:fc:0b:61:a5",
				"src_port":         "1119",
			},
			map[string]interface{}{
				"bytes":              uint64(0x0dec25),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0xb5),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0xa3),
				"ip_ttl":             uint64(0x36),
				"sampling_rate":      uint64(0x13b1),
				"tcp_header_length":  uint64(0x14),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0xed),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "137.221.79.1",
				"dst_ip":           "2.28.148.14",
				"dst_mac":          "02:31:46:6d:0b:2c",
				"dst_port":         "57557",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "655",
				"output_ifindex":   "524",
				"sample_direction": "egress",
				"source_id_index":  "524",
				"source_id_type":   "0",
				"src_ip":           "5.42.189.141",
				"src_mac":          "4c:16:fc:0b:61:a5",
				"src_port":         "26599",
			},
			map[string]interface{}{
				"bytes":              uint64(0x1e9d2e),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x018e),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x017c),
				"ip_ttl":             uint64(0x3a),
				"sampling_rate":      uint64(0x13b1),
				"udp_length":         uint64(0x0168),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "137.221.79.1",
				"dst_ip":           "24.105.29.76",
				"dst_mac":          "4c:16:fc:0b:62:01",
				"dst_port":         "443",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "673",
				"output_ifindex":   "655",
				"sample_direction": "ingress",
				"source_id_index":  "673",
				"source_id_type":   "0",
				"src_ip":           "31.205.128.162",
				"src_mac":          "d8:b1:22:76:6a:2c",
				"src_port":         "62206",
			},
			map[string]interface{}{
				"bytes":              uint64(0x74c38e),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x05ee),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x05dc),
				"ip_ttl":             uint64(0x77),
				"sampling_rate":      uint64(0x13b1),
				"tcp_header_length":  uint64(0x14),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0xfabd),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func TestIPv6SW(t *testing.T) {
	packet, err := hex.DecodeString(
		"00000005000000010ae0648100000002000093d824ac82340000000100000001000000d000019f94000001010000100019f9400000000000000001010" +
			"0000000000000020000000100000090000000010000058c00000008000000800008e3fffc10d4f4be04612486dd60000000054e113a2607f8b040020" +
			"0140000000000000008262000edc000e804a25e30c581af36fa01bbfa6f054e249810b584bcbf12926c2e29a779c26c72db483e8191524fe2288bfda" +
			"ceaf9d2e724d04305706efcfdef70db86873bbacf29698affe4e7d6faa21d302f9b4b023291a05a000003e90000001000000001000000000000000100000000",
	)
	require.NoError(t, err)

	dc := newDecoder()
	p, err := dc.decodeOnePacket(bytes.NewBuffer(packet))
	require.NoError(t, err)
	actual := makeMetrics(p)

	expected := []telegraf.Metric{

		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.224.100.129",
				"dst_ip":           "2620:ed:c000:e804:a25e:30c5:81af:36fa",
				"dst_mac":          "00:08:e3:ff:fc:10",
				"dst_port":         "64111",
				"ether_type":       "IPv6",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "257",
				"output_ifindex":   "0",
				"sample_direction": "ingress",
				"source_id_index":  "257",
				"source_id_type":   "0",
				"src_ip":           "2607:f8b0:4002:14::8",
				"src_mac":          "d4:f4:be:04:61:24",
				"src_port":         "443",
			},
			map[string]interface{}{
				"bytes":          uint64(0x58c000),
				"drops":          uint64(0x00),
				"frame_length":   uint64(0x058c),
				"header_length":  uint64(0x80),
				"sampling_rate":  uint64(0x1000),
				"payload_length": uint64(0x054e),
				"udp_length":     uint64(0x054e),
				"ip_dscp":        "0",
				"ip_ecn":         "0",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func TestExpandFlowCounter(t *testing.T) {
	packet, err := hex.DecodeString(
		"00000005000000010a00015000000000000f58898ae0fa380000000700000004000000ec00006ece00000000001017840000000300000002000000340" +
			"000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000580" +
			"01017840000000600000002540be400000000010000000300007b8ebd37b97e61ff94860803e8e908ffb2b500000000000000000000000000018e7c3" +
			"1ee7ba4195f041874579ff021ba936300000000000000000000000100000007000000380011223344550003f8b15645e7e7d6960000002fe2fc02fc0" +
			"1edbf580000000000000000000000000000000001dcb9cf000000000000000000000004000000ec00006ece000000000010018400000003000000020" +
			"000003400000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010" +
			"0000058001001840000000600000002540be400000000010000000300000841131d1fd9f850bfb103617cb401e659890000000000000000000000000" +
			"0000bec1902e5da9212e3e96d7996e922513250000000000000000000000001000000070000003800112233445500005c260acbddb3000100000003e" +
			"2fc02fc01ee414f0000000000000000000000000000000001dccdd30000000000000000000000030000008400004606000000000010030400004000a" +
			"d9dc19b0000000000000000001017840000000000100304000000010000000100000050000000010000004400000004000000400012815116c400151" +
			"7cf426d8100200608004500002895da40008006d74bc0a8060ac0a8064f04ef04aab1797122cf7eaf4f5010ffff77270000000000000000000000030" +
			"00000b0001bd698000000000010148400000400700b180f000000000000000000101504000000000010148400000001000000010000007c000000010" +
			"000006f000000040000006b001b17000131f0f755b9afc081000439080045000059045340005206920c1f0d4703d94d52e201bbf14977d1e9f15498a" +
			"f36801800417f1100000101080afdf3c70400e043871503010020ff268cfe2e2fd5fffe1d3d704a91d57b895f174c4b4428c66679d80a307294303f0" +
			"0000003000000c40003ceca000000000010170400004000a166aa7a00000000000000000010178400000000001017040000000100000001000000900" +
			"0000001000005f200000004000000800024e8369e2bd4ae52aa0b54810020060800450005dc4c71400080061b45c0a8060cc0a806090050f855692a7" +
			"a94a1154ae1801001046b6a00000101080a6869a48d151016d046a84a7aa1c6743fa05179f7ecbd4e567150cb6f2077ff89480ae730637d26d2237c0" +
			"8548806f672c7476eb1b5a447b42cb9ce405994d152fa3e000000030000008c001bd699000000000010148400000400700b180f00000000000000000" +
			"01015040000000000101484000000010000000100000058000000010000004a0000000400000046001b17000131f0f755b9afc081000439080045000" +
			"0340ce040003a06bea5c1ce8793d94d528f00504c3b08b18f275b83d5df8010054586ad00000101050a5b83d5de5b83d5df11d800000003000000c40" +
			"0004e07000000000010028400004000c7ec97f2000000000000000000100784000000000010028400000001000000010000009000000001000005f20" +
			"00000040000008000005e0001ff005056800dd18100000a0800450005dc5a42400040066ef70a000ac8c0a8967201bbe17c81597908caf8a05f50100" +
			"10328610000f172263da0ba5d6223c079b8238bc841256bf17c4ffb08ad11c4fbff6f87ae1624a6b057b8baa9342114e5f5b46179083020cb560c4e9" +
			"eadcec6dfd83e102ddbc27024803eb5",
	)
	require.NoError(t, err)

	dc := newDecoder()
	p, err := dc.decodeOnePacket(bytes.NewBuffer(packet))
	require.NoError(t, err)
	actual := makeMetrics(p)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "192.168.6.79",
				"dst_mac":          "00:12:81:51:16:c4",
				"dst_port":         "1194",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1054596",
				"output_ifindex":   "1049348",
				"sample_direction": "egress",
				"source_id_index":  "1049348",
				"source_id_type":   "0",
				"src_ip":           "192.168.6.10",
				"src_mac":          "00:15:17:cf:42:6d",
				"src_port":         "1263",
			},
			map[string]interface{}{
				"bytes":              uint64(0x110000),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x44),
				"header_length":      uint64(0x40),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x28),
				"ip_ttl":             uint64(0x80),
				"sampling_rate":      uint64(0x4000),
				"tcp_header_length":  uint64(0x14),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0xffff),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "217.77.82.226",
				"dst_mac":          "00:1b:17:00:01:31",
				"dst_port":         "61769",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1053956",
				"output_ifindex":   "1053828",
				"sample_direction": "egress",
				"source_id_index":  "1053828",
				"source_id_type":   "0",
				"src_ip":           "31.13.71.3",
				"src_mac":          "f0:f7:55:b9:af:c0",
				"src_port":         "443",
			},
			map[string]interface{}{
				"bytes":              uint64(0x01bc00),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x6f),
				"header_length":      uint64(0x6b),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x59),
				"ip_ttl":             uint64(0x52),
				"sampling_rate":      uint64(0x0400),
				"tcp_header_length":  uint64(0x20),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0x41),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "192.168.6.9",
				"dst_mac":          "00:24:e8:36:9e:2b",
				"dst_port":         "63573",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1054596",
				"output_ifindex":   "1054468",
				"sample_direction": "egress",
				"source_id_index":  "1054468",
				"source_id_type":   "0",
				"src_ip":           "192.168.6.12",
				"src_mac":          "d4:ae:52:aa:0b:54",
				"src_port":         "80",
			},
			map[string]interface{}{
				"bytes":              uint64(0x017c8000),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x05f2),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x05dc),
				"ip_ttl":             uint64(0x80),
				"sampling_rate":      uint64(0x4000),
				"tcp_header_length":  uint64(0x20),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0x0104),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "217.77.82.143",
				"dst_mac":          "00:1b:17:00:01:31",
				"dst_port":         "19515",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1053956",
				"output_ifindex":   "1053828",
				"sample_direction": "egress",
				"source_id_index":  "1053828",
				"source_id_type":   "0",
				"src_ip":           "193.206.135.147",
				"src_mac":          "f0:f7:55:b9:af:c0",
				"src_port":         "80",
			},
			map[string]interface{}{
				"bytes":              uint64(0x012800),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x4a),
				"header_length":      uint64(0x46),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x34),
				"ip_ttl":             uint64(0x3a),
				"sampling_rate":      uint64(0x0400),
				"tcp_header_length":  uint64(0x20),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0x0545),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"sflow",
			map[string]string{
				"agent_address":    "10.0.1.80",
				"dst_ip":           "192.168.150.114",
				"dst_mac":          "00:00:5e:00:01:ff",
				"dst_port":         "57724",
				"ether_type":       "IPv4",
				"header_protocol":  "ETHERNET-ISO88023",
				"input_ifindex":    "1050500",
				"output_ifindex":   "1049220",
				"sample_direction": "egress",
				"source_id_index":  "1049220",
				"source_id_type":   "0",
				"src_ip":           "10.0.10.200",
				"src_mac":          "00:50:56:80:0d:d1",
				"src_port":         "443",
			},
			map[string]interface{}{
				"bytes":              uint64(0x017c8000),
				"drops":              uint64(0x00),
				"frame_length":       uint64(0x05f2),
				"header_length":      uint64(0x80),
				"ip_flags":           uint64(0x02),
				"ip_fragment_offset": uint64(0x00),
				"ip_total_length":    uint64(0x05dc),
				"ip_ttl":             uint64(0x40),
				"sampling_rate":      uint64(0x4000),
				"tcp_header_length":  uint64(0x14),
				"tcp_urgent_pointer": uint64(0x00),
				"tcp_window_size":    uint64(0x0103),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func TestFlowExpandCounter(t *testing.T) {
	packet, err := hex.DecodeString(
		"00000005000000010a000150000000000006d14d8ae0fe200000000200000004000000ac00006d15000000004b00ca000000000200000002000000340" +
			"000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000584" +
			"b00ca0000000001000000000000000000000001000000010000308ae33bb950eb92a8a3004d0bb406899571000000000000000000000000000012f7e" +
			"d9c9db8c24ed90604eaf0bd04636edb00000000000000000000000100000004000000ac00006d15000000004b0054000000000200000002000000340" +
			"000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000584" +
			"b00540000000001000000003b9aca000000000100000003000067ba8e64fd23fa65f26d0215ec4a0021086600000000000000000000000000002002c" +
			"3b21045c2378ad3001fb2f300061872000000000000000000000001",
	)
	require.NoError(t, err)

	dc := newDecoder()
	p, err := dc.decodeOnePacket(bytes.NewBuffer(packet))
	require.NoError(t, err)
	actual := makeMetrics(p)

	// we don't do anything with samples yet
	expected := make([]telegraf.Metric, 0)
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}
