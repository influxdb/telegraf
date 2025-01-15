//go:generate ../../../tools/readme_config_includer/generator
package riak

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Riak struct {
	// Servers is a slice of servers as http addresses (ex. http://127.0.0.1:8098)
	Servers []string `toml:"servers"`

	client *http.Client
}

// Type riakStats represents the data received from Riak
type riakStats struct {
	CPUAvg1                  int64  `json:"cpu_avg1"`
	CPUAvg15                 int64  `json:"cpu_avg15"`
	CPUAvg5                  int64  `json:"cpu_avg5"`
	MemoryCode               int64  `json:"memory_code"`
	MemoryEts                int64  `json:"memory_ets"`
	MemoryProcesses          int64  `json:"memory_processes"`
	MemorySystem             int64  `json:"memory_system"`
	MemoryTotal              int64  `json:"memory_total"`
	NodeGetFsmObjsize100     int64  `json:"node_get_fsm_objsize_100"`
	NodeGetFsmObjsize95      int64  `json:"node_get_fsm_objsize_95"`
	NodeGetFsmObjsize99      int64  `json:"node_get_fsm_objsize_99"`
	NodeGetFsmObjsizeMean    int64  `json:"node_get_fsm_objsize_mean"`
	NodeGetFsmObjsizeMedian  int64  `json:"node_get_fsm_objsize_median"`
	NodeGetFsmSiblings100    int64  `json:"node_get_fsm_siblings_100"`
	NodeGetFsmSiblings95     int64  `json:"node_get_fsm_siblings_95"`
	NodeGetFsmSiblings99     int64  `json:"node_get_fsm_siblings_99"`
	NodeGetFsmSiblingsMean   int64  `json:"node_get_fsm_siblings_mean"`
	NodeGetFsmSiblingsMedian int64  `json:"node_get_fsm_siblings_median"`
	NodeGetFsmTime100        int64  `json:"node_get_fsm_time_100"`
	NodeGetFsmTime95         int64  `json:"node_get_fsm_time_95"`
	NodeGetFsmTime99         int64  `json:"node_get_fsm_time_99"`
	NodeGetFsmTimeMean       int64  `json:"node_get_fsm_time_mean"`
	NodeGetFsmTimeMedian     int64  `json:"node_get_fsm_time_median"`
	NodeGets                 int64  `json:"node_gets"`
	NodeGetsTotal            int64  `json:"node_gets_total"`
	Nodename                 string `json:"nodename"`
	NodePutFsmTime100        int64  `json:"node_put_fsm_time_100"`
	NodePutFsmTime95         int64  `json:"node_put_fsm_time_95"`
	NodePutFsmTime99         int64  `json:"node_put_fsm_time_99"`
	NodePutFsmTimeMean       int64  `json:"node_put_fsm_time_mean"`
	NodePutFsmTimeMedian     int64  `json:"node_put_fsm_time_median"`
	NodePuts                 int64  `json:"node_puts"`
	NodePutsTotal            int64  `json:"node_puts_total"`
	PbcActive                int64  `json:"pbc_active"`
	PbcConnects              int64  `json:"pbc_connects"`
	PbcConnectsTotal         int64  `json:"pbc_connects_total"`
	VnodeGets                int64  `json:"vnode_gets"`
	VnodeGetsTotal           int64  `json:"vnode_gets_total"`
	VnodeIndexReads          int64  `json:"vnode_index_reads"`
	VnodeIndexReadsTotal     int64  `json:"vnode_index_reads_total"`
	VnodeIndexWrites         int64  `json:"vnode_index_writes"`
	VnodeIndexWritesTotal    int64  `json:"vnode_index_writes_total"`
	VnodePuts                int64  `json:"vnode_puts"`
	VnodePutsTotal           int64  `json:"vnode_puts_total"`
	ReadRepairs              int64  `json:"read_repairs"`
	ReadRepairsTotal         int64  `json:"read_repairs_total"`
}

func (*Riak) SampleConfig() string {
	return sampleConfig
}

func (r *Riak) Gather(acc telegraf.Accumulator) error {
	// Default to a single server at localhost (default port) if none specified
	if len(r.Servers) == 0 {
		r.Servers = []string{"http://127.0.0.1:8098"}
	}

	// Range over all servers, gathering stats. Returns early in case of any error.
	for _, s := range r.Servers {
		acc.AddError(r.gatherServer(s, acc))
	}

	return nil
}

func (r *Riak) gatherServer(s string, acc telegraf.Accumulator) error {
	// Parse the given URL to extract the server tag
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("riak unable to parse given server URL %q: %w", s, err)
	}

	// Perform the GET request to the riak /stats endpoint
	resp, err := r.client.Get(s + "/stats")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Successful responses will always return status code 200
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("riak responded with unexpected status code %d", resp.StatusCode)
	}

	// Decode the response JSON into a new stats struct
	stats := &riakStats{}
	if err := json.NewDecoder(resp.Body).Decode(stats); err != nil {
		return fmt.Errorf("unable to decode riak response: %w", err)
	}

	// Build a map of tags
	tags := map[string]string{
		"nodename": stats.Nodename,
		"server":   u.Host,
	}

	// Build a map of field values
	fields := map[string]interface{}{
		"cpu_avg1":                     stats.CPUAvg1,
		"cpu_avg15":                    stats.CPUAvg15,
		"cpu_avg5":                     stats.CPUAvg5,
		"memory_code":                  stats.MemoryCode,
		"memory_ets":                   stats.MemoryEts,
		"memory_processes":             stats.MemoryProcesses,
		"memory_system":                stats.MemorySystem,
		"memory_total":                 stats.MemoryTotal,
		"node_get_fsm_objsize_100":     stats.NodeGetFsmObjsize100,
		"node_get_fsm_objsize_95":      stats.NodeGetFsmObjsize95,
		"node_get_fsm_objsize_99":      stats.NodeGetFsmObjsize99,
		"node_get_fsm_objsize_mean":    stats.NodeGetFsmObjsizeMean,
		"node_get_fsm_objsize_median":  stats.NodeGetFsmObjsizeMedian,
		"node_get_fsm_siblings_100":    stats.NodeGetFsmSiblings100,
		"node_get_fsm_siblings_95":     stats.NodeGetFsmSiblings95,
		"node_get_fsm_siblings_99":     stats.NodeGetFsmSiblings99,
		"node_get_fsm_siblings_mean":   stats.NodeGetFsmSiblingsMean,
		"node_get_fsm_siblings_median": stats.NodeGetFsmSiblingsMedian,
		"node_get_fsm_time_100":        stats.NodeGetFsmTime100,
		"node_get_fsm_time_95":         stats.NodeGetFsmTime95,
		"node_get_fsm_time_99":         stats.NodeGetFsmTime99,
		"node_get_fsm_time_mean":       stats.NodeGetFsmTimeMean,
		"node_get_fsm_time_median":     stats.NodeGetFsmTimeMedian,
		"node_gets":                    stats.NodeGets,
		"node_gets_total":              stats.NodeGetsTotal,
		"node_put_fsm_time_100":        stats.NodePutFsmTime100,
		"node_put_fsm_time_95":         stats.NodePutFsmTime95,
		"node_put_fsm_time_99":         stats.NodePutFsmTime99,
		"node_put_fsm_time_mean":       stats.NodePutFsmTimeMean,
		"node_put_fsm_time_median":     stats.NodePutFsmTimeMedian,
		"node_puts":                    stats.NodePuts,
		"node_puts_total":              stats.NodePutsTotal,
		"pbc_active":                   stats.PbcActive,
		"pbc_connects":                 stats.PbcConnects,
		"pbc_connects_total":           stats.PbcConnectsTotal,
		"vnode_gets":                   stats.VnodeGets,
		"vnode_gets_total":             stats.VnodeGetsTotal,
		"vnode_index_reads":            stats.VnodeIndexReads,
		"vnode_index_reads_total":      stats.VnodeIndexReadsTotal,
		"vnode_index_writes":           stats.VnodeIndexWrites,
		"vnode_index_writes_total":     stats.VnodeIndexWritesTotal,
		"vnode_puts":                   stats.VnodePuts,
		"vnode_puts_total":             stats.VnodePutsTotal,
		"read_repairs":                 stats.ReadRepairs,
		"read_repairs_total":           stats.ReadRepairsTotal,
	}

	// Accumulate the tags and values
	acc.AddFields("riak", fields, tags)

	return nil
}

// newRiak return a new instance of Riak with a default http client
func newRiak() *Riak {
	tr := &http.Transport{ResponseHeaderTimeout: 3 * time.Second}
	client := &http.Client{
		Transport: tr,
		Timeout:   4 * time.Second,
	}
	return &Riak{client: client}
}

func init() {
	inputs.Add("riak", func() telegraf.Input {
		return newRiak()
	})
}
