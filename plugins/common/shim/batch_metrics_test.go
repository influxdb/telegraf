package shim

import (
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestBatchMetricsAdd(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.RWMutex

	bm := &batchMetrics{
		metrics: make([]telegraf.Metric, 0),
		wg:      &wg,
		mu:      &mu,
	}

	metric := testutil.TestMetric(101, "metric1")

	bm.add(metric)

	testutil.RequireMetricsEqual(t, []telegraf.Metric{metric}, bm.metrics)
}

func TestBatchMetricsClear(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.RWMutex

	wg.Add(2)

	bm := &batchMetrics{
		metrics: make([]telegraf.Metric, 0),
		wg:      &wg,
		mu:      &mu,
	}

	metric1 := testutil.TestMetric(101, "metric1")
	metric2 := testutil.TestMetric(102, "metric2")

	bm.add(metric1)
	bm.add(metric2)

	require.Len(t, bm.metrics, 2)
	bm.clear()

	require.Empty(t, bm.metrics)
}

func TestBatchMetricsLen(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.RWMutex

	bm := &batchMetrics{
		metrics: make([]telegraf.Metric, 0),
		wg:      &wg,
		mu:      &mu,
	}

	require.Empty(t, bm.metrics)

	metric := testutil.TestMetric(101, "metric1")
	bm.add(metric)

	require.Len(t, bm.metrics, 1)
}
