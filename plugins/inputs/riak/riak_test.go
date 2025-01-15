package riak

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestRiak(t *testing.T) {
	// Create a test server with the const response JSON
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprintln(w, response); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Parse the URL of the test server, used to verify the expected host
	u, err := url.Parse(ts.URL)
	require.NoError(t, err)

	// Create a new Riak instance with our given test server
	riak := newRiak()
	riak.Servers = []string{ts.URL}

	// Create a test accumulator
	acc := &testutil.Accumulator{}

	// Gather data from the test server
	require.NoError(t, riak.Gather(acc))

	// Expect the correct values for all known keys
	expectFields := map[string]interface{}{
		"cpu_avg1":                     int64(504),
		"cpu_avg15":                    int64(294),
		"cpu_avg5":                     int64(325),
		"memory_code":                  int64(12329143),
		"memory_ets":                   int64(17330176),
		"memory_processes":             int64(58454730),
		"memory_system":                int64(120401678),
		"memory_total":                 int64(178856408),
		"node_get_fsm_objsize_100":     int64(73596),
		"node_get_fsm_objsize_95":      int64(36663),
		"node_get_fsm_objsize_99":      int64(51552),
		"node_get_fsm_objsize_mean":    int64(13241),
		"node_get_fsm_objsize_median":  int64(10365),
		"node_get_fsm_siblings_100":    int64(1),
		"node_get_fsm_siblings_95":     int64(1),
		"node_get_fsm_siblings_99":     int64(1),
		"node_get_fsm_siblings_mean":   int64(1),
		"node_get_fsm_siblings_median": int64(1),
		"node_get_fsm_time_100":        int64(230445),
		"node_get_fsm_time_95":         int64(24259),
		"node_get_fsm_time_99":         int64(96653),
		"node_get_fsm_time_mean":       int64(6851),
		"node_get_fsm_time_median":     int64(2368),
		"node_gets":                    int64(1116),
		"node_gets_total":              int64(1026058217),
		"node_put_fsm_time_100":        int64(267390),
		"node_put_fsm_time_95":         int64(38286),
		"node_put_fsm_time_99":         int64(84422),
		"node_put_fsm_time_mean":       int64(10832),
		"node_put_fsm_time_median":     int64(4085),
		"read_repairs":                 int64(2),
		"read_repairs_total":           int64(7918375),
		"node_puts":                    int64(1155),
		"node_puts_total":              int64(444895769),
		"pbc_active":                   int64(360),
		"pbc_connects":                 int64(120),
		"pbc_connects_total":           int64(66793268),
		"vnode_gets":                   int64(14629),
		"vnode_gets_total":             int64(3748432761),
		"vnode_index_reads":            int64(20),
		"vnode_index_reads_total":      int64(3438296),
		"vnode_index_writes":           int64(4293),
		"vnode_index_writes_total":     int64(1515986619),
		"vnode_puts":                   int64(4308),
		"vnode_puts_total":             int64(1519062272),
	}

	// Expect the correct values for all tags
	expectTags := map[string]string{
		"nodename": "riak@127.0.0.1",
		"server":   u.Host,
	}

	acc.AssertContainsTaggedFields(t, "riak", expectFields, expectTags)
}

var response = `
{
  "riak_kv_stat_ts": 1455908558,
  "vnode_gets": 14629,
  "vnode_gets_total": 3748432761,
  "vnode_puts": 4308,
  "vnode_puts_total": 1519062272,
  "vnode_index_refreshes": 0,
  "vnode_index_refreshes_total": 0,
  "vnode_index_reads": 20,
  "vnode_index_reads_total": 3438296,
  "vnode_index_writes": 4293,
  "vnode_index_writes_total": 1515986619,
  "vnode_index_writes_postings": 1,
  "vnode_index_writes_postings_total": 265613,
  "vnode_index_deletes": 0,
  "vnode_index_deletes_total": 0,
  "vnode_index_deletes_postings": 0,
  "vnode_index_deletes_postings_total": 1,
  "node_gets": 1116,
  "node_gets_total": 1026058217,
  "node_get_fsm_siblings_mean": 1,
  "node_get_fsm_siblings_median": 1,
  "node_get_fsm_siblings_95": 1,
  "node_get_fsm_siblings_99": 1,
  "node_get_fsm_siblings_100": 1,
  "node_get_fsm_objsize_mean": 13241,
  "node_get_fsm_objsize_median": 10365,
  "node_get_fsm_objsize_95": 36663,
  "node_get_fsm_objsize_99": 51552,
  "node_get_fsm_objsize_100": 73596,
  "node_get_fsm_time_mean": 6851,
  "node_get_fsm_time_median": 2368,
  "node_get_fsm_time_95": 24259,
  "node_get_fsm_time_99": 96653,
  "node_get_fsm_time_100": 230445,
  "node_puts": 1155,
  "node_puts_total": 444895769,
  "node_put_fsm_time_mean": 10832,
  "node_put_fsm_time_median": 4085,
  "node_put_fsm_time_95": 38286,
  "node_put_fsm_time_99": 84422,
  "node_put_fsm_time_100": 267390,
  "read_repairs": 2,
  "read_repairs_total": 7918375,
  "coord_redirs_total": 118238575,
  "executing_mappers": 0,
  "precommit_fail": 0,
  "postcommit_fail": 0,
  "index_fsm_create": 0,
  "index_fsm_create_error": 0,
  "index_fsm_active": 0,
  "list_fsm_create": 0,
  "list_fsm_create_error": 0,
  "list_fsm_active": 0,
  "pbc_active": 360,
  "pbc_connects": 120,
  "pbc_connects_total": 66793268,
  "late_put_fsm_coordinator_ack": 152,
  "node_get_fsm_active": 1,
  "node_get_fsm_active_60s": 1029,
  "node_get_fsm_in_rate": 21,
  "node_get_fsm_out_rate": 21,
  "node_get_fsm_rejected": 0,
  "node_get_fsm_rejected_60s": 0,
  "node_get_fsm_rejected_total": 0,
  "node_put_fsm_active": 69,
  "node_put_fsm_active_60s": 1053,
  "node_put_fsm_in_rate": 30,
  "node_put_fsm_out_rate": 31,
  "node_put_fsm_rejected": 0,
  "node_put_fsm_rejected_60s": 0,
  "node_put_fsm_rejected_total": 0,
  "read_repairs_primary_outofdate_one": 4,
  "read_repairs_primary_outofdate_count": 14761552,
  "read_repairs_primary_notfound_one": 0,
  "read_repairs_primary_notfound_count": 65879,
  "read_repairs_fallback_outofdate_one": 0,
  "read_repairs_fallback_outofdate_count": 23761,
  "read_repairs_fallback_notfound_one": 0,
  "read_repairs_fallback_notfound_count": 455697,
  "leveldb_read_block_error": 0,
  "riak_pipe_stat_ts": 1455908558,
  "pipeline_active": 0,
  "pipeline_create_count": 0,
  "pipeline_create_one": 0,
  "pipeline_create_error_count": 0,
  "pipeline_create_error_one": 0,
  "cpu_nprocs": 362,
  "cpu_avg1": 504,
  "cpu_avg5": 325,
  "cpu_avg15": 294,
  "mem_total": 33695432704,
  "mem_allocated": 33454874624,
  "nodename": "riak@127.0.0.1",
  "connected_nodes": [],
  "sys_driver_version": "2.0",
  "sys_global_heaps_size": 0,
  "sys_heap_type": "private",
  "sys_logical_processors": 8,
  "sys_otp_release": "R15B01",
  "sys_process_count": 2201,
  "sys_smp_support": true,
  "sys_system_version": "Erlang R15B01 (erts-5.9.1) [source] [64-bit] [smp:8:8] [async-threads:64] [kernel-poll:true]",
  "sys_system_architecture": "x86_64-unknown-linux-gnu",
  "sys_threads_enabled": true,
  "sys_thread_pool_size": 64,
  "sys_wordsize": 8,
  "ring_members": [
    "riak@127.0.0.1"
  ],
  "ring_num_partitions": 256,
  "ring_ownership": "[{'riak@127.0.0.1',256}]",
  "ring_creation_size": 256,
  "storage_backend": "riak_kv_eleveldb_backend",
  "erlydtl_version": "0.7.0",
  "riak_control_version": "1.4.12-0-g964c5db",
  "cluster_info_version": "1.2.4",
  "riak_search_version": "1.4.12-0-g7fe0e00",
  "merge_index_version": "1.3.2-0-gcb38ee7",
  "riak_kv_version": "1.4.12-0-gc6bbd66",
  "sidejob_version": "0.2.0",
  "riak_api_version": "1.4.12-0-gd9e1cc8",
  "riak_pipe_version": "1.4.12-0-g986a226",
  "riak_core_version": "1.4.10",
  "bitcask_version": "1.6.8-0-gea14cb0",
  "basho_stats_version": "1.0.3",
  "webmachine_version": "1.10.4-0-gfcff795",
  "mochiweb_version": "1.5.1p6",
  "inets_version": "5.9",
  "erlang_js_version": "1.2.2",
  "runtime_tools_version": "1.8.8",
  "os_mon_version": "2.2.9",
  "riak_sysmon_version": "1.1.3",
  "ssl_version": "5.0.1",
  "public_key_version": "0.15",
  "crypto_version": "2.1",
  "sasl_version": "2.2.1",
  "lager_version": "2.0.1",
  "goldrush_version": "0.1.5",
  "compiler_version": "4.8.1",
  "syntax_tools_version": "1.6.8",
  "stdlib_version": "1.18.1",
  "kernel_version": "2.15.1",
  "memory_total": 178856408,
  "memory_processes": 58454730,
  "memory_processes_used": 58371238,
  "memory_system": 120401678,
  "memory_atom": 586345,
  "memory_atom_used": 563485,
  "memory_binary": 48677920,
  "memory_code": 12329143,
  "memory_ets": 17330176,
  "riak_core_stat_ts": 1455908559,
  "ignored_gossip_total": 0,
  "rings_reconciled_total": 5459,
  "rings_reconciled": 0,
  "gossip_received": 6,
  "rejected_handoffs": 94,
  "handoff_timeouts": 0,
  "dropped_vnode_requests_total": 0,
  "converge_delay_min": 0,
  "converge_delay_max": 0,
  "converge_delay_mean": 0,
  "converge_delay_last": 0,
  "rebalance_delay_min": 0,
  "rebalance_delay_max": 0,
  "rebalance_delay_mean": 0,
  "rebalance_delay_last": 0,
  "riak_kv_vnodes_running": 16,
  "riak_kv_vnodeq_min": 0,
  "riak_kv_vnodeq_median": 0,
  "riak_kv_vnodeq_mean": 0,
  "riak_kv_vnodeq_max": 0,
  "riak_kv_vnodeq_total": 0,
  "riak_pipe_vnodes_running": 16,
  "riak_pipe_vnodeq_min": 0,
  "riak_pipe_vnodeq_median": 0,
  "riak_pipe_vnodeq_mean": 0,
  "riak_pipe_vnodeq_max": 0,
  "riak_pipe_vnodeq_total": 0
}
`
