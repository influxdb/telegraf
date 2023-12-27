//go:build linux

package psi

import (
	"testing"

	"github.com/influxdata/telegraf/testutil"
	"github.com/prometheus/procfs"
	"github.com/stretchr/testify/require"
)

func TestPSIStats(t *testing.T) {
	var (
		psi *Psi
		err error
		acc testutil.Accumulator
	)

	mockPressureSome := &procfs.PSILine{
		Avg10:  10,
		Avg60:  60,
		Avg300: 300,
		Total:  114514,
	}
	mockPressureFull := &procfs.PSILine{
		Avg10:  1,
		Avg60:  6,
		Avg300: 30,
		Total:  11451,
	}
	mockPSIStats := procfs.PSIStats{
		Some: mockPressureSome,
		Full: mockPressureFull,
	}
	mockStats := map[string]procfs.PSIStats{
		"cpu":    mockPSIStats,
		"memory": mockPSIStats,
		"io":     mockPSIStats,
	}

	err = psi.Gather(&acc)
	require.NoError(t, err)

	pressureFields := map[string]map[string]interface{}{
		"some": {
			"avg10":  float64(10),
			"avg60":  float64(60),
			"avg300": float64(300),
		},
		"full": {
			"avg10":  float64(1),
			"avg60":  float64(6),
			"avg300": float64(30),
		},
	}
	pressureTotalFields := map[string]map[string]interface{}{
		"some": {
			"total": uint64(114514),
		},
		"full": {
			"total": uint64(11451),
		},
	}

	acc.ClearMetrics()
	psi.uploadPressure(mockStats, &acc)
	for _, typ := range []string{"some", "full"} {
		for _, resource := range []string{"cpu", "memory", "io"} {
			if resource == "cpu" && typ == "full" {
				continue
			}

			tags := map[string]string{
				"resource": resource,
				"type":     typ,
			}

			acc.AssertContainsTaggedFields(t, "pressure", pressureFields[typ], tags)
			acc.AssertContainsTaggedFields(t, "pressureTotal", pressureTotalFields[typ], tags)

			// "pressure" and "pressureTotal" should contain disjoint set of fields
			acc.AssertDoesNotContainsTaggedFields(t, "pressure", pressureTotalFields[typ], tags)
			acc.AssertDoesNotContainsTaggedFields(t, "pressureTotal", pressureFields[typ], tags)
		}
	}

	// The combination "resource=cpu,type=full" should NOT appear anywhere
	forbiddenTags := map[string]string{
		"resource": "cpu",
		"type":     "full",
	}
	acc.AssertDoesNotContainsTaggedFields(t, "pressure", pressureFields["full"], forbiddenTags)
	acc.AssertDoesNotContainsTaggedFields(t, "pressureTotal", pressureTotalFields["full"], forbiddenTags)
}
