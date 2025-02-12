package prometheusremotewrite

import (
	"fmt"
	"math"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

func (p *Parser) extractMetricsV1(ts *prompb.TimeSeries) ([]telegraf.Metric, error) {
	now := time.Now()

	// Convert each prometheus metrics to the corresponding telegraf metrics.
	// You will get one telegraf metric with one field per prometheus metric
	// for "simple" types like Gauge and Counter.
	// However, since in prometheus remote write, a "complex" type is already
	// broken down into multiple "simple" types metrics, you will still get
	// multiple telegraf metrics per Histogram or Summary.
	// One bucket of a histogram could also be split into multiple remote
	// write requests, so we won't try to aggregate them here.
	// However, for Native Histogram, you will get one telegraf metric with
	// multiple fields.
	metrics := make([]telegraf.Metric, 0, len(ts.Samples)+len(ts.Histograms))

	tags := make(map[string]string, len(p.DefaultTags)+len(ts.Labels))
	for key, value := range p.DefaultTags {
		tags[key] = value
	}
	for _, l := range ts.Labels {
		tags[l.Name] = l.Value
	}

	metricName := tags[model.MetricNameLabel]
	if metricName == "" {
		return nil, fmt.Errorf("metric name %q not found in tag-set or empty", model.MetricNameLabel)
	}
	delete(tags, model.MetricNameLabel)
	t := now
	for _, s := range ts.Samples {
		if !math.IsNaN(s.Value) {
			// In prometheus remote write,
			// You won't know if it's a counter or gauge or a sub-counter in a histogram
			fields := map[string]interface{}{"value": s.Value}
			if s.Timestamp > 0 {
				t = time.Unix(0, s.Timestamp*1000000)
			}
			m := metric.New(metricName, tags, fields, t)
			metrics = append(metrics, m)
		}
	}

	for _, hp := range ts.Histograms {
		h := hp.ToFloatHistogram()

		if hp.Timestamp > 0 {
			t = time.Unix(0, hp.Timestamp*1000000)
		}

		fields := map[string]any{
			"counter_reset_hint": uint64(h.CounterResetHint),
			"schema":             int64(h.Schema),
			"zero_threshold":     h.ZeroThreshold,
			"zero_count":         h.ZeroCount,
			"count":              h.Count,
			"sum":                h.Sum,
		}

		count := 0.0
		iter := h.AllBucketIterator()
		for iter.Next() {
			bucket := iter.At()
			count = count + bucket.Count
			fields[fmt.Sprintf("%g", bucket.Upper)] = count
		}

		// expand positiveSpans and negativeSpans into fields
		for i, span := range h.PositiveSpans {
			fields[fmt.Sprintf("positive_span_%d_offset", i)] = int64(span.Offset)
			fields[fmt.Sprintf("positive_span_%d_length", i)] = uint64(span.Length)
		}

		for i, span := range h.NegativeSpans {
			fields[fmt.Sprintf("negative_span_%d_offset", i)] = int64(span.Offset)
			fields[fmt.Sprintf("negative_span_%d_length", i)] = uint64(span.Length)
		}
		// expand positiveBuckets and negativeBuckets into fields
		for i, bucket := range h.PositiveBuckets {
			fields[fmt.Sprintf("positive_bucket_%d", i)] = bucket
		}

		for i, bucket := range h.NegativeBuckets {
			fields[fmt.Sprintf("negative_bucket_%d", i)] = bucket
		}

		m := metric.New(metricName, tags, fields, t, telegraf.Histogram)
		metrics = append(metrics, m)
	}

	return metrics, nil
}
