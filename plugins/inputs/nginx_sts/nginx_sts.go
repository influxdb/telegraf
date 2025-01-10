//go:generate ../../../tools/readme_config_includer/generator
package nginx_sts

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

type NginxSTS struct {
	Urls            []string        `toml:"urls"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	tls.ClientConfig

	client *http.Client
}

func (*NginxSTS) SampleConfig() string {
	return sampleConfig
}

func (n *NginxSTS) Gather(acc telegraf.Accumulator) error {
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

func (n *NginxSTS) createHTTPClient() (*http.Client, error) {
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

func (n *NginxSTS) gatherURL(addr *url.URL, acc telegraf.Accumulator) error {
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

type nginxSTSResponse struct {
	Connections struct {
		Active   uint64 `json:"active"`
		Reading  uint64 `json:"reading"`
		Writing  uint64 `json:"writing"`
		Waiting  uint64 `json:"waiting"`
		Accepted uint64 `json:"accepted"`
		Handled  uint64 `json:"handled"`
		Requests uint64 `json:"requests"`
	} `json:"connections"`
	Hostname            string                       `json:"hostName"`
	StreamFilterZones   map[string]map[string]server `json:"streamFilterZones"`
	StreamServerZones   map[string]server            `json:"streamServerZones"`
	StreamUpstreamZones map[string][]upstream        `json:"streamUpstreamZones"`
}

type server struct {
	ConnectCounter     uint64 `json:"connectCounter"`
	InBytes            uint64 `json:"inBytes"`
	OutBytes           uint64 `json:"outBytes"`
	SessionMsecCounter uint64 `json:"sessionMsecCounter"`
	SessionMsec        uint64 `json:"sessionMsec"`
	Responses          struct {
		OneXx   uint64 `json:"1xx"`
		TwoXx   uint64 `json:"2xx"`
		ThreeXx uint64 `json:"3xx"`
		FourXx  uint64 `json:"4xx"`
		FiveXx  uint64 `json:"5xx"`
	} `json:"responses"`
}

type upstream struct {
	Server         string `json:"server"`
	ConnectCounter uint64 `json:"connectCounter"`
	InBytes        uint64 `json:"inBytes"`
	OutBytes       uint64 `json:"outBytes"`
	Responses      struct {
		OneXx   uint64 `json:"1xx"`
		TwoXx   uint64 `json:"2xx"`
		ThreeXx uint64 `json:"3xx"`
		FourXx  uint64 `json:"4xx"`
		FiveXx  uint64 `json:"5xx"`
	} `json:"responses"`
	SessionMsecCounter    uint64 `json:"sessionMsecCounter"`
	SessionMsec           uint64 `json:"sessionMsec"`
	USessionMsecCounter   uint64 `json:"uSessionMsecCounter"`
	USessionMsec          uint64 `json:"uSessionMsec"`
	UConnectMsecCounter   uint64 `json:"uConnectMsecCounter"`
	UConnectMsec          uint64 `json:"uConnectMsec"`
	UFirstByteMsecCounter uint64 `json:"uFirstByteMsecCounter"`
	UFirstByteMsec        uint64 `json:"uFirstByteMsec"`
	Weight                uint64 `json:"weight"`
	MaxFails              uint64 `json:"maxFails"`
	FailTimeout           uint64 `json:"failTimeout"`
	Backup                bool   `json:"backup"`
	Down                  bool   `json:"down"`
}

func gatherStatusURL(r *bufio.Reader, tags map[string]string, acc telegraf.Accumulator) error {
	dec := json.NewDecoder(r)
	status := &nginxSTSResponse{}
	if err := dec.Decode(status); err != nil {
		return errors.New("error while decoding JSON response")
	}

	acc.AddFields("nginx_sts_connections", map[string]interface{}{
		"active":   status.Connections.Active,
		"reading":  status.Connections.Reading,
		"writing":  status.Connections.Writing,
		"waiting":  status.Connections.Waiting,
		"accepted": status.Connections.Accepted,
		"handled":  status.Connections.Handled,
		"requests": status.Connections.Requests,
	}, tags)

	for zoneName, zone := range status.StreamServerZones {
		zoneTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			zoneTags[k] = v
		}
		zoneTags["zone"] = zoneName

		acc.AddFields("nginx_sts_server", map[string]interface{}{
			"connects":             zone.ConnectCounter,
			"in_bytes":             zone.InBytes,
			"out_bytes":            zone.OutBytes,
			"session_msec_counter": zone.SessionMsecCounter,
			"session_msec":         zone.SessionMsec,

			"response_1xx_count": zone.Responses.OneXx,
			"response_2xx_count": zone.Responses.TwoXx,
			"response_3xx_count": zone.Responses.ThreeXx,
			"response_4xx_count": zone.Responses.FourXx,
			"response_5xx_count": zone.Responses.FiveXx,
		}, zoneTags)
	}

	for filterName, filters := range status.StreamFilterZones {
		for filterKey, upstream := range filters {
			filterTags := make(map[string]string, len(tags)+2)
			for k, v := range tags {
				filterTags[k] = v
			}
			filterTags["filter_key"] = filterKey
			filterTags["filter_name"] = filterName

			acc.AddFields("nginx_sts_filter", map[string]interface{}{
				"connects":             upstream.ConnectCounter,
				"in_bytes":             upstream.InBytes,
				"out_bytes":            upstream.OutBytes,
				"session_msec_counter": upstream.SessionMsecCounter,
				"session_msec":         upstream.SessionMsec,

				"response_1xx_count": upstream.Responses.OneXx,
				"response_2xx_count": upstream.Responses.TwoXx,
				"response_3xx_count": upstream.Responses.ThreeXx,
				"response_4xx_count": upstream.Responses.FourXx,
				"response_5xx_count": upstream.Responses.FiveXx,
			}, filterTags)
		}
	}

	for upstreamName, upstreams := range status.StreamUpstreamZones {
		for _, upstream := range upstreams {
			upstreamServerTags := make(map[string]string, len(tags)+2)
			for k, v := range tags {
				upstreamServerTags[k] = v
			}
			upstreamServerTags["upstream"] = upstreamName
			upstreamServerTags["upstream_address"] = upstream.Server
			acc.AddFields("nginx_sts_upstream", map[string]interface{}{
				"connects":                        upstream.ConnectCounter,
				"session_msec":                    upstream.SessionMsec,
				"session_msec_counter":            upstream.SessionMsecCounter,
				"upstream_session_msec":           upstream.USessionMsec,
				"upstream_session_msec_counter":   upstream.USessionMsecCounter,
				"upstream_connect_msec":           upstream.UConnectMsec,
				"upstream_connect_msec_counter":   upstream.UConnectMsecCounter,
				"upstream_firstbyte_msec":         upstream.UFirstByteMsec,
				"upstream_firstbyte_msec_counter": upstream.UFirstByteMsecCounter,
				"in_bytes":                        upstream.InBytes,
				"out_bytes":                       upstream.OutBytes,

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
	inputs.Add("nginx_sts", func() telegraf.Input {
		return &NginxSTS{}
	})
}
