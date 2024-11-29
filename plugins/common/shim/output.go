package shim

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
)

type batchMetrics struct {
	metrics []telegraf.Metric
	wg      *sync.WaitGroup
	mu      *sync.RWMutex
}

func (bm *batchMetrics) add(metric telegraf.Metric) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.metrics = append(bm.metrics, metric)
}

func (bm *batchMetrics) clear() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.wg.Add(-len(bm.metrics))
	bm.metrics = bm.metrics[:0]
}

func (bm *batchMetrics) len() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return len(bm.metrics)
}

// AddOutput adds the input to the shim. Later calls to Run() will run this.
func (s *Shim) AddOutput(output telegraf.Output) error {
	models.SetLoggerOnPlugin(output, s.Log())
	if p, ok := output.(telegraf.Initializer); ok {
		err := p.Init()
		if err != nil {
			return fmt.Errorf("failed to init input: %w", err)
		}
	}

	s.Output = output
	return nil
}

func (s *Shim) RunOutput() error {
	parser := influx.Parser{}
	err := parser.Init()
	if err != nil {
		return fmt.Errorf("failed to create new parser: %w", err)
	}

	err = s.Output.Connect()
	if err != nil {
		return fmt.Errorf("failed to start processor: %w", err)
	}
	defer s.Output.Close()

	mCh := make(chan telegraf.Metric)
	done := make(chan struct{})
	batch := batchMetrics{wg: &sync.WaitGroup{}, mu: &sync.RWMutex{}}

	go func() {
		timer := time.NewTimer(s.BatchTimeout)
		defer timer.Stop()

		for {
			select {
			case m := <-mCh:
				batch.add(m)
				if batch.len() >= s.BatchSize {
					if err = s.Output.Write(batch.metrics); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to write metrics: %s\n", err)
					}
					batch.clear()
					timer.Reset(s.BatchTimeout)
				}
			case <-timer.C:
				if batch.len() > 0 {
					if err = s.Output.Write(batch.metrics); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to write metrics: %s\n", err)
					}
					batch.clear()
				}
				timer.Reset(s.BatchTimeout)
			case <-done:
				if batch.len() > 0 {
					if err = s.Output.Write(batch.metrics); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to write remaining metrics: %s\n", err)
					}
				}
				return
			}
		}
	}()

	scanner := bufio.NewScanner(s.stdin)
	for scanner.Scan() {
		m, err := parser.ParseLine(scanner.Text())
		if err != nil {
			fmt.Fprintf(s.stderr, "Failed to parse metric: %s\n", err)
			continue
		}

		batch.wg.Add(1)
		mCh <- m
	}

	batch.wg.Wait()

	close(done)

	return nil
}
