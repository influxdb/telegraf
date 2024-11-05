package shim

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/plugins/serializers/influx"
)

type empty struct{}

var (
	forever       = 100 * 365 * 24 * time.Hour
	envVarEscaper = strings.NewReplacer(
		`"`, `\"`,
		`\`, `\\`,
	)
)

const (
	// PollIntervalDisabled is used to indicate that you want to disable polling,
	// as opposed to duration 0 meaning poll constantly.
	PollIntervalDisabled = time.Duration(0)
)

// Shim allows you to wrap your inputs and run them as if they were part of Telegraf,
// except built externally.
type Shim struct {
	Input     telegraf.Input
	Processor telegraf.StreamingProcessor
	Output    telegraf.Output

	BatchSize    int
	BatchTimeout time.Duration

	log telegraf.Logger

	// streams
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// outgoing metric channel
	metricCh chan telegraf.Metric

	// input only
	gatherPromptCh chan empty
}

// New creates a new shim interface
func New() *Shim {
	return &Shim{
		BatchSize:    1,
		BatchTimeout: 5 * time.Second,
		metricCh:     make(chan telegraf.Metric, 1),
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		log:          logger.New("", "", ""),
	}
}

func (s *Shim) watchForShutdown(cancel context.CancelFunc) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit // user-triggered quit
		// cancel, but keep looping until the metric channel closes.
		cancel()
	}()
}

// Run the input plugins..
func (s *Shim) Run(pollInterval time.Duration) error {
	if s.Input != nil {
		err := s.RunInput(pollInterval)
		if err != nil {
			return fmt.Errorf("running input failed: %w", err)
		}
	} else if s.Processor != nil {
		err := s.RunProcessor()
		if err != nil {
			return fmt.Errorf("running processor failed: %w", err)
		}
	} else if s.Output != nil {
		err := s.RunOutput()
		if err != nil {
			return fmt.Errorf("running output failed: %w", err)
		}
	} else {
		return errors.New("nothing to run")
	}

	return nil
}

func hasQuit(ctx context.Context) bool {
	return ctx.Err() != nil
}

func (s *Shim) writeProcessedMetrics() error {
	serializer := &influx.Serializer{}
	if err := serializer.Init(); err != nil {
		return fmt.Errorf("creating serializer failed: %w", err)
	}
	for { //nolint:gosimple // for-select used on purpose
		select {
		case m, open := <-s.metricCh:
			if !open {
				return nil
			}
			b, err := serializer.Serialize(m)
			if err != nil {
				m.Reject()
				return fmt.Errorf("failed to serialize metric: %w", err)
			}
			// Write this to stdout
			_, err = fmt.Fprint(s.stdout, string(b))
			if err != nil {
				m.Drop()
				return fmt.Errorf("failed to write metric: %w", err)
			}
			m.Accept()
		}
	}
}

// LogName satisfies the MetricMaker interface
func (s *Shim) LogName() string {
	return ""
}

// MakeMetric satisfies the MetricMaker interface
func (s *Shim) MakeMetric(m telegraf.Metric) telegraf.Metric {
	return m // don't need to do anything to it.
}

// Log satisfies the MetricMaker interface
func (s *Shim) Log() telegraf.Logger {
	return s.log
}
