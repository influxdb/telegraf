//go:generate ../../../tools/readme_config_includer/generator
package redis

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	replicationSlaveMetricPrefix = regexp.MustCompile(`^slave\d+`)
	tracking                     = map[string]string{
		"uptime_in_seconds": "uptime",
		"connected_clients": "clients",
		"role":              "replication_role",
	}
)

type Redis struct {
	Commands []*redisCommand `toml:"commands"`
	Servers  []string        `toml:"servers"`
	Username string          `toml:"username"`
	Password string          `toml:"password"`

	tls.ClientConfig

	Log telegraf.Logger `toml:"-"`

	clients   []client
	connected bool
}

type redisCommand struct {
	Command []interface{} `toml:"command"`
	Field   string        `toml:"field"`
	Type    string        `toml:"type"`
}

type redisClient struct {
	client *redis.Client
	tags   map[string]string
}

// redisFieldTypes defines the types expected for each of the fields redis reports on
type redisFieldTypes struct {
	ActiveDefragHits            int64   `json:"active_defrag_hits"`
	ActiveDefragKeyHits         int64   `json:"active_defrag_key_hits"`
	ActiveDefragKeyMisses       int64   `json:"active_defrag_key_misses"`
	ActiveDefragMisses          int64   `json:"active_defrag_misses"`
	ActiveDefragRunning         int64   `json:"active_defrag_running"`
	AllocatorActive             int64   `json:"allocator_active"`
	AllocatorAllocated          int64   `json:"allocator_allocated"`
	AllocatorFragBytes          float64 `json:"allocator_frag_bytes"` // for historical reasons this was left as float although redis reports it as an int
	AllocatorFragRatio          float64 `json:"allocator_frag_ratio"`
	AllocatorResident           int64   `json:"allocator_resident"`
	AllocatorRssBytes           int64   `json:"allocator_rss_bytes"`
	AllocatorRssRatio           float64 `json:"allocator_rss_ratio"`
	AofCurrentRewriteTimeSec    int64   `json:"aof_current_rewrite_time_sec"`
	AofEnabled                  int64   `json:"aof_enabled"`
	AofLastBgrewriteStatus      string  `json:"aof_last_bgrewrite_status"`
	AofLastCowSize              int64   `json:"aof_last_cow_size"`
	AofLastRewriteTimeSec       int64   `json:"aof_last_rewrite_time_sec"`
	AofLastWriteStatus          string  `json:"aof_last_write_status"`
	AofRewriteInProgress        int64   `json:"aof_rewrite_in_progress"`
	AofRewriteScheduled         int64   `json:"aof_rewrite_scheduled"`
	BlockedClients              int64   `json:"blocked_clients"`
	ClientRecentMaxInputBuffer  int64   `json:"client_recent_max_input_buffer"`
	ClientRecentMaxOutputBuffer int64   `json:"client_recent_max_output_buffer"`
	Clients                     int64   `json:"clients"`
	ClientsInTimeoutTable       int64   `json:"clients_in_timeout_table"`
	ClusterEnabled              int64   `json:"cluster_enabled"`
	ConnectedSlaves             int64   `json:"connected_slaves"`
	EvictedKeys                 int64   `json:"evicted_keys"`
	ExpireCycleCPUMilliseconds  int64   `json:"expire_cycle_cpu_milliseconds"`
	ExpiredKeys                 int64   `json:"expired_keys"`
	ExpiredStalePerc            float64 `json:"expired_stale_perc"`
	ExpiredTimeCapReachedCount  int64   `json:"expired_time_cap_reached_count"`
	InstantaneousInputKbps      float64 `json:"instantaneous_input_kbps"`
	InstantaneousOpsPerSec      int64   `json:"instantaneous_ops_per_sec"`
	InstantaneousOutputKbps     float64 `json:"instantaneous_output_kbps"`
	IoThreadedReadsProcessed    int64   `json:"io_threaded_reads_processed"`
	IoThreadedWritesProcessed   int64   `json:"io_threaded_writes_processed"`
	KeyspaceHits                int64   `json:"keyspace_hits"`
	KeyspaceMisses              int64   `json:"keyspace_misses"`
	LatestForkUsec              int64   `json:"latest_fork_usec"`
	LazyfreePendingObjects      int64   `json:"lazyfree_pending_objects"`
	Loading                     int64   `json:"loading"`
	LruClock                    int64   `json:"lru_clock"`
	MasterReplOffset            int64   `json:"master_repl_offset"`
	MaxMemory                   int64   `json:"maxmemory"`
	MaxMemoryPolicy             string  `json:"maxmemory_policy"`
	MemAofBuffer                int64   `json:"mem_aof_buffer"`
	MemClientsNormal            int64   `json:"mem_clients_normal"`
	MemClientsSlaves            int64   `json:"mem_clients_slaves"`
	MemFragmentationBytes       int64   `json:"mem_fragmentation_bytes"`
	MemFragmentationRatio       float64 `json:"mem_fragmentation_ratio"`
	MemNotCountedForEvict       int64   `json:"mem_not_counted_for_evict"`
	MemReplicationBacklog       int64   `json:"mem_replication_backlog"`
	MigrateCachedSockets        int64   `json:"migrate_cached_sockets"`
	ModuleForkInProgress        int64   `json:"module_fork_in_progress"`
	ModuleForkLastCowSize       int64   `json:"module_fork_last_cow_size"`
	NumberOfCachedScripts       int64   `json:"number_of_cached_scripts"`
	PubsubChannels              int64   `json:"pubsub_channels"`
	PubsubPatterns              int64   `json:"pubsub_patterns"`
	RdbBgsaveInProgress         int64   `json:"rdb_bgsave_in_progress"`
	RdbChangesSinceLastSave     int64   `json:"rdb_changes_since_last_save"`
	RdbCurrentBgsaveTimeSec     int64   `json:"rdb_current_bgsave_time_sec"`
	RdbLastBgsaveStatus         string  `json:"rdb_last_bgsave_status"`
	RdbLastBgsaveTimeSec        int64   `json:"rdb_last_bgsave_time_sec"`
	RdbLastCowSize              int64   `json:"rdb_last_cow_size"`
	RdbLastSaveTime             int64   `json:"rdb_last_save_time"`
	RdbLastSaveTimeElapsed      int64   `json:"rdb_last_save_time_elapsed"`
	RedisVersion                string  `json:"redis_version"`
	RejectedConnections         int64   `json:"rejected_connections"`
	ReplBacklogActive           int64   `json:"repl_backlog_active"`
	ReplBacklogFirstByteOffset  int64   `json:"repl_backlog_first_byte_offset"`
	ReplBacklogHistlen          int64   `json:"repl_backlog_histlen"`
	ReplBacklogSize             int64   `json:"repl_backlog_size"`
	RssOverheadBytes            int64   `json:"rss_overhead_bytes"`
	RssOverheadRatio            float64 `json:"rss_overhead_ratio"`
	SecondReplOffset            int64   `json:"second_repl_offset"`
	SlaveExpiresTrackedKeys     int64   `json:"slave_expires_tracked_keys"`
	SyncFull                    int64   `json:"sync_full"`
	SyncPartialErr              int64   `json:"sync_partial_err"`
	SyncPartialOk               int64   `json:"sync_partial_ok"`
	TotalCommandsProcessed      int64   `json:"total_commands_processed"`
	TotalConnectionsReceived    int64   `json:"total_connections_received"`
	TotalNetInputBytes          int64   `json:"total_net_input_bytes"`
	TotalNetOutputBytes         int64   `json:"total_net_output_bytes"`
	TotalReadsProcessed         int64   `json:"total_reads_processed"`
	TotalSystemMemory           int64   `json:"total_system_memory"`
	TotalWritesProcessed        int64   `json:"total_writes_processed"`
	TrackingClients             int64   `json:"tracking_clients"`
	TrackingTotalItems          int64   `json:"tracking_total_items"`
	TrackingTotalKeys           int64   `json:"tracking_total_keys"`
	TrackingTotalPrefixes       int64   `json:"tracking_total_prefixes"`
	UnexpectedErrorReplies      int64   `json:"unexpected_error_replies"`
	Uptime                      int64   `json:"uptime"`
	UsedCPUSys                  float64 `json:"used_cpu_sys"`
	UsedCPUSysChildren          float64 `json:"used_cpu_sys_children"`
	UsedCPUUser                 float64 `json:"used_cpu_user"`
	UsedCPUUserChildren         float64 `json:"used_cpu_user_children"`
	UsedMemory                  int64   `json:"used_memory"`
	UsedMemoryDataset           int64   `json:"used_memory_dataset"`
	UsedMemoryDatasetPerc       float64 `json:"used_memory_dataset_perc"`
	UsedMemoryLua               int64   `json:"used_memory_lua"`
	UsedMemoryOverhead          int64   `json:"used_memory_overhead"`
	UsedMemoryPeak              int64   `json:"used_memory_peak"`
	UsedMemoryPeakPerc          float64 `json:"used_memory_peak_perc"`
	UsedMemoryRss               int64   `json:"used_memory_rss"`
	UsedMemoryScripts           int64   `json:"used_memory_scripts"`
	UsedMemoryStartup           int64   `json:"used_memory_startup"`
}

type client interface {
	do(returnType string, args ...interface{}) (interface{}, error)
	info() *redis.StringCmd
	baseTags() map[string]string
	close() error
}

func (*Redis) SampleConfig() string {
	return sampleConfig
}

func (r *Redis) Init() error {
	for _, command := range r.Commands {
		if command.Type != "string" && command.Type != "integer" && command.Type != "float" {
			return fmt.Errorf(`unknown result type: expected one of "string", "integer", "float"; got %q`, command.Type)
		}
	}

	return nil
}

func (*Redis) Start(telegraf.Accumulator) error {
	return nil
}

func (r *Redis) Gather(acc telegraf.Accumulator) error {
	if !r.connected {
		err := r.connect()
		if err != nil {
			return err
		}
	}

	var wg sync.WaitGroup

	for _, cl := range r.clients {
		wg.Add(1)
		go func(client client) {
			defer wg.Done()
			acc.AddError(gatherServer(client, acc))
			acc.AddError(r.gatherCommandValues(client, acc))
		}(cl)
	}

	wg.Wait()
	return nil
}

// Stop close the client through ServiceInput interface Start/Stop methods impl.
func (r *Redis) Stop() {
	for _, c := range r.clients {
		err := c.close()
		if err != nil {
			r.Log.Errorf("error closing client: %v", err)
		}
	}
}

func (r *Redis) connect() error {
	if r.connected {
		return nil
	}

	if len(r.Servers) == 0 {
		r.Servers = []string{"tcp://localhost:6379"}
	}

	r.clients = make([]client, 0, len(r.Servers))
	for _, serv := range r.Servers {
		if !strings.HasPrefix(serv, "tcp://") && !strings.HasPrefix(serv, "unix://") {
			r.Log.Warn("Server URL found without scheme; please update your configuration file")
			serv = "tcp://" + serv
		}

		u, err := url.Parse(serv)
		if err != nil {
			return fmt.Errorf("unable to parse to address %q: %w", serv, err)
		}

		username := ""
		password := ""
		if u.User != nil {
			username = u.User.Username()
			pw, ok := u.User.Password()
			if ok {
				password = pw
			}
		}
		if len(r.Username) > 0 {
			username = r.Username
		}
		if len(r.Password) > 0 {
			password = r.Password
		}

		var address string
		if u.Scheme == "unix" {
			address = u.Path
		} else {
			address = u.Host
		}

		tlsConfig, err := r.ClientConfig.TLSConfig()
		if err != nil {
			return err
		}

		client := redis.NewClient(
			&redis.Options{
				Addr:      address,
				Username:  username,
				Password:  password,
				Network:   u.Scheme,
				PoolSize:  1,
				TLSConfig: tlsConfig,
			},
		)

		tags := make(map[string]string, 2)
		if u.Scheme == "unix" {
			tags["socket"] = u.Path
		} else {
			tags["server"] = u.Hostname()
			tags["port"] = u.Port()
		}

		r.clients = append(r.clients, &redisClient{
			client: client,
			tags:   tags,
		})
	}

	r.connected = true
	return nil
}

func (r *Redis) gatherCommandValues(client client, acc telegraf.Accumulator) error {
	fields := make(map[string]interface{})
	for _, command := range r.Commands {
		val, err := client.do(command.Type, command.Command...)
		if err != nil {
			if strings.Contains(err.Error(), "unexpected type=") {
				return fmt.Errorf("could not get command result: %w", err)
			}

			return err
		}

		fields[command.Field] = val
	}

	acc.AddFields("redis_commands", fields, client.baseTags())

	return nil
}

func (r *redisClient) do(returnType string, args ...interface{}) (interface{}, error) {
	rawVal := r.client.Do(context.Background(), args...)

	switch returnType {
	case "integer":
		return rawVal.Int64()
	case "string":
		return rawVal.Text()
	case "float":
		return rawVal.Float64()
	default:
		return rawVal.Text()
	}
}

func (r *redisClient) info() *redis.StringCmd {
	return r.client.Info(context.Background(), "ALL")
}

func (r *redisClient) baseTags() map[string]string {
	tags := make(map[string]string)
	for k, v := range r.tags {
		tags[k] = v
	}
	return tags
}

func (r *redisClient) close() error {
	return r.client.Close()
}

func gatherServer(client client, acc telegraf.Accumulator) error {
	info, err := client.info().Result()
	if err != nil {
		return err
	}

	rdr := strings.NewReader(info)
	return gatherInfoOutput(rdr, acc, client.baseTags())
}

func gatherInfoOutput(rdr io.Reader, acc telegraf.Accumulator, tags map[string]string) error {
	var section string
	var keyspaceHits, keyspaceMisses int64

	scanner := bufio.NewScanner(rdr)
	fields := make(map[string]interface{})
	for scanner.Scan() {
		line := scanner.Text()

		if len(line) == 0 {
			continue
		}

		if line[0] == '#' {
			if len(line) > 2 {
				section = line[2:]
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]

		if section == "Server" {
			if name != "lru_clock" && name != "uptime_in_seconds" && name != "redis_version" {
				continue
			}
		}

		if strings.HasPrefix(name, "master_replid") {
			continue
		}

		if name == "mem_allocator" {
			continue
		}

		if strings.HasSuffix(name, "_human") {
			continue
		}

		metric, ok := tracking[name]
		if !ok {
			if section == "Keyspace" {
				kline := strings.TrimSpace(parts[1])
				gatherKeyspaceLine(name, kline, acc, tags)
				continue
			}
			if section == "Commandstats" {
				kline := strings.TrimSpace(parts[1])
				gatherCommandStateLine(name, kline, acc, tags)
				continue
			}
			if section == "Latencystats" {
				kline := strings.TrimSpace(parts[1])
				gatherLatencyStatsLine(name, kline, acc, tags)
				continue
			}
			if section == "Replication" && replicationSlaveMetricPrefix.MatchString(name) {
				kline := strings.TrimSpace(parts[1])
				gatherReplicationLine(name, kline, acc, tags)
				continue
			}
			if section == "Errorstats" {
				kline := strings.TrimSpace(parts[1])
				gatherErrorStatsLine(name, kline, acc, tags)
				continue
			}

			metric = name
		}

		val := strings.TrimSpace(parts[1])

		// Some percentage values have a "%" suffix that we need to get rid of before int/float conversion
		val = strings.TrimSuffix(val, "%")

		// Try parsing as int
		if ival, err := strconv.ParseInt(val, 10, 64); err == nil {
			switch name {
			case "keyspace_hits":
				keyspaceHits = ival
			case "keyspace_misses":
				keyspaceMisses = ival
			case "rdb_last_save_time":
				// influxdb can't calculate this, so we have to do it
				fields["rdb_last_save_time_elapsed"] = time.Now().Unix() - ival
			}
			fields[metric] = ival
			continue
		}

		// Try parsing as a float
		if fval, err := strconv.ParseFloat(val, 64); err == nil {
			fields[metric] = fval
			continue
		}

		// Treat it as a string

		if name == "role" {
			tags["replication_role"] = val
			continue
		}

		fields[metric] = val
	}
	var keyspaceHitrate float64
	if keyspaceHits != 0 || keyspaceMisses != 0 {
		keyspaceHitrate = float64(keyspaceHits) / float64(keyspaceHits+keyspaceMisses)
	}
	fields["keyspace_hitrate"] = keyspaceHitrate

	o := redisFieldTypes{}

	setStructFieldsFromObject(fields, &o)
	setExistingFieldsFromStruct(fields, &o)

	acc.AddFields("redis", fields, tags)
	return nil
}

// Parse the special Keyspace line at end of redis stats
// This is a special line that looks something like:
//
//	db0:keys=2,expires=0,avg_ttl=0
//
// And there is one for each db on the redis instance
func gatherKeyspaceLine(name, line string, acc telegraf.Accumulator, globalTags map[string]string) {
	if strings.Contains(line, "keys=") {
		fields := make(map[string]interface{})
		tags := make(map[string]string)
		for k, v := range globalTags {
			tags[k] = v
		}
		tags["database"] = name
		dbparts := strings.Split(line, ",")
		for _, dbp := range dbparts {
			kv := strings.Split(dbp, "=")
			ival, err := strconv.ParseInt(kv[1], 10, 64)
			if err == nil {
				fields[kv[0]] = ival
			}
		}
		acc.AddFields("redis_keyspace", fields, tags)
	}
}

// Parse the special cmdstat lines.
// Example:
//
//	cmdstat_publish:calls=33791,usec=208789,usec_per_call=6.18
//
// Tag: command=publish; Fields: calls=33791i,usec=208789i,usec_per_call=6.18
func gatherCommandStateLine(name, line string, acc telegraf.Accumulator, globalTags map[string]string) {
	if !strings.HasPrefix(name, "cmdstat") {
		return
	}

	fields := make(map[string]interface{})
	tags := make(map[string]string)
	for k, v := range globalTags {
		tags[k] = v
	}
	tags["command"] = strings.TrimPrefix(name, "cmdstat_")
	parts := strings.Split(line, ",")
	for _, part := range parts {
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "calls":
			fallthrough
		case "usec", "rejected_calls", "failed_calls":
			ival, err := strconv.ParseInt(kv[1], 10, 64)
			if err == nil {
				fields[kv[0]] = ival
			}
		case "usec_per_call":
			fval, err := strconv.ParseFloat(kv[1], 64)
			if err == nil {
				fields[kv[0]] = fval
			}
		}
	}
	acc.AddFields("redis_cmdstat", fields, tags)
}

// Parse the special latency_percentiles_usec lines.
// Example:
//
//	latency_percentiles_usec_zadd:p50=9.023,p99=28.031,p99.9=43.007
//
// Tag: command=zadd; Fields: p50=9.023,p99=28.031,p99.9=43.007
func gatherLatencyStatsLine(name, line string, acc telegraf.Accumulator, globalTags map[string]string) {
	if !strings.HasPrefix(name, "latency_percentiles_usec") {
		return
	}

	fields := make(map[string]interface{})
	tags := make(map[string]string)
	for k, v := range globalTags {
		tags[k] = v
	}
	tags["command"] = strings.TrimPrefix(name, "latency_percentiles_usec_")
	parts := strings.Split(line, ",")
	for _, part := range parts {
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "p50", "p99", "p99.9":
			fval, err := strconv.ParseFloat(kv[1], 64)
			if err == nil {
				fields[kv[0]] = fval
			}
		}
	}
	acc.AddFields("redis_latency_percentiles_usec", fields, tags)
}

// Parse the special Replication line
// Example:
//
//	slave0:ip=127.0.0.1,port=7379,state=online,offset=4556468,lag=0
//
// This line will only be visible when a node has a replica attached.
func gatherReplicationLine(name, line string, acc telegraf.Accumulator, globalTags map[string]string) {
	fields := make(map[string]interface{})
	tags := make(map[string]string)
	for k, v := range globalTags {
		tags[k] = v
	}

	tags["replica_id"] = strings.TrimLeft(name, "slave")
	tags["replication_role"] = "slave"

	parts := strings.Split(line, ",")
	for _, part := range parts {
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "ip":
			tags["replica_ip"] = kv[1]
		case "port":
			tags["replica_port"] = kv[1]
		case "state":
			tags[kv[0]] = kv[1]
		default:
			ival, err := strconv.ParseInt(kv[1], 10, 64)
			if err == nil {
				fields[kv[0]] = ival
			}
		}
	}

	acc.AddFields("redis_replication", fields, tags)
}

// Parse the special Errorstats lines.
// Example:
//
// errorstat_ERR:count=37
// errorstat_MOVED:count=3626
func gatherErrorStatsLine(name, line string, acc telegraf.Accumulator, globalTags map[string]string) {
	tags := make(map[string]string, len(globalTags)+1)
	for k, v := range globalTags {
		tags[k] = v
	}
	tags["err"] = strings.TrimPrefix(name, "errorstat_")
	kv := strings.Split(line, "=")
	if len(kv) < 2 {
		acc.AddError(fmt.Errorf("invalid line for %q: %s", name, line))
		return
	}
	ival, err := strconv.ParseInt(kv[1], 10, 64)
	if err != nil {
		acc.AddError(fmt.Errorf("parsing value in line %q failed: %w", line, err))
		return
	}

	fields := map[string]interface{}{"total": ival}
	acc.AddFields("redis_errorstat", fields, tags)
}

func setExistingFieldsFromStruct(fields map[string]interface{}, o *redisFieldTypes) {
	val := reflect.ValueOf(o).Elem()
	typ := val.Type()

	for key := range fields {
		if _, exists := fields[key]; exists {
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				jsonFieldName := f.Tag.Get("json")
				if jsonFieldName == key {
					fields[key] = val.Field(i).Interface()
					break
				}
			}
		}
	}
}

func setStructFieldsFromObject(fields map[string]interface{}, o *redisFieldTypes) {
	val := reflect.ValueOf(o).Elem()
	typ := val.Type()

	for key, value := range fields {
		if _, exists := fields[key]; exists {
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				jsonFieldName := f.Tag.Get("json")
				if jsonFieldName == key {
					structFieldValue := val.Field(i)
					structFieldValue.Set(coerceType(value, structFieldValue.Type()))
					break
				}
			}
		}
	}
}

func coerceType(value interface{}, typ reflect.Type) reflect.Value {
	switch sourceType := value.(type) {
	case bool:
		switch typ.Kind() {
		case reflect.String:
			if sourceType {
				value = "true"
			} else {
				value = "false"
			}
		case reflect.Int64:
			if sourceType {
				value = int64(1)
			} else {
				value = int64(0)
			}
		case reflect.Float64:
			if sourceType {
				value = float64(1)
			} else {
				value = float64(0)
			}
		default:
			panic("unhandled destination type " + typ.Kind().String())
		}
	case int, int8, int16, int32, int64:
		switch typ.Kind() {
		case reflect.String:
			value = fmt.Sprintf("%d", value)
		case reflect.Int64:
			// types match
		case reflect.Float64:
			value = float64(reflect.ValueOf(sourceType).Int())
		default:
			panic("unhandled destination type " + typ.Kind().String())
		}
	case uint, uint8, uint16, uint32, uint64:
		switch typ.Kind() {
		case reflect.String:
			value = fmt.Sprintf("%d", value)
		case reflect.Int64:
			// types match
		case reflect.Float64:
			value = float64(reflect.ValueOf(sourceType).Uint())
		default:
			panic("unhandled destination type " + typ.Kind().String())
		}
	case float32, float64:
		switch typ.Kind() {
		case reflect.String:
			value = fmt.Sprintf("%f", value)
		case reflect.Int64:
			value = int64(reflect.ValueOf(sourceType).Float())
		case reflect.Float64:
			// types match
		default:
			panic("unhandled destination type " + typ.Kind().String())
		}
	case string:
		switch typ.Kind() {
		case reflect.String:
			// types match
		case reflect.Int64:
			//nolint:errcheck // no way to propagate, shouldn't panic
			value, _ = strconv.ParseInt(value.(string), 10, 64)
		case reflect.Float64:
			//nolint:errcheck // no way to propagate, shouldn't panic
			value, _ = strconv.ParseFloat(value.(string), 64)
		default:
			panic("unhandled destination type " + typ.Kind().String())
		}
	default:
		panic(fmt.Sprintf("unhandled source type %T", sourceType))
	}
	return reflect.ValueOf(value)
}

func init() {
	inputs.Add("redis", func() telegraf.Input {
		return &Redis{}
	})
}
