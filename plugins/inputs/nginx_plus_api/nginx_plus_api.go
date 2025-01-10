//go:generate ../../../tools/readme_config_includer/generator
package nginx_plus_api

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	// Default settings
	defaultAPIVersion = 3

	// Paths
	processesPath   = "processes"
	connectionsPath = "connections"
	slabsPath       = "slabs"
	sslPath         = "ssl"

	httpRequestsPath      = "http/requests"
	httpServerZonesPath   = "http/server_zones"
	httpLocationZonesPath = "http/location_zones"
	httpUpstreamsPath     = "http/upstreams"
	httpCachesPath        = "http/caches"
	httpLimitReqsPath     = "http/limit_reqs"
	resolverZonesPath     = "resolvers"

	streamServerZonesPath = "stream/server_zones"
	streamUpstreamsPath   = "stream/upstreams"
)

type NginxPlusAPI struct {
	Urls            []string        `toml:"urls"`
	APIVersion      int64           `toml:"api_version"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	tls.ClientConfig

	client *http.Client
}

func (*NginxPlusAPI) SampleConfig() string {
	return sampleConfig
}

func (n *NginxPlusAPI) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	// Create an HTTP client that is re-used for each
	// collection interval

	if n.APIVersion == 0 {
		n.APIVersion = defaultAPIVersion
	}

	if n.client == nil {
		client, err := n.createHTTPClient()
		if err != nil {
			return err
		}
		n.client = client
	}

	for _, u := range n.Urls {
		addr, err := url.Parse(u)
		if err != nil {
			acc.AddError(fmt.Errorf("unable to parse address %q: %w", u, err))
			continue
		}

		wg.Add(1)
		go func(addr *url.URL) {
			defer wg.Done()
			n.gatherMetrics(addr, acc)
		}(addr)
	}

	wg.Wait()
	return nil
}

func (n *NginxPlusAPI) createHTTPClient() (*http.Client, error) {
	if n.ResponseTimeout < config.Duration(time.Second) {
		n.ResponseTimeout = config.Duration(time.Second * 5)
	}

	tlsConfig, err := n.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: time.Duration(n.ResponseTimeout),
	}

	return client, nil
}

func init() {
	inputs.Add("nginx_plus_api", func() telegraf.Input {
		return &NginxPlusAPI{}
	})
}
