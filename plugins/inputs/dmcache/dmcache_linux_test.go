//go:build linux

package dmcache

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

var (
	measurement              = "dmcache"
	badFormatOutput          = []string{"cs-1: 0 4883791872 cache 8 1018/1501122 512 7/464962 139 352643 "}
	good2DevicesFormatOutput = []string{
		"cs-1: 0 4883791872 cache 8 1018/1501122 512 7/464962 139 352643 15 46 0 7 0 1 writeback 2 migration_threshold 2048 mq 10 random_threshold 4 " +
			"sequential_threshold 512 discard_promote_adjustment 1 read_promote_adjustment 4 write_promote_adjustment 8",
		"cs-2: 0 4294967296 cache 8 72352/1310720 128 26/24327168 2409 286 265 524682 0 0 0 1 writethrough 2 migration_threshold 2048 mq 10 " +
			"random_threshold 4 sequential_threshold 512 discard_promote_adjustment 1 read_promote_adjustment 4 write_promote_adjustment 8",
	}
)

func TestPerDeviceGoodOutput(t *testing.T) {
	var acc testutil.Accumulator
	var plugin = &DMCache{
		PerDevice: true,
		getCurrentStatus: func() ([]string, error) {
			return good2DevicesFormatOutput, nil
		},
	}

	err := plugin.Gather(&acc)
	require.NoError(t, err)

	tags1 := map[string]string{
		"device": "cs-1",
	}
	fields1 := map[string]interface{}{
		"length":             int64(4883791872),
		"metadata_blocksize": int64(8),
		"metadata_used":      int64(1018),
		"metadata_total":     int64(1501122),
		"cache_blocksize":    int64(512),
		"cache_used":         int64(7),
		"cache_total":        int64(464962),
		"read_hits":          int64(139),
		"read_misses":        int64(352643),
		"write_hits":         int64(15),
		"write_misses":       int64(46),
		"demotions":          int64(0),
		"promotions":         int64(7),
		"dirty":              int64(0),
	}
	acc.AssertContainsTaggedFields(t, measurement, fields1, tags1)

	tags2 := map[string]string{
		"device": "cs-2",
	}
	fields2 := map[string]interface{}{
		"length":             int64(4294967296),
		"metadata_blocksize": int64(8),
		"metadata_used":      int64(72352),
		"metadata_total":     int64(1310720),
		"cache_blocksize":    int64(128),
		"cache_used":         int64(26),
		"cache_total":        int64(24327168),
		"read_hits":          int64(2409),
		"read_misses":        int64(286),
		"write_hits":         int64(265),
		"write_misses":       int64(524682),
		"demotions":          int64(0),
		"promotions":         int64(0),
		"dirty":              int64(0),
	}
	acc.AssertContainsTaggedFields(t, measurement, fields2, tags2)

	tags3 := map[string]string{
		"device": "all",
	}

	fields3 := map[string]interface{}{
		"length":             int64(9178759168),
		"metadata_blocksize": int64(16),
		"metadata_used":      int64(73370),
		"metadata_total":     int64(2811842),
		"cache_blocksize":    int64(640),
		"cache_used":         int64(33),
		"cache_total":        int64(24792130),
		"read_hits":          int64(2548),
		"read_misses":        int64(352929),
		"write_hits":         int64(280),
		"write_misses":       int64(524728),
		"demotions":          int64(0),
		"promotions":         int64(7),
		"dirty":              int64(0),
	}
	acc.AssertContainsTaggedFields(t, measurement, fields3, tags3)
}

func TestNotPerDeviceGoodOutput(t *testing.T) {
	var acc testutil.Accumulator
	var plugin = &DMCache{
		PerDevice: false,
		getCurrentStatus: func() ([]string, error) {
			return good2DevicesFormatOutput, nil
		},
	}

	err := plugin.Gather(&acc)
	require.NoError(t, err)

	tags := map[string]string{
		"device": "all",
	}

	fields := map[string]interface{}{
		"length":             int64(9178759168),
		"metadata_blocksize": int64(16),
		"metadata_used":      int64(73370),
		"metadata_total":     int64(2811842),
		"cache_blocksize":    int64(640),
		"cache_used":         int64(33),
		"cache_total":        int64(24792130),
		"read_hits":          int64(2548),
		"read_misses":        int64(352929),
		"write_hits":         int64(280),
		"write_misses":       int64(524728),
		"demotions":          int64(0),
		"promotions":         int64(7),
		"dirty":              int64(0),
	}
	acc.AssertContainsTaggedFields(t, measurement, fields, tags)
}

func TestNoDevicesOutput(t *testing.T) {
	var acc testutil.Accumulator
	var plugin = &DMCache{
		PerDevice: true,
		getCurrentStatus: func() ([]string, error) {
			return nil, nil
		},
	}

	err := plugin.Gather(&acc)
	require.NoError(t, err)
}

func TestErrorDuringGettingStatus(t *testing.T) {
	var acc testutil.Accumulator
	var plugin = &DMCache{
		PerDevice: true,
		getCurrentStatus: func() ([]string, error) {
			return nil, errors.New("dmsetup doesn't exist")
		},
	}

	err := plugin.Gather(&acc)
	require.Error(t, err)
}

func TestBadFormatOfStatus(t *testing.T) {
	var acc testutil.Accumulator
	var plugin = &DMCache{
		PerDevice: true,
		getCurrentStatus: func() ([]string, error) {
			return badFormatOutput, nil
		},
	}

	err := plugin.Gather(&acc)
	require.Error(t, err)
}
