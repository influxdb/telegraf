//go:build linux

package lustre2

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/influxdata/toml"
	"github.com/influxdata/toml/ast"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

// Set config file variables to point to fake directory structure instead of /proc?

const obdfilterProcContents = `snapshot_time             1438693064.430544 secs.usecs
read_bytes                203238095 samples [bytes] 4096 1048576 78026117632000
write_bytes               71893382 samples [bytes] 1 1048576 15201500833981
get_info                  1182008495 samples [reqs]
set_info_async            2 samples [reqs]
connect                   1117 samples [reqs]
reconnect                 1160 samples [reqs]
disconnect                1084 samples [reqs]
statfs                    3575885 samples [reqs]
create                    698 samples [reqs]
destroy                   3190060 samples [reqs]
setattr                   605647 samples [reqs]
punch                     805187 samples [reqs]
sync                      6608753 samples [reqs]
preprw                    275131477 samples [reqs]
commitrw                  275131477 samples [reqs]
quotactl                  229231 samples [reqs]
ping                      78020757 samples [reqs]
`

const osdldiskfsProcContents = `snapshot_time             1438693135.640551 secs.usecs
get_page                  275132812 samples [usec] 0 3147 1320420955 22041662259
cache_access              19047063027 samples [pages] 1 1 19047063027
cache_hit                 7393729777 samples [pages] 1 1 7393729777
cache_miss                11653333250 samples [pages] 1 1 11653333250
`

const obdfilterJobStatsContents = `job_stats:
- job_id:          cluster-testjob1
  snapshot_time:   1461772761
  read_bytes:      { samples:           1, unit: bytes, min:    4096, max:    4096, sum:            4096 }
  write_bytes:     { samples:          25, unit: bytes, min: 1048576, max:16777216, sum:        26214400 }
  getattr:         { samples:           0, unit:  reqs }
  setattr:         { samples:           0, unit:  reqs }
  punch:           { samples:           1, unit:  reqs }
  sync:            { samples:           0, unit:  reqs }
  destroy:         { samples:           0, unit:  reqs }
  create:          { samples:           0, unit:  reqs }
  statfs:          { samples:           0, unit:  reqs }
  get_info:        { samples:           0, unit:  reqs }
  set_info:        { samples:           0, unit:  reqs }
  quotactl:        { samples:           0, unit:  reqs }
- job_id:          testjob2
  snapshot_time:   1461772761
  read_bytes:      { samples:           1, unit: bytes, min:    1024, max:    1024, sum:            1024 }
  write_bytes:     { samples:          25, unit: bytes, min:    2048, max:    2048, sum:           51200 }
  getattr:         { samples:           0, unit:  reqs }
  setattr:         { samples:           0, unit:  reqs }
  punch:           { samples:           1, unit:  reqs }
  sync:            { samples:           0, unit:  reqs }
  destroy:         { samples:           0, unit:  reqs }
  create:          { samples:           0, unit:  reqs }
  statfs:          { samples:           0, unit:  reqs }
  get_info:        { samples:           0, unit:  reqs }
  set_info:        { samples:           0, unit:  reqs }
  quotactl:        { samples:           0, unit:  reqs }
`

const mdtProcContents = `snapshot_time             1438693238.20113 secs.usecs
open                      1024577037 samples [reqs]
close                     873243496 samples [reqs]
mknod                     349042 samples [reqs]
link                      445 samples [reqs]
unlink                    3549417 samples [reqs]
mkdir                     705499 samples [reqs]
rmdir                     227434 samples [reqs]
rename                    629196 samples [reqs]
getattr                   1503663097 samples [reqs]
setattr                   1898364 samples [reqs]
getxattr                  6145349681 samples [reqs]
setxattr                  83969 samples [reqs]
statfs                    2916320 samples [reqs]
sync                      434081 samples [reqs]
samedir_rename            259625 samples [reqs]
crossdir_rename           369571 samples [reqs]
`

const mdtJobStatsContents = `job_stats:
- job_id:          cluster-testjob1
  snapshot_time:   1461772761
  open:            { samples:           5, unit:  reqs }
  close:           { samples:           4, unit:  reqs }
  mknod:           { samples:           6, unit:  reqs }
  link:            { samples:           8, unit:  reqs }
  unlink:          { samples:          90, unit:  reqs }
  mkdir:           { samples:         521, unit:  reqs }
  rmdir:           { samples:         520, unit:  reqs }
  rename:          { samples:           9, unit:  reqs }
  getattr:         { samples:          11, unit:  reqs }
  setattr:         { samples:           1, unit:  reqs }
  getxattr:        { samples:           3, unit:  reqs }
  setxattr:        { samples:           4, unit:  reqs }
  statfs:          { samples:        1205, unit:  reqs }
  sync:            { samples:           2, unit:  reqs }
  samedir_rename:  { samples:         705, unit:  reqs }
  crossdir_rename: { samples:         200, unit:  reqs }
- job_id:          testjob2
  snapshot_time:   1461772761
  open:            { samples:           6, unit:  reqs }
  close:           { samples:           7, unit:  reqs }
  mknod:           { samples:           8, unit:  reqs }
  link:            { samples:           9, unit:  reqs }
  unlink:          { samples:          20, unit:  reqs }
  mkdir:           { samples:         200, unit:  reqs }
  rmdir:           { samples:         210, unit:  reqs }
  rename:          { samples:           8, unit:  reqs }
  getattr:         { samples:          10, unit:  reqs }
  setattr:         { samples:           2, unit:  reqs }
  getxattr:        { samples:           4, unit:  reqs }
  setxattr:        { samples:           5, unit:  reqs }
  statfs:          { samples:        1207, unit:  reqs }
  sync:            { samples:           3, unit:  reqs }
  samedir_rename:  { samples:         706, unit:  reqs }
  crossdir_rename: { samples:         201, unit:  reqs }
`

// Subset of a brw_stats file. Contains all headers, with representative buckets.
const brwstatsProcContents = `snapshot_time:         1589909588.327213269 (secs.nsecs)
                           read      |     write
pages per bulk r/w     rpcs  % cum % |  rpcs        % cum %
1:                    5271   0   0   | 337023  22  22
2:                    3030   0   0   | 5672   0  23
4:                    4449   0   0   | 255983  17  40
8:                    2780   0   0   | 33612   2  42
                           read      |     write
discontiguous pages    rpcs  % cum % |  rpcs        % cum %
0:                43942683 100 100   | 337023  22  22
1:                       0   0 100   | 5672   0  23
2:                       0   0 100   | 28016   1  24
3:                       0   0 100   | 227967  15  40
4:                       0   0 100   | 12869   0  41
                           read      |     write
disk I/Os in flight    ios   % cum % |  ios         % cum %
1:                 2892221   6   6   | 1437946  96  96
2:                 2763141   6  12   | 44373   2  99
3:                 3014304   6  19   | 2677   0  99
4:                 3212360   7  27   |  183   0  99
                           read      |     write
I/O time (1/1000s)     ios   % cum % |  ios         % cum %
1:                  521780   1   1   |    0   0   0
16:                6035560  16  22   |    0   0   0
128:               5044958  14  98   |    0   0   0
1K:                    651   0  99   |    0   0   0
                           read      |     write
disk I/O size          ios   % cum % |  ios         % cum %
1:                       0   0   0   | 327301  22  22
16:                      0   0   0   |    0   0  22
128:                    35   0   0   |  209   0  22
1K:                      0   0   0   | 1703   0  22
16K:                  4449   0   0   | 255983  17  40
128K:                  855   0   0   |   23   0  42
1M:               43866371  99 100   | 850248  57 100
`

func TestLustre2GeneratesHealth(t *testing.T) {
	tmpDir := t.TempDir()

	rootdir := tmpDir + "/telegraf"
	sysdir := rootdir + "/sys/fs/lustre/"
	err := os.MkdirAll(sysdir, 0750)
	require.NoError(t, err)

	err = os.WriteFile(sysdir+"health_check", []byte("healthy\n"), 0640)
	require.NoError(t, err)

	m := &Lustre2{rootdir: rootdir}

	var acc testutil.Accumulator

	err = m.Gather(&acc)
	require.NoError(t, err)

	acc.AssertContainsTaggedFields(
		t,
		"lustre2",
		map[string]interface{}{
			"health": uint64(1),
		},
		map[string]string{},
	)
}

func TestLustre2GeneratesMetrics(t *testing.T) {
	tmpDir := t.TempDir()

	rootdir := tmpDir + "/telegraf"
	tempdir := rootdir + "/proc/fs/lustre/"
	ostName := "OST0001"

	mdtdir := tempdir + "/mdt/"
	err := os.MkdirAll(mdtdir+"/"+ostName, 0750)
	require.NoError(t, err)

	osddir := tempdir + "/osd-ldiskfs/"
	err = os.MkdirAll(osddir+"/"+ostName, 0750)
	require.NoError(t, err)

	obddir := tempdir + "/obdfilter/"
	err = os.MkdirAll(obddir+"/"+ostName, 0750)
	require.NoError(t, err)

	err = os.WriteFile(mdtdir+"/"+ostName+"/md_stats", []byte(mdtProcContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(osddir+"/"+ostName+"/stats", []byte(osdldiskfsProcContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(obddir+"/"+ostName+"/stats", []byte(obdfilterProcContents), 0640)
	require.NoError(t, err)

	// Begin by testing standard Lustre stats
	m := &Lustre2{rootdir: rootdir}

	var acc testutil.Accumulator

	err = m.Gather(&acc)
	require.NoError(t, err)

	tags := map[string]string{
		"name": ostName,
	}

	fields := map[string]interface{}{
		"cache_access":    uint64(19047063027),
		"cache_hit":       uint64(7393729777),
		"cache_miss":      uint64(11653333250),
		"close":           uint64(873243496),
		"crossdir_rename": uint64(369571),
		"getattr":         uint64(1503663097),
		"getxattr":        uint64(6145349681),
		"link":            uint64(445),
		"mkdir":           uint64(705499),
		"mknod":           uint64(349042),
		"open":            uint64(1024577037),
		"read_bytes":      uint64(78026117632000),
		"read_calls":      uint64(203238095),
		"rename":          uint64(629196),
		"rmdir":           uint64(227434),
		"samedir_rename":  uint64(259625),
		"setattr":         uint64(1898364),
		"setxattr":        uint64(83969),
		"statfs":          uint64(2916320),
		"sync":            uint64(434081),
		"unlink":          uint64(3549417),
		"write_bytes":     uint64(15201500833981),
		"write_calls":     uint64(71893382),
	}

	acc.AssertContainsTaggedFields(t, "lustre2", fields, tags)
}

func TestLustre2GeneratesClientMetrics(t *testing.T) {
	tmpDir := t.TempDir()

	rootdir := tmpDir + "/telegraf"
	tempdir := rootdir + "/proc/fs/lustre/"
	ostName := "OST0001"
	clientName := "10.2.4.27@o2ib1"
	mdtdir := tempdir + "/mdt/"
	err := os.MkdirAll(mdtdir+"/"+ostName+"/exports/"+clientName, 0750)
	require.NoError(t, err)

	obddir := tempdir + "/obdfilter/"
	err = os.MkdirAll(obddir+"/"+ostName+"/exports/"+clientName, 0750)
	require.NoError(t, err)

	err = os.WriteFile(mdtdir+"/"+ostName+"/exports/"+clientName+"/stats", []byte(mdtProcContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(obddir+"/"+ostName+"/exports/"+clientName+"/stats", []byte(obdfilterProcContents), 0640)
	require.NoError(t, err)

	// Begin by testing standard Lustre stats
	m := &Lustre2{
		OstProcfiles: []string{obddir + "/*/exports/*/stats"},
		MdsProcfiles: []string{mdtdir + "/*/exports/*/stats"},
	}

	var acc testutil.Accumulator

	err = m.Gather(&acc)
	require.NoError(t, err)

	tags := map[string]string{
		"name":   ostName,
		"client": clientName,
	}

	fields := map[string]interface{}{
		"close":           uint64(873243496),
		"crossdir_rename": uint64(369571),
		"getattr":         uint64(1503663097),
		"getxattr":        uint64(6145349681),
		"link":            uint64(445),
		"mkdir":           uint64(705499),
		"mknod":           uint64(349042),
		"open":            uint64(1024577037),
		"read_bytes":      uint64(78026117632000),
		"read_calls":      uint64(203238095),
		"rename":          uint64(629196),
		"rmdir":           uint64(227434),
		"samedir_rename":  uint64(259625),
		"setattr":         uint64(1898364),
		"setxattr":        uint64(83969),
		"statfs":          uint64(2916320),
		"sync":            uint64(434081),
		"unlink":          uint64(3549417),
		"write_bytes":     uint64(15201500833981),
		"write_calls":     uint64(71893382),
	}

	acc.AssertContainsTaggedFields(t, "lustre2", fields, tags)
}

func TestLustre2GeneratesJobstatsMetrics(t *testing.T) {
	tmpDir := t.TempDir()

	rootdir := tmpDir + "/telegraf"
	tempdir := rootdir + "/proc/fs/lustre/"
	ostName := "OST0001"
	jobNames := []string{"cluster-testjob1", "testjob2"}

	mdtdir := tempdir + "/mdt/"
	err := os.MkdirAll(mdtdir+"/"+ostName, 0750)
	require.NoError(t, err)

	obddir := tempdir + "/obdfilter/"
	err = os.MkdirAll(obddir+"/"+ostName, 0750)
	require.NoError(t, err)

	err = os.WriteFile(mdtdir+"/"+ostName+"/job_stats", []byte(mdtJobStatsContents), 0640)
	require.NoError(t, err)

	err = os.WriteFile(obddir+"/"+ostName+"/job_stats", []byte(obdfilterJobStatsContents), 0640)
	require.NoError(t, err)

	// Test Lustre Jobstats
	m := &Lustre2{rootdir: rootdir}

	var acc testutil.Accumulator

	err = m.Gather(&acc)
	require.NoError(t, err)

	// make this two tags
	// and even further make this dependent on summing per OST
	tags := []map[string]string{
		{
			"name":  ostName,
			"jobid": jobNames[0],
		},
		{
			"name":  ostName,
			"jobid": jobNames[1],
		},
	}

	// make this for two tags
	fields := []map[string]interface{}{
		{
			"jobstats_read_calls":      uint64(1),
			"jobstats_read_min_size":   uint64(4096),
			"jobstats_read_max_size":   uint64(4096),
			"jobstats_read_bytes":      uint64(4096),
			"jobstats_write_calls":     uint64(25),
			"jobstats_write_min_size":  uint64(1048576),
			"jobstats_write_max_size":  uint64(16777216),
			"jobstats_write_bytes":     uint64(26214400),
			"jobstats_ost_getattr":     uint64(0),
			"jobstats_ost_setattr":     uint64(0),
			"jobstats_punch":           uint64(1),
			"jobstats_ost_sync":        uint64(0),
			"jobstats_destroy":         uint64(0),
			"jobstats_create":          uint64(0),
			"jobstats_ost_statfs":      uint64(0),
			"jobstats_get_info":        uint64(0),
			"jobstats_set_info":        uint64(0),
			"jobstats_quotactl":        uint64(0),
			"jobstats_open":            uint64(5),
			"jobstats_close":           uint64(4),
			"jobstats_mknod":           uint64(6),
			"jobstats_link":            uint64(8),
			"jobstats_unlink":          uint64(90),
			"jobstats_mkdir":           uint64(521),
			"jobstats_rmdir":           uint64(520),
			"jobstats_rename":          uint64(9),
			"jobstats_getattr":         uint64(11),
			"jobstats_setattr":         uint64(1),
			"jobstats_getxattr":        uint64(3),
			"jobstats_setxattr":        uint64(4),
			"jobstats_statfs":          uint64(1205),
			"jobstats_sync":            uint64(2),
			"jobstats_samedir_rename":  uint64(705),
			"jobstats_crossdir_rename": uint64(200),
		},
		{
			"jobstats_read_calls":      uint64(1),
			"jobstats_read_min_size":   uint64(1024),
			"jobstats_read_max_size":   uint64(1024),
			"jobstats_read_bytes":      uint64(1024),
			"jobstats_write_calls":     uint64(25),
			"jobstats_write_min_size":  uint64(2048),
			"jobstats_write_max_size":  uint64(2048),
			"jobstats_write_bytes":     uint64(51200),
			"jobstats_ost_getattr":     uint64(0),
			"jobstats_ost_setattr":     uint64(0),
			"jobstats_punch":           uint64(1),
			"jobstats_ost_sync":        uint64(0),
			"jobstats_destroy":         uint64(0),
			"jobstats_create":          uint64(0),
			"jobstats_ost_statfs":      uint64(0),
			"jobstats_get_info":        uint64(0),
			"jobstats_set_info":        uint64(0),
			"jobstats_quotactl":        uint64(0),
			"jobstats_open":            uint64(6),
			"jobstats_close":           uint64(7),
			"jobstats_mknod":           uint64(8),
			"jobstats_link":            uint64(9),
			"jobstats_unlink":          uint64(20),
			"jobstats_mkdir":           uint64(200),
			"jobstats_rmdir":           uint64(210),
			"jobstats_rename":          uint64(8),
			"jobstats_getattr":         uint64(10),
			"jobstats_setattr":         uint64(2),
			"jobstats_getxattr":        uint64(4),
			"jobstats_setxattr":        uint64(5),
			"jobstats_statfs":          uint64(1207),
			"jobstats_sync":            uint64(3),
			"jobstats_samedir_rename":  uint64(706),
			"jobstats_crossdir_rename": uint64(201),
		},
	}

	for index := 0; index < len(fields); index++ {
		acc.AssertContainsTaggedFields(t, "lustre2", fields[index], tags[index])
	}
}

func TestLustre2CanParseConfiguration(t *testing.T) {
	config := []byte(`
[[inputs.lustre2]]
   ost_procfiles = [
     "/proc/fs/lustre/obdfilter/*/stats",
     "/proc/fs/lustre/osd-ldiskfs/*/stats",
   ]
   mds_procfiles = [
     "/proc/fs/lustre/mdt/*/md_stats",
   ]`)

	table, err := toml.Parse(config)
	require.NoError(t, err)

	inputs, ok := table.Fields["inputs"]
	require.True(t, ok)

	lustre2, ok := inputs.(*ast.Table).Fields["lustre2"]
	require.True(t, ok)

	var plugin Lustre2

	require.NoError(t, toml.UnmarshalTable(lustre2.([]*ast.Table)[0], &plugin))

	require.Equal(t, Lustre2{
		OstProcfiles: []string{
			"/proc/fs/lustre/obdfilter/*/stats",
			"/proc/fs/lustre/osd-ldiskfs/*/stats",
		},
		MdsProcfiles: []string{
			"/proc/fs/lustre/mdt/*/md_stats",
		},
	}, plugin)
}

func TestLustre2GeneratesBrwstatsMetrics(t *testing.T) {
	tmpdir := t.TempDir()

	rootdir := tmpdir + "/telegraf"
	tempdir := rootdir + "/proc/fs/lustre"
	ostname := "OST0001"

	osddir := tempdir + "/osd-ldiskfs/"
	err := os.MkdirAll(osddir+"/"+ostname, 0750)
	require.NoError(t, err)

	err = os.WriteFile(osddir+"/"+ostname+"/brw_stats", []byte(brwstatsProcContents), 0640)
	require.NoError(t, err)

	m := &Lustre2{rootdir: rootdir}

	var acc testutil.Accumulator

	err = m.Gather(&acc)
	require.NoError(t, err)

	expectedData := map[string]map[string][]uint64{
		"pages_per_bulk_rw": {
			"1": {5271, 0, 337023, 22},
			"2": {3030, 0, 5672, 0},
			"4": {4449, 0, 255983, 17},
			"8": {2780, 0, 33612, 2}},
		"discontiguous_pages": {
			"0": {43942683, 100, 337023, 22},
			"1": {0, 0, 5672, 0},
			"2": {0, 0, 28016, 1},
			"3": {0, 0, 227967, 15},
			"4": {0, 0, 12869, 0}},
		"disk_ios_in_flight": {
			"1": {2892221, 6, 1437946, 96},
			"2": {2763141, 6, 44373, 2},
			"3": {3014304, 6, 2677, 0},
			"4": {3212360, 7, 183, 0}},
		"io_time": {
			"1":   {521780, 1, 0, 0},
			"16":  {6035560, 16, 0, 0},
			"128": {5044958, 14, 0, 0},
			"1K":  {651, 0, 0, 0}},
		"disk_io_size": {
			"1":    {0, 0, 327301, 22},
			"16":   {0, 0, 0, 0},
			"128":  {35, 0, 209, 0},
			"1K":   {0, 0, 1703, 0},
			"16K":  {4449, 0, 255983, 17},
			"128K": {855, 0, 23, 0},
			"1M":   {43866371, 99, 850248, 57}},
	}

	for brwSection, buckets := range expectedData {
		for bucket, values := range buckets {
			tags := map[string]string{
				"name":        ostname,
				"brw_section": brwSection,
				"bucket":      bucket,
			}
			fields := map[string]interface{}{
				"read_ios":      values[0],
				"read_percent":  values[1],
				"write_ios":     values[2],
				"write_percent": values[3],
			}
			t.Log("\n", tags)
			t.Log("\n", fields)
			acc.AssertContainsTaggedFields(t, "lustre2", fields, tags)
		}
	}
}

func TestLustre2GeneratesEvictionMetrics(t *testing.T) {
	rootdir := t.TempDir()

	// setup files in mock sysfs
	type fileEntry struct {
		targetType string
		targetName string
		value      uint64
	}
	fileEntries := []fileEntry{
		{"mdt", "fs-MDT0000", 101},
		{"mgs", "MGS", 202},
		{"obdfilter", "fs-OST0001", 303},
	}
	for _, f := range fileEntries {
		d := filepath.Join(rootdir, "sys", "fs", "lustre", f.targetType, f.targetName)
		err := os.MkdirAll(d, 0750)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(d, "eviction_count"), []byte(fmt.Sprintf("%d\n", f.value)), 0640)
		require.NoError(t, err)
	}

	// gather metrics
	m := &Lustre2{rootdir: rootdir}
	var acc testutil.Accumulator
	err := m.Gather(&acc)
	require.NoError(t, err)

	// compare with expectations
	for _, f := range fileEntries {
		acc.AssertContainsTaggedFields(
			t,
			"lustre2",
			map[string]interface{}{
				"evictions": f.value,
			},
			map[string]string{"name": f.targetName},
		)
	}
}
