package unpivot

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	unpivot := &Unpivot{}
	require.NoError(t, unpivot.Init())
	require.Equal(t, "tag", unpivot.FieldNameAs)
	require.Equal(t, "name", unpivot.TagKey)
	require.Equal(t, "value", unpivot.ValueKey)
}

func TestInvalidMetricMode(t *testing.T) {
	unpivot := &Unpivot{FieldNameAs: "unknown"}
	require.Error(t, unpivot.Init())
}

func TestOriginalMode(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		tagKey   string
		valueKey string

		metrics  []telegraf.Metric
		expected []telegraf.Metric
	}{
		{
			name:     "simple",
			tagKey:   "name",
			valueKey: "value",
			metrics: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"idle_time": int64(42),
					},
					now,
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"name": "idle_time",
					},
					map[string]interface{}{
						"value": int64(42),
					},
					now,
				),
			},
		},
		{
			name:     "multi fields",
			tagKey:   "name",
			valueKey: "value",
			metrics: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"idle_time": int64(42),
						"idle_user": int64(43),
					},
					now,
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"name": "idle_time",
					},
					map[string]interface{}{
						"value": int64(42),
					},
					now,
				),
				testutil.MustMetric("cpu",
					map[string]string{
						"name": "idle_user",
					},
					map[string]interface{}{
						"value": int64(43),
					},
					now,
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Unpivot{
				TagKey:   tt.tagKey,
				ValueKey: tt.valueKey,
			}
			require.NoError(t, plugin.Init())

			actual := plugin.Apply(tt.metrics...)
			testutil.RequireMetricsEqual(t, tt.expected, actual, testutil.SortMetrics())
		})
	}
}

func TestFieldMode(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		fieldNameAs string
		tagKey      string
		valueKey    string
		metrics     []telegraf.Metric
		expected    []telegraf.Metric
	}{
		{
			name:        "simple",
			fieldNameAs: "metric",
			tagKey:      "name",
			valueKey:    "value",
			metrics: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"idle_time": int64(42),
					},
					now,
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("idle_time",
					map[string]string{},
					map[string]interface{}{
						"value": int64(42),
					},
					now,
				),
			},
		},
		{
			name:        "multi fields",
			fieldNameAs: "metric",
			tagKey:      "name",
			valueKey:    "value",
			metrics: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"idle_time": int64(42),
						"idle_user": int64(43),
					},
					now,
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("idle_time",
					map[string]string{},
					map[string]interface{}{
						"value": int64(42),
					},
					now,
				),
				testutil.MustMetric("idle_user",
					map[string]string{},
					map[string]interface{}{
						"value": int64(43),
					},
					now,
				),
			},
		},
		{
			name:        "multi fields and tags",
			fieldNameAs: "metric",
			tagKey:      "name",
			valueKey:    "value",
			metrics: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"building": "5a",
					},
					map[string]interface{}{
						"idle_time": int64(42),
						"idle_user": int64(43),
					},
					now,
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("idle_time",
					map[string]string{
						"building": "5a",
					},
					map[string]interface{}{
						"value": int64(42),
					},
					now,
				),
				testutil.MustMetric("idle_user",
					map[string]string{
						"building": "5a",
					},
					map[string]interface{}{
						"value": int64(43),
					},
					now,
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Unpivot{
				FieldNameAs: tt.fieldNameAs,
				TagKey:      tt.tagKey,
				ValueKey:    tt.valueKey,
			}
			require.NoError(t, plugin.Init())

			actual := plugin.Apply(tt.metrics...)
			testutil.RequireMetricsEqual(t, tt.expected, actual, testutil.SortMetrics())
		})
	}
}

func TestTrackedMetricNotLost(t *testing.T) {
	var mu sync.Mutex
	delivered := make([]telegraf.DeliveryInfo, 0, 3)
	notify := func(di telegraf.DeliveryInfo) {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, di)
	}
	input := make([]telegraf.Metric, 0, 3)
	expected := make([]telegraf.Metric, 0, 6)
	for i := 0; i < 3; i++ {
		strI := strconv.Itoa(i)

		m := metric.New("m"+strI, map[string]string{}, map[string]interface{}{"x": int64(1), "y": int64(2)}, time.Unix(0, 0))
		tm, _ := metric.WithTracking(m, notify)
		input = append(input, tm)

		unpivot1 := metric.New("m"+strI, map[string]string{"name": "x"}, map[string]interface{}{"value": int64(1)}, time.Unix(0, 0))
		unpivot2 := metric.New("m"+strI, map[string]string{"name": "y"}, map[string]interface{}{"value": int64(2)}, time.Unix(0, 0))
		expected = append(expected, unpivot1, unpivot2)
	}

	// Process expected metrics and compare with resulting metrics
	plugin := &Unpivot{TagKey: "name", ValueKey: "value"}
	require.NoError(t, plugin.Init())

	actual := plugin.Apply(input...)
	testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics())

	// Simulate output acknowledging delivery
	for _, m := range actual {
		m.Accept()
	}

	// Check delivery
	require.Eventuallyf(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(input) == len(delivered)
	}, time.Second, 100*time.Millisecond, "%d delivered but %d expected", len(delivered), len(input))
}

func BenchmarkAsTag(b *testing.B) {
	input := metric.New(
		"test",
		map[string]string{
			"source":   "device A",
			"location": "main building",
		},
		map[string]interface{}{
			"field0": 0.1,
			"field1": 1.2,
			"field2": 2.3,
			"field3": 3.4,
			"field4": 4.5,
			"field5": 5.6,
			"field6": 6.7,
			"field7": 7.8,
			"field8": 8.9,
			"field9": 9.0,
		},
		time.Now(),
	)

	plugin := &Unpivot{}
	require.NoError(b, plugin.Init())

	for n := 0; n < b.N; n++ {
		plugin.Apply(input)
	}
}

func BenchmarkAsMetric(b *testing.B) {
	input := metric.New(
		"test",
		map[string]string{
			"source":   "device A",
			"location": "main building",
		},
		map[string]interface{}{
			"field0": 0.1,
			"field1": 1.2,
			"field2": 2.3,
			"field3": 3.4,
			"field4": 4.5,
			"field5": 5.6,
			"field6": 6.7,
			"field7": 7.8,
			"field8": 8.9,
			"field9": 9.0,
		},
		time.Now(),
	)

	plugin := &Unpivot{FieldNameAs: "metric"}
	require.NoError(b, plugin.Init())

	for n := 0; n < b.N; n++ {
		plugin.Apply(input)
	}
}
