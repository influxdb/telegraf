//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

package kernel

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/prometheus/procfs"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// /proc/stat file line prefixes to gather stats on:
var (
	interrupts      = []byte("intr")
	contextSwitches = []byte("ctxt")
	processesForked = []byte("processes")
	diskPages       = []byte("page")
	bootTime        = []byte("btime")
)

type Kernel struct {
	ConfigCollect []string `toml:"collect"`

	optCollect      map[string]bool
	statFile        string
	entropyStatFile string
	ksmStatsDir     string
	psiDir          string
	procfs          procfs.FS
}

func (*Kernel) SampleConfig() string {
	return sampleConfig
}

func (k *Kernel) Init() error {
	k.optCollect = make(map[string]bool, len(k.ConfigCollect))
	for _, v := range k.ConfigCollect {
		k.optCollect[v] = true
	}

	if k.optCollect["ksm"] {
		if _, err := os.Stat(k.ksmStatsDir); os.IsNotExist(err) {
			// ksm probably not enabled in the kernel, bail out early
			return fmt.Errorf("directory %q does not exist. KSM is not enabled in this kernel", k.ksmStatsDir)
		}
	}
	if k.optCollect["psi"] {
		procdir := filepath.Dir(k.psiDir)
		var err error
		if k.procfs, err = procfs.NewFS(procdir); err != nil {
			// psi probably not supported in the kernel, bail out early
			return fmt.Errorf("failed to initialize procfs on %s: %w", procdir, err)
		}
	}
	return nil
}

func (k *Kernel) Gather(acc telegraf.Accumulator) error {
	data, err := getProcValueBytes(k.statFile)
	if err != nil {
		return err
	}

	entropyValue, err := getProcValueInt(k.entropyStatFile)
	if err != nil {
		return err
	}

	fields := make(map[string]interface{})

	fields["entropy_avail"] = entropyValue

	dataFields := bytes.Fields(data)
	for i, field := range dataFields {
		switch {
		case bytes.Equal(field, interrupts):
			m, err := strconv.ParseInt(string(dataFields[i+1]), 10, 64)
			if err != nil {
				return err
			}
			fields["interrupts"] = m
		case bytes.Equal(field, contextSwitches):
			m, err := strconv.ParseInt(string(dataFields[i+1]), 10, 64)
			if err != nil {
				return err
			}
			fields["context_switches"] = m
		case bytes.Equal(field, processesForked):
			m, err := strconv.ParseInt(string(dataFields[i+1]), 10, 64)
			if err != nil {
				return err
			}
			fields["processes_forked"] = m
		case bytes.Equal(field, bootTime):
			m, err := strconv.ParseInt(string(dataFields[i+1]), 10, 64)
			if err != nil {
				return err
			}
			fields["boot_time"] = m
		case bytes.Equal(field, diskPages):
			in, err := strconv.ParseInt(string(dataFields[i+1]), 10, 64)
			if err != nil {
				return err
			}
			out, err := strconv.ParseInt(string(dataFields[i+2]), 10, 64)
			if err != nil {
				return err
			}
			fields["disk_pages_in"] = in
			fields["disk_pages_out"] = out
		}
	}

	if k.optCollect["ksm"] {
		stats := []string{
			"full_scans", "max_page_sharing",
			"merge_across_nodes", "pages_shared",
			"pages_sharing", "pages_to_scan",
			"pages_unshared", "pages_volatile",
			"run", "sleep_millisecs",
			"stable_node_chains", "stable_node_chains_prune_millisecs",
			"stable_node_dups", "use_zero_pages",
		}
		// these exist in very recent Linux versions only, but useful to include if there.
		extraStats := []string{"general_profit"}

		for _, f := range stats {
			m, err := getProcValueInt(filepath.Join(k.ksmStatsDir, f))
			if err != nil {
				return err
			}

			fields["ksm_"+f] = m
		}

		for _, f := range extraStats {
			m, err := getProcValueInt(filepath.Join(k.ksmStatsDir, f))
			if err != nil {
				// if an extraStats metric doesn't exist in our kernel version, ignore it.
				continue
			}

			fields["ksm_"+f] = m
		}
	}
	acc.AddCounter("kernel", fields, make(map[string]string))

	if k.optCollect["psi"] {
		if err := k.gatherPressure(acc); err != nil {
			return err
		}
	}

	return nil
}

func getProcValueBytes(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("path %q does not exist", path)
	} else if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read from %q: %w", path, err)
	}

	return data, nil
}

func getProcValueInt(path string) (int64, error) {
	data, err := getProcValueBytes(path)
	if err != nil {
		return -1, err
	}

	m, err := strconv.ParseInt(string(bytes.TrimSpace(data)), 10, 64)
	if err != nil {
		return -1, fmt.Errorf("failed to parse %q as an integer: %w", data, err)
	}

	return m, nil
}

func init() {
	inputs.Add("kernel", func() telegraf.Input {
		return &Kernel{
			statFile:        "/proc/stat",
			entropyStatFile: "/proc/sys/kernel/random/entropy_avail",
			ksmStatsDir:     "/sys/kernel/mm/ksm",
			psiDir:          "/proc/pressure",
		}
	})
}
