package opensearch_query

import (
	"encoding/json"
	"time"
)

type query struct {
	Size         int                `json:"size"`
	Aggregations aggregationRequest `json:"aggregations"`
	Query        interface{}        `json:"query,omitempty"`
}

type boolQuery struct {
	FilterQueryString string
	TimestampField    string
	TimeRangeFrom     time.Time
	TimeRangeTo       time.Time
	DateFieldFormat   string
}

func (b *boolQuery) MarshalJSON() ([]byte, error) {
	// Construct range
	dateTimeRange := map[string]interface{}{
		"from":          b.TimeRangeFrom,
		"to":            b.TimeRangeTo,
		"include_lower": true,
		"include_upper": true,
	}
	if b.DateFieldFormat != "" {
		dateTimeRange["format"] = b.DateFieldFormat
	}
	rangeFilter := map[string]map[string]interface{}{"range": {b.TimestampField: dateTimeRange}}

	// Construct Filter
	queryFilter := map[string]map[string]interface{}{
		"query_string": {"query": b.FilterQueryString},
	}

	// Construct boolean query
	bq := map[string]map[string]interface{}{"bool": {"filter": []interface{}{rangeFilter, queryFilter}}}

	return json.Marshal(&bq)
}
