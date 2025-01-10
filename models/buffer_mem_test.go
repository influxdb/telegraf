package models

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf/metric"
	"github.com/stretchr/testify/require"
)

func TestMemoryBufferAcceptCallsMetricAccept(t *testing.T) {
	buf, err := NewBuffer("test", "123", "", 5, "memory", "")
	require.NoError(t, err)
	buf.Stats().MetricsAdded.Set(0)
	buf.Stats().MetricsWritten.Set(0)
	buf.Stats().MetricsDropped.Set(0)
	defer buf.Close()

	var accept int
	mm := &mockMetric{
		Metric: metric.New("cpu", map[string]string{}, map[string]interface{}{"value": 42.0}, time.Unix(0, 0)),
		AcceptF: func() {
			accept++
		},
	}
	buf.Add(mm, mm, mm)
	tx := buf.BeginTransaction(2)
	tx.AcceptAll()
	buf.EndTransaction(tx)
	require.Equal(t, 2, accept)
}

func BenchmarkMemoryBufferAddMetrics(b *testing.B) {
	buf, err := NewBuffer("test", "123", "", 10000, "memory", "")
	require.NoError(b, err)
	buf.Stats().MetricsAdded.Set(0)
	buf.Stats().MetricsWritten.Set(0)
	buf.Stats().MetricsDropped.Set(0)
	defer buf.Close()

	m := metric.New("cpu", map[string]string{}, map[string]interface{}{"value": 42.0}, time.Unix(0, 0))
	for n := 0; n < b.N; n++ {
		buf.Add(m)
	}
}
