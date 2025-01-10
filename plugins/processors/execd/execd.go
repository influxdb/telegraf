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
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/plugins/processors"
)

//go:embed sample.conf
var sampleConfig string

type Execd struct {
	Command      []string        `toml:"command"`
	Environment  []string        `toml:"environment"`
	RestartDelay config.Duration `toml:"restart_delay"`
	Log          telegraf.Logger

	parser     telegraf.Parser
	serializer telegraf.Serializer
	acc        telegraf.Accumulator
	process    *process.Process
}

func New() *Execd {
	return &Execd{
		RestartDelay: config.Duration(10 * time.Second),
	}
}

func (e *Execd) SetParser(p telegraf.Parser) {
	e.parser = p
}

func (e *Execd) SetSerializer(s telegraf.Serializer) {
	e.serializer = s
}

func (*Execd) SampleConfig() string {
	return sampleConfig
}

func (e *Execd) Start(acc telegraf.Accumulator) error {
	e.acc = acc

	var err error
	e.process, err = process.New(e.Command, e.Environment)
	if err != nil {
		return fmt.Errorf("error creating new process: %w", err)
	}
	e.process.Log = e.Log
	e.process.RestartDelay = time.Duration(e.RestartDelay)
	e.process.ReadStdoutFn = e.cmdReadOut
	e.process.ReadStderrFn = e.cmdReadErr

	if err = e.process.Start(); err != nil {
		// if there was only one argument, and it contained spaces, warn the user
		// that they may have configured it wrong.
		if len(e.Command) == 1 && strings.Contains(e.Command[0], " ") {
			e.Log.Warn("The processors.execd Command contained spaces but no arguments. " +
				"This setting expects the program and arguments as an array of strings, " +
				"not as a space-delimited string. See the plugin readme for an example.")
		}
		return fmt.Errorf("failed to start process %s: %w", e.Command, err)
	}

	return nil
}

func (e *Execd) Add(m telegraf.Metric, _ telegraf.Accumulator) error {
	b, err := e.serializer.Serialize(m)
	if err != nil {
		return fmt.Errorf("metric serializing error: %w", err)
	}

	_, err = e.process.Stdin.Write(b)
	if err != nil {
		return fmt.Errorf("error writing to process stdin: %w", err)
	}

	// We cannot maintain tracking metrics at the moment because input/output
	// is done asynchronously and we don't have any metric metadata to tie the
	// output metric back to the original input metric.
	m.Accept()
	return nil
}

func (e *Execd) Stop() {
	e.process.Stop()
}

func (e *Execd) cmdReadOut(out io.Reader) {
	// Prefer using the StreamParser when parsing influx format.
	if _, isInfluxParser := e.parser.(*influx.Parser); isInfluxParser {
		e.cmdReadOutStream(out)
		return
	}

	scanner := bufio.NewScanner(out)
	scanBuf := make([]byte, 4096)
	scanner.Buffer(scanBuf, 262144)

	for scanner.Scan() {
		metrics, err := e.parser.Parse(scanner.Bytes())
		if err != nil {
			e.Log.Errorf("Parse error: %s", err)
		}

		for _, metric := range metrics {
			e.acc.AddMetric(metric)
		}
	}

	if err := scanner.Err(); err != nil {
		e.Log.Errorf("Error reading stdout: %s", err)
	}
}

func (e *Execd) cmdReadOutStream(out io.Reader) {
	parser := influx.NewStreamParser(out)

	for {
		metric, err := parser.Next()

		if err != nil {
			// Stop parsing when we've reached the end.
			if errors.Is(err, influx.EOF) {
				break
			}

			var parseErr *influx.ParseError
			if errors.As(err, &parseErr) {
				// Continue past parse errors.
				e.acc.AddError(parseErr)
				continue
			}

			// Stop reading on any non-recoverable error.
			e.acc.AddError(err)
			return
		}

		e.acc.AddMetric(metric)
	}
}

func (e *Execd) cmdReadErr(out io.Reader) {
	scanner := bufio.NewScanner(out)

	for scanner.Scan() {
		e.Log.Errorf("stderr: %q", scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		e.Log.Errorf("Error reading stderr: %s", err)
	}
}

func (e *Execd) Init() error {
	if len(e.Command) == 0 {
		return errors.New("no command specified")
	}
	return nil
}

func init() {
	processors.AddStreaming("execd", func() telegraf.StreamingProcessor {
		return New()
	})
}
