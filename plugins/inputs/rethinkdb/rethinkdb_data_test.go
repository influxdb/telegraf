package rethinkdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

var tags = make(map[string]string)

func TestAddEngineStats(t *testing.T) {
	engine := &engine{
		ClientConns:   0,
		ClientActive:  0,
		QueriesPerSec: 0,
		TotalQueries:  0,
		ReadsPerSec:   0,
		TotalReads:    0,
		WritesPerSec:  0,
		TotalWrites:   0,
	}

	var acc testutil.Accumulator

	keys := []string{
		"active_clients",
		"clients",
		"queries_per_sec",
		"total_queries",
		"read_docs_per_sec",
		"total_reads",
		"written_docs_per_sec",
		"total_writes",
	}
	engine.addEngineStats(keys, &acc, tags)

	for _, metric := range keys {
		require.True(t, acc.HasInt64Field("rethinkdb_engine", metric))
	}
}

func TestAddEngineStatsPartial(t *testing.T) {
	engine := &engine{
		ClientConns:   0,
		ClientActive:  0,
		QueriesPerSec: 0,
		ReadsPerSec:   0,
		WritesPerSec:  0,
	}

	var acc testutil.Accumulator

	keys := []string{
		"active_clients",
		"clients",
		"queries_per_sec",
		"read_docs_per_sec",
		"written_docs_per_sec",
	}

	missingKeys := []string{
		"total_queries",
		"total_reads",
		"total_writes",
	}
	engine.addEngineStats(keys, &acc, tags)

	for _, metric := range missingKeys {
		require.False(t, acc.HasInt64Field("rethinkdb", metric))
	}
}

func TestAddStorageStats(t *testing.T) {
	storage := &storage{
		Cache: cache{
			BytesInUse: 0,
		},
		Disk: disk{
			ReadBytesPerSec:  0,
			ReadBytesTotal:   0,
			WriteBytesPerSec: 0,
			WriteBytesTotal:  0,
			SpaceUsage: spaceUsage{
				Data:     0,
				Garbage:  0,
				Metadata: 0,
				Prealloc: 0,
			},
		},
	}

	var acc testutil.Accumulator

	keys := []string{
		"cache_bytes_in_use",
		"disk_read_bytes_per_sec",
		"disk_read_bytes_total",
		"disk_written_bytes_per_sec",
		"disk_written_bytes_total",
		"disk_usage_data_bytes",
		"disk_usage_garbage_bytes",
		"disk_usage_metadata_bytes",
		"disk_usage_preallocated_bytes",
	}

	storage.addStats(&acc, tags)

	for _, metric := range keys {
		require.True(t, acc.HasInt64Field("rethinkdb", metric))
	}
}
