package couchbase

import (
	couchbase "github.com/couchbase/go-couchbase"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"sync"
)

type Couchbase struct {
	Servers []string
}

var sampleConfig = `
  ## specify servers via a url matching:
  ##  [protocol://][:password]@address[:port]
  ##  e.g.
  ##    http://couchbase-0.example.com/
  ##    http://admin:secret@couchbase-0.example.com:8091/
  ##
  ## If no servers are specified, then localhost is used as the host.
  ## If no protocol is specifed, HTTP is used.
  ## If no port is specified, 8091 is used.
  servers = ["http://localhost:8091"]
`

func (r *Couchbase) SampleConfig() string {
	return sampleConfig
}

func (r *Couchbase) Description() string {
	return "Read metrics from one or many couchbase clusters"
}

// Reads stats from all configured clusters. Accumulates stats.
// Returns one of the errors encountered while gathering stats (if any).
func (r *Couchbase) Gather(acc telegraf.Accumulator) error {
	if len(r.Servers) == 0 {
		r.gatherServer("http://localhost:8091/", acc)
		return nil
	}

	var wg sync.WaitGroup

	var outerr error

	for _, serv := range r.Servers {
		wg.Add(1)
		go func(serv string) {
			defer wg.Done()
			outerr = r.gatherServer(serv, acc)
		}(serv)
	}

	wg.Wait()

	return outerr
}

func (r *Couchbase) gatherServer(addr string, acc telegraf.Accumulator) error {
	client, err := couchbase.Connect(addr)
	if err != nil {
		return err
	}
	pool, err := client.GetPool("default")
	if err != nil {
		return err
	}
	for i := 0; i < len(pool.Nodes); i++ {
		node := pool.Nodes[i]
		tags := map[string]string{"cluster": addr, "hostname": node.Hostname}
		fields := make(map[string]interface{})
		fields["memory_free"] = node.MemoryFree
		fields["memory_total"] = node.MemoryTotal
		acc.AddFields("couchbase_node", fields, tags)
	}
	for bucketName, _ := range pool.BucketMap {
		bucket := pool.BucketMap[bucketName]
		tags := map[string]string{"cluster": addr, "bucket": bucketName}
		acc.AddFields("couchbase_bucket", bucket.BasicStats, tags)
	}
	return nil
}

func init() {
	inputs.Add("couchbase", func() telegraf.Input {
		return &Couchbase{}
	})
}
