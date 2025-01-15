package swap

import (
	"testing"

	"github.com/influxdata/telegraf/plugins/inputs/system"
	"github.com/influxdata/telegraf/testutil"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/stretchr/testify/require"
)

func TestSwapStats(t *testing.T) {
	var mps system.MockPS
	var err error
	defer mps.AssertExpectations(t)
	var acc testutil.Accumulator

	sms := &mem.SwapMemoryStat{
		Total:       8123,
		Used:        1232,
		Free:        6412,
		UsedPercent: 12.2,
		Sin:         7,
		Sout:        830,
	}

	mps.On("SwapStat").Return(sms, nil)

	err = (&Swap{&mps}).Gather(&acc)
	require.NoError(t, err)

	swapfields := map[string]interface{}{
		"total":        uint64(8123),
		"used":         uint64(1232),
		"used_percent": float64(12.2),
		"free":         uint64(6412),
	}
	acc.AssertContainsTaggedFields(t, "swap", swapfields, make(map[string]string))
}
