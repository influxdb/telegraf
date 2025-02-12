//go:build linux

package conntrack

import (
	"os"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/net"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/plugins/inputs/system"
	"github.com/influxdata/telegraf/testutil"
)

func restoreDflts(savedFiles, savedDirs []string) {
	dfltFiles = savedFiles
	dfltDirs = savedDirs
}

func TestNoFilesFound(t *testing.T) {
	defer restoreDflts(dfltFiles, dfltDirs)

	dfltFiles = []string{"baz.txt"}
	dfltDirs = []string{"./foo/bar"}
	c := &Conntrack{}
	require.NoError(t, c.Init())
	acc := &testutil.Accumulator{}
	err := c.Gather(acc)

	require.EqualError(t, err, "conntrack input failed to collect metrics, make sure that the kernel module is loaded")
}

func TestDefaultsUsed(t *testing.T) {
	defer restoreDflts(dfltFiles, dfltDirs)
	tmpdir := t.TempDir()

	tmpFile, err := os.CreateTemp(tmpdir, "ip_conntrack_count")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	dfltDirs = []string{tmpdir}
	fname := path.Base(tmpFile.Name())
	dfltFiles = []string{fname}

	count := 1234321
	require.NoError(t, os.WriteFile(tmpFile.Name(), []byte(strconv.Itoa(count)), 0640))
	c := &Conntrack{}
	require.NoError(t, c.Init())
	acc := &testutil.Accumulator{}

	require.NoError(t, c.Gather(acc))
	acc.AssertContainsFields(t, inputName, map[string]interface{}{
		fname: float64(count)})
}

func TestConfigsUsed(t *testing.T) {
	defer restoreDflts(dfltFiles, dfltDirs)
	tmpdir := t.TempDir()

	cntFile, err := os.CreateTemp(tmpdir, "nf_conntrack_count")
	require.NoError(t, err)
	maxFile, err := os.CreateTemp(tmpdir, "nf_conntrack_max")
	require.NoError(t, err)
	defer os.Remove(cntFile.Name())
	defer os.Remove(maxFile.Name())

	dfltDirs = []string{tmpdir}
	cntFname := path.Base(cntFile.Name())
	maxFname := path.Base(maxFile.Name())
	dfltFiles = []string{cntFname, maxFname}

	count := 1234321
	limit := 9999999
	require.NoError(t, os.WriteFile(cntFile.Name(), []byte(strconv.Itoa(count)), 0640))
	require.NoError(t, os.WriteFile(maxFile.Name(), []byte(strconv.Itoa(limit)), 0640))
	c := &Conntrack{}
	require.NoError(t, c.Init())
	acc := &testutil.Accumulator{}

	require.NoError(t, c.Gather(acc))

	fix := func(s string) string {
		return strings.Replace(s, "nf_", "ip_", 1)
	}

	acc.AssertContainsFields(t, inputName,
		map[string]interface{}{
			fix(cntFname): float64(count),
			fix(maxFname): float64(limit),
		})
}

func TestCollectStats(t *testing.T) {
	var mps system.MockPS
	defer mps.AssertExpectations(t)
	var acc testutil.Accumulator

	sts := net.ConntrackStat{
		Entries:       1234,
		Searched:      10,
		Found:         1,
		New:           5,
		Invalid:       43,
		Ignore:        13,
		Delete:        3,
		DeleteList:    5,
		Insert:        9,
		InsertFailed:  20,
		Drop:          49,
		EarlyDrop:     7,
		IcmpError:     21,
		ExpectNew:     12,
		ExpectCreate:  44,
		ExpectDelete:  53,
		SearchRestart: 31,
	}

	mps.On("NetConntrack", false).Return([]net.ConntrackStat{sts}, nil)
	cs := &Conntrack{
		ps:      &mps,
		Collect: []string{"all"},
	}
	require.NoError(t, cs.Init())

	err := cs.Gather(&acc)
	if err != nil && strings.Contains(err.Error(), "Is the conntrack kernel module loaded?") {
		t.Skip("Conntrack kernel module not loaded.")
	}
	require.NoError(t, err)

	expectedTags := map[string]string{
		"cpu": "all",
	}

	expectedFields := map[string]interface{}{
		"entries":        uint32(1234),
		"searched":       uint32(10),
		"found":          uint32(1),
		"new":            uint32(5),
		"invalid":        uint32(43),
		"ignore":         uint32(13),
		"delete":         uint32(3),
		"delete_list":    uint32(5),
		"insert":         uint32(9),
		"insert_failed":  uint32(20),
		"drop":           uint32(49),
		"early_drop":     uint32(7),
		"icmp_error":     uint32(21),
		"expect_new":     uint32(12),
		"expect_create":  uint32(44),
		"expect_delete":  uint32(53),
		"search_restart": uint32(31),
	}

	acc.AssertContainsFields(t, inputName, expectedFields)
	acc.AssertContainsTaggedFields(t, inputName, expectedFields, expectedTags)

	require.Equal(t, 19, acc.NFields())
}

func TestCollectStatsPerCpu(t *testing.T) {
	var mps system.MockPS
	defer mps.AssertExpectations(t)
	var acc testutil.Accumulator

	sts := []net.ConntrackStat{
		{
			Entries:       59,
			Searched:      10,
			Found:         1,
			New:           5,
			Invalid:       43,
			Ignore:        13,
			Delete:        3,
			DeleteList:    5,
			Insert:        9,
			InsertFailed:  20,
			Drop:          49,
			EarlyDrop:     7,
			IcmpError:     21,
			ExpectNew:     12,
			ExpectCreate:  44,
			ExpectDelete:  53,
			SearchRestart: 31,
		},
		{
			Entries:       79,
			Searched:      10,
			Found:         1,
			New:           5,
			Invalid:       43,
			Ignore:        13,
			Delete:        3,
			DeleteList:    5,
			Insert:        9,
			InsertFailed:  10,
			Drop:          49,
			EarlyDrop:     7,
			IcmpError:     21,
			ExpectNew:     12,
			ExpectCreate:  44,
			ExpectDelete:  53,
			SearchRestart: 31,
		},
	}

	mps.On("NetConntrack", true).Return(sts, nil)

	allSts := []net.ConntrackStat{
		{
			Entries:       129,
			Searched:      20,
			Found:         2,
			New:           10,
			Invalid:       86,
			Ignore:        26,
			Delete:        6,
			DeleteList:    10,
			Insert:        18,
			InsertFailed:  40,
			Drop:          98,
			EarlyDrop:     17,
			IcmpError:     42,
			ExpectNew:     24,
			ExpectCreate:  88,
			ExpectDelete:  106,
			SearchRestart: 62,
		},
	}

	mps.On("NetConntrack", false).Return(allSts, nil)

	cs := &Conntrack{
		ps:      &mps,
		Collect: []string{"all", "percpu"},
	}
	require.NoError(t, cs.Init())

	err := cs.Gather(&acc)
	if err != nil && strings.Contains(err.Error(), "Is the conntrack kernel module loaded?") {
		t.Skip("Conntrack kernel module not loaded.")
	}
	require.NoError(t, err)

	// cpu0
	expectedFields := map[string]interface{}{
		"entries":        uint32(59),
		"searched":       uint32(10),
		"found":          uint32(1),
		"new":            uint32(5),
		"invalid":        uint32(43),
		"ignore":         uint32(13),
		"delete":         uint32(3),
		"delete_list":    uint32(5),
		"insert":         uint32(9),
		"insert_failed":  uint32(20),
		"drop":           uint32(49),
		"early_drop":     uint32(7),
		"icmp_error":     uint32(21),
		"expect_new":     uint32(12),
		"expect_create":  uint32(44),
		"expect_delete":  uint32(53),
		"search_restart": uint32(31),
	}

	acc.AssertContainsTaggedFields(t, inputName, expectedFields,
		map[string]string{
			"cpu": "cpu0",
		})

	// cpu1
	expectedFields1 := map[string]interface{}{
		"entries":        uint32(79),
		"searched":       uint32(10),
		"found":          uint32(1),
		"new":            uint32(5),
		"invalid":        uint32(43),
		"ignore":         uint32(13),
		"delete":         uint32(3),
		"delete_list":    uint32(5),
		"insert":         uint32(9),
		"insert_failed":  uint32(10),
		"drop":           uint32(49),
		"early_drop":     uint32(7),
		"icmp_error":     uint32(21),
		"expect_new":     uint32(12),
		"expect_create":  uint32(44),
		"expect_delete":  uint32(53),
		"search_restart": uint32(31),
	}

	acc.AssertContainsTaggedFields(t, inputName, expectedFields1,
		map[string]string{
			"cpu": "cpu1",
		})

	allFields := map[string]interface{}{
		"entries":        uint32(129),
		"searched":       uint32(20),
		"found":          uint32(2),
		"new":            uint32(10),
		"invalid":        uint32(86),
		"ignore":         uint32(26),
		"delete":         uint32(6),
		"delete_list":    uint32(10),
		"insert":         uint32(18),
		"insert_failed":  uint32(40),
		"drop":           uint32(98),
		"early_drop":     uint32(17),
		"icmp_error":     uint32(42),
		"expect_new":     uint32(24),
		"expect_create":  uint32(88),
		"expect_delete":  uint32(106),
		"search_restart": uint32(62),
	}

	acc.AssertContainsTaggedFields(t, inputName, allFields,
		map[string]string{
			"cpu": "all",
		})

	require.Equal(t, 53, acc.NFields())
}

func TestCollectPsSystemInit(t *testing.T) {
	var acc testutil.Accumulator
	cs := &Conntrack{
		ps:      system.NewSystemPS(),
		Collect: []string{"all"},
	}
	require.NoError(t, cs.Init())
	err := cs.Gather(&acc)
	if err != nil && strings.Contains(err.Error(), "Is the conntrack kernel module loaded?") {
		t.Skip("Conntrack kernel module not loaded.")
	}
	// make sure Conntrack.ps gets initialized without mocking
	require.NoError(t, err)
}
