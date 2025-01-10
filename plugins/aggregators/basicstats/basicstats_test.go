package basicstats

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

var m1 = metric.New("m1",
	map[string]string{"foo": "bar"},
	map[string]interface{}{
		"a": int64(1),
		"b": int64(1),
		"c": float64(2),
		"d": float64(2),
		"g": int64(3),
	},
	time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
)
var m2 = metric.New("m1",
	map[string]string{"foo": "bar"},
	map[string]interface{}{
		"a":        int64(1),
		"b":        int64(3),
		"c":        float64(4),
		"d":        float64(6),
		"e":        float64(200),
		"f":        uint64(200),
		"ignoreme": "string",
		"andme":    true,
		"g":        int64(1),
	},
	time.Date(2000, 1, 1, 0, 0, 0, 1e6, time.UTC),
)

func BenchmarkApply(b *testing.B) {
	minmax := NewBasicStats()
	minmax.Log = testutil.Logger{}
	minmax.initConfiguredStats()

	for n := 0; n < b.N; n++ {
		minmax.Add(m1)
		minmax.Add(m2)
	}
}

// Test two metrics getting added.
func TestBasicStatsWithPeriod(t *testing.T) {
	acc := testutil.Accumulator{}
	minmax := NewBasicStats()
	minmax.Log = testutil.Logger{}
	minmax.initConfiguredStats()

	minmax.Add(m1)
	minmax.Add(m2)
	minmax.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_count": float64(2), // a
		"a_max":   float64(1),
		"a_min":   float64(1),
		"a_mean":  float64(1),
		"a_stdev": float64(0),
		"a_s2":    float64(0),
		"b_count": float64(2), // b
		"b_max":   float64(3),
		"b_min":   float64(1),
		"b_mean":  float64(2),
		"b_s2":    float64(2),
		"b_stdev": math.Sqrt(2),
		"c_count": float64(2), // c
		"c_max":   float64(4),
		"c_min":   float64(2),
		"c_mean":  float64(3),
		"c_s2":    float64(2),
		"c_stdev": math.Sqrt(2),
		"d_count": float64(2), // d
		"d_max":   float64(6),
		"d_min":   float64(2),
		"d_mean":  float64(4),
		"d_s2":    float64(8),
		"d_stdev": math.Sqrt(8),
		"e_count": float64(1), // e
		"e_max":   float64(200),
		"e_min":   float64(200),
		"e_mean":  float64(200),
		"f_count": float64(1), // f
		"f_max":   float64(200),
		"f_min":   float64(200),
		"f_mean":  float64(200),
		"g_count": float64(2), // g
		"g_max":   float64(3),
		"g_min":   float64(1),
		"g_mean":  float64(2),
		"g_s2":    float64(2),
		"g_stdev": math.Sqrt(2),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test two metrics getting added with a push/reset in between (simulates
// getting added in different periods.)
func TestBasicStatsDifferentPeriods(t *testing.T) {
	acc := testutil.Accumulator{}
	minmax := NewBasicStats()
	minmax.Stats = []string{"count", "max", "min", "mean", "last", "first"}
	minmax.Log = testutil.Logger{}
	minmax.initConfiguredStats()

	minmax.Add(m1)
	minmax.Push(&acc)
	expectedFields := map[string]interface{}{
		"a_count": float64(1), // a
		"a_max":   float64(1),
		"a_min":   float64(1),
		"a_mean":  float64(1),
		"a_last":  float64(1),
		"a_first": float64(1),
		"b_count": float64(1), // b
		"b_max":   float64(1),
		"b_min":   float64(1),
		"b_mean":  float64(1),
		"b_last":  float64(1),
		"b_first": float64(1),
		"c_count": float64(1), // c
		"c_max":   float64(2),
		"c_min":   float64(2),
		"c_mean":  float64(2),
		"c_last":  float64(2),
		"c_first": float64(2),
		"d_count": float64(1), // d
		"d_max":   float64(2),
		"d_min":   float64(2),
		"d_mean":  float64(2),
		"d_last":  float64(2),
		"d_first": float64(2),
		"g_count": float64(1), // g
		"g_max":   float64(3),
		"g_min":   float64(3),
		"g_mean":  float64(3),
		"g_last":  float64(3),
		"g_first": float64(3),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)

	acc.ClearMetrics()
	minmax.Reset()
	minmax.Add(m2)
	minmax.Push(&acc)
	expectedFields = map[string]interface{}{
		"a_count": float64(1), // a
		"a_max":   float64(1),
		"a_min":   float64(1),
		"a_mean":  float64(1),
		"a_last":  float64(1),
		"a_first": float64(1),
		"b_count": float64(1), // b
		"b_max":   float64(3),
		"b_min":   float64(3),
		"b_mean":  float64(3),
		"b_last":  float64(3),
		"b_first": float64(3),
		"c_count": float64(1), // c
		"c_max":   float64(4),
		"c_min":   float64(4),
		"c_mean":  float64(4),
		"c_last":  float64(4),
		"c_first": float64(4),
		"d_count": float64(1), // d
		"d_max":   float64(6),
		"d_min":   float64(6),
		"d_mean":  float64(6),
		"d_last":  float64(6),
		"d_first": float64(6),
		"e_count": float64(1), // e
		"e_max":   float64(200),
		"e_min":   float64(200),
		"e_mean":  float64(200),
		"e_last":  float64(200),
		"e_first": float64(200),
		"f_count": float64(1), // f
		"f_max":   float64(200),
		"f_min":   float64(200),
		"f_mean":  float64(200),
		"f_last":  float64(200),
		"f_first": float64(200),
		"g_count": float64(1), // g
		"g_max":   float64(1),
		"g_min":   float64(1),
		"g_mean":  float64(1),
		"g_last":  float64(1),
		"g_first": float64(1),
	}
	expectedTags = map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating count
func TestBasicStatsWithOnlyCount(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"count"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_count": float64(2),
		"b_count": float64(2),
		"c_count": float64(2),
		"d_count": float64(2),
		"e_count": float64(1),
		"f_count": float64(1),
		"g_count": float64(2),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating minimum
func TestBasicStatsWithOnlyMin(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"min"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_min": float64(1),
		"b_min": float64(1),
		"c_min": float64(2),
		"d_min": float64(2),
		"e_min": float64(200),
		"f_min": float64(200),
		"g_min": float64(1),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating maximum
func TestBasicStatsWithOnlyMax(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"max"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_max": float64(1),
		"b_max": float64(3),
		"c_max": float64(4),
		"d_max": float64(6),
		"e_max": float64(200),
		"f_max": float64(200),
		"g_max": float64(3),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating mean
func TestBasicStatsWithOnlyMean(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"mean"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_mean": float64(1),
		"b_mean": float64(2),
		"c_mean": float64(3),
		"d_mean": float64(4),
		"e_mean": float64(200),
		"f_mean": float64(200),
		"g_mean": float64(2),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating sum
func TestBasicStatsWithOnlySum(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"sum"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_sum": float64(2),
		"b_sum": float64(4),
		"c_sum": float64(6),
		"d_sum": float64(8),
		"e_sum": float64(200),
		"f_sum": float64(200),
		"g_sum": float64(4),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Verify that sum doesn't suffer from floating point errors.  Early
// implementations of sum were calculated from mean and count, which
// e.g. summed "1, 1, 5, 1" as "7.999999..." instead of 8.
func TestBasicStatsWithOnlySumFloatingPointErrata(t *testing.T) {
	var sum1 = metric.New("m1",
		map[string]string{},
		map[string]interface{}{
			"a": int64(1),
		},
		time.Now(),
	)
	var sum2 = metric.New("m1",
		map[string]string{},
		map[string]interface{}{
			"a": int64(1),
		},
		time.Now(),
	)
	var sum3 = metric.New("m1",
		map[string]string{},
		map[string]interface{}{
			"a": int64(5),
		},
		time.Now(),
	)
	var sum4 = metric.New("m1",
		map[string]string{},
		map[string]interface{}{
			"a": int64(1),
		},
		time.Now(),
	)

	aggregator := NewBasicStats()
	aggregator.Stats = []string{"sum"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(sum1)
	aggregator.Add(sum2)
	aggregator.Add(sum3)
	aggregator.Add(sum4)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_sum": float64(8),
	}
	expectedTags := map[string]string{}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating variance
func TestBasicStatsWithOnlyVariance(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"s2"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_s2": float64(0),
		"b_s2": float64(2),
		"c_s2": float64(2),
		"d_s2": float64(8),
		"g_s2": float64(2),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating standard deviation
func TestBasicStatsWithOnlyStandardDeviation(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"stdev"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_stdev": float64(0),
		"b_stdev": math.Sqrt(2),
		"c_stdev": math.Sqrt(2),
		"d_stdev": math.Sqrt(8),
		"g_stdev": math.Sqrt(2),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating minimum and maximum
func TestBasicStatsWithMinAndMax(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"min", "max"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_max": float64(1), // a
		"a_min": float64(1),
		"b_max": float64(3), // b
		"b_min": float64(1),
		"c_max": float64(4), // c
		"c_min": float64(2),
		"d_max": float64(6), // d
		"d_min": float64(2),
		"e_max": float64(200), // e
		"e_min": float64(200),
		"f_max": float64(200), // f
		"f_min": float64(200),
		"g_max": float64(3), // g
		"g_min": float64(1),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating diff
func TestBasicStatsWithDiff(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"diff"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_diff": float64(0),
		"b_diff": float64(2),
		"c_diff": float64(2),
		"d_diff": float64(4),
		"g_diff": float64(-2),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

func TestBasicStatsWithRate(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"rate"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)
	expectedFields := map[string]interface{}{
		"a_rate": float64(0),
		"b_rate": float64(2000),
		"c_rate": float64(2000),
		"d_rate": float64(4000),
		"g_rate": float64(-2000),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

func TestBasicStatsWithNonNegativeRate(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"non_negative_rate"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_non_negative_rate": float64(0),
		"b_non_negative_rate": float64(2000),
		"c_non_negative_rate": float64(2000),
		"d_non_negative_rate": float64(4000),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

func TestBasicStatsWithPctChange(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"percent_change"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)
	expectedFields := map[string]interface{}{
		"a_percent_change": float64(0),
		"b_percent_change": float64(200),
		"c_percent_change": float64(100),
		"d_percent_change": float64(200),
		"g_percent_change": float64(-66.66666666666666),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

func TestBasicStatsWithInterval(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"interval"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_interval": int64(time.Millisecond),
		"b_interval": int64(time.Millisecond),
		"c_interval": int64(time.Millisecond),
		"d_interval": int64(time.Millisecond),
		"g_interval": int64(time.Millisecond),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test only aggregating non_negative_diff
func TestBasicStatsWithNonNegativeDiff(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"non_negative_diff"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_non_negative_diff": float64(0),
		"b_non_negative_diff": float64(2),
		"c_non_negative_diff": float64(2),
		"d_non_negative_diff": float64(4),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test aggregating with all stats
func TestBasicStatsWithAllStats(t *testing.T) {
	acc := testutil.Accumulator{}
	minmax := NewBasicStats()
	minmax.Log = testutil.Logger{}
	minmax.Stats = []string{"count", "min", "max", "mean", "stdev", "s2", "sum", "last", "first"}
	minmax.initConfiguredStats()

	minmax.Add(m1)
	minmax.Add(m2)
	minmax.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_count": float64(2), // a
		"a_max":   float64(1),
		"a_min":   float64(1),
		"a_mean":  float64(1),
		"a_stdev": float64(0),
		"a_s2":    float64(0),
		"a_sum":   float64(2),
		"a_last":  float64(1),
		"a_first": float64(1),
		"b_count": float64(2), // b
		"b_max":   float64(3),
		"b_min":   float64(1),
		"b_mean":  float64(2),
		"b_s2":    float64(2),
		"b_sum":   float64(4),
		"b_last":  float64(3),
		"b_stdev": math.Sqrt(2),
		"b_first": float64(1),
		"c_count": float64(2), // c
		"c_max":   float64(4),
		"c_min":   float64(2),
		"c_mean":  float64(3),
		"c_s2":    float64(2),
		"c_stdev": math.Sqrt(2),
		"c_sum":   float64(6),
		"c_last":  float64(4),
		"c_first": float64(2),
		"d_count": float64(2), // d
		"d_max":   float64(6),
		"d_min":   float64(2),
		"d_mean":  float64(4),
		"d_s2":    float64(8),
		"d_stdev": math.Sqrt(8),
		"d_sum":   float64(8),
		"d_last":  float64(6),
		"d_first": float64(2),
		"e_count": float64(1), // e
		"e_max":   float64(200),
		"e_min":   float64(200),
		"e_mean":  float64(200),
		"e_sum":   float64(200),
		"e_last":  float64(200),
		"e_first": float64(200),
		"f_count": float64(1), // f
		"f_max":   float64(200),
		"f_min":   float64(200),
		"f_mean":  float64(200),
		"f_sum":   float64(200),
		"f_last":  float64(200),
		"f_first": float64(200),
		"g_count": float64(2), // g
		"g_max":   float64(3),
		"g_min":   float64(1),
		"g_mean":  float64(2),
		"g_s2":    float64(2),
		"g_stdev": math.Sqrt(2),
		"g_sum":   float64(4),
		"g_last":  float64(1),
		"g_first": float64(3),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

// Test that if an empty array is passed, no points are pushed
func TestBasicStatsWithNoStats(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = make([]string, 0)
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	acc.AssertDoesNotContainMeasurement(t, "m1")
}

// Test that if an unknown stat is configured, it doesn't explode
func TestBasicStatsWithUnknownStat(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"crazy"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	acc.AssertDoesNotContainMeasurement(t, "m1")
}

// Test that if Stats isn't supplied, then we only do count, min, max, mean,
// stdev, and s2.  We purposely exclude sum for backwards compatibility,
// otherwise user's working systems will suddenly (and surprisingly) start
// capturing sum without their input.
func TestBasicStatsWithDefaultStats(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	require.True(t, acc.HasField("m1", "a_count"))
	require.True(t, acc.HasField("m1", "a_min"))
	require.True(t, acc.HasField("m1", "a_max"))
	require.True(t, acc.HasField("m1", "a_mean"))
	require.True(t, acc.HasField("m1", "a_stdev"))
	require.True(t, acc.HasField("m1", "a_s2"))
	require.False(t, acc.HasField("m1", "a_sum"))
}

func TestBasicStatsWithOnlyLast(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"last"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_last": float64(1),
		"b_last": float64(3),
		"c_last": float64(4),
		"d_last": float64(6),
		"e_last": float64(200),
		"f_last": float64(200),
		"g_last": float64(1),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}

func TestBasicStatsWithOnlyFirst(t *testing.T) {
	aggregator := NewBasicStats()
	aggregator.Stats = []string{"first"}
	aggregator.Log = testutil.Logger{}
	aggregator.initConfiguredStats()

	aggregator.Add(m1)
	aggregator.Add(m2)

	acc := testutil.Accumulator{}
	aggregator.Push(&acc)

	expectedFields := map[string]interface{}{
		"a_first": float64(1),
		"b_first": float64(1),
		"c_first": float64(2),
		"d_first": float64(2),
		"e_first": float64(200),
		"f_first": float64(200),
		"g_first": float64(3),
	}
	expectedTags := map[string]string{
		"foo": "bar",
	}
	acc.AssertContainsTaggedFields(t, "m1", expectedFields, expectedTags)
}
