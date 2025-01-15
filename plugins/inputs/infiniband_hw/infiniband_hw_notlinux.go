//go:build !linux

package infiniband_hw

import (
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

func (i *InfinibandHW) Init() error {
	i.Log.Warn("Current platform is not supported")
	return nil
}

func (*InfinibandHW) Gather(_ telegraf.Accumulator) error {
	return nil
}

func init() {
	inputs.Add("infiniband_hw", func() telegraf.Input {
		return &InfinibandHW{}
	})
}
