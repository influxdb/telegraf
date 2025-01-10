package opensearch_query

import "fmt"

type metricAggregationRequest map[string]*aggregationFunction

func (m metricAggregationRequest) addAggregation(name, aggType, field string) error {
	if t := getAggregationFunctionType(aggType); t != "metric" {
		return fmt.Errorf("aggregation function %q not supported", aggType)
	}

	m[name] = &aggregationFunction{
		aggType: aggType,
		field:   field,
	}

	return nil
}
