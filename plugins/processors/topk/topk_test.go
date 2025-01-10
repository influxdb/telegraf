package topk

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

var tenMillisecondsDuration = config.Duration(10 * time.Millisecond)

// Key, value pair that represents a telegraf.Metric Field
type field struct {
	key string
	val interface{}
}

func fieldList(fields ...field) []field {
	return fields
}

// Key, value pair that represents a telegraf.Metric Tag
type tag struct {
	key string
	val string
}

func tagList(tags ...tag) []tag {
	return tags
}

// Abstraction of a change in a single metric
type metricChange struct {
	newFields []field // Fieldsthat should be added to the metric
	newTags   []tag   // Tags that should be added to the metric

	runHash bool // Sometimes the metrics' HashID must be run so reflect.DeepEqual works
	// This happens because telegraf.Metric maintains an internal cache of
	// its hash value that is set when HashID() is called for the first time
}

// Generate a new set of metrics from a set of changes. This is used to generate an answer which will be
// compare against the output of the processor
// NOTE: A `changeSet` is a map where the keys are the indices of the metrics to keep, and the values
//
//	are list of new tags and fields to be added to the metric in that index.
//	THE ORDERING OF THE NEW TAGS AND FIELDS MATTERS. When using reflect.DeepEqual to compare metrics,
//	comparing metrics that have the same fields/tags added in different orders will return false, although
//	they are semantically equal.
//	Therefore the fields and tags must be in the same order that the processor would add them
func generateAns(input []telegraf.Metric, changeSet map[int]metricChange) []telegraf.Metric {
	answer := make([]telegraf.Metric, 0, len(input))

	// For every input metric, we check if there is a change we need to apply
	// If there is no change for a given input metric, the metric is dropped
	for i, m := range input {
		change, ok := changeSet[i]
		if ok {
			// Deep copy the metric
			newMetric := m.Copy()

			// Add new fields
			if change.newFields != nil {
				for _, p := range change.newFields {
					newMetric.AddField(p.key, p.val)
				}
			}

			// Add new tags
			if change.newTags != nil {
				for _, p := range change.newTags {
					newMetric.AddTag(p.key, p.val)
				}
			}

			// Run the hash function if required
			if change.runHash {
				newMetric.HashID()
			}

			answer = append(answer, newMetric)
		}
	}

	return answer
}

func deepCopy(a []telegraf.Metric) []telegraf.Metric {
	ret := make([]telegraf.Metric, 0, len(a))
	for _, m := range a {
		ret = append(ret, m.Copy())
	}

	return ret
}

func belongs(m telegraf.Metric, ms []telegraf.Metric) bool {
	for _, i := range ms {
		if testutil.MetricEqual(i, m) {
			return true
		}
	}
	return false
}

func subSet(a, b []telegraf.Metric) bool {
	subset := true
	for _, m := range a {
		if !belongs(m, b) {
			subset = false
			break
		}
	}
	return subset
}

func equalSets(l1, l2 []telegraf.Metric) bool {
	return subSet(l1, l2) && subSet(l2, l1)
}

func runAndCompare(topk *TopK, metrics, answer []telegraf.Metric, testID string, t *testing.T) {
	// Sleep for `period`, otherwise the processor will only
	// cache the metrics, but it will not process them
	time.Sleep(time.Duration(topk.Period))

	// Run the processor
	ret := topk.Apply(metrics...)
	topk.Reset()

	// The returned set mut be equal to the answer set
	if !equalSets(ret, answer) {
		t.Error("\nExpected metrics for", testID, ":\n",
			answer, "\nReturned metrics:\n", ret)
	}
}

// Smoke tests
func TestTopkAggregatorsSmokeTests(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.Fields = []string{"a"}
	topk.GroupBy = []string{"tag_name"}

	aggregators := []string{"mean", "sum", "max", "min"}

	// The answer is equal to the original set for these particular scenarios
	input := MetricsSet1
	answer := MetricsSet1

	for _, ag := range aggregators {
		topk.Aggregation = ag

		runAndCompare(&topk, input, answer, "SmokeAggregator_"+ag, t)
	}
}

// AddAggregateFields + Mean aggregator
func TestTopkMeanAddAggregateFields(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.Aggregation = "mean"
	topk.AddAggregateFields = []string{"a"}
	topk.Fields = []string{"a"}
	topk.GroupBy = []string{"tag_name"}

	// Get the input
	input := deepCopy(MetricsSet1)

	// Generate the answer
	chng := fieldList(field{"a_topk_aggregate", float64(28.044)})
	changeSet := map[int]metricChange{
		0: {newFields: chng},
		1: {newFields: chng},
		2: {newFields: chng},
		3: {newFields: chng},
		4: {newFields: chng},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "MeanAddAggregateFields test", t)
}

// AddAggregateFields + Sum aggregator
func TestTopkSumAddAggregateFields(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.Aggregation = "sum"
	topk.AddAggregateFields = []string{"a"}
	topk.Fields = []string{"a"}
	topk.GroupBy = []string{"tag_name"}

	// Get the input
	input := deepCopy(MetricsSet1)

	// Generate the answer
	chng := fieldList(field{"a_topk_aggregate", float64(140.22)})
	changeSet := map[int]metricChange{
		0: {newFields: chng},
		1: {newFields: chng},
		2: {newFields: chng},
		3: {newFields: chng},
		4: {newFields: chng},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "SumAddAggregateFields test", t)
}

// AddAggregateFields + Max aggregator
func TestTopkMaxAddAggregateFields(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.Aggregation = "max"
	topk.AddAggregateFields = []string{"a"}
	topk.Fields = []string{"a"}
	topk.GroupBy = []string{"tag_name"}

	// Get the input
	input := deepCopy(MetricsSet1)

	// Generate the answer
	chng := fieldList(field{"a_topk_aggregate", float64(50.5)})
	changeSet := map[int]metricChange{
		0: {newFields: chng},
		1: {newFields: chng},
		2: {newFields: chng},
		3: {newFields: chng},
		4: {newFields: chng},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "MaxAddAggregateFields test", t)
}

// AddAggregateFields + Min aggregator
func TestTopkMinAddAggregateFields(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.Aggregation = "min"
	topk.AddAggregateFields = []string{"a"}
	topk.Fields = []string{"a"}
	topk.GroupBy = []string{"tag_name"}

	// Get the input
	input := deepCopy(MetricsSet1)

	// Generate the answer
	chng := fieldList(field{"a_topk_aggregate", float64(0.3)})
	changeSet := map[int]metricChange{
		0: {newFields: chng},
		1: {newFields: chng},
		2: {newFields: chng},
		3: {newFields: chng},
		4: {newFields: chng},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "MinAddAggregateFields test", t)
}

// GroupBy
func TestTopkGroupby1(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 3
	topk.Aggregation = "sum"
	topk.AddAggregateFields = []string{"value"}
	topk.GroupBy = []string{"tag[13]"}

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	changeSet := map[int]metricChange{
		2: {newFields: fieldList(field{"value_topk_aggregate", float64(74.18)})},
		3: {newFields: fieldList(field{"value_topk_aggregate", float64(72)})},
		4: {newFields: fieldList(field{"value_topk_aggregate", float64(163.22)})},
		5: {newFields: fieldList(field{"value_topk_aggregate", float64(163.22)})},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupBy test 1", t)
}
func TestTopkGroupby2(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 3
	topk.Aggregation = "mean"
	topk.AddAggregateFields = []string{"value"}
	topk.GroupBy = []string{"tag1"}

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	chng1 := fieldList(field{"value_topk_aggregate", float64(66.805)})
	chng2 := fieldList(field{"value_topk_aggregate", float64(72)})
	chng3 := fieldList(field{"value_topk_aggregate", float64(81.61)})
	changeSet := map[int]metricChange{
		1: {newFields: chng1},
		2: {newFields: chng1},
		3: {newFields: chng2},
		4: {newFields: chng3},
		5: {newFields: chng3},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupBy test 2", t)
}
func TestTopkGroupby3(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 1
	topk.Aggregation = "min"
	topk.AddAggregateFields = []string{"value"}
	topk.GroupBy = []string{"tag4"}

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	chng := fieldList(field{"value_topk_aggregate", float64(75.3)})
	changeSet := map[int]metricChange{
		4: {newFields: chng},
		5: {newFields: chng},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupBy test 3", t)
}

// GroupBy + Fields
func TestTopkGroupbyFields1(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 4 // This settings generate less than 3 groups
	topk.Aggregation = "mean"
	topk.AddAggregateFields = []string{"A"}
	topk.GroupBy = []string{"tag1", "tag2"}
	topk.Fields = []string{"A"}

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	changeSet := map[int]metricChange{
		0: {newFields: fieldList(field{"A_topk_aggregate", float64(95.36)})},
		1: {newFields: fieldList(field{"A_topk_aggregate", float64(39.01)})},
		2: {newFields: fieldList(field{"A_topk_aggregate", float64(39.01)})},
		5: {newFields: fieldList(field{"A_topk_aggregate", float64(29.45)})},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupBy Fields test 1", t)
}

func TestTopkGroupbyFields2(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 2
	topk.Aggregation = "sum"
	topk.AddAggregateFields = []string{"B", "C"}
	topk.GroupBy = []string{"tag1", "tag3"}
	topk.Fields = []string{"B", "C"}

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	changeSet := map[int]metricChange{
		0: {newFields: fieldList(field{"C_topk_aggregate", float64(72.41)})},
		2: {newFields: fieldList(field{"B_topk_aggregate", float64(60.96)})},
		4: {newFields: fieldList(field{"B_topk_aggregate", float64(81.55)}, field{"C_topk_aggregate", float64(49.96)})},
		5: {newFields: fieldList(field{"C_topk_aggregate", float64(49.96)})},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupBy Fields test 2", t)
}

// GroupBy metric name
func TestTopkGroupbyMetricName1(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 1
	topk.Aggregation = "sum"
	topk.AddAggregateFields = []string{"value"}
	topk.GroupBy = make([]string, 0)

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	chng := fieldList(field{"value_topk_aggregate", float64(235.22000000000003)})
	changeSet := map[int]metricChange{
		3: {newFields: chng},
		4: {newFields: chng},
		5: {newFields: chng},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupBy by metric name test 1", t)
}

func TestTopkGroupbyMetricName2(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 2
	topk.Aggregation = "sum"
	topk.AddAggregateFields = []string{"A", "value"}
	topk.GroupBy = []string{"tag[12]"}
	topk.Fields = []string{"A", "value"}

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	changeSet := map[int]metricChange{
		0: {newFields: fieldList(field{"A_topk_aggregate", float64(95.36)})},
		1: {newFields: fieldList(field{"A_topk_aggregate", float64(78.02)}, field{"value_topk_aggregate", float64(133.61)})},
		2: {newFields: fieldList(field{"A_topk_aggregate", float64(78.02)}, field{"value_topk_aggregate", float64(133.61)})},
		4: {newFields: fieldList(field{"value_topk_aggregate", float64(87.92)})},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupBy by metric name test 2", t)
}

// BottomK
func TestTopkBottomk(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 3
	topk.Aggregation = "sum"
	topk.GroupBy = []string{"tag1", "tag3"}
	topk.Bottomk = true

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	changeSet := map[int]metricChange{
		0: {},
		1: {},
		3: {},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "Bottom k test", t)
}

// GroupByKeyTag
func TestTopkGroupByKeyTag(t *testing.T) {
	// Build the processor
	topk := *New()
	topk.Period = tenMillisecondsDuration
	topk.K = 3
	topk.Aggregation = "sum"
	topk.GroupBy = []string{"tag1", "tag3"}
	topk.AddGroupByTag = "gbt"

	// Get the input
	input := deepCopy(MetricsSet2)

	// Generate the answer
	changeSet := map[int]metricChange{
		2: {newTags: tagList(tag{"gbt", "metric1&tag1=TWO&tag3=SIX&"})},
		3: {newTags: tagList(tag{"gbt", "metric2&tag1=ONE&tag3=THREE&"})},
		4: {newTags: tagList(tag{"gbt", "metric2&tag1=TWO&tag3=SEVEN&"})},
		5: {newTags: tagList(tag{"gbt", "metric2&tag1=TWO&tag3=SEVEN&"})},
	}
	answer := generateAns(input, changeSet)

	// Run the test
	runAndCompare(&topk, input, answer, "GroupByKeyTag test", t)
}

func TestTracking(t *testing.T) {
	inputRaw := []telegraf.Metric{
		metric.New("foo", map[string]string{}, map[string]interface{}{"value": 100}, time.Unix(0, 0)),
		metric.New("bar", map[string]string{}, map[string]interface{}{"value": 22}, time.Unix(0, 0)),
		metric.New("baz", map[string]string{}, map[string]interface{}{"value": 1}, time.Unix(0, 0)),
	}

	var mu sync.Mutex
	delivered := make([]telegraf.DeliveryInfo, 0, len(inputRaw))
	notify := func(di telegraf.DeliveryInfo) {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, di)
	}

	input := make([]telegraf.Metric, 0, len(inputRaw))
	for _, m := range inputRaw {
		tm, _ := metric.WithTracking(m, notify)
		input = append(input, tm)
	}

	expected := []telegraf.Metric{
		metric.New(
			"foo",
			map[string]string{},
			map[string]interface{}{"value": 100},
			time.Unix(0, 0),
		),
		metric.New(
			"bar",
			map[string]string{},
			map[string]interface{}{"value": 22},
			time.Unix(0, 0),
		),
		metric.New(
			"baz",
			map[string]string{},
			map[string]interface{}{"value": 1},
			time.Unix(0, 0),
		),
	}

	// Only doing this over 1 period, so we should expect the same number of
	// metrics back.
	plugin := &TopK{
		Period:      1,
		K:           3,
		Aggregation: "mean",
		Fields:      []string{"value"},
		Log:         testutil.Logger{},
	}
	plugin.Reset()

	// Process expected metrics and compare with resulting metrics
	var actual []telegraf.Metric
	require.Eventuallyf(t, func() bool {
		actual = plugin.Apply(input...)
		return len(actual) > 0
	}, time.Second, 100*time.Millisecond, "never got any metrics")
	testutil.RequireMetricsEqual(t, expected, actual)

	// Simulate output acknowledging delivery
	for _, m := range actual {
		m.Accept()
	}

	// Check delivery
	require.Eventuallyf(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(input) == len(delivered)
	}, time.Second, 100*time.Millisecond, "%d delivered but %d expected", len(delivered), len(expected))
}
