//go:generate ../../../tools/readme_config_includer/generator
// The MIT License (MIT)
//
// Copyright (c) 2015 Jeff Nickoloff (jeff@allingeek.com)
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package nsq

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	requestPattern = `%s/stats?format=json`
)

type NSQ struct {
	Endpoints []string `toml:"endpoints"`

	tls.ClientConfig
	httpClient *http.Client
}

func (*NSQ) SampleConfig() string {
	return sampleConfig
}

func (n *NSQ) Gather(acc telegraf.Accumulator) error {
	var err error

	if n.httpClient == nil {
		n.httpClient, err = n.getHTTPClient()
		if err != nil {
			return err
		}
	}

	var wg sync.WaitGroup
	for _, e := range n.Endpoints {
		wg.Add(1)
		go func(e string) {
			defer wg.Done()
			acc.AddError(n.gatherEndpoint(e, acc))
		}(e)
	}

	wg.Wait()
	return nil
}

func (n *NSQ) getHTTPClient() (*http.Client, error) {
	tlsConfig, err := n.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	httpClient := &http.Client{
		Transport: tr,
		Timeout:   4 * time.Second,
	}
	return httpClient, nil
}

func (n *NSQ) gatherEndpoint(e string, acc telegraf.Accumulator) error {
	u, err := buildURL(e)
	if err != nil {
		return err
	}
	r, err := n.httpClient.Get(u.String())
	if err != nil {
		return fmt.Errorf("error while polling %s: %w", u.String(), err)
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP status %s", u.String(), r.Status)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf(`error reading body: %w`, err)
	}

	data := &nsqStatsData{}
	err = json.Unmarshal(body, data)
	if err != nil {
		return fmt.Errorf(`error parsing response: %w`, err)
	}
	// Data was not parsed correctly attempt to use old format.
	if len(data.Version) < 1 {
		wrapper := &nsqStats{}
		err = json.Unmarshal(body, wrapper)
		if err != nil {
			return fmt.Errorf(`error parsing response: %w`, err)
		}
		data = &wrapper.Data
	}

	tags := map[string]string{
		`server_host`:    u.Host,
		`server_version`: data.Version,
	}

	fields := make(map[string]interface{})
	if data.Health == `OK` {
		fields["server_count"] = int64(1)
	} else {
		fields["server_count"] = int64(0)
	}
	fields["topic_count"] = int64(len(data.Topics))

	acc.AddFields("nsq_server", fields, tags)
	for _, t := range data.Topics {
		gatherTopicStats(t, acc, u.Host, data.Version)
	}

	return nil
}

func buildURL(e string) (*url.URL, error) {
	u := fmt.Sprintf(requestPattern, e)
	addr, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("unable to parse address %q: %w", u, err)
	}
	return addr, nil
}

func gatherTopicStats(t topicStats, acc telegraf.Accumulator, host, version string) {
	// per topic overall (tag: name, paused, channel count)
	tags := map[string]string{
		"server_host":    host,
		"server_version": version,
		"topic":          t.Name,
	}

	fields := map[string]interface{}{
		"depth":         t.Depth,
		"backend_depth": t.BackendDepth,
		"message_count": t.MessageCount,
		"channel_count": int64(len(t.Channels)),
	}
	acc.AddFields("nsq_topic", fields, tags)

	for _, c := range t.Channels {
		gatherChannelStats(c, acc, host, version, t.Name)
	}
}

func gatherChannelStats(c channelStats, acc telegraf.Accumulator, host, version, topic string) {
	tags := map[string]string{
		"server_host":    host,
		"server_version": version,
		"topic":          topic,
		"channel":        c.Name,
	}

	fields := map[string]interface{}{
		"depth":          c.Depth,
		"backend_depth":  c.BackendDepth,
		"inflight_count": c.InFlightCount,
		"deferred_count": c.DeferredCount,
		"message_count":  c.MessageCount,
		"requeue_count":  c.RequeueCount,
		"timeout_count":  c.TimeoutCount,
		"client_count":   int64(len(c.Clients)),
	}

	acc.AddFields("nsq_channel", fields, tags)
	for _, cl := range c.Clients {
		gatherClientStats(cl, acc, host, version, topic, c.Name)
	}
}

func gatherClientStats(c clientStats, acc telegraf.Accumulator, host, version, topic, channel string) {
	tags := map[string]string{
		"server_host":       host,
		"server_version":    version,
		"topic":             topic,
		"channel":           channel,
		"client_id":         c.ID,
		"client_hostname":   c.Hostname,
		"client_version":    c.Version,
		"client_address":    c.RemoteAddress,
		"client_user_agent": c.UserAgent,
		"client_tls":        strconv.FormatBool(c.TLS),
		"client_snappy":     strconv.FormatBool(c.Snappy),
		"client_deflate":    strconv.FormatBool(c.Deflate),
	}
	if len(c.Name) > 0 {
		tags["client_name"] = c.Name
	}

	fields := map[string]interface{}{
		"ready_count":    c.ReadyCount,
		"inflight_count": c.InFlightCount,
		"message_count":  c.MessageCount,
		"finish_count":   c.FinishCount,
		"requeue_count":  c.RequeueCount,
	}
	acc.AddFields("nsq_client", fields, tags)
}

type nsqStats struct {
	Code int64        `json:"status_code"`
	Txt  string       `json:"status_txt"`
	Data nsqStatsData `json:"data"`
}

type nsqStatsData struct {
	Version   string       `json:"version"`
	Health    string       `json:"health"`
	StartTime int64        `json:"start_time"`
	Topics    []topicStats `json:"topics"`
}

// e2e_processing_latency is not modeled
type topicStats struct {
	Name         string         `json:"topic_name"`
	Depth        int64          `json:"depth"`
	BackendDepth int64          `json:"backend_depth"`
	MessageCount int64          `json:"message_count"`
	Paused       bool           `json:"paused"`
	Channels     []channelStats `json:"channels"`
}

// e2e_processing_latency is not modeled
type channelStats struct {
	Name          string        `json:"channel_name"`
	Depth         int64         `json:"depth"`
	BackendDepth  int64         `json:"backend_depth"`
	InFlightCount int64         `json:"in_flight_count"`
	DeferredCount int64         `json:"deferred_count"`
	MessageCount  int64         `json:"message_count"`
	RequeueCount  int64         `json:"requeue_count"`
	TimeoutCount  int64         `json:"timeout_count"`
	Paused        bool          `json:"paused"`
	Clients       []clientStats `json:"clients"`
}

type clientStats struct {
	Name                          string `json:"name"` // DEPRECATED 1.x+, still here as the structs are currently being shared for parsing v3.x and 1.x
	ID                            string `json:"client_id"`
	Hostname                      string `json:"hostname"`
	Version                       string `json:"version"`
	RemoteAddress                 string `json:"remote_address"`
	State                         int64  `json:"state"`
	ReadyCount                    int64  `json:"ready_count"`
	InFlightCount                 int64  `json:"in_flight_count"`
	MessageCount                  int64  `json:"message_count"`
	FinishCount                   int64  `json:"finish_count"`
	RequeueCount                  int64  `json:"requeue_count"`
	ConnectTime                   int64  `json:"connect_ts"`
	SampleRate                    int64  `json:"sample_rate"`
	Deflate                       bool   `json:"deflate"`
	Snappy                        bool   `json:"snappy"`
	UserAgent                     string `json:"user_agent"`
	TLS                           bool   `json:"tls"`
	TLSCipherSuite                string `json:"tls_cipher_suite"`
	TLSVersion                    string `json:"tls_version"`
	TLSNegotiatedProtocol         string `json:"tls_negotiated_protocol"`
	TLSNegotiatedProtocolIsMutual bool   `json:"tls_negotiated_protocol_is_mutual"`
}

func newNSQ() *NSQ {
	return &NSQ{}
}

func init() {
	inputs.Add("nsq", func() telegraf.Input {
		return newNSQ()
	})
}
