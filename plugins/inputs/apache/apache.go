//go:generate ../../../tools/readme_config_includer/generator
package apache

import (
	"bufio"
	_ "embed"
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

type Apache struct {
	Urls            []string
	Username        string
	Password        string
	ResponseTimeout config.Duration
	tls.ClientConfig

	client *http.Client
}

func (*Apache) SampleConfig() string {
	return sampleConfig
}

func (n *Apache) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	if len(n.Urls) == 0 {
		n.Urls = []string{"http://localhost/server-status?auto"}
	}
	if n.ResponseTimeout < config.Duration(time.Second) {
		n.ResponseTimeout = config.Duration(time.Second * 5)
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
			acc.AddError(n.gatherURL(addr, acc))
		}(addr)
	}

	wg.Wait()
	return nil
}

func (n *Apache) createHTTPClient() (*http.Client, error) {
	tlsCfg, err := n.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
		Timeout: time.Duration(n.ResponseTimeout),
	}

	return client, nil
}

func (n *Apache) gatherURL(addr *url.URL, acc telegraf.Accumulator) error {
	req, err := http.NewRequest("GET", addr.String(), nil)
	if err != nil {
		return fmt.Errorf("error on new request to %q: %w", addr.String(), err)
	}

	if len(n.Username) != 0 && len(n.Password) != 0 {
		req.SetBasicAuth(n.Username, n.Password)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("error on request to %q: %w", addr.String(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP status %s", addr.String(), resp.Status)
	}

	tags := getTags(addr)

	sc := bufio.NewScanner(resp.Body)
	fields := make(map[string]interface{})
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key, part := strings.ReplaceAll(parts[0], " ", ""), strings.TrimSpace(parts[1])

			switch key {
			case "Scoreboard":
				for field, value := range gatherScores(part) {
					fields[field] = value
				}
			default:
				value, err := strconv.ParseFloat(part, 64)
				if err != nil {
					continue
				}
				fields[key] = value
			}
		}
	}
	acc.AddFields("apache", fields, tags)

	return nil
}

func gatherScores(data string) map[string]interface{} {
	var waiting, open = 0, 0
	var s, r, w, k, d, c, l, g, i = 0, 0, 0, 0, 0, 0, 0, 0, 0

	for _, str := range strings.Split(data, "") {
		switch str {
		case "_":
			waiting++
		case "S":
			s++
		case "R":
			r++
		case "W":
			w++
		case "K":
			k++
		case "D":
			d++
		case "C":
			c++
		case "L":
			l++
		case "G":
			g++
		case "I":
			i++
		case ".":
			open++
		}
	}

	fields := map[string]interface{}{
		"scboard_waiting":      float64(waiting),
		"scboard_starting":     float64(s),
		"scboard_reading":      float64(r),
		"scboard_sending":      float64(w),
		"scboard_keepalive":    float64(k),
		"scboard_dnslookup":    float64(d),
		"scboard_closing":      float64(c),
		"scboard_logging":      float64(l),
		"scboard_finishing":    float64(g),
		"scboard_idle_cleanup": float64(i),
		"scboard_open":         float64(open),
	}
	return fields
}

// Get tag(s) for the apache plugin
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
	inputs.Add("apache", func() telegraf.Input {
		return &Apache{}
	})
}
