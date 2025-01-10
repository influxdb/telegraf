package dedup

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

func TestMetrics(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		input        []telegraf.Metric
		expected     []telegraf.Metric
		cacheContent []telegraf.Metric
	}{
		{
			name: "retain metric",
			input: []telegraf.Metric{
				metric.New("m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
			expected: []telegraf.Metric{
				metric.New("m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
			cacheContent: []telegraf.Metric{
				metric.New("m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
		},
		{
			name: "suppress repeated metric",
			input: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Second),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
			expected: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Second),
				),
			},
			cacheContent: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Second),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Second),
				),
			},
		},
		{
			name: "pass updated metric",
			input: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Second),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 2},
					now,
				),
			},
			expected: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Second),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 2},
					now,
				),
			},
			cacheContent: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Second),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 2},
					now,
				),
			},
		},
		{
			name: "pass after cache expired",
			input: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
			expected: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
			cacheContent: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-1*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
		},
		{
			name: "cache retains metrics",
			input: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-3*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-2*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
			expected: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-3*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-2*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
			cacheContent: []telegraf.Metric{
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-3*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now.Add(-2*time.Hour),
				),
				metric.New(
					"m1",
					map[string]string{"tag": "tag_value"},
					map[string]interface{}{"value": 1},
					now,
				),
			},
		},
		{
			name: "same timestamp",
			input: []telegraf.Metric{
				metric.New("metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"foo": 1}, // field
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"bar": 1}, // different field
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"bar": 2}, // same field different value
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"bar": 2}, // same field same value
					now,
				),
			},
			expected: []telegraf.Metric{
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"foo": 1},
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"bar": 1},
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"bar": 2},
					now,
				),
			},
			cacheContent: []telegraf.Metric{
				metric.New("metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"foo": 1},
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"foo": 1, "bar": 1},
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"bar": 2},
					now,
				),
				metric.New(
					"metric",
					map[string]string{"tag": "value"},
					map[string]interface{}{"bar": 2},
					now,
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plugin instance
			plugin := &Dedup{
				DedupInterval: config.Duration(10 * time.Minute),
				FlushTime:     now.Add(-1 * time.Second),
				Cache:         make(map[uint64]telegraf.Metric),
			}

			// Feed the input metrics and record the outputs
			var actual []telegraf.Metric
			for i, m := range tt.input {
				actual = append(actual, plugin.Apply(m)...)

				// Check the cache content
				if cm := tt.cacheContent[i]; cm == nil {
					require.Empty(t, plugin.Cache)
				} else {
					id := m.HashID()
					require.NotEmpty(t, plugin.Cache)
					require.Contains(t, plugin.Cache, id)
					testutil.RequireMetricEqual(t, cm, plugin.Cache[id])
				}
			}

			// Check if we got the expected metrics
			testutil.RequireMetricsEqual(t, tt.expected, actual)
		})
	}
}

func TestCacheShrink(t *testing.T) {
	now := time.Now()

	// Time offset is more than 2 * DedupInterval
	plugin := &Dedup{
		DedupInterval: config.Duration(10 * time.Minute),
		FlushTime:     now.Add(-2 * time.Hour),
		Cache:         make(map[uint64]telegraf.Metric),
	}

	// Time offset is more than 1 * DedupInterval
	input := []telegraf.Metric{
		metric.New(
			"m1",
			map[string]string{"tag": "tag_value"},
			map[string]interface{}{"value": 1},
			now.Add(-1*time.Hour),
		),
	}
	actual := plugin.Apply(input...)
	expected := input
	testutil.RequireMetricsEqual(t, expected, actual)
	require.Empty(t, plugin.Cache)
}

func TestTracking(t *testing.T) {
	now := time.Now()

	inputRaw := []telegraf.Metric{
		metric.New("metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 1},
			now.Add(-2*time.Second),
		),
		metric.New("metric",
			map[string]string{"tag": "pass"},
			map[string]interface{}{"foo": 1},
			now.Add(-2*time.Second),
		),
		metric.New("metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 1},
			now.Add(-1*time.Second),
		),
		metric.New("metric",
			map[string]string{"tag": "pass"},
			map[string]interface{}{"foo": 1},
			now.Add(-1*time.Second),
		),
		metric.New(
			"metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 3},
			now,
		),
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
		metric.New("metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 1},
			now.Add(-2*time.Second),
		),
		metric.New("metric",
			map[string]string{"tag": "pass"},
			map[string]interface{}{"foo": 1},
			now.Add(-2*time.Second),
		),
		metric.New(
			"metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 3},
			now,
		),
	}

	// Create plugin instance
	plugin := &Dedup{
		DedupInterval: config.Duration(10 * time.Minute),
		FlushTime:     now.Add(-1 * time.Second),
		Cache:         make(map[uint64]telegraf.Metric),
	}

	// Process expected metrics and compare with resulting metrics
	actual := plugin.Apply(input...)
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

func TestStatePersistence(t *testing.T) {
	now := time.Now()

	// Define the metrics and states
	state := fmt.Sprintf("metric,tag=value foo=1i %d\n", now.Add(-1*time.Minute).UnixNano())
	input := []telegraf.Metric{
		metric.New("metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 1},
			now.Add(-2*time.Second),
		),
		metric.New("metric",
			map[string]string{"tag": "pass"},
			map[string]interface{}{"foo": 1},
			now.Add(-1*time.Second),
		),
		metric.New(
			"metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 3},
			now,
		),
	}

	expected := []telegraf.Metric{
		metric.New("metric",
			map[string]string{"tag": "pass"},
			map[string]interface{}{"foo": 1},
			now.Add(-1*time.Second),
		),
		metric.New(
			"metric",
			map[string]string{"tag": "value"},
			map[string]interface{}{"foo": 3},
			now,
		),
	}
	expectedState := []string{
		fmt.Sprintf("metric,tag=pass foo=1i %d\n", now.Add(-1*time.Second).UnixNano()),
		fmt.Sprintf("metric,tag=value foo=3i %d\n", now.UnixNano()),
	}

	// Configure the plugin
	plugin := &Dedup{
		DedupInterval: config.Duration(10 * time.Hour), // use a long interval to avoid flaky tests
		FlushTime:     now.Add(-1 * time.Second),
		Cache:         make(map[uint64]telegraf.Metric),
	}
	require.Empty(t, plugin.Cache)

	// Setup the "persisted" state
	var pi telegraf.StatefulPlugin = plugin
	require.NoError(t, pi.SetState([]byte(state)))
	require.Len(t, plugin.Cache, 1)

	// Process expected metrics and compare with resulting metrics
	actual := plugin.Apply(input...)
	testutil.RequireMetricsEqual(t, expected, actual)

	// Check getting the persisted state
	// Because the cache is a map, the order of metrics in the state is not
	// guaranteed, so check the string contents regardless of the order.
	actualState, ok := pi.GetState().([]byte)
	require.True(t, ok, "state is not a bytes array")
	var expectedLen int
	for _, m := range expectedState {
		require.Contains(t, string(actualState), m)
		expectedLen += len(m)
	}
	require.Len(t, actualState, expectedLen)
}
