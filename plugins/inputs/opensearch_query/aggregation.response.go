package opensearch_query

import (
	"encoding/json"

	"github.com/influxdata/telegraf"
)

type aggregationResponse struct {
	Hits         *searchHits  `json:"hits"`
	Aggregations *aggregation `json:"aggregations"`
}

type searchHits struct {
	TotalHits *totalHits `json:"total,omitempty"`
}

type totalHits struct {
	Relation string `json:"relation"`
	Value    int64  `json:"value"`
}

type metricAggregation map[string]interface{}

type aggregateValue struct {
	metrics metricAggregation
	buckets []bucketData
}

type aggregation map[string]aggregateValue

type bucketData struct {
	DocumentCount int64  `json:"doc_count"`
	Key           string `json:"key"`

	subaggregation aggregation
}

func (a *aggregationResponse) getMetrics(acc telegraf.Accumulator, measurement string) error {
	// Simple case (no aggregations)
	if a.Aggregations == nil {
		tags := make(map[string]string)
		fields := map[string]interface{}{
			"doc_count": a.Hits.TotalHits.Value,
		}
		acc.AddFields(measurement, fields, tags)
		return nil
	}

	return a.Aggregations.getMetrics(acc, measurement, a.Hits.TotalHits.Value, make(map[string]string))
}

func (a *aggregation) getMetrics(acc telegraf.Accumulator, measurement string, docCount int64, tags map[string]string) error {
	var err error
	fields := make(map[string]interface{})
	for name, agg := range *a {
		if agg.isAggregation() {
			for _, bucket := range agg.buckets {
				tt := map[string]string{name: bucket.Key}
				for k, v := range tags {
					tt[k] = v
				}
				err = bucket.subaggregation.getMetrics(acc, measurement, bucket.DocumentCount, tt)
				if err != nil {
					return err
				}
			}
			return nil
		}
		for metric, value := range agg.metrics {
			switch value := value.(type) {
			case map[string]interface{}:
				for k, v := range value {
					fields[name+"_"+metric+"_"+k] = v
				}
			default:
				fields[name+"_"+metric] = value
			}
		}
	}

	fields["doc_count"] = docCount
	acc.AddFields(measurement, fields, tags)

	return nil
}

func (a *aggregateValue) UnmarshalJSON(bytes []byte) error {
	var partial map[string]json.RawMessage
	err := json.Unmarshal(bytes, &partial)
	if err != nil {
		return err
	}

	// We'll continue to unmarshal if we have buckets
	if b, found := partial["buckets"]; found {
		return json.Unmarshal(b, &a.buckets)
	}

	// Use the remaining bytes as metrics
	return json.Unmarshal(bytes, &a.metrics)
}

func (a *aggregateValue) isAggregation() bool {
	return !(a.buckets == nil)
}

func (b *bucketData) UnmarshalJSON(bytes []byte) error {
	var partial map[string]json.RawMessage
	var err error

	err = json.Unmarshal(bytes, &partial)
	if err != nil {
		return err
	}

	err = json.Unmarshal(partial["doc_count"], &b.DocumentCount)
	if err != nil {
		return err
	}
	delete(partial, "doc_count")

	err = json.Unmarshal(partial["key"], &b.Key)
	if err != nil {
		return err
	}
	delete(partial, "key")

	if b.subaggregation == nil {
		b.subaggregation = make(aggregation)
	}

	for name, message := range partial {
		var subaggregation aggregateValue
		err = json.Unmarshal(message, &subaggregation)
		if err != nil {
			return err
		}
		b.subaggregation[name] = subaggregation
	}

	return nil
}
