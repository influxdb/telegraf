package models

import (
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	logging "github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/selfstat"
)

type RunningAggregator struct {
	sync.Mutex
	Aggregator  telegraf.Aggregator
	Config      *AggregatorConfig
	periodStart time.Time
	periodEnd   time.Time
	log         telegraf.Logger

	MetricsPushed   selfstat.Stat
	MetricsFiltered selfstat.Stat
	MetricsDropped  selfstat.Stat
	PushTime        selfstat.Stat
}

func NewRunningAggregator(aggregator telegraf.Aggregator, config *AggregatorConfig) *RunningAggregator {
	tags := map[string]string{"aggregator": config.Name}
	if config.Alias != "" {
		tags["alias"] = config.Alias
	}

	aggErrorsRegister := selfstat.Register("aggregate", "errors", tags)
	logger := logging.New("aggregators", config.Name, config.Alias)
	logger.RegisterErrorCallback(func() {
		aggErrorsRegister.Incr(1)
	})
	if err := logger.SetLogLevel(config.LogLevel); err != nil {
		logger.Error(err)
	}
	SetLoggerOnPlugin(aggregator, logger)

	return &RunningAggregator{
		Aggregator: aggregator,
		Config:     config,
		MetricsPushed: selfstat.Register(
			"aggregate",
			"metrics_pushed",
			tags,
		),
		MetricsFiltered: selfstat.Register(
			"aggregate",
			"metrics_filtered",
			tags,
		),
		MetricsDropped: selfstat.Register(
			"aggregate",
			"metrics_dropped",
			tags,
		),
		PushTime: selfstat.Register(
			"aggregate",
			"push_time_ns",
			tags,
		),
		log: logger,
	}
}

// AggregatorConfig is the common config for all aggregators.
type AggregatorConfig struct {
	Name         string
	Alias        string
	ID           string
	DropOriginal bool
	Period       time.Duration
	Delay        time.Duration
	Grace        time.Duration
	LogLevel     string

	NameOverride      string
	MeasurementPrefix string
	MeasurementSuffix string
	Tags              map[string]string
	Filter            Filter
}

func (r *RunningAggregator) LogName() string {
	return logName("aggregators", r.Config.Name, r.Config.Alias)
}

func (r *RunningAggregator) Init() error {
	if p, ok := r.Aggregator.(telegraf.Initializer); ok {
		err := p.Init()
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RunningAggregator) ID() string {
	if p, ok := r.Aggregator.(telegraf.PluginWithID); ok {
		return p.ID()
	}
	return r.Config.ID
}

func (r *RunningAggregator) Period() time.Duration {
	return r.Config.Period
}

func (r *RunningAggregator) EndPeriod() time.Time {
	return r.periodEnd
}

func (r *RunningAggregator) UpdateWindow(start, until time.Time) {
	r.periodStart = start
	r.periodEnd = until
	r.log.Debugf("Updated aggregation range [%s, %s]", start, until)
}

func (r *RunningAggregator) MakeMetric(telegrafMetric telegraf.Metric) telegraf.Metric {
	m := makeMetric(
		telegrafMetric,
		r.Config.NameOverride,
		r.Config.MeasurementPrefix,
		r.Config.MeasurementSuffix,
		r.Config.Tags,
		nil)

	r.MetricsPushed.Incr(1)

	return m
}

// Add a metric to the aggregator and return true if the original metric
// should be dropped.
func (r *RunningAggregator) Add(m telegraf.Metric) bool {
	ok, err := r.Config.Filter.Select(m)
	if err != nil {
		r.log.Errorf("filtering failed: %v", err)
	} else if !ok {
		return false
	}

	// Make a copy of the metric but don't retain tracking.  We do not fail a
	// delivery due to the aggregation not being sent because we can't create
	// aggregations of historical data.  Additionally, waiting for the
	// aggregation to be pushed would introduce a hefty latency to delivery.
	m = metric.FromMetric(m)

	r.Config.Filter.Modify(m)
	if len(m.FieldList()) == 0 {
		r.MetricsFiltered.Incr(1)
		return r.Config.DropOriginal
	}

	r.Lock()
	defer r.Unlock()

	if m.Time().Before(r.periodStart.Add(-r.Config.Grace)) || m.Time().After(r.periodEnd.Add(r.Config.Delay)) {
		r.log.Debugf("Metric is outside aggregation window; discarding. %s: m: %s e: %s g: %s",
			m.Time(), r.periodStart, r.periodEnd, r.Config.Grace)
		r.MetricsDropped.Incr(1)
		return r.Config.DropOriginal
	}

	r.Aggregator.Add(m)
	return r.Config.DropOriginal
}

func (r *RunningAggregator) Push(acc telegraf.Accumulator) {
	r.Lock()
	defer r.Unlock()

	since := r.periodEnd
	until := r.periodEnd.Add(r.Config.Period)
	r.UpdateWindow(since, until)

	start := time.Now()
	r.Aggregator.Push(acc)
	elapsed := time.Since(start)
	r.PushTime.Incr(elapsed.Nanoseconds())
	r.Aggregator.Reset()
}

func (r *RunningAggregator) Log() telegraf.Logger {
	return r.log
}
