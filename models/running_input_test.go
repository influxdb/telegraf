package models

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/selfstat"
	"github.com/influxdata/telegraf/testutil"
)

func TestRunningInputMakeMetricFilterAfterApplyingGlobalTags(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Filter: Filter{
			TagInclude: []string{"b"},
		},
	})
	require.NoError(t, ri.Config.Filter.Compile())
	ri.SetDefaultTags(map[string]string{"a": "x", "b": "y"})

	m := metric.New("cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42,
		},
		now)

	actual := ri.MakeMetric(m)

	expected := metric.New("cpu",
		map[string]string{
			"b": "y",
		},
		map[string]interface{}{
			"value": 42,
		},
		now)

	testutil.RequireMetricEqual(t, expected, actual)
}

func TestRunningInputMakeMetricNoFields(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
	})

	m := metric.New("RITest",
		map[string]string{},
		map[string]interface{}{},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)
	require.Nil(t, actual)
}

// nil fields should get dropped
func TestRunningInputMakeMetricNilFields(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
	})

	m := metric.New("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int64(101),
			"nil":   nil,
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)

	expected := metric.New("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int(101),
		},
		now,
	)

	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricWithPluginTags(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
		Tags: map[string]string{
			"foo": "bar",
		},
	})

	m := testutil.MustMetric("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)

	expected := metric.New("RITest",
		map[string]string{
			"foo": "bar",
		},
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricFilteredOut(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
		Tags: map[string]string{
			"foo": "bar",
		},
		Filter: Filter{NamePass: []string{"foobar"}},
	})

	require.NoError(t, ri.Config.Filter.Compile())

	m := metric.New("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)
	require.Nil(t, actual)
}

func TestRunningInputMakeMetricWithDaemonTags(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
	})
	ri.SetDefaultTags(map[string]string{
		"foo": "bar",
	})

	m := testutil.MustMetric("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)
	expected := metric.New("RITest",
		map[string]string{
			"foo": "bar",
		},
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricNameOverride(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name:         "TestRunningInput",
		NameOverride: "foobar",
	})

	m := metric.New("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)
	expected := metric.New("foobar",
		nil,
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricNamePrefix(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name:              "TestRunningInput",
		MeasurementPrefix: "foobar_",
	})

	m := metric.New("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)
	expected := metric.New("foobar_RITest",
		nil,
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricNameSuffix(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name:              "TestRunningInput",
		MeasurementSuffix: "_foobar",
	})

	m := metric.New("RITest",
		map[string]string{},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)
	expected := metric.New("RITest_foobar",
		nil,
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMetricErrorCounters(t *testing.T) {
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestMetricErrorCounters",
	})

	getGatherErrors := func() int64 {
		for _, r := range selfstat.Metrics() {
			tag, hasTag := r.GetTag("input")
			if r.Name() == "internal_gather" && hasTag && tag == "TestMetricErrorCounters" {
				errCount, ok := r.GetField("errors")
				if !ok {
					t.Fatal("Expected error field")
				}
				return errCount.(int64)
			}
		}
		return 0
	}

	before := getGatherErrors()

	ri.Log().Error("Oh no")

	after := getGatherErrors()

	require.Greater(t, after, before)
	require.GreaterOrEqual(t, int64(1), GlobalGatherErrors.Get())
}

func TestRunningInputMakeMetricWithAlwaysKeepingPluginTagsDisabled(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
		Tags: map[string]string{
			"foo": "bar",
		},
		Filter: Filter{
			TagInclude: []string{"b"},
		},
	})
	ri.SetDefaultTags(map[string]string{"logic": "rulez"})
	require.NoError(t, ri.Config.Filter.Compile())

	m := testutil.MustMetric("RITest",
		map[string]string{
			"b": "test",
		},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)

	expected := metric.New("RITest",
		map[string]string{
			"b": "test",
		},
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricWithAlwaysKeepingLocalPluginTagsEnabled(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
		Tags: map[string]string{
			"foo": "bar",
		},
		Filter: Filter{
			TagInclude: []string{"b"},
		},
		AlwaysIncludeLocalTags: true,
	})
	ri.SetDefaultTags(map[string]string{"logic": "rulez"})
	require.NoError(t, ri.Config.Filter.Compile())

	m := testutil.MustMetric("RITest",
		map[string]string{
			"b": "test",
		},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)

	expected := metric.New("RITest",
		map[string]string{
			"b":   "test",
			"foo": "bar",
		},
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricWithAlwaysKeepingGlobalPluginTagsEnabled(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
		Tags: map[string]string{
			"foo": "bar",
		},
		Filter: Filter{
			TagInclude: []string{"b"},
		},
		AlwaysIncludeGlobalTags: true,
	})
	ri.SetDefaultTags(map[string]string{"logic": "rulez"})
	require.NoError(t, ri.Config.Filter.Compile())

	m := testutil.MustMetric("RITest",
		map[string]string{
			"b": "test",
		},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)

	expected := metric.New("RITest",
		map[string]string{
			"b":     "test",
			"logic": "rulez",
		},
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricWithAlwaysKeepingPluginTagsEnabled(t *testing.T) {
	now := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name: "TestRunningInput",
		Tags: map[string]string{
			"foo": "bar",
		},
		Filter: Filter{
			TagInclude: []string{"b"},
		},
		AlwaysIncludeLocalTags:  true,
		AlwaysIncludeGlobalTags: true,
	})
	ri.SetDefaultTags(map[string]string{"logic": "rulez"})
	require.NoError(t, ri.Config.Filter.Compile())

	m := testutil.MustMetric("RITest",
		map[string]string{
			"b": "test",
		},
		map[string]interface{}{
			"value": int64(101),
		},
		now,
		telegraf.Untyped)
	actual := ri.MakeMetric(m)

	expected := metric.New("RITest",
		map[string]string{
			"b":     "test",
			"foo":   "bar",
			"logic": "rulez",
		},
		map[string]interface{}{
			"value": 101,
		},
		now,
	)
	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricWithGatherMetricTimeSource(t *testing.T) {
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name:                    "TestRunningInput",
		Tags:                    make(map[string]string),
		Filter:                  Filter{},
		AlwaysIncludeLocalTags:  false,
		AlwaysIncludeGlobalTags: false,
		TimeSource:              "metric",
	})
	start := time.Now()
	ri.gatherStart = start
	ri.gatherEnd = start.Add(time.Second)

	expected := testutil.MockMetrics()[0]

	m := testutil.MockMetrics()[0]
	actual := ri.MakeMetric(m)

	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricWithGatherStartTimeSource(t *testing.T) {
	start := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name:                    "TestRunningInput",
		Tags:                    make(map[string]string),
		Filter:                  Filter{},
		AlwaysIncludeLocalTags:  false,
		AlwaysIncludeGlobalTags: false,
		TimeSource:              "collection_start",
	})
	ri.gatherStart = start

	expected := testutil.MockMetrics()[0]
	expected.SetTime(start)

	m := testutil.MockMetrics()[0]
	actual := ri.MakeMetric(m)

	require.Equal(t, expected, actual)
}

func TestRunningInputMakeMetricWithGatherEndTimeSource(t *testing.T) {
	end := time.Now()
	ri := NewRunningInput(&mockInput{}, &InputConfig{
		Name:       "TestRunningInput",
		TimeSource: "collection_end",
	})
	ri.gatherEnd = end

	expected := testutil.MockMetrics()[0]
	expected.SetTime(end)

	m := testutil.MockMetrics()[0]
	actual := ri.MakeMetric(m)

	require.Equal(t, expected, actual)
}

type mockInput struct{}

func (*mockInput) SampleConfig() string {
	return ""
}

func (*mockInput) Gather(telegraf.Accumulator) error {
	return nil
}

func TestRunningInputProbingFailure(t *testing.T) {
	ri := NewRunningInput(&mockProbingInput{
		probeReturn: errors.New("probing error"),
	}, &InputConfig{
		Name:                 "TestRunningInput",
		StartupErrorBehavior: "probe",
	})
	ri.log = testutil.Logger{}
	require.Error(t, ri.Probe())
}

func TestRunningInputProbingSuccess(t *testing.T) {
	probeErr := errors.New("probing error")
	for _, tt := range []struct {
		name                 string
		input                telegraf.Input
		startupErrorBehavior string
	}{
		{
			name:                 "non-probing plugin with probe value set",
			input:                &mockInput{},
			startupErrorBehavior: "probe",
		},
		{
			name:                 "non-probing plugin with probe value not set",
			input:                &mockInput{},
			startupErrorBehavior: "ignore",
		},
		{
			name:                 "probing plugin with probe value not set",
			input:                &mockProbingInput{probeErr},
			startupErrorBehavior: "ignore",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ri := NewRunningInput(tt.input, &InputConfig{
				Name:                 "TestRunningInput",
				StartupErrorBehavior: tt.startupErrorBehavior,
			})
			ri.log = testutil.Logger{}
			require.NoError(t, ri.Probe())
		})
	}
}

type mockProbingInput struct {
	probeReturn error
}

func (m *mockProbingInput) SampleConfig() string {
	return ""
}

func (m *mockProbingInput) Gather(_ telegraf.Accumulator) error {
	return nil
}

func (m *mockProbingInput) Probe() error {
	return m.probeReturn
}
