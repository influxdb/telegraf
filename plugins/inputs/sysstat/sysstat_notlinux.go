//go:generate ../../../tools/readme_config_includer/generator
//go:build !linux

package sysstat

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Sysstat struct {
	Log telegraf.Logger `toml:"-"`
}

func (*Sysstat) SampleConfig() string { return sampleConfig }

func (s *Sysstat) Init() error {
	s.Log.Warn("Current platform is not supported")
	return nil
}

func (*Sysstat) Gather(telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("sysstat", func() telegraf.Input {
		return &Sysstat{}
	})
}
