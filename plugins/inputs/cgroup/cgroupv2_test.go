//go:build linux

package cgroup

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestCgroupV2Cpu(t *testing.T) {
	var acc testutil.Accumulator
	var cg = &CGroup{
		Paths:  []string{"testdata/v2"},
		Files:  []string{"cpu.*"},
		logged: make(map[string]bool),
	}

	expected := []telegraf.Metric{
		metric.New(
			"cgroup",
			map[string]string{"path": `testdata/v2`},
			map[string]interface{}{

				"cpu.idle": int64(0),

				"cpu.max.0":     int64(4800000),
				"cpu.max.1":     int64(100000),
				"cpu.max.burst": int64(0),

				"cpu.pressure.full.avg10":  float64(0),
				"cpu.pressure.full.avg300": float64(0.05),
				"cpu.pressure.full.avg60":  float64(0.08),
				"cpu.pressure.full.total":  int64(277111656),
				"cpu.pressure.some.avg10":  float64(0),
				"cpu.pressure.some.avg300": float64(0.06),
				"cpu.pressure.some.avg60":  float64(0.08),
				"cpu.pressure.some.total":  int64(293391454),

				"cpu.stat.burst_usec":                 int64(0),
				"cpu.stat.core_sched.force_idle_usec": int64(0),
				"cpu.stat.nr_bursts":                  int64(0),
				"cpu.stat.nr_periods":                 int64(3936904),
				"cpu.stat.nr_throttled":               int64(6004),
				"cpu.stat.system_usec":                int64(37345608977),
				"cpu.stat.throttled_usec":             int64(19175137007),
				"cpu.stat.usage_usec":                 int64(98701325189),
				"cpu.stat.user_usec":                  int64(61355716211),

				"cpu.weight":      int64(79),
				"cpu.weight.nice": int64(1),
			},
			time.Unix(0, 0),
		),
	}

	require.NoError(t, acc.GatherError(cg.Gather))
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestCgroupV2Memory(t *testing.T) {
	var acc testutil.Accumulator
	var cg = &CGroup{
		Paths:  []string{"testdata/v2"},
		Files:  []string{"memory.*"},
		logged: make(map[string]bool),
	}

	expected := []telegraf.Metric{
		metric.New(
			"cgroup",
			map[string]string{"path": `testdata/v2`},
			map[string]interface{}{
				"memory.current":                               int64(13071106048),
				"memory.events.high":                           int64(0),
				"memory.events.local.high":                     int64(0),
				"memory.events.local.low":                      int64(0),
				"memory.events.local.max":                      int64(0),
				"memory.events.local.oom":                      int64(0),
				"memory.events.local.oom_group_kill":           int64(0),
				"memory.events.local.oom_kill":                 int64(0),
				"memory.events.low":                            int64(0),
				"memory.events.max":                            int64(0),
				"memory.events.oom":                            int64(0),
				"memory.events.oom_group_kill":                 int64(0),
				"memory.events.oom_kill":                       int64(0),
				"memory.high":                                  "max",
				"memory.low":                                   int64(0),
				"memory.max":                                   int64(103079215104),
				"memory.min":                                   int64(0),
				"memory.numa_stat.active_anon.N0":              int64(81920),
				"memory.numa_stat.active_anon.N1":              int64(98304),
				"memory.numa_stat.active_file.N0":              int64(2946760704),
				"memory.numa_stat.active_file.N1":              int64(2650640384),
				"memory.numa_stat.anon.N0":                     int64(1330585600),
				"memory.numa_stat.anon.N1":                     int64(1141161984),
				"memory.numa_stat.anon_thp.N0":                 int64(0),
				"memory.numa_stat.anon_thp.N1":                 int64(2097152),
				"memory.numa_stat.file.N0":                     int64(4531773440),
				"memory.numa_stat.file.N1":                     int64(4001075200),
				"memory.numa_stat.file_dirty.N0":               int64(258048),
				"memory.numa_stat.file_dirty.N1":               int64(45056),
				"memory.numa_stat.file_mapped.N0":              int64(10272768),
				"memory.numa_stat.file_mapped.N1":              int64(3940352),
				"memory.numa_stat.file_thp.N0":                 int64(0),
				"memory.numa_stat.file_thp.N1":                 int64(0),
				"memory.numa_stat.file_writeback.N0":           int64(0),
				"memory.numa_stat.file_writeback.N1":           int64(0),
				"memory.numa_stat.inactive_anon.N0":            int64(1330479104),
				"memory.numa_stat.inactive_anon.N1":            int64(1141067776),
				"memory.numa_stat.inactive_file.N0":            int64(1584979968),
				"memory.numa_stat.inactive_file.N1":            int64(1350430720),
				"memory.numa_stat.kernel_stack.N0":             int64(4161536),
				"memory.numa_stat.kernel_stack.N1":             int64(5537792),
				"memory.numa_stat.pagetables.N0":               int64(7839744),
				"memory.numa_stat.pagetables.N1":               int64(8462336),
				"memory.numa_stat.sec_pagetables.N0":           int64(0),
				"memory.numa_stat.sec_pagetables.N1":           int64(0),
				"memory.numa_stat.shmem.N0":                    int64(0),
				"memory.numa_stat.shmem.N1":                    int64(4096),
				"memory.numa_stat.shmem_thp.N0":                int64(0),
				"memory.numa_stat.shmem_thp.N1":                int64(0),
				"memory.numa_stat.slab_reclaimable.N0":         int64(950447920),
				"memory.numa_stat.slab_reclaimable.N1":         int64(1081869088),
				"memory.numa_stat.slab_unreclaimable.N0":       int64(2654816),
				"memory.numa_stat.slab_unreclaimable.N1":       int64(2661512),
				"memory.numa_stat.swapcached.N0":               int64(0),
				"memory.numa_stat.swapcached.N1":               int64(0),
				"memory.numa_stat.unevictable.N0":              int64(0),
				"memory.numa_stat.unevictable.N1":              int64(0),
				"memory.numa_stat.workingset_activate_anon.N0": int64(0),
				"memory.numa_stat.workingset_activate_anon.N1": int64(0),
				"memory.numa_stat.workingset_activate_file.N0": int64(40145),
				"memory.numa_stat.workingset_activate_file.N1": int64(65541),
				"memory.numa_stat.workingset_nodereclaim.N0":   int64(0),
				"memory.numa_stat.workingset_nodereclaim.N1":   int64(0),
				"memory.numa_stat.workingset_refault_anon.N0":  int64(0),
				"memory.numa_stat.workingset_refault_anon.N1":  int64(0),
				"memory.numa_stat.workingset_refault_file.N0":  int64(346752),
				"memory.numa_stat.workingset_refault_file.N1":  int64(282604),
				"memory.numa_stat.workingset_restore_anon.N0":  int64(0),
				"memory.numa_stat.workingset_restore_anon.N1":  int64(0),
				"memory.numa_stat.workingset_restore_file.N0":  int64(19386),
				"memory.numa_stat.workingset_restore_file.N1":  int64(10010),
				"memory.oom.group":                             int64(1),
				"memory.peak":                                  int64(87302021120),
				"memory.pressure.full.avg10":                   float64(0),
				"memory.pressure.full.avg300":                  float64(0),
				"memory.pressure.full.avg60":                   float64(0),
				"memory.pressure.full.total":                   int64(250662),
				"memory.pressure.some.avg10":                   float64(0),
				"memory.pressure.some.avg300":                  float64(0),
				"memory.pressure.some.avg60":                   float64(0),
				"memory.pressure.some.total":                   int64(250773),
				"memory.stat.active_anon":                      int64(180224),
				"memory.stat.active_file":                      int64(5597401088),
				"memory.stat.anon":                             int64(2471755776),
				"memory.stat.anon_thp":                         int64(2097152),
				"memory.stat.file":                             int64(8532865024),
				"memory.stat.file_dirty":                       int64(319488),
				"memory.stat.file_mapped":                      int64(14213120),
				"memory.stat.file_thp":                         int64(0),
				"memory.stat.file_writeback":                   int64(0),
				"memory.stat.inactive_anon":                    int64(2471559168),
				"memory.stat.inactive_file":                    int64(2935459840),
				"memory.stat.kernel":                           int64(2065149952),
				"memory.stat.kernel_stack":                     int64(9699328),
				"memory.stat.pagetables":                       int64(16302080),
				"memory.stat.percpu":                           int64(3528),
				"memory.stat.pgactivate":                       int64(13516655),
				"memory.stat.pgdeactivate":                     int64(9151751),
				"memory.stat.pgfault":                          int64(1973187551),
				"memory.stat.pglazyfree":                       int64(5549),
				"memory.stat.pglazyfreed":                      int64(1),
				"memory.stat.pgmajfault":                       int64(8497),
				"memory.stat.pgrefill":                         int64(9153617),
				"memory.stat.pgscan":                           int64(12149209),
				"memory.stat.pgscan_direct":                    int64(4436521),
				"memory.stat.pgscan_kswapd":                    int64(7712688),
				"memory.stat.pgsteal":                          int64(12139915),
				"memory.stat.pgsteal_direct":                   int64(4429690),
				"memory.stat.pgsteal_kswapd":                   int64(7710225),
				"memory.stat.sec_pagetables":                   int64(0),
				"memory.stat.shmem":                            int64(4096),
				"memory.stat.shmem_thp":                        int64(0),
				"memory.stat.slab":                             int64(2037641160),
				"memory.stat.slab_reclaimable":                 int64(2032322192),
				"memory.stat.slab_unreclaimable":               int64(5318968),
				"memory.stat.sock":                             int64(0),
				"memory.stat.swapcached":                       int64(0),
				"memory.stat.thp_collapse_alloc":               int64(3),
				"memory.stat.thp_fault_alloc":                  int64(13),
				"memory.stat.unevictable":                      int64(0),
				"memory.stat.vmalloc":                          int64(0),
				"memory.stat.workingset_activate_anon":         int64(0),
				"memory.stat.workingset_activate_file":         int64(105686),
				"memory.stat.workingset_nodereclaim":           int64(0),
				"memory.stat.workingset_refault_anon":          int64(0),
				"memory.stat.workingset_refault_file":          int64(629356),
				"memory.stat.workingset_restore_anon":          int64(0),
				"memory.stat.workingset_restore_file":          int64(29396),
				"memory.swap.current":                          int64(0),
				"memory.swap.events.fail":                      int64(0),
				"memory.swap.events.high":                      int64(0),
				"memory.swap.events.max":                       int64(0),
				"memory.swap.high":                             "max",
				"memory.swap.max":                              int64(0),
			},
			time.Unix(0, 0),
		),
	}

	require.NoError(t, acc.GatherError(cg.Gather))
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}
func TestCgroupV2Io(t *testing.T) {
	var acc testutil.Accumulator
	var cg = &CGroup{
		Paths:  []string{"testdata/v2"},
		Files:  []string{"io.*"},
		logged: make(map[string]bool),
	}

	expected := []telegraf.Metric{
		metric.New(
			"cgroup",
			map[string]string{"path": `testdata/v2`},
			map[string]interface{}{
				"io.bfq.weight.default":   int64(100),
				"io.pressure.full.avg10":  float64(0),
				"io.pressure.full.avg300": float64(0),
				"io.pressure.full.avg60":  float64(0),
				"io.pressure.full.total":  184607952,
				"io.pressure.some.avg10":  float64(0),
				"io.pressure.some.avg300": float64(0),
				"io.pressure.some.avg60":  float64(0),
				"io.pressure.some.total":  185162400,
				"io.stat.259:8.dbytes":    int64(0),
				"io.stat.259:8.dios":      int64(0),
				"io.stat.259:8.rbytes":    int64(74526720),
				"io.stat.259:8.rios":      int64(2936),
				"io.stat.259:8.wbytes":    int64(3789381632),
				"io.stat.259:8.wios":      int64(181928),
			},
			time.Unix(0, 0),
		),
	}

	require.NoError(t, acc.GatherError(cg.Gather))
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestCgroupV2Hugetlb(t *testing.T) {
	var acc testutil.Accumulator
	var cg = &CGroup{
		Paths:  []string{"testdata/v2"},
		Files:  []string{"hugetlb.*"},
		logged: make(map[string]bool),
	}

	expected := []telegraf.Metric{
		metric.New(
			"cgroup",
			map[string]string{"path": `testdata/v2`},
			map[string]interface{}{
				"hugetlb.1GB.current":         int64(0),
				"hugetlb.1GB.events.0":        "max",
				"hugetlb.1GB.events.1":        int64(0),
				"hugetlb.1GB.events.local.0":  "max",
				"hugetlb.1GB.events.local.1":  int64(0),
				"hugetlb.1GB.max":             "max",
				"hugetlb.1GB.numa_stat.N0":    int64(0),
				"hugetlb.1GB.numa_stat.N1":    int64(0),
				"hugetlb.1GB.numa_stat.total": int64(0),
				"hugetlb.1GB.rsvd.current":    int64(0),
				"hugetlb.1GB.rsvd.max":        "max",
				"hugetlb.2MB.current":         int64(0),
				"hugetlb.2MB.events.0":        "max",
				"hugetlb.2MB.events.1":        int64(0),
				"hugetlb.2MB.events.local.0":  "max",
				"hugetlb.2MB.events.local.1":  int64(0),
				"hugetlb.2MB.max":             "max",
				"hugetlb.2MB.numa_stat.N0":    int64(0),
				"hugetlb.2MB.numa_stat.N1":    int64(0),
				"hugetlb.2MB.numa_stat.total": int64(0),
				"hugetlb.2MB.rsvd.current":    int64(0),
				"hugetlb.2MB.rsvd.max":        "max",
			},
			time.Unix(0, 0),
		),
	}

	require.NoError(t, acc.GatherError(cg.Gather))
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestCgroupV2Pids(t *testing.T) {
	var acc testutil.Accumulator
	var cg = &CGroup{
		Paths:  []string{"testdata/v2"},
		Files:  []string{"pids.*"},
		logged: make(map[string]bool),
	}

	expected := []telegraf.Metric{
		metric.New(
			"cgroup",
			map[string]string{"path": `testdata/v2`},
			map[string]interface{}{
				"pids.current":  int64(592),
				"pids.events.0": "max",
				"pids.events.1": int64(0),
				"pids.max":      int64(629145),
				"pids.peak":     int64(2438),
			},
			time.Unix(0, 0),
		),
	}

	require.NoError(t, acc.GatherError(cg.Gather))
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}
