//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

package linux_cpu

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	defaultHostSys = "/sys"
	cpufreq        = "cpufreq"
	thermal        = "thermal"
)

type LinuxCPU struct {
	PathSysfs string          `toml:"host_sys"`
	Metrics   []string        `toml:"metrics"`
	Log       telegraf.Logger `toml:"-"`
	cpus      []cpu
}

type cpu struct {
	id    string
	path  string
	props map[string]string
}

type prop struct {
	name     string
	path     string
	optional bool
}

func (*LinuxCPU) SampleConfig() string {
	return sampleConfig
}

func (g *LinuxCPU) Init() error {
	if g.PathSysfs == "" {
		g.PathSysfs = defaultHostSys
	}

	if len(g.Metrics) == 0 {
		// The user has not enabled any of the metrics
		return errors.New("no metrics selected")
	}

	cpus, err := g.discoverCpus()
	if err != nil {
		return err
	} else if len(cpus) == 0 {
		// Although the user has specified metrics to collect, `discoverCpus` failed to find the required metrics
		return errors.New("no CPUs detected to track")
	}
	g.cpus = cpus

	return nil
}

func (g *LinuxCPU) Gather(acc telegraf.Accumulator) error {
	for _, cpu := range g.cpus {
		fields := make(map[string]interface{})
		tags := map[string]string{"cpu": cpu.id}

		failed := false
		for name, propPath := range cpu.props {
			v, err := readUintFromFile(propPath)
			if err != nil {
				acc.AddError(err)
				failed = true
				break
			}

			fields[name] = v
		}

		if !failed {
			acc.AddFields("linux_cpu", fields, tags)
		}
	}

	return nil
}

func (g *LinuxCPU) discoverCpus() ([]cpu, error) {
	var cpus []cpu

	glob := path.Join(g.PathSysfs, "devices/system/cpu/cpu[0-9]*")
	cpuDirs, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}

	if len(cpuDirs) == 0 {
		return nil, fmt.Errorf("no CPUs detected at: %s", glob)
	}

	for _, dir := range cpuDirs {
		_, cpuName := filepath.Split(dir)
		cpuNum := strings.TrimPrefix(cpuName, "cpu")

		cpu := cpu{
			id:    cpuNum,
			path:  dir,
			props: make(map[string]string),
		}

		var props []prop

		if choice.Contains(cpufreq, g.Metrics) {
			props = append(props,
				prop{name: "scaling_cur_freq", path: "cpufreq/scaling_cur_freq", optional: false},
				prop{name: "scaling_min_freq", path: "cpufreq/scaling_min_freq", optional: false},
				prop{name: "scaling_max_freq", path: "cpufreq/scaling_max_freq", optional: false},
				prop{name: "cpuinfo_cur_freq", path: "cpufreq/cpuinfo_cur_freq", optional: true},
				prop{name: "cpuinfo_min_freq", path: "cpufreq/cpuinfo_min_freq", optional: true},
				prop{name: "cpuinfo_max_freq", path: "cpufreq/cpuinfo_max_freq", optional: true},
			)
		}

		if choice.Contains(thermal, g.Metrics) {
			props = append(
				props,
				prop{name: "throttle_count", path: "thermal_throttle/core_throttle_count", optional: false},
				prop{name: "throttle_max_time", path: "thermal_throttle/core_throttle_max_time_ms", optional: false},
				prop{name: "throttle_total_time", path: "thermal_throttle/core_throttle_total_time_ms", optional: false},
			)
		}

		var failed = false
		for _, prop := range props {
			propPath := filepath.Join(dir, prop.path)
			err := validatePath(propPath)
			if err != nil {
				if prop.optional {
					continue
				}

				g.Log.Warnf("Failed to load property %s: %v", propPath, err)
				failed = true
				break
			}

			cpu.props[prop.name] = propPath
		}

		if len(cpu.props) == 0 {
			g.Log.Warnf("No properties enabled/loaded for CPU %s", cpuNum)
			failed = true
		}

		if !failed {
			cpus = append(cpus, cpu)
		}
	}
	return cpus, nil
}

func validatePath(propPath string) error {
	f, err := os.Open(propPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file with CPU property does not exist: %q", propPath)
	}
	if err != nil {
		return fmt.Errorf("cannot get system information for CPU property %q: %w", propPath, err)
	}

	_ = f.Close() // File is not written to, closing should be safe
	return nil
}

func readUintFromFile(propPath string) (uint64, error) {
	f, err := os.Open(propPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buffer := make([]byte, 22)

	n, err := f.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("error on reading file: %w", err)
	} else if n == 0 {
		return 0, errors.New("error on reading file: file is empty")
	}

	return strconv.ParseUint(string(buffer[:n-1]), 10, 64)
}

func init() {
	inputs.Add("linux_cpu", func() telegraf.Input {
		return &LinuxCPU{
			Metrics: []string{"cpufreq"},
		}
	})
}
