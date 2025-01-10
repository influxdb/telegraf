//go:generate ../../../tools/readme_config_includer/generator
package clickhouse

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var defaultTimeout = 5 * time.Second

type ClickHouse struct {
	Username       string          `toml:"username"`
	Password       string          `toml:"password"`
	Servers        []string        `toml:"servers"`
	AutoDiscovery  bool            `toml:"auto_discovery"`
	ClusterInclude []string        `toml:"cluster_include"`
	ClusterExclude []string        `toml:"cluster_exclude"`
	Timeout        config.Duration `toml:"timeout"`
	Variant        string          `toml:"variant"`

	HTTPClient http.Client
	tls.ClientConfig
}

type connect struct {
	Cluster  string `json:"cluster"`
	ShardNum int    `json:"shard_num"`
	Hostname string `json:"host_name"`
	url      *url.URL
}

func (*ClickHouse) SampleConfig() string {
	return sampleConfig
}

func (ch *ClickHouse) Init() error {
	switch ch.Variant {
	case "":
		ch.Variant = "self-hosted"
	case "self-hosted", "managed":
		// valid options
	default:
		return fmt.Errorf("unknown variant %q", ch.Variant)
	}

	return nil
}

// Start ClickHouse input service
func (ch *ClickHouse) Start(telegraf.Accumulator) error {
	timeout := defaultTimeout
	if time.Duration(ch.Timeout) != 0 {
		timeout = time.Duration(ch.Timeout)
	}
	tlsCfg, err := ch.ClientConfig.TLSConfig()
	if err != nil {
		return err
	}

	ch.HTTPClient = http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:     tlsCfg,
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConnsPerHost: 1,
		},
	}
	return nil
}

// Gather collect data from ClickHouse server
func (ch *ClickHouse) Gather(acc telegraf.Accumulator) (err error) {
	var (
		connects []connect
		exists   = func(host string) bool {
			for _, c := range connects {
				if c.Hostname == host {
					return true
				}
			}
			return false
		}
	)

	for _, server := range ch.Servers {
		u, err := url.Parse(server)
		if err != nil {
			return err
		}
		switch {
		case ch.AutoDiscovery:
			var conns []connect
			if err := ch.execQuery(u, "SELECT cluster, shard_num, host_name FROM system.clusters "+ch.clusterIncludeExcludeFilter(), &conns); err != nil {
				acc.AddError(err)
				continue
			}
			for _, c := range conns {
				if !exists(c.Hostname) {
					c.url = &url.URL{
						Scheme: u.Scheme,
						Host:   net.JoinHostPort(c.Hostname, u.Port()),
					}
					connects = append(connects, c)
				}
			}
		default:
			connects = append(connects, connect{
				Hostname: u.Hostname(),
				url:      u,
			})
		}
	}

	for i := range connects {
		metricsFuncs := []func(acc telegraf.Accumulator, conn *connect) error{
			ch.tables,
			ch.replicationQueue,
			ch.detachedParts,
			ch.dictionaries,
			ch.mutations,
			ch.disks,
			ch.processes,
			ch.textLog,
		}

		// Managed instances on Clickhouse Cloud does not give a user
		// permissions to the zookeeper table
		if ch.Variant != "managed" {
			metricsFuncs = append(metricsFuncs, ch.zookeeper)
		}

		for _, metricFunc := range metricsFuncs {
			if err := metricFunc(acc, &connects[i]); err != nil {
				acc.AddError(err)
			}
		}

		for metric := range commonMetrics {
			if err := ch.commonMetrics(acc, &connects[i], metric); err != nil {
				acc.AddError(err)
			}
		}
	}
	return nil
}

func (ch *ClickHouse) Stop() {
	ch.HTTPClient.CloseIdleConnections()
}

func (ch *ClickHouse) clusterIncludeExcludeFilter() string {
	if len(ch.ClusterInclude) == 0 && len(ch.ClusterExclude) == 0 {
		return ""
	}
	var (
		escape = func(in string) string {
			return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(in) + "'"
		}
		makeFilter = func(expr string, args []string) string {
			in := make([]string, 0, len(args))
			for _, v := range args {
				in = append(in, escape(v))
			}
			return fmt.Sprintf("cluster %s (%s)", expr, strings.Join(in, ", "))
		}
		includeFilter, excludeFilter string
	)

	if len(ch.ClusterInclude) != 0 {
		includeFilter = makeFilter("IN", ch.ClusterInclude)
	}
	if len(ch.ClusterExclude) != 0 {
		excludeFilter = makeFilter("NOT IN", ch.ClusterExclude)
	}
	if includeFilter != "" && excludeFilter != "" {
		return "WHERE " + includeFilter + " OR " + excludeFilter
	}
	if includeFilter == "" && excludeFilter != "" {
		return "WHERE " + excludeFilter
	}
	return "WHERE " + includeFilter
}

func (ch *ClickHouse) commonMetrics(acc telegraf.Accumulator, conn *connect, metric string) error {
	var intResult []struct {
		Metric string   `json:"metric"`
		Value  chUInt64 `json:"value"`
	}

	var floatResult []struct {
		Metric string  `json:"metric"`
		Value  float64 `json:"value"`
	}

	tags := makeDefaultTags(conn)
	fields := make(map[string]interface{})

	if commonMetricsIsFloat[metric] {
		if err := ch.execQuery(conn.url, commonMetrics[metric], &floatResult); err != nil {
			return err
		}
		for _, r := range floatResult {
			fields[internal.SnakeCase(r.Metric)] = r.Value
		}
	} else {
		if err := ch.execQuery(conn.url, commonMetrics[metric], &intResult); err != nil {
			return err
		}
		for _, r := range intResult {
			fields[internal.SnakeCase(r.Metric)] = uint64(r.Value)
		}
	}
	acc.AddFields("clickhouse_"+metric, fields, tags)

	return nil
}

func (ch *ClickHouse) zookeeper(acc telegraf.Accumulator, conn *connect) error {
	var zkExists []struct {
		ZkExists chUInt64 `json:"zk_exists"`
	}

	if err := ch.execQuery(conn.url, systemZookeeperExistsSQL, &zkExists); err != nil {
		return err
	}
	tags := makeDefaultTags(conn)

	if len(zkExists) > 0 && zkExists[0].ZkExists > 0 {
		var zkRootNodes []struct {
			ZkRootNodes chUInt64 `json:"zk_root_nodes"`
		}
		if err := ch.execQuery(conn.url, systemZookeeperRootNodesSQL, &zkRootNodes); err != nil {
			return err
		}

		acc.AddFields("clickhouse_zookeeper",
			map[string]interface{}{
				"root_nodes": uint64(zkRootNodes[0].ZkRootNodes),
			},
			tags,
		)
	}
	return nil
}

func (ch *ClickHouse) replicationQueue(acc telegraf.Accumulator, conn *connect) error {
	var replicationQueueExists []struct {
		ReplicationQueueExists chUInt64 `json:"replication_queue_exists"`
	}

	if err := ch.execQuery(conn.url, systemReplicationExistsSQL, &replicationQueueExists); err != nil {
		return err
	}

	tags := makeDefaultTags(conn)

	if len(replicationQueueExists) > 0 && replicationQueueExists[0].ReplicationQueueExists > 0 {
		var replicationTooManyTries []struct {
			NumTriesReplicas     chUInt64 `json:"replication_num_tries_replicas"`
			TooManyTriesReplicas chUInt64 `json:"replication_too_many_tries_replicas"`
		}
		if err := ch.execQuery(conn.url, systemReplicationNumTriesSQL, &replicationTooManyTries); err != nil {
			return err
		}

		acc.AddFields("clickhouse_replication_queue",
			map[string]interface{}{
				"too_many_tries_replicas": uint64(replicationTooManyTries[0].TooManyTriesReplicas),
				"num_tries_replicas":      uint64(replicationTooManyTries[0].NumTriesReplicas),
			},
			tags,
		)
	}
	return nil
}

func (ch *ClickHouse) detachedParts(acc telegraf.Accumulator, conn *connect) error {
	var detachedParts []struct {
		DetachedParts chUInt64 `json:"detached_parts"`
	}
	if err := ch.execQuery(conn.url, systemDetachedPartsSQL, &detachedParts); err != nil {
		return err
	}

	if len(detachedParts) > 0 {
		tags := makeDefaultTags(conn)
		acc.AddFields("clickhouse_detached_parts",
			map[string]interface{}{
				"detached_parts": uint64(detachedParts[0].DetachedParts),
			},
			tags,
		)
	}
	return nil
}

func (ch *ClickHouse) dictionaries(acc telegraf.Accumulator, conn *connect) error {
	var brokenDictionaries []struct {
		Origin         string   `json:"origin"`
		BytesAllocated chUInt64 `json:"bytes_allocated"`
		Status         string   `json:"status"`
	}
	if err := ch.execQuery(conn.url, systemDictionariesSQL, &brokenDictionaries); err != nil {
		return err
	}

	for _, dict := range brokenDictionaries {
		tags := makeDefaultTags(conn)

		isLoaded := uint64(1)
		if dict.Status != "LOADED" {
			isLoaded = 0
		}

		if dict.Origin != "" {
			tags["dict_origin"] = dict.Origin
			acc.AddFields("clickhouse_dictionaries",
				map[string]interface{}{
					"is_loaded":       isLoaded,
					"bytes_allocated": uint64(dict.BytesAllocated),
				},
				tags,
			)
		}
	}

	return nil
}

func (ch *ClickHouse) mutations(acc telegraf.Accumulator, conn *connect) error {
	var mutationsStatus []struct {
		Failed    chUInt64 `json:"failed"`
		Running   chUInt64 `json:"running"`
		Completed chUInt64 `json:"completed"`
	}
	if err := ch.execQuery(conn.url, systemMutationSQL, &mutationsStatus); err != nil {
		return err
	}

	if len(mutationsStatus) > 0 {
		tags := makeDefaultTags(conn)

		acc.AddFields("clickhouse_mutations",
			map[string]interface{}{
				"failed":    uint64(mutationsStatus[0].Failed),
				"running":   uint64(mutationsStatus[0].Running),
				"completed": uint64(mutationsStatus[0].Completed),
			},
			tags,
		)
	}

	return nil
}

func (ch *ClickHouse) disks(acc telegraf.Accumulator, conn *connect) error {
	var disksStatus []struct {
		Name            string   `json:"name"`
		Path            string   `json:"path"`
		FreePercent     chUInt64 `json:"free_space_percent"`
		KeepFreePercent chUInt64 `json:"keep_free_space_percent"`
	}

	if err := ch.execQuery(conn.url, systemDisksSQL, &disksStatus); err != nil {
		return err
	}

	for _, disk := range disksStatus {
		tags := makeDefaultTags(conn)
		tags["name"] = disk.Name
		tags["path"] = disk.Path

		acc.AddFields("clickhouse_disks",
			map[string]interface{}{
				"free_space_percent":      uint64(disk.FreePercent),
				"keep_free_space_percent": uint64(disk.KeepFreePercent),
			},
			tags,
		)
	}

	return nil
}

func (ch *ClickHouse) processes(acc telegraf.Accumulator, conn *connect) error {
	var processesStats []struct {
		QueryType      string  `json:"query_type"`
		Percentile50   float64 `json:"p50"`
		Percentile90   float64 `json:"p90"`
		LongestRunning float64 `json:"longest_running"`
	}

	if err := ch.execQuery(conn.url, systemProcessesSQL, &processesStats); err != nil {
		return err
	}

	for _, process := range processesStats {
		tags := makeDefaultTags(conn)
		tags["query_type"] = process.QueryType

		acc.AddFields("clickhouse_processes",
			map[string]interface{}{
				"percentile_50":   process.Percentile50,
				"percentile_90":   process.Percentile90,
				"longest_running": process.LongestRunning,
			},
			tags,
		)
	}

	return nil
}

func (ch *ClickHouse) textLog(acc telegraf.Accumulator, conn *connect) error {
	var textLogExists []struct {
		TextLogExists chUInt64 `json:"text_log_exists"`
	}

	if err := ch.execQuery(conn.url, systemTextLogExistsSQL, &textLogExists); err != nil {
		return err
	}

	if len(textLogExists) > 0 && textLogExists[0].TextLogExists > 0 {
		var textLogLast10MinMessages []struct {
			Level             string   `json:"level"`
			MessagesLast10Min chUInt64 `json:"messages_last_10_min"`
		}
		if err := ch.execQuery(conn.url, systemTextLogSQL, &textLogLast10MinMessages); err != nil {
			return err
		}

		for _, textLogItem := range textLogLast10MinMessages {
			tags := makeDefaultTags(conn)
			tags["level"] = textLogItem.Level
			acc.AddFields("clickhouse_text_log",
				map[string]interface{}{
					"messages_last_10_min": uint64(textLogItem.MessagesLast10Min),
				},
				tags,
			)
		}
	}
	return nil
}

func (ch *ClickHouse) tables(acc telegraf.Accumulator, conn *connect) error {
	var parts []struct {
		Database string   `json:"database"`
		Table    string   `json:"table"`
		Bytes    chUInt64 `json:"bytes"`
		Parts    chUInt64 `json:"parts"`
		Rows     chUInt64 `json:"rows"`
	}

	if err := ch.execQuery(conn.url, systemPartsSQL, &parts); err != nil {
		return err
	}
	tags := makeDefaultTags(conn)

	for _, part := range parts {
		tags["table"] = part.Table
		tags["database"] = part.Database
		acc.AddFields("clickhouse_tables",
			map[string]interface{}{
				"bytes": uint64(part.Bytes),
				"parts": uint64(part.Parts),
				"rows":  uint64(part.Rows),
			},
			tags,
		)
	}
	return nil
}

func makeDefaultTags(conn *connect) map[string]string {
	tags := map[string]string{
		"source": conn.Hostname,
	}
	if len(conn.Cluster) != 0 {
		tags["cluster"] = conn.Cluster
	}
	if conn.ShardNum != 0 {
		tags["shard_num"] = strconv.Itoa(conn.ShardNum)
	}
	return tags
}

type clickhouseError struct {
	StatusCode int
	body       []byte
}

func (e *clickhouseError) Error() string {
	return fmt.Sprintf("received error code %d: %s", e.StatusCode, e.body)
}

func (ch *ClickHouse) execQuery(address *url.URL, query string, i interface{}) error {
	q := address.Query()
	q.Set("query", query+" FORMAT JSON")
	address.RawQuery = q.Encode()
	req, err := http.NewRequest("GET", address.String(), nil)
	if err != nil {
		return err
	}
	if ch.Username != "" {
		req.Header.Add("X-ClickHouse-User", ch.Username)
	}
	if ch.Password != "" {
		req.Header.Add("X-ClickHouse-Key", ch.Password)
	}
	resp, err := ch.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		//nolint:errcheck // reading body for error reporting
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return &clickhouseError{
			StatusCode: resp.StatusCode,
			body:       body,
		}
	}
	var response struct {
		Data json.RawMessage
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return err
	}
	if err := json.Unmarshal(response.Data, i); err != nil {
		return err
	}

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	return nil
}

// see https://clickhouse.yandex/docs/en/operations/settings/settings/#session_settings-output_format_json_quote_64bit_integers
type chUInt64 uint64

func (i *chUInt64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimPrefix(b, []byte(`"`))
	b = bytes.TrimSuffix(b, []byte(`"`))
	v, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		return err
	}
	*i = chUInt64(v)
	return nil
}

const (
	systemEventsSQL       = "SELECT event AS metric, toUInt64(value) AS value FROM system.events"
	systemMetricsSQL      = "SELECT          metric, toUInt64(value) AS value FROM system.metrics"
	systemAsyncMetricsSQL = "SELECT          metric, toFloat64(value) AS value FROM system.asynchronous_metrics"
	systemPartsSQL        = `
		SELECT
			database,
			table,
			SUM(bytes) AS bytes,
			COUNT(*)   AS parts,
			SUM(rows)  AS rows
		FROM system.parts
		WHERE active = 1
		GROUP BY
			database, table
		ORDER BY
			database, table
	`
	systemZookeeperExistsSQL    = "SELECT count() AS zk_exists FROM system.tables WHERE database='system' AND name='zookeeper'"
	systemZookeeperRootNodesSQL = "SELECT count() AS zk_root_nodes FROM system.zookeeper WHERE path='/'"

	systemReplicationExistsSQL   = "SELECT count() AS replication_queue_exists FROM system.tables WHERE database='system' AND name='replication_queue'"
	systemReplicationNumTriesSQL = "SELECT countIf(num_tries>1) AS replication_num_tries_replicas, countIf(num_tries>100) " +
		"AS replication_too_many_tries_replicas FROM system.replication_queue SETTINGS empty_result_for_aggregation_by_empty_set=0"

	systemDetachedPartsSQL = "SELECT count() AS detached_parts FROM system.detached_parts SETTINGS empty_result_for_aggregation_by_empty_set=0"

	systemDictionariesSQL = "SELECT origin, status, bytes_allocated FROM system.dictionaries"

	systemMutationSQL = "SELECT countIf(latest_fail_time>toDateTime('0000-00-00 00:00:00') AND is_done=0) " +
		"AS failed, countIf(latest_fail_time=toDateTime('0000-00-00 00:00:00') AND is_done=0) " +
		"AS running, countIf(is_done=1) AS completed FROM system.mutations SETTINGS empty_result_for_aggregation_by_empty_set=0"
	systemDisksSQL = "SELECT name, path, toUInt64(100*free_space / total_space) " +
		"AS free_space_percent, toUInt64( 100 * keep_free_space / total_space) AS keep_free_space_percent FROM system.disks"
	systemProcessesSQL = "SELECT multiIf(positionCaseInsensitive(query,'select')=1,'select',positionCaseInsensitive(query,'insert')=1,'insert','other') " +
		"AS query_type, quantile\n(0.5)(elapsed) AS p50, quantile(0.9)(elapsed) AS p90, max(elapsed) AS longest_running " +
		"FROM system.processes GROUP BY query_type SETTINGS empty_result_for_aggregation_by_empty_set=0"

	systemTextLogExistsSQL = "SELECT count() AS text_log_exists FROM system.tables WHERE database='system' AND name='text_log'"
	systemTextLogSQL       = "SELECT count() AS messages_last_10_min, level FROM system.text_log " +
		"WHERE level <= 'Notice' AND event_time >= now() - INTERVAL 600 SECOND GROUP BY level SETTINGS empty_result_for_aggregation_by_empty_set=0"
)

var commonMetrics = map[string]string{
	"events":               systemEventsSQL,
	"metrics":              systemMetricsSQL,
	"asynchronous_metrics": systemAsyncMetricsSQL,
}

var commonMetricsIsFloat = map[string]bool{
	"events":               false,
	"metrics":              false,
	"asynchronous_metrics": true,
}

var _ telegraf.ServiceInput = &ClickHouse{}

func init() {
	inputs.Add("clickhouse", func() telegraf.Input {
		return &ClickHouse{
			AutoDiscovery: true,
			ClientConfig: tls.ClientConfig{
				InsecureSkipVerify: false,
			},
			Timeout: config.Duration(defaultTimeout),
		}
	})
}
