//go:generate ../../../tools/readme_config_includer/generator
package proxmox

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Proxmox struct {
	BaseURL               string          `toml:"base_url"`
	APIToken              string          `toml:"api_token"`
	ResponseTimeout       config.Duration `toml:"response_timeout"`
	NodeName              string          `toml:"node_name"`
	AdditionalVmstatsTags []string        `toml:"additional_vmstats_tags"`
	Log                   telegraf.Logger `toml:"-"`
	tls.ClientConfig

	httpClient       *http.Client
	nodeSearchDomain string

	requestFunction func(apiUrl string, method string, data url.Values) ([]byte, error)
}

func (*Proxmox) SampleConfig() string {
	return sampleConfig
}

func (px *Proxmox) Init() error {
	// Check parameters
	for _, v := range px.AdditionalVmstatsTags {
		switch v {
		case "vmid", "status":
			// Do nothing as those are valid values
		default:
			return fmt.Errorf("invalid additional vmstats tag %q", v)
		}
	}

	// Set hostname as default node name for backwards compatibility
	if px.NodeName == "" {
		//nolint:errcheck // best attempt setting of NodeName
		hostname, _ := os.Hostname()
		px.NodeName = hostname
	}

	tlsCfg, err := px.ClientConfig.TLSConfig()
	if err != nil {
		return err
	}
	px.httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
		Timeout: time.Duration(px.ResponseTimeout),
	}

	px.requestFunction = px.performRequest

	return nil
}

func (px *Proxmox) Gather(acc telegraf.Accumulator) error {
	if err := px.getNodeSearchDomain(); err != nil {
		return err
	}

	px.gatherVMData(acc, lxc)
	px.gatherVMData(acc, qemu)

	return nil
}

func (px *Proxmox) getNodeSearchDomain() error {
	apiURL := "/nodes/" + px.NodeName + "/dns"
	jsonData, err := px.requestFunction(apiURL, http.MethodGet, nil)
	if err != nil {
		return err
	}

	var nodeDNS nodeDNS
	err = json.Unmarshal(jsonData, &nodeDNS)
	if err != nil {
		return err
	}

	if nodeDNS.Data.Searchdomain == "" {
		return errors.New("search domain is not set")
	}
	px.nodeSearchDomain = nodeDNS.Data.Searchdomain

	return nil
}

func (px *Proxmox) performRequest(apiURL, method string, data url.Values) ([]byte, error) {
	request, err := http.NewRequest(method, px.BaseURL+apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Add("Authorization", "PVEAPIToken="+px.APIToken)

	resp, err := px.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return responseBody, nil
}

func (px *Proxmox) gatherVMData(acc telegraf.Accumulator, rt resourceType) {
	vmStats, err := px.getVMStats(rt)
	if err != nil {
		px.Log.Errorf("Error getting VM stats: %v", err)
		return
	}

	for _, vmStat := range vmStats.Data {
		vmConfig, err := px.getVMConfig(vmStat.ID, rt)
		if err != nil {
			px.Log.Errorf("Error getting VM config: %v", err)
			return
		}

		if vmConfig.Data.Template == 1 {
			px.Log.Debugf("Ignoring template VM %s (%s)", vmStat.ID, vmStat.Name)
			continue
		}

		currentVMStatus, err := px.getCurrentVMStatus(rt, vmStat.ID)
		if err != nil {
			px.Log.Errorf("Error getting VM current VM status: %v", err)
			return
		}

		hostname := vmConfig.Data.Hostname
		if hostname == "" {
			hostname = vmStat.Name
		}
		domain := vmConfig.Data.Searchdomain
		if domain == "" {
			domain = px.nodeSearchDomain
		}
		fqdn := hostname + "." + domain

		tags := map[string]string{
			"node_fqdn": px.NodeName + "." + px.nodeSearchDomain,
			"vm_name":   vmStat.Name,
			"vm_fqdn":   fqdn,
			"vm_type":   string(rt),
		}
		if slices.Contains(px.AdditionalVmstatsTags, "vmid") {
			tags["vm_id"] = vmStat.ID.String()
		}
		if slices.Contains(px.AdditionalVmstatsTags, "status") {
			tags["status"] = currentVMStatus.Status
		}

		memMetrics := getByteMetrics(currentVMStatus.TotalMem, currentVMStatus.UsedMem)
		swapMetrics := getByteMetrics(currentVMStatus.TotalSwap, currentVMStatus.UsedSwap)
		diskMetrics := getByteMetrics(currentVMStatus.TotalDisk, currentVMStatus.UsedDisk)

		fields := map[string]interface{}{
			"status":               currentVMStatus.Status,
			"uptime":               jsonNumberToInt64(currentVMStatus.Uptime),
			"cpuload":              jsonNumberToFloat64(currentVMStatus.CPULoad),
			"mem_used":             memMetrics.used,
			"mem_total":            memMetrics.total,
			"mem_free":             memMetrics.free,
			"mem_used_percentage":  memMetrics.usedPercentage,
			"swap_used":            swapMetrics.used,
			"swap_total":           swapMetrics.total,
			"swap_free":            swapMetrics.free,
			"swap_used_percentage": swapMetrics.usedPercentage,
			"disk_used":            diskMetrics.used,
			"disk_total":           diskMetrics.total,
			"disk_free":            diskMetrics.free,
			"disk_used_percentage": diskMetrics.usedPercentage,
		}
		acc.AddFields("proxmox", fields, tags)
	}
}

func (px *Proxmox) getCurrentVMStatus(rt resourceType, id json.Number) (vmStat, error) {
	apiURL := "/nodes/" + px.NodeName + "/" + string(rt) + "/" + string(id) + "/status/current"

	jsonData, err := px.requestFunction(apiURL, http.MethodGet, nil)
	if err != nil {
		return vmStat{}, err
	}

	var currentVMStatus vmCurrentStats
	err = json.Unmarshal(jsonData, &currentVMStatus)
	if err != nil {
		return vmStat{}, err
	}

	return currentVMStatus.Data, nil
}

func (px *Proxmox) getVMStats(rt resourceType) (vmStats, error) {
	apiURL := "/nodes/" + px.NodeName + "/" + string(rt)
	jsonData, err := px.requestFunction(apiURL, http.MethodGet, nil)
	if err != nil {
		return vmStats{}, err
	}

	var vmStatistics vmStats
	err = json.Unmarshal(jsonData, &vmStatistics)
	if err != nil {
		return vmStats{}, err
	}

	return vmStatistics, nil
}

func (px *Proxmox) getVMConfig(vmID json.Number, rt resourceType) (vmConfig, error) {
	apiURL := "/nodes/" + px.NodeName + "/" + string(rt) + "/" + string(vmID) + "/config"
	jsonData, err := px.requestFunction(apiURL, http.MethodGet, nil)
	if err != nil {
		return vmConfig{}, err
	}

	var vmCfg vmConfig
	err = json.Unmarshal(jsonData, &vmCfg)
	if err != nil {
		return vmConfig{}, err
	}

	return vmCfg, nil
}

func getByteMetrics(total, used json.Number) metrics {
	int64Total := jsonNumberToInt64(total)
	int64Used := jsonNumberToInt64(used)
	int64Free := int64Total - int64Used
	usedPercentage := 0.0
	if int64Total != 0 {
		usedPercentage = float64(int64Used) * 100 / float64(int64Total)
	}

	return metrics{
		total:          int64Total,
		used:           int64Used,
		free:           int64Free,
		usedPercentage: usedPercentage,
	}
}

func jsonNumberToInt64(value json.Number) int64 {
	int64Value, err := value.Int64()
	if err != nil {
		return 0
	}

	return int64Value
}

func jsonNumberToFloat64(value json.Number) float64 {
	float64Value, err := value.Float64()
	if err != nil {
		return 0
	}

	return float64Value
}

func init() {
	inputs.Add("proxmox", func() telegraf.Input {
		return &Proxmox{}
	})
}
