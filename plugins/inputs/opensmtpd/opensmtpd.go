//go:generate ../../../tools/readme_config_includer/generator
package opensmtpd

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	defaultBinary  = "/usr/sbin/smtpctl"
	defaultTimeout = config.Duration(time.Second)
)

type Opensmtpd struct {
	Binary  string          `toml:"binary"`
	Timeout config.Duration `toml:"timeout"`
	UseSudo bool            `toml:"use_sudo"`

	run runner
}

type runner func(cmdName string, timeout config.Duration, useSudo bool) (*bytes.Buffer, error)

func (*Opensmtpd) SampleConfig() string {
	return sampleConfig
}

func (s *Opensmtpd) Gather(acc telegraf.Accumulator) error {
	// All the dots in stat name will be replaced by underscores.
	// Histogram statistics will not be collected.

	// Always exclude uptime.human statistics
	statExcluded := []string{"uptime.human"}
	filterExcluded, err := filter.Compile(statExcluded)
	if err != nil {
		return err
	}

	out, err := s.run(s.Binary, s.Timeout, s.UseSudo)
	if err != nil {
		return fmt.Errorf("error gathering metrics: %w", err)
	}

	// Process values
	fields := make(map[string]interface{})
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		cols := strings.Split(scanner.Text(), "=")

		// Check split correctness
		if len(cols) != 2 {
			continue
		}

		stat := cols[0]
		value := cols[1]

		// Filter value
		if filterExcluded.Match(stat) {
			continue
		}

		field := strings.ReplaceAll(stat, ".", "_")

		fields[field], err = strconv.ParseFloat(value, 64)
		if err != nil {
			acc.AddError(fmt.Errorf("expected a numerical value for %s = %v", stat, value))
		}
	}

	acc.AddFields("opensmtpd", fields, nil)

	return nil
}

// Shell out to opensmtpd_stat and return the output
func opensmtpdRunner(cmdName string, timeout config.Duration, useSudo bool) (*bytes.Buffer, error) {
	cmdArgs := []string{"show", "stats"}

	cmd := exec.Command(cmdName, cmdArgs...)

	if useSudo {
		cmdArgs = append([]string{cmdName}, cmdArgs...)
		cmd = exec.Command("sudo", cmdArgs...)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	err := internal.RunTimeout(cmd, time.Duration(timeout))
	if err != nil {
		return &out, fmt.Errorf("error running smtpctl: %w", err)
	}

	return &out, nil
}

func init() {
	inputs.Add("opensmtpd", func() telegraf.Input {
		return &Opensmtpd{
			run:     opensmtpdRunner,
			Binary:  defaultBinary,
			Timeout: defaultTimeout,
			UseSudo: false,
		}
	})
}
