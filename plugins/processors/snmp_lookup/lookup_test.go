package snmp_lookup

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/snmp"
	"github.com/influxdata/telegraf/plugins/common/parallel"
	si "github.com/influxdata/telegraf/plugins/inputs/snmp"
	"github.com/influxdata/telegraf/plugins/processors"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestRegistry(t *testing.T) {
	require.Contains(t, processors.Processors, "snmp_lookup")
	require.IsType(t, &Lookup{}, processors.Processors["snmp_lookup"]())
}

func TestInit(t *testing.T) {
	tests := []struct {
		name     string
		plugin   Lookup
		expected string
	}{
		{
			name: "empty",
		},
		{
			name: "defaults",
			plugin: Lookup{
				AgentTag:        "source",
				IndexTag:        "index",
				CacheSize:       defaultCacheSize,
				ParallelLookups: defaultParallelLookups,
				ClientConfig:    *snmp.DefaultClientConfig(),
				CacheTTL:        defaultCacheTTL,
			},
		},
		{
			name: "wrong SNMP client config",
			plugin: Lookup{
				ClientConfig: snmp.ClientConfig{
					Version: 99,
				},
			},
			expected: "parsing SNMP client config: invalid version",
		},
		{
			name: "table init",
			plugin: Lookup{
				Tags: []si.Field{
					{
						Name: "ifName",
						Oid:  ".1.3.6.1.2.1.31.1.1.1.1",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.plugin.Log = testutil.Logger{}

			if tt.expected == "" {
				require.NoError(t, tt.plugin.Init())
			} else {
				require.ErrorContains(t, tt.plugin.Init(), tt.expected)
			}
		})
	}
}

func TestSetTranslator(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{name: "gosmi"},
		{name: "netsnmp"},
		{
			name:     "unknown",
			expected: `invalid agent.snmp_translator value "unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Lookup{Log: testutil.Logger{}}

			p.SetTranslator(tt.name)
			require.Equal(t, tt.name, p.Translator)

			if tt.expected == "" {
				require.NoError(t, p.Init())
			} else {
				require.ErrorContains(t, p.Init(), tt.expected)
			}
		})
	}
}

func TestStart(t *testing.T) {
	acc := &testutil.NopAccumulator{}
	p := Lookup{}
	require.NoError(t, p.Init())
	defer p.Stop()

	p.Ordered = true
	require.NoError(t, p.Start(acc))
	require.IsType(t, &parallel.Ordered{}, p.parallel)
	p.Stop()

	p.Ordered = false
	require.NoError(t, p.Start(acc))
	require.IsType(t, &parallel.Unordered{}, p.parallel)
}

func TestGetConnection(t *testing.T) {
	tests := []struct {
		name     string
		input    telegraf.Metric
		expected string
	}{
		{
			name: "agent error",
			input: testutil.MustMetric(
				"test",
				map[string]string{
					"source": "test://127.0.0.1",
				},
				map[string]interface{}{},
				time.Unix(0, 0),
			),
			expected: "parsing agent tag: unsupported scheme: test",
		},
	}

	p := Lookup{
		AgentTag:     "source",
		ClientConfig: *snmp.DefaultClientConfig(),
		Log:          testutil.Logger{},
	}

	require.NoError(t, p.Init())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.getConnection(tt.input)

			if tt.expected == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.expected)
			}
		})
	}
}

func TestAddAsync(t *testing.T) {
	tests := []struct {
		name     string
		input    telegraf.Metric
		expected []telegraf.Metric
	}{
		{
			name:     "no source tag",
			input:    testutil.MockMetrics()[0],
			expected: testutil.MockMetrics(),
		},
		{
			name: "no index tag",
			input: testutil.MustMetric(
				"test",
				map[string]string{
					"source": "127.0.0.1",
				},
				map[string]interface{}{},
				time.Unix(0, 0),
			),
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"source": "127.0.0.1",
					},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "cached",
			input: testutil.MustMetric(
				"test",
				map[string]string{
					"source": "127.0.0.1",
					"index":  "123",
				},
				map[string]interface{}{},
				time.Unix(0, 0),
			),
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"source": "127.0.0.1",
						"index":  "123",
						"ifName": "eth123",
					},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "non-existing index",
			input: testutil.MustMetric(
				"test",
				map[string]string{
					"source": "127.0.0.1",
					"index":  "999",
				},
				map[string]interface{}{},
				time.Unix(0, 0),
			),
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"source": "127.0.0.1",
						"index":  "999",
					},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
		},
	}

	acc := &testutil.NopAccumulator{}
	p := Lookup{
		AgentTag:        "source",
		IndexTag:        "index",
		CacheSize:       defaultCacheSize,
		ParallelLookups: defaultParallelLookups,
		ClientConfig:    *snmp.DefaultClientConfig(),
		CacheTTL:        defaultCacheTTL,
		Log:             testutil.Logger{},
	}

	require.NoError(t, p.Init())
	require.NoError(t, p.Start(acc))
	defer p.Stop()

	// Add sample data
	p.cache.Add("127.0.0.1", tagMap{rows: map[string]map[string]string{"123": {"ifName": "eth123"}}})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.RequireMetricsEqual(t, tt.expected, p.addAsync(tt.input))
		})
	}
}