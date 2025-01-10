//go:build linux

package mdstat

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestFullMdstatProcFile(t *testing.T) {
	filename := makeFakeMDStatFile([]byte(mdStatFileFull))
	defer os.Remove(filename)
	k := Mdstat{
		FileName: filename,
	}
	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.NoError(t, err)

	fields := map[string]interface{}{
		"BlocksSynced":           int64(10620027200),
		"BlocksSyncedFinishTime": float64(101.6),
		"BlocksSyncedPct":        float64(94.3),
		"BlocksSyncedSpeed":      float64(103517),
		"BlocksTotal":            int64(11251451904),
		"DisksActive":            int64(12),
		"DisksFailed":            int64(0),
		"DisksSpare":             int64(0),
		"DisksTotal":             int64(12),
		"DisksDown":              int64(0),
	}
	acc.AssertContainsFields(t, "mdstat", fields)
}

func TestMdstatSyncStart(t *testing.T) {
	filename := makeFakeMDStatFile([]byte(mdStatSyncStart))
	defer os.Remove(filename)
	k := Mdstat{
		FileName: filename,
	}
	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.NoError(t, err)

	fields := map[string]interface{}{
		"BlocksSynced":           int64(10620027200),
		"BlocksSyncedFinishTime": float64(101.6),
		"BlocksSyncedPct":        float64(1.5),
		"BlocksSyncedSpeed":      float64(103517),
		"BlocksTotal":            int64(11251451904),
		"DisksActive":            int64(12),
		"DisksFailed":            int64(0),
		"DisksSpare":             int64(0),
		"DisksTotal":             int64(12),
		"DisksDown":              int64(0),
	}
	acc.AssertContainsFields(t, "mdstat", fields)
}

func TestFailedDiskMdStatProcFile1(t *testing.T) {
	filename := makeFakeMDStatFile([]byte(mdStatFileFailedDisk))
	defer os.Remove(filename)

	k := Mdstat{
		FileName: filename,
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.NoError(t, err)

	fields := map[string]interface{}{
		"BlocksSynced":           int64(5860144128),
		"BlocksSyncedFinishTime": float64(0),
		"BlocksSyncedPct":        float64(0),
		"BlocksSyncedSpeed":      float64(0),
		"BlocksTotal":            int64(5860144128),
		"DisksActive":            int64(3),
		"DisksFailed":            int64(0),
		"DisksSpare":             int64(0),
		"DisksTotal":             int64(4),
		"DisksDown":              int64(1),
	}
	acc.AssertContainsFields(t, "mdstat", fields)
}

func TestEmptyMdStatProcFile1(t *testing.T) {
	filename := makeFakeMDStatFile([]byte(mdStatFileEmpty))
	defer os.Remove(filename)

	k := Mdstat{
		FileName: filename,
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.NoError(t, err)
}

func TestInvalidMdStatProcFile1(t *testing.T) {
	filename := makeFakeMDStatFile([]byte(mdStatFileInvalid))
	defer os.Remove(filename)

	k := Mdstat{
		FileName: filename,
	}

	acc := testutil.Accumulator{}
	err := k.Gather(&acc)
	require.Error(t, err)
}

const mdStatFileFull = `
Personalities : [raid1] [raid10] [linear] [multipath] [raid0] [raid6] [raid5] [raid4]
md2 : active raid10 sde[2] sdl[9] sdf[3] sdk[8] sdh[5] sdd[1] sdg[4] sdn[11] sdm[10] sdj[7] sdc[0] sdi[6]
      11251451904 blocks super 1.2 512K chunks 2 near-copies [12/12] [UUUUUUUUUUUU]
      [==================>..]  check = 94.3% (10620027200/11251451904) finish=101.6min speed=103517K/sec
      bitmap: 35/84 pages [140KB], 65536KB chunk

md1 : active raid1 sdb2[2] sda2[0]
      5909504 blocks super 1.2 [2/2] [UU]

md0 : active raid1 sdb1[2] sda1[0]
      244005888 blocks super 1.2 [2/2] [UU]
      bitmap: 1/2 pages [4KB], 65536KB chunk

unused devices: <none>
`

const mdStatSyncStart = `
Personalities : [raid1] [raid10] [linear] [multipath] [raid0] [raid6] [raid5] [raid4]
md2 : active raid10 sde[2] sdl[9] sdf[3] sdk[8] sdh[5] sdd[1] sdg[4] sdn[11] sdm[10] sdj[7] sdc[0] sdi[6]
      11251451904 blocks super 1.2 512K chunks 2 near-copies [12/12] [UUUUUUUUUUUU]
      [>....................]  check =  1.5% (10620027200/11251451904) finish=101.6min speed=103517K/sec
      bitmap: 35/84 pages [140KB], 65536KB chunk

md1 : active raid1 sdb2[2] sda2[0]
      5909504 blocks super 1.2 [2/2] [UU]

md0 : active raid1 sdb1[2] sda1[0]
      244005888 blocks super 1.2 [2/2] [UU]
      bitmap: 1/2 pages [4KB], 65536KB chunk

unused devices: <none>
`

const mdStatFileFailedDisk = `
Personalities : [linear] [multipath] [raid0] [raid1] [raid6] [raid5] [raid4] [raid10]
md0 : active raid5 sdd1[3] sdb1[1] sda1[0]
      5860144128 blocks super 1.2 level 5, 64k chunk, algorithm 2 [4/3] [UUU_]
      bitmap: 8/15 pages [32KB], 65536KB chunk

unused devices: <none>
`

const mdStatFileEmpty = `
Personalities :
unused devices: <none>
`

const mdStatFileInvalid = `
Personalities :

mdf1: testman actve

md0 : active raid1 sdb1[2] sda1[0]
      244005888 blocks super 1.2 [2/2] [UU]
      bitmap: 1/2 pages [4KB], 65536KB chunk

unused devices: <none>
`

func makeFakeMDStatFile(content []byte) (filename string) {
	fileobj, err := os.CreateTemp("", "mdstat")
	if err != nil {
		panic(err)
	}

	if _, err = fileobj.Write(content); err != nil {
		panic(err)
	}
	if err := fileobj.Close(); err != nil {
		panic(err)
	}
	return fileobj.Name()
}
