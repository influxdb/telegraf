package sflow

import (
	"encoding/hex"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestSFlow(t *testing.T) {
	sflow := &SFlow{
		ServiceAddress: "udp://127.0.0.1:0",
		Log:            testutil.Logger{},
	}
	err := sflow.Init()
	require.NoError(t, err)

	var acc testutil.Accumulator
	err = sflow.Start(&acc)
	require.NoError(t, err)
	defer sflow.Stop()

	client, err := net.Dial(sflow.address().Network(), sflow.address().String())
	require.NoError(t, err)

	packetBytes, err := hex.DecodeString(
		"0000000500000001c0a80102000000100000f3d40bfa047f0000000200000001000000d00001210a000001fe000004000484240000000000000001fe" +
			"00000200000000020000000100000090000000010000010b0000000400000080000c2936d3d694c691aa97600800450000f9f19040004011b4f5c0a" +
			"80913c0a8090a00a1ba0500e5641f3081da02010104066d6f746f6770a281cc02047b46462e0201000201003081bd3012060d2b0601020119050101" +
			"0281dc710201003013060d2b06010201190501010281e66802025acc3012060d2b0601020119050101000003e900000010000000090000000000000" +
			"0090000000000000001000000d00000e3cc000002100000400048eb7400000000000000021000000200000000020000000100000090000000010000" +
			"00970000000400000080000c2936d3d6fcecda44008f81000009080045000081186440003f119098c0a80815c0a8090a9a690202006d23083c33303" +
			"e4170722031312030393a33333a3031206b6e6f64653120736e6d70645b313039385d3a20436f6e6e656374696f6e2066726f6d205544503a205b31" +
			"39322e3136382e392e31305d3a34393233362d000003e90000001000000009000000000000000900000000",
	)
	require.NoError(t, err)
	_, err = client.Write(packetBytes)
	require.NoError(t, err)

	acc.Wait(2)

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
				"bytes":              uint64(273408),
				"drops":              uint64(0),
				"frame_length":       uint64(267),
				"header_length":      uint64(128),
				"ip_flags":           uint64(2),
				"ip_fragment_offset": uint64(0),
				"ip_total_length":    uint64(249),
				"ip_ttl":             uint64(64),
				"sampling_rate":      uint64(1024),
				"udp_length":         uint64(229),
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
				"bytes":              uint64(2473984),
				"drops":              uint64(0),
				"frame_length":       uint64(151),
				"header_length":      uint64(128),
				"ip_flags":           uint64(2),
				"ip_fragment_offset": uint64(0),
				"ip_total_length":    uint64(129),
				"ip_ttl":             uint64(63),
				"sampling_rate":      uint64(16384),
				"udp_length":         uint64(109),
				"ip_dscp":            "0",
				"ip_ecn":             "0",
			},
			time.Unix(0, 0),
		),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(),
		testutil.IgnoreTime())
}

func BenchmarkSFlow(b *testing.B) {
	sflow := &SFlow{
		ServiceAddress: "udp://127.0.0.1:0",
		Log:            testutil.Logger{},
	}
	err := sflow.Init()
	require.NoError(b, err)

	var acc testutil.Accumulator
	err = sflow.Start(&acc)
	require.NoError(b, err)
	defer sflow.Stop()

	client, err := net.Dial(sflow.address().Network(), sflow.address().String())
	require.NoError(b, err)

	packetBytes, err := hex.DecodeString(
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

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := client.Write(packetBytes)
		require.NoError(b, err)
		acc.Wait(2)
	}
}
