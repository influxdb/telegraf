package prometheusremotewrite

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/prompb"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/serializers"
	"github.com/influxdata/telegraf/plugins/serializers/prometheus"
)

type MetricKey uint64

type Serializer struct {
	SortMetrics   bool            `toml:"prometheus_sort_metrics"`
	StringAsLabel bool            `toml:"prometheus_string_as_label"`
	Log           telegraf.Logger `toml:"-"`
}

func (s *Serializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	return s.SerializeBatch([]telegraf.Metric{metric})
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	var lastErr error
	// traceAndKeepErr logs on Trace level every passed error.
	// with each call it updates lastErr, so it can be logged later with higher level.
	traceAndKeepErr := func(format string, a ...any) {
		lastErr = fmt.Errorf(format, a...)
		s.Log.Trace(lastErr)
	}

	var buf bytes.Buffer
	var entries = make(map[MetricKey]prompb.TimeSeries)
	var labels = make([]prompb.Label, 0)
	for _, metric := range metrics {
		labels = s.appendCommonLabels(labels[:0], metric)
		var metrickey MetricKey
		var promts prompb.TimeSeries

		if metric.Type() == telegraf.Histogram {
			if ok, fh := tryGetNativeHistogram(metric); ok {
				metrickey, promts = getPromNativeHistogramTS(metric.Name(), labels, *fh, metric.Time())
				// A batch of metrics can contain multiple values for a single
				// Prometheus histogram. If this metric is older than the existing
				// histogram then we can skip over it.
				m, ok := entries[metrickey]
				if ok {
					if metric.Time().Before(time.Unix(0, m.Histograms[0].Timestamp*1_000_000)) {
						traceAndKeepErr("metric %q has histograms with timestamp %v older than already registered before", metric.Name(), metric.Time())
						continue
					}
				}
				entries[metrickey] = promts
				continue
			}
			fmt.Println(fmt.Sprintf("metric %q is not a native histogram: fields %v", metric.Name(), metric.Fields()))
		}

		for _, field := range metric.FieldList() {
			metricName := prometheus.MetricName(metric.Name(), field.Key, metric.Type())
			metricName, ok := prometheus.SanitizeMetricName(metricName)
			if !ok {
				traceAndKeepErr("failed to parse metric name %q", metricName)
				continue
			}

			switch metric.Type() {
			case telegraf.Counter:
				fallthrough
			case telegraf.Gauge:
				fallthrough
			case telegraf.Untyped:
				value, ok := prometheus.SampleValue(field.Value)
				if !ok {
					traceAndKeepErr("failed to parse %q: bad sample value %#v", metricName, field.Value)
					continue
				}
				metrickey, promts = getPromTS(metricName, labels, value, metric.Time())
			case telegraf.Histogram:
				switch {
				case strings.HasSuffix(field.Key, "_bucket"):
					// if bucket only, init sum, count, inf
					metrickeysum, promtssum := getPromTS(metricName+"_sum", labels, float64(0), metric.Time())
					if _, ok = entries[metrickeysum]; !ok {
						entries[metrickeysum] = promtssum
					}
					metrickeycount, promtscount := getPromTS(metricName+"_count", labels, float64(0), metric.Time())
					if _, ok = entries[metrickeycount]; !ok {
						entries[metrickeycount] = promtscount
					}
					extraLabel := prompb.Label{
						Name:  "le",
						Value: "+Inf",
					}
					metrickeyinf, promtsinf := getPromTS(metricName+"_bucket", labels, float64(0), metric.Time(), extraLabel)
					if _, ok = entries[metrickeyinf]; !ok {
						entries[metrickeyinf] = promtsinf
					}

					le, ok := metric.GetTag("le")
					if !ok {
						traceAndKeepErr("failed to parse %q: can't find `le` label", metricName)
						continue
					}
					bound, err := strconv.ParseFloat(le, 64)
					if err != nil {
						traceAndKeepErr("failed to parse %q: can't parse %q value: %w", metricName, le, err)
						continue
					}
					count, ok := prometheus.SampleCount(field.Value)
					if !ok {
						traceAndKeepErr("failed to parse %q: bad sample value %#v", metricName, field.Value)
						continue
					}

					extraLabel = prompb.Label{
						Name:  "le",
						Value: fmt.Sprint(bound),
					}
					metrickey, promts = getPromTS(metricName+"_bucket", labels, float64(count), metric.Time(), extraLabel)
				case strings.HasSuffix(field.Key, "_sum"):
					sum, ok := prometheus.SampleSum(field.Value)
					if !ok {
						traceAndKeepErr("failed to parse %q: bad sample value %#v", metricName, field.Value)
						continue
					}

					metrickey, promts = getPromTS(metricName+"_sum", labels, sum, metric.Time())
				case strings.HasSuffix(field.Key, "_count"):
					count, ok := prometheus.SampleCount(field.Value)
					if !ok {
						traceAndKeepErr("failed to parse %q: bad sample value %#v", metricName, field.Value)
						continue
					}

					// if no bucket generate +Inf entry
					extraLabel := prompb.Label{
						Name:  "le",
						Value: "+Inf",
					}
					metrickeyinf, promtsinf := getPromTS(metricName+"_bucket", labels, float64(count), metric.Time(), extraLabel)
					if minf, ok := entries[metrickeyinf]; !ok || minf.Samples[0].Value == 0 {
						entries[metrickeyinf] = promtsinf
					}

					metrickey, promts = getPromTS(metricName+"_count", labels, float64(count), metric.Time())
				default:
					traceAndKeepErr("failed to parse %q: series %q should have `_count`, `_sum` or `_bucket` suffix", metricName, field.Key)
					continue
				}
			case telegraf.Summary:
				switch {
				case strings.HasSuffix(field.Key, "_sum"):
					sum, ok := prometheus.SampleSum(field.Value)
					if !ok {
						traceAndKeepErr("failed to parse %q: bad sample value %#v", metricName, field.Value)
						continue
					}

					metrickey, promts = getPromTS(metricName+"_sum", labels, sum, metric.Time())
				case strings.HasSuffix(field.Key, "_count"):
					count, ok := prometheus.SampleCount(field.Value)
					if !ok {
						traceAndKeepErr("failed to parse %q: bad sample value %#v", metricName, field.Value)
						continue
					}

					metrickey, promts = getPromTS(metricName+"_count", labels, float64(count), metric.Time())
				default:
					quantileTag, ok := metric.GetTag("quantile")
					if !ok {
						traceAndKeepErr("failed to parse %q: can't find `quantile` label", metricName)
						continue
					}
					quantile, err := strconv.ParseFloat(quantileTag, 64)
					if err != nil {
						traceAndKeepErr("failed to parse %q: can't parse %q value: %w", metricName, quantileTag, err)
						continue
					}
					value, ok := prometheus.SampleValue(field.Value)
					if !ok {
						traceAndKeepErr("failed to parse %q: bad sample value %#v", metricName, field.Value)
						continue
					}

					extraLabel := prompb.Label{
						Name:  "quantile",
						Value: fmt.Sprint(quantile),
					}
					metrickey, promts = getPromTS(metricName, labels, value, metric.Time(), extraLabel)
				}
			default:
				return nil, fmt.Errorf("unknown type %v", metric.Type())
			}

			// A batch of metrics can contain multiple values for a single
			// Prometheus sample. If this metric is older than the existing
			// sample then we can skip over it.
			m, ok := entries[metrickey]
			if ok {
				if metric.Time().Before(time.Unix(0, m.Samples[0].Timestamp*1_000_000)) {
					traceAndKeepErr("metric %q has samples with timestamp %v older than already registered before", metric.Name(), metric.Time())
					continue
				}
			}
			entries[metrickey] = promts
		}
	}

	if lastErr != nil {
		// log only the last recorded error in the batch, as it could have many errors and logging each one
		// could be too verbose. The following log line still provides enough info for user to act on.
		s.Log.Errorf("some series were dropped, %d series left to send; last recorded error: %v", len(entries), lastErr)
	}

	var promTS = make([]prompb.TimeSeries, len(entries))
	var i int
	for _, promts := range entries {
		promTS[i] = promts
		i++
	}

	if s.SortMetrics {
		sort.Slice(promTS, func(i, j int) bool {
			lhs := promTS[i].Labels
			rhs := promTS[j].Labels
			if len(lhs) != len(rhs) {
				return len(lhs) < len(rhs)
			}

			for index := range lhs {
				l := lhs[index]
				r := rhs[index]

				if l.Name != r.Name {
					return l.Name < r.Name
				}

				if l.Value != r.Value {
					return l.Value < r.Value
				}
			}

			return false
		})
	}
	pb := &prompb.WriteRequest{Timeseries: promTS}
	data, err := pb.Marshal()
	if err != nil {
		return nil, fmt.Errorf("unable to marshal protobuf: %w", err)
	}
	encoded := snappy.Encode(nil, data)
	buf.Write(encoded)
	return buf.Bytes(), nil
}

func hasLabel(name string, labels []prompb.Label) bool {
	for _, label := range labels {
		if name == label.Name {
			return true
		}
	}
	return false
}

func (s *Serializer) appendCommonLabels(labels []prompb.Label, metric telegraf.Metric) []prompb.Label {
	for _, tag := range metric.TagList() {
		// Ignore special tags for histogram and summary types.
		switch metric.Type() {
		case telegraf.Histogram:
			if tag.Key == "le" {
				continue
			}
		case telegraf.Summary:
			if tag.Key == "quantile" {
				continue
			}
		}

		name, ok := prometheus.SanitizeLabelName(tag.Key)
		if !ok {
			continue
		}

		// remove tags with empty values
		if tag.Value == "" {
			continue
		}

		labels = append(labels, prompb.Label{Name: name, Value: tag.Value})
	}

	if !s.StringAsLabel {
		return labels
	}

	for _, field := range metric.FieldList() {
		value, ok := field.Value.(string)
		if !ok {
			continue
		}

		name, ok := prometheus.SanitizeLabelName(field.Key)
		if !ok {
			continue
		}

		// If there is a tag with the same name as the string field, discard
		// the field and use the tag instead.
		if hasLabel(name, labels) {
			continue
		}

		labels = append(labels, prompb.Label{Name: name, Value: value})
	}

	return labels
}

func MakeMetricKey(labels []prompb.Label) MetricKey {
	h := fnv.New64a()
	for _, label := range labels {
		h.Write([]byte(label.Name))
		h.Write([]byte("\x00"))
		h.Write([]byte(label.Value))
		h.Write([]byte("\x00"))
	}
	return MetricKey(h.Sum64())
}

func getPromTS(name string, labels []prompb.Label, value float64, ts time.Time, extraLabels ...prompb.Label) (MetricKey, prompb.TimeSeries) {
	labelscopy := make([]prompb.Label, len(labels), len(labels)+1)
	copy(labelscopy, labels)

	sample := []prompb.Sample{{
		// Timestamp is int milliseconds for remote write.
		Timestamp: ts.UnixNano() / int64(time.Millisecond),
		Value:     value,
	}}
	labelscopy = append(labelscopy, extraLabels...)
	labelscopy = append(labelscopy, prompb.Label{
		Name:  "__name__",
		Value: name,
	})

	// we sort the labels since Prometheus TSDB does not like out of order labels
	sort.Sort(sortableLabels(labelscopy))

	return MakeMetricKey(labelscopy), prompb.TimeSeries{Labels: labelscopy, Samples: sample}
}

func tryGetNativeHistogram(metric telegraf.Metric) (bool, *histogram.FloatHistogram) {
	fields := metric.Fields()
	count, ok := fields["count"]
	if !ok {
		return false, nil
	}
	countFloat, ok := count.(float64)
	if !ok {
		return false, nil
	}
	sum, ok := fields["sum"]
	if !ok {
		return false, nil
	}
	sumFloat, ok := sum.(float64)
	if !ok {
		return false, nil
	}
	schema, ok := fields["schema"]
	if !ok {
		return false, nil
	}
	schemaInt, ok := schema.(int64)
	if !ok {
		return false, nil
	}
	counterResetHint, ok := fields["counter_reset_hint"]
	if !ok {
		return false, nil
	}
	counterResetHintInt, ok := counterResetHint.(uint64)
	if !ok {
		return false, nil
	}
	zeroThreshold, ok := fields["zero_threshold"]
	if !ok {
		return false, nil
	}
	zeroThresholdFloat, ok := zeroThreshold.(float64)
	if !ok {
		return false, nil
	}
	zeroCount, ok := fields["zero_count"]
	if !ok {
		return false, nil
	}
	zeroCountFloat, ok := zeroCount.(float64)
	if !ok {
		return false, nil
	}

	floatHistogram := &histogram.FloatHistogram{
		Count:            countFloat,
		Sum:              sumFloat,
		Schema:           int32(schemaInt),
		CounterResetHint: histogram.CounterResetHint(counterResetHintInt),
		ZeroThreshold:    zeroThresholdFloat,
		ZeroCount:        zeroCountFloat,
		PositiveSpans:    make([]histogram.Span, 0),
		NegativeSpans:    make([]histogram.Span, 0),
		PositiveBuckets:  make([]float64, 0),
		NegativeBuckets:  make([]float64, 0),
	}

	fmt.Println(fmt.Sprintf("floatHistogram alright so far: %v", floatHistogram))

	// expand positiveSpans and negativeSpans into fields
	i := 0
	for {
		offset, ok := fields[fmt.Sprintf("positive_span_%d_offset", i)]
		if !ok {
			break
		}
		offsetInt, ok := offset.(int64)
		if !ok {
			break
		}
		length, ok := fields[fmt.Sprintf("positive_span_%d_length", i)]
		if !ok {
			break
		}
		lengthInt, ok := length.(uint64)
		if !ok {
			break
		}
		positiveSpan := histogram.Span{
			Offset: int32(offsetInt),
			Length: uint32(lengthInt),
		}
		floatHistogram.PositiveSpans = append(floatHistogram.PositiveSpans, positiveSpan)
		i++
	}
	i = 0
	for {
		offset, ok := fields[fmt.Sprintf("negative_span_%d_offset", i)]
		if !ok {
			break
		}
		offsetInt, ok := offset.(int64)
		if !ok {
			break
		}
		length, ok := fields[fmt.Sprintf("negative_span_%d_length", i)]
		if !ok {
			break
		}
		lengthInt, ok := length.(uint64)
		if !ok {
			break
		}
		negativeSpan := histogram.Span{
			Offset: int32(offsetInt),
			Length: uint32(lengthInt),
		}
		floatHistogram.NegativeSpans = append(floatHistogram.NegativeSpans, negativeSpan)
		i++
	}
	i = 0
	for {
		bucket, ok := fields[fmt.Sprintf("positive_bucket_%d", i)]
		if !ok {
			break
		}
		bucketFloat, ok := bucket.(float64)
		if !ok {
			break
		}
		floatHistogram.PositiveBuckets = append(floatHistogram.PositiveBuckets, bucketFloat)
		i++
	}
	i = 0
	for {
		bucket, ok := fields[fmt.Sprintf("negative_bucket_%d", i)]
		if !ok {
			break
		}
		bucketFloat, ok := bucket.(float64)
		if !ok {
			break
		}
		floatHistogram.NegativeBuckets = append(floatHistogram.NegativeBuckets, bucketFloat)
		i++
	}
	err := floatHistogram.Validate()
	if err != nil {
		return false, nil
	}
	return true, floatHistogram
}

func getPromNativeHistogramTS(name string, labels []prompb.Label, fh histogram.FloatHistogram, ts time.Time, extraLabels ...prompb.Label) (MetricKey, prompb.TimeSeries) {
	labelscopy := make([]prompb.Label, len(labels), len(labels)+1)
	copy(labelscopy, labels)

	histograms := []prompb.Histogram{
		prompb.FromFloatHistogram(
			ts.UnixNano()/int64(time.Millisecond),
			&fh,
		),
	}
	labelscopy = append(labelscopy, extraLabels...)
	labelscopy = append(labelscopy, prompb.Label{
		Name:  "__name__",
		Value: name,
	})

	// we sort the labels since Prometheus TSDB does not like out of order labels
	sort.Sort(sortableLabels(labelscopy))

	// for a native histogram, samples are not used; instead, histograms field is used
	return MakeMetricKey(labelscopy), prompb.TimeSeries{Labels: labelscopy, Histograms: histograms}
}

type sortableLabels []prompb.Label

func (sl sortableLabels) Len() int { return len(sl) }
func (sl sortableLabels) Less(i, j int) bool {
	return sl[i].Name < sl[j].Name
}
func (sl sortableLabels) Swap(i, j int) {
	sl[i], sl[j] = sl[j], sl[i]
}

func init() {
	serializers.Add("prometheusremotewrite",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
