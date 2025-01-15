//go:generate ../../../tools/readme_config_includer/generator
//go:build !linux

package systemd_units

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type SystemdUnits struct {
	Log telegraf.Logger `toml:"-"`
}

func (*SystemdUnits) SampleConfig() string { return sampleConfig }

func (s *SystemdUnits) Init() error {
	s.Log.Warn("Current platform is not supported")
	return nil
}

func (*SystemdUnits) Gather(telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("systemd_units", func() telegraf.Input {
		return &SystemdUnits{}
	})
}
