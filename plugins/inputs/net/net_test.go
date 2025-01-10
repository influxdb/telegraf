package net

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/inputs/system"
	"github.com/influxdata/telegraf/testutil"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/stretchr/testify/require"
)

func TestNetIOStats(t *testing.T) {
	var mps system.MockPS
	defer mps.AssertExpectations(t)

	netio := net.IOCountersStat{
		Name:        "eth0",
		BytesSent:   1123,
		BytesRecv:   8734422,
		PacketsSent: 781,
		PacketsRecv: 23456,
		Errin:       832,
		Errout:      8,
		Dropin:      7,
		Dropout:     1,
	}

	mps.On("NetIO").Return([]net.IOCountersStat{netio}, nil)

	netprotos := []net.ProtoCountersStat{
		{
			Protocol: "Udp",
			Stats: map[string]int64{
				"InDatagrams": 4655,
				"NoPorts":     892592,
			},
		},
	}
	mps.On("NetProto").Return(netprotos, nil)

	t.Setenv("HOST_SYS", filepath.Join("testdata", "general", "sys"))

	plugin := &Net{ps: &mps, skipChecks: true}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))

	expected := []telegraf.Metric{
		metric.New(
			"net",
			map[string]string{"interface": "eth0"},
			map[string]interface{}{
				"bytes_sent":   uint64(1123),
				"bytes_recv":   uint64(8734422),
				"packets_sent": uint64(781),
				"packets_recv": uint64(23456),
				"err_in":       uint64(832),
				"err_out":      uint64(8),
				"drop_in":      uint64(7),
				"drop_out":     uint64(1),
				"speed":        int64(100),
			},
			time.Unix(0, 0),
			telegraf.Counter,
		),
		metric.New(
			"net",
			map[string]string{"interface": "all"},
			map[string]interface{}{
				"udp_noports":     int64(892592),
				"udp_indatagrams": int64(4655),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestNetIOStatsSpeedUnsupported(t *testing.T) {
	var mps system.MockPS
	defer mps.AssertExpectations(t)

	netio := net.IOCountersStat{
		Name:        "eth1",
		BytesSent:   1123,
		BytesRecv:   8734422,
		PacketsSent: 781,
		PacketsRecv: 23456,
		Errin:       832,
		Errout:      8,
		Dropin:      7,
		Dropout:     1,
	}

	mps.On("NetIO").Return([]net.IOCountersStat{netio}, nil)

	netprotos := []net.ProtoCountersStat{
		{
			Protocol: "Udp",
			Stats: map[string]int64{
				"InDatagrams": 4655,
				"NoPorts":     892592,
			},
		},
	}
	mps.On("NetProto").Return(netprotos, nil)

	t.Setenv("HOST_SYS", filepath.Join("testdata", "general", "sys"))

	plugin := &Net{ps: &mps, skipChecks: true}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))

	expected := []telegraf.Metric{
		metric.New(
			"net",
			map[string]string{"interface": "eth1"},
			map[string]interface{}{
				"bytes_sent":   uint64(1123),
				"bytes_recv":   uint64(8734422),
				"packets_sent": uint64(781),
				"packets_recv": uint64(23456),
				"err_in":       uint64(832),
				"err_out":      uint64(8),
				"drop_in":      uint64(7),
				"drop_out":     uint64(1),
				"speed":        int64(-1),
			},
			time.Unix(0, 0),
			telegraf.Counter,
		),
		metric.New(
			"net",
			map[string]string{"interface": "all"},
			map[string]interface{}{
				"udp_noports":     int64(892592),
				"udp_indatagrams": int64(4655),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestNetIOStatsNoSpeedFile(t *testing.T) {
	var mps system.MockPS
	defer mps.AssertExpectations(t)

	netio := net.IOCountersStat{
		Name:        "eth2",
		BytesSent:   1123,
		BytesRecv:   8734422,
		PacketsSent: 781,
		PacketsRecv: 23456,
		Errin:       832,
		Errout:      8,
		Dropin:      7,
		Dropout:     1,
	}

	mps.On("NetIO").Return([]net.IOCountersStat{netio}, nil)

	netprotos := []net.ProtoCountersStat{
		{
			Protocol: "Udp",
			Stats: map[string]int64{
				"InDatagrams": 4655,
				"NoPorts":     892592,
			},
		},
	}
	mps.On("NetProto").Return(netprotos, nil)

	t.Setenv("HOST_SYS", filepath.Join("testdata", "general", "sys"))

	plugin := &Net{ps: &mps, skipChecks: true}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))

	expected := []telegraf.Metric{
		metric.New(
			"net",
			map[string]string{"interface": "eth2"},
			map[string]interface{}{
				"bytes_sent":   uint64(1123),
				"bytes_recv":   uint64(8734422),
				"packets_sent": uint64(781),
				"packets_recv": uint64(23456),
				"err_in":       uint64(832),
				"err_out":      uint64(8),
				"drop_in":      uint64(7),
				"drop_out":     uint64(1),
				"speed":        int64(-1),
			},
			time.Unix(0, 0),
			telegraf.Counter,
		),
		metric.New(
			"net",
			map[string]string{"interface": "all"},
			map[string]interface{}{
				"udp_noports":     int64(892592),
				"udp_indatagrams": int64(4655),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}
