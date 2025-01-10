//go:generate ../../../tools/readme_config_includer/generator
package exec

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

const maxStderrBytes = 512

// Exec defines the exec output plugin.
type Exec struct {
	Command        []string        `toml:"command"`
	Environment    []string        `toml:"environment"`
	Timeout        config.Duration `toml:"timeout"`
	UseBatchFormat bool            `toml:"use_batch_format"`
	Log            telegraf.Logger `toml:"-"`

	runner     Runner
	serializer telegraf.Serializer
}

func (*Exec) SampleConfig() string {
	return sampleConfig
}

func (e *Exec) Init() error {
	e.runner = &CommandRunner{log: e.Log}

	return nil
}

// SetSerializer sets the serializer for the output.
func (e *Exec) SetSerializer(serializer telegraf.Serializer) {
	e.serializer = serializer
}

// Connect satisfies the Output interface.
func (e *Exec) Connect() error {
	return nil
}

// Close satisfies the Output interface.
func (e *Exec) Close() error {
	return nil
}

// Write writes the metrics to the configured command.
func (e *Exec) Write(metrics []telegraf.Metric) error {
	var buffer bytes.Buffer
	if e.UseBatchFormat {
		serializedMetrics, err := e.serializer.SerializeBatch(metrics)
		if err != nil {
			return err
		}
		buffer.Write(serializedMetrics)

		if buffer.Len() <= 0 {
			return nil
		}

		return e.runner.Run(time.Duration(e.Timeout), e.Command, e.Environment, &buffer)
	}
	errs := make([]error, 0, len(metrics))
	for _, metric := range metrics {
		serializedMetric, err := e.serializer.Serialize(metric)
		if err != nil {
			return err
		}
		buffer.Reset()
		buffer.Write(serializedMetric)

		err = e.runner.Run(time.Duration(e.Timeout), e.Command, e.Environment, &buffer)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Runner provides an interface for running exec.Cmd.
type Runner interface {
	Run(time.Duration, []string, []string, io.Reader) error
}

// CommandRunner runs a command with the ability to kill the process before the timeout.
type CommandRunner struct {
	cmd *exec.Cmd
	log telegraf.Logger
}

// Run runs the command.
func (c *CommandRunner) Run(timeout time.Duration, command, environments []string, buffer io.Reader) error {
	cmd := exec.Command(command[0], command[1:]...)
	if len(environments) > 0 {
		cmd.Env = append(os.Environ(), environments...)
	}
	cmd.Stdin = buffer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := internal.RunTimeout(cmd, timeout)
	s := stderr

	if err != nil {
		if errors.Is(err, internal.ErrTimeout) {
			return fmt.Errorf("%q timed out and was killed", command)
		}

		s = removeWindowsCarriageReturns(s)
		if s.Len() > 0 {
			if c.log.Level() < telegraf.Debug {
				c.log.Errorf("Command error: %q", c.truncate(s))
			} else {
				c.log.Debugf("Command error: %q", s)
			}
		}

		if status, ok := internal.ExitStatus(err); ok {
			return fmt.Errorf("%q exited %d with %w", command, status, err)
		}

		return fmt.Errorf("%q failed with %w", command, err)
	}

	c.cmd = cmd

	return nil
}

func (c *CommandRunner) truncate(buf bytes.Buffer) string {
	// Limit the number of bytes.
	didTruncate := false
	if buf.Len() > maxStderrBytes {
		buf.Truncate(maxStderrBytes)
		didTruncate = true
	}
	if i := bytes.IndexByte(buf.Bytes(), '\n'); i > 0 {
		// Only show truncation if the newline wasn't the last character.
		if i < buf.Len()-1 {
			didTruncate = true
		}
		buf.Truncate(i)
	}
	if didTruncate {
		buf.WriteString("...")
	}
	return buf.String()
}

func init() {
	outputs.Add("exec", func() telegraf.Output {
		return &Exec{
			Timeout:        config.Duration(time.Second * 5),
			UseBatchFormat: true,
		}
	})
}

// removeWindowsCarriageReturns removes all carriage returns from the input if the
// OS is Windows. It does not return any errors.
func removeWindowsCarriageReturns(b bytes.Buffer) bytes.Buffer {
	if runtime.GOOS == "windows" {
		var buf bytes.Buffer
		for {
			byt, err := b.ReadBytes(0x0D)
			byt = bytes.TrimRight(byt, "\x0d")
			if len(byt) > 0 {
				buf.Write(byt)
			}
			if errors.Is(err, io.EOF) {
				return buf
			}
		}
	}
	return b
}
