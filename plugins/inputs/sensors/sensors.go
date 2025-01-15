//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

package sensors

import (
	_ "embed"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	execCommand    = exec.Command // execCommand is used to mock commands in tests.
	numberRegp     = regexp.MustCompile("[0-9]+")
	defaultTimeout = config.Duration(5 * time.Second)
)

const cmd = "sensors"

type Sensors struct {
	RemoveNumbers bool            `toml:"remove_numbers"`
	Timeout       config.Duration `toml:"timeout"`
	path          string
}

func (*Sensors) SampleConfig() string {
	return sampleConfig
}

func (s *Sensors) Init() error {
	// Set defaults
	if s.path == "" {
		path, err := exec.LookPath(cmd)
		if err != nil {
			return fmt.Errorf("looking up %q failed: %w", cmd, err)
		}
		s.path = path
	}

	// Check parameters
	if s.path == "" {
		return fmt.Errorf("no path specified for %q", cmd)
	}

	return nil
}

func (s *Sensors) Gather(acc telegraf.Accumulator) error {
	if len(s.path) == 0 {
		return errors.New("sensors not found: verify that lm-sensors package is installed and that sensors is in your PATH")
	}

	return s.parse(acc)
}

// parse forks the command:
//
//	sensors -u -A
//
// and parses the output to add it to the telegraf.Accumulator.
func (s *Sensors) parse(acc telegraf.Accumulator) error {
	tags := make(map[string]string)
	fields := make(map[string]interface{})
	chip := ""
	cmd := execCommand(s.path, "-A", "-u")
	out, err := internal.StdOutputTimeout(cmd, time.Duration(s.Timeout))
	if err != nil {
		return fmt.Errorf("failed to run command %q: %w - %s", strings.Join(cmd.Args, " "), err, string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			acc.AddFields("sensors", fields, tags)
			chip = ""
			tags = make(map[string]string)
			fields = make(map[string]interface{})
			continue
		}
		if len(chip) == 0 {
			chip = line
			tags["chip"] = chip
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			if len(tags) > 1 {
				acc.AddFields("sensors", fields, tags)
			}
			fields = make(map[string]interface{})
			tags = map[string]string{
				"chip":    chip,
				"feature": strings.TrimRight(snake(line), ":"),
			}
		} else {
			splitted := strings.Split(line, ":")
			fieldName := strings.TrimSpace(splitted[0])
			if s.RemoveNumbers {
				fieldName = numberRegp.ReplaceAllString(fieldName, "")
			}
			fieldValue, err := strconv.ParseFloat(strings.TrimSpace(splitted[1]), 64)
			if err != nil {
				return err
			}
			fields[fieldName] = fieldValue
		}
	}
	acc.AddFields("sensors", fields, tags)
	return nil
}

// snake converts string to snake case
func snake(input string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input), " ", "_"))
}

func init() {
	inputs.Add("sensors", func() telegraf.Input {
		return &Sensors{
			RemoveNumbers: true,
			Timeout:       defaultTimeout,
		}
	})
}
