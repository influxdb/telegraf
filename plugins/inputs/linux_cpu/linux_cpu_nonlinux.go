//go:build !linux

package linux_cpu

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type LinuxCPU struct {
	Log telegraf.Logger `toml:"-"`
}

func (*LinuxCPU) SampleConfig() string { return sampleConfig }

func (l *LinuxCPU) Init() error {
	l.Log.Warn("Current platform is not supported")
	return nil
}

func (*LinuxCPU) Gather(_ telegraf.Accumulator) error { return nil }

func init() {
	inputs.Add("linux_cpu", func() telegraf.Input {
		return &LinuxCPU{}
	})
}
