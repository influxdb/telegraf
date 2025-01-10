//go:build linux

package kernel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

func TestGetProcValueInt(t *testing.T) {
	d, err := getProcValueInt("testdata/entropy_stat_file_full")
	require.NoError(t, err)
	require.IsType(t, int64(1), d)
}

func TestGetProcValueByte(t *testing.T) {
	d, err := getProcValueBytes("testdata/entropy_stat_file_full")
	require.NoError(t, err)
	require.IsType(t, []byte("test"), d)
}

func TestFullProcFile(t *testing.T) {
	k := Kernel{
		statFile:        "testdata/stat_file_full",
		entropyStatFile: "testdata/entropy_stat_file_full",
	}

	acc := testutil.Accumulator{}
	require.NoError(t, k.Gather(&acc))

	expected := []telegraf.Metric{
		metric.New(
			"kernel",
			map[string]string{},
			map[string]interface{}{
				"boot_time":        int64(1457505775),
				"context_switches": int64(2626618),
				"disk_pages_in":    int64(5741),
				"disk_pages_out":   int64(1808),
				"interrupts":       int64(1472736),
				"processes_forked": int64(10673),
				"entropy_avail":    int64(1024),
			},
			time.Unix(0, 0),
			1,
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestPartialProcFile(t *testing.T) {
	k := Kernel{
		statFile:        "testdata/stat_file_partial",
		entropyStatFile: "testdata/entropy_stat_file_partial",
	}

	acc := testutil.Accumulator{}
	require.NoError(t, k.Gather(&acc))

	fields := map[string]interface{}{
		"boot_time":        int64(1457505775),
		"context_switches": int64(2626618),
		"disk_pages_in":    int64(5741),
		"disk_pages_out":   int64(1808),
		"interrupts":       int64(1472736),
		"entropy_avail":    int64(1024),
	}
	acc.AssertContainsFields(t, "kernel", fields)
}

func TestInvalidProcFile1(t *testing.T) {
	// missing btime measurement
	k := Kernel{
		statFile:        "testdata/stat_file_invalid",
		entropyStatFile: "testdata/entropy_stat_file_invalid",
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid syntax")
}

func TestInvalidProcFile2(t *testing.T) {
	// missing second page measurement
	k := Kernel{
		statFile: "testdata/stat_file_invalid2",
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

func TestNoProcFile(t *testing.T) {
	k := Kernel{
		statFile: "testdata/this_file_does_not_exist",
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

func TestInvalidCollectOption(t *testing.T) {
	k := Kernel{
		statFile:        "testdata/stat_file_full",
		entropyStatFile: "testdata/entropy_stat_file_full",
		ConfigCollect:   []string{"invalidOption"},
	}

	acc := testutil.Accumulator{}

	require.NoError(t, k.Init())
	require.NoError(t, k.Gather(&acc))
}

func TestKsmEnabledValidKsmDirectory(t *testing.T) {
	k := Kernel{
		statFile:        "testdata/stat_file_full",
		entropyStatFile: "testdata/entropy_stat_file_full",
		ksmStatsDir:     "testdata/ksm/valid",
		ConfigCollect:   []string{"ksm"},
	}

	require.NoError(t, k.Init())

	acc := testutil.Accumulator{}
	require.NoError(t, k.Gather(&acc))

	expected := []telegraf.Metric{
		metric.New(
			"kernel",
			map[string]string{},
			map[string]interface{}{
				"boot_time":                              int64(1457505775),
				"context_switches":                       int64(2626618),
				"disk_pages_in":                          int64(5741),
				"disk_pages_out":                         int64(1808),
				"interrupts":                             int64(1472736),
				"processes_forked":                       int64(10673),
				"entropy_avail":                          int64(1024),
				"ksm_full_scans":                         int64(123),
				"ksm_max_page_sharing":                   int64(10000),
				"ksm_merge_across_nodes":                 int64(1),
				"ksm_pages_shared":                       int64(12922),
				"ksm_pages_sharing":                      int64(28384),
				"ksm_pages_to_scan":                      int64(12928),
				"ksm_pages_unshared":                     int64(92847),
				"ksm_pages_volatile":                     int64(2824171),
				"ksm_run":                                int64(1),
				"ksm_sleep_millisecs":                    int64(1000),
				"ksm_stable_node_chains":                 int64(0),
				"ksm_stable_node_chains_prune_millisecs": int64(0),
				"ksm_stable_node_dups":                   int64(0),
				"ksm_use_zero_pages":                     int64(1),
			},
			time.Unix(0, 0),
			1,
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestKSMEnabledMissingFile(t *testing.T) {
	k := Kernel{
		statFile:        "/proc/stat",
		entropyStatFile: "/proc/sys/kernel/random/entropy_avail",
		ksmStatsDir:     "testdata/ksm/missing",
		ConfigCollect:   []string{"ksm"},
	}

	require.NoError(t, k.Init())

	acc := testutil.Accumulator{}
	require.ErrorContains(t, k.Gather(&acc), "does not exist")
}

func TestKSMEnabledWrongDir(t *testing.T) {
	k := Kernel{
		ksmStatsDir:   "testdata/this_file_does_not_exist",
		ConfigCollect: []string{"ksm"},
	}

	require.ErrorContains(t, k.Init(), "KSM is not enabled in this kernel")
}

func TestKSMDisabledNoKSMTags(t *testing.T) {
	k := Kernel{
		statFile:        "testdata/stat_file_full",
		entropyStatFile: "testdata/entropy_stat_file_full",
		ksmStatsDir:     "testdata/this_file_does_not_exist",
	}

	acc := testutil.Accumulator{}

	require.NoError(t, k.Init())
	require.NoError(t, k.Gather(&acc))
	require.False(t, acc.HasField("kernel", "ksm_run"))
}
