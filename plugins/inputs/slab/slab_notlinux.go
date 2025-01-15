//go:generate ../../../tools/readme_config_includer/generator
//go:build !linux

package slab

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Slab struct {
	Log telegraf.Logger `toml:"-"`
}

func (*Slab) SampleConfig() string { return sampleConfig }

func (s *Slab) Init() error {
	s.Log.Warn("Current platform is not supported")
	return nil
}

func (*Slab) Gather(telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("slab", func() telegraf.Input {
		return &Slab{}
	})
}
