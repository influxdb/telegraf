//go:generate ../../../tools/readme_config_includer/generator
//go:build !freebsd || (freebsd && cgo)

package nats

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	gnatsd "github.com/nats-io/nats-server/v2/server"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Nats struct {
	Server          string          `toml:"server"`
	ResponseTimeout config.Duration `toml:"response_timeout"`

	client *http.Client
}

func (*Nats) SampleConfig() string {
	return sampleConfig
}

func (n *Nats) Gather(acc telegraf.Accumulator) error {
	address, err := url.Parse(n.Server)
	if err != nil {
		return err
	}
	address.Path = path.Join(address.Path, "varz")

	if n.client == nil {
		n.client = n.createHTTPClient()
	}
	resp, err := n.client.Get(address.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	stats := new(gnatsd.Varz)
	err = json.Unmarshal(bytes, &stats)
	if err != nil {
		return err
	}

	acc.AddFields("nats",
		map[string]interface{}{
			"in_msgs":           stats.InMsgs,
			"out_msgs":          stats.OutMsgs,
			"in_bytes":          stats.InBytes,
			"out_bytes":         stats.OutBytes,
			"uptime":            stats.Now.Sub(stats.Start).Nanoseconds(),
			"cores":             stats.Cores,
			"cpu":               stats.CPU,
			"mem":               stats.Mem,
			"connections":       stats.Connections,
			"total_connections": stats.TotalConnections,
			"subscriptions":     stats.Subscriptions,
			"slow_consumers":    stats.SlowConsumers,
			"routes":            stats.Routes,
			"remotes":           stats.Remotes,
		},
		map[string]string{"server": n.Server},
		time.Now())

	return nil
}

func (n *Nats) createHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	timeout := time.Duration(n.ResponseTimeout)
	if timeout == time.Duration(0) {
		timeout = 5 * time.Second
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

func init() {
	inputs.Add("nats", func() telegraf.Input {
		return &Nats{
			Server: "http://localhost:8222",
		}
	})
}
