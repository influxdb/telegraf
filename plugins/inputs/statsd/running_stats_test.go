package statsd

import (
	"math"
	"testing"
)

// Test that a single metric is handled correctly
func TestRunningStats_Single(t *testing.T) {
	rs := runningStats{}
	values := []float64{10.1}

	for _, v := range values {
		rs.addValue(v)
	}

	if rs.mean() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.mean())
	}
	if rs.median() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.median())
	}
	if rs.upper() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.upper())
	}
	if rs.lower() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.lower())
	}
	if rs.percentile(100) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(100))
	}
	if rs.percentile(99.95) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(99.95))
	}
	if rs.percentile(90) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(90))
	}
	if rs.percentile(50) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(50))
	}
	if rs.percentile(0) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(0))
	}
	if rs.count() != 1 {
		t.Errorf("Expected %v, got %v", 1, rs.count())
	}
	if rs.variance() != 0 {
		t.Errorf("Expected %v, got %v", 0, rs.variance())
	}
	if rs.stddev() != 0 {
		t.Errorf("Expected %v, got %v", 0, rs.stddev())
	}
}

// Test that duplicate values are handled correctly
func TestRunningStats_Duplicate(t *testing.T) {
	rs := runningStats{}
	values := []float64{10.1, 10.1, 10.1, 10.1}

	for _, v := range values {
		rs.addValue(v)
	}

	if rs.mean() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.mean())
	}
	if rs.median() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.median())
	}
	if rs.upper() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.upper())
	}
	if rs.lower() != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.lower())
	}
	if rs.percentile(100) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(100))
	}
	if rs.percentile(99.95) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(99.95))
	}
	if rs.percentile(90) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(90))
	}
	if rs.percentile(50) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(50))
	}
	if rs.percentile(0) != 10.1 {
		t.Errorf("Expected %v, got %v", 10.1, rs.percentile(0))
	}
	if rs.count() != 4 {
		t.Errorf("Expected %v, got %v", 4, rs.count())
	}
	if rs.variance() != 0 {
		t.Errorf("Expected %v, got %v", 0, rs.variance())
	}
	if rs.stddev() != 0 {
		t.Errorf("Expected %v, got %v", 0, rs.stddev())
	}
}

// Test a list of sample values, returns all correct values
func TestRunningStats(t *testing.T) {
	rs := runningStats{}
	values := []float64{10, 20, 10, 30, 20, 11, 12, 32, 45, 9, 5, 5, 5, 10, 23, 8}

	for _, v := range values {
		rs.addValue(v)
	}

	if rs.mean() != 15.9375 {
		t.Errorf("Expected %v, got %v", 15.9375, rs.mean())
	}
	if rs.median() != 10.5 {
		t.Errorf("Expected %v, got %v", 10.5, rs.median())
	}
	if rs.upper() != 45 {
		t.Errorf("Expected %v, got %v", 45, rs.upper())
	}
	if rs.lower() != 5 {
		t.Errorf("Expected %v, got %v", 5, rs.lower())
	}
	if rs.percentile(100) != 45 {
		t.Errorf("Expected %v, got %v", 45, rs.percentile(100))
	}
	if rs.percentile(99.98) != 45 {
		t.Errorf("Expected %v, got %v", 45, rs.percentile(99.98))
	}
	if rs.percentile(90) != 32 {
		t.Errorf("Expected %v, got %v", 32, rs.percentile(90))
	}
	if rs.percentile(50.1) != 11 {
		t.Errorf("Expected %v, got %v", 11, rs.percentile(50.1))
	}
	if rs.percentile(50) != 11 {
		t.Errorf("Expected %v, got %v", 11, rs.percentile(50))
	}
	if rs.percentile(49.9) != 10 {
		t.Errorf("Expected %v, got %v", 10, rs.percentile(49.9))
	}
	if rs.percentile(0) != 5 {
		t.Errorf("Expected %v, got %v", 5, rs.percentile(0))
	}
	if rs.count() != 16 {
		t.Errorf("Expected %v, got %v", 4, rs.count())
	}
	if !fuzzyEqual(rs.variance(), 124.93359, .00001) {
		t.Errorf("Expected %v, got %v", 124.93359, rs.variance())
	}
	if !fuzzyEqual(rs.stddev(), 11.17736, .00001) {
		t.Errorf("Expected %v, got %v", 11.17736, rs.stddev())
	}
}

// Test that the percentile limit is respected.
func TestRunningStats_PercentileLimit(t *testing.T) {
	rs := runningStats{}
	rs.percLimit = 10
	values := []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}

	for _, v := range values {
		rs.addValue(v)
	}

	if rs.count() != 11 {
		t.Errorf("Expected %v, got %v", 11, rs.count())
	}
	if len(rs.perc) != 10 {
		t.Errorf("Expected %v, got %v", 10, len(rs.perc))
	}
}

func fuzzyEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}

// Test that the median limit is respected and medInsertIndex is properly incrementing index.
func TestRunningStats_MedianLimitIndex(t *testing.T) {
	rs := runningStats{}
	rs.medLimit = 10
	values := []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}

	for _, v := range values {
		rs.addValue(v)
	}

	if rs.count() != 11 {
		t.Errorf("Expected %v, got %v", 11, rs.count())
	}
	if len(rs.med) != 10 {
		t.Errorf("Expected %v, got %v", 10, len(rs.med))
	}
	if rs.medInsertIndex != 1 {
		t.Errorf("Expected %v, got %v", 0, rs.medInsertIndex)
	}
}
