package derivative

import (
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/aggregators"
)

type Derivative struct {
	Variable    string          `toml:"variable"`
	Infix       string          `toml:"infix"`
	MaxRollOver uint            `toml:"max_roll_over"`
	Log         telegraf.Logger `toml:"-"`
	cache       map[uint64]aggregate
}

type aggregate struct {
	first    *event
	last     *event
	name     string
	tags     map[string]string
	rollOver uint
}

type event struct {
	fields map[string]float64
	time   time.Time
}

func NewDerivative() *Derivative {
	derivative := &Derivative{Infix: "_by_", MaxRollOver: 10}
	derivative.cache = make(map[uint64]aggregate)
	derivative.Reset()
	return derivative
}

var sampleConfig = `
	## This Aggregator will estimate a derivative for each field, which is
	## contained in both the first and last metric of the aggregation interval.
	## Without further configuration the derivative will be calculated with
	## respect to the time difference between these two measurements in seconds.
	## The formula applied is for every field:
	##
	##               value_last - value_first
	## derivative = --------------------------
	##              time_difference_in_seconds
	##
	## The resulting derivative will be named *fieldname_by_seconds*. The infix
	## "_by_" can be configured by the *infix* parameter.
	# infix = "_by_"
	##
	## As an abstraction the derivative can be calculated not only by the time
	## difference but by the difference of a field, which is contained in the
	## measurement. This field is assumed to be monotonously increasing. This
	## feature is used by specifying a *variable*.
	# variable = "parameter"
	##
	## When using a field as the derivation parameter the name of that field will
	## be used for the resulting derivative, e.g. *fieldname_by_parameter*.
	##
	## As a genereal parameter the period needs to be given. It determines the
	## period for which a derivative will be calculated
	period = "30s"
	##
	## Note, that the calculation is based on the actual timestamp of the
	## measurements. When there is only one measurement during that period, the
	## measurement will be rolled over to the next period. The maximum number of
	## such roll-overs can be configured with a default of 10.
	# max_roll_over = 10
	##
`

func (d *Derivative) SampleConfig() string {
	return sampleConfig
}

func (d *Derivative) Description() string {
	return "Calculates a derivative for every field."
}

func (d *Derivative) Add(in telegraf.Metric) {
	id := in.HashID()
	if current, ok := d.cache[id]; !ok {
		// hit an uncached metric, create caches for first time:
		d.cache[id] = newAggregate(in)
	} else {
		if current.first.time.After(in.Time()) {
			current.first = newEvent(in)
			current.rollOver = 0
		}
		if current.first.time.Equal(in.Time()) {
			upsertConvertedFields(in.Fields(), current.first.fields)
			current.rollOver = 0
		}
		if current.last.time.Before(in.Time()) {
			current.last = newEvent(in)
			current.rollOver = 0
		}
		if current.last.time.Equal(in.Time()) {
			upsertConvertedFields(in.Fields(), current.last.fields)
			current.rollOver = 0
		}

		d.cache[id] = current
	}
}

func newAggregate(in telegraf.Metric) aggregate {
	return aggregate{
		name:     in.Name(),
		tags:     in.Tags(),
		first:    newEvent(in),
		last:     newEvent(in),
		rollOver: 0,
	}
}

func newEvent(in telegraf.Metric) *event {
	return &event{
		fields: extractConvertedFields(in),
		time:   in.Time(),
	}
}

func extractConvertedFields(in telegraf.Metric) map[string]float64 {
	fields := make(map[string]float64, len(in.Fields()))
	upsertConvertedFields(in.Fields(), fields)
	return fields
}

func upsertConvertedFields(source map[string]interface{}, target map[string]float64) {
	for k, v := range source {
		if value, ok := convert(v); ok {
			target[k] = value
		}
	}
}

func convert(in interface{}) (float64, bool) {
	switch v := in.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

func (d *Derivative) Push(acc telegraf.Accumulator) {
	for _, aggregate := range d.cache {
		if aggregate.first == aggregate.last {
			d.Log.Debugf("Same first and last event for %q, skipping.", aggregate.name)
			continue
		}
		denominator := d.calculateDenominator(aggregate)
		if denominator == 0 {
			d.Log.Infof("Got difference 0 in denominator for %q, skipping.", aggregate.name)
			continue
		}
		derivatives := make(map[string]interface{})
		for key, start := range aggregate.first.fields {
			if end, ok := aggregate.last.fields[key]; key != d.Variable && ok {
				derivatives[d.derivativeFieldName(key)] = (end - start) / denominator
			}
		}
		acc.AddFields(aggregate.name, derivatives, aggregate.tags)
	}
}

func (d *Derivative) calculateDenominator(aggregate aggregate) float64 {
	if len(d.Variable) != 0 {
		return aggregate.last.fields[d.Variable] - aggregate.first.fields[d.Variable]
	}
	return aggregate.last.time.Sub(aggregate.first.time).Seconds()
}

func (d *Derivative) derivativeFieldName(field string) string {
	if len(d.Variable) != 0 {
		return field + d.Infix + d.Variable
	}
	return field + d.Infix + "seconds"
}

func (d *Derivative) Reset() {
	for id, aggregate := range d.cache {
		if aggregate.rollOver < d.MaxRollOver {
			aggregate.first = aggregate.last
			aggregate.rollOver = aggregate.rollOver + 1
			d.cache[id] = aggregate
		} else {
			delete(d.cache, id)
		}
	}
}

func (d *Derivative) Init() error {
	d.Infix = strings.TrimSpace(d.Infix)
	d.Variable = strings.TrimSpace(d.Variable)
	return nil
}

func init() {
	aggregators.Add("derivative", func() telegraf.Aggregator {
		return NewDerivative()
	})
}
