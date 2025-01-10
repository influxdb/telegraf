//go:generate ../../../tools/readme_config_includer/generator
package execd

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/process"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

type Execd struct {
	Command                  []string        `toml:"command"`
	Environment              []string        `toml:"environment"`
	RestartDelay             config.Duration `toml:"restart_delay"`
	IgnoreSerializationError bool            `toml:"ignore_serialization_error"`
	UseBatchFormat           bool            `toml:"use_batch_format"`
	Log                      telegraf.Logger

	process    *process.Process
	serializer telegraf.Serializer
}

func (*Execd) SampleConfig() string {
	return sampleConfig
}

func (e *Execd) SetSerializer(s telegraf.Serializer) {
	e.serializer = s
}

func (e *Execd) Init() error {
	if len(e.Command) == 0 {
		return errors.New("no command specified")
	}

	var err error

	e.process, err = process.New(e.Command, e.Environment)
	if err != nil {
		return fmt.Errorf("error creating process %s: %w", e.Command, err)
	}
	e.process.Log = e.Log
	e.process.RestartDelay = time.Duration(e.RestartDelay)
	e.process.ReadStdoutFn = e.cmdReadOut
	e.process.ReadStderrFn = e.cmdReadErr

	return nil
}

func (e *Execd) Connect() error {
	if err := e.process.Start(); err != nil {
		// if there was only one argument, and it contained spaces, warn the user
		// that they may have configured it wrong.
		if len(e.Command) == 1 && strings.Contains(e.Command[0], " ") {
			e.Log.Warn("The outputs.execd Command contained spaces but no arguments. " +
				"This setting expects the program and arguments as an array of strings, " +
				"not as a space-delimited string. See the plugin readme for an example.")
		}
		return fmt.Errorf("failed to start process %s: %w", e.Command, err)
	}

	return nil
}

func (e *Execd) Close() error {
	e.process.Stop()
	return nil
}

func (e *Execd) Write(metrics []telegraf.Metric) error {
	if e.UseBatchFormat {
		b, err := e.serializer.SerializeBatch(metrics)
		if err != nil {
			return fmt.Errorf("error serializing metrics: %w", err)
		}

		if _, err = e.process.Stdin.Write(b); err != nil {
			return fmt.Errorf("error writing metrics: %w", err)
		}
		return nil
	}
	for _, m := range metrics {
		b, err := e.serializer.Serialize(m)
		if err != nil {
			if !e.IgnoreSerializationError {
				return fmt.Errorf("error serializing metrics: %w", err)
			}
			e.Log.Errorf("Skipping metric due to a serialization error: %v", err)
			continue
		}

		if _, err = e.process.Stdin.Write(b); err != nil {
			return fmt.Errorf("error writing metrics: %w", err)
		}
	}
	return nil
}

func (e *Execd) cmdReadErr(out io.Reader) {
	scanner := bufio.NewScanner(out)

	for scanner.Scan() {
		e.Log.Errorf("stderr: %s", scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		e.Log.Errorf("Error reading stderr: %s", err)
	}
}

func (e *Execd) cmdReadOut(out io.Reader) {
	scanner := bufio.NewScanner(out)

	for scanner.Scan() {
		e.Log.Info(scanner.Text())
	}
}

func init() {
	outputs.Add("execd", func() telegraf.Output {
		return &Execd{}
	})
}
