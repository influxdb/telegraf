//go:build linux

package zfs

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

const arcstatsContents = `5 1 0x01 86 4128 23617128247 12081618582809582
name                            type data
hits                            4    5968846374
misses                          4    1659178751
demand_data_hits                4    4860247322
demand_data_misses              4    501499535
demand_metadata_hits            4    708608325
demand_metadata_misses          4    156591375
prefetch_data_hits              4    367047144
prefetch_data_misses            4    974529898
prefetch_metadata_hits          4    32943583
prefetch_metadata_misses        4    26557943
mru_hits                        4    301176811
mru_ghost_hits                  4    47066067
mfu_hits                        4    5520612438
mfu_ghost_hits                  4    45784009
deleted                         4    1718937704
recycle_miss                    4    481222994
mutex_miss                      4    20575623
evict_skip                      4    14655903906543
evict_l2_cached                 4    145310202998272
evict_l2_eligible               4    16345402777088
evict_l2_ineligible             4    7437226893312
hash_elements                   4    36617980
hash_elements_max               4    36618318
hash_collisions                 4    554145157
hash_chains                     4    4187651
hash_chain_max                  4    26
p                               4    13963222064
c                               4    16381258376
c_min                           4    4194304
c_max                           4    16884125696
size                            4    16319887096
hdr_size                        4    42567864
data_size                       4    60066304
meta_size                       4    1701534208
other_size                      4    1661543168
anon_size                       4    94720
anon_evict_data                 4    0
anon_evict_metadata             4    0
mru_size                        4    973099008
mru_evict_data                  4    9175040
mru_evict_metadata              4    32768
mru_ghost_size                  4    32768
mru_ghost_evict_data            4    0
mru_ghost_evict_metadata        4    32768
mfu_size                        4    788406784
mfu_evict_data                  4    50881024
mfu_evict_metadata              4    81920
mfu_ghost_size                  4    0
mfu_ghost_evict_data            4    0
mfu_ghost_evict_metadata        4    0
l2_hits                         4    573868618
l2_misses                       4    1085309718
l2_feeds                        4    12182087
l2_rw_clash                     4    9610
l2_read_bytes                   4    32695938336768
l2_write_bytes                  4    2826774778880
l2_writes_sent                  4    4267687
l2_writes_done                  4    4267687
l2_writes_error                 4    0
l2_writes_hdr_miss              4    164
l2_evict_lock_retry             4    5
l2_evict_reading                4    0
l2_free_on_write                4    1606914
l2_cdata_free_on_write          4    1775
l2_abort_lowmem                 4    83462
l2_cksum_bad                    4    393860640
l2_io_error                     4    53881460
l2_size                         4    2471466648576
l2_asize                        4    2461690072064
l2_hdr_size                     4    12854175552
l2_compress_successes           4    12184849
l2_compress_zeros               4    0
l2_compress_failures            4    0
memory_throttle_count           4    0
duplicate_buffers               4    0
duplicate_buffers_size          4    0
duplicate_reads                 4    0
memory_direct_count             4    5159942
memory_indirect_count           4    3034640
arc_no_grow                     4    0
arc_tempreserve                 4    0
arc_loaned_bytes                4    0
arc_prune                       4    114554259559
arc_meta_used                   4    16259820792
arc_meta_limit                  4    12663094272
arc_meta_max                    4    18327165696
`

const zfetchstatsContents = `3 1 0x01 11 528 23607270446 12081656848148208
name                            type data
hits                            4    7812959060
misses                          4    4154484207
colinear_hits                   4    1366368
colinear_misses                 4    4153117839
stride_hits                     4    7309776732
stride_misses                   4    222766182
reclaim_successes               4    107788388
reclaim_failures                4    4045329451
streams_resets                  4    20989756
streams_noresets                4    503182328
bogus_streams                   4    0
`
const poolIoContents = `11 3 0x00 1 80 2225326830828 32953476980628
nread    nwritten reads    writes   wtime    wlentime wupdate  rtime    rlentime rupdate  wcnt     rcnt
1884160  6450688  22       978      272187126 2850519036 2263669418655 424226814 2850519036 2263669871823 0        0
`
const objsetContents = `36 1 0x01 7 2160 5214787391 74985931356512
name                            type data
dataset_name                    7    HOME
writes                          4    978
nwritten                        4    6450688
reads                           4    22
nread                           4    1884160
nunlinks                        4    14148
nunlinked                       4    14147
`
const objsetV22Contents = `36 1 0x01 7 2160 5214787391 74985931356512
name                            type data
dataset_name                    7    HOMEV22
writes                          4    978
nwritten                        4    6450688
reads                           4    22
nread                           4    1884160
nunlinks                        4    14148
nunlinked                       4    14147
zil_commit_count                4    1
zil_commit_writer_count         4    2
zil_itx_count                   4    3
zil_itx_indirect_count          4    4
zil_itx_indirect_bytes          4    5
zil_itx_copied_count            4    6
zil_itx_copied_bytes            4    7
zil_itx_needcopy_count          4    8
zil_itx_needcopy_bytes          4    9
zil_itx_metaslab_normal_count   4    10
zil_itx_metaslab_normal_bytes   4    11
zil_itx_metaslab_normal_write   4    12
zil_itx_metaslab_normal_alloc   4    13
zil_itx_metaslab_slog_count     4    14
zil_itx_metaslab_slog_bytes     4    15
zil_itx_metaslab_slog_write     4    16
zil_itx_metaslab_slog_alloc     4    17
`
const zilContents = `7 1 0x01 14 672 34118481334 437444452158445
name                            type data
zil_commit_count                4    77
zil_commit_writer_count         4    77
zil_itx_count                   4    1
zil_itx_indirect_count          4    2
zil_itx_indirect_bytes          4    3
zil_itx_copied_count            4    4
zil_itx_copied_bytes            4    5
zil_itx_needcopy_count          4    6
zil_itx_needcopy_bytes          4    7
zil_itx_metaslab_normal_count   4    8
zil_itx_metaslab_normal_bytes   4    9
zil_itx_metaslab_slog_count     4    10
zil_itx_metaslab_slog_bytes     4    11
`
const fmContents = `0 1 0x01 4 192 34087340971 437562103532892
name                            type data
erpt-dropped                    4    101
erpt-set-failed                 4    202
fmri-set-failed                 4    303
payload-set-failed              4    404
`
const dmuTxContents = `5 1 0x01 11 528 34103260832 437683925071438
name                            type data
dmu_tx_assigned                 4    39321636
dmu_tx_delay                    4    111
dmu_tx_error                    4    222
dmu_tx_suspended                4    333
dmu_tx_group                    4    444
dmu_tx_memory_reserve           4    555
dmu_tx_memory_reclaim           4    666
dmu_tx_dirty_throttle           4    777
dmu_tx_dirty_delay              4    888
dmu_tx_dirty_over_max           4    999
dmu_tx_quota                    4    101010
`

const abdstatsContents = `7 1 0x01 21 1008 25476602923533 29223577332204
name                            type data
struct_size                     4    33840
linear_cnt                      4    834
linear_data_size                4    989696
scatter_cnt                     4    12
scatter_data_size               4    187904
scatter_chunk_waste             4    4608
scatter_order_0                 4    1
scatter_order_1                 4    21
scatter_order_2                 4    11
scatter_order_3                 4    33
scatter_order_4                 4    44
scatter_order_5                 4    76
scatter_order_6                 4    489
scatter_order_7                 4    237483
scatter_order_8                 4    233
scatter_order_9                 4    4411
scatter_order_10                4    1023
scatter_page_multi_chunk        4    32122
scatter_page_multi_zone         4    9930
scatter_page_alloc_retry        4    99311
scatter_sg_table_retry          4    99221
`

func TestZfsPoolMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	testKstatPath := tmpDir + "/telegraf/proc/spl/kstat/zfs"
	err := os.MkdirAll(testKstatPath, 0750)
	require.NoError(t, err)

	err = os.MkdirAll(testKstatPath+"/HOME", 0750)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/HOME/io", []byte(poolIoContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/arcstats", []byte(arcstatsContents), 0640)
	require.NoError(t, err)

	poolMetrics := getPoolMetrics()

	var acc testutil.Accumulator

	z := &Zfs{KstatPath: testKstatPath, KstatMetrics: []string{"arcstats"}}
	err = z.Gather(&acc)
	require.NoError(t, err)

	require.False(t, acc.HasMeasurement("zfs_pool"))
	acc.Metrics = nil

	z = &Zfs{KstatPath: testKstatPath, KstatMetrics: []string{"arcstats"}, PoolMetrics: true}
	err = z.Gather(&acc)
	require.NoError(t, err)

	// one pool, all metrics
	tags := map[string]string{
		"pool": "HOME",
	}

	acc.AssertContainsTaggedFields(t, "zfs_pool", poolMetrics, tags)

	err = os.WriteFile(testKstatPath+"/HOME/objset-0x20a", []byte(objsetContents), 0640)
	require.NoError(t, err)
	err = os.WriteFile(testKstatPath+"/HOME/objset-0x20b", []byte(objsetV22Contents), 0640)
	require.NoError(t, err)

	acc.Metrics = nil

	err = z.Gather(&acc)
	require.NoError(t, err)

	tags["dataset"] = "HOME"
	poolMetrics = getPoolMetricsNewFormat()
	acc.AssertContainsTaggedFields(t, "zfs_pool", poolMetrics, tags)

	tags["dataset"] = "HOMEV22"
	poolMetrics = getPoolMetricsNewFormatV22()
	acc.AssertContainsTaggedFields(t, "zfs_pool", poolMetrics, tags)
}

func TestZfsGeneratesMetrics(t *testing.T) {
	tmpDir := t.TempDir()

	testKstatPath := tmpDir + "/telegraf/proc/spl/kstat/zfs"
	err := os.MkdirAll(testKstatPath, 0750)
	require.NoError(t, err)

	err = os.MkdirAll(testKstatPath, 0750)
	require.NoError(t, err)

	err = os.MkdirAll(testKstatPath+"/HOME", 0750)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/HOME/io", []byte(""), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/arcstats", []byte(arcstatsContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/zfetchstats", []byte(zfetchstatsContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/zil", []byte(zilContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/fm", []byte(fmContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/dmu_tx", []byte(dmuTxContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/abdstats", []byte(abdstatsContents), 0640)
	require.NoError(t, err)

	intMetrics := getKstatMetricsAll()

	var acc testutil.Accumulator

	// one pool, all metrics
	tags := map[string]string{
		"pools": "HOME",
	}

	z := &Zfs{KstatPath: testKstatPath}
	err = z.Gather(&acc)
	require.NoError(t, err)

	acc.AssertContainsTaggedFields(t, "zfs", intMetrics, tags)
	acc.Metrics = nil

	// two pools, all metrics
	err = os.MkdirAll(testKstatPath+"/STORAGE", 0750)
	require.NoError(t, err)

	err = os.WriteFile(testKstatPath+"/STORAGE/io", []byte(""), 0640)
	require.NoError(t, err)

	tags = map[string]string{
		"pools": "HOME::STORAGE",
	}

	z = &Zfs{KstatPath: testKstatPath}
	acc2 := testutil.Accumulator{}
	err = z.Gather(&acc2)
	require.NoError(t, err)

	acc2.AssertContainsTaggedFields(t, "zfs", intMetrics, tags)
	acc2.Metrics = nil

	intMetrics = getKstatMetricsArcOnly()

	// two pools, one metric
	z = &Zfs{KstatPath: testKstatPath, KstatMetrics: []string{"arcstats"}}
	acc3 := testutil.Accumulator{}
	err = z.Gather(&acc3)
	require.NoError(t, err)

	acc3.AssertContainsTaggedFields(t, "zfs", intMetrics, tags)
}

func TestGetTags(t *testing.T) {
	tests := []struct {
		name     string
		pools    []poolInfo
		expected map[string]string
	}{
		{
			name:     "no pools",
			expected: map[string]string{"pools": ""},
		},
		{
			name: "single pool",
			pools: []poolInfo{
				{"data", "/proc/spl/kstat/zfs/data/objset-0x9288", v2},
			},
			expected: map[string]string{"pools": "data"},
		},
		{
			name: "duplicate pool names",
			pools: []poolInfo{
				{"pool", "/proc/spl/kstat/zfs/pool/objset-0x23ce1", v2},
				{"pool", "/proc/spl/kstat/zfs/pool/objset-0x2e", v2},
				{"data", "/proc/spl/kstat/zfs/data/objset-0x9288", v2},
			},
			expected: map[string]string{"pools": "pool::data"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tags := getTags(tc.pools)
			require.Equal(t, tc.expected, tags)
		})
	}
}

func getKstatMetricsArcOnly() map[string]interface{} {
	return map[string]interface{}{
		"arcstats_hits":                     int64(5968846374),
		"arcstats_misses":                   int64(1659178751),
		"arcstats_demand_data_hits":         int64(4860247322),
		"arcstats_demand_data_misses":       int64(501499535),
		"arcstats_demand_metadata_hits":     int64(708608325),
		"arcstats_demand_metadata_misses":   int64(156591375),
		"arcstats_prefetch_data_hits":       int64(367047144),
		"arcstats_prefetch_data_misses":     int64(974529898),
		"arcstats_prefetch_metadata_hits":   int64(32943583),
		"arcstats_prefetch_metadata_misses": int64(26557943),
		"arcstats_mru_hits":                 int64(301176811),
		"arcstats_mru_ghost_hits":           int64(47066067),
		"arcstats_mfu_hits":                 int64(5520612438),
		"arcstats_mfu_ghost_hits":           int64(45784009),
		"arcstats_deleted":                  int64(1718937704),
		"arcstats_recycle_miss":             int64(481222994),
		"arcstats_mutex_miss":               int64(20575623),
		"arcstats_evict_skip":               int64(14655903906543),
		"arcstats_evict_l2_cached":          int64(145310202998272),
		"arcstats_evict_l2_eligible":        int64(16345402777088),
		"arcstats_evict_l2_ineligible":      int64(7437226893312),
		"arcstats_hash_elements":            int64(36617980),
		"arcstats_hash_elements_max":        int64(36618318),
		"arcstats_hash_collisions":          int64(554145157),
		"arcstats_hash_chains":              int64(4187651),
		"arcstats_hash_chain_max":           int64(26),
		"arcstats_p":                        int64(13963222064),
		"arcstats_c":                        int64(16381258376),
		"arcstats_c_min":                    int64(4194304),
		"arcstats_c_max":                    int64(16884125696),
		"arcstats_size":                     int64(16319887096),
		"arcstats_hdr_size":                 int64(42567864),
		"arcstats_data_size":                int64(60066304),
		"arcstats_meta_size":                int64(1701534208),
		"arcstats_other_size":               int64(1661543168),
		"arcstats_anon_size":                int64(94720),
		"arcstats_anon_evict_data":          int64(0),
		"arcstats_anon_evict_metadata":      int64(0),
		"arcstats_mru_size":                 int64(973099008),
		"arcstats_mru_evict_data":           int64(9175040),
		"arcstats_mru_evict_metadata":       int64(32768),
		"arcstats_mru_ghost_size":           int64(32768),
		"arcstats_mru_ghost_evict_data":     int64(0),
		"arcstats_mru_ghost_evict_metadata": int64(32768),
		"arcstats_mfu_size":                 int64(788406784),
		"arcstats_mfu_evict_data":           int64(50881024),
		"arcstats_mfu_evict_metadata":       int64(81920),
		"arcstats_mfu_ghost_size":           int64(0),
		"arcstats_mfu_ghost_evict_data":     int64(0),
		"arcstats_mfu_ghost_evict_metadata": int64(0),
		"arcstats_l2_hits":                  int64(573868618),
		"arcstats_l2_misses":                int64(1085309718),
		"arcstats_l2_feeds":                 int64(12182087),
		"arcstats_l2_rw_clash":              int64(9610),
		"arcstats_l2_read_bytes":            int64(32695938336768),
		"arcstats_l2_write_bytes":           int64(2826774778880),
		"arcstats_l2_writes_sent":           int64(4267687),
		"arcstats_l2_writes_done":           int64(4267687),
		"arcstats_l2_writes_error":          int64(0),
		"arcstats_l2_writes_hdr_miss":       int64(164),
		"arcstats_l2_evict_lock_retry":      int64(5),
		"arcstats_l2_evict_reading":         int64(0),
		"arcstats_l2_free_on_write":         int64(1606914),
		"arcstats_l2_cdata_free_on_write":   int64(1775),
		"arcstats_l2_abort_lowmem":          int64(83462),
		"arcstats_l2_cksum_bad":             int64(393860640),
		"arcstats_l2_io_error":              int64(53881460),
		"arcstats_l2_size":                  int64(2471466648576),
		"arcstats_l2_asize":                 int64(2461690072064),
		"arcstats_l2_hdr_size":              int64(12854175552),
		"arcstats_l2_compress_successes":    int64(12184849),
		"arcstats_l2_compress_zeros":        int64(0),
		"arcstats_l2_compress_failures":     int64(0),
		"arcstats_memory_throttle_count":    int64(0),
		"arcstats_duplicate_buffers":        int64(0),
		"arcstats_duplicate_buffers_size":   int64(0),
		"arcstats_duplicate_reads":          int64(0),
		"arcstats_memory_direct_count":      int64(5159942),
		"arcstats_memory_indirect_count":    int64(3034640),
		"arcstats_arc_no_grow":              int64(0),
		"arcstats_arc_tempreserve":          int64(0),
		"arcstats_arc_loaned_bytes":         int64(0),
		"arcstats_arc_prune":                int64(114554259559),
		"arcstats_arc_meta_used":            int64(16259820792),
		"arcstats_arc_meta_limit":           int64(12663094272),
		"arcstats_arc_meta_max":             int64(18327165696),
	}
}

func getKstatMetricsAll() map[string]interface{} {
	otherMetrics := map[string]interface{}{
		"zfetchstats_hits":                  int64(7812959060),
		"zfetchstats_misses":                int64(4154484207),
		"zfetchstats_colinear_hits":         int64(1366368),
		"zfetchstats_colinear_misses":       int64(4153117839),
		"zfetchstats_stride_hits":           int64(7309776732),
		"zfetchstats_stride_misses":         int64(222766182),
		"zfetchstats_reclaim_successes":     int64(107788388),
		"zfetchstats_reclaim_failures":      int64(4045329451),
		"zfetchstats_streams_resets":        int64(20989756),
		"zfetchstats_streams_noresets":      int64(503182328),
		"zfetchstats_bogus_streams":         int64(0),
		"zil_commit_count":                  int64(77),
		"zil_commit_writer_count":           int64(77),
		"zil_itx_count":                     int64(1),
		"zil_itx_indirect_count":            int64(2),
		"zil_itx_indirect_bytes":            int64(3),
		"zil_itx_copied_count":              int64(4),
		"zil_itx_copied_bytes":              int64(5),
		"zil_itx_needcopy_count":            int64(6),
		"zil_itx_needcopy_bytes":            int64(7),
		"zil_itx_metaslab_normal_count":     int64(8),
		"zil_itx_metaslab_normal_bytes":     int64(9),
		"zil_itx_metaslab_slog_count":       int64(10),
		"zil_itx_metaslab_slog_bytes":       int64(11),
		"fm_erpt-dropped":                   int64(101),
		"fm_erpt-set-failed":                int64(202),
		"fm_fmri-set-failed":                int64(303),
		"fm_payload-set-failed":             int64(404),
		"dmu_tx_assigned":                   int64(39321636),
		"dmu_tx_delay":                      int64(111),
		"dmu_tx_error":                      int64(222),
		"dmu_tx_suspended":                  int64(333),
		"dmu_tx_group":                      int64(444),
		"dmu_tx_memory_reserve":             int64(555),
		"dmu_tx_memory_reclaim":             int64(666),
		"dmu_tx_dirty_throttle":             int64(777),
		"dmu_tx_dirty_delay":                int64(888),
		"dmu_tx_dirty_over_max":             int64(999),
		"dmu_tx_quota":                      int64(101010),
		"abdstats_struct_size":              int64(33840),
		"abdstats_linear_cnt":               int64(834),
		"abdstats_linear_data_size":         int64(989696),
		"abdstats_scatter_cnt":              int64(12),
		"abdstats_scatter_data_size":        int64(187904),
		"abdstats_scatter_chunk_waste":      int64(4608),
		"abdstats_scatter_order_0":          int64(1),
		"abdstats_scatter_order_1":          int64(21),
		"abdstats_scatter_order_2":          int64(11),
		"abdstats_scatter_order_3":          int64(33),
		"abdstats_scatter_order_4":          int64(44),
		"abdstats_scatter_order_5":          int64(76),
		"abdstats_scatter_order_6":          int64(489),
		"abdstats_scatter_order_7":          int64(237483),
		"abdstats_scatter_order_8":          int64(233),
		"abdstats_scatter_order_9":          int64(4411),
		"abdstats_scatter_order_10":         int64(1023),
		"abdstats_scatter_page_multi_chunk": int64(32122),
		"abdstats_scatter_page_multi_zone":  int64(9930),
		"abdstats_scatter_page_alloc_retry": int64(99311),
		"abdstats_scatter_sg_table_retry":   int64(99221),
	}
	arcMetrics := getKstatMetricsArcOnly()
	for k, v := range otherMetrics {
		arcMetrics[k] = v
	}
	return arcMetrics
}

func getPoolMetrics() map[string]interface{} {
	return map[string]interface{}{
		"nread":    int64(1884160),
		"nwritten": int64(6450688),
		"reads":    int64(22),
		"writes":   int64(978),
		"wtime":    int64(272187126),
		"wlentime": int64(2850519036),
		"wupdate":  int64(2263669418655),
		"rtime":    int64(424226814),
		"rlentime": int64(2850519036),
		"rupdate":  int64(2263669871823),
		"wcnt":     int64(0),
		"rcnt":     int64(0),
	}
}

func getPoolMetricsNewFormat() map[string]interface{} {
	return map[string]interface{}{
		"nread":     int64(1884160),
		"nunlinked": int64(14147),
		"nunlinks":  int64(14148),
		"nwritten":  int64(6450688),
		"reads":     int64(22),
		"writes":    int64(978),
	}
}

func getPoolMetricsNewFormatV22() map[string]interface{} {
	return map[string]interface{}{
		"nread":                         int64(1884160),
		"nunlinked":                     int64(14147),
		"nunlinks":                      int64(14148),
		"nwritten":                      int64(6450688),
		"reads":                         int64(22),
		"writes":                        int64(978),
		"zil_commit_count":              int64(1),
		"zil_commit_writer_count":       int64(2),
		"zil_itx_count":                 int64(3),
		"zil_itx_indirect_count":        int64(4),
		"zil_itx_indirect_bytes":        int64(5),
		"zil_itx_copied_count":          int64(6),
		"zil_itx_copied_bytes":          int64(7),
		"zil_itx_needcopy_count":        int64(8),
		"zil_itx_needcopy_bytes":        int64(9),
		"zil_itx_metaslab_normal_count": int64(10),
		"zil_itx_metaslab_normal_bytes": int64(11),
		"zil_itx_metaslab_normal_write": int64(12),
		"zil_itx_metaslab_normal_alloc": int64(13),
		"zil_itx_metaslab_slog_count":   int64(14),
		"zil_itx_metaslab_slog_bytes":   int64(15),
		"zil_itx_metaslab_slog_write":   int64(16),
		"zil_itx_metaslab_slog_alloc":   int64(17),
	}
}
