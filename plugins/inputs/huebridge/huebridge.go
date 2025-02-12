//go:generate ../../../tools/readme_config_includer/generator
package huebridge

import (
	_ "embed"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type RemoteClientConfig struct {
	RemoteClientId     string `toml:"remote_client_id"`
	RemoteClientSecret string `toml:"remote_client_secret"`
	RemoteCallbackUrl  string `toml:"remote_callback_url"`
	RemoteTokenDir     string `toml:"remote_token_dir"`
}

type HueBridge struct {
	BridgeUrls      []string          `toml:"bridges"`
	RoomAssignments map[string]string `toml:"room_assignments"`
	Timeout         config.Duration   `toml:"timeout"`
	Log             telegraf.Logger   `toml:"-"`
	RemoteClientConfig
	tls.ClientConfig

	bridges []*bridge
}

func (*HueBridge) SampleConfig() string {
	return sampleConfig
}

func (h *HueBridge) Init() error {
	h.bridges = make([]*bridge, 0, len(h.BridgeUrls))
	for _, bridgeUrl := range h.BridgeUrls {
		bridge, err := newBridge(bridgeUrl, h.RoomAssignments, &h.RemoteClientConfig, &h.ClientConfig, h.Timeout, h.Log)
		if err != nil {
			h.Log.Warnf("Failed to instantiate bridge for URL %s: %s", bridgeUrl, err)
			continue
		}
		h.bridges = append(h.bridges, bridge)
	}
	return nil
}

func (h *HueBridge) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	for _, bridge := range h.bridges {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc.AddError(bridge.process(acc))
		}()
	}
	wg.Wait()
	return nil
}

func init() {
	inputs.Add("huebridge", func() telegraf.Input {
		return &HueBridge{Timeout: config.Duration(10 * time.Second)}
	})
}
