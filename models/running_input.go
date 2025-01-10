package models

import (
	"errors"
	"fmt"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	logging "github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/selfstat"
)

var (
	GlobalMetricsGathered = selfstat.Register("agent", "metrics_gathered", make(map[string]string))
	GlobalGatherErrors    = selfstat.Register("agent", "gather_errors", make(map[string]string))
	GlobalGatherTimeouts  = selfstat.Register("agent", "gather_timeouts", make(map[string]string))
)

type RunningInput struct {
	Input  telegraf.Input
	Config *InputConfig

	log         telegraf.Logger
	defaultTags map[string]string

	startAcc    telegraf.Accumulator
	started     bool
	retries     uint64
	gatherStart time.Time
	gatherEnd   time.Time

	MetricsGathered selfstat.Stat
	GatherTime      selfstat.Stat
	GatherTimeouts  selfstat.Stat
	StartupErrors   selfstat.Stat
}

func NewRunningInput(input telegraf.Input, config *InputConfig) *RunningInput {
	tags := map[string]string{"input": config.Name}
	if config.Alias != "" {
		tags["alias"] = config.Alias
	}

	inputErrorsRegister := selfstat.Register("gather", "errors", tags)
	logger := logging.New("inputs", config.Name, config.Alias)
	logger.RegisterErrorCallback(func() {
		inputErrorsRegister.Incr(1)
		GlobalGatherErrors.Incr(1)
	})
	if err := logger.SetLogLevel(config.LogLevel); err != nil {
		logger.Error(err)
	}
	SetLoggerOnPlugin(input, logger)

	return &RunningInput{
		Input:  input,
		Config: config,
		MetricsGathered: selfstat.Register(
			"gather",
			"metrics_gathered",
			tags,
		),
		GatherTime: selfstat.RegisterTiming(
			"gather",
			"gather_time_ns",
			tags,
		),
		GatherTimeouts: selfstat.Register(
			"gather",
			"gather_timeouts",
			tags,
		),
		StartupErrors: selfstat.Register(
			"write",
			"startup_errors",
			tags,
		),
		log: logger,
	}
}

// InputConfig is the common config for all inputs.
type InputConfig struct {
	Name                 string
	Alias                string
	ID                   string
	Interval             time.Duration
	CollectionJitter     time.Duration
	CollectionOffset     time.Duration
	Precision            time.Duration
	TimeSource           string
	StartupErrorBehavior string
	LogLevel             string

	NameOverride            string
	MeasurementPrefix       string
	MeasurementSuffix       string
	Tags                    map[string]string
	Filter                  Filter
	AlwaysIncludeLocalTags  bool
	AlwaysIncludeGlobalTags bool
}

func (r *RunningInput) metricFiltered(metric telegraf.Metric) {
	metric.Drop()
}

func (r *RunningInput) LogName() string {
	return logName("inputs", r.Config.Name, r.Config.Alias)
}

func (r *RunningInput) Init() error {
	switch r.Config.StartupErrorBehavior {
	case "", "error", "retry", "ignore":
	default:
		return fmt.Errorf("invalid 'startup_error_behavior' setting %q", r.Config.StartupErrorBehavior)
	}

	switch r.Config.TimeSource {
	case "":
		r.Config.TimeSource = "metric"
	case "metric", "collection_start", "collection_end":
	default:
		return fmt.Errorf("invalid 'time_source' setting %q", r.Config.TimeSource)
	}

	if p, ok := r.Input.(telegraf.Initializer); ok {
		return p.Init()
	}
	return nil
}

func (r *RunningInput) Start(acc telegraf.Accumulator) error {
	plugin, ok := r.Input.(telegraf.ServiceInput)
	if !ok {
		return nil
	}

	// Try to start the plugin and exit early on success
	r.startAcc = acc
	err := plugin.Start(acc)
	if err == nil {
		r.started = true
		return nil
	}
	r.StartupErrors.Incr(1)

	// Check if the plugin reports a retry-able error, otherwise we exit.
	var serr *internal.StartupError
	if !errors.As(err, &serr) {
		return err
	}

	// Handle the retry-able error depending on the configured behavior
	switch r.Config.StartupErrorBehavior {
	case "", "error": // fall-trough to return the actual error
	case "retry":
		if !serr.Retry {
			return err
		}
		r.log.Infof("Startup failed: %v; retrying...", err)
		return nil
	case "ignore":
		return &internal.FatalError{Err: serr}
	default:
		r.log.Errorf("Invalid 'startup_error_behavior' setting %q", r.Config.StartupErrorBehavior)
	}

	return err
}

func (r *RunningInput) Stop() {
	if plugin, ok := r.Input.(telegraf.ServiceInput); ok {
		plugin.Stop()
	}
}

func (r *RunningInput) ID() string {
	if p, ok := r.Input.(telegraf.PluginWithID); ok {
		return p.ID()
	}
	return r.Config.ID
}

func (r *RunningInput) MakeMetric(metric telegraf.Metric) telegraf.Metric {
	ok, err := r.Config.Filter.Select(metric)
	if err != nil {
		r.log.Errorf("filtering failed: %v", err)
	} else if !ok {
		r.metricFiltered(metric)
		return nil
	}

	makeMetric(
		metric,
		r.Config.NameOverride,
		r.Config.MeasurementPrefix,
		r.Config.MeasurementSuffix,
		r.Config.Tags,
		r.defaultTags)

	r.Config.Filter.Modify(metric)
	if len(metric.FieldList()) == 0 {
		r.metricFiltered(metric)
		return nil
	}

	if r.Config.AlwaysIncludeLocalTags || r.Config.AlwaysIncludeGlobalTags {
		var local, global map[string]string
		if r.Config.AlwaysIncludeLocalTags {
			local = r.Config.Tags
		}
		if r.Config.AlwaysIncludeGlobalTags {
			global = r.defaultTags
		}
		makeMetric(metric, "", "", "", local, global)
	}

	switch r.Config.TimeSource {
	case "collection_start":
		metric.SetTime(r.gatherStart)
	case "collection_end":
		metric.SetTime(r.gatherEnd)
	default:
	}

	r.MetricsGathered.Incr(1)
	GlobalMetricsGathered.Incr(1)
	return metric
}

func (r *RunningInput) Gather(acc telegraf.Accumulator) error {
	// Try to connect if we are not yet started up
	if plugin, ok := r.Input.(telegraf.ServiceInput); ok && !r.started {
		r.retries++
		if err := plugin.Start(r.startAcc); err != nil {
			var serr *internal.StartupError
			if !errors.As(err, &serr) || !serr.Retry || !serr.Partial {
				r.StartupErrors.Incr(1)
				return internal.ErrNotConnected
			}
			r.log.Debugf("Partially connected after %d attempts", r.retries)
		} else {
			r.started = true
			r.log.Debugf("Successfully connected after %d attempts", r.retries)
		}
	}

	r.gatherStart = time.Now()
	err := r.Input.Gather(acc)
	r.gatherEnd = time.Now()

	r.GatherTime.Incr(r.gatherEnd.Sub(r.gatherStart).Nanoseconds())
	return err
}

func (r *RunningInput) SetDefaultTags(tags map[string]string) {
	r.defaultTags = tags
}

func (r *RunningInput) Log() telegraf.Logger {
	return r.log
}

func (r *RunningInput) IncrGatherTimeouts() {
	GlobalGatherTimeouts.Incr(1)
	r.GatherTimeouts.Incr(1)
}
