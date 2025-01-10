package agent

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/testutil"
)

func TestAddFields(t *testing.T) {
	metrics := make(chan telegraf.Metric, 10)
	defer close(metrics)
	a := NewAccumulator(&TestMetricMaker{}, metrics)

	tags := map[string]string{"foo": "bar"}
	fields := map[string]interface{}{
		"usage": float64(99),
	}
	now := time.Now()
	a.AddCounter("acctest", fields, tags, now)

	testm := <-metrics

	require.Equal(t, "acctest", testm.Name())
	actual, ok := testm.GetField("usage")

	require.True(t, ok)
	require.InDelta(t, float64(99), actual, testutil.DefaultDelta)

	actual, ok = testm.GetTag("foo")
	require.True(t, ok)
	require.Equal(t, "bar", actual)

	tm := testm.Time()
	// okay if monotonic clock differs
	require.True(t, now.Equal(tm))

	tp := testm.Type()
	require.Equal(t, telegraf.Counter, tp)
}

func TestAccAddError(t *testing.T) {
	errBuf := bytes.NewBuffer(nil)
	logger.RedirectLogging(errBuf)
	defer logger.RedirectLogging(os.Stderr)

	metrics := make(chan telegraf.Metric, 10)
	defer close(metrics)
	a := NewAccumulator(&TestMetricMaker{}, metrics)

	a.AddError(errors.New("foo"))
	a.AddError(errors.New("bar"))
	a.AddError(errors.New("baz"))

	errs := bytes.Split(errBuf.Bytes(), []byte{'\n'})
	require.Len(t, errs, 4) // 4 because of trailing newline
	require.Contains(t, string(errs[0]), "TestPlugin")
	require.Contains(t, string(errs[0]), "foo")
	require.Contains(t, string(errs[1]), "TestPlugin")
	require.Contains(t, string(errs[1]), "bar")
	require.Contains(t, string(errs[2]), "TestPlugin")
	require.Contains(t, string(errs[2]), "baz")
}

func TestSetPrecision(t *testing.T) {
	tests := []struct {
		name      string
		unset     bool
		precision time.Duration
		timestamp time.Time
		expected  time.Time
	}{
		{
			name:      "default precision is nanosecond",
			unset:     true,
			timestamp: time.Date(2006, time.February, 10, 12, 0, 0, 82912748, time.UTC),
			expected:  time.Date(2006, time.February, 10, 12, 0, 0, 82912748, time.UTC),
		},
		{
			name:      "second interval",
			precision: time.Second,
			timestamp: time.Date(2006, time.February, 10, 12, 0, 0, 82912748, time.UTC),
			expected:  time.Date(2006, time.February, 10, 12, 0, 0, 0, time.UTC),
		},
		{
			name:      "microsecond interval",
			precision: time.Microsecond,
			timestamp: time.Date(2006, time.February, 10, 12, 0, 0, 82912748, time.UTC),
			expected:  time.Date(2006, time.February, 10, 12, 0, 0, 82913000, time.UTC),
		},
		{
			name:      "2 second precision",
			precision: 2 * time.Second,
			timestamp: time.Date(2006, time.February, 10, 12, 0, 2, 4, time.UTC),
			expected:  time.Date(2006, time.February, 10, 12, 0, 2, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := make(chan telegraf.Metric, 10)

			a := NewAccumulator(&TestMetricMaker{}, metrics)
			if !tt.unset {
				a.SetPrecision(tt.precision)
			}

			a.AddFields("acctest",
				map[string]interface{}{"value": float64(101)},
				map[string]string{},
				tt.timestamp,
			)

			testm := <-metrics
			require.Equal(t, tt.expected, testm.Time())

			close(metrics)
		})
	}
}

func TestAddTrackingMetricGroupEmpty(t *testing.T) {
	ch := make(chan telegraf.Metric, 10)
	metrics := make([]telegraf.Metric, 0)
	acc := NewAccumulator(&TestMetricMaker{}, ch).WithTracking(1)

	id := acc.AddTrackingMetricGroup(metrics)

	select {
	case tracking := <-acc.Delivered():
		require.Equal(t, tracking.ID(), id)
	default:
		t.Fatal("empty group should be delivered immediately")
	}
}

type TestMetricMaker struct {
}

func (tm *TestMetricMaker) Name() string {
	return "TestPlugin"
}

func (tm *TestMetricMaker) LogName() string {
	return tm.Name()
}

func (tm *TestMetricMaker) MakeMetric(metric telegraf.Metric) telegraf.Metric {
	return metric
}

func (tm *TestMetricMaker) Log() telegraf.Logger {
	return logger.New("TestPlugin", "test", "")
}
