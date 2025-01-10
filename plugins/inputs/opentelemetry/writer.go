package opentelemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/influxdata/influxdb-observability/common"
	"github.com/influxdata/influxdb-observability/otel2influx"

	"github.com/influxdata/telegraf"
)

var (
	_ otel2influx.InfluxWriter      = (*writeToAccumulator)(nil)
	_ otel2influx.InfluxWriterBatch = (*writeToAccumulator)(nil)
)

type writeToAccumulator struct {
	accumulator telegraf.Accumulator
}

func (w *writeToAccumulator) NewBatch() otel2influx.InfluxWriterBatch {
	return w
}

func (w *writeToAccumulator) EnqueuePoint(
	_ context.Context,
	measurement string,
	tags map[string]string,
	fields map[string]interface{},
	ts time.Time,
	vType common.InfluxMetricValueType,
) error {
	switch vType {
	case common.InfluxMetricValueTypeUntyped:
		w.accumulator.AddFields(measurement, fields, tags, ts)
	case common.InfluxMetricValueTypeGauge:
		w.accumulator.AddGauge(measurement, fields, tags, ts)
	case common.InfluxMetricValueTypeSum:
		w.accumulator.AddCounter(measurement, fields, tags, ts)
	case common.InfluxMetricValueTypeHistogram:
		w.accumulator.AddHistogram(measurement, fields, tags, ts)
	case common.InfluxMetricValueTypeSummary:
		w.accumulator.AddSummary(measurement, fields, tags, ts)
	default:
		return fmt.Errorf("unrecognized InfluxMetricValueType %q", vType)
	}
	return nil
}

func (*writeToAccumulator) WriteBatch(context.Context) error {
	return nil
}
