//go:generate ../../../tools/readme_config_includer/generator
//go:build !linux

package lustre2

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Lustre2 struct {
	Log telegraf.Logger `toml:"-"`
}

func (*Lustre2) SampleConfig() string { return sampleConfig }

func (l *Lustre2) Init() error {
	l.Log.Warn("Current platform is not supported")
	return nil
}

func (*Lustre2) Gather(_ telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("lustre2", func() telegraf.Input {
		return &Lustre2{}
	})
}
