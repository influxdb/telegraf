package ratelimiter

import (
	"math"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/serializers/influx"
	"github.com/stretchr/testify/require"
)

func TestIndividualSerializer(t *testing.T) {
	input := []telegraf.Metric{
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "A",
				"status":   "ok",
			},
			map[string]interface{}{
				"operating_hours": 123,
				"temperature":     25.0,
				"pressure":        1023.4,
			},
			time.Unix(1722443551, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "B",
				"status":   "failed",
			},
			map[string]interface{}{
				"operating_hours": 8430,
				"temperature":     65.2,
				"pressure":        985.9,
			},
			time.Unix(1722443554, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "C",
				"status":   "warning",
			},
			map[string]interface{}{
				"operating_hours": 6765,
				"temperature":     42.5,
				"pressure":        986.1,
			},
			time.Unix(1722443555, 0),
		),
		metric.New(
			"device",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
			},
			map[string]interface{}{
				"status": "ok",
			},
			time.Unix(1722443556, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
				"machine":  "A",
				"status":   "ok",
			},
			map[string]interface{}{
				"operating_hours": 5544,
				"temperature":     18.6,
				"pressure":        1069.4,
			},
			time.Unix(1722443552, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
				"machine":  "B",
				"status":   "ok",
			},
			map[string]interface{}{
				"operating_hours": 65,
				"temperature":     29.7,
				"pressure":        1101.2,
			},
			time.Unix(1722443553, 0),
		),
		metric.New(
			"device",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
			},
			map[string]interface{}{
				"status": "ok",
			},
			time.Unix(1722443559, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
				"machine":  "C",
				"status":   "off",
			},
			map[string]interface{}{
				"operating_hours": 0,
				"temperature":     0.0,
				"pressure":        0.0,
			},
			time.Unix(1722443562, 0),
		),
	}
	//nolint:lll // Resulting metrics should not be wrapped for readability
	expected := []string{
		"serializer_test,location=factory_north,machine=A,source=localhost,status=ok operating_hours=123i,pressure=1023.4,temperature=25 1722443551000000000\n" +
			"serializer_test,location=factory_north,machine=B,source=localhost,status=failed operating_hours=8430i,pressure=985.9,temperature=65.2 1722443554000000000\n",
		"serializer_test,location=factory_north,machine=C,source=localhost,status=warning operating_hours=6765i,pressure=986.1,temperature=42.5 1722443555000000000\n" +
			"device,location=factory_north,source=localhost status=\"ok\" 1722443556000000000\n" +
			"serializer_test,location=factory_south,machine=A,source=gateway_af43e,status=ok operating_hours=5544i,pressure=1069.4,temperature=18.6 1722443552000000000\n",
		"serializer_test,location=factory_south,machine=B,source=gateway_af43e,status=ok operating_hours=65i,pressure=1101.2,temperature=29.7 1722443553000000000\n" +
			"device,location=factory_south,source=gateway_af43e status=\"ok\" 1722443559000000000\n" +
			"serializer_test,location=factory_south,machine=C,source=gateway_af43e,status=off operating_hours=0i,pressure=0,temperature=0 1722443562000000000\n",
	}

	// Setup the limited serializer
	s := &influx.Serializer{SortFields: true}
	require.NoError(t, s.Init())
	serializer := NewIndividualSerializer(s)

	var werr *internal.PartialWriteError

	// Do the first serialization runs with all metrics
	buf, err := serializer.SerializeBatch(input, 400)
	require.ErrorAs(t, err, &werr)
	require.ErrorIs(t, werr.Err, internal.ErrSizeLimitReached)
	require.EqualValues(t, []int{0, 1}, werr.MetricsAccept)
	require.Empty(t, werr.MetricsReject)
	require.Equal(t, expected[0], string(buf))

	// Run again with the successful metrics removed
	buf, err = serializer.SerializeBatch(input[2:], 400)
	require.ErrorAs(t, err, &werr)
	require.ErrorIs(t, werr.Err, internal.ErrSizeLimitReached)
	require.EqualValues(t, []int{0, 1, 2}, werr.MetricsAccept)
	require.Empty(t, werr.MetricsReject)
	require.Equal(t, expected[1], string(buf))

	// Final run with the successful metrics removed
	buf, err = serializer.SerializeBatch(input[5:], 400)
	require.NoError(t, err)
	require.Equal(t, expected[2], string(buf))
}

func TestIndividualSerializerFirstTooBig(t *testing.T) {
	input := []telegraf.Metric{
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "A",
				"status":   "ok",
			},
			map[string]interface{}{
				"operating_hours": 123,
				"temperature":     25.0,
				"pressure":        1023.4,
			},
			time.Unix(1722443551, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "B",
				"status":   "failed",
			},
			map[string]interface{}{
				"operating_hours": 8430,
				"temperature":     65.2,
				"pressure":        985.9,
			},
			time.Unix(1722443554, 0),
		),
	}

	// Setup the limited serializer
	s := &influx.Serializer{SortFields: true}
	require.NoError(t, s.Init())
	serializer := NewIndividualSerializer(s)

	// The first metric will already exceed the size so all metrics fail and
	// we expect a shortcut error.
	buf, err := serializer.SerializeBatch(input, 100)
	require.ErrorIs(t, err, internal.ErrSizeLimitReached)
	require.Empty(t, buf)
}

func TestIndividualSerializerUnlimited(t *testing.T) {
	input := []telegraf.Metric{
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "A",
				"status":   "ok",
			},
			map[string]interface{}{
				"operating_hours": 123,
				"temperature":     25.0,
				"pressure":        1023.4,
			},
			time.Unix(1722443551, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "B",
				"status":   "failed",
			},
			map[string]interface{}{
				"operating_hours": 8430,
				"temperature":     65.2,
				"pressure":        985.9,
			},
			time.Unix(1722443554, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
				"machine":  "C",
				"status":   "warning",
			},
			map[string]interface{}{
				"operating_hours": 6765,
				"temperature":     42.5,
				"pressure":        986.1,
			},
			time.Unix(1722443555, 0),
		),
		metric.New(
			"device",
			map[string]string{
				"source":   "localhost",
				"location": "factory_north",
			},
			map[string]interface{}{
				"status": "ok",
			},
			time.Unix(1722443556, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
				"machine":  "A",
				"status":   "ok",
			},
			map[string]interface{}{
				"operating_hours": 5544,
				"temperature":     18.6,
				"pressure":        1069.4,
			},
			time.Unix(1722443552, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
				"machine":  "B",
				"status":   "ok",
			},
			map[string]interface{}{
				"operating_hours": 65,
				"temperature":     29.7,
				"pressure":        1101.2,
			},
			time.Unix(1722443553, 0),
		),
		metric.New(
			"device",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
			},
			map[string]interface{}{
				"status": "ok",
			},
			time.Unix(1722443559, 0),
		),
		metric.New(
			"serializer_test",
			map[string]string{
				"source":   "gateway_af43e",
				"location": "factory_south",
				"machine":  "C",
				"status":   "off",
			},
			map[string]interface{}{
				"operating_hours": 0,
				"temperature":     0.0,
				"pressure":        0.0,
			},
			time.Unix(1722443562, 0),
		),
	}
	//nolint:lll // Resulting metrics should not be wrapped for readability
	expected := "serializer_test,location=factory_north,machine=A,source=localhost,status=ok operating_hours=123i,pressure=1023.4,temperature=25 1722443551000000000\n" +
		"serializer_test,location=factory_north,machine=B,source=localhost,status=failed operating_hours=8430i,pressure=985.9,temperature=65.2 1722443554000000000\n" +
		"serializer_test,location=factory_north,machine=C,source=localhost,status=warning operating_hours=6765i,pressure=986.1,temperature=42.5 1722443555000000000\n" +
		"device,location=factory_north,source=localhost status=\"ok\" 1722443556000000000\n" +
		"serializer_test,location=factory_south,machine=A,source=gateway_af43e,status=ok operating_hours=5544i,pressure=1069.4,temperature=18.6 1722443552000000000\n" +
		"serializer_test,location=factory_south,machine=B,source=gateway_af43e,status=ok operating_hours=65i,pressure=1101.2,temperature=29.7 1722443553000000000\n" +
		"device,location=factory_south,source=gateway_af43e status=\"ok\" 1722443559000000000\n" +
		"serializer_test,location=factory_south,machine=C,source=gateway_af43e,status=off operating_hours=0i,pressure=0,temperature=0 1722443562000000000\n"

	// Setup the limited serializer
	s := &influx.Serializer{SortFields: true}
	require.NoError(t, s.Init())
	serializer := NewIndividualSerializer(s)

	// Do the first serialization runs with all metrics
	buf, err := serializer.SerializeBatch(input, math.MaxInt64)
	require.NoError(t, err)
	require.Equal(t, expected, string(buf))
}
