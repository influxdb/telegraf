package opensearch_query

import (
	"errors"
	"fmt"
)

type bucketAggregationRequest map[string]*aggregationFunction

func (b bucketAggregationRequest) addAggregation(name, aggType, field string) error {
	switch aggType {
	case "terms":
	default:
		return fmt.Errorf("aggregation function %q not supported", aggType)
	}

	b[name] = &aggregationFunction{
		aggType: aggType,
		field:   field,
	}

	return nil
}

func (b bucketAggregationRequest) addNestedAggregation(name string, a aggregationRequest) {
	b[name].nested = a
}

func (b bucketAggregationRequest) bucketSize(name string, size int) error {
	if size <= 0 {
		return errors.New("invalid size; must be integer value > 0")
	}

	if _, ok := b[name]; !ok {
		return fmt.Errorf("aggregation %q not found", name)
	}

	b[name].setSize(size)

	return nil
}

func (b bucketAggregationRequest) missing(name, missing string) {
	b[name].setMissing(missing)
}
