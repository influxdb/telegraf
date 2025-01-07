package shim

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestBatchMetricsAdd(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.RWMutex

	bm := &batchMetrics{
		metrics: make([]telegraf.Metric, 0),
		wg:      &wg,
		mu:      &mu,
	}

	metric := testutil.MustMetric("test_metric", map[string]string{"tag1": "value1"}, map[string]interface{}{"field1": 1}, time.Unix(0, 0))

	bm.add(metric)

	testutil.RequireMetricsEqual(t, []telegraf.Metric{metric}, bm.metrics, testutil.IgnoreTime())
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

	metric1 := testutil.MustMetric("metric1", nil, map[string]interface{}{"field1": 1}, time.Unix(0, 0))
	metric2 := testutil.MustMetric("metric2", nil, map[string]interface{}{"field2": 2}, time.Unix(0, 0))

	bm.add(metric1)
	bm.add(metric2)

	require.Len(t, bm.metrics, 2)
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

	metric := testutil.MustMetric("test_metric", nil, map[string]interface{}{"field1": 1}, time.Unix(0, 0))
	bm.add(metric)

	require.Len(t, bm, 1)
}
