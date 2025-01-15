//go:generate ../../../tools/readme_config_includer/generator
//go:build !linux || (linux && !386 && !amd64 && !arm && !arm64)

package ras

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Ras struct {
	Log telegraf.Logger `toml:"-"`
}

func (*Ras) SampleConfig() string { return sampleConfig }

func (r *Ras) Init() error {
	r.Log.Warn("Current platform is not supported")
	return nil
}

func (*Ras) Gather(telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("ras", func() telegraf.Input {
		return &Ras{}
	})
}
