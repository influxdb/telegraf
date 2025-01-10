package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/selfstat"
	"github.com/influxdata/telegraf/testutil"
)

func TestSelfPlugin(t *testing.T) {
	s := Internal{
		CollectMemstats: true,
	}
	acc := &testutil.Accumulator{}

	require.NoError(t, s.Gather(acc))
	require.True(t, acc.HasMeasurement("internal_memstats"))

	// test that a registered stat is incremented
	stat := selfstat.Register("mytest", "test", map[string]string{"test": "foo"})
	stat.Incr(1)
	stat.Incr(2)
	require.NoError(t, s.Gather(acc))

	acc.AssertContainsTaggedFields(t, "internal_mytest",
		map[string]interface{}{
			"test": int64(3),
		},
		map[string]string{
			"test":    "foo",
			"version": "unknown",
		},
	)
	acc.ClearMetrics()

	// test that a registered stat is set properly
	stat.Set(101)
	require.NoError(t, s.Gather(acc))
	acc.AssertContainsTaggedFields(t, "internal_mytest",
		map[string]interface{}{
			"test": int64(101),
		},
		map[string]string{
			"test":    "foo",
			"version": "unknown",
		},
	)
	acc.ClearMetrics()

	// test that regular and timing stats can share the same measurement, and
	// that timings are set properly.
	timing := selfstat.RegisterTiming("mytest", "test_ns", map[string]string{"test": "foo"})
	timing.Incr(100)
	timing.Incr(200)
	require.NoError(t, s.Gather(acc))
	acc.AssertContainsTaggedFields(t, "internal_mytest",
		map[string]interface{}{
			"test":    int64(101),
			"test_ns": int64(150),
		},
		map[string]string{
			"test":    "foo",
			"version": "unknown",
		},
	)
}

func TestNoMemStat(t *testing.T) {
	s := Internal{
		CollectMemstats: false,
		CollectGostats:  false,
	}
	acc := &testutil.Accumulator{}

	require.NoError(t, s.Gather(acc))
	require.False(t, acc.HasMeasurement("internal_memstats"))
	require.False(t, acc.HasMeasurement("internal_gostats"))
}

func TestGostats(t *testing.T) {
	s := Internal{
		CollectMemstats: false,
		CollectGostats:  true,
	}
	acc := &testutil.Accumulator{}

	require.NoError(t, s.Gather(acc))
	require.False(t, acc.HasMeasurement("internal_memstats"))
	require.True(t, acc.HasMeasurement("internal_gostats"))

	var metric *testutil.Metric
	for _, m := range acc.Metrics {
		if m.Measurement == "internal_gostats" {
			metric = m
			break
		}
	}

	require.NotNil(t, metric)
	require.Equal(t, "internal_gostats", metric.Measurement)
	require.Len(t, metric.Tags, 1)
	require.Contains(t, metric.Tags, "go_version")

	for name, value := range metric.Fields {
		switch value.(type) {
		case int64, uint64, float64:
		default:
			require.Truef(t, false, "field %s is of non-numeric type %T\n", name, value)
		}
	}
}
