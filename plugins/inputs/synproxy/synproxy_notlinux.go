//go:generate ../../../tools/readme_config_includer/generator
//go:build !linux

package synproxy

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Synproxy struct {
	Log telegraf.Logger `toml:"-"`
}

func (*Synproxy) SampleConfig() string { return sampleConfig }

func (s *Synproxy) Init() error {
	s.Log.Warn("Current platform is not supported")
	return nil
}

func (*Synproxy) Gather(telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("slab", func() telegraf.Input {
		return &Synproxy{}
	})
}
