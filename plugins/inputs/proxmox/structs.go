package proxmox

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
)

var (
	qemu resourceType = "qemu"
	lxc  resourceType = "lxc"
)

type Proxmox struct {
	BaseURL         string          `toml:"base_url"`
	APIToken        string          `toml:"api_token"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	NodeName        string          `toml:"node_name"`
	tls.ClientConfig

	Log telegraf.Logger `toml:"-"`

	httpClient *http.Client

	nodeSearchDomain string
	requestFunction  func(px *Proxmox, apiUrl string, method string, data url.Values) ([]byte, error)
}

type resourceType string

type vmStats struct {
	Data []vmStat `json:"data"`
}

type vmCurrentStats struct {
	Data vmStat `json:"data"`
}

type vmStat struct {
	ID        json.Number `json:"vmid"`
	Name      string      `json:"name"`
	Status    string      `json:"status"`
	UsedMem   json.Number `json:"mem"`
	TotalMem  json.Number `json:"maxmem"`
	UsedDisk  json.Number `json:"disk"`
	TotalDisk json.Number `json:"maxdisk"`
	UsedSwap  json.Number `json:"swap"`
	TotalSwap json.Number `json:"maxswap"`
	Uptime    json.Number `json:"uptime"`
	CPULoad   json.Number `json:"cpu"`
}

type vmConfig struct {
	Data struct {
		Searchdomain string `json:"searchdomain"`
		Hostname     string `json:"hostname"`
		Template     int    `json:"template"`
	} `json:"data"`
}

type nodeDNS struct {
	Data struct {
		Searchdomain string `json:"search"`
	} `json:"data"`
}

type metrics struct {
	total          int64
	used           int64
	free           int64
	usedPercentage float64
}
