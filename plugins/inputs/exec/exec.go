//go:generate ../../../tools/readme_config_includer/generator
package exec

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
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
	run(string, []string, time.Duration) ([]byte, []byte, error)
}

type commandRunner struct {
	debug bool
}

func (*Exec) SampleConfig() string {
	return sampleConfig
}

func (*Exec) Init() error {
	return nil
}

func (e *Exec) SetParser(parser telegraf.Parser) {
	e.parser = parser
	unwrapped, ok := parser.(*models.RunningParser)
	if ok {
		if _, ok := unwrapped.Parser.(*nagios.Parser); ok {
			e.exitCodeHandler = nagiosHandler
			e.parseDespiteError = true
		}
	}
}

func (e *Exec) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	// Legacy single command support
	if e.Command != "" {
		e.Commands = append(e.Commands, e.Command)
		e.Command = ""
	}

	commands := make([]string, 0, len(e.Commands))
	for _, pattern := range e.Commands {
		cmdAndArgs := strings.SplitN(pattern, " ", 2)
		if len(cmdAndArgs) == 0 {
			continue
		}

		matches, err := filepath.Glob(cmdAndArgs[0])
		if err != nil {
			acc.AddError(err)
			continue
		}

		if len(matches) == 0 {
			// There were no matches with the glob pattern, so let's assume
			// that the command is in PATH and just run it as it is
			commands = append(commands, pattern)
		} else {
			// There were matches, so we'll append each match together with
			// the arguments to the commands slice
			for _, match := range matches {
				if len(cmdAndArgs) == 1 {
					commands = append(commands, match)
				} else {
					commands = append(commands,
						strings.Join([]string{match, cmdAndArgs[1]}, " "))
				}
			}
		}
	}

	wg.Add(len(commands))
	for _, command := range commands {
		go e.processCommand(command, acc, &wg)
	}
	wg.Wait()
	return nil
}

func truncate(buf bytes.Buffer) bytes.Buffer {
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
	return buf
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

func (e *Exec) processCommand(command string, acc telegraf.Accumulator, wg *sync.WaitGroup) {
	defer wg.Done()

	out, errBuf, runErr := e.runner.run(command, e.Environment, time.Duration(e.Timeout))
	if !e.IgnoreError && !e.parseDespiteError && runErr != nil {
		err := fmt.Errorf("exec: %w for command %q: %s", runErr, command, string(errBuf))
		acc.AddError(err)
		return
	}

	metrics, err := e.parser.Parse(out)
	if err != nil {
		acc.AddError(err)
		return
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
}

func nagiosHandler(metrics []telegraf.Metric, err error, msg []byte) []telegraf.Metric {
	return nagios.AddState(err, msg, metrics)
}

func newExec() *Exec {
	return &Exec{
		runner:  commandRunner{},
		Timeout: config.Duration(time.Second * 5),
	}
}

func init() {
	inputs.Add("exec", func() telegraf.Input {
		return newExec()
	})
}
