package models

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	logging "github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/selfstat"
)

const (
	// Default size of metrics batch size.
	DefaultMetricBatchSize = 1000

	// Default number of metrics kept. It should be a multiple of batch size.
	DefaultMetricBufferLimit = 10000
)

// OutputConfig containing name and filter
type OutputConfig struct {
	Name                 string
	Source               string
	Alias                string
	ID                   string
	StartupErrorBehavior string
	Filter               Filter

	FlushInterval     time.Duration
	FlushJitter       time.Duration
	MetricBufferLimit int
	MetricBatchSize   int

	NameOverride string
	NamePrefix   string
	NameSuffix   string

	BufferStrategy  string
	BufferDirectory string

	LogLevel string
}

// RunningOutput contains the output configuration
type RunningOutput struct {
	// Must be 64-bit aligned
	newMetricsCount int64
	droppedMetrics  int64

	Output            telegraf.Output
	Config            *OutputConfig
	MetricBufferLimit int
	MetricBatchSize   int

	MetricsFiltered selfstat.Stat
	WriteTime       selfstat.Stat
	StartupErrors   selfstat.Stat

	BatchReady chan time.Time

	buffer Buffer
	log    telegraf.Logger

	started bool
	retries uint64

	aggMutex sync.Mutex
}

func NewRunningOutput(output telegraf.Output, config *OutputConfig, batchSize, bufferLimit int) *RunningOutput {
	tags := map[string]string{"output": config.Name}
	if config.Alias != "" {
		tags["alias"] = config.Alias
	}

	writeErrorsRegister := selfstat.Register("write", "errors", tags)
	logger := logging.New("outputs", config.Name, config.Alias)
	logger.RegisterErrorCallback(func() {
		writeErrorsRegister.Incr(1)
	})
	if err := logger.SetLogLevel(config.LogLevel); err != nil {
		logger.Error(err)
	}
	SetLoggerOnPlugin(output, logger)

	if config.MetricBufferLimit > 0 {
		bufferLimit = config.MetricBufferLimit
	}
	if bufferLimit == 0 {
		bufferLimit = DefaultMetricBufferLimit
	}
	if config.MetricBatchSize > 0 {
		batchSize = config.MetricBatchSize
	}
	if batchSize == 0 {
		batchSize = DefaultMetricBatchSize
	}

	b, err := NewBuffer(config.Name, config.ID, config.Alias, bufferLimit, config.BufferStrategy, config.BufferDirectory)
	if err != nil {
		panic(err)
	}

	ro := &RunningOutput{
		buffer:            b,
		BatchReady:        make(chan time.Time, 1),
		Output:            output,
		Config:            config,
		MetricBufferLimit: bufferLimit,
		MetricBatchSize:   batchSize,
		MetricsFiltered: selfstat.Register(
			"write",
			"metrics_filtered",
			tags,
		),
		WriteTime: selfstat.RegisterTiming(
			"write",
			"write_time_ns",
			tags,
		),
		StartupErrors: selfstat.Register(
			"write",
			"startup_errors",
			tags,
		),
		log: logger,
	}

	return ro
}

func (r *RunningOutput) LogName() string {
	return logName("outputs", r.Config.Name, r.Config.Alias)
}

func (r *RunningOutput) metricFiltered(metric telegraf.Metric) {
	r.MetricsFiltered.Incr(1)
	metric.Drop()
}

func (r *RunningOutput) ID() string {
	if p, ok := r.Output.(telegraf.PluginWithID); ok {
		return p.ID()
	}
	return r.Config.ID
}

func (r *RunningOutput) Init() error {
	switch r.Config.StartupErrorBehavior {
	case "", "error", "retry", "ignore":
	default:
		return fmt.Errorf("invalid 'startup_error_behavior' setting %q", r.Config.StartupErrorBehavior)
	}

	if p, ok := r.Output.(telegraf.Initializer); ok {
		err := p.Init()
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RunningOutput) Connect() error {
	// Try to connect and exit early on success
	err := r.Output.Connect()
	if err == nil {
		r.started = true
		return nil
	}
	r.StartupErrors.Incr(1)

	// Check if the plugin reports a retry-able error, otherwise we exit.
	var serr *internal.StartupError
	if !errors.As(err, &serr) || !serr.Retry {
		return err
	}

	// Handle the retry-able error depending on the configured behavior
	switch r.Config.StartupErrorBehavior {
	case "", "error": // fall-trough to return the actual error
	case "retry":
		r.log.Infof("Connect failed: %v; retrying...", err)
		return nil
	case "ignore":
		return &internal.FatalError{Err: serr}
	default:
		r.log.Errorf("Invalid 'startup_error_behavior' setting %q", r.Config.StartupErrorBehavior)
	}

	return err
}

// Close closes the output
func (r *RunningOutput) Close() {
	if err := r.Output.Close(); err != nil {
		r.log.Errorf("Error closing output: %v", err)
	}

	if err := r.buffer.Close(); err != nil {
		r.log.Errorf("Error closing output buffer: %v", err)
	}
}

// AddMetric adds a metric to the output.
// The given metric will be copied if the output selects the metric.
func (r *RunningOutput) AddMetric(metric telegraf.Metric) {
	ok, err := r.Config.Filter.Select(metric)
	if err != nil {
		r.log.Errorf("filtering failed: %v", err)
	} else if !ok {
		r.MetricsFiltered.Incr(1)
		return
	}

	r.add(metric.Copy())
}

// AddMetricNoCopy adds a metric to the output.
// Takes ownership of metric regardless of whether the output selects it for outputting.
func (r *RunningOutput) AddMetricNoCopy(metric telegraf.Metric) {
	ok, err := r.Config.Filter.Select(metric)
	if err != nil {
		r.log.Errorf("filtering failed: %v", err)
	} else if !ok {
		r.metricFiltered(metric)
		return
	}

	r.add(metric)
}

func (r *RunningOutput) add(metric telegraf.Metric) {
	r.Config.Filter.Modify(metric)
	if len(metric.FieldList()) == 0 {
		r.metricFiltered(metric)
		return
	}

	if output, ok := r.Output.(telegraf.AggregatingOutput); ok {
		r.aggMutex.Lock()
		output.Add(metric)
		r.aggMutex.Unlock()
		return
	}

	if len(r.Config.NameOverride) > 0 {
		metric.SetName(r.Config.NameOverride)
	}

	if len(r.Config.NamePrefix) > 0 {
		metric.AddPrefix(r.Config.NamePrefix)
	}

	if len(r.Config.NameSuffix) > 0 {
		metric.AddSuffix(r.Config.NameSuffix)
	}

	dropped := r.buffer.Add(metric)
	atomic.AddInt64(&r.droppedMetrics, int64(dropped))

	count := atomic.AddInt64(&r.newMetricsCount, 1)
	if count == int64(r.MetricBatchSize) {
		atomic.StoreInt64(&r.newMetricsCount, 0)
		select {
		case r.BatchReady <- time.Now():
		default:
		}
	}
}

// Write writes all metrics to the output, stopping when all have been sent on
// or error.
func (r *RunningOutput) Write() error {
	// Try to connect if we are not yet started up
	if !r.started {
		r.retries++
		if err := r.Output.Connect(); err != nil {
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

	if output, ok := r.Output.(telegraf.AggregatingOutput); ok {
		r.aggMutex.Lock()
		metrics := output.Push()
		r.buffer.Add(metrics...)
		output.Reset()
		r.aggMutex.Unlock()
	}

	atomic.StoreInt64(&r.newMetricsCount, 0)

	// Only process the metrics in the buffer now. Metrics added while we are
	// writing will be sent on the next call.
	nBuffer := r.buffer.Len()
	nBatches := nBuffer/r.MetricBatchSize + 1
	for i := 0; i < nBatches; i++ {
		tx := r.buffer.BeginTransaction(r.MetricBatchSize)
		if len(tx.Batch) == 0 {
			return nil
		}
		err := r.writeMetrics(tx.Batch)
		r.updateTransaction(tx, err)
		r.buffer.EndTransaction(tx)
		if err != nil {
			return err
		}
	}
	return nil
}

// WriteBatch writes a single batch of metrics to the output.
func (r *RunningOutput) WriteBatch() error {
	// Try to connect if we are not yet started up
	if !r.started {
		r.retries++
		if err := r.Output.Connect(); err != nil {
			r.StartupErrors.Incr(1)
			return internal.ErrNotConnected
		}
		r.started = true
		r.log.Debugf("Successfully connected after %d attempts", r.retries)
	}

	tx := r.buffer.BeginTransaction(r.MetricBatchSize)
	if len(tx.Batch) == 0 {
		return nil
	}
	err := r.writeMetrics(tx.Batch)
	r.updateTransaction(tx, err)
	r.buffer.EndTransaction(tx)

	return err
}

func (r *RunningOutput) writeMetrics(metrics []telegraf.Metric) error {
	dropped := atomic.LoadInt64(&r.droppedMetrics)
	if dropped > 0 {
		r.log.Warnf("Metric buffer overflow; %d metrics have been dropped", dropped)
		atomic.StoreInt64(&r.droppedMetrics, 0)
	}

	start := time.Now()
	err := r.Output.Write(metrics)
	elapsed := time.Since(start)
	r.WriteTime.Incr(elapsed.Nanoseconds())

	if err == nil {
		r.log.Debugf("Wrote batch of %d metrics in %s", len(metrics), elapsed)
	}
	return err
}

func (*RunningOutput) updateTransaction(tx *Transaction, err error) {
	// No error indicates all metrics were written successfully
	if err == nil {
		tx.AcceptAll()
		return
	}

	// A non-partial-write-error indicated none of the metrics were written
	// successfully and we should keep them for the next write cycle
	var writeErr *internal.PartialWriteError
	if !errors.As(err, &writeErr) {
		tx.KeepAll()
		return
	}

	// Transfer the accepted and rejected indices based on the write error values
	tx.Accept = writeErr.MetricsAccept
	tx.Reject = writeErr.MetricsReject
}

func (r *RunningOutput) LogBufferStatus() {
	nBuffer := r.buffer.Len()
	if r.Config.BufferStrategy == "disk" {
		r.log.Debugf("Buffer fullness: %d metrics", nBuffer)
	} else {
		r.log.Debugf("Buffer fullness: %d / %d metrics", nBuffer, r.MetricBufferLimit)
	}
}

func (r *RunningOutput) Log() telegraf.Logger {
	return r.log
}

func (r *RunningOutput) BufferLength() int {
	return r.buffer.Len()
}
