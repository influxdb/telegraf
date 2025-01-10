//go:generate ../../../tools/readme_config_includer/generator
package couchbase

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/couchbase/go-couchbase"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var regexpURI = regexp.MustCompile(`(\S+://)?(\S+\:\S+@)`)

type Couchbase struct {
	Servers             []string `toml:"servers"`
	BucketStatsIncluded []string `toml:"bucket_stats_included"`
	ClusterBucketStats  bool     `toml:"cluster_bucket_stats"`
	NodeBucketStats     bool     `toml:"node_bucket_stats"`
	AdditionalStats     []string `toml:"additional_stats"`

	bucketInclude filter.Filter
	client        *http.Client

	tls.ClientConfig
}

type autoFailover struct {
	Count    int  `json:"count"`
	Enabled  bool `json:"enabled"`
	MaxCount int  `json:"maxCount"`
	Timeout  int  `json:"timeout"`
}

func (*Couchbase) SampleConfig() string {
	return sampleConfig
}

func (cb *Couchbase) Init() error {
	f, err := filter.NewIncludeExcludeFilter(cb.BucketStatsIncluded, nil)
	if err != nil {
		return err
	}

	cb.bucketInclude = f

	tlsConfig, err := cb.TLSConfig()
	if err != nil {
		return err
	}

	cb.client = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: couchbase.MaxIdleConnsPerHost,
			TLSClientConfig:     tlsConfig,
		},
	}

	couchbase.SetSkipVerify(cb.ClientConfig.InsecureSkipVerify)
	couchbase.SetCertFile(cb.ClientConfig.TLSCert)
	couchbase.SetKeyFile(cb.ClientConfig.TLSKey)
	couchbase.SetRootFile(cb.ClientConfig.TLSCA)

	return nil
}

// Gather reads stats from all configured clusters. Accumulates stats.
// Returns one of the errors encountered while gathering stats (if any).
func (cb *Couchbase) Gather(acc telegraf.Accumulator) error {
	if len(cb.Servers) == 0 {
		return cb.gatherServer(acc, "http://localhost:8091/")
	}

	var wg sync.WaitGroup
	for _, serv := range cb.Servers {
		wg.Add(1)
		go func(serv string) {
			defer wg.Done()
			acc.AddError(cb.gatherServer(acc, serv))
		}(serv)
	}

	wg.Wait()

	return nil
}

func (cb *Couchbase) gatherServer(acc telegraf.Accumulator, addr string) error {
	escapedAddr := regexpURI.ReplaceAllString(addr, "${1}")

	client, err := couchbase.Connect(addr)
	if err != nil {
		return err
	}

	// `default` is the only possible pool name. It's a
	// placeholder for a possible future Couchbase feature. See
	// http://stackoverflow.com/a/16990911/17498.
	pool, err := client.GetPool("default")
	if err != nil {
		return err
	}
	defer pool.Close()

	for i := 0; i < len(pool.Nodes); i++ {
		node := pool.Nodes[i]
		tags := map[string]string{"cluster": escapedAddr, "hostname": node.Hostname}
		fields := make(map[string]interface{})
		fields["memory_free"] = node.MemoryFree
		fields["memory_total"] = node.MemoryTotal
		acc.AddFields("couchbase_node", fields, tags)
	}

	cluster := regexpURI.ReplaceAllString(addr, "${1}")
	for name, bucket := range pool.BucketMap {
		if cb.ClusterBucketStats {
			fields := cb.basicBucketStats(bucket.BasicStats)
			tags := map[string]string{"cluster": cluster, "bucket": name}

			err := cb.gatherDetailedBucketStats(addr, name, "", fields)
			if err != nil {
				return err
			}

			acc.AddFields("couchbase_bucket", fields, tags)
		}

		if cb.NodeBucketStats {
			for _, node := range bucket.Nodes() {
				fields := cb.basicBucketStats(bucket.BasicStats)
				tags := map[string]string{"cluster": cluster, "bucket": name, "hostname": node.Hostname}

				err := cb.gatherDetailedBucketStats(addr, name, node.Hostname, fields)
				if err != nil {
					return err
				}

				acc.AddFields("couchbase_node_bucket", fields, tags)
			}
		}
	}

	if choice.Contains("autofailover", cb.AdditionalStats) {
		tags := map[string]string{"cluster": cluster}
		fields, err := cb.gatherAutoFailoverStats(addr)
		if err != nil {
			return fmt.Errorf("unable to collect autofailover settings: %w", err)
		}

		acc.AddFields("couchbase_autofailover", fields, tags)
	}

	return nil
}

func (cb *Couchbase) gatherAutoFailoverStats(server string) (map[string]any, error) {
	var fields map[string]any

	url := server + "/settings/autoFailover"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fields, err
	}

	r, err := cb.client.Do(req)
	if err != nil {
		return fields, err
	}
	defer r.Body.Close()

	var stats autoFailover
	if err := json.NewDecoder(r.Body).Decode(&stats); err != nil {
		return fields, err
	}

	fields = map[string]any{
		"count":     stats.Count,
		"enabled":   stats.Enabled,
		"max_count": stats.MaxCount,
		"timeout":   stats.Timeout,
	}

	return fields, nil
}

// basicBucketStats gets the basic bucket statistics
func (cb *Couchbase) basicBucketStats(basicStats map[string]interface{}) map[string]interface{} {
	fields := make(map[string]interface{})
	cb.addBucketField(fields, "quota_percent_used", basicStats["quotaPercentUsed"])
	cb.addBucketField(fields, "ops_per_sec", basicStats["opsPerSec"])
	cb.addBucketField(fields, "disk_fetches", basicStats["diskFetches"])
	cb.addBucketField(fields, "item_count", basicStats["itemCount"])
	cb.addBucketField(fields, "disk_used", basicStats["diskUsed"])
	cb.addBucketField(fields, "data_used", basicStats["dataUsed"])
	cb.addBucketField(fields, "mem_used", basicStats["memUsed"])
	return fields
}

func (cb *Couchbase) gatherDetailedBucketStats(server, bucket, nodeHostname string, fields map[string]interface{}) error {
	extendedBucketStats := &bucketStats{}
	err := cb.queryDetailedBucketStats(server, bucket, nodeHostname, extendedBucketStats)
	if err != nil {
		return err
	}

	cb.addBucketFieldChecked(fields, "couch_total_disk_size", extendedBucketStats.Op.Samples.CouchTotalDiskSize)
	cb.addBucketFieldChecked(fields, "couch_docs_fragmentation", extendedBucketStats.Op.Samples.CouchDocsFragmentation)
	cb.addBucketFieldChecked(fields, "couch_views_fragmentation", extendedBucketStats.Op.Samples.CouchViewsFragmentation)
	cb.addBucketFieldChecked(fields, "hit_ratio", extendedBucketStats.Op.Samples.HitRatio)
	cb.addBucketFieldChecked(fields, "ep_cache_miss_rate", extendedBucketStats.Op.Samples.EpCacheMissRate)
	cb.addBucketFieldChecked(fields, "ep_resident_items_rate", extendedBucketStats.Op.Samples.EpResidentItemsRate)
	cb.addBucketFieldChecked(fields, "vb_avg_active_queue_age", extendedBucketStats.Op.Samples.VbAvgActiveQueueAge)
	cb.addBucketFieldChecked(fields, "vb_avg_replica_queue_age", extendedBucketStats.Op.Samples.VbAvgReplicaQueueAge)
	cb.addBucketFieldChecked(fields, "vb_avg_pending_queue_age", extendedBucketStats.Op.Samples.VbAvgPendingQueueAge)
	cb.addBucketFieldChecked(fields, "vb_avg_total_queue_age", extendedBucketStats.Op.Samples.VbAvgTotalQueueAge)
	cb.addBucketFieldChecked(fields, "vb_active_resident_items_ratio", extendedBucketStats.Op.Samples.VbActiveResidentItemsRatio)
	cb.addBucketFieldChecked(fields, "vb_replica_resident_items_ratio", extendedBucketStats.Op.Samples.VbReplicaResidentItemsRatio)
	cb.addBucketFieldChecked(fields, "vb_pending_resident_items_ratio", extendedBucketStats.Op.Samples.VbPendingResidentItemsRatio)
	cb.addBucketFieldChecked(fields, "avg_disk_update_time", extendedBucketStats.Op.Samples.AvgDiskUpdateTime)
	cb.addBucketFieldChecked(fields, "avg_disk_commit_time", extendedBucketStats.Op.Samples.AvgDiskCommitTime)
	cb.addBucketFieldChecked(fields, "avg_bg_wait_time", extendedBucketStats.Op.Samples.AvgBgWaitTime)
	cb.addBucketFieldChecked(fields, "avg_active_timestamp_drift", extendedBucketStats.Op.Samples.AvgActiveTimestampDrift)
	cb.addBucketFieldChecked(fields, "avg_replica_timestamp_drift", extendedBucketStats.Op.Samples.AvgReplicaTimestampDrift)
	cb.addBucketFieldChecked(fields, "ep_dcp_views+indexes_count", extendedBucketStats.Op.Samples.EpDcpViewsIndexesCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_views+indexes_items_remaining", extendedBucketStats.Op.Samples.EpDcpViewsIndexesItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_views+indexes_producer_count", extendedBucketStats.Op.Samples.EpDcpViewsIndexesProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_views+indexes_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpViewsIndexesTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_views+indexes_items_sent", extendedBucketStats.Op.Samples.EpDcpViewsIndexesItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_views+indexes_total_bytes", extendedBucketStats.Op.Samples.EpDcpViewsIndexesTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_views+indexes_backoff", extendedBucketStats.Op.Samples.EpDcpViewsIndexesBackoff)
	cb.addBucketFieldChecked(fields, "bg_wait_count", extendedBucketStats.Op.Samples.BgWaitCount)
	cb.addBucketFieldChecked(fields, "bg_wait_total", extendedBucketStats.Op.Samples.BgWaitTotal)
	cb.addBucketFieldChecked(fields, "bytes_read", extendedBucketStats.Op.Samples.BytesRead)
	cb.addBucketFieldChecked(fields, "bytes_written", extendedBucketStats.Op.Samples.BytesWritten)
	cb.addBucketFieldChecked(fields, "cas_badval", extendedBucketStats.Op.Samples.CasBadval)
	cb.addBucketFieldChecked(fields, "cas_hits", extendedBucketStats.Op.Samples.CasHits)
	cb.addBucketFieldChecked(fields, "cas_misses", extendedBucketStats.Op.Samples.CasMisses)
	cb.addBucketFieldChecked(fields, "cmd_get", extendedBucketStats.Op.Samples.CmdGet)
	cb.addBucketFieldChecked(fields, "cmd_lookup", extendedBucketStats.Op.Samples.CmdLookup)
	cb.addBucketFieldChecked(fields, "cmd_set", extendedBucketStats.Op.Samples.CmdSet)
	cb.addBucketFieldChecked(fields, "couch_docs_actual_disk_size", extendedBucketStats.Op.Samples.CouchDocsActualDiskSize)
	cb.addBucketFieldChecked(fields, "couch_docs_data_size", extendedBucketStats.Op.Samples.CouchDocsDataSize)
	cb.addBucketFieldChecked(fields, "couch_docs_disk_size", extendedBucketStats.Op.Samples.CouchDocsDiskSize)
	cb.addBucketFieldChecked(fields, "couch_spatial_data_size", extendedBucketStats.Op.Samples.CouchSpatialDataSize)
	cb.addBucketFieldChecked(fields, "couch_spatial_disk_size", extendedBucketStats.Op.Samples.CouchSpatialDiskSize)
	cb.addBucketFieldChecked(fields, "couch_spatial_ops", extendedBucketStats.Op.Samples.CouchSpatialOps)
	cb.addBucketFieldChecked(fields, "couch_views_actual_disk_size", extendedBucketStats.Op.Samples.CouchViewsActualDiskSize)
	cb.addBucketFieldChecked(fields, "couch_views_data_size", extendedBucketStats.Op.Samples.CouchViewsDataSize)
	cb.addBucketFieldChecked(fields, "couch_views_disk_size", extendedBucketStats.Op.Samples.CouchViewsDiskSize)
	cb.addBucketFieldChecked(fields, "couch_views_ops", extendedBucketStats.Op.Samples.CouchViewsOps)
	cb.addBucketFieldChecked(fields, "curr_connections", extendedBucketStats.Op.Samples.CurrConnections)
	cb.addBucketFieldChecked(fields, "curr_items", extendedBucketStats.Op.Samples.CurrItems)
	cb.addBucketFieldChecked(fields, "curr_items_tot", extendedBucketStats.Op.Samples.CurrItemsTot)
	cb.addBucketFieldChecked(fields, "decr_hits", extendedBucketStats.Op.Samples.DecrHits)
	cb.addBucketFieldChecked(fields, "decr_misses", extendedBucketStats.Op.Samples.DecrMisses)
	cb.addBucketFieldChecked(fields, "delete_hits", extendedBucketStats.Op.Samples.DeleteHits)
	cb.addBucketFieldChecked(fields, "delete_misses", extendedBucketStats.Op.Samples.DeleteMisses)
	cb.addBucketFieldChecked(fields, "disk_commit_count", extendedBucketStats.Op.Samples.DiskCommitCount)
	cb.addBucketFieldChecked(fields, "disk_commit_total", extendedBucketStats.Op.Samples.DiskCommitTotal)
	cb.addBucketFieldChecked(fields, "disk_update_count", extendedBucketStats.Op.Samples.DiskUpdateCount)
	cb.addBucketFieldChecked(fields, "disk_update_total", extendedBucketStats.Op.Samples.DiskUpdateTotal)
	cb.addBucketFieldChecked(fields, "disk_write_queue", extendedBucketStats.Op.Samples.DiskWriteQueue)
	cb.addBucketFieldChecked(fields, "ep_active_ahead_exceptions", extendedBucketStats.Op.Samples.EpActiveAheadExceptions)
	cb.addBucketFieldChecked(fields, "ep_active_hlc_drift", extendedBucketStats.Op.Samples.EpActiveHlcDrift)
	cb.addBucketFieldChecked(fields, "ep_active_hlc_drift_count", extendedBucketStats.Op.Samples.EpActiveHlcDriftCount)
	cb.addBucketFieldChecked(fields, "ep_bg_fetched", extendedBucketStats.Op.Samples.EpBgFetched)
	cb.addBucketFieldChecked(fields, "ep_clock_cas_drift_threshold_exceeded", extendedBucketStats.Op.Samples.EpClockCasDriftThresholdExceeded)
	cb.addBucketFieldChecked(fields, "ep_data_read_failed", extendedBucketStats.Op.Samples.EpDataReadFailed)
	cb.addBucketFieldChecked(fields, "ep_data_write_failed", extendedBucketStats.Op.Samples.EpDataWriteFailed)
	cb.addBucketFieldChecked(fields, "ep_dcp_2i_backoff", extendedBucketStats.Op.Samples.EpDcp2IBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_2i_count", extendedBucketStats.Op.Samples.EpDcp2ICount)
	cb.addBucketFieldChecked(fields, "ep_dcp_2i_items_remaining", extendedBucketStats.Op.Samples.EpDcp2IItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_2i_items_sent", extendedBucketStats.Op.Samples.EpDcp2IItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_2i_producer_count", extendedBucketStats.Op.Samples.EpDcp2IProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_2i_total_backlog_size", extendedBucketStats.Op.Samples.EpDcp2ITotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_2i_total_bytes", extendedBucketStats.Op.Samples.EpDcp2ITotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_cbas_backoff", extendedBucketStats.Op.Samples.EpDcpCbasBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_cbas_count", extendedBucketStats.Op.Samples.EpDcpCbasCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_cbas_items_remaining", extendedBucketStats.Op.Samples.EpDcpCbasItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_cbas_items_sent", extendedBucketStats.Op.Samples.EpDcpCbasItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_cbas_producer_count", extendedBucketStats.Op.Samples.EpDcpCbasProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_cbas_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpCbasTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_cbas_total_bytes", extendedBucketStats.Op.Samples.EpDcpCbasTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_eventing_backoff", extendedBucketStats.Op.Samples.EpDcpEventingBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_eventing_count", extendedBucketStats.Op.Samples.EpDcpEventingCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_eventing_items_remaining", extendedBucketStats.Op.Samples.EpDcpEventingItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_eventing_items_sent", extendedBucketStats.Op.Samples.EpDcpEventingItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_eventing_producer_count", extendedBucketStats.Op.Samples.EpDcpEventingProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_eventing_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpEventingTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_eventing_total_bytes", extendedBucketStats.Op.Samples.EpDcpEventingTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_fts_backoff", extendedBucketStats.Op.Samples.EpDcpFtsBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_fts_count", extendedBucketStats.Op.Samples.EpDcpFtsCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_fts_items_remaining", extendedBucketStats.Op.Samples.EpDcpFtsItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_fts_items_sent", extendedBucketStats.Op.Samples.EpDcpFtsItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_fts_producer_count", extendedBucketStats.Op.Samples.EpDcpFtsProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_fts_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpFtsTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_fts_total_bytes", extendedBucketStats.Op.Samples.EpDcpFtsTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_other_backoff", extendedBucketStats.Op.Samples.EpDcpOtherBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_other_count", extendedBucketStats.Op.Samples.EpDcpOtherCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_other_items_remaining", extendedBucketStats.Op.Samples.EpDcpOtherItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_other_items_sent", extendedBucketStats.Op.Samples.EpDcpOtherItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_other_producer_count", extendedBucketStats.Op.Samples.EpDcpOtherProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_other_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpOtherTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_other_total_bytes", extendedBucketStats.Op.Samples.EpDcpOtherTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_replica_backoff", extendedBucketStats.Op.Samples.EpDcpReplicaBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_replica_count", extendedBucketStats.Op.Samples.EpDcpReplicaCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_replica_items_remaining", extendedBucketStats.Op.Samples.EpDcpReplicaItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_replica_items_sent", extendedBucketStats.Op.Samples.EpDcpReplicaItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_replica_producer_count", extendedBucketStats.Op.Samples.EpDcpReplicaProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_replica_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpReplicaTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_replica_total_bytes", extendedBucketStats.Op.Samples.EpDcpReplicaTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_views_backoff", extendedBucketStats.Op.Samples.EpDcpViewsBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_views_count", extendedBucketStats.Op.Samples.EpDcpViewsCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_views_items_remaining", extendedBucketStats.Op.Samples.EpDcpViewsItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_views_items_sent", extendedBucketStats.Op.Samples.EpDcpViewsItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_views_producer_count", extendedBucketStats.Op.Samples.EpDcpViewsProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_views_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpViewsTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_views_total_bytes", extendedBucketStats.Op.Samples.EpDcpViewsTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_dcp_xdcr_backoff", extendedBucketStats.Op.Samples.EpDcpXdcrBackoff)
	cb.addBucketFieldChecked(fields, "ep_dcp_xdcr_count", extendedBucketStats.Op.Samples.EpDcpXdcrCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_xdcr_items_remaining", extendedBucketStats.Op.Samples.EpDcpXdcrItemsRemaining)
	cb.addBucketFieldChecked(fields, "ep_dcp_xdcr_items_sent", extendedBucketStats.Op.Samples.EpDcpXdcrItemsSent)
	cb.addBucketFieldChecked(fields, "ep_dcp_xdcr_producer_count", extendedBucketStats.Op.Samples.EpDcpXdcrProducerCount)
	cb.addBucketFieldChecked(fields, "ep_dcp_xdcr_total_backlog_size", extendedBucketStats.Op.Samples.EpDcpXdcrTotalBacklogSize)
	cb.addBucketFieldChecked(fields, "ep_dcp_xdcr_total_bytes", extendedBucketStats.Op.Samples.EpDcpXdcrTotalBytes)
	cb.addBucketFieldChecked(fields, "ep_diskqueue_drain", extendedBucketStats.Op.Samples.EpDiskqueueDrain)
	cb.addBucketFieldChecked(fields, "ep_diskqueue_fill", extendedBucketStats.Op.Samples.EpDiskqueueFill)
	cb.addBucketFieldChecked(fields, "ep_diskqueue_items", extendedBucketStats.Op.Samples.EpDiskqueueItems)
	cb.addBucketFieldChecked(fields, "ep_flusher_todo", extendedBucketStats.Op.Samples.EpFlusherTodo)
	cb.addBucketFieldChecked(fields, "ep_item_commit_failed", extendedBucketStats.Op.Samples.EpItemCommitFailed)
	cb.addBucketFieldChecked(fields, "ep_kv_size", extendedBucketStats.Op.Samples.EpKvSize)
	cb.addBucketFieldChecked(fields, "ep_max_size", extendedBucketStats.Op.Samples.EpMaxSize)
	cb.addBucketFieldChecked(fields, "ep_mem_high_wat", extendedBucketStats.Op.Samples.EpMemHighWat)
	cb.addBucketFieldChecked(fields, "ep_mem_low_wat", extendedBucketStats.Op.Samples.EpMemLowWat)
	cb.addBucketFieldChecked(fields, "ep_meta_data_memory", extendedBucketStats.Op.Samples.EpMetaDataMemory)
	cb.addBucketFieldChecked(fields, "ep_num_non_resident", extendedBucketStats.Op.Samples.EpNumNonResident)
	cb.addBucketFieldChecked(fields, "ep_num_ops_del_meta", extendedBucketStats.Op.Samples.EpNumOpsDelMeta)
	cb.addBucketFieldChecked(fields, "ep_num_ops_del_ret_meta", extendedBucketStats.Op.Samples.EpNumOpsDelRetMeta)
	cb.addBucketFieldChecked(fields, "ep_num_ops_get_meta", extendedBucketStats.Op.Samples.EpNumOpsGetMeta)
	cb.addBucketFieldChecked(fields, "ep_num_ops_set_meta", extendedBucketStats.Op.Samples.EpNumOpsSetMeta)
	cb.addBucketFieldChecked(fields, "ep_num_ops_set_ret_meta", extendedBucketStats.Op.Samples.EpNumOpsSetRetMeta)
	cb.addBucketFieldChecked(fields, "ep_num_value_ejects", extendedBucketStats.Op.Samples.EpNumValueEjects)
	cb.addBucketFieldChecked(fields, "ep_oom_errors", extendedBucketStats.Op.Samples.EpOomErrors)
	cb.addBucketFieldChecked(fields, "ep_ops_create", extendedBucketStats.Op.Samples.EpOpsCreate)
	cb.addBucketFieldChecked(fields, "ep_ops_update", extendedBucketStats.Op.Samples.EpOpsUpdate)
	cb.addBucketFieldChecked(fields, "ep_overhead", extendedBucketStats.Op.Samples.EpOverhead)
	cb.addBucketFieldChecked(fields, "ep_queue_size", extendedBucketStats.Op.Samples.EpQueueSize)
	cb.addBucketFieldChecked(fields, "ep_replica_ahead_exceptions", extendedBucketStats.Op.Samples.EpReplicaAheadExceptions)
	cb.addBucketFieldChecked(fields, "ep_replica_hlc_drift", extendedBucketStats.Op.Samples.EpReplicaHlcDrift)
	cb.addBucketFieldChecked(fields, "ep_replica_hlc_drift_count", extendedBucketStats.Op.Samples.EpReplicaHlcDriftCount)
	cb.addBucketFieldChecked(fields, "ep_tmp_oom_errors", extendedBucketStats.Op.Samples.EpTmpOomErrors)
	cb.addBucketFieldChecked(fields, "ep_vb_total", extendedBucketStats.Op.Samples.EpVbTotal)
	cb.addBucketFieldChecked(fields, "evictions", extendedBucketStats.Op.Samples.Evictions)
	cb.addBucketFieldChecked(fields, "get_hits", extendedBucketStats.Op.Samples.GetHits)
	cb.addBucketFieldChecked(fields, "get_misses", extendedBucketStats.Op.Samples.GetMisses)
	cb.addBucketFieldChecked(fields, "incr_hits", extendedBucketStats.Op.Samples.IncrHits)
	cb.addBucketFieldChecked(fields, "incr_misses", extendedBucketStats.Op.Samples.IncrMisses)
	cb.addBucketFieldChecked(fields, "misses", extendedBucketStats.Op.Samples.Misses)
	cb.addBucketFieldChecked(fields, "ops", extendedBucketStats.Op.Samples.Ops)
	cb.addBucketFieldChecked(fields, "timestamp", extendedBucketStats.Op.Samples.Timestamp)
	cb.addBucketFieldChecked(fields, "vb_active_eject", extendedBucketStats.Op.Samples.VbActiveEject)
	cb.addBucketFieldChecked(fields, "vb_active_itm_memory", extendedBucketStats.Op.Samples.VbActiveItmMemory)
	cb.addBucketFieldChecked(fields, "vb_active_meta_data_memory", extendedBucketStats.Op.Samples.VbActiveMetaDataMemory)
	cb.addBucketFieldChecked(fields, "vb_active_num", extendedBucketStats.Op.Samples.VbActiveNum)
	cb.addBucketFieldChecked(fields, "vb_active_num_non_resident", extendedBucketStats.Op.Samples.VbActiveNumNonResident)
	cb.addBucketFieldChecked(fields, "vb_active_ops_create", extendedBucketStats.Op.Samples.VbActiveOpsCreate)
	cb.addBucketFieldChecked(fields, "vb_active_ops_update", extendedBucketStats.Op.Samples.VbActiveOpsUpdate)
	cb.addBucketFieldChecked(fields, "vb_active_queue_age", extendedBucketStats.Op.Samples.VbActiveQueueAge)
	cb.addBucketFieldChecked(fields, "vb_active_queue_drain", extendedBucketStats.Op.Samples.VbActiveQueueDrain)
	cb.addBucketFieldChecked(fields, "vb_active_queue_fill", extendedBucketStats.Op.Samples.VbActiveQueueFill)
	cb.addBucketFieldChecked(fields, "vb_active_queue_size", extendedBucketStats.Op.Samples.VbActiveQueueSize)
	cb.addBucketFieldChecked(fields, "vb_active_sync_write_aborted_count", extendedBucketStats.Op.Samples.VbActiveSyncWriteAbortedCount)
	cb.addBucketFieldChecked(fields, "vb_active_sync_write_accepted_count", extendedBucketStats.Op.Samples.VbActiveSyncWriteAcceptedCount)
	cb.addBucketFieldChecked(fields, "vb_active_sync_write_committed_count", extendedBucketStats.Op.Samples.VbActiveSyncWriteCommittedCount)
	cb.addBucketFieldChecked(fields, "vb_pending_curr_items", extendedBucketStats.Op.Samples.VbPendingCurrItems)
	cb.addBucketFieldChecked(fields, "vb_pending_eject", extendedBucketStats.Op.Samples.VbPendingEject)
	cb.addBucketFieldChecked(fields, "vb_pending_itm_memory", extendedBucketStats.Op.Samples.VbPendingItmMemory)
	cb.addBucketFieldChecked(fields, "vb_pending_meta_data_memory", extendedBucketStats.Op.Samples.VbPendingMetaDataMemory)
	cb.addBucketFieldChecked(fields, "vb_pending_num", extendedBucketStats.Op.Samples.VbPendingNum)
	cb.addBucketFieldChecked(fields, "vb_pending_num_non_resident", extendedBucketStats.Op.Samples.VbPendingNumNonResident)
	cb.addBucketFieldChecked(fields, "vb_pending_ops_create", extendedBucketStats.Op.Samples.VbPendingOpsCreate)
	cb.addBucketFieldChecked(fields, "vb_pending_ops_update", extendedBucketStats.Op.Samples.VbPendingOpsUpdate)
	cb.addBucketFieldChecked(fields, "vb_pending_queue_age", extendedBucketStats.Op.Samples.VbPendingQueueAge)
	cb.addBucketFieldChecked(fields, "vb_pending_queue_drain", extendedBucketStats.Op.Samples.VbPendingQueueDrain)
	cb.addBucketFieldChecked(fields, "vb_pending_queue_fill", extendedBucketStats.Op.Samples.VbPendingQueueFill)
	cb.addBucketFieldChecked(fields, "vb_pending_queue_size", extendedBucketStats.Op.Samples.VbPendingQueueSize)
	cb.addBucketFieldChecked(fields, "vb_replica_curr_items", extendedBucketStats.Op.Samples.VbReplicaCurrItems)
	cb.addBucketFieldChecked(fields, "vb_replica_eject", extendedBucketStats.Op.Samples.VbReplicaEject)
	cb.addBucketFieldChecked(fields, "vb_replica_itm_memory", extendedBucketStats.Op.Samples.VbReplicaItmMemory)
	cb.addBucketFieldChecked(fields, "vb_replica_meta_data_memory", extendedBucketStats.Op.Samples.VbReplicaMetaDataMemory)
	cb.addBucketFieldChecked(fields, "vb_replica_num", extendedBucketStats.Op.Samples.VbReplicaNum)
	cb.addBucketFieldChecked(fields, "vb_replica_num_non_resident", extendedBucketStats.Op.Samples.VbReplicaNumNonResident)
	cb.addBucketFieldChecked(fields, "vb_replica_ops_create", extendedBucketStats.Op.Samples.VbReplicaOpsCreate)
	cb.addBucketFieldChecked(fields, "vb_replica_ops_update", extendedBucketStats.Op.Samples.VbReplicaOpsUpdate)
	cb.addBucketFieldChecked(fields, "vb_replica_queue_age", extendedBucketStats.Op.Samples.VbReplicaQueueAge)
	cb.addBucketFieldChecked(fields, "vb_replica_queue_drain", extendedBucketStats.Op.Samples.VbReplicaQueueDrain)
	cb.addBucketFieldChecked(fields, "vb_replica_queue_fill", extendedBucketStats.Op.Samples.VbReplicaQueueFill)
	cb.addBucketFieldChecked(fields, "vb_replica_queue_size", extendedBucketStats.Op.Samples.VbReplicaQueueSize)
	cb.addBucketFieldChecked(fields, "vb_total_queue_age", extendedBucketStats.Op.Samples.VbTotalQueueAge)
	cb.addBucketFieldChecked(fields, "xdc_ops", extendedBucketStats.Op.Samples.XdcOps)
	cb.addBucketFieldChecked(fields, "allocstall", extendedBucketStats.Op.Samples.Allocstall)
	cb.addBucketFieldChecked(fields, "cpu_cores_available", extendedBucketStats.Op.Samples.CPUCoresAvailable)
	cb.addBucketFieldChecked(fields, "cpu_irq_rate", extendedBucketStats.Op.Samples.CPUIrqRate)
	cb.addBucketFieldChecked(fields, "cpu_stolen_rate", extendedBucketStats.Op.Samples.CPUStolenRate)
	cb.addBucketFieldChecked(fields, "cpu_sys_rate", extendedBucketStats.Op.Samples.CPUSysRate)
	cb.addBucketFieldChecked(fields, "cpu_user_rate", extendedBucketStats.Op.Samples.CPUUserRate)
	cb.addBucketFieldChecked(fields, "cpu_utilization_rate", extendedBucketStats.Op.Samples.CPUUtilizationRate)
	cb.addBucketFieldChecked(fields, "hibernated_requests", extendedBucketStats.Op.Samples.HibernatedRequests)
	cb.addBucketFieldChecked(fields, "hibernated_waked", extendedBucketStats.Op.Samples.HibernatedWaked)
	cb.addBucketFieldChecked(fields, "mem_actual_free", extendedBucketStats.Op.Samples.MemActualFree)
	cb.addBucketFieldChecked(fields, "mem_actual_used", extendedBucketStats.Op.Samples.MemActualUsed)
	cb.addBucketFieldChecked(fields, "mem_free", extendedBucketStats.Op.Samples.MemFree)
	cb.addBucketFieldChecked(fields, "mem_limit", extendedBucketStats.Op.Samples.MemLimit)
	cb.addBucketFieldChecked(fields, "mem_total", extendedBucketStats.Op.Samples.MemTotal)
	cb.addBucketFieldChecked(fields, "mem_used_sys", extendedBucketStats.Op.Samples.MemUsedSys)
	cb.addBucketFieldChecked(fields, "odp_report_failed", extendedBucketStats.Op.Samples.OdpReportFailed)
	cb.addBucketFieldChecked(fields, "rest_requests", extendedBucketStats.Op.Samples.RestRequests)
	cb.addBucketFieldChecked(fields, "swap_total", extendedBucketStats.Op.Samples.SwapTotal)
	cb.addBucketFieldChecked(fields, "swap_used", extendedBucketStats.Op.Samples.SwapUsed)

	return nil
}

func (cb *Couchbase) addBucketField(fields map[string]interface{}, fieldKey string, value interface{}) {
	if !cb.bucketInclude.Match(fieldKey) {
		return
	}

	fields[fieldKey] = value
}

func (cb *Couchbase) addBucketFieldChecked(fields map[string]interface{}, fieldKey string, values []float64) {
	if values == nil {
		return
	}

	cb.addBucketField(fields, fieldKey, values[len(values)-1])
}

func (cb *Couchbase) queryDetailedBucketStats(server, bucket, nodeHostname string, bucketStats *bucketStats) error {
	url := server + "/pools/default/buckets/" + bucket
	if nodeHostname != "" {
		url += "/nodes/" + nodeHostname
	}
	url += "/stats?"

	// Set up an HTTP request to get the complete set of bucket stats.
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	r, err := cb.client.Do(req)
	if err != nil {
		return err
	}

	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(bucketStats)
}

func init() {
	inputs.Add("couchbase", func() telegraf.Input {
		return &Couchbase{
			BucketStatsIncluded: []string{"quota_percent_used", "ops_per_sec", "disk_fetches", "item_count", "disk_used", "data_used", "mem_used"},
			ClusterBucketStats:  true,
		}
	})
}
