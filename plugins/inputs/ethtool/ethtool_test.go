//go:build linux

package ethtool

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

var (
	eth          *Ethtool
	interfaceMap map[string]*interfaceMock
)

type interfaceMock struct {
	name          string
	driverName    string
	namespaceName string
	stat          map[string]uint64
	loopBack      bool
	interfaceUp   bool
	cmdGet        map[string]uint64
}

type namespaceMock struct {
	namespaceName string
}

func (n *namespaceMock) name() string {
	return n.namespaceName
}

func (*namespaceMock) interfaces() ([]namespacedInterface, error) {
	return nil, errors.New("it is a test bug to invoke this function")
}

func (*namespaceMock) driverName(_ namespacedInterface) (string, error) {
	return "", errors.New("it is a test bug to invoke this function")
}

func (*namespaceMock) stats(_ namespacedInterface) (map[string]uint64, error) {
	return nil, errors.New("it is a test bug to invoke this function")
}

func (*namespaceMock) get(_ namespacedInterface) (map[string]uint64, error) {
	return nil, errors.New("it is a test bug to invoke this function")
}

type commandEthtoolMock struct {
	interfaceMap map[string]*interfaceMock
}

func (*commandEthtoolMock) init() error {
	// Not required for test mock
	return nil
}

func (c *commandEthtoolMock) driverName(intf namespacedInterface) (string, error) {
	i := c.interfaceMap[intf.Name]
	if i != nil {
		return i.driverName, nil
	}
	return "", errors.New("interface not found")
}

func (c *commandEthtoolMock) interfaces(includeNamespaces bool) ([]namespacedInterface, error) {
	namespaces := map[string]*namespaceMock{"": {namespaceName: ""}}

	interfaces := make([]namespacedInterface, 0)
	for k, v := range c.interfaceMap {
		if v.namespaceName != "" && !includeNamespaces {
			continue
		}

		var flag net.Flags
		// When interface is up
		if v.interfaceUp {
			flag |= net.FlagUp
		}
		// For loopback interface
		if v.loopBack {
			flag |= net.FlagLoopback
		}

		// Create a dummy interface
		iface := net.Interface{
			Index:        0,
			MTU:          1500,
			Name:         k,
			HardwareAddr: nil,
			Flags:        flag,
		}

		// Ensure there is a namespace if necessary
		if _, ok := namespaces[v.namespaceName]; !ok {
			namespaces[v.namespaceName] = &namespaceMock{
				namespaceName: v.namespaceName,
			}
		}

		interfaces = append(
			interfaces,
			namespacedInterface{
				Interface: iface,
				namespace: namespaces[v.namespaceName],
			},
		)
	}
	return interfaces, nil
}

func (c *commandEthtoolMock) stats(intf namespacedInterface) (map[string]uint64, error) {
	i := c.interfaceMap[intf.Name]
	if i != nil {
		return i.stat, nil
	}
	return nil, errors.New("interface not found")
}

func (c *commandEthtoolMock) get(intf namespacedInterface) (map[string]uint64, error) {
	i := c.interfaceMap[intf.Name]
	if i != nil {
		return i.cmdGet, nil
	}
	return nil, errors.New("interface not found")
}

func setup() {
	interfaceMap = make(map[string]*interfaceMock)

	eth1Stat := map[string]uint64{
		"interface_up":                   1,
		"port_rx_1024_to_15xx":           25167245,
		"port_rx_128_to_255":             1573526387,
		"port_rx_15xx_to_jumbo":          137819058,
		"port_rx_256_to_511":             772038107,
		"port_rx_512_to_1023":            78294457,
		"port_rx_64":                     8798065,
		"port_rx_65_to_127":              450348015,
		"port_rx_bad":                    0,
		"port_rx_bad_bytes":              0,
		"port_rx_bad_gtjumbo":            0,
		"port_rx_broadcast":              6428250,
		"port_rx_bytes":                  893460472634,
		"port_rx_control":                0,
		"port_rx_dp_di_dropped_packets":  2772680304,
		"port_rx_dp_hlb_fetch":           0,
		"port_rx_dp_hlb_wait":            0,
		"port_rx_dp_q_disabled_packets":  0,
		"port_rx_dp_streaming_packets":   0,
		"port_rx_good":                   3045991334,
		"port_rx_good_bytes":             893460472927,
		"port_rx_gtjumbo":                0,
		"port_rx_lt64":                   0,
		"port_rx_multicast":              1639566045,
		"port_rx_nodesc_drops":           0,
		"port_rx_overflow":               0,
		"port_rx_packets":                3045991334,
		"port_rx_pause":                  0,
		"port_rx_pm_discard_bb_overflow": 0,
		"port_rx_pm_discard_mapping":     0,
		"port_rx_pm_discard_qbb":         0,
		"port_rx_pm_discard_vfifo_full":  0,
		"port_rx_pm_trunc_bb_overflow":   0,
		"port_rx_pm_trunc_qbb":           0,
		"port_rx_pm_trunc_vfifo_full":    0,
		"port_rx_unicast":                1399997040,
		"port_tx_1024_to_15xx":           236,
		"port_tx_128_to_255":             275090219,
		"port_tx_15xx_to_jumbo":          926,
		"port_tx_256_to_511":             48567221,
		"port_tx_512_to_1023":            5142016,
		"port_tx_64":                     113903973,
		"port_tx_65_to_127":              161935699,
		"port_tx_broadcast":              8,
		"port_tx_bytes":                  94357131016,
		"port_tx_control":                0,
		"port_tx_lt64":                   0,
		"port_tx_multicast":              325891647,
		"port_tx_packets":                604640290,
		"port_tx_pause":                  0,
		"port_tx_unicast":                278748635,
		"ptp_bad_syncs":                  1,
		"ptp_fast_syncs":                 1,
		"ptp_filter_matches":             0,
		"ptp_good_syncs":                 136151,
		"ptp_invalid_sync_windows":       0,
		"ptp_no_time_syncs":              1,
		"ptp_non_filter_matches":         0,
		"ptp_oversize_sync_windows":      53,
		"ptp_rx_no_timestamp":            0,
		"ptp_rx_timestamp_packets":       0,
		"ptp_sync_timeouts":              1,
		"ptp_timestamp_packets":          0,
		"ptp_tx_timestamp_packets":       0,
		"ptp_undersize_sync_windows":     3,
		"rx-0.rx_packets":                55659234,
		"rx-1.rx_packets":                87880538,
		"rx-2.rx_packets":                26746234,
		"rx-3.rx_packets":                103026471,
		"rx-4.rx_packets":                0,
		"rx_eth_crc_err":                 0,
		"rx_frm_trunc":                   0,
		"rx_inner_ip_hdr_chksum_err":     0,
		"rx_inner_tcp_udp_chksum_err":    0,
		"rx_ip_hdr_chksum_err":           0,
		"rx_mcast_mismatch":              0,
		"rx_merge_events":                0,
		"rx_merge_packets":               0,
		"rx_nodesc_trunc":                0,
		"rx_noskb_drops":                 0,
		"rx_outer_ip_hdr_chksum_err":     0,
		"rx_outer_tcp_udp_chksum_err":    0,
		"rx_reset":                       0,
		"rx_tcp_udp_chksum_err":          0,
		"rx_tobe_disc":                   0,
		"tx-0.tx_packets":                85843565,
		"tx-1.tx_packets":                108642725,
		"tx-2.tx_packets":                202596078,
		"tx-3.tx_packets":                207561010,
		"tx-4.tx_packets":                0,
		"tx_cb_packets":                  4,
		"tx_merge_events":                11025,
		"tx_pio_packets":                 531928114,
		"tx_pushes":                      604643378,
		"tx_tso_bursts":                  0,
		"tx_tso_fallbacks":               0,
		"tx_tso_long_headers":            0,
	}
	eth1Get := map[string]uint64{
		"autoneg": 1,
		"duplex":  1,
		"link":    1,
		"speed":   1000,
	}
	eth1 := &interfaceMock{"eth1", "driver1", "", eth1Stat, false, true, eth1Get}
	interfaceMap[eth1.name] = eth1

	eth2Stat := map[string]uint64{
		"interface_up":                   0,
		"port_rx_1024_to_15xx":           11529312,
		"port_rx_128_to_255":             1868952037,
		"port_rx_15xx_to_jumbo":          130339387,
		"port_rx_256_to_511":             843846270,
		"port_rx_512_to_1023":            173194372,
		"port_rx_64":                     9190374,
		"port_rx_65_to_127":              507806115,
		"port_rx_bad":                    0,
		"port_rx_bad_bytes":              0,
		"port_rx_bad_gtjumbo":            0,
		"port_rx_broadcast":              6648019,
		"port_rx_bytes":                  1007358162202,
		"port_rx_control":                0,
		"port_rx_dp_di_dropped_packets":  3164124639,
		"port_rx_dp_hlb_fetch":           0,
		"port_rx_dp_hlb_wait":            0,
		"port_rx_dp_q_disabled_packets":  0,
		"port_rx_dp_streaming_packets":   0,
		"port_rx_good":                   3544857867,
		"port_rx_good_bytes":             1007358162202,
		"port_rx_gtjumbo":                0,
		"port_rx_lt64":                   0,
		"port_rx_multicast":              2231999743,
		"port_rx_nodesc_drops":           0,
		"port_rx_overflow":               0,
		"port_rx_packets":                3544857867,
		"port_rx_pause":                  0,
		"port_rx_pm_discard_bb_overflow": 0,
		"port_rx_pm_discard_mapping":     0,
		"port_rx_pm_discard_qbb":         0,
		"port_rx_pm_discard_vfifo_full":  0,
		"port_rx_pm_trunc_bb_overflow":   0,
		"port_rx_pm_trunc_qbb":           0,
		"port_rx_pm_trunc_vfifo_full":    0,
		"port_rx_unicast":                1306210105,
		"port_tx_1024_to_15xx":           379,
		"port_tx_128_to_255":             202767251,
		"port_tx_15xx_to_jumbo":          558,
		"port_tx_256_to_511":             31454719,
		"port_tx_512_to_1023":            6865731,
		"port_tx_64":                     17268276,
		"port_tx_65_to_127":              272816313,
		"port_tx_broadcast":              6,
		"port_tx_bytes":                  78071946593,
		"port_tx_control":                0,
		"port_tx_lt64":                   0,
		"port_tx_multicast":              239510586,
		"port_tx_packets":                531173227,
		"port_tx_pause":                  0,
		"port_tx_unicast":                291662635,
		"ptp_bad_syncs":                  0,
		"ptp_fast_syncs":                 0,
		"ptp_filter_matches":             0,
		"ptp_good_syncs":                 0,
		"ptp_invalid_sync_windows":       0,
		"ptp_no_time_syncs":              0,
		"ptp_non_filter_matches":         0,
		"ptp_oversize_sync_windows":      0,
		"ptp_rx_no_timestamp":            0,
		"ptp_rx_timestamp_packets":       0,
		"ptp_sync_timeouts":              0,
		"ptp_timestamp_packets":          0,
		"ptp_tx_timestamp_packets":       0,
		"ptp_undersize_sync_windows":     0,
		"rx-0.rx_packets":                84587075,
		"rx-1.rx_packets":                74029305,
		"rx-2.rx_packets":                134586471,
		"rx-3.rx_packets":                87531322,
		"rx-4.rx_packets":                0,
		"rx_eth_crc_err":                 0,
		"rx_frm_trunc":                   0,
		"rx_inner_ip_hdr_chksum_err":     0,
		"rx_inner_tcp_udp_chksum_err":    0,
		"rx_ip_hdr_chksum_err":           0,
		"rx_mcast_mismatch":              0,
		"rx_merge_events":                0,
		"rx_merge_packets":               0,
		"rx_nodesc_trunc":                0,
		"rx_noskb_drops":                 0,
		"rx_outer_ip_hdr_chksum_err":     0,
		"rx_outer_tcp_udp_chksum_err":    0,
		"rx_reset":                       0,
		"rx_tcp_udp_chksum_err":          0,
		"rx_tobe_disc":                   0,
		"tx-0.tx_packets":                232521451,
		"tx-1.tx_packets":                97876137,
		"tx-2.tx_packets":                106822111,
		"tx-3.tx_packets":                93955050,
		"tx-4.tx_packets":                0,
		"tx_cb_packets":                  1,
		"tx_merge_events":                8402,
		"tx_pio_packets":                 481040054,
		"tx_pushes":                      531174491,
		"tx_tso_bursts":                  128,
		"tx_tso_fallbacks":               0,
		"tx_tso_long_headers":            0,
	}
	eth2Get := map[string]uint64{
		"autoneg": 0,
		"duplex":  255,
		"link":    0,
		"speed":   9223372036854775807,
	}
	eth2 := &interfaceMock{"eth2", "driver1", "", eth2Stat, false, false, eth2Get}
	interfaceMap[eth2.name] = eth2

	eth3Stat := map[string]uint64{
		"interface_up":                   1,
		"port_rx_1024_to_15xx":           25167245,
		"port_rx_128_to_255":             1573526387,
		"port_rx_15xx_to_jumbo":          137819058,
		"port_rx_256_to_511":             772038107,
		"port_rx_512_to_1023":            78294457,
		"port_rx_64":                     8798065,
		"port_rx_65_to_127":              450348015,
		"port_rx_bad":                    0,
		"port_rx_bad_bytes":              0,
		"port_rx_bad_gtjumbo":            0,
		"port_rx_broadcast":              6428250,
		"port_rx_bytes":                  893460472634,
		"port_rx_control":                0,
		"port_rx_dp_di_dropped_packets":  2772680304,
		"port_rx_dp_hlb_fetch":           0,
		"port_rx_dp_hlb_wait":            0,
		"port_rx_dp_q_disabled_packets":  0,
		"port_rx_dp_streaming_packets":   0,
		"port_rx_good":                   3045991334,
		"port_rx_good_bytes":             893460472927,
		"port_rx_gtjumbo":                0,
		"port_rx_lt64":                   0,
		"port_rx_multicast":              1639566045,
		"port_rx_nodesc_drops":           0,
		"port_rx_overflow":               0,
		"port_rx_packets":                3045991334,
		"port_rx_pause":                  0,
		"port_rx_pm_discard_bb_overflow": 0,
		"port_rx_pm_discard_mapping":     0,
		"port_rx_pm_discard_qbb":         0,
		"port_rx_pm_discard_vfifo_full":  0,
		"port_rx_pm_trunc_bb_overflow":   0,
		"port_rx_pm_trunc_qbb":           0,
		"port_rx_pm_trunc_vfifo_full":    0,
		"port_rx_unicast":                1399997040,
		"port_tx_1024_to_15xx":           236,
		"port_tx_128_to_255":             275090219,
		"port_tx_15xx_to_jumbo":          926,
		"port_tx_256_to_511":             48567221,
		"port_tx_512_to_1023":            5142016,
		"port_tx_64":                     113903973,
		"port_tx_65_to_127":              161935699,
		"port_tx_broadcast":              8,
		"port_tx_bytes":                  94357131016,
		"port_tx_control":                0,
		"port_tx_lt64":                   0,
		"port_tx_multicast":              325891647,
		"port_tx_packets":                604640290,
		"port_tx_pause":                  0,
		"port_tx_unicast":                278748635,
		"ptp_bad_syncs":                  1,
		"ptp_fast_syncs":                 1,
		"ptp_filter_matches":             0,
		"ptp_good_syncs":                 136151,
		"ptp_invalid_sync_windows":       0,
		"ptp_no_time_syncs":              1,
		"ptp_non_filter_matches":         0,
		"ptp_oversize_sync_windows":      53,
		"ptp_rx_no_timestamp":            0,
		"ptp_rx_timestamp_packets":       0,
		"ptp_sync_timeouts":              1,
		"ptp_timestamp_packets":          0,
		"ptp_tx_timestamp_packets":       0,
		"ptp_undersize_sync_windows":     3,
		"rx-0.rx_packets":                55659234,
		"rx-1.rx_packets":                87880538,
		"rx-2.rx_packets":                26746234,
		"rx-3.rx_packets":                103026471,
		"rx-4.rx_packets":                0,
		"rx_eth_crc_err":                 0,
		"rx_frm_trunc":                   0,
		"rx_inner_ip_hdr_chksum_err":     0,
		"rx_inner_tcp_udp_chksum_err":    0,
		"rx_ip_hdr_chksum_err":           0,
		"rx_mcast_mismatch":              0,
		"rx_merge_events":                0,
		"rx_merge_packets":               0,
		"rx_nodesc_trunc":                0,
		"rx_noskb_drops":                 0,
		"rx_outer_ip_hdr_chksum_err":     0,
		"rx_outer_tcp_udp_chksum_err":    0,
		"rx_reset":                       0,
		"rx_tcp_udp_chksum_err":          0,
		"rx_tobe_disc":                   0,
		"tx-0.tx_packets":                85843565,
		"tx-1.tx_packets":                108642725,
		"tx-2.tx_packets":                202596078,
		"tx-3.tx_packets":                207561010,
		"tx-4.tx_packets":                0,
		"tx_cb_packets":                  4,
		"tx_merge_events":                11025,
		"tx_pio_packets":                 531928114,
		"tx_pushes":                      604643378,
		"tx_tso_bursts":                  0,
		"tx_tso_fallbacks":               0,
		"tx_tso_long_headers":            0,
	}
	eth3Get := map[string]uint64{
		"autoneg": 1,
		"duplex":  1,
		"link":    1,
		"speed":   1000,
	}
	eth3 := &interfaceMock{"eth3", "driver1", "namespace1", eth3Stat, false, true, eth3Get}
	interfaceMap[eth3.name] = eth3

	eth4Stat := map[string]uint64{
		"interface_up":                   1,
		"port_rx_1024_to_15xx":           25167245,
		"port_rx_128_to_255":             1573526387,
		"port_rx_15xx_to_jumbo":          137819058,
		"port_rx_256_to_511":             772038107,
		"port_rx_512_to_1023":            78294457,
		"port_rx_64":                     8798065,
		"port_rx_65_to_127":              450348015,
		"port_rx_bad":                    0,
		"port_rx_bad_bytes":              0,
		"port_rx_bad_gtjumbo":            0,
		"port_rx_broadcast":              6428250,
		"port_rx_bytes":                  893460472634,
		"port_rx_control":                0,
		"port_rx_dp_di_dropped_packets":  2772680304,
		"port_rx_dp_hlb_fetch":           0,
		"port_rx_dp_hlb_wait":            0,
		"port_rx_dp_q_disabled_packets":  0,
		"port_rx_dp_streaming_packets":   0,
		"port_rx_good":                   3045991334,
		"port_rx_good_bytes":             893460472927,
		"port_rx_gtjumbo":                0,
		"port_rx_lt64":                   0,
		"port_rx_multicast":              1639566045,
		"port_rx_nodesc_drops":           0,
		"port_rx_overflow":               0,
		"port_rx_packets":                3045991334,
		"port_rx_pause":                  0,
		"port_rx_pm_discard_bb_overflow": 0,
		"port_rx_pm_discard_mapping":     0,
		"port_rx_pm_discard_qbb":         0,
		"port_rx_pm_discard_vfifo_full":  0,
		"port_rx_pm_trunc_bb_overflow":   0,
		"port_rx_pm_trunc_qbb":           0,
		"port_rx_pm_trunc_vfifo_full":    0,
		"port_rx_unicast":                1399997040,
		"port_tx_1024_to_15xx":           236,
		"port_tx_128_to_255":             275090219,
		"port_tx_15xx_to_jumbo":          926,
		"port_tx_256_to_511":             48567221,
		"port_tx_512_to_1023":            5142016,
		"port_tx_64":                     113903973,
		"port_tx_65_to_127":              161935699,
		"port_tx_broadcast":              8,
		"port_tx_bytes":                  94357131016,
		"port_tx_control":                0,
		"port_tx_lt64":                   0,
		"port_tx_multicast":              325891647,
		"port_tx_packets":                604640290,
		"port_tx_pause":                  0,
		"port_tx_unicast":                278748635,
		"ptp_bad_syncs":                  1,
		"ptp_fast_syncs":                 1,
		"ptp_filter_matches":             0,
		"ptp_good_syncs":                 136151,
		"ptp_invalid_sync_windows":       0,
		"ptp_no_time_syncs":              1,
		"ptp_non_filter_matches":         0,
		"ptp_oversize_sync_windows":      53,
		"ptp_rx_no_timestamp":            0,
		"ptp_rx_timestamp_packets":       0,
		"ptp_sync_timeouts":              1,
		"ptp_timestamp_packets":          0,
		"ptp_tx_timestamp_packets":       0,
		"ptp_undersize_sync_windows":     3,
		"rx-0.rx_packets":                55659234,
		"rx-1.rx_packets":                87880538,
		"rx-2.rx_packets":                26746234,
		"rx-3.rx_packets":                103026471,
		"rx-4.rx_packets":                0,
		"rx_eth_crc_err":                 0,
		"rx_frm_trunc":                   0,
		"rx_inner_ip_hdr_chksum_err":     0,
		"rx_inner_tcp_udp_chksum_err":    0,
		"rx_ip_hdr_chksum_err":           0,
		"rx_mcast_mismatch":              0,
		"rx_merge_events":                0,
		"rx_merge_packets":               0,
		"rx_nodesc_trunc":                0,
		"rx_noskb_drops":                 0,
		"rx_outer_ip_hdr_chksum_err":     0,
		"rx_outer_tcp_udp_chksum_err":    0,
		"rx_reset":                       0,
		"rx_tcp_udp_chksum_err":          0,
		"rx_tobe_disc":                   0,
		"tx-0.tx_packets":                85843565,
		"tx-1.tx_packets":                108642725,
		"tx-2.tx_packets":                202596078,
		"tx-3.tx_packets":                207561010,
		"tx-4.tx_packets":                0,
		"tx_cb_packets":                  4,
		"tx_merge_events":                11025,
		"tx_pio_packets":                 531928114,
		"tx_pushes":                      604643378,
		"tx_tso_bursts":                  0,
		"tx_tso_fallbacks":               0,
		"tx_tso_long_headers":            0,
	}
	eth4Get := map[string]uint64{
		"autoneg": 1,
		"duplex":  1,
		"link":    1,
		"speed":   100,
	}
	eth4 := &interfaceMock{"eth4", "driver1", "namespace2", eth4Stat, false, true, eth4Get}
	interfaceMap[eth4.name] = eth4

	// dummy loopback including dummy stat to ensure that the ignore feature is working
	lo0Stat := map[string]uint64{
		"dummy": 0,
	}
	lo0Get := map[string]uint64{
		"autoneg": 1,
		"duplex":  1,
		"link":    1,
		"speed":   1000,
	}
	lo0 := &interfaceMock{"lo0", "", "", lo0Stat, true, true, lo0Get}
	interfaceMap[lo0.name] = lo0

	c := &commandEthtoolMock{interfaceMap}
	eth = &Ethtool{
		DownInterfaces: "expose",
		command:        c,
	}
}

func toStringMapInterface(in map[string]uint64) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range in {
		m[k] = v
	}
	return m
}

func toStringMapUint(in map[string]interface{}) map[string]uint64 {
	m := map[string]uint64{}
	for k, v := range in {
		t := v.(uint64)
		m[k] = t
	}
	return m
}

func TestGather(t *testing.T) {
	setup()

	err := eth.Init()
	require.NoError(t, err)

	var acc testutil.Accumulator
	err = eth.Gather(&acc)
	require.NoError(t, err)
	require.Len(t, acc.Metrics, 2)

	expectedFieldsEth1 := toStringMapInterface(interfaceMap["eth1"].stat)
	for k, v := range interfaceMap["eth1"].cmdGet {
		expectedFieldsEth1[k] = v
	}
	expectedFieldsEth1["interface_up_counter"] = expectedFieldsEth1["interface_up"]
	expectedFieldsEth1["interface_up"] = true

	expectedTagsEth1 := map[string]string{
		"interface": "eth1",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth1, expectedTagsEth1)

	expectedFieldsEth2 := toStringMapInterface(interfaceMap["eth2"].stat)
	for k, v := range interfaceMap["eth2"].cmdGet {
		expectedFieldsEth2[k] = v
	}
	expectedFieldsEth2["interface_up_counter"] = expectedFieldsEth2["interface_up"]
	expectedFieldsEth2["interface_up"] = false
	expectedTagsEth2 := map[string]string{
		"driver":    "driver1",
		"interface": "eth2",
		"namespace": "",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth2, expectedTagsEth2)
}

func TestGatherIncludeInterfaces(t *testing.T) {
	setup()

	eth.InterfaceInclude = append(eth.InterfaceInclude, "eth1")

	err := eth.Init()
	require.NoError(t, err)

	var acc testutil.Accumulator
	err = eth.Gather(&acc)
	require.NoError(t, err)
	require.Len(t, acc.Metrics, 1)

	// Should contain eth1
	expectedFieldsEth1 := toStringMapInterface(interfaceMap["eth1"].stat)
	for k, v := range interfaceMap["eth1"].cmdGet {
		expectedFieldsEth1[k] = v
	}
	expectedFieldsEth1["interface_up_counter"] = expectedFieldsEth1["interface_up"]
	expectedFieldsEth1["interface_up"] = true
	expectedTagsEth1 := map[string]string{
		"interface": "eth1",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth1, expectedTagsEth1)

	// Should not contain eth2
	expectedFieldsEth2 := toStringMapInterface(interfaceMap["eth2"].stat)
	for k, v := range interfaceMap["eth2"].cmdGet {
		expectedFieldsEth2[k] = v
	}
	expectedFieldsEth2["interface_up_counter"] = expectedFieldsEth2["interface_up"]
	expectedFieldsEth2["interface_up"] = false
	expectedTagsEth2 := map[string]string{
		"interface": "eth2",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertDoesNotContainsTaggedFields(t, pluginName, expectedFieldsEth2, expectedTagsEth2)
}

func TestGatherIgnoreInterfaces(t *testing.T) {
	setup()

	eth.InterfaceExclude = append(eth.InterfaceExclude, "eth1")

	err := eth.Init()
	require.NoError(t, err)

	var acc testutil.Accumulator
	err = eth.Gather(&acc)
	require.NoError(t, err)
	require.Len(t, acc.Metrics, 1)

	// Should not contain eth1
	expectedFieldsEth1 := toStringMapInterface(interfaceMap["eth1"].stat)
	for k, v := range interfaceMap["eth1"].cmdGet {
		expectedFieldsEth1[k] = v
	}
	expectedFieldsEth1["interface_up_counter"] = expectedFieldsEth1["interface_up"]
	expectedFieldsEth1["interface_up"] = true
	expectedTagsEth1 := map[string]string{
		"interface": "eth1",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertDoesNotContainsTaggedFields(t, pluginName, expectedFieldsEth1, expectedTagsEth1)

	// Should contain eth2
	expectedFieldsEth2 := toStringMapInterface(interfaceMap["eth2"].stat)
	for k, v := range interfaceMap["eth2"].cmdGet {
		expectedFieldsEth2[k] = v
	}
	expectedFieldsEth2["interface_up_counter"] = expectedFieldsEth2["interface_up"]
	expectedFieldsEth2["interface_up"] = false
	expectedTagsEth2 := map[string]string{
		"interface": "eth2",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth2, expectedTagsEth2)
}

func TestSkipMetricsForInterfaceDown(t *testing.T) {
	setup()

	eth.DownInterfaces = "skip"

	err := eth.Init()
	require.NoError(t, err)

	var acc testutil.Accumulator
	err = eth.Gather(&acc)
	require.NoError(t, err)
	require.Len(t, acc.Metrics, 1)

	expectedFieldsEth1 := toStringMapInterface(interfaceMap["eth1"].stat)
	for k, v := range interfaceMap["eth1"].cmdGet {
		expectedFieldsEth1[k] = v
	}
	expectedFieldsEth1["interface_up_counter"] = expectedFieldsEth1["interface_up"]
	expectedFieldsEth1["interface_up"] = true

	expectedTagsEth1 := map[string]string{
		"interface": "eth1",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth1, expectedTagsEth1)
}

func TestGatherIncludeNamespaces(t *testing.T) {
	setup()
	var acc testutil.Accumulator

	eth.NamespaceInclude = append(eth.NamespaceInclude, "namespace1")

	err := eth.Init()
	require.NoError(t, err)

	err = eth.Gather(&acc)
	require.NoError(t, err)
	require.Len(t, acc.Metrics, 1)

	// Should contain eth3
	expectedFieldsEth3 := toStringMapInterface(interfaceMap["eth3"].stat)
	for k, v := range interfaceMap["eth3"].cmdGet {
		expectedFieldsEth3[k] = v
	}
	expectedFieldsEth3["interface_up_counter"] = expectedFieldsEth3["interface_up"]
	expectedFieldsEth3["interface_up"] = true
	expectedTagsEth3 := map[string]string{
		"interface": "eth3",
		"driver":    "driver1",
		"namespace": "namespace1",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth3, expectedTagsEth3)

	// Should not contain eth2
	expectedFieldsEth2 := toStringMapInterface(interfaceMap["eth2"].stat)
	for k, v := range interfaceMap["eth2"].cmdGet {
		expectedFieldsEth2[k] = v
	}
	expectedFieldsEth2["interface_up_counter"] = expectedFieldsEth2["interface_up"]
	expectedFieldsEth2["interface_up"] = false
	expectedTagsEth2 := map[string]string{
		"interface": "eth2",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertDoesNotContainsTaggedFields(t, pluginName, expectedFieldsEth2, expectedTagsEth2)
}

func TestGatherIgnoreNamespaces(t *testing.T) {
	setup()
	var acc testutil.Accumulator

	eth.NamespaceExclude = append(eth.NamespaceExclude, "namespace2")

	err := eth.Init()
	require.NoError(t, err)

	err = eth.Gather(&acc)
	require.NoError(t, err)
	require.Len(t, acc.Metrics, 3)

	// Should not contain eth4
	expectedFieldsEth4 := toStringMapInterface(interfaceMap["eth4"].stat)
	for k, v := range interfaceMap["eth4"].cmdGet {
		expectedFieldsEth4[k] = v
	}
	expectedFieldsEth4["interface_up_counter"] = expectedFieldsEth4["interface_up"]
	expectedFieldsEth4["interface_up"] = true
	expectedTagsEth4 := map[string]string{
		"interface": "eth4",
		"driver":    "driver1",
		"namespace": "namespace2",
	}
	acc.AssertDoesNotContainsTaggedFields(t, pluginName, expectedFieldsEth4, expectedTagsEth4)

	// Should contain eth2
	expectedFieldsEth2 := toStringMapInterface(interfaceMap["eth2"].stat)
	for k, v := range interfaceMap["eth2"].cmdGet {
		expectedFieldsEth2[k] = v
	}
	expectedFieldsEth2["interface_up_counter"] = expectedFieldsEth2["interface_up"]
	expectedFieldsEth2["interface_up"] = false
	expectedTagsEth2 := map[string]string{
		"interface": "eth2",
		"driver":    "driver1",
		"namespace": "",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth2, expectedTagsEth2)

	// Should contain eth3
	expectedFieldsEth3 := toStringMapInterface(interfaceMap["eth3"].stat)
	for k, v := range interfaceMap["eth3"].cmdGet {
		expectedFieldsEth3[k] = v
	}
	expectedFieldsEth3["interface_up_counter"] = expectedFieldsEth3["interface_up"]
	expectedFieldsEth3["interface_up"] = true
	expectedTagsEth3 := map[string]string{
		"interface": "eth3",
		"driver":    "driver1",
		"namespace": "namespace1",
	}
	acc.AssertContainsTaggedFields(t, pluginName, expectedFieldsEth3, expectedTagsEth3)
}

type testCase struct {
	normalization  []string
	stats          map[string]interface{}
	expectedFields map[string]interface{}
}

func TestNormalizedKeys(t *testing.T) {
	cases := []testCase{
		{
			normalization: []string{"underscore"},
			stats: map[string]interface{}{
				"port rx":      uint64(1),
				" Port_tx":     uint64(0),
				"interface_up": uint64(0),
			},
			expectedFields: map[string]interface{}{
				"port_rx":              uint64(1),
				"_Port_tx":             uint64(0),
				"interface_up":         true,
				"interface_up_counter": uint64(0),
			},
		},
		{
			normalization: []string{"underscore", "lower"},
			stats: map[string]interface{}{
				"Port rx":      uint64(1),
				" Port_tx":     uint64(0),
				"interface_up": uint64(0),
			},
			expectedFields: map[string]interface{}{
				"port_rx":              uint64(1),
				"_port_tx":             uint64(0),
				"interface_up":         true,
				"interface_up_counter": uint64(0),
			},
		},
		{
			normalization: []string{"underscore", "lower", "trim"},
			stats: map[string]interface{}{
				"  Port RX ":   uint64(1),
				" Port_tx":     uint64(0),
				"interface_up": uint64(0),
			},
			expectedFields: map[string]interface{}{
				"port_rx":              uint64(1),
				"port_tx":              uint64(0),
				"interface_up":         true,
				"interface_up_counter": uint64(0),
			},
		},
		{
			normalization: []string{"underscore", "lower", "snakecase", "trim"},
			stats: map[string]interface{}{
				"  Port RX ":   uint64(1),
				" Port_tx":     uint64(0),
				"interface_up": uint64(0),
			},
			expectedFields: map[string]interface{}{
				"port_rx":              uint64(1),
				"port_tx":              uint64(0),
				"interface_up":         true,
				"interface_up_counter": uint64(0),
			},
		},
		{
			normalization: []string{"snakecase"},
			stats: map[string]interface{}{
				"  PortRX ":    uint64(1),
				" PortTX":      uint64(0),
				"interface_up": uint64(0),
			},
			expectedFields: map[string]interface{}{
				"port_rx":              uint64(1),
				"port_tx":              uint64(0),
				"interface_up":         true,
				"interface_up_counter": uint64(0),
			},
		},
		{
			stats: map[string]interface{}{
				"  Port RX ":   uint64(1),
				" Port_tx":     uint64(0),
				"interface_up": uint64(0),
			},
			expectedFields: map[string]interface{}{
				"  Port RX ":           uint64(1),
				" Port_tx":             uint64(0),
				"interface_up":         true,
				"interface_up_counter": uint64(0),
			},
		},
		{
			stats: map[string]interface{}{
				"  Port RX ": uint64(1),
				" Port_tx":   uint64(0),
			},
			expectedFields: map[string]interface{}{
				"  Port RX ":   uint64(1),
				" Port_tx":     uint64(0),
				"interface_up": true,
			},
		},
	}

	for _, c := range cases {
		eth0 := &interfaceMock{"eth0", "e1000e", "", toStringMapUint(c.stats), false, true, map[string]uint64{}}
		expectedTags := map[string]string{
			"interface": eth0.name,
			"driver":    eth0.driverName,
			"namespace": "",
		}

		interfaceMap = make(map[string]*interfaceMock)
		interfaceMap[eth0.name] = eth0

		cmd := &commandEthtoolMock{interfaceMap}
		eth = &Ethtool{
			NormalizeKeys: c.normalization,
			command:       cmd,
		}

		err := eth.Init()
		require.NoError(t, err)

		var acc testutil.Accumulator
		err = eth.Gather(&acc)

		require.NoError(t, err)
		require.Len(t, acc.Metrics, 1)

		acc.AssertContainsFields(t, pluginName, c.expectedFields)
		acc.AssertContainsTaggedFields(t, pluginName, c.expectedFields, expectedTags)
	}
}
