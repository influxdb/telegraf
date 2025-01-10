//go:generate ../../../tools/readme_config_includer/generator
package nsd

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"net"
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
	defaultBinary  = "/usr/sbin/nsd-control"
	defaultTimeout = config.Duration(time.Second)
)

type NSD struct {
	Binary     string          `toml:"binary"`
	Timeout    config.Duration `toml:"timeout"`
	UseSudo    bool            `toml:"use_sudo"`
	Server     string          `toml:"server"`
	ConfigFile string          `toml:"config_file"`

	run runner
}

type runner func(cmdName string, timeout config.Duration, useSudo bool, Server string, ConfigFile string) (*bytes.Buffer, error)

func (*NSD) SampleConfig() string {
	return sampleConfig
}

func (s *NSD) Gather(acc telegraf.Accumulator) error {
	out, err := s.run(s.Binary, s.Timeout, s.UseSudo, s.Server, s.ConfigFile)
	if err != nil {
		return fmt.Errorf("error gathering metrics: %w", err)
	}

	// Process values
	fields := make(map[string]interface{})
	fieldsServers := make(map[string]map[string]interface{})

	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		cols := strings.Split(scanner.Text(), "=")

		// Check split correctness
		if len(cols) != 2 {
			continue
		}

		stat := cols[0]
		value := cols[1]

		fieldValue, err := strconv.ParseFloat(value, 64)
		if err != nil {
			acc.AddError(fmt.Errorf("expected a numerical value for %s = %v",
				stat, value))
			continue
		}

		if strings.HasPrefix(stat, "server") {
			statTokens := strings.Split(stat, ".")
			if len(statTokens) > 1 {
				serverID := strings.TrimPrefix(statTokens[0], "server")
				if _, err := strconv.Atoi(serverID); err == nil {
					serverTokens := statTokens[1:]
					field := strings.Join(serverTokens[:], "_")
					if fieldsServers[serverID] == nil {
						fieldsServers[serverID] = make(map[string]interface{})
					}
					fieldsServers[serverID][field] = fieldValue
				}
			}
		} else {
			field := strings.ReplaceAll(stat, ".", "_")
			fields[field] = fieldValue
		}
	}

	acc.AddFields("nsd", fields, nil)
	for thisServerID, thisServerFields := range fieldsServers {
		thisServerTag := map[string]string{"server": thisServerID}
		acc.AddFields("nsd_servers", thisServerFields, thisServerTag)
	}

	return nil
}

// Shell out to nsd_stat and return the output
func nsdRunner(cmdName string, timeout config.Duration, useSudo bool, server, configFile string) (*bytes.Buffer, error) {
	cmdArgs := []string{"stats_noreset"}

	if server != "" {
		host, port, err := net.SplitHostPort(server)
		if err == nil {
			server = host + "@" + port
		}

		cmdArgs = append([]string{"-s", server}, cmdArgs...)
	}

	if configFile != "" {
		cmdArgs = append([]string{"-c", configFile}, cmdArgs...)
	}

	cmd := exec.Command(cmdName, cmdArgs...)

	if useSudo {
		cmdArgs = append([]string{cmdName}, cmdArgs...)
		cmd = exec.Command("sudo", cmdArgs...)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	err := internal.RunTimeout(cmd, time.Duration(timeout))
	if err != nil {
		return &out, fmt.Errorf("error running nsd-control: %w (%s %v)", err, cmdName, cmdArgs)
	}

	return &out, nil
}

func init() {
	inputs.Add("nsd", func() telegraf.Input {
		return &NSD{
			run:        nsdRunner,
			Binary:     defaultBinary,
			Timeout:    defaultTimeout,
			UseSudo:    false,
			Server:     "",
			ConfigFile: "",
		}
	})
}
