//go:build linux

package slab

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestSlab(t *testing.T) {
	slabStats := Slab{
		statFile: path.Join("testdata", "slabinfo"),
		useSudo:  false,
	}

	var acc testutil.Accumulator
	require.NoError(t, slabStats.Gather(&acc))

	fields := map[string]interface{}{
		"ext4_allocation_context_size": int(16384),
		"ext4_extent_status_size":      int(8160),
		"ext4_free_data_size":          int(0),
		"ext4_inode_cache_size":        int(491520),
		"ext4_io_end_size":             int(4032),
		"ext4_xattr_size":              int(0),
		"kmalloc_1024_size":            int(239927296),
		"kmalloc_128_size":             int(5586944),
		"kmalloc_16_size":              int(17002496),
		"kmalloc_192_size":             int(4015872),
		"kmalloc_2048_size":            int(3309568),
		"kmalloc_256_size":             int(5423104),
		"kmalloc_32_size":              int(3657728),
		"kmalloc_4096_size":            int(2359296),
		"kmalloc_512_size":             int(41435136),
		"kmalloc_64_size":              int(8536064),
		"kmalloc_8_size":               int(229376),
		"kmalloc_8192_size":            int(1048576),
		"kmalloc_96_size":              int(12378240),
		"kmem_cache_size":              int(81920),
		"kmem_cache_node_size":         int(36864),
	}

	acc.AssertContainsFields(t, "slab", fields)
}
