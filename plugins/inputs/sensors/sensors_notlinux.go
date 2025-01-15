//go:generate ../../../tools/readme_config_includer/generator
//go:build !linux

package sensors

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Sensors struct {
	Log telegraf.Logger `toml:"-"`
}

func (*Sensors) SampleConfig() string { return sampleConfig }

func (s *Sensors) Init() error {
	s.Log.Warn("Current platform is not supported")
	return nil
}

func (*Sensors) Gather(telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("sensors", func() telegraf.Input {
		return &Sensors{}
	})
}
