package starlark

import (
	"bytes"
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	common "github.com/influxdata/telegraf/plugins/common/starlark"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

// Tests for runtime errors in the processors Init function.
func TestInitError(t *testing.T) {
	tests := []struct {
		name      string
		constants map[string]interface{}
		plugin    *Starlark
	}{
		{
			name:   "source must define apply",
			plugin: newStarlarkFromSource(""),
		},
		{
			name: "apply must be a function",
			plugin: newStarlarkFromSource(`
apply = 42
`),
		},
		{
			name: "apply function must take one arg",
			plugin: newStarlarkFromSource(`
def apply():
	pass
`),
		},
		{
			name: "package scope must have valid syntax",
			plugin: newStarlarkFromSource(`
for
`),
		},
		{
			name:   "no source no script",
			plugin: newStarlarkNoScript(),
		},
		{
			name: "source and script",
			plugin: newStarlarkFromSource(`
def apply():
	pass
`),
		},
		{
			name:   "script file not found",
			plugin: newStarlarkFromScript("testdata/file_not_found.star"),
		},
		{
			name: "source and script",
			plugin: newStarlarkFromSource(`
def apply(metric):
	metric.fields["p1"] = unsupported_type
	return metric
`),
			constants: map[string]interface{}{
				"unsupported_type": time.Now(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.plugin.Constants = tt.constants
			err := tt.plugin.Init()
			require.Error(t, err)
		})
	}
}

func TestApply(t *testing.T) {
	// Tests for the behavior of the processors Apply function.
	var applyTests = []struct {
		name             string
		source           string
		input            []telegraf.Metric
		expected         []telegraf.Metric
		expectedErrorStr string
	}{
		{
			name: "drop metric",
			source: `
def apply(metric):
	return None
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "passthrough",
			source: `
def apply(metric):
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "read value from global scope",
			source: `
names = {
	'cpu': 'cpu2',
	'mem': 'mem2',
}

def apply(metric):
	metric.name = names[metric.name]
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu2",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "cannot write to frozen global scope",
			source: `
cache = []

def apply(metric):
	cache.append(deepcopy(metric))
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 1.0,
					},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "append: cannot append to frozen list",
		},
		{
			name: "cannot return multiple references to same metric",
			source: `
def apply(metric):
	# Should be return [metric, deepcopy(metric)]
	return [metric, metric]
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
	}

	for _, tt := range applyTests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := newStarlarkFromSource(tt.source)
			err := plugin.Init()
			require.NoError(t, err)

			var acc testutil.Accumulator

			err = plugin.Start(&acc)
			require.NoError(t, err)

			for _, m := range tt.input {
				err = plugin.Add(m, &acc)
				if tt.expectedErrorStr != "" {
					require.EqualError(t, err, tt.expectedErrorStr)
				} else {
					require.NoError(t, err)
				}
			}

			plugin.Stop()
			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics())
		})
	}
}

// Tests for the behavior of the Metric type.
func TestMetric(t *testing.T) {
	var tests = []struct {
		name             string
		source           string
		constants        map[string]interface{}
		input            []telegraf.Metric
		expected         []telegraf.Metric
		expectedErrorStr string
	}{
		{
			name: "create new metric",
			source: `
def apply(metric):
	m = Metric('cpu')
	m.fields['time_guest'] = 2.0
	m.time = 0
	return m
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest": 2.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "deepcopy",
			source: `
def apply(metric):
	return [metric, deepcopy(metric)]
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set name",
			source: `
def apply(metric):
	metric.name = "howdy"
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("howdy",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set name wrong type",
			source: `
def apply(metric):
	metric.name = 42
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "type error",
		},
		{
			name: "get name",
			source: `
def apply(metric):
	metric.tags['measurement'] = metric.name
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"measurement": "cpu",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "getattr tags",
			source: `
def apply(metric):
	metric.tags
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "setattr tags is not allowed",
			source: `
def apply(metric):
	metric.tags = {}
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot set tags",
		},
		{
			name: "empty tags are false",
			source: `
def apply(metric):
	if not metric.tags:
		return metric
	return None
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "non-empty tags are true",
			source: `
def apply(metric):
	if metric.tags:
		return metric
	return None
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tags in operator",
			source: `
def apply(metric):
	if 'host' not in metric.tags:
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup tag",
			source: `
def apply(metric):
	metric.tags['result'] = metric.tags['host']
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host":   "example.org",
						"result": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup tag not set",
			source: `
def apply(metric):
	metric.tags['foo']
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: `key "foo" not in Tags`,
		},
		{
			name: "get tag",
			source: `
def apply(metric):
	metric.tags['result'] = metric.tags.get('host')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host":   "example.org",
						"result": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "get tag default",
			source: `
def apply(metric):
	metric.tags['result'] = metric.tags.get('foo', 'example.org')
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"result": "example.org",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "get tag not set returns none",
			source: `
def apply(metric):
	if metric.tags.get('foo') != None:
		return
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set tag",
			source: `
def apply(metric):
	metric.tags['host'] = 'example.org'
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set tag type error",
			source: `
def apply(metric):
	metric.tags['host'] = 42
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "tag value must be of type 'str'",
		},
		{
			name: "pop tag",
			source: `
def apply(metric):
	metric.tags['host2'] = metric.tags.pop('host')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host2": "example.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "pop tag (default)",
			source: `
def apply(metric):
	metric.tags['host2'] = metric.tags.pop('url', 'foo.org')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"url":  "bar.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host":  "example.org",
						"host2": "foo.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
				testutil.MustMetric("cpu",
					map[string]string{
						"host":  "example.org",
						"host2": "bar.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "popitem tags",
			source: `
def apply(metric):
	metric.tags['result'] = '='.join(metric.tags.popitem())
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"result": "host=example.org",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "popitem tags empty dict",
			source: `
def apply(metric):
	metric.tags.popitem()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "popitem(): tag dictionary is empty",
		},
		{
			name: "tags setdefault key not set",
			source: `
def apply(metric):
	metric.tags.setdefault('a', 'b')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tags setdefault key already set",
			source: `
def apply(metric):
	metric.tags.setdefault('a', 'c')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tags update list of tuple",
			source: `
def apply(metric):
	metric.tags.update([('b', 'y'), ('c', 'z')])
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
						"b": "y",
						"c": "z",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tags update kwargs",
			source: `
def apply(metric):
	metric.tags.update(b='y', c='z')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
						"b": "y",
						"c": "z",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tags update dict",
			source: `
def apply(metric):
	metric.tags.update({'b': 'y', 'c': 'z'})
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
						"b": "y",
						"c": "z",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tags update list tuple and kwargs",
			source: `
def apply(metric):
	metric.tags.update([('b', 'y'), ('c', 'z')], d='zz')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "x",
						"b": "y",
						"c": "z",
						"d": "zz",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tags",
			source: `
def apply(metric):
	for k in metric.tags:
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
						"foo":  "bar",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
						"foo":  "bar",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tags and copy to fields",
			source: `
def apply(metric):
	for k in metric.tags:
		metric.fields[k] = k
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{
						"host":      "host",
						"cpu":       "cpu",
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tag keys",
			source: `
def apply(metric):
	for k in metric.tags.keys():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
						"foo":  "bar",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
						"foo":  "bar",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tag keys and copy to fields",
			source: `
def apply(metric):
	for k in metric.tags.keys():
		metric.fields[k] = k
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{
						"host":      "host",
						"cpu":       "cpu",
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tag items",
			source: `
def apply(metric):
	for k, v in metric.tags.items():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tag items and copy to fields",
			source: `
def apply(metric):
	for k, v in metric.tags.items():
		metric.fields[k] = v
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{
						"time_idle": 42,
						"host":      "example.org",
						"cpu":       "cpu0",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tag values",
			source: `
def apply(metric):
	for v in metric.tags.values():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tag values and copy to fields",
			source: `
def apply(metric):
	for v in metric.tags.values():
		metric.fields[v] = v
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
						"cpu":  "cpu0",
					},
					map[string]interface{}{
						"time_idle":   42,
						"example.org": "example.org",
						"cpu0":        "cpu0",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "clear tags",
			source: `
def apply(metric):
	metric.tags.clear()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tags cannot pop while iterating",
			source: `
def apply(metric):
	for k in metric.tags:
		metric.tags.pop(k)
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "pop: cannot delete during iteration",
		},
		{
			name: "tags cannot popitem while iterating",
			source: `
def apply(metric):
	for k in metric.tags:
		metric.tags.popitem()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot delete during iteration",
		},
		{
			name: "tags cannot clear while iterating",
			source: `
def apply(metric):
	for k in metric.tags:
		metric.tags.clear()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot delete during iteration",
		},
		{
			name: "tags cannot insert while iterating",
			source: `
def apply(metric):
	for k in metric.tags:
		metric.tags['i'] = 'j'
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot insert during iteration",
		},
		{
			name: "tags can be cleared after iterating",
			source: `
def apply(metric):
	for k in metric.tags:
		pass
	metric.tags.clear()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
					},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "getattr fields",
			source: `
def apply(metric):
	metric.fields
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "setattr fields is not allowed",
			source: `
def apply(metric):
	metric.fields = {}
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot set fields",
		},
		{
			name: "empty fields are false",
			source: `
def apply(metric):
	if not metric.fields:
		metric.fields["time_idle"] = 42
		return metric
	return None
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "non-empty fields are true",
			source: `
def apply(metric):
	if metric.fields:
		return metric
	return None
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "fields in operator",
			source: `
def apply(metric):
	if 'time_idle' not in metric.fields:
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup string field",
			source: `
def apply(metric):
	value = metric.fields['value']
	if value != "xyzzy" and type(value) != "str":
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": "xyzzy"},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": "xyzzy"},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup integer field",
			source: `
def apply(metric):
	value = metric.fields['value']
	if value != 42 and type(value) != "int":
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup unsigned field",
			source: `
def apply(metric):
	value = metric.fields['value']
	if value != 42 and type(value) != "int":
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": uint64(42)},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": uint64(42)},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup bool field",
			source: `
def apply(metric):
	value = metric.fields['value']
	if value != True and type(value) != "bool":
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": true},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": true},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup float field",
			source: `
def apply(metric):
	value = metric.fields['value']
	if value != 42.0 and type(value) != "float":
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "lookup field not set",
			source: `
def apply(metric):
	metric.fields['foo']
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: `key "foo" not in Fields`,
		},
		{
			name: "get field",
			source: `
def apply(metric):
	metric.fields['result'] = metric.fields.get('time_idle')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
						"result":    42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "get field default",
			source: `
def apply(metric):
	metric.fields['result'] = metric.fields.get('foo', 'example.org')
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
						"result":    "example.org",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "get field not set returns none",
			source: `
def apply(metric):
	if metric.fields.get('foo') != None:
		return
	return metric
		`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set string field",
			source: `
def apply(metric):
	metric.fields['host'] = 'example.org'
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"host": "example.org",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set integer field",
			source: `
def apply(metric):
	metric.fields['time_idle'] = 42
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set float field",
			source: `
def apply(metric):
	metric.fields['time_idle'] = 42.0
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set bool field",
			source: `
def apply(metric):
	metric.fields['time_idle'] = True
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": true,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set field type error",
			source: `
def apply(metric):
	metric.fields['time_idle'] = {}
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "invalid starlark type",
		},
		{
			name: "pop field",
			source: `
def apply(metric):
	time_idle = metric.fields.pop('time_idle')
	if time_idle != 0:
		return
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":  0,
						"time_guest": 0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_guest": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "pop field (default)",
			source: `
def apply(metric):
	metric.fields['idle_count'] = metric.fields.pop('count', 10)
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":  0,
						"time_guest": 0,
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":  0,
						"time_guest": 0,
						"count":      0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":  0,
						"time_guest": 0,
						"idle_count": 10,
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":  0,
						"time_guest": 0,
						"idle_count": 0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "popitem field",
			source: `
def apply(metric):
	item = metric.fields.popitem()
	if item != ("time_idle", 0):
		return
	metric.fields['time_guest'] = 0
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_guest": 0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "popitem fields empty dict",
			source: `
def apply(metric):
	metric.fields.popitem()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "popitem(): field dictionary is empty",
		},
		{
			name: "fields setdefault key not set",
			source: `
def apply(metric):
	metric.fields.setdefault('a', 'b')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"a": "b"},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "fields setdefault key already set",
			source: `
def apply(metric):
	metric.fields.setdefault('a', 'c')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"a": "b"},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"a": "b"},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "fields update list of tuple",
			source: `
def apply(metric):
	metric.fields.update([('a', 'b'), ('c', 'd')])
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "fields update kwargs",
			source: `
def apply(metric):
	metric.fields.update(a='b', c='d')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "fields update dict",
			source: `
def apply(metric):
	metric.fields.update({'a': 'b', 'c': 'd'})
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "fields update list tuple and kwargs",
			source: `
def apply(metric):
	metric.fields.update([('a', 'b'), ('c', 'd')], e='f')
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
						"e": "f",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate fields",
			source: `
def apply(metric):
	for k in metric.fields:
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate field keys",
			source: `
def apply(metric):
	for k in metric.fields.keys():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate field keys and copy to tags",
			source: `
def apply(metric):
	for k in metric.fields.keys():
		metric.tags[k] = k
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"time_guest":  "time_guest",
						"time_idle":   "time_idle",
						"time_system": "time_system",
					},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate field items",
			source: `
def apply(metric):
	for k, v in metric.fields.items():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.0,
						"time_idle":   2.0,
						"time_system": 3.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate field items and copy to tags",
			source: `
def apply(metric):
	for k, v in metric.fields.items():
		metric.tags[k] = str(v)
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_guest":  1.1,
						"time_idle":   2.1,
						"time_system": 3.1,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"time_guest":  "1.1",
						"time_idle":   "2.1",
						"time_system": "3.1",
					},
					map[string]interface{}{
						"time_guest":  1.1,
						"time_idle":   2.1,
						"time_system": 3.1,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate field values",
			source: `
def apply(metric):
	for v in metric.fields.values():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
						"e": "f",
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
						"e": "f",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate field values and copy to tags",
			source: `
def apply(metric):
	for v in metric.fields.values():
		metric.tags[str(v)] = str(v)
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
						"e": "f",
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"b": "b",
						"d": "d",
						"f": "f",
					},
					map[string]interface{}{
						"a": "b",
						"c": "d",
						"e": "f",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "clear fields",
			source: `
def apply(metric):
	metric.fields.clear()
	metric.fields['notempty'] = 0
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":   0,
						"time_guest":  0,
						"time_system": 0,
						"time_user":   0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"notempty": 0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "fields cannot pop while iterating",
			source: `
def apply(metric):
	for k in metric.fields:
		metric.fields.pop(k)
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "pop: cannot delete during iteration",
		},
		{
			name: "fields cannot popitem while iterating",
			source: `
def apply(metric):
	for k in metric.fields:
		metric.fields.popitem()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot delete during iteration",
		},
		{
			name: "fields cannot clear while iterating",
			source: `
def apply(metric):
	for k in metric.fields:
		metric.fields.clear()
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot delete during iteration",
		},
		{
			name: "fields cannot insert while iterating",
			source: `
def apply(metric):
	for k in metric.fields:
		metric.fields['time_guest'] = 0
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "cannot insert during iteration",
		},
		{
			name: "fields can be cleared after iterating",
			source: `
def apply(metric):
	for k in metric.fields:
		pass
	metric.fields.clear()
	metric.fields['notempty'] = 0
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"notempty": 0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set time",
			source: `
def apply(metric):
	metric.time = 42
	return metric
			`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0).UTC(),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 42).UTC(),
				),
			},
		},
		{
			name: "set time wrong type",
			source: `
def apply(metric):
	metric.time = 'howdy'
	return metric
			`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0).UTC(),
				),
			},
			expectedErrorStr: "type error",
		},
		{
			name: "get time",
			source: `
def apply(metric):
	metric.time -= metric.time % 100000000
	return metric
			`,
			input: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(42, 11).UTC(),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(42, 0).UTC(),
				),
			},
		},
		{
			name: "support errors",
			source: `
load("json.star", "json")

def apply(metric):
    msg = catch(lambda: process(metric))
    if msg != None:
	    metric.fields["error"] = msg
	    metric.fields["value"] = "default"
    return metric

def process(metric):
    metric.fields["field1"] = "value1"
    metric.tags["tags1"] = "value2"
    # Throw an error
    json.decode(metric.fields.get('value'))
    # Should never be called
    metric.fields["msg"] = "value4"
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"value": "non-json-content", "msg": "value3"},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{"tags1": "value2"},
					map[string]interface{}{
						"value":  "default",
						"field1": "value1",
						"msg":    "value3",
						"error":  "json.decode: at offset 0, unexpected character 'n'",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "support constants",
			source: `
def apply(metric):
    metric.fields["p1"] = max_size
    metric.fields["p2"] = threshold
    metric.fields["p3"] = default_name
    metric.fields["p4"] = debug_mode
    metric.fields["p5"] = supported_values[0]
    metric.fields["p6"] = supported_values[1]
    metric.fields["p7"] = supported_entries[2]
    metric.fields["p8"] = supported_entries["3"]
    return metric
           `,
			constants: map[string]interface{}{
				"max_size":         10,
				"threshold":        0.75,
				"default_name":     "Julia",
				"debug_mode":       true,
				"supported_values": []interface{}{2, "3"},
				"supported_entries": map[interface{}]interface{}{
					2:   "two",
					"3": "three",
				},
			},
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"p1": 10,
						"p2": 0.75,
						"p3": "Julia",
						"p4": true,
						"p5": 2,
						"p6": "3",
						"p7": "two",
						"p8": "three",
					},
					time.Unix(0, 0),
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := newStarlarkFromSource(tt.source)
			plugin.Constants = tt.constants
			err := plugin.Init()
			require.NoError(t, err)

			var acc testutil.Accumulator

			err = plugin.Start(&acc)
			require.NoError(t, err)

			for _, m := range tt.input {
				err = plugin.Add(m, &acc)
				if tt.expectedErrorStr != "" {
					require.ErrorContains(t, err, tt.expectedErrorStr)
				} else {
					require.NoError(t, err)
				}
			}

			plugin.Stop()
			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics())
		})
	}
}

// Tests the behavior of the plugin according the provided TOML configuration.
func TestConfig(t *testing.T) {
	var tests = []struct {
		name     string
		config   string
		input    []telegraf.Metric
		expected []telegraf.Metric
	}{
		{
			name: "support constants from configuration",
			config: `
[[processors.starlark]]
  source = '''
def apply(metric):
    metric.fields["p1"] = max_size
    metric.fields["p2"] = threshold
    metric.fields["p3"] = default_name
    metric.fields["p4"] = debug_mode
    metric.fields["p5"] = supported_values[0]
    metric.fields["p6"] = supported_values[1]
    metric.fields["p7"] = supported_entries["2"]
    metric.fields["p8"] = supported_entries["3"]
    return metric
'''
  [processors.starlark.constants]
    max_size = 10
    threshold = 0.75
    default_name = "Elsa"
	debug_mode = true
	supported_values = ["2", "3"]
	supported_entries = { "2" = "two", "3" = "three" }
           `,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"p1": 10,
						"p2": 0.75,
						"p3": "Elsa",
						"p4": true,
						"p5": "2",
						"p6": "3",
						"p7": "two",
						"p8": "three",
					},
					time.Unix(0, 0),
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin, err := buildPlugin(tt.config)
			require.NoError(t, err)
			err = plugin.Init()
			require.NoError(t, err)

			var acc testutil.Accumulator

			err = plugin.Start(&acc)
			require.NoError(t, err)

			for _, m := range tt.input {
				err = plugin.Add(m, &acc)
				require.NoError(t, err)
			}

			plugin.Stop()
			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics())
		})
	}
}

// Build a Starlark plugin from the provided configuration.
func buildPlugin(configContent string) (*Starlark, error) {
	c := config.NewConfig()
	err := c.LoadConfigData([]byte(configContent))
	if err != nil {
		return nil, err
	}
	if len(c.Processors) != 1 {
		return nil, errors.New("only one processor was expected")
	}
	plugin, ok := (c.Processors[0].Processor).(*Starlark)
	if !ok {
		return nil, errors.New("only a Starlark processor was expected")
	}
	plugin.Log = testutil.Logger{}
	return plugin, nil
}

func TestScript(t *testing.T) {
	var tests = []struct {
		name             string
		plugin           *Starlark
		input            []telegraf.Metric
		expected         []telegraf.Metric
		expectedErrorStr string
	}{
		{
			name:   "rename",
			plugin: newStarlarkFromScript("testdata/rename.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"lower": "0",
						"upper": "10",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"min": "0",
						"max": "10",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:   "drop fields by type",
			plugin: newStarlarkFromScript("testdata/drop_string_fields.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("device",
					map[string]string{},
					map[string]interface{}{
						"a": 42,
						"b": "42",
						"c": 42.0,
						"d": "42.0",
						"e": true,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("device",
					map[string]string{},
					map[string]interface{}{
						"a": 42,
						"c": 42.0,
						"e": true,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:   "drop fields with unexpected type",
			plugin: newStarlarkFromScript("testdata/drop_fields_with_unexpected_type.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("device",
					map[string]string{},
					map[string]interface{}{
						"a": 42,
						"b": "42",
						"c": 42.0,
						"d": "42.0",
						"e": true,
						"f": 23.0,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("device",
					map[string]string{},
					map[string]interface{}{
						"a": 42,
						"c": 42.0,
						"d": "42.0",
						"e": true,
						"f": 23.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:   "scale",
			plugin: newStarlarkFromScript("testdata/scale.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 10.0},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 100.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:   "ratio",
			plugin: newStarlarkFromScript("testdata/ratio.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("mem",
					map[string]string{},
					map[string]interface{}{
						"used":  2,
						"total": 10,
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("mem",
					map[string]string{},
					map[string]interface{}{
						"used":  2,
						"total": 10,
						"usage": 20.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:   "logging",
			plugin: newStarlarkFromScript("testdata/logging.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("log",
					map[string]string{},
					map[string]interface{}{
						"debug": "a debug message",
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("log",
					map[string]string{},
					map[string]interface{}{
						"debug": "a debug message",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:   "multiple_metrics",
			plugin: newStarlarkFromScript("testdata/multiple_metrics.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("mm",
					map[string]string{},
					map[string]interface{}{
						"value": "a",
					},
					time.Unix(0, 0),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("mm2",
					map[string]string{},
					map[string]interface{}{
						"value": "b",
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric("mm1",
					map[string]string{},
					map[string]interface{}{
						"value": "a",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:   "multiple_metrics_with_json",
			plugin: newStarlarkFromScript("testdata/multiple_metrics_with_json.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("json",
					map[string]string{},
					map[string]interface{}{
						"value": "[{\"label\": \"hello\"}, {\"label\": \"world\"}]",
					},
					time.Unix(1618488000, 999),
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("json",
					map[string]string{},
					map[string]interface{}{
						"value": "hello",
					},
					time.Unix(1618488000, 999),
				),
				testutil.MustMetric("json",
					map[string]string{},
					map[string]interface{}{
						"value": "world",
					},
					time.Unix(1618488000, 999),
				),
			},
		},
		{
			name:   "fail",
			plugin: newStarlarkFromScript("testdata/fail.star"),
			input: []telegraf.Metric{
				testutil.MustMetric("fail",
					map[string]string{},
					map[string]interface{}{
						"value": 1,
					},
					time.Unix(0, 0),
				),
			},
			expectedErrorStr: "fail: The field value should be greater than 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.Init()
			require.NoError(t, err)

			var acc testutil.Accumulator

			err = tt.plugin.Start(&acc)
			require.NoError(t, err)

			for _, m := range tt.input {
				err = tt.plugin.Add(m, &acc)
				if tt.expectedErrorStr != "" {
					require.EqualError(t, err, tt.expectedErrorStr)
				} else {
					require.NoError(t, err)
				}
			}

			tt.plugin.Stop()
			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics())
		})
	}
}

// Benchmarks modify the metric in place, so the scripts shouldn't modify the
// metric.
func Benchmark(b *testing.B) {
	var tests = []struct {
		name   string
		source string
		input  []telegraf.Metric
	}{
		{
			name: "passthrough",
			source: `
def apply(metric):
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "create new metric",
			source: `
def apply(metric):
	m = Metric('cpu')
	m.fields['time_guest'] = 2.0
	m.time = 0
	return m
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set name",
			source: `
def apply(metric):
	metric.name = "cpu"
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set tag",
			source: `
def apply(metric):
	metric.tags['host'] = 'example.org'
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"host": "example.org",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "tag in operator",
			source: `
def apply(metric):
	if 'c' in metric.tags:
		return metric
	return None
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tags",
			source: `
def apply(metric):
	for k in metric.tags:
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 42.0},
					time.Unix(0, 0),
				),
			},
		},
		{
			// This should be faster than calling items()
			name: "iterate tags and get values",
			source: `
def apply(metric):
	for k in metric.tags:
		v = metric.tags[k]
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate tag items",
			source: `
def apply(metric):
	for k, v in metric.tags.items():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					map[string]interface{}{"time_idle": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "set string field",
			source: `
def apply(metric):
	metric.fields['host'] = 'example.org'
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"host": "example.org",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate fields",
			source: `
def apply(metric):
	for k in metric.fields:
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":   42.0,
						"time_user":   42.0,
						"time_guest":  42.0,
						"time_system": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			// This should be faster than calling items()
			name: "iterate fields and get values",
			source: `
def apply(metric):
	for k in metric.fields:
		v = metric.fields[k]
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle":   42.0,
						"time_user":   42.0,
						"time_guest":  42.0,
						"time_system": 42.0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "iterate field items",
			source: `
def apply(metric):
	for k, v in metric.fields.items():
		pass
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{},
					map[string]interface{}{
						"a": "b",
						"c": "d",
						"e": "f",
						"g": "h",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "concatenate 2 tags",
			source: `
def apply(metric):
	metric.tags["result"] = '_'.join(metric.tags.values())
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"tag_1": "a",
						"tag_2": "b",
					},
					map[string]interface{}{"value": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "concatenate 4 tags",
			source: `
def apply(metric):
	metric.tags["result"] = '_'.join(metric.tags.values())
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"tag_1": "a",
						"tag_2": "b",
						"tag_3": "c",
						"tag_4": "d",
					},
					map[string]interface{}{"value": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "concatenate 8 tags",
			source: `
def apply(metric):
	metric.tags["result"] = '_'.join(metric.tags.values())
	return metric
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"tag_1": "a",
						"tag_2": "b",
						"tag_3": "c",
						"tag_4": "d",
						"tag_5": "e",
						"tag_6": "f",
						"tag_7": "g",
						"tag_8": "h",
					},
					map[string]interface{}{"value": 42},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "filter by field value",
			source: `
def apply(metric):
	match = metric.tags.get("bar") == "yeah" or metric.tags.get("tag_1") == "foo"
	match = match and metric.fields.get("value_1") > 5 and metric.fields.get("value_2") < 3.5
	if match:
		return metric
	return None
`,
			input: []telegraf.Metric{
				testutil.MustMetric("cpu",
					map[string]string{
						"tag_1": "foo",
					},
					map[string]interface{}{
						"value_1": 42,
						"value_2": 3.1415,
					},
					time.Unix(0, 0),
				),
			},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			plugin := newStarlarkFromSource(tt.source)

			err := plugin.Init()
			require.NoError(b, err)

			var acc testutil.NopAccumulator

			err = plugin.Start(&acc)
			require.NoError(b, err)

			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				for _, m := range tt.input {
					err = plugin.Add(m, &acc)
					require.NoError(b, err)
				}
			}

			plugin.Stop()
		})
	}
}

func TestAllScriptTestData(t *testing.T) {
	// can be run from multiple folders
	paths := []string{"testdata", "plugins/processors/starlark/testdata"}
	for _, testdataPath := range paths {
		err := filepath.Walk(testdataPath, func(path string, info os.FileInfo, _ error) error {
			if info == nil || info.IsDir() {
				return nil
			}
			fn := path
			t.Run(fn, func(t *testing.T) {
				b, err := os.ReadFile(fn)
				require.NoError(t, err)
				lines := strings.Split(string(b), "\n")
				inputMetrics := parseMetricsFrom(t, lines, "Example Input:")
				expectedErrorStr := parseErrorMessage(t, lines, "Example Output Error:")
				var outputMetrics []telegraf.Metric
				if expectedErrorStr == "" {
					outputMetrics = parseMetricsFrom(t, lines, "Example Output:")
				}
				plugin := newStarlarkFromScript(fn)
				require.NoError(t, plugin.Init())

				acc := &testutil.Accumulator{}

				err = plugin.Start(acc)
				require.NoError(t, err)

				for _, m := range inputMetrics {
					err = plugin.Add(m, acc)
					if expectedErrorStr != "" {
						require.EqualError(t, err, expectedErrorStr)
					} else {
						require.NoError(t, err)
					}
				}

				plugin.Stop()
				testutil.RequireMetricsEqual(t, outputMetrics, acc.GetTelegrafMetrics(), testutil.SortMetrics())
			})
			return nil
		})
		require.NoError(t, err)
	}
}

func TestTracking(t *testing.T) {
	var testCases = []struct {
		name       string
		source     string
		numMetrics int
	}{
		{
			name:       "return none",
			numMetrics: 0,
			source: `
def apply(metric):
	return None
`,
		},
		{
			name:       "return empty list of metrics",
			numMetrics: 0,
			source: `
def apply(metric):
	return []
`,
		},
		{
			name:       "return original metric",
			numMetrics: 1,
			source: `
def apply(metric):
	return metric
`,
		},
		{
			name:       "return original metric in a list",
			numMetrics: 1,
			source: `
def apply(metric):
	return [metric]
`,
		},
		{
			name:       "return new metric",
			numMetrics: 1,
			source: `
def apply(metric):
	newmetric = Metric("new_metric")
	newmetric.fields["value"] = 42
	return newmetric
`,
		},
		{
			name:       "return new metric in a list",
			numMetrics: 1,
			source: `
def apply(metric):
	newmetric = Metric("new_metric")
	newmetric.fields["value"] = 42
	return [newmetric]
`,
		},
		{
			name:       "return original and new metric in a list",
			numMetrics: 2,
			source: `
def apply(metric):
	newmetric = Metric("new_metric")
	newmetric.fields["value"] = 42
	return [metric, newmetric]
`,
		},
		{
			name:       "return original and deep-copy",
			numMetrics: 2,
			source: `
def apply(metric):
    return [metric, deepcopy(metric, track=True)]
`,
		},
		{
			name:       "deep-copy but do not return",
			numMetrics: 1,
			source: `
def apply(metric):
    x = deepcopy(metric)
    return [metric]
`,
		},
		{
			name:       "deep-copy but do not return original metric",
			numMetrics: 1,
			source: `
def apply(metric):
    x = deepcopy(metric, track=True)
    return [x]
`,
		},
		{
			name:       "issue #14484",
			numMetrics: 1,
			source: `
def apply(metric):
    metric.tags.pop("tag1")
    return [metric]
`,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Create a tracking metric and tap the delivery information
			var mu sync.Mutex
			delivered := make([]telegraf.DeliveryInfo, 0, 1)
			notify := func(di telegraf.DeliveryInfo) {
				mu.Lock()
				defer mu.Unlock()
				delivered = append(delivered, di)
			}

			// Configure the plugin
			plugin := newStarlarkFromSource(tt.source)
			require.NoError(t, plugin.Init())
			acc := &testutil.Accumulator{}
			require.NoError(t, plugin.Start(acc))

			// Process expected metrics and compare with resulting metrics
			input, _ := metric.WithTracking(testutil.TestMetric(1.23), notify)
			require.NoError(t, plugin.Add(input, acc))
			plugin.Stop()

			// Ensure we get back the correct number of metrics
			actual := acc.GetTelegrafMetrics()
			require.Lenf(t, actual, tt.numMetrics, "expected %d metrics but got %d", tt.numMetrics, len(actual))
			for _, m := range actual {
				m.Accept()
			}

			// Simulate output acknowledging delivery of metrics and check delivery
			require.Eventuallyf(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(delivered) == 1
			}, 1*time.Second, 100*time.Millisecond, "original metric not delivered")
		})
	}
}

func TestTrackingStateful(t *testing.T) {
	var testCases = []struct {
		name     string
		source   string
		results  int
		loops    int
		delivery int
	}{
		{
			name:     "delayed release",
			loops:    4,
			results:  3,
			delivery: 4,
			source: `
state = {"last": None}

def apply(metric):
  previous = state["last"]
  state["last"] = deepcopy(metric)
  return previous
`,
		},
		{
			name:     "delayed release with tracking",
			loops:    4,
			results:  3,
			delivery: 3,
			source: `
state = {"last": None}

def apply(metric):
  previous = state["last"]
  state["last"] = deepcopy(metric, track=True)
  return previous
`,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Create a tracking metric and tap the delivery information
			var mu sync.Mutex
			delivered := make([]telegraf.TrackingID, 0, tt.delivery)
			notify := func(di telegraf.DeliveryInfo) {
				mu.Lock()
				defer mu.Unlock()
				delivered = append(delivered, di.ID())
			}

			// Configure the plugin
			plugin := newStarlarkFromSource(tt.source)
			require.NoError(t, plugin.Init())
			acc := &testutil.Accumulator{}
			require.NoError(t, plugin.Start(acc))

			// Do the requested number of loops
			expected := make([]telegraf.TrackingID, 0, tt.loops)
			for i := 0; i < tt.loops; i++ {
				// Process expected metrics and compare with resulting metrics
				input, tid := metric.WithTracking(testutil.TestMetric(i), notify)
				expected = append(expected, tid)
				require.NoError(t, plugin.Add(input, acc))
			}
			plugin.Stop()
			expected = expected[:tt.delivery]

			// Simulate output acknowledging delivery of metrics and check delivery
			actual := acc.GetTelegrafMetrics()
			// Ensure we get back the correct number of metrics
			require.Lenf(t, actual, tt.results, "expected %d metrics but got %d", tt.results, len(actual))
			for _, m := range actual {
				m.Accept()
			}

			require.Eventuallyf(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(delivered) >= tt.delivery
			}, 1*time.Second, 100*time.Millisecond, "original metric(s) not delivered")

			mu.Lock()
			defer mu.Unlock()
			require.ElementsMatch(t, expected, delivered, "mismatch in delivered metrics")
		})
	}
}

func TestGlobalState(t *testing.T) {
	source := `
def apply(metric):
  count = state.get("count", 0)
  count += 1
  state["count"] = count

  metric.fields["count"] = count

  return metric
`
	// Define the metrics
	input := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1713188113, 10),
		),
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1713188113, 20),
		),
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1713188113, 30),
		)}
	expected := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42, "count": 1},
			time.Unix(1713188113, 10),
		),
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42, "count": 2},
			time.Unix(1713188113, 20),
		),
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42, "count": 3},
			time.Unix(1713188113, 30),
		),
	}

	// Configure the plugin
	plugin := &Starlark{
		Common: common.Common{
			StarlarkLoadFunc: testLoadFunc,
			Source:           source,
			Log:              testutil.Logger{},
		},
	}
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))

	// Do the processing
	for _, m := range input {
		require.NoError(t, plugin.Add(m, &acc))
	}
	plugin.Stop()

	// Check
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual)
}

func TestStatePersistence(t *testing.T) {
	source := `
def apply(metric):
  count = state.get("count", 0)
  count += 1
  state["count"] = count

  metric.fields["count"] = count
  metric.tags["instance"] = state.get("instance", "unknown")

  return metric
`
	// Define the metrics
	input := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1713188113, 10),
		),
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1713188113, 20),
		),
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1713188113, 30),
		)}
	expected := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{"instance": "myhost"},
			map[string]interface{}{"value": 42, "count": 1},
			time.Unix(1713188113, 10),
		),
		metric.New(
			"test",
			map[string]string{"instance": "myhost"},
			map[string]interface{}{"value": 42, "count": 2},
			time.Unix(1713188113, 20),
		),
		metric.New(
			"test",
			map[string]string{"instance": "myhost"},
			map[string]interface{}{"value": 42, "count": 3},
			time.Unix(1713188113, 30),
		),
	}

	// Configure the plugin
	plugin := &Starlark{
		Common: common.Common{
			StarlarkLoadFunc: testLoadFunc,
			Source:           source,
			Log:              testutil.Logger{},
		},
	}
	require.NoError(t, plugin.Init())

	// Setup the "persisted" state
	var pi telegraf.StatefulPlugin = plugin
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(map[string]interface{}{"instance": "myhost"}))
	require.NoError(t, pi.SetState(buf.Bytes()))

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))

	// Do the processing
	for _, m := range input {
		require.NoError(t, plugin.Add(m, &acc))
	}
	plugin.Stop()

	// Check
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual)

	// Check getting the persisted state
	expectedState := map[string]interface{}{"instance": "myhost", "count": int64(3)}

	var actualState map[string]interface{}
	stateData, ok := pi.GetState().([]byte)
	require.True(t, ok, "state is not a bytes array")
	require.NoError(t, gob.NewDecoder(bytes.NewBuffer(stateData)).Decode(&actualState))
	require.EqualValues(t, expectedState, actualState, "mismatch in state")
}

func TestUsePredefinedStateName(t *testing.T) {
	source := `
def apply(metric):
  return metric
`
	// Configure the plugin
	plugin := &Starlark{
		Common: common.Common{
			StarlarkLoadFunc: testLoadFunc,
			Source:           source,
			Constants:        map[string]interface{}{"state": "invalid"},
			Log:              testutil.Logger{},
		},
	}
	require.ErrorContains(t, plugin.Init(), "'state' constant uses reserved name")
}

// parses metric lines out of line protocol following a header, with a trailing blank line
func parseMetricsFrom(t *testing.T, lines []string, header string) (metrics []telegraf.Metric) {
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())

	require.NotEmpty(t, lines, "Expected some lines to parse from .star file, found none")
	startIdx := -1
	endIdx := len(lines)
	for i := range lines {
		if strings.TrimLeft(lines[i], "# ") == header {
			startIdx = i + 1
			break
		}
	}
	require.NotEqualf(t, -1, startIdx, "Header %q must exist in file", header)
	for i := startIdx; i < len(lines); i++ {
		line := strings.TrimLeft(lines[i], "# ")
		if line == "" || line == "'''" {
			endIdx = i
			break
		}
	}
	for i := startIdx; i < endIdx; i++ {
		m, err := parser.ParseLine(strings.TrimLeft(lines[i], "# "))
		require.NoErrorf(t, err, "Expected to be able to parse %q metric, but found error", header)
		metrics = append(metrics, m)
	}
	return metrics
}

// parses error message out of line protocol following a header
func parseErrorMessage(t *testing.T, lines []string, header string) string {
	require.NotEmpty(t, lines, "Expected some lines to parse from .star file, found none")
	startIdx := -1
	for i := range lines {
		if strings.TrimLeft(lines[i], "# ") == header {
			startIdx = i + 1
			break
		}
	}
	if startIdx == -1 {
		return ""
	}
	require.Lessf(t, startIdx, len(lines), "Expected to find the error message after %q, but found none", header)
	return strings.TrimLeft(lines[startIdx], "# ")
}

func testLoadFunc(module string, logger telegraf.Logger) (starlark.StringDict, error) {
	result, err := common.LoadFunc(module, logger)
	if err != nil {
		return nil, err
	}

	if module == "time.star" {
		customModule := result["time"].(*starlarkstruct.Module)
		customModule.Members["now"] = starlark.NewBuiltin("now", testNow)
		result["time"] = customModule
	}

	return result, nil
}

func testNow(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	return starlarktime.Time(time.Date(2021, 4, 15, 12, 0, 0, 999, time.UTC)), nil
}

func newStarlarkFromSource(source string) *Starlark {
	return &Starlark{
		Common: common.Common{
			StarlarkLoadFunc: testLoadFunc,
			Log:              testutil.Logger{},
			Source:           source,
		},
	}
}

func newStarlarkFromScript(script string) *Starlark {
	return &Starlark{
		Common: common.Common{
			StarlarkLoadFunc: testLoadFunc,
			Log:              testutil.Logger{},
			Script:           script,
		},
	}
}

func newStarlarkNoScript() *Starlark {
	return &Starlark{
		Common: common.Common{
			StarlarkLoadFunc: testLoadFunc,
			Log:              testutil.Logger{},
		},
	}
}
