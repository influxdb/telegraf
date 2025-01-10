//go:generate ../../../tools/readme_config_includer/generator
package nginx

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

type Nginx struct {
	Urls            []string        `toml:"urls"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	tls.ClientConfig

	// HTTP client
	client *http.Client
}

func (*Nginx) SampleConfig() string {
	return sampleConfig
}

func (n *Nginx) Gather(acc telegraf.Accumulator) error {
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

func (n *Nginx) createHTTPClient() (*http.Client, error) {
	tlsCfg, err := n.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}

	if n.ResponseTimeout < config.Duration(time.Second) {
		n.ResponseTimeout = config.Duration(time.Second * 5)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
		Timeout: time.Duration(n.ResponseTimeout),
	}

	return client, nil
}

func (n *Nginx) gatherURL(addr *url.URL, acc telegraf.Accumulator) error {
	resp, err := n.client.Get(addr.String())
	if err != nil {
		return fmt.Errorf("error making HTTP request to %q: %w", addr.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP status %s", addr.String(), resp.Status)
	}
	r := bufio.NewReader(resp.Body)

	// Active connections
	_, err = r.ReadString(':')
	if err != nil {
		return err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	active, err := strconv.ParseUint(strings.TrimSpace(line), 10, 64)
	if err != nil {
		return err
	}

	// Server accepts handled requests
	_, err = r.ReadString('\n')
	if err != nil {
		return err
	}
	line, err = r.ReadString('\n')
	if err != nil {
		return err
	}
	data := strings.Fields(line)
	accepts, err := strconv.ParseUint(data[0], 10, 64)
	if err != nil {
		return err
	}

	handled, err := strconv.ParseUint(data[1], 10, 64)
	if err != nil {
		return err
	}
	requests, err := strconv.ParseUint(data[2], 10, 64)
	if err != nil {
		return err
	}

	// Reading/Writing/Waiting
	line, err = r.ReadString('\n')
	if err != nil {
		return err
	}
	data = strings.Fields(line)
	reading, err := strconv.ParseUint(data[1], 10, 64)
	if err != nil {
		return err
	}
	writing, err := strconv.ParseUint(data[3], 10, 64)
	if err != nil {
		return err
	}
	waiting, err := strconv.ParseUint(data[5], 10, 64)
	if err != nil {
		return err
	}

	tags := getTags(addr)
	fields := map[string]interface{}{
		"active":   active,
		"accepts":  accepts,
		"handled":  handled,
		"requests": requests,
		"reading":  reading,
		"writing":  writing,
		"waiting":  waiting,
	}
	acc.AddFields("nginx", fields, tags)

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
	return map[string]string{"server": host, "port": port}
}

func init() {
	inputs.Add("nginx", func() telegraf.Input {
		return &Nginx{}
	})
}
