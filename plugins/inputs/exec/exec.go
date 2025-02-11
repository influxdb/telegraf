//go:generate ../../../tools/readme_config_includer/generator
package exec

import (
	"bytes"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/nagios"
)

//go:embed sample.conf
var sampleConfig string

var once sync.Once

const maxStderrBytes int = 512

type Exec struct {
	Commands    []string        `toml:"commands"`
	Command     string          `toml:"command"`
	Environment []string        `toml:"environment"`
	IgnoreError bool            `toml:"ignore_error"`
	Timeout     config.Duration `toml:"timeout"`
	Log         telegraf.Logger `toml:"-"`

	parser telegraf.Parser

	runner runner

	// Allow post-processing of command exit codes
	exitCodeHandler   exitCodeHandlerFunc
	parseDespiteError bool
}

type exitCodeHandlerFunc func([]telegraf.Metric, error, []byte) []telegraf.Metric

type runner interface {
	run(string) ([]byte, []byte, error)
}

type commandRunner struct {
	environment []string
	timeout     time.Duration
	debug       bool
}

func (*Exec) SampleConfig() string {
	return sampleConfig
}

func (e *Exec) Init() error {
	// Legacy single command support
	if e.Command != "" {
		e.Commands = append(e.Commands, e.Command)
	}

	e.runner = &commandRunner{
		environment: e.Environment,
		timeout:     time.Duration(e.Timeout),
		debug:       e.Log.Level().Includes(telegraf.Debug),
	}

	return nil
}

func (e *Exec) SetParser(parser telegraf.Parser) {
	e.parser = parser
	unwrapped, ok := parser.(*models.RunningParser)
	if ok {
		if _, ok := unwrapped.Parser.(*nagios.Parser); ok {
			e.exitCodeHandler = func(metrics []telegraf.Metric, err error, msg []byte) []telegraf.Metric {
				return nagios.AddState(err, msg, metrics)
			}
			e.parseDespiteError = true
		}
	}
}

func (e *Exec) Gather(acc telegraf.Accumulator) error {
	commands := e.updateRunners()

	var wg sync.WaitGroup
	for _, cmd := range commands {
		wg.Add(1)

		go func(c string) {
			defer wg.Done()
			acc.AddError(e.processCommand(acc, c))
		}(cmd)
	}
	wg.Wait()
	return nil
}

func (e *Exec) updateRunners() []string {
	commands := make([]string, 0, len(e.Commands))
	for _, pattern := range e.Commands {
		if pattern == "" {
			continue
		}

		// Try to expand globbing expressions
		cmd, args, found := strings.Cut(pattern, " ")
		matches, err := filepath.Glob(cmd)
		if err != nil {
			e.Log.Errorf("Matching command %q failed: %v", cmd, err)
			continue
		}

		if len(matches) == 0 {
			// There were no matches with the glob pattern, so let's assume
			// the command is in PATH and just run it as it is
			commands = append(commands, pattern)
		} else {
			// There were matches, so we'll append each match together with
			// the arguments to the commands slice
			for _, match := range matches {
				if found {
					match += " " + args
				}
				commands = append(commands, match)
			}
		}
	}

	return commands
}

func (e *Exec) processCommand(acc telegraf.Accumulator, cmd string) error {
	out, errBuf, runErr := e.runner.run(cmd)
	if !e.IgnoreError && !e.parseDespiteError && runErr != nil {
		return fmt.Errorf("exec: %w for command %q: %s", runErr, cmd, string(errBuf))
	}

	metrics, err := e.parser.Parse(out)
	if err != nil {
		return err
	}

	if len(metrics) == 0 {
		once.Do(func() {
			e.Log.Debug(internal.NoMetricsCreatedMsg)
		})
	}

	if e.exitCodeHandler != nil {
		metrics = e.exitCodeHandler(metrics, runErr, errBuf)
	}

	for _, m := range metrics {
		acc.AddMetric(m)
	}

	return nil
}

func truncate(buf *bytes.Buffer) {
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
}

func init() {
	inputs.Add("exec", func() telegraf.Input {
		return &Exec{
			Timeout: config.Duration(5 * time.Second),
		}
	})
}
