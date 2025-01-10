//go:build windows

package postfix

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Postfix struct {
	Log telegraf.Logger `toml:"-"`
}

func (*Postfix) SampleConfig() string { return sampleConfig }

func (p *Postfix) Init() error {
	p.Log.Warn("Current platform is not supported")
	return nil
}

func (*Postfix) Gather(_ telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("postfix", func() telegraf.Input {
		return &Postfix{}
	})
}
