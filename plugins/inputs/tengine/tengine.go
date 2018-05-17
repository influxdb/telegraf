package tengine

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"io"
)

type Tengine struct {
	Urls            []string
	ResponseTimeout internal.Duration
	tls.ClientConfig

	// HTTP client
	client *http.Client
}

var sampleConfig = `
  # An array of Nginx stub_status URI to gather stats.
  urls = ["http://localhost/server_status"]

  ## Optional TLS Config
  tls_ca = "/etc/telegraf/ca.pem"
  tls_cert = "/etc/telegraf/cert.cer"
  tls_key = "/etc/telegraf/key.key"
  ## Use TLS but skip chain & host verification
  insecure_skip_verify = false

  # HTTP response timeout (default: 5s)
  response_timeout = "5s"
`

func (n *Tengine) SampleConfig() string {
	return sampleConfig
}

func (n *Tengine) Description() string {
	return "Read Nginx's basic status information (ngx_http_stub_status_module)"
}

func (n *Tengine) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	// Create an HTTP client that is re-used for each
	// collection interval
	if n.client == nil {
		client, err := n.createHttpClient()
		if err != nil {
			return err
		}
		n.client = client
	}

	for _, u := range n.Urls {
		addr, err := url.Parse(u)
		if err != nil {
			acc.AddError(fmt.Errorf("Unable to parse address '%s': %s", u, err))
			continue
		}

		wg.Add(1)
		go func(addr *url.URL) {
			defer wg.Done()
			acc.AddError(n.gatherUrl(addr, acc))
		}(addr)
	}

	wg.Wait()
	return nil
}

func (n *Tengine) createHttpClient() (*http.Client, error) {
	tlsCfg, err := n.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}

	if n.ResponseTimeout.Duration < time.Second {
		n.ResponseTimeout.Duration = time.Second * 5
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
		Timeout: n.ResponseTimeout.Duration,
	}

	return client, nil
}
type TengineSatus struct {
	host string `json:"host"`
	bytes_in uint64 `json:"bytes_in"`
	bytes_out uint64 `json:"bytes_out"`
	conn_total uint64 `json:"conn_total"`
	req_total uint64 `json:"req_total"`
	http_2xx uint64 `json:"http_2xx"`
	http_3xx uint64 `json:"http_3xx"`
	http_4xx uint64 `json:"http_4xx"`
	http_5xx uint64 `json:"http_5xx"`
	http_other_status uint64 `json:"http_other_status"`
	rt uint64 `json:"rt"`
	ups_req uint64 `json:"ups_req"`
	ups_rt uint64 `json:"ups_rt"`
	ups_tries uint64 `json:"ups_tries"`
	http_200 uint64 `json:"http_200"`
	http_206 uint64 `json:"http_206"`
	http_302 uint64 `json:"http_302"`
	http_304 uint64 `json:"http_304"`
	http_403 uint64 `json:"http_403"`
	http_404 uint64 `json:"http_404"`
	http_416 uint64 `json:"http_416"`
	http_499 uint64 `json:"http_499"`
	http_500 uint64 `json:"http_500"`
	http_502 uint64 `json:"http_502"`
	http_503 uint64 `json:"http_503"`
	http_504 uint64 `json:"http_504"`
	http_508 uint64 `json:"http_508"`
	http_other_detail_status uint64 `json:"http_other_detail_status"`
	http_ups_4xx uint64 `json:"http_ups_4xx"`
	http_ups_5xx uint64 `json:"http_ups_5xx"`
}

func (n *Tengine) gatherUrl(addr *url.URL, acc telegraf.Accumulator) error {
	var tenginestatus TengineSatus
	resp, err := n.client.Get(addr.String())
	if err != nil {
		return fmt.Errorf("error making HTTP request to %s: %s", addr.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP status %s", addr.String(), resp.Status)
	}
	r := bufio.NewReader(resp.Body)

	for {
		line, err := r.ReadString('\n')

		if err != nil || io.EOF == err {
			break
		}
		line_split := strings.Split(strings.TrimSpace(line), ",")
		tenginestatus.host= line_split[0]
		if err != nil {
			return err
		}
		tenginestatus.bytes_in, err = strconv.ParseUint(line_split[1], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.bytes_out, err = strconv.ParseUint(line_split[2], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.conn_total, err = strconv.ParseUint(line_split[3], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.req_total, err = strconv.ParseUint(line_split[4], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_2xx, err = strconv.ParseUint(line_split[5], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_3xx, err = strconv.ParseUint(line_split[6], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_4xx, err = strconv.ParseUint(line_split[7], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_5xx, err = strconv.ParseUint(line_split[8], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_other_status, err = strconv.ParseUint(line_split[9], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.rt, err = strconv.ParseUint(line_split[10], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.ups_req, err = strconv.ParseUint(line_split[11], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.ups_rt, err = strconv.ParseUint(line_split[12], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.ups_tries, err = strconv.ParseUint(line_split[13], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_200, err = strconv.ParseUint(line_split[14], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_206, err = strconv.ParseUint(line_split[15], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_302, err = strconv.ParseUint(line_split[16], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_304, err = strconv.ParseUint(line_split[17], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_403, err = strconv.ParseUint(line_split[18], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_404, err = strconv.ParseUint(line_split[19], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_416, err = strconv.ParseUint(line_split[20], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_499, err = strconv.ParseUint(line_split[21], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_500, err = strconv.ParseUint(line_split[22], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_502, err = strconv.ParseUint(line_split[23], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_503, err = strconv.ParseUint(line_split[24], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_504, err = strconv.ParseUint(line_split[25], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_508, err = strconv.ParseUint(line_split[26], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_other_detail_status, err = strconv.ParseUint(line_split[27], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_ups_4xx, err = strconv.ParseUint(line_split[28], 10, 64)
		if err != nil {
			return err
		}
		tenginestatus.http_ups_5xx, err = strconv.ParseUint(line_split[29], 10, 64)
		if err != nil {
			return err
		}
		tags := getTags(addr)
		tags["server_name"] = tenginestatus.host
		fields := map[string]interface{}{
			"bytes_in": tenginestatus.bytes_in,
			"bytes_out": tenginestatus.bytes_out,
			"conn_total": tenginestatus.conn_total,
			"req_total": tenginestatus.req_total,
			"http_2xx": tenginestatus.http_2xx,
			"http_3xx": tenginestatus.http_3xx,
			"http_4xx": tenginestatus.http_4xx,
			"http_5xx": tenginestatus.http_5xx,
			"http_other_status": tenginestatus.http_other_status,
			"rt": tenginestatus.rt,
			"ups_req": tenginestatus.ups_req,
			"ups_rt": tenginestatus.ups_rt,
			"ups_tries": tenginestatus.ups_tries,
			"http_200": tenginestatus.http_200,
			"http_206": tenginestatus.http_206,
			"http_302": tenginestatus.http_302,
			"http_304": tenginestatus.http_304,
			"http_403": tenginestatus.http_403,
			"http_404": tenginestatus.http_404,
			"http_416": tenginestatus.http_416,
			"http_499": tenginestatus.http_499,
			"http_500": tenginestatus.http_500,
			"http_502": tenginestatus.http_502,
			"http_503": tenginestatus.http_503,
			"http_504": tenginestatus.http_504,
			"http_508": tenginestatus.http_508,
			"http_other_detail_status": tenginestatus.http_other_status,
			"http_ups_4xx": tenginestatus.http_ups_4xx,
			"http_ups_5xx": tenginestatus.http_ups_5xx,
		}
		acc.AddFields("tengine", fields, tags)
	}

	return nil
}

// Get tag(s) for the tengine plugin
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

func init() {
	inputs.Add("tengine", func() telegraf.Input {
		return &Tengine{}
	})
}
