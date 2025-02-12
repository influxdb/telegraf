//go:build linux

package bcache

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

const (
	dirtyData           = "1.5G"
	bypassed            = "4.7T"
	cacheBypassHits     = "146155333"
	cacheBypassMisses   = "0"
	cacheHitRatio       = "90"
	cacheHits           = "511469583"
	cacheMissCollisions = "157567"
	cacheMisses         = "50616331"
	cacheReadaheads     = "2"
)

func TestBcacheGeneratesMetrics(t *testing.T) {
	tmpDir := t.TempDir()

	testBcachePath := tmpDir + "/telegraf-bcache/sys/fs/bcache"
	testBcacheUUIDPath := testBcachePath + "/663955a3-765a-4737-a9fd-8250a7a78411"
	testBcacheDevPath := tmpDir + "/telegraf/sys/devices/virtual/block/bcache0"
	testBcacheBackingDevPath := tmpDir + "/telegraf/sys/devices/virtual/block/md10"

	err := os.MkdirAll(testBcacheUUIDPath, 0750)
	require.NoError(t, err)

	err = os.MkdirAll(testBcacheDevPath, 0750)
	require.NoError(t, err)

	err = os.MkdirAll(testBcacheBackingDevPath+"/bcache", 0750)
	require.NoError(t, err)

	err = os.Symlink(testBcacheBackingDevPath+"/bcache", testBcacheUUIDPath+"/bdev0")
	require.NoError(t, err)

	err = os.Symlink(testBcacheDevPath, testBcacheUUIDPath+"/bdev0/dev")
	require.NoError(t, err)

	err = os.MkdirAll(testBcacheUUIDPath+"/bdev0/stats_total", 0750)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/dirty_data", []byte(dirtyData), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/bypassed", []byte(bypassed), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/cache_bypass_hits", []byte(cacheBypassHits), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/cache_bypass_misses", []byte(cacheBypassMisses), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/cache_hit_ratio", []byte(cacheHitRatio), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/cache_hits", []byte(cacheHits), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/cache_miss_collisions", []byte(cacheMissCollisions), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/cache_misses", []byte(cacheMisses), 0640)
	require.NoError(t, err)

	err = os.WriteFile(testBcacheUUIDPath+"/bdev0/stats_total/cache_readaheads", []byte(cacheReadaheads), 0640)
	require.NoError(t, err)

	fields := map[string]interface{}{
		"dirty_data":            uint64(1610612736),
		"bypassed":              uint64(5167704440832),
		"cache_bypass_hits":     uint64(146155333),
		"cache_bypass_misses":   uint64(0),
		"cache_hit_ratio":       uint64(90),
		"cache_hits":            uint64(511469583),
		"cache_miss_collisions": uint64(157567),
		"cache_misses":          uint64(50616331),
		"cache_readaheads":      uint64(2),
	}

	tags := map[string]string{
		"backing_dev": "md10",
		"bcache_dev":  "bcache0",
	}

	var acc testutil.Accumulator

	// all devs
	b := &Bcache{BcachePath: testBcachePath}

	err = b.Gather(&acc)
	require.NoError(t, err)
	acc.AssertContainsTaggedFields(t, "bcache", fields, tags)

	// one exist dev
	b = &Bcache{BcachePath: testBcachePath, BcacheDevs: []string{"bcache0"}}

	err = b.Gather(&acc)
	require.NoError(t, err)
	acc.AssertContainsTaggedFields(t, "bcache", fields, tags)
}
