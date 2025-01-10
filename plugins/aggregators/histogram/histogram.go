//go:generate ../../../tools/readme_config_includer/generator
package histogram

import (
	_ "embed"
	"sort"
	"strconv"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/aggregators"
)

//go:embed sample.conf
var sampleConfig string

// bucketRightTag is the tag, which contains right bucket border
const bucketRightTag = "le"

// bucketPosInf is the right bucket border for infinite values
const bucketPosInf = "+Inf"

// bucketLeftTag is the tag, which contains left bucket border (exclusive)
const bucketLeftTag = "gt"

// bucketNegInf is the left bucket border for infinite values
const bucketNegInf = "-Inf"

// HistogramAggregator is aggregator with histogram configs and particular histograms for defined metrics
type HistogramAggregator struct {
	Configs            []bucketConfig  `toml:"config"`
	ResetBuckets       bool            `toml:"reset"`
	Cumulative         bool            `toml:"cumulative"`
	ExpirationInterval config.Duration `toml:"expiration_interval"`
	PushOnlyOnUpdate   bool            `toml:"push_only_on_update"`

	buckets bucketsByMetrics
	cache   map[uint64]metricHistogramCollection
}

// bucketConfig is the config, which contains name, field of metric and histogram buckets.
type bucketConfig struct {
	Metric  string   `toml:"measurement_name"`
	Fields  []string `toml:"fields"`
	Buckets buckets  `toml:"buckets"`
}

// bucketsByMetrics contains the buckets grouped by metric and field name
type bucketsByMetrics map[string]bucketsByFields

// bucketsByFields contains the buckets grouped by field name
type bucketsByFields map[string]buckets

// buckets contains the right borders buckets
type buckets []float64

// metricHistogramCollection aggregates the histogram data
type metricHistogramCollection struct {
	histogramCollection map[string]counts
	name                string
	tags                map[string]string
	expireTime          time.Time
	updated             bool
}

// counts is the number of hits in the bucket
type counts []int64

// groupedByCountFields contains grouped fields by their count and fields values
type groupedByCountFields struct {
	name            string
	tags            map[string]string
	fieldsWithCount map[string]int64
}

var timeNow = time.Now

// NewHistogramAggregator creates new histogram aggregator
func NewHistogramAggregator() *HistogramAggregator {
	h := &HistogramAggregator{
		Cumulative: true,
	}
	h.buckets = make(bucketsByMetrics)
	h.resetCache()

	return h
}

func (*HistogramAggregator) SampleConfig() string {
	return sampleConfig
}

// Add adds new hit to the buckets
func (h *HistogramAggregator) Add(in telegraf.Metric) {
	addTime := timeNow()

	bucketsByField := make(map[string][]float64)
	for field := range in.Fields() {
		buckets := h.getBuckets(in.Name(), field)
		if buckets != nil {
			bucketsByField[field] = buckets
		}
	}

	if len(bucketsByField) == 0 {
		return
	}

	id := in.HashID()
	agr, ok := h.cache[id]
	if !ok {
		agr = metricHistogramCollection{
			name:                in.Name(),
			tags:                in.Tags(),
			histogramCollection: make(map[string]counts),
		}
	}

	for field, value := range in.Fields() {
		if buckets, ok := bucketsByField[field]; ok {
			if agr.histogramCollection[field] == nil {
				agr.histogramCollection[field] = make(counts, len(buckets)+1)
			}

			if value, ok := convert(value); ok {
				index := sort.SearchFloat64s(buckets, value)
				agr.histogramCollection[field][index]++
			}
			if h.ExpirationInterval != 0 {
				agr.expireTime = addTime.Add(time.Duration(h.ExpirationInterval))
			}
			agr.updated = true
		}
	}

	h.cache[id] = agr
}

// Push returns histogram values for metrics
func (h *HistogramAggregator) Push(acc telegraf.Accumulator) {
	now := timeNow()
	metricsWithGroupedFields := make([]groupedByCountFields, 0)
	for id, aggregate := range h.cache {
		if h.ExpirationInterval != 0 && now.After(aggregate.expireTime) {
			delete(h.cache, id)
			continue
		}
		if h.PushOnlyOnUpdate && !h.cache[id].updated {
			continue
		}
		aggregate.updated = false
		h.cache[id] = aggregate
		for field, counts := range aggregate.histogramCollection {
			h.groupFieldsByBuckets(&metricsWithGroupedFields, aggregate.name, field, copyTags(aggregate.tags), counts)
		}
	}

	for _, metric := range metricsWithGroupedFields {
		acc.AddFields(metric.name, makeFieldsWithCount(metric.fieldsWithCount), metric.tags)
	}
}

// groupFieldsByBuckets groups fields by metric buckets which are represented as tags
func (h *HistogramAggregator) groupFieldsByBuckets(
	metricsWithGroupedFields *[]groupedByCountFields, name, field string, tags map[string]string, counts []int64,
) {
	sum := int64(0)
	buckets := h.getBuckets(name, field) // note that len(buckets) + 1 == len(counts)

	for index, count := range counts {
		if !h.Cumulative {
			sum = 0 // reset sum -> don't store cumulative counts

			tags[bucketLeftTag] = bucketNegInf
			if index > 0 {
				tags[bucketLeftTag] = strconv.FormatFloat(buckets[index-1], 'f', -1, 64)
			}
		}

		tags[bucketRightTag] = bucketPosInf
		if index < len(buckets) {
			tags[bucketRightTag] = strconv.FormatFloat(buckets[index], 'f', -1, 64)
		}

		sum += count
		h.groupField(metricsWithGroupedFields, name, field, sum, copyTags(tags))
	}
}

// groupField groups field by count value
func (h *HistogramAggregator) groupField(metricsWithGroupedFields *[]groupedByCountFields, name, field string, count int64, tags map[string]string) {
	for key, metric := range *metricsWithGroupedFields {
		if name == metric.name && isTagsIdentical(tags, metric.tags) {
			(*metricsWithGroupedFields)[key].fieldsWithCount[field] = count
			return
		}
	}

	fieldsWithCount := map[string]int64{
		field: count,
	}

	*metricsWithGroupedFields = append(
		*metricsWithGroupedFields,
		groupedByCountFields{name: name, tags: tags, fieldsWithCount: fieldsWithCount},
	)
}

// Reset does nothing by default, because we typically need to collect counts for a long time.
// Otherwise if config parameter 'reset' has 'true' value, we will get a histogram
// with a small amount of the distribution. However in some use cases a reset is useful.
func (h *HistogramAggregator) Reset() {
	if h.ResetBuckets {
		h.resetCache()
		h.buckets = make(bucketsByMetrics)
	}
}

// resetCache resets cached counts(hits) in the buckets
func (h *HistogramAggregator) resetCache() {
	h.cache = make(map[uint64]metricHistogramCollection)
}

// getBuckets finds buckets and returns them
func (h *HistogramAggregator) getBuckets(metric, field string) []float64 {
	if buckets, ok := h.buckets[metric][field]; ok {
		return buckets
	}

	for _, cfg := range h.Configs {
		if cfg.Metric == metric {
			if !isBucketExists(field, cfg) {
				continue
			}

			if _, ok := h.buckets[metric]; !ok {
				h.buckets[metric] = make(bucketsByFields)
			}

			h.buckets[metric][field] = sortBuckets(cfg.Buckets)
		}
	}

	return h.buckets[metric][field]
}

// isBucketExists checks if buckets exists for the passed field
func isBucketExists(field string, cfg bucketConfig) bool {
	if len(cfg.Fields) == 0 {
		return true
	}

	for _, fl := range cfg.Fields {
		if fl == field {
			return true
		}
	}

	return false
}

// sortBuckets sorts the buckets if it is needed
func sortBuckets(buckets []float64) []float64 {
	for i, bucket := range buckets {
		if i < len(buckets)-1 && bucket >= buckets[i+1] {
			sort.Float64s(buckets)
			break
		}
	}

	return buckets
}

// convert converts interface to concrete type
func convert(in interface{}) (float64, bool) {
	switch v := in.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

// copyTags copies tags
func copyTags(tags map[string]string) map[string]string {
	copiedTags := make(map[string]string, len(tags))
	for key, val := range tags {
		copiedTags[key] = val
	}

	return copiedTags
}

// isTagsIdentical checks the identity of two list of tags
func isTagsIdentical(originalTags, checkedTags map[string]string) bool {
	if len(originalTags) != len(checkedTags) {
		return false
	}

	for tagName, tagValue := range originalTags {
		if tagValue != checkedTags[tagName] {
			return false
		}
	}

	return true
}

// makeFieldsWithCount assigns count value to all metric fields
func makeFieldsWithCount(fieldsWithCountIn map[string]int64) map[string]interface{} {
	fieldsWithCountOut := make(map[string]interface{}, len(fieldsWithCountIn))
	for field, count := range fieldsWithCountIn {
		fieldsWithCountOut[field+"_bucket"] = count
	}

	return fieldsWithCountOut
}

// init initializes histogram aggregator plugin
func init() {
	aggregators.Add("histogram", func() telegraf.Aggregator {
		return NewHistogramAggregator()
	})
}
