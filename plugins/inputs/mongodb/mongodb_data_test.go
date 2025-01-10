package mongodb

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

var tags = make(map[string]string)

func TestAddNonReplStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			StorageEngine:       "",
			Time:                time.Now(),
			UptimeNanos:         0,
			Insert:              0,
			Query:               0,
			Update:              0,
			UpdateCnt:           0,
			Delete:              0,
			GetMore:             0,
			Command:             0,
			Flushes:             0,
			FlushesCnt:          0,
			Virtual:             0,
			Resident:            0,
			QueuedReaders:       0,
			QueuedWriters:       0,
			ActiveReaders:       0,
			ActiveWriters:       0,
			AvailableReaders:    0,
			AvailableWriters:    0,
			TotalTicketsReaders: 0,
			TotalTicketsWriters: 0,
			NetIn:               0,
			NetOut:              0,
			NumConnections:      0,
			Passes:              0,
			DeletedDocuments:    0,
			TimedOutC:           0,
			NoTimeoutC:          0,
			PinnedC:             0,
			TotalC:              0,
			DeletedD:            0,
			InsertedD:           0,
			ReturnedD:           0,
			UpdatedD:            0,
			CurrentC:            0,
			AvailableC:          0,
			TotalCreatedC:       0,
			ScanAndOrderOp:      0,
			WriteConflictsOp:    0,
			TotalKeysScanned:    0,
			TotalObjectsScanned: 0,
		},
		tags,
	)
	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range defaultStats {
		require.True(t, acc.HasFloatField("mongodb", key) || acc.HasInt64Field("mongodb", key), key)
	}
}

func TestAddReplStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			StorageEngine: "mmapv1",
			Mapped:        0,
			NonMapped:     0,
			Faults:        0,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range mmapStats {
		require.True(t, acc.HasInt64Field("mongodb", key), key)
	}
}

func TestAddWiredTigerStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			StorageEngine:              "wiredTiger",
			CacheDirtyPercent:          0,
			CacheUsedPercent:           0,
			TrackedDirtyBytes:          0,
			CurrentCachedBytes:         0,
			MaxBytesConfigured:         0,
			AppThreadsPageReadCount:    0,
			AppThreadsPageReadTime:     0,
			AppThreadsPageWriteCount:   0,
			BytesWrittenFrom:           0,
			BytesReadInto:              0,
			PagesEvictedByAppThread:    0,
			PagesQueuedForEviction:     0,
			ServerEvictingPages:        0,
			WorkerThreadEvictingPages:  0,
			PagesReadIntoCache:         0,
			PagesRequestedFromCache:    0,
			PagesWrittenFromCache:      1247,
			InternalPagesEvicted:       0,
			ModifiedPagesEvicted:       0,
			UnmodifiedPagesEvicted:     0,
			FilesCurrentlyOpen:         0,
			DataHandlesCurrentlyActive: 0,
			FaultsCnt:                  204,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range wiredTigerStats {
		require.True(t, acc.HasFloatField("mongodb", key), key)
	}

	for key := range wiredTigerExtStats {
		require.True(t, acc.HasFloatField("mongodb", key) || acc.HasInt64Field("mongodb", key), key)
	}

	require.True(t, acc.HasInt64Field("mongodb", "page_faults"))
}

func TestAddShardStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			TotalInUse:      0,
			TotalAvailable:  0,
			TotalCreated:    0,
			TotalRefreshing: 0,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range defaultShardStats {
		require.True(t, acc.HasInt64Field("mongodb", key))
	}
}

func TestAddLatencyStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			CommandOpsCnt:  73,
			CommandLatency: 364,
			ReadOpsCnt:     113,
			ReadLatency:    201,
			WriteOpsCnt:    7,
			WriteLatency:   55,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range defaultLatencyStats {
		require.True(t, acc.HasInt64Field("mongodb", key))
	}
}

func TestAddAssertsStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			Regular:   3,
			Warning:   9,
			Msg:       2,
			User:      34,
			Rollovers: 0,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range defaultAssertsStats {
		require.True(t, acc.HasInt64Field("mongodb", key))
	}
}

func TestAddCommandsStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			AggregateCommandTotal:      12,
			AggregateCommandFailed:     2,
			CountCommandTotal:          18,
			CountCommandFailed:         5,
			DeleteCommandTotal:         73,
			DeleteCommandFailed:        364,
			DistinctCommandTotal:       87,
			DistinctCommandFailed:      19,
			FindCommandTotal:           113,
			FindCommandFailed:          201,
			FindAndModifyCommandTotal:  7,
			FindAndModifyCommandFailed: 55,
			GetMoreCommandTotal:        4,
			GetMoreCommandFailed:       55,
			InsertCommandTotal:         34,
			InsertCommandFailed:        65,
			UpdateCommandTotal:         23,
			UpdateCommandFailed:        6,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range defaultCommandsStats {
		require.True(t, acc.HasInt64Field("mongodb", key))
	}
}

func TestAddTCMallocStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			TCMallocCurrentAllocatedBytes:        5877253096,
			TCMallocHeapSize:                     8067108864,
			TCMallocPageheapFreeBytes:            1054994432,
			TCMallocPageheapUnmappedBytes:        677859328,
			TCMallocMaxTotalThreadCacheBytes:     1073741824,
			TCMallocCurrentTotalThreadCacheBytes: 80405312,
			TCMallocTotalFreeBytes:               457002008,
			TCMallocCentralCacheFreeBytes:        375131800,
			TCMallocTransferCacheFreeBytes:       1464896,
			TCMallocThreadCacheFreeBytes:         80405312,
			TCMallocPageheapComittedBytes:        7389249536,
			TCMallocPageheapScavengeCount:        396394,
			TCMallocPageheapCommitCount:          641765,
			TCMallocPageheapTotalCommitBytes:     102248751104,
			TCMallocPageheapDecommitCount:        396394,
			TCMallocPageheapTotalDecommitBytes:   94859501568,
			TCMallocPageheapReserveCount:         6179,
			TCMallocPageheapTotalReserveBytes:    8067108864,
			TCMallocSpinLockTotalDelayNanos:      2344453860,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range defaultTCMallocStats {
		require.True(t, acc.HasInt64Field("mongodb", key))
	}
}

func TestAddStorageStats(t *testing.T) {
	d := newMongodbData(
		&statLine{
			StorageFreelistSearchBucketExhausted: 0,
			StorageFreelistSearchRequests:        0,
			StorageFreelistSearchScanned:         0,
		},
		tags,
	)

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)

	for key := range defaultStorageStats {
		require.True(t, acc.HasInt64Field("mongodb", key))
	}
}

func TestAddShardHostStats(t *testing.T) {
	expectedHosts := []string{"hostA", "hostB"}
	hostStatLines := map[string]shardHostStatLine{}
	for _, host := range expectedHosts {
		hostStatLines[host] = shardHostStatLine{
			InUse:      0,
			Available:  0,
			Created:    0,
			Refreshing: 0,
		}
	}

	d := newMongodbData(
		&statLine{
			ShardHostStatsLines: hostStatLines,
		},
		map[string]string{}, // Use empty tags, so we don't break existing tests
	)

	var acc testutil.Accumulator
	d.addShardHostStats()
	d.flush(&acc)

	hostsFound := make([]string, 0, len(hostStatLines))
	for host := range hostStatLines {
		for key := range shardHostStats {
			require.True(t, acc.HasInt64Field("mongodb_shard_stats", key))
		}

		require.True(t, acc.HasTag("mongodb_shard_stats", "hostname"))
		hostsFound = append(hostsFound, host)
	}
	sort.Strings(hostsFound)
	sort.Strings(expectedHosts)
	require.Equal(t, expectedHosts, hostsFound)
}

func TestStateTag(t *testing.T) {
	d := newMongodbData(
		&statLine{
			StorageEngine: "",
			Time:          time.Now(),
			Insert:        0,
			Query:         0,
			NodeType:      "PRI",
			NodeState:     "PRIMARY",
			ReplSetName:   "rs1",
			Version:       "3.6.17",
		},
		tags,
	)

	stateTags := make(map[string]string)
	stateTags["node_type"] = "PRI"
	stateTags["rs_name"] = "rs1"

	var acc testutil.Accumulator

	d.addDefaultStats()
	d.flush(&acc)
	fields := map[string]interface{}{
		"active_reads":                              int64(0),
		"active_writes":                             int64(0),
		"aggregate_command_failed":                  int64(0),
		"aggregate_command_total":                   int64(0),
		"assert_msg":                                int64(0),
		"assert_regular":                            int64(0),
		"assert_rollovers":                          int64(0),
		"assert_user":                               int64(0),
		"assert_warning":                            int64(0),
		"available_reads":                           int64(0),
		"available_writes":                          int64(0),
		"commands":                                  int64(0),
		"commands_per_sec":                          int64(0),
		"connections_available":                     int64(0),
		"connections_current":                       int64(0),
		"connections_total_created":                 int64(0),
		"count_command_failed":                      int64(0),
		"count_command_total":                       int64(0),
		"cursor_no_timeout":                         int64(0),
		"cursor_no_timeout_count":                   int64(0),
		"cursor_pinned":                             int64(0),
		"cursor_pinned_count":                       int64(0),
		"cursor_timed_out":                          int64(0),
		"cursor_timed_out_count":                    int64(0),
		"cursor_total":                              int64(0),
		"cursor_total_count":                        int64(0),
		"delete_command_failed":                     int64(0),
		"delete_command_total":                      int64(0),
		"deletes":                                   int64(0),
		"deletes_per_sec":                           int64(0),
		"distinct_command_failed":                   int64(0),
		"distinct_command_total":                    int64(0),
		"document_deleted":                          int64(0),
		"document_inserted":                         int64(0),
		"document_returned":                         int64(0),
		"document_updated":                          int64(0),
		"find_and_modify_command_failed":            int64(0),
		"find_and_modify_command_total":             int64(0),
		"find_command_failed":                       int64(0),
		"find_command_total":                        int64(0),
		"flushes":                                   int64(0),
		"flushes_per_sec":                           int64(0),
		"flushes_total_time_ns":                     int64(0),
		"get_more_command_failed":                   int64(0),
		"get_more_command_total":                    int64(0),
		"getmores":                                  int64(0),
		"getmores_per_sec":                          int64(0),
		"insert_command_failed":                     int64(0),
		"insert_command_total":                      int64(0),
		"inserts":                                   int64(0),
		"inserts_per_sec":                           int64(0),
		"jumbo_chunks":                              int64(0),
		"member_status":                             "PRI",
		"net_in_bytes":                              int64(0),
		"net_in_bytes_count":                        int64(0),
		"net_out_bytes":                             int64(0),
		"net_out_bytes_count":                       int64(0),
		"open_connections":                          int64(0),
		"operation_scan_and_order":                  int64(0),
		"operation_write_conflicts":                 int64(0),
		"queries":                                   int64(0),
		"queries_per_sec":                           int64(0),
		"queued_reads":                              int64(0),
		"queued_writes":                             int64(0),
		"repl_apply_batches_num":                    int64(0),
		"repl_apply_batches_total_millis":           int64(0),
		"repl_apply_ops":                            int64(0),
		"repl_buffer_count":                         int64(0),
		"repl_buffer_size_bytes":                    int64(0),
		"repl_commands":                             int64(0),
		"repl_commands_per_sec":                     int64(0),
		"repl_deletes":                              int64(0),
		"repl_deletes_per_sec":                      int64(0),
		"repl_executor_pool_in_progress_count":      int64(0),
		"repl_executor_queues_network_in_progress":  int64(0),
		"repl_executor_queues_sleepers":             int64(0),
		"repl_executor_unsignaled_events":           int64(0),
		"repl_getmores":                             int64(0),
		"repl_getmores_per_sec":                     int64(0),
		"repl_inserts":                              int64(0),
		"repl_inserts_per_sec":                      int64(0),
		"repl_lag":                                  int64(0),
		"repl_network_bytes":                        int64(0),
		"repl_network_getmores_num":                 int64(0),
		"repl_network_getmores_total_millis":        int64(0),
		"repl_network_ops":                          int64(0),
		"repl_queries":                              int64(0),
		"repl_queries_per_sec":                      int64(0),
		"repl_updates":                              int64(0),
		"repl_updates_per_sec":                      int64(0),
		"repl_state":                                int64(0),
		"repl_member_health":                        int64(0),
		"repl_health_avg":                           float64(0),
		"resident_megabytes":                        int64(0),
		"state":                                     "PRIMARY",
		"storage_freelist_search_bucket_exhausted":  int64(0),
		"storage_freelist_search_requests":          int64(0),
		"storage_freelist_search_scanned":           int64(0),
		"tcmalloc_central_cache_free_bytes":         int64(0),
		"tcmalloc_current_allocated_bytes":          int64(0),
		"tcmalloc_current_total_thread_cache_bytes": int64(0),
		"tcmalloc_heap_size":                        int64(0),
		"tcmalloc_max_total_thread_cache_bytes":     int64(0),
		"tcmalloc_pageheap_commit_count":            int64(0),
		"tcmalloc_pageheap_committed_bytes":         int64(0),
		"tcmalloc_pageheap_decommit_count":          int64(0),
		"tcmalloc_pageheap_free_bytes":              int64(0),
		"tcmalloc_pageheap_reserve_count":           int64(0),
		"tcmalloc_pageheap_scavenge_count":          int64(0),
		"tcmalloc_pageheap_total_commit_bytes":      int64(0),
		"tcmalloc_pageheap_total_decommit_bytes":    int64(0),
		"tcmalloc_pageheap_total_reserve_bytes":     int64(0),
		"tcmalloc_pageheap_unmapped_bytes":          int64(0),
		"tcmalloc_spinlock_total_delay_ns":          int64(0),
		"tcmalloc_thread_cache_free_bytes":          int64(0),
		"tcmalloc_total_free_bytes":                 int64(0),
		"tcmalloc_transfer_cache_free_bytes":        int64(0),
		"total_available":                           int64(0),
		"total_created":                             int64(0),
		"total_docs_scanned":                        int64(0),
		"total_in_use":                              int64(0),
		"total_keys_scanned":                        int64(0),
		"total_refreshing":                          int64(0),
		"total_tickets_reads":                       int64(0),
		"total_tickets_writes":                      int64(0),
		"ttl_deletes":                               int64(0),
		"ttl_deletes_per_sec":                       int64(0),
		"ttl_passes":                                int64(0),
		"ttl_passes_per_sec":                        int64(0),
		"update_command_failed":                     int64(0),
		"update_command_total":                      int64(0),
		"updates":                                   int64(0),
		"updates_per_sec":                           int64(0),
		"uptime_ns":                                 int64(0),
		"version":                                   "3.6.17",
		"vsize_megabytes":                           int64(0),
	}
	acc.AssertContainsTaggedFields(t, "mongodb", fields, stateTags)
}

func TestAddTopStats(t *testing.T) {
	collections := []string{"collectionOne", "collectionTwo"}
	topStatLines := make([]topStatLine, 0, len(collections))
	for _, collection := range collections {
		topStatLine := topStatLine{
			CollectionName: collection,
			TotalTime:      0,
			TotalCount:     0,
			ReadLockTime:   0,
			ReadLockCount:  0,
			WriteLockTime:  0,
			WriteLockCount: 0,
			QueriesTime:    0,
			QueriesCount:   0,
			GetMoreTime:    0,
			GetMoreCount:   0,
			InsertTime:     0,
			InsertCount:    0,
			UpdateTime:     0,
			UpdateCount:    0,
			RemoveTime:     0,
			RemoveCount:    0,
			CommandsTime:   0,
			CommandsCount:  0,
		}
		topStatLines = append(topStatLines, topStatLine)
	}

	d := newMongodbData(
		&statLine{
			TopStatLines: topStatLines,
		},
		tags,
	)

	var acc testutil.Accumulator
	d.addTopStats()
	d.flush(&acc)

	for range topStatLines {
		for key := range topDataStats {
			require.True(t, acc.HasInt64Field("mongodb_top_stats", key))
		}
	}
}
