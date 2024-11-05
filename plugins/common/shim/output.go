package shim

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
)

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

	go func() {
		var batch []telegraf.Metric
		timer := time.NewTimer(s.BatchTimeout)
		defer timer.Stop()

		for {
			select {
			case m := <-mCh:
				batch = append(batch, m)
				if len(batch) >= s.BatchSize {
					if err = s.Output.Write(batch); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to write metrics: %s\n", err)
					}
					batch = batch[:0]
					timer.Reset(s.BatchTimeout)
				}
			case <-timer.C:
				if len(batch) > 0 {
					if err = s.Output.Write(batch); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to write metrics: %s\n", err)
					}
					batch = batch[:0]
				}
				timer.Reset(s.BatchTimeout)
			case <-done:
				if len(batch) > 0 {
					if err = s.Output.Write(batch); err != nil {
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

		mCh <- m
	}

	close(done)

	return nil
}
