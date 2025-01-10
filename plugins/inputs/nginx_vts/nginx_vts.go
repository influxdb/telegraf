//go:generate ../../../tools/readme_config_includer/generator
package nginx_vts

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type NginxVTS struct {
	Urls            []string        `toml:"urls"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	tls.ClientConfig

	client *http.Client
}

func (*NginxVTS) SampleConfig() string {
	return sampleConfig
}

func (n *NginxVTS) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	// Create an HTTP client that is re-used for each
	// collection interval

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
			acc.AddError(n.gatherURL(addr, acc))
		}(addr)
	}

	wg.Wait()
	return nil
}

func (n *NginxVTS) createHTTPClient() (*http.Client, error) {
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

func (n *NginxVTS) gatherURL(addr *url.URL, acc telegraf.Accumulator) error {
	resp, err := n.client.Get(addr.String())
	if err != nil {
		return fmt.Errorf("error making HTTP request to %q: %w", addr.String(), err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP status %s", addr.String(), resp.Status)
	}
	contentType := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
	switch contentType {
	case "application/json":
		return gatherStatusURL(bufio.NewReader(resp.Body), getTags(addr), acc)
	default:
		return fmt.Errorf("%s returned unexpected content type %s", addr.String(), contentType)
	}
}

type nginxVTSResponse struct {
	Connections struct {
		Active   uint64 `json:"active"`
		Reading  uint64 `json:"reading"`
		Writing  uint64 `json:"writing"`
		Waiting  uint64 `json:"waiting"`
		Accepted uint64 `json:"accepted"`
		Handled  uint64 `json:"handled"`
		Requests uint64 `json:"requests"`
	} `json:"connections"`
	ServerZones   map[string]server            `json:"serverZones"`
	FilterZones   map[string]map[string]server `json:"filterZones"`
	UpstreamZones map[string][]upstream        `json:"upstreamZones"`
	CacheZones    map[string]cache             `json:"cacheZones"`
}

type server struct {
	RequestCounter uint64 `json:"requestCounter"`
	InBytes        uint64 `json:"inBytes"`
	OutBytes       uint64 `json:"outBytes"`
	RequestMsec    uint64 `json:"requestMsec"`
	Responses      struct {
		OneXx       uint64 `json:"1xx"`
		TwoXx       uint64 `json:"2xx"`
		ThreeXx     uint64 `json:"3xx"`
		FourXx      uint64 `json:"4xx"`
		FiveXx      uint64 `json:"5xx"`
		Miss        uint64 `json:"miss"`
		Bypass      uint64 `json:"bypass"`
		Expired     uint64 `json:"expired"`
		Stale       uint64 `json:"stale"`
		Updating    uint64 `json:"updating"`
		Revalidated uint64 `json:"revalidated"`
		Hit         uint64 `json:"hit"`
		Scarce      uint64 `json:"scarce"`
	} `json:"responses"`
}

type upstream struct {
	Server         string `json:"server"`
	RequestCounter uint64 `json:"requestCounter"`
	InBytes        uint64 `json:"inBytes"`
	OutBytes       uint64 `json:"outBytes"`
	Responses      struct {
		OneXx   uint64 `json:"1xx"`
		TwoXx   uint64 `json:"2xx"`
		ThreeXx uint64 `json:"3xx"`
		FourXx  uint64 `json:"4xx"`
		FiveXx  uint64 `json:"5xx"`
	} `json:"responses"`
	ResponseMsec uint64 `json:"responseMsec"`
	RequestMsec  uint64 `json:"requestMsec"`
	Weight       uint64 `json:"weight"`
	MaxFails     uint64 `json:"maxFails"`
	FailTimeout  uint64 `json:"failTimeout"`
	Backup       bool   `json:"backup"`
	Down         bool   `json:"down"`
}

type cache struct {
	MaxSize   uint64 `json:"maxSize"`
	UsedSize  uint64 `json:"usedSize"`
	InBytes   uint64 `json:"inBytes"`
	OutBytes  uint64 `json:"outBytes"`
	Responses struct {
		Miss        uint64 `json:"miss"`
		Bypass      uint64 `json:"bypass"`
		Expired     uint64 `json:"expired"`
		Stale       uint64 `json:"stale"`
		Updating    uint64 `json:"updating"`
		Revalidated uint64 `json:"revalidated"`
		Hit         uint64 `json:"hit"`
		Scarce      uint64 `json:"scarce"`
	} `json:"responses"`
}

func gatherStatusURL(r *bufio.Reader, tags map[string]string, acc telegraf.Accumulator) error {
	dec := json.NewDecoder(r)
	status := &nginxVTSResponse{}
	if err := dec.Decode(status); err != nil {
		return errors.New("error while decoding JSON response")
	}

	acc.AddFields("nginx_vts_connections", map[string]interface{}{
		"active":   status.Connections.Active,
		"reading":  status.Connections.Reading,
		"writing":  status.Connections.Writing,
		"waiting":  status.Connections.Waiting,
		"accepted": status.Connections.Accepted,
		"handled":  status.Connections.Handled,
		"requests": status.Connections.Requests,
	}, tags)

	for zoneName, zone := range status.ServerZones {
		zoneTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			zoneTags[k] = v
		}
		zoneTags["zone"] = zoneName

		acc.AddFields("nginx_vts_server", map[string]interface{}{
			"requests":     zone.RequestCounter,
			"request_time": zone.RequestMsec,
			"in_bytes":     zone.InBytes,
			"out_bytes":    zone.OutBytes,

			"response_1xx_count": zone.Responses.OneXx,
			"response_2xx_count": zone.Responses.TwoXx,
			"response_3xx_count": zone.Responses.ThreeXx,
			"response_4xx_count": zone.Responses.FourXx,
			"response_5xx_count": zone.Responses.FiveXx,

			"cache_miss":        zone.Responses.Miss,
			"cache_bypass":      zone.Responses.Bypass,
			"cache_expired":     zone.Responses.Expired,
			"cache_stale":       zone.Responses.Stale,
			"cache_updating":    zone.Responses.Updating,
			"cache_revalidated": zone.Responses.Revalidated,
			"cache_hit":         zone.Responses.Hit,
			"cache_scarce":      zone.Responses.Scarce,
		}, zoneTags)
	}

	for filterName, filters := range status.FilterZones {
		for filterKey, upstream := range filters {
			filterTags := make(map[string]string, len(tags)+2)
			for k, v := range tags {
				filterTags[k] = v
			}
			filterTags["filter_key"] = filterKey
			filterTags["filter_name"] = filterName

			acc.AddFields("nginx_vts_filter", map[string]interface{}{
				"requests":     upstream.RequestCounter,
				"request_time": upstream.RequestMsec,
				"in_bytes":     upstream.InBytes,
				"out_bytes":    upstream.OutBytes,

				"response_1xx_count": upstream.Responses.OneXx,
				"response_2xx_count": upstream.Responses.TwoXx,
				"response_3xx_count": upstream.Responses.ThreeXx,
				"response_4xx_count": upstream.Responses.FourXx,
				"response_5xx_count": upstream.Responses.FiveXx,

				"cache_miss":        upstream.Responses.Miss,
				"cache_bypass":      upstream.Responses.Bypass,
				"cache_expired":     upstream.Responses.Expired,
				"cache_stale":       upstream.Responses.Stale,
				"cache_updating":    upstream.Responses.Updating,
				"cache_revalidated": upstream.Responses.Revalidated,
				"cache_hit":         upstream.Responses.Hit,
				"cache_scarce":      upstream.Responses.Scarce,
			}, filterTags)
		}
	}

	for upstreamName, upstreams := range status.UpstreamZones {
		for _, upstream := range upstreams {
			upstreamServerTags := make(map[string]string, len(tags)+2)
			for k, v := range tags {
				upstreamServerTags[k] = v
			}
			upstreamServerTags["upstream"] = upstreamName
			upstreamServerTags["upstream_address"] = upstream.Server
			acc.AddFields("nginx_vts_upstream", map[string]interface{}{
				"requests":      upstream.RequestCounter,
				"request_time":  upstream.RequestMsec,
				"response_time": upstream.ResponseMsec,
				"in_bytes":      upstream.InBytes,
				"out_bytes":     upstream.OutBytes,

				"response_1xx_count": upstream.Responses.OneXx,
				"response_2xx_count": upstream.Responses.TwoXx,
				"response_3xx_count": upstream.Responses.ThreeXx,
				"response_4xx_count": upstream.Responses.FourXx,
				"response_5xx_count": upstream.Responses.FiveXx,

				"weight":       upstream.Weight,
				"max_fails":    upstream.MaxFails,
				"fail_timeout": upstream.FailTimeout,
				"backup":       upstream.Backup,
				"down":         upstream.Down,
			}, upstreamServerTags)
		}
	}

	for zoneName, zone := range status.CacheZones {
		zoneTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			zoneTags[k] = v
		}
		zoneTags["zone"] = zoneName

		acc.AddFields("nginx_vts_cache", map[string]interface{}{
			"max_bytes":  zone.MaxSize,
			"used_bytes": zone.UsedSize,
			"in_bytes":   zone.InBytes,
			"out_bytes":  zone.OutBytes,

			"miss":        zone.Responses.Miss,
			"bypass":      zone.Responses.Bypass,
			"expired":     zone.Responses.Expired,
			"stale":       zone.Responses.Stale,
			"updating":    zone.Responses.Updating,
			"revalidated": zone.Responses.Revalidated,
			"hit":         zone.Responses.Hit,
			"scarce":      zone.Responses.Scarce,
		}, zoneTags)
	}

	return nil
}

// Get tag(s) for the nginx plugin
func getTags(addr *url.URL) map[string]string {
	h := addr.Host
	host, port, err := net.SplitHostPort(h)
	if err != nil {
		host = addr.Host
		if addr.Scheme == "http" {
			port = "80"
		} else if addr.Scheme == "https" {
			port = "443"
		} else {
			port = ""
		}
	}
	return map[string]string{"source": host, "port": port}
}

func init() {
	inputs.Add("nginx_vts", func() telegraf.Input {
		return &NginxVTS{}
	})
}
