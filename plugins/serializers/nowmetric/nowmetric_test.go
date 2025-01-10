package nowmetric

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/serializers"
)

func TestSerializeMetricFloat(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu": "cpu0",
	}
	fields := map[string]interface{}{
		"usage_idle": float64(91.5),
	}
	m := metric.New("cpu", tags, fields, now)

	s := &Serializer{}
	var buf []byte
	buf, err := s.Serialize(m)
	require.NoError(t, err)
	expS := []byte(
		fmt.Sprintf(
			`[{"metric_type":"usage_idle","resource":"","node":"","value":91.5,"timestamp":%d,"ci2metric_id":null,"source":"Telegraf"}]`,
			now.UnixNano()/int64(time.Millisecond),
		),
	)
	require.Equal(t, string(expS), string(buf))
}

func TestSerialize_TimestampUnits(t *testing.T) {
	tests := []struct {
		name           string
		timestampUnits time.Duration
		expected       string
	}{
		{
			name:           "1ms",
			timestampUnits: 1 * time.Millisecond,
			expected:       `[{"metric_type":"value","resource":"","node":"","value":42,"timestamp":1525478795123,"ci2metric_id":null,"source":"Telegraf"}]`,
		},
		{
			name:           "10ms",
			timestampUnits: 10 * time.Millisecond,
			expected:       `[{"metric_type":"value","resource":"","node":"","value":42,"timestamp":1525478795123,"ci2metric_id":null,"source":"Telegraf"}]`,
		},
		{
			name:           "15ms is reduced to 10ms",
			timestampUnits: 15 * time.Millisecond,
			expected:       `[{"metric_type":"value","resource":"","node":"","value":42,"timestamp":1525478795123,"ci2metric_id":null,"source":"Telegraf"}]`,
		},
		{
			name:           "65ms is reduced to 10ms",
			timestampUnits: 65 * time.Millisecond,
			expected:       `[{"metric_type":"value","resource":"","node":"","value":42,"timestamp":1525478795123,"ci2metric_id":null,"source":"Telegraf"}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := metric.New(
				"cpu",
				map[string]string{},
				map[string]interface{}{
					"value": 42.0,
				},
				time.Unix(1525478795, 123456789),
			)
			s := &Serializer{}
			actual, err := s.Serialize(m)
			require.NoError(t, err)
			require.Equal(t, tt.expected, string(actual))
		})
	}
}

func TestSerializeMetricInt(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu": "cpu0",
	}
	fields := map[string]interface{}{
		"usage_idle": int64(90),
	}
	m := metric.New("cpu", tags, fields, now)

	s := &Serializer{}
	var buf []byte
	buf, err := s.Serialize(m)
	require.NoError(t, err)

	expS := []byte(
		fmt.Sprintf(
			`[{"metric_type":"usage_idle","resource":"","node":"","value":90,"timestamp":%d,"ci2metric_id":null,"source":"Telegraf"}]`,
			now.UnixNano()/int64(time.Millisecond),
		),
	)
	require.Equal(t, string(expS), string(buf))
}

func TestSerializeMetricString(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu": "cpu0",
	}
	fields := map[string]interface{}{
		"usage_idle": "foobar",
	}
	m := metric.New("cpu", tags, fields, now)

	s := &Serializer{}
	var buf []byte
	buf, err := s.Serialize(m)
	require.NoError(t, err)

	require.Equal(t, "null", string(buf))
}

func TestSerializeMultiFields(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu": "cpu0",
	}
	fields := map[string]interface{}{
		"usage_idle":  int64(90),
		"usage_total": 8559615,
	}
	m := metric.New("cpu", tags, fields, now)

	// Sort for predictable field order
	sort.Slice(m.FieldList(), func(i, j int) bool {
		return m.FieldList()[i].Key < m.FieldList()[j].Key
	})

	s := &Serializer{}
	var buf []byte
	buf, err := s.Serialize(m)
	require.NoError(t, err)

	expS := []byte(
		fmt.Sprintf(
			`[{"metric_type":"usage_idle","resource":"","node":"","value":90,"timestamp":%d,"ci2metric_id":null,"source":"Telegraf"},`+
				`{"metric_type":"usage_total","resource":"","node":"","value":8559615,"timestamp":%d,"ci2metric_id":null,"source":"Telegraf"}]`,
			now.UnixNano()/int64(time.Millisecond),
			now.UnixNano()/int64(time.Millisecond),
		),
	)
	require.Equal(t, string(expS), string(buf))
}

func TestSerializeMetricWithEscapes(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu tag": "cpu0",
	}
	fields := map[string]interface{}{
		"U,age=Idle": int64(90),
	}
	m := metric.New("My CPU", tags, fields, now)

	s := &Serializer{}
	buf, err := s.Serialize(m)
	require.NoError(t, err)

	expS := []byte(
		fmt.Sprintf(
			`[{"metric_type":"U,age=Idle","resource":"","node":"","value":90,"timestamp":%d,"ci2metric_id":null,"source":"Telegraf"}]`,
			now.UnixNano()/int64(time.Millisecond),
		),
	)
	require.Equal(t, string(expS), string(buf))
}

func TestSerializeBatch(t *testing.T) {
	m := metric.New(
		"cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	metrics := []telegraf.Metric{m, m}
	s := &Serializer{}
	buf, err := s.SerializeBatch(metrics)
	require.NoError(t, err)
	require.JSONEq(
		t,
		`[{"metric_type":"value","resource":"","node":"","value":42,"timestamp":0,"ci2metric_id":null,"source":"Telegraf"},`+
			`{"metric_type":"value","resource":"","node":"","value":42,"timestamp":0,"ci2metric_id":null,"source":"Telegraf"}]`,
		string(buf),
	)
}

func TestSerializeJSONv2Format(t *testing.T) {
	m := metric.New(
		"cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	s := &Serializer{Format: "jsonv2"}
	buf, err := s.Serialize(m)
	require.NoError(t, err)
	require.JSONEq(
		t,
		`{"records":[{"metric_type":"value","resource":"","node":"","value":42,"timestamp":0,"ci2metric_id":null,"source":"Telegraf"}]}`,
		string(buf),
	)
}

func TestSerializeJSONv2FormatBatch(t *testing.T) {
	m := metric.New(
		"cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	s := &Serializer{Format: "jsonv2"}
	metrics := []telegraf.Metric{m, m}
	buf, err := s.SerializeBatch(metrics)
	require.NoError(t, err)
	require.JSONEq(
		t,
		`{"records":[`+
			`{"metric_type":"value","resource":"","node":"","value":42,"timestamp":0,"ci2metric_id":null,"source":"Telegraf"},`+
			`{"metric_type":"value","resource":"","node":"","value":42,"timestamp":0,"ci2metric_id":null,"source":"Telegraf"}`+
			`]}`,
		string(buf),
	)
}

func TestSerializeInvalidFormat(t *testing.T) {
	s := &Serializer{Format: "foo"}
	require.Error(t, s.Init())
}

func BenchmarkSerialize(b *testing.B) {
	s := &Serializer{}
	require.NoError(b, s.Init())
	metrics := serializers.BenchmarkMetrics(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Serialize(metrics[i%len(metrics)])
		require.NoError(b, err)
	}
}

func BenchmarkSerializeBatch(b *testing.B) {
	s := &Serializer{}
	require.NoError(b, s.Init())
	m := serializers.BenchmarkMetrics(b)
	metrics := m[:]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.SerializeBatch(metrics)
		require.NoError(b, err)
	}
}
