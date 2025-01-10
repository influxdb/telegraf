//go:generate ../../../tools/readme_config_includer/generator
package kibana

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const statusPath = "/api/status"

type Kibana struct {
	Servers  []string `toml:"servers"`
	Username string   `toml:"username"`
	Password string   `toml:"password"`

	Log telegraf.Logger `toml:"-"`

	client *http.Client
	common_http.HTTPClientConfig
}

type kibanaStatus struct {
	Name    string  `json:"name"`
	Version version `json:"version"`
	Status  status  `json:"status"`
	Metrics metrics `json:"metrics"`
}

type version struct {
	Number        string `json:"number"`
	BuildHash     string `json:"build_hash"`
	BuildNumber   int    `json:"build_number"`
	BuildSnapshot bool   `json:"build_snapshot"`
}

type status struct {
	Overall  overallStatus `json:"overall"`
	Statuses interface{}   `json:"statuses"`
}

type overallStatus struct {
	State string `json:"state"`
}

type metrics struct {
	UptimeInMillis             float64       `json:"uptime_in_millis"`
	ConcurrentConnections      int64         `json:"concurrent_connections"`
	CollectionIntervalInMilles int64         `json:"collection_interval_in_millis"`
	ResponseTimes              responseTimes `json:"response_times"`
	Process                    process       `json:"process"`
	Requests                   requests      `json:"requests"`
}

type responseTimes struct {
	AvgInMillis float64 `json:"avg_in_millis"`
	MaxInMillis int64   `json:"max_in_millis"`
}

type process struct {
	Mem            mem     `json:"mem"`
	Memory         memory  `json:"memory"`
	UptimeInMillis float64 `json:"uptime_in_millis"`
}

type requests struct {
	Total int64 `json:"total"`
}

type mem struct {
	HeapMaxInBytes  int64 `json:"heap_max_in_bytes"`
	HeapUsedInBytes int64 `json:"heap_used_in_bytes"`
}

type memory struct {
	Heap heap `json:"heap"`
}

type heap struct {
	TotalInBytes int64 `json:"total_in_bytes"`
	UsedInBytes  int64 `json:"used_in_bytes"`
	SizeLimit    int64 `json:"size_limit"`
}

func (*Kibana) SampleConfig() string {
	return sampleConfig
}

func (*Kibana) Start(telegraf.Accumulator) error {
	return nil
}

func (k *Kibana) Gather(acc telegraf.Accumulator) error {
	if k.client == nil {
		client, err := k.createHTTPClient()

		if err != nil {
			return err
		}
		k.client = client
	}

	var wg sync.WaitGroup
	wg.Add(len(k.Servers))

	for _, serv := range k.Servers {
		go func(baseUrl string, acc telegraf.Accumulator) {
			defer wg.Done()
			if err := k.gatherKibanaStatus(baseUrl, acc); err != nil {
				acc.AddError(fmt.Errorf("[url=%s]: %w", baseUrl, err))
				return
			}
		}(serv, acc)
	}

	wg.Wait()
	return nil
}

func (k *Kibana) Stop() {
	if k.client != nil {
		k.client.CloseIdleConnections()
	}
}

func (k *Kibana) createHTTPClient() (*http.Client, error) {
	ctx := context.Background()
	return k.HTTPClientConfig.CreateClient(ctx, k.Log)
}

func (k *Kibana) gatherKibanaStatus(baseURL string, acc telegraf.Accumulator) error {
	kibanaStatus := &kibanaStatus{}
	url := baseURL + statusPath

	host, err := k.gatherJSONData(url, kibanaStatus)
	if err != nil {
		return err
	}

	fields := make(map[string]interface{})
	tags := make(map[string]string)

	tags["name"] = kibanaStatus.Name
	tags["source"] = host
	tags["version"] = kibanaStatus.Version.Number
	tags["status"] = kibanaStatus.Status.Overall.State

	fields["status_code"] = mapHealthStatusToCode(kibanaStatus.Status.Overall.State)
	fields["concurrent_connections"] = kibanaStatus.Metrics.ConcurrentConnections
	fields["response_time_avg_ms"] = kibanaStatus.Metrics.ResponseTimes.AvgInMillis
	fields["response_time_max_ms"] = kibanaStatus.Metrics.ResponseTimes.MaxInMillis
	fields["requests_per_sec"] = float64(kibanaStatus.Metrics.Requests.Total) / float64(kibanaStatus.Metrics.CollectionIntervalInMilles) * 1000

	versionArray := strings.Split(kibanaStatus.Version.Number, ".")
	arrayElement := 1

	if len(versionArray) > 1 {
		arrayElement = 2
	}
	versionNumber, err := strconv.ParseFloat(strings.Join(versionArray[:arrayElement], "."), 64)
	if err != nil {
		return err
	}

	// Same value will be assigned to both the metrics [heap_max_bytes and heap_total_bytes ]
	// Which keeps the code backward compatible
	if versionNumber >= 6.4 {
		fields["uptime_ms"] = int64(kibanaStatus.Metrics.Process.UptimeInMillis)
		fields["heap_max_bytes"] = kibanaStatus.Metrics.Process.Memory.Heap.TotalInBytes
		fields["heap_total_bytes"] = kibanaStatus.Metrics.Process.Memory.Heap.TotalInBytes
		fields["heap_used_bytes"] = kibanaStatus.Metrics.Process.Memory.Heap.UsedInBytes
		fields["heap_size_limit"] = kibanaStatus.Metrics.Process.Memory.Heap.SizeLimit
	} else {
		fields["uptime_ms"] = int64(kibanaStatus.Metrics.UptimeInMillis)
		fields["heap_max_bytes"] = kibanaStatus.Metrics.Process.Mem.HeapMaxInBytes
		fields["heap_total_bytes"] = kibanaStatus.Metrics.Process.Mem.HeapMaxInBytes
		fields["heap_used_bytes"] = kibanaStatus.Metrics.Process.Mem.HeapUsedInBytes
	}
	acc.AddFields("kibana", fields, tags)

	return nil
}

func (k *Kibana) gatherJSONData(url string, v interface{}) (host string, err error) {
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("unable to create new request %q: %w", url, err)
	}

	if (k.Username != "") || (k.Password != "") {
		request.SetBasicAuth(k.Username, k.Password)
	}

	response, err := k.client.Do(request)
	if err != nil {
		return "", err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		//nolint:errcheck // LimitReader returns io.EOF and we're not interested in read errors.
		body, _ := io.ReadAll(io.LimitReader(response.Body, 200))
		return request.Host, fmt.Errorf("%s returned HTTP status %s: %q", url, response.Status, body)
	}

	if err := json.NewDecoder(response.Body).Decode(v); err != nil {
		return request.Host, err
	}

	return request.Host, nil
}

// perform status mapping
func mapHealthStatusToCode(s string) int {
	switch strings.ToLower(s) {
	case "green":
		return 1
	case "yellow":
		return 2
	case "red":
		return 3
	}
	return 0
}

func newKibana() *Kibana {
	return &Kibana{
		HTTPClientConfig: common_http.HTTPClientConfig{
			Timeout: config.Duration(5 * time.Second),
		},
	}
}

func init() {
	inputs.Add("kibana", func() telegraf.Input {
		return newKibana()
	})
}
