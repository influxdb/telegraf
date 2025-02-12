//go:build linux

package kernel_vmstat

import (
	"os"
	"testing"

	"github.com/influxdata/telegraf/testutil"

	"github.com/stretchr/testify/require"
)

func TestFullVmStatProcFile(t *testing.T) {
	tmpfile := makeFakeVMStatFile(t, []byte(vmStatFileFull))
	defer os.Remove(tmpfile)

	k := KernelVmstat{
		statFile: tmpfile,
	}

	acc := testutil.Accumulator{}
	require.NoError(t, k.Gather(&acc))

	fields := map[string]interface{}{
		"nr_free_pages":                 int64(78730),
		"nr_inactive_anon":              int64(426259),
		"nr_active_anon":                int64(2515657),
		"nr_inactive_file":              int64(2366791),
		"nr_active_file":                int64(2244914),
		"nr_unevictable":                int64(0),
		"nr_mlock":                      int64(0),
		"nr_anon_pages":                 int64(1358675),
		"nr_mapped":                     int64(558821),
		"nr_file_pages":                 int64(5153546),
		"nr_dirty":                      int64(5690),
		"nr_writeback":                  int64(0),
		"nr_slab_reclaimable":           int64(459806),
		"nr_slab_unreclaimable":         int64(47859),
		"nr_page_table_pages":           int64(11115),
		"nr_kernel_stack":               int64(579),
		"nr_unstable":                   int64(0),
		"nr_bounce":                     int64(0),
		"nr_vmscan_write":               int64(6206),
		"nr_writeback_temp":             int64(0),
		"nr_isolated_anon":              int64(0),
		"nr_isolated_file":              int64(0),
		"nr_shmem":                      int64(541689),
		"numa_hit":                      int64(6690743595),
		"numa_miss":                     int64(0),
		"numa_foreign":                  int64(0),
		"numa_interleave":               int64(35793),
		"numa_local":                    int64(5113399878),
		"numa_other":                    int64(0),
		"nr_anon_transparent_hugepages": int64(2034),
		"pgpgin":                        int64(219717626),
		"pgpgout":                       int64(3495885510),
		"pswpin":                        int64(2092),
		"pswpout":                       int64(6206),
		"pgalloc_dma":                   int64(0),
		"pgalloc_dma32":                 int64(122480220),
		"pgalloc_normal":                int64(5233176719),
		"pgalloc_movable":               int64(0),
		"pgfree":                        int64(5359765021),
		"pgactivate":                    int64(375664931),
		"pgdeactivate":                  int64(122735906),
		"pgfault":                       int64(8699921410),
		"pgmajfault":                    int64(122210),
		"pgrefill_dma":                  int64(0),
		"pgrefill_dma32":                int64(1180010),
		"pgrefill_normal":               int64(119866676),
		"pgrefill_movable":              int64(0),
		"pgsteal_dma":                   int64(0),
		"pgsteal_dma32":                 int64(4466436),
		"pgsteal_normal":                int64(318463755),
		"pgsteal_movable":               int64(0),
		"pgscan_kswapd_dma":             int64(0),
		"pgscan_kswapd_dma32":           int64(4480608),
		"pgscan_kswapd_normal":          int64(287857984),
		"pgscan_kswapd_movable":         int64(0),
		"pgscan_direct_dma":             int64(0),
		"pgscan_direct_dma32":           int64(12256),
		"pgscan_direct_normal":          int64(31501600),
		"pgscan_direct_movable":         int64(0),
		"zone_reclaim_failed":           int64(0),
		"pginodesteal":                  int64(9188431),
		"slabs_scanned":                 int64(93775616),
		"kswapd_steal":                  int64(291534428),
		"kswapd_inodesteal":             int64(29770874),
		"kswapd_low_wmark_hit_quickly":  int64(8756),
		"kswapd_high_wmark_hit_quickly": int64(25439),
		"kswapd_skip_congestion_wait":   int64(0),
		"pageoutrun":                    int64(505006),
		"allocstall":                    int64(81496),
		"pgrotated":                     int64(60620),
		"compact_blocks_moved":          int64(238196),
		"compact_pages_moved":           int64(6370588),
		"compact_pagemigrate_failed":    int64(0),
		"compact_stall":                 int64(142092),
		"compact_fail":                  int64(135220),
		"compact_success":               int64(6872),
		"htlb_buddy_alloc_success":      int64(0),
		"htlb_buddy_alloc_fail":         int64(0),
		"unevictable_pgs_culled":        int64(1531),
		"unevictable_pgs_scanned":       int64(0),
		"unevictable_pgs_rescued":       int64(5426),
		"unevictable_pgs_mlocked":       int64(6988),
		"unevictable_pgs_munlocked":     int64(6988),
		"unevictable_pgs_cleared":       int64(0),
		"unevictable_pgs_stranded":      int64(0),
		"unevictable_pgs_mlockfreed":    int64(0),
		"thp_fault_alloc":               int64(346219),
		"thp_fault_fallback":            int64(895453),
		"thp_collapse_alloc":            int64(24857),
		"thp_collapse_alloc_failed":     int64(102214),
		"thp_split":                     int64(9817),
	}
	acc.AssertContainsFields(t, "kernel_vmstat", fields)
}

func TestPartialVmStatProcFile(t *testing.T) {
	tmpfile := makeFakeVMStatFile(t, []byte(vmStatFilePartial))
	defer os.Remove(tmpfile)

	k := KernelVmstat{
		statFile: tmpfile,
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.NoError(t, err)

	fields := map[string]interface{}{
		"unevictable_pgs_culled":     int64(1531),
		"unevictable_pgs_scanned":    int64(0),
		"unevictable_pgs_rescued":    int64(5426),
		"unevictable_pgs_mlocked":    int64(6988),
		"unevictable_pgs_munlocked":  int64(6988),
		"unevictable_pgs_cleared":    int64(0),
		"unevictable_pgs_stranded":   int64(0),
		"unevictable_pgs_mlockfreed": int64(0),
		"thp_fault_alloc":            int64(346219),
		"thp_fault_fallback":         int64(895453),
		"thp_collapse_alloc":         int64(24857),
		"thp_collapse_alloc_failed":  int64(102214),
		"thp_split":                  int64(9817),
	}
	acc.AssertContainsFields(t, "kernel_vmstat", fields)
}

func TestInvalidVmStatProcFile1(t *testing.T) {
	tmpfile := makeFakeVMStatFile(t, []byte(vmStatFileInvalid))
	defer os.Remove(tmpfile)

	k := KernelVmstat{
		statFile: tmpfile,
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid syntax")
}

func TestNoVmStatProcFile(t *testing.T) {
	tmpfile := makeFakeVMStatFile(t, []byte(vmStatFileInvalid))
	require.NoError(t, os.Remove(tmpfile))

	k := KernelVmstat{
		statFile: tmpfile,
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

const vmStatFileFull = `nr_free_pages 78730
nr_inactive_anon 426259
nr_active_anon 2515657
nr_inactive_file 2366791
nr_active_file 2244914
nr_unevictable 0
nr_mlock 0
nr_anon_pages 1358675
nr_mapped 558821
nr_file_pages 5153546
nr_dirty 5690
nr_writeback 0
nr_slab_reclaimable 459806
nr_slab_unreclaimable 47859
nr_page_table_pages 11115
nr_kernel_stack 579
nr_unstable 0
nr_bounce 0
nr_vmscan_write 6206
nr_writeback_temp 0
nr_isolated_anon 0
nr_isolated_file 0
nr_shmem 541689
numa_hit 6690743595
numa_miss 0
numa_foreign 0
numa_interleave 35793
numa_local 5113399878
numa_other 0
nr_anon_transparent_hugepages 2034
pgpgin 219717626
pgpgout 3495885510
pswpin 2092
pswpout 6206
pgalloc_dma 0
pgalloc_dma32 122480220
pgalloc_normal 5233176719
pgalloc_movable 0
pgfree 5359765021
pgactivate 375664931
pgdeactivate 122735906
pgfault 8699921410
pgmajfault 122210
pgrefill_dma 0
pgrefill_dma32 1180010
pgrefill_normal 119866676
pgrefill_movable 0
pgsteal_dma 0
pgsteal_dma32 4466436
pgsteal_normal 318463755
pgsteal_movable 0
pgscan_kswapd_dma 0
pgscan_kswapd_dma32 4480608
pgscan_kswapd_normal 287857984
pgscan_kswapd_movable 0
pgscan_direct_dma 0
pgscan_direct_dma32 12256
pgscan_direct_normal 31501600
pgscan_direct_movable 0
zone_reclaim_failed 0
pginodesteal 9188431
slabs_scanned 93775616
kswapd_steal 291534428
kswapd_inodesteal 29770874
kswapd_low_wmark_hit_quickly 8756
kswapd_high_wmark_hit_quickly 25439
kswapd_skip_congestion_wait 0
pageoutrun 505006
allocstall 81496
pgrotated 60620
compact_blocks_moved 238196
compact_pages_moved 6370588
compact_pagemigrate_failed 0
compact_stall 142092
compact_fail 135220
compact_success 6872
htlb_buddy_alloc_success 0
htlb_buddy_alloc_fail 0
unevictable_pgs_culled 1531
unevictable_pgs_scanned 0
unevictable_pgs_rescued 5426
unevictable_pgs_mlocked 6988
unevictable_pgs_munlocked 6988
unevictable_pgs_cleared 0
unevictable_pgs_stranded 0
unevictable_pgs_mlockfreed 0
thp_fault_alloc 346219
thp_fault_fallback 895453
thp_collapse_alloc 24857
thp_collapse_alloc_failed 102214
thp_split 9817`

const vmStatFilePartial = `unevictable_pgs_culled 1531
unevictable_pgs_scanned 0
unevictable_pgs_rescued 5426
unevictable_pgs_mlocked 6988
unevictable_pgs_munlocked 6988
unevictable_pgs_cleared 0
unevictable_pgs_stranded 0
unevictable_pgs_mlockfreed 0
thp_fault_alloc 346219
thp_fault_fallback 895453
thp_collapse_alloc 24857
thp_collapse_alloc_failed 102214
thp_split 9817`

// invalid thp_split measurement
const vmStatFileInvalid = `unevictable_pgs_culled 1531
unevictable_pgs_scanned 0
unevictable_pgs_rescued 5426
unevictable_pgs_mlocked 6988
unevictable_pgs_munlocked 6988
unevictable_pgs_cleared 0
unevictable_pgs_stranded 0
unevictable_pgs_mlockfreed 0
thp_fault_alloc 346219
thp_fault_fallback 895453
thp_collapse_alloc 24857
thp_collapse_alloc_failed 102214
thp_split abcd`

func makeFakeVMStatFile(t *testing.T, content []byte) string {
	tmpfile, err := os.CreateTemp(t.TempDir(), "kernel_vmstat_test")
	require.NoError(t, err)

	_, err = tmpfile.Write(content)
	require.NoError(t, err)

	require.NoError(t, tmpfile.Close())

	return tmpfile.Name()
}
