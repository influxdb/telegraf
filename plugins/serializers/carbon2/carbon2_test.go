package carbon2

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/stretchr/testify/assert"
)

func MustMetric(v telegraf.Metric, err error) telegraf.Metric {
	if err != nil {
		panic(err)
	}
	return v
}

func TestSerializeMetricFloat(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu": "cpu0",
	}
	fields := map[string]interface{}{
		"usage_idle": float64(91.5),
	}
	m, err := metric.New("cpu", tags, fields, now)
	assert.NoError(t, err)

	s, _ := NewSerializer()
	var buf []byte
	buf, err = s.Serialize(m)
	assert.NoError(t, err)
	expS := []byte(fmt.Sprintf(`metric=cpu field=usage_idle cpu=cpu0  91.5 %d`, now.Unix()) + "\n")
	assert.Equal(t, string(expS), string(buf))
}

func TestSerializeMetricInt(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu": "cpu0",
	}
	fields := map[string]interface{}{
		"usage_idle": int64(90),
	}
	m, err := metric.New("cpu", tags, fields, now)
	assert.NoError(t, err)

	s, _ := NewSerializer()
	var buf []byte
	buf, err = s.Serialize(m)
	assert.NoError(t, err)

	expS := []byte(fmt.Sprintf(`metric=cpu field=usage_idle cpu=cpu0  90 %d`, now.Unix()) + "\n")
	assert.Equal(t, string(expS), string(buf))
}

func TestSerializeMetricString(t *testing.T) {
	now := time.Now()
	tags := map[string]string{
		"cpu": "cpu0",
	}
	fields := map[string]interface{}{
		"usage_idle": "foobar",
	}
	m, err := metric.New("cpu", tags, fields, now)
	assert.NoError(t, err)

	s, _ := NewSerializer()
	var buf []byte
	buf, err = s.Serialize(m)
	assert.NoError(t, err)

	expS := []byte("")
	assert.Equal(t, string(expS), string(buf))
}

func TestSerializeBatch(t *testing.T) {
	m := MustMetric(
		metric.New(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"value": 42,
			},
			time.Unix(0, 0),
		),
	)

	metrics := []telegraf.Metric{m, m}
	s, _ := NewSerializer()
	buf, err := s.SerializeBatch(metrics)
	require.NoError(t, err)
	expS := []byte(`metric=cpu field=value  42 0
metric=cpu field=value  42 0
`)
	assert.Equal(t, string(expS), string(buf))
}
