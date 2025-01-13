package health_test

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/outputs/health"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestTimeBetweenMetricsFieldFound(t *testing.T) {
	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			time.Now()),
	}

	time_between := &health.TimeBetweenMetrics{
		Field:                 "time_idle",
		MaxTimeBetweenMetrics: config.Duration(1.0),
	}

	time_between.Init()
	require.True(t, time_between.WaitingForFirstMetric)
	time_between.Process(metrics)
	require.False(t, time_between.WaitingForFirstMetric)
}

func TestTimeBetweenMetricsFieldIgnore(t *testing.T) {
	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"not_the_field": 42.0,
			},
			time.Now()),
	}

	time_between := &health.TimeBetweenMetrics{
		Field:                 "time_idle",
		MaxTimeBetweenMetrics: config.Duration(1.0),
	}

	time_between.Init()
	require.True(t, time_between.WaitingForFirstMetric)
	time_between.Process(metrics)
	require.True(t, time_between.WaitingForFirstMetric)
}

func TestTimeBetweenMetricsLatestTimestampSaved(t *testing.T) {
	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			time.Time{}.AddDate(2000, 0, 0)),
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 43.0,
			},
			time.Time{}.AddDate(2001, 0, 0)),
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 44.0,
			},
			time.Time{}.AddDate(2002, 0, 0)),
	}

	time_between := &health.TimeBetweenMetrics{
		Field:                 "time_idle",
		MaxTimeBetweenMetrics: config.Duration(1.0),
	}

	time_between.Init()
	require.Equal(t, time_between.LatestMetricTimestamp.Compare(time.Time{}), 0)
	time_between.Process(metrics)
	require.Equal(t, time_between.LatestMetricTimestamp.Compare(time.Time{}.AddDate(2002, 0, 0)), 0)
}

func TestTimeBetweenMetricsCheckHealthy(t *testing.T) {
	timestamp := time.Time{}
	metrics := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"time_idle": 42.0,
			},
			timestamp),
	}
	time_between := &health.TimeBetweenMetrics{
		Field:                 "time_idle",
		MaxTimeBetweenMetrics: config.Duration(48 * time.Hour),
	}

	time_between.Init()
	time_between.Process(metrics)
	require.Equal(t, time_between.LatestMetricTimestamp.Compare(time.Time{}), 0)
	require.True(t, time_between.Check(timestamp.AddDate(0, 0, 1)))
	require.False(t, time_between.Check(timestamp.AddDate(0, 0, 3)))
}

func TestTimeBetweenMetricsHealthyBeforeMessage(t *testing.T) {
	time_between := &health.TimeBetweenMetrics{
		Field:                 "time_idle",
		MaxTimeBetweenMetrics: config.Duration(1.0),
	}
	time_between.Init()
	require.True(t, time_between.Check(time.Time{}))
}
