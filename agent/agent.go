package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/snmp"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/processors"
	"github.com/influxdata/telegraf/plugins/serializers/influx"
)

// Agent runs a set of plugins.
type Agent struct {
	Config *config.Config
}

// NewAgent returns an Agent for the given Config.
func NewAgent(cfg *config.Config) *Agent {
	a := &Agent{
		Config: cfg,
	}
	return a
}

// inputUnit is a group of input plugins and the shared channel they write to.
//
// ┌───────┐
// │ Input │───┐
// └───────┘   │
// ┌───────┐   │     ______
// │ Input │───┼──▶ ()_____)
// └───────┘   │
// ┌───────┐   │
// │ Input │───┘
// └───────┘
type inputUnit struct {
	dst    chan<- telegraf.Metric
	inputs []*models.RunningInput
}

//  ______     ┌───────────┐     ______
// ()_____)──▶ │ Processor │──▶ ()_____)
//             └───────────┘

type processorUnit struct {
	src       <-chan telegraf.Metric
	dst       chan<- telegraf.Metric
	processor *models.RunningProcessor
}

// aggregatorUnit is a group of Aggregators and their source and sink channels.
// Typically, the aggregators write to a processor channel and pass the original
// metrics to the output channel.  The sink channels may be the same channel.

//                 ┌────────────┐
//            ┌──▶ │ Aggregator │───┐
//            │    └────────────┘   │
//  ______    │    ┌────────────┐   │     ______
// ()_____)───┼──▶ │ Aggregator │───┼──▶ ()_____)
//            │    └────────────┘   │
//            │    ┌────────────┐   │
//            ├──▶ │ Aggregator │───┘
//            │    └────────────┘
//            │                           ______
//            └────────────────────────▶ ()_____)

type aggregatorUnit struct {
	src         <-chan telegraf.Metric
	aggC        chan<- telegraf.Metric
	outputC     chan<- telegraf.Metric
	aggregators []*models.RunningAggregator
}

// outputUnit is a group of Outputs and their source channel.  Metrics on the
// channel are written to all outputs.

//                            ┌────────┐
//                       ┌──▶ │ Output │
//                       │    └────────┘
//  ______     ┌─────┐   │    ┌────────┐
// ()_____)──▶ │ Fan │───┼──▶ │ Output │
//             └─────┘   │    └────────┘
//                       │    ┌────────┐
//                       └──▶ │ Output │
//                            └────────┘

type outputUnit struct {
	src     <-chan telegraf.Metric
	outputs []*models.RunningOutput
}

// Run starts and runs the Agent until the context is done.
func (a *Agent) Run(ctx context.Context) error {
	log.Printf("I! [agent] Config: Interval:%s, Quiet:%#v, Hostname:%#v, "+
		"Flush Interval:%s",
		time.Duration(a.Config.Agent.Interval), a.Config.Agent.Quiet,
		a.Config.Agent.Hostname, time.Duration(a.Config.Agent.FlushInterval))

	// Set the default for processor skipping
	if a.Config.Agent.SkipProcessorsAfterAggregators == nil {
		msg := `The default value of 'skip_processors_after_aggregators' will change to 'true' with Telegraf v1.40.0! `
		msg += `If you need the current default behavior, please explicitly set the option to 'false'!`
		log.Print("W! [agent] ", color.YellowString(msg))
		skipProcessorsAfterAggregators := false
		a.Config.Agent.SkipProcessorsAfterAggregators = &skipProcessorsAfterAggregators
	}

	log.Printf("D! [agent] Initializing plugins")
	if err := a.InitPlugins(); err != nil {
		return err
	}

	if a.Config.Persister != nil {
		log.Printf("D! [agent] Initializing plugin states")
		if err := a.initPersister(); err != nil {
			return err
		}
		if err := a.Config.Persister.Load(); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			log.Print("I! [agent] State file does not exist... Skip restoring states...")
		}
	}

	startTime := time.Now()

	log.Printf("D! [agent] Connecting outputs")
	next, ou, err := a.startOutputs(ctx, a.Config.Outputs)
	if err != nil {
		return err
	}

	var apu []*processorUnit
	var au *aggregatorUnit
	if len(a.Config.Aggregators) != 0 {
		aggC := next
		if len(a.Config.AggProcessors) != 0 && !*a.Config.Agent.SkipProcessorsAfterAggregators {
			aggC, apu, err = a.startProcessors(next, a.Config.AggProcessors)
			if err != nil {
				return err
			}
		}

		next, au = a.startAggregators(aggC, next, a.Config.Aggregators)
	}

	var pu []*processorUnit
	if len(a.Config.Processors) != 0 {
		next, pu, err = a.startProcessors(next, a.Config.Processors)
		if err != nil {
			return err
		}
	}

	iu, err := a.startInputs(next, a.Config.Inputs)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runOutputs(ou)
	}()

	if au != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runProcessors(apu)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runAggregators(startTime, au)
		}()
	}

	if pu != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runProcessors(pu)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runInputs(ctx, startTime, iu)
	}()

	wg.Wait()

	if a.Config.Persister != nil {
		log.Printf("D! [agent] Persisting plugin states")
		if err := a.Config.Persister.Store(); err != nil {
			return err
		}
	}

	log.Printf("D! [agent] Stopped Successfully")
	return err
}

// InitPlugins runs the Init function on plugins.
func (a *Agent) InitPlugins() error {
	for _, input := range a.Config.Inputs {
		// Share the snmp translator setting with plugins that need it.
		if tp, ok := input.Input.(snmp.TranslatorPlugin); ok {
			tp.SetTranslator(a.Config.Agent.SnmpTranslator)
		}
		err := input.Init()
		if err != nil {
			return fmt.Errorf("could not initialize input %s: %w", input.LogName(), err)
		}
	}
	for _, processor := range a.Config.Processors {
		err := processor.Init()
		if err != nil {
			return fmt.Errorf("could not initialize processor %s: %w", processor.LogName(), err)
		}
	}
	for _, aggregator := range a.Config.Aggregators {
		err := aggregator.Init()
		if err != nil {
			return fmt.Errorf("could not initialize aggregator %s: %w", aggregator.LogName(), err)
		}
	}
	if !*a.Config.Agent.SkipProcessorsAfterAggregators {
		for _, processor := range a.Config.AggProcessors {
			err := processor.Init()
			if err != nil {
				return fmt.Errorf("could not initialize processor %s: %w", processor.LogName(), err)
			}
		}
	}
	for _, output := range a.Config.Outputs {
		err := output.Init()
		if err != nil {
			return fmt.Errorf("could not initialize output %s: %w", output.LogName(), err)
		}
	}
	return nil
}

// initPersister initializes the persister and registers the plugins.
func (a *Agent) initPersister() error {
	if err := a.Config.Persister.Init(); err != nil {
		return err
	}

	for _, input := range a.Config.Inputs {
		plugin, ok := input.Input.(telegraf.StatefulPlugin)
		if !ok {
			continue
		}

		name := input.LogName()
		id := input.ID()
		if err := a.Config.Persister.Register(id, plugin); err != nil {
			return fmt.Errorf("could not register input %s: %w", name, err)
		}
	}

	for _, processor := range a.Config.Processors {
		var plugin telegraf.StatefulPlugin
		if p, ok := processor.Processor.(processors.HasUnwrap); ok {
			plugin, ok = p.Unwrap().(telegraf.StatefulPlugin)
			if !ok {
				continue
			}
		} else {
			plugin, ok = processor.Processor.(telegraf.StatefulPlugin)
			if !ok {
				continue
			}
		}

		name := processor.LogName()
		id := processor.ID()
		if err := a.Config.Persister.Register(id, plugin); err != nil {
			return fmt.Errorf("could not register processor %s: %w", name, err)
		}
	}

	for _, aggregator := range a.Config.Aggregators {
		plugin, ok := aggregator.Aggregator.(telegraf.StatefulPlugin)
		if !ok {
			continue
		}

		name := aggregator.LogName()
		id := aggregator.ID()
		if err := a.Config.Persister.Register(id, plugin); err != nil {
			return fmt.Errorf("could not register aggregator %s: %w", name, err)
		}
	}

	for _, processor := range a.Config.AggProcessors {
		plugin, ok := processor.Processor.(telegraf.StatefulPlugin)
		if !ok {
			continue
		}

		name := processor.LogName()
		id := processor.ID()
		if err := a.Config.Persister.Register(id, plugin); err != nil {
			return fmt.Errorf("could not register aggregating processor %s: %w", name, err)
		}
	}

	for _, output := range a.Config.Outputs {
		plugin, ok := output.Output.(telegraf.StatefulPlugin)
		if !ok {
			continue
		}

		name := output.LogName()
		id := output.ID()
		if err := a.Config.Persister.Register(id, plugin); err != nil {
			return fmt.Errorf("could not register output %s: %w", name, err)
		}
	}

	return nil
}

func (a *Agent) startInputs(
	dst chan<- telegraf.Metric,
	inputs []*models.RunningInput,
) (*inputUnit, error) {
	log.Printf("D! [agent] Starting service inputs")

	unit := &inputUnit{
		dst: dst,
	}

	for _, input := range inputs {
		// Service input plugins are not normally subject to timestamp
		// rounding except for when precision is set on the input plugin.
		//
		// This only applies to the accumulator passed to Start(), the
		// Gather() accumulator does apply rounding according to the
		// precision and interval agent/plugin settings.
		var interval time.Duration
		var precision time.Duration
		if input.Config.Precision != 0 {
			precision = input.Config.Precision
		}

		acc := NewAccumulator(input, dst)
		acc.SetPrecision(getPrecision(precision, interval))

		if err := input.Start(acc); err != nil {
			// If the model tells us to remove the plugin we do so without error
			var fatalErr *internal.FatalError
			if errors.As(err, &fatalErr) {
				log.Printf("I! [agent] Failed to start %s, shutting down plugin: %s", input.LogName(), err)
				continue
			}

			stopRunningInputs(unit.inputs)

			return nil, fmt.Errorf("starting input %s: %w", input.LogName(), err)
		}
		unit.inputs = append(unit.inputs, input)
	}

	return unit, nil
}

// runInputs starts and triggers the periodic gather for Inputs.
//
// When the context is done the timers are stopped and this function returns
// after all ongoing Gather calls complete.
func (a *Agent) runInputs(
	ctx context.Context,
	startTime time.Time,
	unit *inputUnit,
) {
	var wg sync.WaitGroup
	tickers := make([]Ticker, 0, len(unit.inputs))
	for _, input := range unit.inputs {
		// Overwrite agent interval if this plugin has its own.
		interval := time.Duration(a.Config.Agent.Interval)
		if input.Config.Interval != 0 {
			interval = input.Config.Interval
		}

		// Overwrite agent precision if this plugin has its own.
		precision := time.Duration(a.Config.Agent.Precision)
		if input.Config.Precision != 0 {
			precision = input.Config.Precision
		}

		// Overwrite agent collection_jitter if this plugin has its own.
		jitter := time.Duration(a.Config.Agent.CollectionJitter)
		if input.Config.CollectionJitter != 0 {
			jitter = input.Config.CollectionJitter
		}

		// Overwrite agent collection_offset if this plugin has its own.
		offset := time.Duration(a.Config.Agent.CollectionOffset)
		if input.Config.CollectionOffset != 0 {
			offset = input.Config.CollectionOffset
		}

		var ticker Ticker
		if a.Config.Agent.RoundInterval {
			ticker = NewAlignedTicker(startTime, interval, jitter, offset)
		} else {
			ticker = NewUnalignedTicker(interval, jitter, offset)
		}
		tickers = append(tickers, ticker)

		acc := NewAccumulator(input, unit.dst)
		acc.SetPrecision(getPrecision(precision, interval))

		wg.Add(1)
		go func(input *models.RunningInput) {
			defer wg.Done()
			a.gatherLoop(ctx, acc, input, ticker, interval)
		}(input)
	}
	defer stopTickers(tickers)
	wg.Wait()

	log.Printf("D! [agent] Stopping service inputs")
	stopRunningInputs(unit.inputs)

	close(unit.dst)
	log.Printf("D! [agent] Input channel closed")
}

// testStartInputs is a variation of startInputs for use in --test and --once
// mode. It differs by logging Start errors and returning only plugins
// successfully started.
func (a *Agent) testStartInputs(
	dst chan<- telegraf.Metric,
	inputs []*models.RunningInput,
) *inputUnit {
	log.Printf("D! [agent] Starting service inputs")

	unit := &inputUnit{
		dst: dst,
	}

	for _, input := range inputs {
		// Service input plugins are not subject to timestamp rounding.
		// This only applies to the accumulator passed to Start(), the
		// Gather() accumulator does apply rounding according to the
		// precision agent setting.
		acc := NewAccumulator(input, dst)
		acc.SetPrecision(time.Nanosecond)

		if err := input.Start(acc); err != nil {
			log.Printf("E! [agent] Starting input %s: %v", input.LogName(), err)
			continue
		}

		unit.inputs = append(unit.inputs, input)
	}

	return unit
}

// testRunInputs is a variation of runInputs for use in --test and --once mode.
// Instead of using a ticker to run the inputs they are called once immediately.
func (a *Agent) testRunInputs(
	ctx context.Context,
	wait time.Duration,
	unit *inputUnit,
) {
	var wg sync.WaitGroup

	nul := make(chan telegraf.Metric)
	go func() {
		//nolint:revive // empty block needed here
		for range nul {
		}
	}()

	for _, input := range unit.inputs {
		wg.Add(1)
		go func(input *models.RunningInput) {
			defer wg.Done()

			// Overwrite agent interval if this plugin has its own.
			interval := time.Duration(a.Config.Agent.Interval)
			if input.Config.Interval != 0 {
				interval = input.Config.Interval
			}

			// Overwrite agent precision if this plugin has its own.
			precision := time.Duration(a.Config.Agent.Precision)
			if input.Config.Precision != 0 {
				precision = input.Config.Precision
			}

			// Run plugins that require multiple gathers to calculate rate
			// and delta metrics twice.
			switch input.Config.Name {
			case "cpu", "mongodb", "procstat":
				nulAcc := NewAccumulator(input, nul)
				nulAcc.SetPrecision(getPrecision(precision, interval))
				if err := input.Input.Gather(nulAcc); err != nil {
					nulAcc.AddError(err)
				}

				time.Sleep(500 * time.Millisecond)
			}

			acc := NewAccumulator(input, unit.dst)
			acc.SetPrecision(getPrecision(precision, interval))

			if err := input.Input.Gather(acc); err != nil {
				acc.AddError(err)
			}
		}(input)
	}
	wg.Wait()

	if err := internal.SleepContext(ctx, wait); err != nil {
		log.Printf("E! [agent] SleepContext finished with: %v", err)
	}

	log.Printf("D! [agent] Stopping service inputs")
	stopRunningInputs(unit.inputs)

	close(unit.dst)
	log.Printf("D! [agent] Input channel closed")
}

// stopRunningInputs stops all service inputs.
func stopRunningInputs(inputs []*models.RunningInput) {
	for _, input := range inputs {
		input.Stop()
	}
}

// stopRunningOutputs stops all running outputs.
func stopRunningOutputs(outputs []*models.RunningOutput) {
	for _, output := range outputs {
		output.Close()
	}
}

// gather runs an input's gather function periodically until the context is
// done.
func (a *Agent) gatherLoop(
	ctx context.Context,
	acc telegraf.Accumulator,
	input *models.RunningInput,
	ticker Ticker,
	interval time.Duration,
) {
	for {
		select {
		case <-ticker.Elapsed():
			err := a.gatherOnce(acc, input, ticker, interval)
			if err != nil {
				acc.AddError(err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// gatherOnce runs the input's Gather function once, logging a warning each
// interval it fails to complete before.
func (a *Agent) gatherOnce(
	acc telegraf.Accumulator,
	input *models.RunningInput,
	ticker Ticker,
	interval time.Duration,
) error {
	done := make(chan error)
	go func() {
		defer panicRecover(input)
		done <- input.Gather(acc)
	}()

	// Only warn after interval seconds, even if the interval is started late.
	// Intervals can start late if the previous interval went over or due to
	// clock changes.
	slowWarning := time.NewTicker(interval)
	defer slowWarning.Stop()

	for {
		select {
		case err := <-done:
			return err
		case <-slowWarning.C:
			log.Printf("W! [%s] Collection took longer than expected; not complete after interval of %s",
				input.LogName(), interval)
			input.IncrGatherTimeouts()
		case <-ticker.Elapsed():
			log.Printf("D! [%s] Previous collection has not completed; scheduled collection skipped",
				input.LogName())
		}
	}
}

// startProcessors sets up the processor chain and calls Start on all
// processors.  If an error occurs any started processors are Stopped.
func (a *Agent) startProcessors(
	dst chan<- telegraf.Metric,
	runningProcessors models.RunningProcessors,
) (chan<- telegraf.Metric, []*processorUnit, error) {
	var src chan telegraf.Metric
	units := make([]*processorUnit, 0, len(runningProcessors))
	// The processor chain is constructed from the output side starting from
	// the output(s) and walking the way back to the input(s). However, the
	// processor-list is sorted by order and/or by appearance in the config,
	// i.e. in input-to-output direction. Therefore, reverse the processor list
	// to reflect the order/definition order in the processing chain.
	for i := len(runningProcessors) - 1; i >= 0; i-- {
		processor := runningProcessors[i]

		src = make(chan telegraf.Metric, 100)
		acc := NewAccumulator(processor, dst)

		err := processor.Start(acc)
		if err != nil {
			for _, u := range units {
				u.processor.Stop()
				close(u.dst)
			}
			return nil, nil, fmt.Errorf("starting processor %s: %w", processor.LogName(), err)
		}

		units = append(units, &processorUnit{
			src:       src,
			dst:       dst,
			processor: processor,
		})

		dst = src
	}

	return src, units, nil
}

// runProcessors begins processing metrics and runs until the source channel is
// closed and all metrics have been written.
func (a *Agent) runProcessors(
	units []*processorUnit,
) {
	var wg sync.WaitGroup
	for _, unit := range units {
		wg.Add(1)
		go func(unit *processorUnit) {
			defer wg.Done()

			acc := NewAccumulator(unit.processor, unit.dst)
			for m := range unit.src {
				if err := unit.processor.Add(m, acc); err != nil {
					acc.AddError(err)
					m.Drop()
				}
			}
			unit.processor.Stop()
			close(unit.dst)
			log.Printf("D! [agent] Processor channel closed")
		}(unit)
	}
	wg.Wait()
}

// startAggregators sets up the aggregator unit and returns the source channel.
func (a *Agent) startAggregators(aggC, outputC chan<- telegraf.Metric, aggregators []*models.RunningAggregator) (chan<- telegraf.Metric, *aggregatorUnit) {
	src := make(chan telegraf.Metric, 100)
	unit := &aggregatorUnit{
		src:         src,
		aggC:        aggC,
		outputC:     outputC,
		aggregators: aggregators,
	}
	return src, unit
}

// runAggregators beings aggregating metrics and runs until the source channel
// is closed and all metrics have been written.
func (a *Agent) runAggregators(
	startTime time.Time,
	unit *aggregatorUnit,
) {
	ctx, cancel := context.WithCancel(context.Background())

	// Before calling Add, initialize the aggregation window.  This ensures
	// that any metric created after start time will be aggregated.
	for _, agg := range a.Config.Aggregators {
		since, until := updateWindow(startTime, a.Config.Agent.RoundInterval, agg.Period())
		agg.UpdateWindow(since, until)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for metric := range unit.src {
			var dropOriginal bool
			for _, agg := range a.Config.Aggregators {
				if ok := agg.Add(metric); ok {
					dropOriginal = true
				}
			}

			if !dropOriginal {
				unit.outputC <- metric // keep original.
			} else {
				metric.Drop()
			}
		}
		cancel()
	}()

	for _, agg := range a.Config.Aggregators {
		wg.Add(1)
		go func(agg *models.RunningAggregator) {
			defer wg.Done()

			interval := time.Duration(a.Config.Agent.Interval)
			precision := time.Duration(a.Config.Agent.Precision)

			acc := NewAccumulator(agg, unit.aggC)
			acc.SetPrecision(getPrecision(precision, interval))
			a.push(ctx, agg, acc)
		}(agg)
	}

	wg.Wait()

	// In the case that there are no processors, both aggC and outputC are the
	// same channel.  If there are processors, we close the aggC and the
	// processor chain will close the outputC when it finishes processing.
	close(unit.aggC)
	log.Printf("D! [agent] Aggregator channel closed")
}

func updateWindow(start time.Time, roundInterval bool, period time.Duration) (time.Time, time.Time) {
	var until time.Time
	if roundInterval {
		until = internal.AlignTime(start, period)
		if until.Equal(start) {
			until = internal.AlignTime(start.Add(time.Nanosecond), period)
		}
	} else {
		until = start.Add(period)
	}

	since := until.Add(-period)

	return since, until
}

// push runs the push for a single aggregator every period.
func (a *Agent) push(
	ctx context.Context,
	aggregator *models.RunningAggregator,
	acc telegraf.Accumulator,
) {
	for {
		// Ensures that Push will be called for each period, even if it has
		// already elapsed before this function is called.  This is guaranteed
		// because so long as only Push updates the EndPeriod.  This method
		// also avoids drift by not using a ticker.
		until := time.Until(aggregator.EndPeriod())

		select {
		case <-time.After(until):
			aggregator.Push(acc)
		case <-ctx.Done():
			aggregator.Push(acc)
			return
		}
	}
}

// startOutputs calls Connect on all outputs and returns the source channel.
// If an error occurs calling Connect, all started plugins have Close called.
func (a *Agent) startOutputs(
	ctx context.Context,
	outputs []*models.RunningOutput,
) (chan<- telegraf.Metric, *outputUnit, error) {
	src := make(chan telegraf.Metric, 100)
	unit := &outputUnit{src: src}
	for _, output := range outputs {
		if err := a.connectOutput(ctx, output); err != nil {
			var fatalErr *internal.FatalError
			if errors.As(err, &fatalErr) {
				// If the model tells us to remove the plugin we do so without error
				log.Printf("I! [agent] Failed to connect to [%s], error was %q;  shutting down plugin...", output.LogName(), err)
				output.Close()
				continue
			}

			for _, unitOutput := range unit.outputs {
				unitOutput.Close()
			}
			return nil, nil, fmt.Errorf("connecting output %s: %w", output.LogName(), err)
		}

		unit.outputs = append(unit.outputs, output)
	}

	return src, unit, nil
}

// connectOutput connects to all outputs.
func (a *Agent) connectOutput(ctx context.Context, output *models.RunningOutput) error {
	log.Printf("D! [agent] Attempting connection to [%s]", output.LogName())
	if err := output.Connect(); err != nil {
		log.Printf("E! [agent] Failed to connect to [%s], retrying in 15s, error was %q", output.LogName(), err)

		if err := internal.SleepContext(ctx, 15*time.Second); err != nil {
			return err
		}

		if err = output.Connect(); err != nil {
			return fmt.Errorf("error connecting to output %q: %w", output.LogName(), err)
		}
	}
	log.Printf("D! [agent] Successfully connected to %s", output.LogName())
	return nil
}

// runOutputs begins processing metrics and returns until the source channel is
// closed and all metrics have been written.  On shutdown metrics will be
// written one last time and dropped if unsuccessful.
func (a *Agent) runOutputs(
	unit *outputUnit,
) {
	var wg sync.WaitGroup

	// Start flush loop
	interval := time.Duration(a.Config.Agent.FlushInterval)
	jitter := time.Duration(a.Config.Agent.FlushJitter)

	ctx, cancel := context.WithCancel(context.Background())

	for _, output := range unit.outputs {
		interval := interval
		// Overwrite agent flush_interval if this plugin has its own.
		if output.Config.FlushInterval != 0 {
			interval = output.Config.FlushInterval
		}

		jitter := jitter
		// Overwrite agent flush_jitter if this plugin has its own.
		if output.Config.FlushJitter != 0 {
			jitter = output.Config.FlushJitter
		}

		wg.Add(1)
		go func(output *models.RunningOutput) {
			defer wg.Done()

			ticker := NewRollingTicker(interval, jitter)
			defer ticker.Stop()

			a.flushLoop(ctx, output, ticker)
		}(output)
	}

	for metric := range unit.src {
		for i, output := range unit.outputs {
			if i == len(unit.outputs)-1 {
				output.AddMetricNoCopy(metric)
			} else {
				output.AddMetric(metric)
			}
		}
	}

	log.Println("I! [agent] Hang on, flushing any cached metrics before shutdown")
	cancel()
	wg.Wait()

	log.Println("I! [agent] Stopping running outputs")
	stopRunningOutputs(unit.outputs)
}

// flushLoop runs an output's flush function periodically until the context is
// done.
func (a *Agent) flushLoop(
	ctx context.Context,
	output *models.RunningOutput,
	ticker Ticker,
) {
	logError := func(err error) {
		if err != nil {
			log.Printf("E! [agent] Error writing to %s: %v", output.LogName(), err)
		}
	}

	// watch for flush requests
	flushRequested := make(chan os.Signal, 1)
	watchForFlushSignal(flushRequested)
	defer stopListeningForFlushSignal(flushRequested)

	for {
		// Favor shutdown over other methods.
		select {
		case <-ctx.Done():
			logError(a.flushOnce(output, ticker, output.Write))
			return
		default:
		}

		select {
		case <-ctx.Done():
			logError(a.flushOnce(output, ticker, output.Write))
			return
		case <-ticker.Elapsed():
			logError(a.flushOnce(output, ticker, output.Write))
		case <-flushRequested:
			logError(a.flushOnce(output, ticker, output.Write))
		case <-output.BatchReady:
			logError(a.flushBatch(output, output.WriteBatch))
		}
	}
}

// flushOnce runs the output's Write function once, logging a warning each
// interval it fails to complete before the flush interval elapses.
func (a *Agent) flushOnce(
	output *models.RunningOutput,
	ticker Ticker,
	writeFunc func() error,
) error {
	done := make(chan error)
	go func() {
		done <- writeFunc()
	}()

	for {
		select {
		case err := <-done:
			output.LogBufferStatus()
			return err
		case <-ticker.Elapsed():
			log.Printf("W! [agent] [%q] did not complete within its flush interval",
				output.LogName())
			output.LogBufferStatus()
		}
	}
}

// flushBatch runs the output's Write function once Unlike flushOnce the
// interval elapsing is not considered during these flushes.
func (a *Agent) flushBatch(
	output *models.RunningOutput,
	writeFunc func() error,
) error {
	err := writeFunc()
	output.LogBufferStatus()
	return err
}

// Test runs the inputs, processors and aggregators for a single gather and
// writes the metrics to stdout.
func (a *Agent) Test(ctx context.Context, wait time.Duration) error {
	src := make(chan telegraf.Metric, 100)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := &influx.Serializer{SortFields: true, UintSupport: true}
		for metric := range src {
			octets, err := s.Serialize(metric)
			if err == nil {
				fmt.Print("> ", string(octets))
			}
			metric.Reject()
		}
	}()

	err := a.runTest(ctx, wait, src)
	if err != nil {
		return err
	}

	wg.Wait()

	if models.GlobalGatherErrors.Get() != 0 {
		return fmt.Errorf("input plugins recorded %d errors", models.GlobalGatherErrors.Get())
	}
	return nil
}

// runTest runs the agent and performs a single gather sending output to the
// outputC. After gathering pauses for the wait duration to allow service
// inputs to run.
func (a *Agent) runTest(ctx context.Context, wait time.Duration, outputC chan<- telegraf.Metric) error {
	// Set the default for processor skipping
	if a.Config.Agent.SkipProcessorsAfterAggregators == nil {
		msg := `The default value of 'skip_processors_after_aggregators' will change to 'true' with Telegraf v1.40.0! `
		msg += `If you need the current default behavior, please explicitly set the option to 'false'!`
		log.Print("W! [agent] ", color.YellowString(msg))
		skipProcessorsAfterAggregators := false
		a.Config.Agent.SkipProcessorsAfterAggregators = &skipProcessorsAfterAggregators
	}

	log.Printf("D! [agent] Initializing plugins")
	if err := a.InitPlugins(); err != nil {
		return err
	}

	startTime := time.Now()

	next := outputC

	var apu []*processorUnit
	var au *aggregatorUnit
	if len(a.Config.Aggregators) != 0 {
		procC := next
		if len(a.Config.AggProcessors) != 0 && !*a.Config.Agent.SkipProcessorsAfterAggregators {
			var err error
			procC, apu, err = a.startProcessors(next, a.Config.AggProcessors)
			if err != nil {
				return err
			}
		}

		next, au = a.startAggregators(procC, next, a.Config.Aggregators)
	}

	var pu []*processorUnit
	if len(a.Config.Processors) != 0 {
		var err error
		next, pu, err = a.startProcessors(next, a.Config.Processors)
		if err != nil {
			return err
		}
	}

	iu := a.testStartInputs(next, a.Config.Inputs)

	var wg sync.WaitGroup
	if au != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runProcessors(apu)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runAggregators(startTime, au)
		}()
	}

	if pu != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runProcessors(pu)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.testRunInputs(ctx, wait, iu)
	}()

	wg.Wait()

	log.Printf("D! [agent] Stopped Successfully")

	return nil
}

// Once runs the full agent for a single gather.
func (a *Agent) Once(ctx context.Context, wait time.Duration) error {
	err := a.runOnce(ctx, wait)
	if err != nil {
		return err
	}

	if models.GlobalGatherErrors.Get() != 0 {
		return fmt.Errorf("input plugins recorded %d errors", models.GlobalGatherErrors.Get())
	}

	unsent := 0
	for _, output := range a.Config.Outputs {
		unsent += output.BufferLength()
	}
	if unsent != 0 {
		return fmt.Errorf("output plugins unable to send %d metrics", unsent)
	}
	return nil
}

// runOnce runs the agent and performs a single gather sending output to the
// outputC. After gathering pauses for the wait duration to allow service
// inputs to run.
func (a *Agent) runOnce(ctx context.Context, wait time.Duration) error {
	// Set the default for processor skipping
	if a.Config.Agent.SkipProcessorsAfterAggregators == nil {
		msg := `The default value of 'skip_processors_after_aggregators' will change to 'true' with Telegraf v1.40.0! `
		msg += `If you need the current default behavior, please explicitly set the option to 'false'!`
		log.Print("W! [agent] ", color.YellowString(msg))
		skipProcessorsAfterAggregators := false
		a.Config.Agent.SkipProcessorsAfterAggregators = &skipProcessorsAfterAggregators
	}

	log.Printf("D! [agent] Initializing plugins")
	if err := a.InitPlugins(); err != nil {
		return err
	}

	startTime := time.Now()

	log.Printf("D! [agent] Connecting outputs")
	next, ou, err := a.startOutputs(ctx, a.Config.Outputs)
	if err != nil {
		return err
	}

	var apu []*processorUnit
	var au *aggregatorUnit
	if len(a.Config.Aggregators) != 0 {
		procC := next
		if len(a.Config.AggProcessors) != 0 && !*a.Config.Agent.SkipProcessorsAfterAggregators {
			procC, apu, err = a.startProcessors(next, a.Config.AggProcessors)
			if err != nil {
				return err
			}
		}

		next, au = a.startAggregators(procC, next, a.Config.Aggregators)
	}

	var pu []*processorUnit
	if len(a.Config.Processors) != 0 {
		next, pu, err = a.startProcessors(next, a.Config.Processors)
		if err != nil {
			return err
		}
	}

	iu := a.testStartInputs(next, a.Config.Inputs)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runOutputs(ou)
	}()

	if au != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runProcessors(apu)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runAggregators(startTime, au)
		}()
	}

	if pu != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runProcessors(pu)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.testRunInputs(ctx, wait, iu)
	}()

	wg.Wait()

	log.Printf("D! [agent] Stopped Successfully")

	return nil
}

// Returns the rounding precision for metrics.
func getPrecision(precision, interval time.Duration) time.Duration {
	if precision > 0 {
		return precision
	}

	switch {
	case interval >= time.Second:
		return time.Second
	case interval >= time.Millisecond:
		return time.Millisecond
	case interval >= time.Microsecond:
		return time.Microsecond
	default:
		return time.Nanosecond
	}
}

// panicRecover displays an error if an input panics.
func panicRecover(input *models.RunningInput) {
	//nolint:revive // recover is called inside a deferred function
	if err := recover(); err != nil {
		trace := make([]byte, 2048)
		runtime.Stack(trace, true)
		log.Printf("E! FATAL: [%s] panicked: %s, Stack:\n%s",
			input.LogName(), err, trace)
		log.Fatalln("E! PLEASE REPORT THIS PANIC ON GITHUB with " +
			"stack trace, configuration, and OS information: " +
			"https://github.com/influxdata/telegraf/issues/new/choose")
	}
}

func stopTickers(tickers []Ticker) {
	for _, ticker := range tickers {
		ticker.Stop()
	}
}
