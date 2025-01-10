//go:generate ../../../tools/readme_config_includer/generator
package vault

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// Vault configuration object
type Vault struct {
	URL       string          `toml:"url"`
	TokenFile string          `toml:"token_file"`
	Token     string          `toml:"token"`
	Log       telegraf.Logger `toml:"-"`
	common_http.HTTPClientConfig

	client *http.Client
}

const timeLayout = "2006-01-02 15:04:05 -0700 MST"

func (*Vault) SampleConfig() string {
	return sampleConfig
}

func (n *Vault) Init() error {
	if n.URL == "" {
		n.URL = "http://127.0.0.1:8200"
	}

	if n.TokenFile == "" && n.Token == "" {
		return errors.New("token missing")
	}

	if n.TokenFile != "" && n.Token != "" {
		return errors.New("both token_file and token are set")
	}

	if n.TokenFile != "" {
		token, err := os.ReadFile(n.TokenFile)
		if err != nil {
			return fmt.Errorf("reading file failed: %w", err)
		}
		n.Token = strings.TrimSpace(string(token))
	}

	ctx := context.Background()
	client, err := n.HTTPClientConfig.CreateClient(ctx, n.Log)
	if err != nil {
		return fmt.Errorf("creating client failed: %w", err)
	}
	n.client = client

	return nil
}

func (*Vault) Start(telegraf.Accumulator) error {
	return nil
}

// Gather, collects metrics from Vault endpoint
func (n *Vault) Gather(acc telegraf.Accumulator) error {
	sysMetrics, err := n.loadJSON(n.URL + "/v1/sys/metrics")
	if err != nil {
		return err
	}

	return buildVaultMetrics(acc, sysMetrics)
}

func (n *Vault) Stop() {
	if n.client != nil {
		n.client.CloseIdleConnections()
	}
}

func (n *Vault) loadJSON(url string) (*SysMetrics, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Vault-Token", n.Token)
	req.Header.Add("Accept", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making HTTP request to %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP status %s", url, resp.Status)
	}

	var metrics SysMetrics
	err = json.NewDecoder(resp.Body).Decode(&metrics)
	if err != nil {
		return nil, fmt.Errorf("error parsing json response: %w", err)
	}

	return &metrics, nil
}

// buildVaultMetrics, it builds all the metrics and adds them to the accumulator
func buildVaultMetrics(acc telegraf.Accumulator, sysMetrics *SysMetrics) error {
	t, err := internal.ParseTimestamp(timeLayout, sysMetrics.Timestamp, nil)
	if err != nil {
		return fmt.Errorf("error parsing time: %w", err)
	}

	for _, counters := range sysMetrics.Counters {
		tags := make(map[string]string)
		for key, val := range counters.baseInfo.Labels {
			convertedVal, err := internal.ToString(val)
			if err != nil {
				return fmt.Errorf("converting counter %s=%v failed: %w", key, val, err)
			}
			tags[key] = convertedVal
		}

		fields := map[string]interface{}{
			"count":  counters.Count,
			"rate":   counters.Rate,
			"sum":    counters.Sum,
			"min":    counters.Min,
			"max":    counters.Max,
			"mean":   counters.Mean,
			"stddev": counters.Stddev,
		}
		acc.AddCounter(counters.baseInfo.Name, fields, tags, t)
	}

	for _, gauges := range sysMetrics.Gauges {
		tags := make(map[string]string)
		for key, val := range gauges.baseInfo.Labels {
			convertedVal, err := internal.ToString(val)
			if err != nil {
				return fmt.Errorf("converting gauges %s=%v failed: %w", key, val, err)
			}
			tags[key] = convertedVal
		}

		fields := map[string]interface{}{
			"value": gauges.Value,
		}

		acc.AddGauge(gauges.Name, fields, tags, t)
	}

	for _, summary := range sysMetrics.Summaries {
		tags := make(map[string]string)
		for key, val := range summary.baseInfo.Labels {
			convertedVal, err := internal.ToString(val)
			if err != nil {
				return fmt.Errorf("converting summary %s=%v failed: %w", key, val, err)
			}
			tags[key] = convertedVal
		}

		fields := map[string]interface{}{
			"count":  summary.Count,
			"rate":   summary.Rate,
			"sum":    summary.Sum,
			"stddev": summary.Stddev,
			"min":    summary.Min,
			"max":    summary.Max,
			"mean":   summary.Mean,
		}
		acc.AddCounter(summary.Name, fields, tags, t)
	}

	return nil
}

func init() {
	inputs.Add("vault", func() telegraf.Input {
		return &Vault{
			HTTPClientConfig: common_http.HTTPClientConfig{
				ResponseHeaderTimeout: config.Duration(5 * time.Second),
			},
		}
	})
}
