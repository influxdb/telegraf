package opensearch_query

import (
	"encoding/json"
)

type aggregationRequest interface {
	addAggregation(string, string, string) error
}

type aggregationFunction struct {
	aggType string
	field   string
	size    int
	missing string

	nested aggregationRequest
}

func (a *aggregationFunction) MarshalJSON() ([]byte, error) {
	agg := make(map[string]interface{})
	field := map[string]interface{}{"field": a.field}
	if t := getAggregationFunctionType(a.aggType); t == "bucket" {
		// We'll use the default size of 10 if it hasn't been set; size == 0 is illegal in a bucket aggregation
		if a.size == 0 {
			a.size = 10
		}
		field["size"] = a.size
	}
	if a.missing != "" {
		field["missing"] = a.missing
	}

	agg[a.aggType] = field

	if a.nested != nil {
		agg["aggregations"] = a.nested
	}
	return json.Marshal(agg)
}

func (a *aggregationFunction) setSize(size int) {
	a.size = size
}

func (a *aggregationFunction) setMissing(missing string) {
	a.missing = missing
}

func getAggregationFunctionType(field string) string {
	switch field {
	case "avg", "sum", "min", "max", "value_count", "stats", "extended_stats", "percentiles":
		return "metric"
	case "terms":
		return "bucket"
	default:
		return ""
	}
}
