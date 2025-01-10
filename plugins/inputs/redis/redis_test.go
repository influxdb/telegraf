package redis

import (
	"bufio"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf/testutil"
)

type testClient struct{}

func (*testClient) BaseTags() map[string]string {
	return map[string]string{"host": "redis.net"}
}

func (*testClient) Info() *redis.StringCmd {
	return nil
}

func (*testClient) Do(string, ...interface{}) (interface{}, error) {
	return 2, nil
}

func (*testClient) Close() error {
	return nil
}

func TestRedisConnectIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	servicePort := "6379"
	container := testutil.Container{
		Image:        "redis:alpine",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForListeningPort(nat.Port(servicePort)),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")
	defer container.Terminate()

	addr := fmt.Sprintf("%s:%s", container.Address, container.Ports[servicePort])

	r := &Redis{
		Log:     testutil.Logger{},
		Servers: []string{addr},
	}

	var acc testutil.Accumulator

	err = acc.GatherError(r.Gather)
	require.NoError(t, err)
}

func TestRedis_Commands(t *testing.T) {
	const redisListKey = "test-list-length"
	var acc testutil.Accumulator

	tc := &testClient{}

	rc := &RedisCommand{
		Command: []interface{}{"llen", "test-list"},
		Field:   redisListKey,
		Type:    "integer",
	}

	r := &Redis{
		Commands: []*RedisCommand{rc},
		clients:  []Client{tc},
	}

	err := r.gatherCommandValues(tc, &acc)
	require.NoError(t, err)

	fields := map[string]interface{}{
		redisListKey: 2,
	}

	acc.AssertContainsFields(t, "redis_commands", fields)
}

func TestRedis_ParseMetrics(t *testing.T) {
	var acc testutil.Accumulator
	tags := map[string]string{"host": "redis.net"}
	rdr := bufio.NewReader(strings.NewReader(testOutput))

	err := gatherInfoOutput(rdr, &acc, tags)
	require.NoError(t, err)

	tags = map[string]string{"host": "redis.net", "replication_role": "master"}
	fields := map[string]interface{}{
		"uptime":                          int64(238),
		"lru_clock":                       int64(2364819),
		"clients":                         int64(1),
		"client_longest_output_list":      int64(0),
		"client_biggest_input_buf":        int64(0),
		"blocked_clients":                 int64(0),
		"used_memory":                     int64(1003936),
		"used_memory_rss":                 int64(811008),
		"used_memory_peak":                int64(1003936),
		"used_memory_lua":                 int64(33792),
		"used_memory_peak_perc":           float64(93.58),
		"used_memory_dataset_perc":        float64(20.27),
		"mem_fragmentation_ratio":         float64(0.81),
		"loading":                         int64(0),
		"rdb_changes_since_last_save":     int64(0),
		"rdb_bgsave_in_progress":          int64(0),
		"rdb_last_save_time":              int64(1428427941),
		"rdb_last_bgsave_status":          "ok",
		"rdb_last_bgsave_time_sec":        int64(-1),
		"rdb_current_bgsave_time_sec":     int64(-1),
		"aof_enabled":                     int64(0),
		"aof_rewrite_in_progress":         int64(0),
		"aof_rewrite_scheduled":           int64(0),
		"aof_last_rewrite_time_sec":       int64(-1),
		"aof_current_rewrite_time_sec":    int64(-1),
		"aof_last_bgrewrite_status":       "ok",
		"aof_last_write_status":           "ok",
		"total_connections_received":      int64(2),
		"total_commands_processed":        int64(1),
		"instantaneous_ops_per_sec":       int64(0),
		"instantaneous_input_kbps":        float64(876.16),
		"instantaneous_output_kbps":       float64(3010.23),
		"rejected_connections":            int64(0),
		"sync_full":                       int64(0),
		"sync_partial_ok":                 int64(0),
		"sync_partial_err":                int64(0),
		"expired_keys":                    int64(0),
		"evicted_keys":                    int64(0),
		"keyspace_hits":                   int64(1),
		"keyspace_misses":                 int64(1),
		"pubsub_channels":                 int64(0),
		"pubsub_patterns":                 int64(0),
		"latest_fork_usec":                int64(0),
		"connected_slaves":                int64(2),
		"master_repl_offset":              int64(0),
		"repl_backlog_active":             int64(0),
		"repl_backlog_size":               int64(1048576),
		"repl_backlog_first_byte_offset":  int64(0),
		"repl_backlog_histlen":            int64(0),
		"second_repl_offset":              int64(-1),
		"used_cpu_sys":                    float64(0.14),
		"used_cpu_user":                   float64(0.05),
		"used_cpu_sys_children":           float64(0.00),
		"used_cpu_user_children":          float64(0.00),
		"keyspace_hitrate":                float64(0.50),
		"redis_version":                   "6.0.9",
		"active_defrag_hits":              int64(0),
		"active_defrag_key_hits":          int64(0),
		"active_defrag_key_misses":        int64(0),
		"active_defrag_misses":            int64(0),
		"active_defrag_running":           int64(0),
		"allocator_active":                int64(1022976),
		"allocator_allocated":             int64(1019632),
		"allocator_frag_bytes":            float64(3344),
		"allocator_frag_ratio":            float64(1.00),
		"allocator_resident":              int64(1022976),
		"allocator_rss_bytes":             int64(0),
		"allocator_rss_ratio":             float64(1.00),
		"aof_last_cow_size":               int64(0),
		"client_recent_max_input_buffer":  int64(16),
		"client_recent_max_output_buffer": int64(0),
		"clients_in_timeout_table":        int64(0),
		"cluster_enabled":                 int64(0),
		"expire_cycle_cpu_milliseconds":   int64(669),
		"expired_stale_perc":              float64(0.00),
		"expired_time_cap_reached_count":  int64(0),
		"io_threaded_reads_processed":     int64(0),
		"io_threaded_writes_processed":    int64(0),
		"total_reads_processed":           int64(31),
		"total_writes_processed":          int64(17),
		"lazyfree_pending_objects":        int64(0),
		"maxmemory":                       int64(0),
		"maxmemory_policy":                "noeviction",
		"mem_aof_buffer":                  int64(0),
		"mem_clients_normal":              int64(17440),
		"mem_clients_slaves":              int64(0),
		"mem_fragmentation_bytes":         int64(41232),
		"mem_not_counted_for_evict":       int64(0),
		"mem_replication_backlog":         int64(0),
		"rss_overhead_bytes":              int64(37888),
		"rss_overhead_ratio":              float64(1.04),
		"total_system_memory":             int64(17179869184),
		"used_memory_dataset":             int64(47088),
		"used_memory_overhead":            int64(1019152),
		"used_memory_scripts":             int64(0),
		"used_memory_startup":             int64(1001712),
		"migrate_cached_sockets":          int64(0),
		"module_fork_in_progress":         int64(0),
		"module_fork_last_cow_size":       int64(0),
		"number_of_cached_scripts":        int64(0),
		"rdb_last_cow_size":               int64(0),
		"slave_expires_tracked_keys":      int64(0),
		"unexpected_error_replies":        int64(0),
		"total_net_input_bytes":           int64(381),
		"total_net_output_bytes":          int64(71521),
		"tracking_clients":                int64(0),
		"tracking_total_items":            int64(0),
		"tracking_total_keys":             int64(0),
		"tracking_total_prefixes":         int64(0),
	}

	// We have to test rdb_last_save_time_offset manually because the value is based on the time when gathered
	for _, m := range acc.Metrics {
		for k, v := range m.Fields {
			if k == "rdb_last_save_time_elapsed" {
				fields[k] = v
			}
		}
	}
	require.InDelta(t,
		time.Now().Unix()-fields["rdb_last_save_time"].(int64),
		fields["rdb_last_save_time_elapsed"].(int64),
		2) // allow for 2 seconds worth of offset

	keyspaceTags := map[string]string{"host": "redis.net", "replication_role": "master", "database": "db0"}
	keyspaceFields := map[string]interface{}{
		"avg_ttl": int64(0),
		"expires": int64(0),
		"keys":    int64(2),
	}
	acc.AssertContainsTaggedFields(t, "redis", fields, tags)
	acc.AssertContainsTaggedFields(t, "redis_keyspace", keyspaceFields, keyspaceTags)

	cmdstatSetTags := map[string]string{"host": "redis.net", "replication_role": "master", "command": "set"}
	cmdstatSetFields := map[string]interface{}{
		"calls":         int64(261265),
		"usec":          int64(1634157),
		"usec_per_call": float64(6.25),
	}
	acc.AssertContainsTaggedFields(t, "redis_cmdstat", cmdstatSetFields, cmdstatSetTags)

	cmdstatCommandTags := map[string]string{"host": "redis.net", "replication_role": "master", "command": "command"}
	cmdstatCommandFields := map[string]interface{}{
		"calls":         int64(1),
		"usec":          int64(990),
		"usec_per_call": float64(990.0),
	}
	acc.AssertContainsTaggedFields(t, "redis_cmdstat", cmdstatCommandFields, cmdstatCommandTags)

	cmdstatPublishTags := map[string]string{"host": "redis.net", "replication_role": "master", "command": "publish"}
	cmdstatPublishFields := map[string]interface{}{
		"calls":          int64(488662),
		"usec":           int64(8573493),
		"usec_per_call":  float64(17.54),
		"rejected_calls": int64(0),
		"failed_calls":   int64(0),
	}
	acc.AssertContainsTaggedFields(t, "redis_cmdstat", cmdstatPublishFields, cmdstatPublishTags)

	latencyZaddTags := map[string]string{"host": "redis.net", "replication_role": "master", "command": "zadd"}
	latencyZaddFields := map[string]interface{}{
		"p50":   float64(9.023),
		"p99":   float64(28.031),
		"p99.9": float64(43.007),
	}
	acc.AssertContainsTaggedFields(t, "redis_latency_percentiles_usec", latencyZaddFields, latencyZaddTags)

	latencyHgetallTags := map[string]string{"host": "redis.net", "replication_role": "master", "command": "hgetall"}
	latencyHgetallFields := map[string]interface{}{
		"p50":   float64(11.007),
		"p99":   float64(34.047),
		"p99.9": float64(66.047),
	}
	acc.AssertContainsTaggedFields(t, "redis_latency_percentiles_usec", latencyHgetallFields, latencyHgetallTags)

	replicationTags := map[string]string{
		"host":             "redis.net",
		"replication_role": "slave",
		"replica_id":       "0",
		"replica_ip":       "127.0.0.1",
		"replica_port":     "7379",
		"state":            "online",
	}
	replicationFields := map[string]interface{}{
		"lag":    int64(0),
		"offset": int64(4556468),
	}

	acc.AssertContainsTaggedFields(t, "redis_replication", replicationFields, replicationTags)

	replicationTags = map[string]string{
		"host":             "redis.net",
		"replication_role": "slave",
		"replica_id":       "1",
		"replica_ip":       "127.0.0.1",
		"replica_port":     "8379",
		"state":            "send_bulk",
	}
	replicationFields = map[string]interface{}{
		"lag":    int64(1),
		"offset": int64(0),
	}

	acc.AssertContainsTaggedFields(t, "redis_replication", replicationFields, replicationTags)

	errorStatsTags := map[string]string{"host": "redis.net", "replication_role": "master", "err": "MOVED"}
	errorStatsFields := map[string]interface{}{"total": int64(3628)}

	acc.AssertContainsTaggedFields(t, "redis_errorstat", errorStatsFields, errorStatsTags)
}

func TestRedis_ParseFloatOnInts(t *testing.T) {
	var acc testutil.Accumulator
	tags := map[string]string{"host": "redis.net"}
	rdr := bufio.NewReader(strings.NewReader(strings.Replace(testOutput, "mem_fragmentation_ratio:0.81", "mem_fragmentation_ratio:1", 1)))
	err := gatherInfoOutput(rdr, &acc, tags)
	require.NoError(t, err)
	var m *testutil.Metric
	for i := range acc.Metrics {
		if _, ok := acc.Metrics[i].Fields["mem_fragmentation_ratio"]; ok {
			m = acc.Metrics[i]
			break
		}
	}
	require.NotNil(t, m)
	fragRatio, ok := m.Fields["mem_fragmentation_ratio"]
	require.True(t, ok)
	require.IsType(t, float64(0.0), fragRatio)
}

func TestRedis_ParseIntOnFloats(t *testing.T) {
	var acc testutil.Accumulator
	tags := map[string]string{"host": "redis.net"}
	rdr := bufio.NewReader(strings.NewReader(strings.Replace(testOutput, "clients_in_timeout_table:0", "clients_in_timeout_table:0.0", 1)))
	err := gatherInfoOutput(rdr, &acc, tags)
	require.NoError(t, err)
	var m *testutil.Metric
	for i := range acc.Metrics {
		if _, ok := acc.Metrics[i].Fields["clients_in_timeout_table"]; ok {
			m = acc.Metrics[i]
			break
		}
	}
	require.NotNil(t, m)
	clientsInTimeout, ok := m.Fields["clients_in_timeout_table"]
	require.True(t, ok)
	require.IsType(t, int64(0), clientsInTimeout)
}

func TestRedis_ParseStringOnInts(t *testing.T) {
	var acc testutil.Accumulator
	tags := map[string]string{"host": "redis.net"}
	rdr := bufio.NewReader(strings.NewReader(strings.Replace(testOutput, "maxmemory_policy:no-eviction", "maxmemory_policy:1", 1)))
	err := gatherInfoOutput(rdr, &acc, tags)
	require.NoError(t, err)
	var m *testutil.Metric
	for i := range acc.Metrics {
		if _, ok := acc.Metrics[i].Fields["maxmemory_policy"]; ok {
			m = acc.Metrics[i]
			break
		}
	}
	require.NotNil(t, m)
	maxmemoryPolicy, ok := m.Fields["maxmemory_policy"]
	require.True(t, ok)
	require.IsType(t, string(""), maxmemoryPolicy)
}

func TestRedis_ParseIntOnString(t *testing.T) {
	var acc testutil.Accumulator
	tags := map[string]string{"host": "redis.net"}
	rdr := bufio.NewReader(strings.NewReader(strings.Replace(testOutput, "clients_in_timeout_table:0", `clients_in_timeout_table:""`, 1)))
	err := gatherInfoOutput(rdr, &acc, tags)
	require.NoError(t, err)
	var m *testutil.Metric
	for i := range acc.Metrics {
		if _, ok := acc.Metrics[i].Fields["clients_in_timeout_table"]; ok {
			m = acc.Metrics[i]
			break
		}
	}
	require.NotNil(t, m)
	clientsInTimeout, ok := m.Fields["clients_in_timeout_table"]
	require.True(t, ok)
	require.IsType(t, int64(0), clientsInTimeout)
}

func TestRedis_GatherErrorstatsLine(t *testing.T) {
	var acc testutil.Accumulator
	globalTags := map[string]string{}

	gatherErrorstatsLine("FOO", "BAR", &acc, globalTags)
	require.Len(t, acc.Errors, 1)
	require.Equal(t, "invalid line for \"FOO\": BAR", acc.Errors[0].Error())

	acc = testutil.Accumulator{}
	gatherErrorstatsLine("FOO", "BAR=a", &acc, globalTags)
	require.Len(t, acc.Errors, 1)
	require.Equal(t, "parsing value in line \"BAR=a\" failed: strconv.ParseInt: parsing \"a\": invalid syntax", acc.Errors[0].Error())

	acc = testutil.Accumulator{}
	gatherErrorstatsLine("FOO", "BAR=77", &acc, globalTags)
	require.Empty(t, acc.Errors)
}

const testOutput = `# Server
redis_version:6.0.9
redis_git_sha1:00000000
redis_git_dirty:0
redis_build_id:26c3229b35eb3beb
redis_mode:standalone
os:Darwin 19.6.0 x86_64
arch_bits:64
multiplexing_api:kqueue
atomicvar_api:atomic-builtin
gcc_version:4.2.1
process_id:46677
run_id:5d6bf38087b23e48f1a59b7aca52e2b55438b02f
tcp_port:6379
uptime_in_seconds:238
uptime_in_days:0
hz:10
configured_hz:10
lru_clock:2364819
executable:/usr/local/opt/redis/bin/redis-server
config_file:/usr/local/etc/redis.conf
io_threads_active:0

# Clients
client_recent_max_input_buffer:16
client_recent_max_output_buffer:0
tracking_clients:0
clients_in_timeout_table:0
connected_clients:1
client_longest_output_list:0
client_biggest_input_buf:0
blocked_clients:0

# Memory
used_memory:1003936
used_memory_human:980.41K
used_memory_rss:811008
used_memory_rss_human:1.01M
used_memory_peak:1003936
used_memory_peak_human:980.41K
used_memory_peak_perc:93.58%
used_memory_overhead:1019152
used_memory_startup:1001712
used_memory_dataset:47088
used_memory_dataset_perc:20.27%
allocator_allocated:1019632
allocator_active:1022976
allocator_resident:1022976
total_system_memory:17179869184
total_system_memory_human:16.00G
used_memory_lua:33792
used_memory_lua_human:37.00K
used_memory_scripts:0
used_memory_scripts_human:0B
number_of_cached_scripts:0
maxmemory:0
maxmemory_human:0B
maxmemory_policy:noeviction
allocator_frag_ratio:1.00
allocator_frag_bytes:3344
allocator_rss_ratio:1.00
allocator_rss_bytes:0
rss_overhead_ratio:1.04
rss_overhead_bytes:37888
mem_fragmentation_ratio:0.81
mem_fragmentation_bytes:41232
mem_not_counted_for_evict:0
mem_replication_backlog:0
mem_clients_slaves:0
mem_clients_normal:17440
mem_aof_buffer:0
mem_allocator:libc
active_defrag_running:0
lazyfree_pending_objects:0

# Persistence
loading:0
rdb_changes_since_last_save:0
rdb_bgsave_in_progress:0
rdb_last_save_time:1428427941
rdb_last_bgsave_status:ok
rdb_last_bgsave_time_sec:-1
rdb_current_bgsave_time_sec:-1
rdb_last_cow_size:0
aof_enabled:0
aof_rewrite_in_progress:0
aof_rewrite_scheduled:0
aof_last_rewrite_time_sec:-1
aof_current_rewrite_time_sec:-1
aof_last_bgrewrite_status:ok
aof_last_write_status:ok
aof_last_cow_size:0
module_fork_in_progress:0
module_fork_last_cow_size:0

# Stats
total_connections_received:2
total_commands_processed:1
instantaneous_ops_per_sec:0
total_net_input_bytes:381
total_net_output_bytes:71521
instantaneous_input_kbps:876.16
instantaneous_output_kbps:3010.23
rejected_connections:0
sync_full:0
sync_partial_ok:0
sync_partial_err:0
expired_keys:0
expired_stale_perc:0.00
expired_time_cap_reached_count:0
expire_cycle_cpu_milliseconds:669
evicted_keys:0
keyspace_hits:1
keyspace_misses:1
pubsub_channels:0
pubsub_patterns:0
latest_fork_usec:0
migrate_cached_sockets:0
slave_expires_tracked_keys:0
active_defrag_hits:0
active_defrag_misses:0
active_defrag_key_hits:0
active_defrag_key_misses:0
tracking_total_keys:0
tracking_total_items:0
tracking_total_prefixes:0
unexpected_error_replies:0
total_reads_processed:31
total_writes_processed:17
io_threaded_reads_processed:0
io_threaded_writes_processed:0

# Replication
role:master
connected_slaves:2
slave0:ip=127.0.0.1,port=7379,state=online,offset=4556468,lag=0
slave1:ip=127.0.0.1,port=8379,state=send_bulk,offset=0,lag=1
master_replid:8c4d7b768b26826825ceb20ff4a2c7c54616350b
master_replid2:0000000000000000000000000000000000000000
master_repl_offset:0
second_repl_offset:-1
repl_backlog_active:0
repl_backlog_size:1048576
repl_backlog_first_byte_offset:0
repl_backlog_histlen:0

# CPU
used_cpu_sys:0.14
used_cpu_user:0.05
used_cpu_sys_children:0.00
used_cpu_user_children:0.00

# Cluster
cluster_enabled:0

# Commandstats
cmdstat_set:calls=261265,usec=1634157,usec_per_call=6.25
cmdstat_command:calls=1,usec=990,usec_per_call=990.00
cmdstat_publish:calls=488662,usec=8573493,usec_per_call=17.54,rejected_calls=0,failed_calls=0

# Errorstats
errorstat_CLUSTERDOWN:count=8
errorstat_CROSSSLOT:count=3
errorstat_ERR:count=172
errorstat_LOADING:count=4284
errorstat_MASTERDOWN:count=102
errorstat_MOVED:count=3628
errorstat_NOSCRIPT:count=4
errorstat_WRONGPASS:count=2
errorstat_WRONGTYPE:count=30

# Latencystats
latency_percentiles_usec_zadd:p50=9.023,p99=28.031,p99.9=43.007
latency_percentiles_usec_hgetall:p50=11.007,p99=34.047,p99.9=66.047

# Keyspace
db0:keys=2,expires=0,avg_ttl=0

(error) ERR unknown command 'eof'`
