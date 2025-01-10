package ipset

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestIpsetEntries(t *testing.T) {
	var acc testutil.Accumulator

	lines := []string{
		"create mylist hash:net family inet hashsize 16384 maxelem 131072 timeout 300 bucketsize 12 initval 0x4effa9ad",
		"add mylist 89.101.238.143 timeout 161558",
		"add mylist 122.224.15.166 timeout 186758",
		"add mylist 47.128.40.145 timeout 431559",
	}

	entries := ipsetEntries{}
	for _, line := range lines {
		require.NoError(t, entries.addLine(line, &acc))
	}
	entries.commit(&acc)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"ipset",
			map[string]string{
				"set": "mylist",
			},
			map[string]interface{}{
				"entries": 3,
				"ips":     3,
			},
			time.Unix(0, 0),
			telegraf.Gauge,
		),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestIpsetEntriesCidr(t *testing.T) {
	var acc testutil.Accumulator

	lines := []string{
		"create mylist0 hash:net family inet hashsize 16384 maxelem 131072 timeout 300 bucketsize 12 initval 0x4effa9ad",
		"add mylist0 89.101.238.143 timeout 161558",
		"add mylist0 122.224.5.0/24 timeout 186758",
		"add mylist0 47.128.40.145 timeout 431559",

		"create mylist1 hash:net family inet hashsize 16384 maxelem 131072 timeout 300 bucketsize 12 initval 0x4effa9ad",
		"add mylist1 90.101.238.143 timeout 161558",
		"add mylist1 44.128.40.145 timeout 431559",
		"add mylist1 122.224.5.0/8 timeout 186758",
		"add mylist1 45.128.40.145 timeout 431560",
	}

	entries := ipsetEntries{}
	for _, line := range lines {
		require.NoError(t, entries.addLine(line, &acc))
	}
	entries.commit(&acc)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"ipset",
			map[string]string{
				"set": "mylist0",
			},
			map[string]interface{}{
				"entries": 3,
				"ips":     256,
			},
			time.Now().Add(time.Millisecond*0),
			telegraf.Gauge,
		),
		testutil.MustMetric(
			"ipset",
			map[string]string{
				"set": "mylist1",
			},
			map[string]interface{}{
				"entries": 4,
				"ips":     16777217,
			},
			time.Unix(0, 0),
			telegraf.Gauge,
		),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}
