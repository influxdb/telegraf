//go:generate ../../../tools/readme_config_includer/generator
package nginx_plus

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
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

type NginxPlus struct {
	Urls            []string        `toml:"urls"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	tls.ClientConfig

	client *http.Client
}

func (*NginxPlus) SampleConfig() string {
	return sampleConfig
}

func (n *NginxPlus) Gather(acc telegraf.Accumulator) error {
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

func (n *NginxPlus) createHTTPClient() (*http.Client, error) {
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

func (n *NginxPlus) gatherURL(addr *url.URL, acc telegraf.Accumulator) error {
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
	return map[string]string{"server": host, "port": port}
}

type responseStats struct {
	Responses1xx int64 `json:"1xx"`
	Responses2xx int64 `json:"2xx"`
	Responses3xx int64 `json:"3xx"`
	Responses4xx int64 `json:"4xx"`
	Responses5xx int64 `json:"5xx"`
	Total        int64 `json:"total"`
}

type basicHitStats struct {
	Responses int64 `json:"responses"`
	Bytes     int64 `json:"bytes"`
}

type extendedHitStats struct {
	basicHitStats
	ResponsesWritten int64 `json:"responses_written"`
	BytesWritten     int64 `json:"bytes_written"`
}

type healthCheckStats struct {
	Checks     int64 `json:"checks"`
	Fails      int64 `json:"fails"`
	Unhealthy  int64 `json:"unhealthy"`
	LastPassed *bool `json:"last_passed"`
}

type status struct {
	Version       int    `json:"version"`
	NginxVersion  string `json:"nginx_version"`
	Address       string `json:"address"`
	Generation    *int   `json:"generation"`     // added in version 5
	LoadTimestamp *int64 `json:"load_timestamp"` // added in version 2
	Timestamp     int64  `json:"timestamp"`
	Pid           *int   `json:"pid"` // added in version 6

	Processes *struct { // added in version 5
		Respawned *int `json:"respawned"`
	} `json:"processes"`

	Connections struct {
		Accepted int64 `json:"accepted"`
		Dropped  int64 `json:"dropped"`
		Active   int64 `json:"active"`
		Idle     int64 `json:"idle"`
	} `json:"connections"`

	Ssl *struct { // added in version 6
		Handshakes       int64 `json:"handshakes"`
		HandshakesFailed int64 `json:"handshakes_failed"`
		SessionReuses    int64 `json:"session_reuses"`
	} `json:"ssl"`

	Requests struct {
		Total   int64 `json:"total"`
		Current int   `json:"current"`
	} `json:"requests"`

	ServerZones map[string]struct { // added in version 2
		Processing int           `json:"processing"`
		Requests   int64         `json:"requests"`
		Responses  responseStats `json:"responses"`
		Discarded  *int64        `json:"discarded"` // added in version 6
		Received   int64         `json:"received"`
		Sent       int64         `json:"sent"`
	} `json:"server_zones"`

	Upstreams map[string]struct {
		Peers []struct {
			ID           *int             `json:"id"` // added in version 3
			Server       string           `json:"server"`
			Backup       bool             `json:"backup"`
			Weight       int              `json:"weight"`
			State        string           `json:"state"`
			Active       int              `json:"active"`
			Keepalive    *int             `json:"keepalive"` // removed in version 5
			MaxConns     *int             `json:"max_conns"` // added in version 3
			Requests     int64            `json:"requests"`
			Responses    responseStats    `json:"responses"`
			Sent         int64            `json:"sent"`
			Received     int64            `json:"received"`
			Fails        int64            `json:"fails"`
			Unavail      int64            `json:"unavail"`
			HealthChecks healthCheckStats `json:"health_checks"`
			Downtime     int64            `json:"downtime"`
			Downstart    int64            `json:"downstart"`
			Selected     *int64           `json:"selected"`      // added in version 4
			HeaderTime   *int64           `json:"header_time"`   // added in version 5
			ResponseTime *int64           `json:"response_time"` // added in version 5
		} `json:"peers"`
		Keepalive int       `json:"keepalive"`
		Zombies   int       `json:"zombies"` // added in version 6
		Queue     *struct { // added in version 6
			Size      int   `json:"size"`
			MaxSize   int   `json:"max_size"`
			Overflows int64 `json:"overflows"`
		} `json:"queue"`
	} `json:"upstreams"`

	Caches map[string]struct { // added in version 2
		Size        int64            `json:"size"`
		MaxSize     int64            `json:"max_size"`
		Cold        bool             `json:"cold"`
		Hit         basicHitStats    `json:"hit"`
		Stale       basicHitStats    `json:"stale"`
		Updating    basicHitStats    `json:"updating"`
		Revalidated *basicHitStats   `json:"revalidated"` // added in version 3
		Miss        extendedHitStats `json:"miss"`
		Expired     extendedHitStats `json:"expired"`
		Bypass      extendedHitStats `json:"bypass"`
	} `json:"caches"`

	Stream struct {
		ServerZones map[string]struct {
			Processing  int            `json:"processing"`
			Connections int            `json:"connections"`
			Sessions    *responseStats `json:"sessions"`
			Discarded   *int64         `json:"discarded"` // added in version 7
			Received    int64          `json:"received"`
			Sent        int64          `json:"sent"`
		} `json:"server_zones"`
		Upstreams map[string]struct {
			Peers []struct {
				ID            int              `json:"id"`
				Server        string           `json:"server"`
				Backup        bool             `json:"backup"`
				Weight        int              `json:"weight"`
				State         string           `json:"state"`
				Active        int              `json:"active"`
				Connections   int64            `json:"connections"`
				ConnectTime   *int             `json:"connect_time"`
				FirstByteTime *int             `json:"first_byte_time"`
				ResponseTime  *int             `json:"response_time"`
				Sent          int64            `json:"sent"`
				Received      int64            `json:"received"`
				Fails         int64            `json:"fails"`
				Unavail       int64            `json:"unavail"`
				HealthChecks  healthCheckStats `json:"health_checks"`
				Downtime      int64            `json:"downtime"`
				Downstart     int64            `json:"downstart"`
				Selected      int64            `json:"selected"`
			} `json:"peers"`
			Zombies int `json:"zombies"`
		} `json:"upstreams"`
	} `json:"stream"`
}

func gatherStatusURL(r *bufio.Reader, tags map[string]string, acc telegraf.Accumulator) error {
	dec := json.NewDecoder(r)
	status := &status{}
	if err := dec.Decode(status); err != nil {
		return errors.New("error while decoding JSON response")
	}
	status.gather(tags, acc)
	return nil
}

func (s *status) gather(tags map[string]string, acc telegraf.Accumulator) {
	s.gatherProcessesMetrics(tags, acc)
	s.gatherConnectionsMetrics(tags, acc)
	s.gatherSslMetrics(tags, acc)
	s.gatherRequestMetrics(tags, acc)
	s.gatherZoneMetrics(tags, acc)
	s.gatherUpstreamMetrics(tags, acc)
	s.gatherCacheMetrics(tags, acc)
	s.gatherStreamMetrics(tags, acc)
}

func (s *status) gatherProcessesMetrics(tags map[string]string, acc telegraf.Accumulator) {
	var respawned int

	if s.Processes.Respawned != nil {
		respawned = *s.Processes.Respawned
	}

	acc.AddFields(
		"nginx_plus_processes",
		map[string]interface{}{
			"respawned": respawned,
		},
		tags,
	)
}

func (s *status) gatherConnectionsMetrics(tags map[string]string, acc telegraf.Accumulator) {
	acc.AddFields(
		"nginx_plus_connections",
		map[string]interface{}{
			"accepted": s.Connections.Accepted,
			"dropped":  s.Connections.Dropped,
			"active":   s.Connections.Active,
			"idle":     s.Connections.Idle,
		},
		tags,
	)
}

func (s *status) gatherSslMetrics(tags map[string]string, acc telegraf.Accumulator) {
	acc.AddFields(
		"nginx_plus_ssl",
		map[string]interface{}{
			"handshakes":        s.Ssl.Handshakes,
			"handshakes_failed": s.Ssl.HandshakesFailed,
			"session_reuses":    s.Ssl.SessionReuses,
		},
		tags,
	)
}

func (s *status) gatherRequestMetrics(tags map[string]string, acc telegraf.Accumulator) {
	acc.AddFields(
		"nginx_plus_requests",
		map[string]interface{}{
			"total":   s.Requests.Total,
			"current": s.Requests.Current,
		},
		tags,
	)
}

func (s *status) gatherZoneMetrics(tags map[string]string, acc telegraf.Accumulator) {
	for zoneName, zone := range s.ServerZones {
		zoneTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			zoneTags[k] = v
		}
		zoneTags["zone"] = zoneName
		acc.AddFields(
			"nginx_plus_zone",
			func() map[string]interface{} {
				result := map[string]interface{}{
					"processing":      zone.Processing,
					"requests":        zone.Requests,
					"responses_1xx":   zone.Responses.Responses1xx,
					"responses_2xx":   zone.Responses.Responses2xx,
					"responses_3xx":   zone.Responses.Responses3xx,
					"responses_4xx":   zone.Responses.Responses4xx,
					"responses_5xx":   zone.Responses.Responses5xx,
					"responses_total": zone.Responses.Total,
					"received":        zone.Received,
					"sent":            zone.Sent,
				}
				if zone.Discarded != nil {
					result["discarded"] = *zone.Discarded
				}
				return result
			}(),
			zoneTags,
		)
	}
}

func (s *status) gatherUpstreamMetrics(tags map[string]string, acc telegraf.Accumulator) {
	for upstreamName, upstream := range s.Upstreams {
		upstreamTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			upstreamTags[k] = v
		}
		upstreamTags["upstream"] = upstreamName
		upstreamFields := map[string]interface{}{
			"keepalive": upstream.Keepalive,
			"zombies":   upstream.Zombies,
		}
		if upstream.Queue != nil {
			upstreamFields["queue_size"] = upstream.Queue.Size
			upstreamFields["queue_max_size"] = upstream.Queue.MaxSize
			upstreamFields["queue_overflows"] = upstream.Queue.Overflows
		}
		acc.AddFields(
			"nginx_plus_upstream",
			upstreamFields,
			upstreamTags,
		)
		for _, peer := range upstream.Peers {
			var selected int64

			if peer.Selected != nil {
				selected = *peer.Selected
			}

			peerFields := map[string]interface{}{
				"backup":                 peer.Backup,
				"weight":                 peer.Weight,
				"state":                  peer.State,
				"active":                 peer.Active,
				"requests":               peer.Requests,
				"responses_1xx":          peer.Responses.Responses1xx,
				"responses_2xx":          peer.Responses.Responses2xx,
				"responses_3xx":          peer.Responses.Responses3xx,
				"responses_4xx":          peer.Responses.Responses4xx,
				"responses_5xx":          peer.Responses.Responses5xx,
				"responses_total":        peer.Responses.Total,
				"sent":                   peer.Sent,
				"received":               peer.Received,
				"fails":                  peer.Fails,
				"unavail":                peer.Unavail,
				"healthchecks_checks":    peer.HealthChecks.Checks,
				"healthchecks_fails":     peer.HealthChecks.Fails,
				"healthchecks_unhealthy": peer.HealthChecks.Unhealthy,
				"downtime":               peer.Downtime,
				"downstart":              peer.Downstart,
				"selected":               selected,
			}
			if peer.HealthChecks.LastPassed != nil {
				peerFields["healthchecks_last_passed"] = *peer.HealthChecks.LastPassed
			}
			if peer.HeaderTime != nil {
				peerFields["header_time"] = *peer.HeaderTime
			}
			if peer.ResponseTime != nil {
				peerFields["response_time"] = *peer.ResponseTime
			}
			if peer.MaxConns != nil {
				peerFields["max_conns"] = *peer.MaxConns
			}
			peerTags := make(map[string]string, len(upstreamTags)+2)
			for k, v := range upstreamTags {
				peerTags[k] = v
			}
			peerTags["upstream_address"] = peer.Server
			if peer.ID != nil {
				peerTags["id"] = strconv.Itoa(*peer.ID)
			}
			acc.AddFields("nginx_plus_upstream_peer", peerFields, peerTags)
		}
	}
}

func (s *status) gatherCacheMetrics(tags map[string]string, acc telegraf.Accumulator) {
	for cacheName, cache := range s.Caches {
		cacheTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			cacheTags[k] = v
		}
		cacheTags["cache"] = cacheName
		acc.AddFields(
			"nginx_plus_cache",
			map[string]interface{}{
				"size":                      cache.Size,
				"max_size":                  cache.MaxSize,
				"cold":                      cache.Cold,
				"hit_responses":             cache.Hit.Responses,
				"hit_bytes":                 cache.Hit.Bytes,
				"stale_responses":           cache.Stale.Responses,
				"stale_bytes":               cache.Stale.Bytes,
				"updating_responses":        cache.Updating.Responses,
				"updating_bytes":            cache.Updating.Bytes,
				"revalidated_responses":     cache.Revalidated.Responses,
				"revalidated_bytes":         cache.Revalidated.Bytes,
				"miss_responses":            cache.Miss.Responses,
				"miss_bytes":                cache.Miss.Bytes,
				"miss_responses_written":    cache.Miss.ResponsesWritten,
				"miss_bytes_written":        cache.Miss.BytesWritten,
				"expired_responses":         cache.Expired.Responses,
				"expired_bytes":             cache.Expired.Bytes,
				"expired_responses_written": cache.Expired.ResponsesWritten,
				"expired_bytes_written":     cache.Expired.BytesWritten,
				"bypass_responses":          cache.Bypass.Responses,
				"bypass_bytes":              cache.Bypass.Bytes,
				"bypass_responses_written":  cache.Bypass.ResponsesWritten,
				"bypass_bytes_written":      cache.Bypass.BytesWritten,
			},
			cacheTags,
		)
	}
}

func (s *status) gatherStreamMetrics(tags map[string]string, acc telegraf.Accumulator) {
	for zoneName, zone := range s.Stream.ServerZones {
		zoneTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			zoneTags[k] = v
		}
		zoneTags["zone"] = zoneName
		acc.AddFields(
			"nginx.stream.zone",
			map[string]interface{}{
				"processing":  zone.Processing,
				"connections": zone.Connections,
				"received":    zone.Received,
				"sent":        zone.Sent,
			},
			zoneTags,
		)
	}
	for upstreamName, upstream := range s.Stream.Upstreams {
		upstreamTags := make(map[string]string, len(tags)+1)
		for k, v := range tags {
			upstreamTags[k] = v
		}
		upstreamTags["upstream"] = upstreamName
		acc.AddFields(
			"nginx_plus_stream_upstream",
			map[string]interface{}{
				"zombies": upstream.Zombies,
			},
			upstreamTags,
		)
		for _, peer := range upstream.Peers {
			peerFields := map[string]interface{}{
				"backup":                 peer.Backup,
				"weight":                 peer.Weight,
				"state":                  peer.State,
				"active":                 peer.Active,
				"connections":            peer.Connections,
				"sent":                   peer.Sent,
				"received":               peer.Received,
				"fails":                  peer.Fails,
				"unavail":                peer.Unavail,
				"healthchecks_checks":    peer.HealthChecks.Checks,
				"healthchecks_fails":     peer.HealthChecks.Fails,
				"healthchecks_unhealthy": peer.HealthChecks.Unhealthy,
				"downtime":               peer.Downtime,
				"downstart":              peer.Downstart,
				"selected":               peer.Selected,
			}
			if peer.HealthChecks.LastPassed != nil {
				peerFields["healthchecks_last_passed"] = *peer.HealthChecks.LastPassed
			}
			if peer.ConnectTime != nil {
				peerFields["connect_time"] = *peer.ConnectTime
			}
			if peer.FirstByteTime != nil {
				peerFields["first_byte_time"] = *peer.FirstByteTime
			}
			if peer.ResponseTime != nil {
				peerFields["response_time"] = *peer.ResponseTime
			}
			peerTags := make(map[string]string, len(upstreamTags)+2)
			for k, v := range upstreamTags {
				peerTags[k] = v
			}
			peerTags["upstream_address"] = peer.Server
			peerTags["id"] = strconv.Itoa(peer.ID)
			acc.AddFields("nginx_plus_stream_upstream_peer", peerFields, peerTags)
		}
	}
}

func init() {
	inputs.Add("nginx_plus", func() telegraf.Input {
		return &NginxPlus{}
	})
}
