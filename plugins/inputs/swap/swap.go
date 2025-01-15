//go:generate ../../../tools/readme_config_includer/generator
package swap

import (
	_ "embed"
	"fmt"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/system"
)

//go:embed sample.conf
var sampleConfig string

type Swap struct {
	ps system.PS
}

func (*Swap) SampleConfig() string {
	return sampleConfig
}

func (ss *Swap) Gather(acc telegraf.Accumulator) error {
	swap, err := ss.ps.SwapStat()
	if err != nil {
		return fmt.Errorf("error getting swap memory info: %w", err)
	}

	fieldsG := map[string]interface{}{
		"total":        swap.Total,
		"used":         swap.Used,
		"free":         swap.Free,
		"used_percent": swap.UsedPercent,
	}
	fieldsC := map[string]interface{}{
		"in":  swap.Sin,
		"out": swap.Sout,
	}
	acc.AddGauge("swap", fieldsG, nil)
	acc.AddCounter("swap", fieldsC, nil)

	return nil
}

func init() {
	ps := system.NewSystemPS()
	inputs.Add("swap", func() telegraf.Input {
		return &Swap{ps: ps}
	})
}
