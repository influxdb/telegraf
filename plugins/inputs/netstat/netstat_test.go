package netstat

import (
	"syscall"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/inputs/system"
	"github.com/influxdata/telegraf/testutil"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/stretchr/testify/require"
)

func TestNetStats(t *testing.T) {
	var mps system.MockPS
	defer mps.AssertExpectations(t)
	mps.On("NetConnections").Return([]net.ConnectionStat{
		{
			Type: syscall.SOCK_DGRAM,
		},
		{
			Status: "ESTABLISHED",
		},
		{
			Status: "ESTABLISHED",
		},
		{
			Status: "CLOSE",
		},
	}, nil)

	var acc testutil.Accumulator
	require.NoError(t, (&NetStat{ps: &mps}).Gather(&acc))

	expected := []telegraf.Metric{
		metric.New(
			"netstat",
			map[string]string{},
			map[string]interface{}{
				"tcp_established": 2,
				"tcp_syn_sent":    0,
				"tcp_syn_recv":    0,
				"tcp_fin_wait1":   0,
				"tcp_fin_wait2":   0,
				"tcp_time_wait":   0,
				"tcp_close":       1,
				"tcp_close_wait":  0,
				"tcp_last_ack":    0,
				"tcp_listen":      0,
				"tcp_closing":     0,
				"tcp_none":        0,
				"udp_socket":      1,
			},
			time.Unix(0, 0),
		),
	}

	testutil.RequireMetricsEqual(t,
		expected,
		acc.GetTelegrafMetrics(),
		testutil.IgnoreTime(),
	)
}
