package prometheusremotewrite

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

func TestParse(t *testing.T) {
	prompbInput := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "go_gc_duration_seconds"},
					{Name: "quantile", Value: "0.99"},
				},
				Samples: []prompb.Sample{
					{Value: 4.63, Timestamp: time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC).UnixNano()},
				},
			},
			{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "prometheus_target_interval_length_seconds"},
					{Name: "job", Value: "prometheus"},
				},
				Samples: []prompb.Sample{
					{Value: 14.99, Timestamp: time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC).UnixNano()},
				},
			},
		},
	}

	inoutBytes, err := prompbInput.Marshal()
	require.NoError(t, err)

	expectedV1 := []telegraf.Metric{
		testutil.MustMetric(
			"go_gc_duration_seconds",
			map[string]string{
				"quantile": "0.99",
			},
			map[string]interface{}{
				"value": float64(4.63),
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"prometheus_target_interval_length_seconds",
			map[string]string{
				"job": "prometheus",
			},
			map[string]interface{}{
				"value": float64(14.99),
			},
			time.Unix(0, 0),
		),
	}

	expectedV2 := []telegraf.Metric{
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{
				"quantile": "0.99",
			},
			map[string]interface{}{
				"go_gc_duration_seconds": float64(4.63),
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{
				"job": "prometheus",
			},
			map[string]interface{}{
				"prometheus_target_interval_length_seconds": float64(14.99),
			},
			time.Unix(0, 0),
		),
	}

	parserV1 := newTestParser(map[string]string{}, 1)
	metricsV1, err := parserV1.Parse(inoutBytes)
	require.NoError(t, err)
	require.Len(t, metricsV1, 2)
	testutil.RequireMetricsEqual(t, expectedV1, metricsV1, testutil.IgnoreTime(), testutil.SortMetrics())

	parserV2 := newTestParser(map[string]string{}, 2)
	metricsV2, err := parserV2.Parse(inoutBytes)
	require.NoError(t, err)
	require.Len(t, metricsV2, 2)
	testutil.RequireMetricsEqual(t, expectedV2, metricsV2, testutil.IgnoreTime(), testutil.SortMetrics())
}

func generateTestHistogram(i int) *histogram.Histogram {
	return &histogram.Histogram{
		Count:         12 + uint64(i*9),
		ZeroCount:     2 + uint64(i),
		ZeroThreshold: 0.001,
		Sum:           18.4 * float64(i+1),
		Schema:        1,
		PositiveSpans: []histogram.Span{
			{Offset: 0, Length: 2},
			{Offset: 1, Length: 2},
		},
		PositiveBuckets: []int64{int64(i + 1), 1, -1, 0},
		NegativeSpans: []histogram.Span{
			{Offset: 0, Length: 2},
			{Offset: 1, Length: 2},
		},
		NegativeBuckets: []int64{int64(i + 1), 1, -1, 0},
	}
}

func generateTestFloatHistogram(i int) *histogram.FloatHistogram {
	return &histogram.FloatHistogram{
		Count:         12 + float64(i*9),
		ZeroCount:     2 + float64(i),
		ZeroThreshold: 0.001,
		Sum:           18.4 * float64(i+1),
		Schema:        1,
		PositiveSpans: []histogram.Span{
			{Offset: 0, Length: 2},
			{Offset: 1, Length: 2},
		},
		PositiveBuckets: []float64{float64(i + 1), float64(i + 2), float64(i + 1), float64(i + 1)},
		NegativeSpans: []histogram.Span{
			{Offset: 0, Length: 2},
			{Offset: 1, Length: 2},
		},
		NegativeBuckets: []float64{float64(i + 1), float64(i + 2), float64(i + 1), float64(i + 1)},
	}
}

func TestHistograms(t *testing.T) {
	testHistogram := generateTestHistogram(1)
	testFloatHistogram := generateTestFloatHistogram(2)

	prompbInput := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "test_metric_seconds"},
				},
				Histograms: []prompb.Histogram{
					prompb.FromIntHistogram(0, testHistogram),
				},
			},
			{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "test_float_metric_seconds"},
				},
				Histograms: []prompb.Histogram{
					prompb.FromFloatHistogram(0, testFloatHistogram),
				},
			},
		},
	}

	inoutBytes, err := prompbInput.Marshal()
	require.NoError(t, err)

	expectedV1 := []telegraf.Metric{
		testutil.MustMetric(
			"test_metric_seconds",
			map[string]string{},
			map[string]interface{}{
				"count":                  float64(testHistogram.Count),
				"sum":                    float64(testHistogram.Sum),
				"zero_count":             float64(testHistogram.ZeroCount),
				"zero_threshold":         float64(testHistogram.ZeroThreshold),
				"schema":                 int64(testHistogram.Schema),
				"counter_reset_hint":     uint64(testHistogram.CounterResetHint),
				"positive_span_0_offset": int64(testHistogram.PositiveSpans[0].Offset),
				"positive_span_0_length": uint64(testHistogram.PositiveSpans[0].Length),
				"positive_span_1_offset": int64(testHistogram.PositiveSpans[1].Offset),
				"positive_span_1_length": uint64(testHistogram.PositiveSpans[1].Length),
				"positive_bucket_0":      float64(testHistogram.PositiveBuckets[0]),
				"positive_bucket_1":      float64(testHistogram.PositiveBuckets[1] + testHistogram.PositiveBuckets[0]),
				"positive_bucket_2": float64(testHistogram.PositiveBuckets[2] + testHistogram.PositiveBuckets[1] +
					testHistogram.PositiveBuckets[0]),
				"positive_bucket_3": float64(testHistogram.PositiveBuckets[3] + testHistogram.PositiveBuckets[2] +
					testHistogram.PositiveBuckets[1] + testHistogram.PositiveBuckets[0]),
				"negative_span_0_offset": int64(testHistogram.NegativeSpans[0].Offset),
				"negative_span_0_length": uint64(testHistogram.NegativeSpans[0].Length),
				"negative_span_1_offset": int64(testHistogram.NegativeSpans[1].Offset),
				"negative_span_1_length": uint64(testHistogram.NegativeSpans[1].Length),
				"negative_bucket_0":      float64(testHistogram.NegativeBuckets[0]),
				"negative_bucket_1":      float64(testHistogram.NegativeBuckets[1] + testHistogram.NegativeBuckets[0]),
				"negative_bucket_2": float64(testHistogram.NegativeBuckets[2] + testHistogram.NegativeBuckets[1] +
					testHistogram.NegativeBuckets[0]),
				"negative_bucket_3": float64(testHistogram.NegativeBuckets[3] + testHistogram.NegativeBuckets[2] +
					testHistogram.NegativeBuckets[1] + testHistogram.NegativeBuckets[0]),
				"-2.82842712474619":   float64(2),
				"-2":                  float64(4),
				"-1":                  float64(7),
				"-0.7071067811865475": float64(9),
				"0.001":               float64(12),
				"1":                   float64(14),
				"1.414213562373095":   float64(17),
				"2.82842712474619":    float64(19),
				"4":                   float64(21),
			},
			time.Unix(0, 0),
			telegraf.Histogram,
		),
		testutil.MustMetric(
			"test_float_metric_seconds",
			map[string]string{},
			map[string]interface{}{
				"count":                  float64(testFloatHistogram.Count),
				"sum":                    float64(testFloatHistogram.Sum),
				"zero_count":             float64(testFloatHistogram.ZeroCount),
				"zero_threshold":         float64(testFloatHistogram.ZeroThreshold),
				"schema":                 int64(testFloatHistogram.Schema),
				"counter_reset_hint":     uint64(testFloatHistogram.CounterResetHint),
				"positive_span_0_offset": int64(testFloatHistogram.PositiveSpans[0].Offset),
				"positive_span_0_length": uint64(testFloatHistogram.PositiveSpans[0].Length),
				"positive_span_1_offset": int64(testFloatHistogram.PositiveSpans[1].Offset),
				"positive_span_1_length": uint64(testFloatHistogram.PositiveSpans[1].Length),
				"positive_bucket_0":      float64(testFloatHistogram.PositiveBuckets[0]),
				"positive_bucket_1":      float64(testFloatHistogram.PositiveBuckets[1]), // Float histogram buckets are already absolute
				"positive_bucket_2":      float64(testFloatHistogram.PositiveBuckets[2]),
				"positive_bucket_3":      float64(testFloatHistogram.PositiveBuckets[3]),
				"negative_span_0_offset": int64(testFloatHistogram.NegativeSpans[0].Offset),
				"negative_span_0_length": uint64(testFloatHistogram.NegativeSpans[0].Length),
				"negative_span_1_offset": int64(testFloatHistogram.NegativeSpans[1].Offset),
				"negative_span_1_length": uint64(testFloatHistogram.NegativeSpans[1].Length),
				"negative_bucket_0":      float64(testFloatHistogram.NegativeBuckets[0]),
				"negative_bucket_1":      float64(testFloatHistogram.NegativeBuckets[1]),
				"negative_bucket_2":      float64(testFloatHistogram.NegativeBuckets[2]),
				"negative_bucket_3":      float64(testFloatHistogram.NegativeBuckets[3]),
				"-2.82842712474619":      float64(3),
				"-2":                     float64(6),
				"-1":                     float64(10),
				"-0.7071067811865475":    float64(13),
				"0.001":                  float64(17),
				"1":                      float64(20),
				"1.414213562373095":      float64(24),
				"2.82842712474619":       float64(27),
				"4":                      float64(30),
			},
			time.Unix(0, 0),
			telegraf.Histogram,
		),
	}

	expectedV2 := []telegraf.Metric{
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{},
			map[string]interface{}{
				"test_metric_seconds_sum": float64(36.8),
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{},
			map[string]interface{}{
				"test_metric_seconds_count": float64(21),
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{},
			map[string]interface{}{
				"test_float_metric_seconds_sum": float64(55.199999999999996),
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{},
			map[string]interface{}{
				"test_float_metric_seconds_count": float64(30),
			},
			time.Unix(0, 0),
		),
	}

	parserV1 := newTestParser(map[string]string{}, 1)

	metricsV1, err := parserV1.Parse(inoutBytes)
	require.NoError(t, err)
	require.Len(t, metricsV1, 2)
	testutil.RequireMetricsEqual(t, expectedV1, metricsV1, testutil.IgnoreTime(), testutil.SortMetrics())

	parserV2 := newTestParser(map[string]string{}, 2)

	metricsV2, err := parserV2.Parse(inoutBytes)
	require.NoError(t, err)
	require.Len(t, metricsV2, 22)
	testutil.RequireMetricsSubset(t, expectedV2, metricsV2, testutil.IgnoreTime(), testutil.SortMetrics())
}

func TestDefaultTags(t *testing.T) {
	prompbInput := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "foo"},
					{Name: "__eg__", Value: "bar"},
				},
				Samples: []prompb.Sample{
					{Value: 1, Timestamp: time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC).UnixNano()},
				},
			},
		},
	}

	inoutBytes, err := prompbInput.Marshal()
	require.NoError(t, err)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{
				"defaultTag": "defaultTagValue",
				"__eg__":     "bar",
			},
			map[string]interface{}{
				"foo": float64(1),
			},
			time.Unix(0, 0),
		),
	}

	parser := newTestParser(map[string]string{
		"defaultTag": "defaultTagValue",
	}, 2)

	metrics, err := parser.Parse(inoutBytes)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	testutil.RequireMetricsEqual(t, expected, metrics, testutil.IgnoreTime(), testutil.SortMetrics())
}

func TestMetricsWithTimestamp(t *testing.T) {
	testTime := time.Date(2020, time.October, 4, 17, 0, 0, 0, time.UTC)
	testTimeUnix := testTime.UnixNano() / int64(time.Millisecond)
	prompbInput := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "foo"},
					{Name: "__eg__", Value: "bar"},
				},
				Samples: []prompb.Sample{
					{Value: 1, Timestamp: testTimeUnix},
				},
			},
		},
	}

	inoutBytes, err := prompbInput.Marshal()
	require.NoError(t, err)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"prometheus_remote_write",
			map[string]string{
				"__eg__": "bar",
			},
			map[string]interface{}{
				"foo": float64(1),
			},
			testTime,
		),
	}
	parser := newTestParser(map[string]string{}, 2)

	metrics, err := parser.Parse(inoutBytes)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	testutil.RequireMetricsEqual(t, expected, metrics, testutil.SortMetrics())
}

var benchmarkData = prompb.WriteRequest{
	Timeseries: []prompb.TimeSeries{
		{
			Labels: []prompb.Label{
				{Name: "__name__", Value: "benchmark_a"},
				{Name: "source", Value: "myhost"},
				{Name: "tags_platform", Value: "python"},
				{Name: "tags_sdkver", Value: "3.11.5"},
			},
			Samples: []prompb.Sample{
				{Value: 5.0, Timestamp: time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC).UnixMilli()},
			},
		},
		{
			Labels: []prompb.Label{
				{Name: "__name__", Value: "benchmark_b"},
				{Name: "source", Value: "myhost"},
				{Name: "tags_platform", Value: "python"},
				{Name: "tags_sdkver", Value: "3.11.4"},
			},
			Samples: []prompb.Sample{
				{Value: 4.0, Timestamp: time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC).UnixMilli()},
			},
		},
	},
}

func TestBenchmarkData(t *testing.T) {
	expected := []telegraf.Metric{
		metric.New(
			"prometheus_remote_write",
			map[string]string{
				"source":        "myhost",
				"tags_platform": "python",
				"tags_sdkver":   "3.11.5",
			},
			map[string]interface{}{
				"benchmark_a": 5.0,
			},
			time.Unix(1585699200, 0),
		),
		metric.New(
			"prometheus_remote_write",
			map[string]string{
				"source":        "myhost",
				"tags_platform": "python",
				"tags_sdkver":   "3.11.4",
			},
			map[string]interface{}{
				"benchmark_b": 4.0,
			},
			time.Unix(1585699200, 0),
		),
	}

	benchmarkData, err := benchmarkData.Marshal()
	require.NoError(t, err)

	plugin := newTestParser(map[string]string{}, 2)

	actual, err := plugin.Parse(benchmarkData)
	require.NoError(t, err)
	testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics())
}

func BenchmarkParsing(b *testing.B) {
	benchmarkData, err := benchmarkData.Marshal()
	require.NoError(b, err)

	plugin := newTestParser(map[string]string{}, 2)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		//nolint:errcheck // Benchmarking so skip the error check to avoid the unnecessary operations
		plugin.Parse(benchmarkData)
	}
}

func newTestParser(defaultTags map[string]string, metricVersion int) *Parser {
	parser := &Parser{
		DefaultTags:   defaultTags,
		MetricVersion: metricVersion,
	}
	return parser
}
