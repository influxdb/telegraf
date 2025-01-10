package histogram

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

type fields map[string]interface{}
type tags map[string]string

// NewTestHistogram creates new test histogram aggregation with specified config
func NewTestHistogram(cfg []bucketConfig, reset, cumulative, pushOnlyOnUpdate bool) telegraf.Aggregator {
	return NewTestHistogramWithExpirationInterval(cfg, reset, cumulative, pushOnlyOnUpdate, 0)
}

func NewTestHistogramWithExpirationInterval(
	cfg []bucketConfig, reset, cumulative, pushOnlyOnUpdate bool, expirationInterval config.Duration,
) telegraf.Aggregator {
	htm := NewHistogramAggregator()
	htm.Configs = cfg
	htm.ResetBuckets = reset
	htm.Cumulative = cumulative
	htm.ExpirationInterval = expirationInterval
	htm.PushOnlyOnUpdate = pushOnlyOnUpdate

	return htm
}

// firstMetric1 is the first test metric
var firstMetric1 = metric.New(
	"first_metric_name",
	tags{},
	fields{
		"a": float64(15.3),
		"b": float64(40),
	},
	time.Now(),
)

// firstMetric1 is the first test metric with other value
var firstMetric2 = metric.New(
	"first_metric_name",
	tags{},
	fields{
		"a": float64(15.9),
		"c": float64(40),
	},
	time.Now(),
)

// secondMetric is the second metric
var secondMetric = metric.New(
	"second_metric_name",
	tags{},
	fields{
		"a":        float64(105),
		"ignoreme": "string",
		"andme":    true,
	},
	time.Now(),
)

// BenchmarkApply runs benchmarks
func BenchmarkApply(b *testing.B) {
	histogram := NewHistogramAggregator()

	for n := 0; n < b.N; n++ {
		histogram.Add(firstMetric1)
		histogram.Add(firstMetric2)
		histogram.Add(secondMetric)
	}
}

// TestHistogram tests metrics for one period and for one field
func TestHistogram(t *testing.T) {
	var cfg []bucketConfig
	cfg = append(cfg, bucketConfig{Metric: "first_metric_name", Fields: []string{"a"}, Buckets: []float64{0.0, 10.0, 20.0, 30.0, 40.0}})
	histogram := NewTestHistogram(cfg, false, true, false)

	acc := &testutil.Accumulator{}

	histogram.Add(firstMetric1)
	histogram.Reset()
	histogram.Add(firstMetric2)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 6, "Incorrect number of metrics")
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "0"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "10"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: "40"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: bucketPosInf})
}

// TestHistogram tests metrics for one period, for one field and push only on histogram update
func TestHistogramPushOnUpdate(t *testing.T) {
	var cfg []bucketConfig
	cfg = append(cfg, bucketConfig{Metric: "first_metric_name", Fields: []string{"a"}, Buckets: []float64{0.0, 10.0, 20.0, 30.0, 40.0}})
	histogram := NewTestHistogram(cfg, false, true, true)

	acc := &testutil.Accumulator{}

	histogram.Add(firstMetric1)
	histogram.Reset()
	histogram.Add(firstMetric2)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 6, "Incorrect number of metrics")
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "0"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "10"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: "40"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketRightTag: bucketPosInf})

	acc.ClearMetrics()
	histogram.Push(acc)
	require.Empty(t, acc.Metrics, "Incorrect number of metrics")
	histogram.Add(firstMetric2)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 6, "Incorrect number of metrics")
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "0"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "10"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(3)}, tags{bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(3)}, tags{bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(3)}, tags{bucketRightTag: "40"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(3)}, tags{bucketRightTag: bucketPosInf})
}

// TestHistogramNonCumulative tests metrics for one period and for one field
func TestHistogramNonCumulative(t *testing.T) {
	var cfg []bucketConfig
	cfg = append(cfg, bucketConfig{Metric: "first_metric_name", Fields: []string{"a"}, Buckets: []float64{0.0, 10.0, 20.0, 30.0, 40.0}})
	histogram := NewTestHistogram(cfg, false, false, false)

	acc := &testutil.Accumulator{}

	histogram.Add(firstMetric1)
	histogram.Reset()
	histogram.Add(firstMetric2)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 6, "Incorrect number of metrics")
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketLeftTag: bucketNegInf, bucketRightTag: "0"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketLeftTag: "0", bucketRightTag: "10"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2)}, tags{bucketLeftTag: "10", bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketLeftTag: "20", bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketLeftTag: "30", bucketRightTag: "40"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketLeftTag: "40", bucketRightTag: bucketPosInf})
}

// TestHistogramWithReset tests metrics for one period and for one field, with reset between metrics adding
func TestHistogramWithReset(t *testing.T) {
	var cfg []bucketConfig
	cfg = append(cfg, bucketConfig{Metric: "first_metric_name", Fields: []string{"a"}, Buckets: []float64{0.0, 10.0, 20.0, 30.0, 40.0}})
	histogram := NewTestHistogram(cfg, true, true, false)

	acc := &testutil.Accumulator{}

	histogram.Add(firstMetric1)
	histogram.Reset()
	histogram.Add(firstMetric2)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 6, "Incorrect number of metrics")
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "0"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0)}, tags{bucketRightTag: "10"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1)}, tags{bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1)}, tags{bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1)}, tags{bucketRightTag: "40"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1)}, tags{bucketRightTag: bucketPosInf})
}

// TestHistogramWithAllFields tests two metrics for one period and for all fields
func TestHistogramWithAllFields(t *testing.T) {
	cfg := []bucketConfig{
		{Metric: "first_metric_name", Buckets: []float64{0.0, 15.5, 20.0, 30.0, 40.0}},
		{Metric: "second_metric_name", Buckets: []float64{0.0, 4.0, 10.0, 23.0, 30.0}},
	}
	histogram := NewTestHistogram(cfg, false, true, false)

	acc := &testutil.Accumulator{}

	histogram.Add(firstMetric1)
	histogram.Add(firstMetric2)
	histogram.Add(secondMetric)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 12, "Incorrect number of metrics")
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0), "b_bucket": int64(0), "c_bucket": int64(0)}, tags{bucketRightTag: "0"})
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(1), "b_bucket": int64(0), "c_bucket": int64(0)},
		tags{bucketRightTag: "15.5"},
	)
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2), "b_bucket": int64(0), "c_bucket": int64(0)}, tags{bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2), "b_bucket": int64(0), "c_bucket": int64(0)}, tags{bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2), "b_bucket": int64(1), "c_bucket": int64(1)}, tags{bucketRightTag: "40"})
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(2), "b_bucket": int64(1), "c_bucket": int64(1)},
		tags{bucketRightTag: bucketPosInf},
	)

	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "0"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "4"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "10"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "23"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "30"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(1), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: bucketPosInf},
	)
}

// TestHistogramWithAllFieldsNonCumulative tests two metrics for one period and for all fields
func TestHistogramWithAllFieldsNonCumulative(t *testing.T) {
	cfg := []bucketConfig{
		{Metric: "first_metric_name", Buckets: []float64{0.0, 15.5, 20.0, 30.0, 40.0}},
		{Metric: "second_metric_name", Buckets: []float64{0.0, 4.0, 10.0, 23.0, 30.0}},
	}
	histogram := NewTestHistogram(cfg, false, false, false)

	acc := &testutil.Accumulator{}

	histogram.Add(firstMetric1)
	histogram.Add(firstMetric2)
	histogram.Add(secondMetric)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 12, "Incorrect number of metrics")
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(0), "b_bucket": int64(0), "c_bucket": int64(0)},
		tags{bucketLeftTag: bucketNegInf, bucketRightTag: "0"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(1), "b_bucket": int64(0), "c_bucket": int64(0)},
		tags{bucketLeftTag: "0", bucketRightTag: "15.5"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(1), "b_bucket": int64(0), "c_bucket": int64(0)},
		tags{bucketLeftTag: "15.5", bucketRightTag: "20"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(0), "b_bucket": int64(0), "c_bucket": int64(0)},
		tags{bucketLeftTag: "20", bucketRightTag: "30"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(0), "b_bucket": int64(1), "c_bucket": int64(1)},
		tags{bucketLeftTag: "30", bucketRightTag: "40"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(0), "b_bucket": int64(0), "c_bucket": int64(0)},
		tags{bucketLeftTag: "40", bucketRightTag: bucketPosInf},
	)

	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketLeftTag: bucketNegInf, bucketRightTag: "0"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketLeftTag: "0", bucketRightTag: "4"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketLeftTag: "4", bucketRightTag: "10"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketLeftTag: "10", bucketRightTag: "23"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketLeftTag: "23", bucketRightTag: "30"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(1), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketLeftTag: "30", bucketRightTag: bucketPosInf},
	)
}

// TestHistogramWithTwoPeriodsAndAllFields tests two metrics getting added with a push/reset in between (simulates
// getting added in different periods) for all fields
func TestHistogramWithTwoPeriodsAndAllFields(t *testing.T) {
	var cfg []bucketConfig
	cfg = append(cfg, bucketConfig{Metric: "first_metric_name", Buckets: []float64{0.0, 10.0, 20.0, 30.0, 40.0}})
	histogram := NewTestHistogram(cfg, false, true, false)

	acc := &testutil.Accumulator{}
	histogram.Add(firstMetric1)
	histogram.Push(acc)

	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0), "b_bucket": int64(0)}, tags{bucketRightTag: "0"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0), "b_bucket": int64(0)}, tags{bucketRightTag: "10"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1), "b_bucket": int64(0)}, tags{bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1), "b_bucket": int64(0)}, tags{bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1), "b_bucket": int64(1)}, tags{bucketRightTag: "40"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(1), "b_bucket": int64(1)}, tags{bucketRightTag: bucketPosInf})

	acc.ClearMetrics()
	histogram.Add(firstMetric2)
	histogram.Push(acc)

	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0), "b_bucket": int64(0), "c_bucket": int64(0)}, tags{bucketRightTag: "0"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(0), "b_bucket": int64(0), "c_bucket": int64(0)}, tags{bucketRightTag: "10"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2), "b_bucket": int64(0), "c_bucket": int64(0)}, tags{bucketRightTag: "20"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2), "b_bucket": int64(0), "c_bucket": int64(0)}, tags{bucketRightTag: "30"})
	assertContainsTaggedField(t, acc, "first_metric_name", fields{"a_bucket": int64(2), "b_bucket": int64(1), "c_bucket": int64(1)}, tags{bucketRightTag: "40"})
	assertContainsTaggedField(
		t,
		acc,
		"first_metric_name",
		fields{"a_bucket": int64(2), "b_bucket": int64(1), "c_bucket": int64(1)},
		tags{bucketRightTag: bucketPosInf},
	)
}

// TestWrongBucketsOrder tests the calling panic with incorrect order of buckets
func TestWrongBucketsOrder(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			require.Equal(
				t,
				"histogram buckets must be in increasing order: 90.00 >= 20.00, metrics: first_metric_name, field: a",
				fmt.Sprint(r),
			)
		}
	}()

	var cfg []bucketConfig
	cfg = append(cfg, bucketConfig{Metric: "first_metric_name", Buckets: []float64{0.0, 90.0, 20.0, 30.0, 40.0}})
	histogram := NewTestHistogram(cfg, false, true, false)
	histogram.Add(firstMetric2)
}

// TestHistogram tests two metrics getting added and metric expiration
func TestHistogramMetricExpiration(t *testing.T) {
	currentTime := time.Unix(10, 0)
	timeNow = func() time.Time {
		return currentTime
	}
	defer func() {
		timeNow = time.Now
	}()

	cfg := []bucketConfig{
		{Metric: "first_metric_name", Fields: []string{"a"}, Buckets: []float64{0.0, 10.0, 20.0, 30.0, 40.0}},
		{Metric: "second_metric_name", Buckets: []float64{0.0, 4.0, 10.0, 23.0, 30.0}},
	}
	histogram := NewTestHistogramWithExpirationInterval(cfg, false, true, false, config.Duration(30))

	acc := &testutil.Accumulator{}

	histogram.Add(firstMetric1)
	currentTime = time.Unix(41, 0)
	histogram.Add(secondMetric)
	histogram.Push(acc)

	require.Len(t, acc.Metrics, 6, "Incorrect number of metrics")
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "0"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "4"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "10"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "23"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(0), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: "30"},
	)
	assertContainsTaggedField(
		t,
		acc,
		"second_metric_name",
		fields{"a_bucket": int64(1), "ignoreme_bucket": int64(0), "andme_bucket": int64(0)},
		tags{bucketRightTag: bucketPosInf},
	)
}

// assertContainsTaggedField is help functions to test histogram data
func assertContainsTaggedField(t *testing.T, acc *testutil.Accumulator, metricName string, fields map[string]interface{}, tags map[string]string) {
	acc.Lock()
	defer acc.Unlock()

	for _, checkedMetric := range acc.Metrics {
		// filter by metric name
		if checkedMetric.Measurement != metricName {
			continue
		}

		// filter by tags
		isTagsIdentical := true
		for tag := range tags {
			if val, ok := checkedMetric.Tags[tag]; !ok || val != tags[tag] {
				isTagsIdentical = false
				break
			}
		}
		if !isTagsIdentical {
			continue
		}

		// filter by field keys
		isFieldKeysIdentical := true
		for field := range fields {
			if _, ok := checkedMetric.Fields[field]; !ok {
				isFieldKeysIdentical = false
				break
			}
		}
		if !isFieldKeysIdentical {
			continue
		}

		// check fields with their counts
		require.Equal(t, fields, checkedMetric.Fields)
		return
	}

	require.Fail(t, fmt.Sprintf("unknown measurement %q with tags: %v, fields: %v", metricName, tags, fields))
}
