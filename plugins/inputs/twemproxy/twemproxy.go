//go:generate ../../../tools/readme_config_includer/generator
package twemproxy

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Twemproxy struct {
	Addr  string
	Pools []string
}

func (*Twemproxy) SampleConfig() string {
	return sampleConfig
}

// Gather data from all Twemproxy instances
func (t *Twemproxy) Gather(acc telegraf.Accumulator) error {
	conn, err := net.DialTimeout("tcp", t.Addr, 1*time.Second)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(conn)
	if err != nil {
		return err
	}

	var stats map[string]interface{}
	if err = json.Unmarshal(body, &stats); err != nil {
		return errors.New("error decoding JSON response")
	}

	tags := make(map[string]string)
	tags["twemproxy"] = t.Addr
	t.processStat(acc, tags, stats)

	return nil
}

// Process Twemproxy server stats
func (t *Twemproxy) processStat(
	acc telegraf.Accumulator,
	tags map[string]string,
	data map[string]interface{},
) {
	if source, ok := data["source"]; ok {
		if val, ok := source.(string); ok {
			tags["source"] = val
		}
	}

	fields := make(map[string]interface{})
	metrics := []string{"total_connections", "curr_connections", "timestamp"}
	for _, m := range metrics {
		if value, ok := data[m]; ok {
			if val, ok := value.(float64); ok {
				fields[m] = val
			}
		}
	}
	acc.AddFields("twemproxy", fields, tags)

	for _, pool := range t.Pools {
		if poolStat, ok := data[pool]; ok {
			if data, ok := poolStat.(map[string]interface{}); ok {
				poolTags := copyTags(tags)
				poolTags["pool"] = pool
				processPool(acc, poolTags, data)
			}
		}
	}
}

// Process pool data in Twemproxy stats
func processPool(acc telegraf.Accumulator, tags map[string]string, data map[string]interface{}) {
	serverTags := make(map[string]map[string]string)

	fields := make(map[string]interface{})
	for key, value := range data {
		switch key {
		case "client_connections", "forward_error", "client_err", "server_ejects", "fragments", "client_eof":
			if val, ok := value.(float64); ok {
				fields[key] = val
			}
		default:
			if data, ok := value.(map[string]interface{}); ok {
				if _, ok := serverTags[key]; !ok {
					serverTags[key] = copyTags(tags)
					serverTags[key]["server"] = key
				}
				processServer(acc, serverTags[key], data)
			}
		}
	}
	acc.AddFields("twemproxy_pool", fields, tags)
}

// Process backend server(redis/memcached) stats
func processServer(acc telegraf.Accumulator, tags map[string]string, data map[string]interface{}) {
	fields := make(map[string]interface{})
	for key, value := range data {
		if val, ok := value.(float64); ok {
			fields[key] = val
		}
	}
	acc.AddFields("twemproxy_pool_server", fields, tags)
}

// Tags is not expected to be mutated after passing to Add.
func copyTags(tags map[string]string) map[string]string {
	newTags := make(map[string]string)
	for k, v := range tags {
		newTags[k] = v
	}
	return newTags
}

func init() {
	inputs.Add("twemproxy", func() telegraf.Input {
		return &Twemproxy{}
	})
}
