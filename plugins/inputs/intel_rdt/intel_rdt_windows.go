//go:build windows

package intel_rdt

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type IntelRDT struct {
	Log telegraf.Logger `toml:"-"`
}

func (i *IntelRDT) Init() error {
	i.Log.Warn("current platform is not supported")
	return nil
}
func (i *IntelRDT) SampleConfig() string                { return sampleConfig }
func (i *IntelRDT) Gather(_ telegraf.Accumulator) error { return nil }
func (i *IntelRDT) Start(_ telegraf.Accumulator) error  { return nil }
func (i *IntelRDT) Stop()                               {}

func init() {
	inputs.Add("intel_rdt", func() telegraf.Input {
		return &IntelRDT{}
	})
}
