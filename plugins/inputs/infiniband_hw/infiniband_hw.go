//go:generate ../../../tools/readme_config_includer/generator
package infiniband_hw

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type InfinibandHW struct {
	Log telegraf.Logger `toml:"-"`
}

func (*InfinibandHW) SampleConfig() string {
	return sampleConfig
}

// Initialise plugin
func init() {
	inputs.Add("infiniband_hw", func() telegraf.Input { return &InfinibandHW{} })
}
