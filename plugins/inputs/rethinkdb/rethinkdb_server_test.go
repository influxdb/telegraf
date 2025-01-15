//go:build integration

package rethinkdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestValidateVersion(t *testing.T) {
	err := server.validateVersion()
	require.NoError(t, err)
}

func TestGetDefaultTags(t *testing.T) {
	var tagTests = []struct {
		in  string
		out string
	}{
		{"rethinkdb_host", server.Url.Host},
		{"rethinkdb_hostname", server.serverStatus.Network.Hostname},
	}
	defaultTags := server.getDefaultTags()
	for _, tt := range tagTests {
		if defaultTags[tt.in] != tt.out {
			t.Errorf("expected %q, got %q", tt.out, defaultTags[tt.in])
		}
	}
}

func TestAddClusterStats(t *testing.T) {
	var acc testutil.Accumulator

	err := server.addClusterStats(&acc)
	require.NoError(t, err)

	for _, metric := range clusterTracking {
		require.True(t, acc.HasIntValue(metric))
	}
}

func TestAddMemberStats(t *testing.T) {
	var acc testutil.Accumulator

	err := server.addMemberStats(&acc)
	require.NoError(t, err)

	for _, metric := range memberTracking {
		require.True(t, acc.HasIntValue(metric))
	}
}

func TestAddTableStats(t *testing.T) {
	var acc testutil.Accumulator

	err := server.addTableStats(&acc)
	require.NoError(t, err)

	for _, metric := range tableTracking {
		require.True(t, acc.HasIntValue(metric))
	}

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

	for _, metric := range keys {
		require.True(t, acc.HasIntValue(metric))
	}
}
