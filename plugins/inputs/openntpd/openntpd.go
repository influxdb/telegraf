//go:generate ../../../tools/readme_config_includer/generator
package openntpd

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
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	defaultBinary  = "/usr/sbin/ntpctl"
	defaultTimeout = config.Duration(5 * time.Second)

	// Mapping of the ntpctl tag key to the index in the command output
	tagI = map[string]int{
		"stratum": 2,
	}
	// Mapping of float metrics to their index in the command output
	floatI = map[string]int{
		"offset": 5,
		"delay":  6,
		"jitter": 7,
	}
	// Mapping of int metrics to their index in the command output
	intI = map[string]int{
		"wt":   0,
		"tl":   1,
		"next": 3,
		"poll": 4,
	}
)

type Openntpd struct {
	Binary  string          `toml:"binary"`
	Timeout config.Duration `toml:"timeout"`
	UseSudo bool            `toml:"use_sudo"`

	run runner
}

type runner func(cmdName string, timeout config.Duration, useSudo bool) (*bytes.Buffer, error)

func (*Openntpd) SampleConfig() string {
	return sampleConfig
}

func (n *Openntpd) Gather(acc telegraf.Accumulator) error {
	out, err := n.run(n.Binary, n.Timeout, n.UseSudo)
	if err != nil {
		return fmt.Errorf("error gathering metrics: %w", err)
	}

	lineCounter := 0
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		// skip first (peer) and second (field list) line
		if lineCounter < 2 {
			lineCounter++
			continue
		}

		line := scanner.Text()

		fields := strings.Fields(line)

		mFields := make(map[string]interface{})
		tags := make(map[string]string)

		// Even line ---> ntp server info
		if lineCounter%2 == 0 {
			// DNS resolution error ---> keep DNS name as remote name
			if fields[0] != "not" {
				tags["remote"] = fields[0]
			} else {
				tags["remote"] = fields[len(fields)-1]
			}
		}

		// Read next line - Odd line ---> ntp server stats
		scanner.Scan()
		line = scanner.Text()
		lineCounter++

		fields = strings.Fields(line)

		// if there is an ntpctl state prefix, remove it and make it it's own tag
		if strings.ContainsAny(fields[0], "*") {
			tags["state_prefix"] = fields[0]
			fields = fields[1:]
		}

		// Get tags from output
		for key, index := range tagI {
			if index >= len(fields) {
				continue
			}
			tags[key] = fields[index]
		}

		// Get integer metrics from output
		for key, index := range intI {
			if index >= len(fields) {
				continue
			}
			if fields[index] == "-" {
				continue
			}

			if key == "next" || key == "poll" {
				m, err := strconv.ParseInt(strings.TrimSuffix(fields[index], "s"), 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("integer value expected, got: %s", fields[index]))
					continue
				}
				mFields[key] = m
			} else {
				m, err := strconv.ParseInt(fields[index], 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("integer value expected, got: %s", fields[index]))
					continue
				}
				mFields[key] = m
			}
		}

		// get float metrics from output
		for key, index := range floatI {
			if len(fields) <= index {
				continue
			}
			if fields[index] == "-" || fields[index] == "----" || fields[index] == "peer" || fields[index] == "not" || fields[index] == "valid" {
				continue
			}

			if key == "offset" || key == "delay" || key == "jitter" {
				m, err := strconv.ParseFloat(strings.TrimSuffix(fields[index], "ms"), 64)
				if err != nil {
					acc.AddError(fmt.Errorf("float value expected, got: %s", fields[index]))
					continue
				}
				mFields[key] = m
			} else {
				m, err := strconv.ParseFloat(fields[index], 64)
				if err != nil {
					acc.AddError(fmt.Errorf("float value expected, got: %s", fields[index]))
					continue
				}
				mFields[key] = m
			}
		}
		acc.AddFields("openntpd", mFields, tags)

		lineCounter++
	}
	return nil
}

// Shell out to ntpctl and return the output
func openntpdRunner(cmdName string, timeout config.Duration, useSudo bool) (*bytes.Buffer, error) {
	cmdArgs := []string{"-s", "peers"}

	cmd := exec.Command(cmdName, cmdArgs...)

	if useSudo {
		cmdArgs = append([]string{cmdName}, cmdArgs...)
		cmd = exec.Command("sudo", cmdArgs...)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	err := internal.RunTimeout(cmd, time.Duration(timeout))
	if err != nil {
		return &out, fmt.Errorf("error running ntpctl: %w", err)
	}

	return &out, nil
}

func init() {
	inputs.Add("openntpd", func() telegraf.Input {
		return &Openntpd{
			run:     openntpdRunner,
			Binary:  defaultBinary,
			Timeout: defaultTimeout,
			UseSudo: false,
		}
	})
}
