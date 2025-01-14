//go:generate ../../../tools/readme_config_includer/generator
package nvidia_smi

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/nvidia_smi/schema_v11"
	"github.com/influxdata/telegraf/plugins/inputs/nvidia_smi/schema_v12"
)

//go:embed sample.conf
var sampleConfig string

// NvidiaSMI holds the methods for this plugin
type NvidiaSMI struct {
	BinPath string          `toml:"bin_path"`
	Timeout config.Duration `toml:"timeout"`
	Log     telegraf.Logger `toml:"-"`

	nvidiaSMIArgs []string
	ignorePlugin  bool
	once          sync.Once
}

func (*NvidiaSMI) SampleConfig() string {
	return sampleConfig
}

func (smi *NvidiaSMI) Start(telegraf.Accumulator) error {
	if _, err := os.Stat(smi.BinPath); os.IsNotExist(err) {
		binPath, err := exec.LookPath("nvidia-smi")
		if err != nil {
			return &internal.StartupError{Err: err}
		}
		smi.BinPath = binPath
	}

	return nil
}

func (*NvidiaSMI) Stop() {}

func (smi *NvidiaSMI) Probe() error {
	// Construct and execute metrics query
	_, err := internal.CombinedOutputTimeout(exec.Command(smi.BinPath, smi.nvidiaSMIArgs...), time.Duration(smi.Timeout))
	if err != nil {
		return fmt.Errorf("calling %q failed: %w", smi.BinPath, err)
	}
	return nil
}

// Gather implements the telegraf interface
func (smi *NvidiaSMI) Gather(acc telegraf.Accumulator) error {
	if smi.ignorePlugin {
		return nil
	}

	// Construct and execute metrics query
	data, err := internal.CombinedOutputTimeout(exec.Command(smi.BinPath, smi.nvidiaSMIArgs...), time.Duration(smi.Timeout))
	if err != nil {
		return fmt.Errorf("calling %q failed: %w", smi.BinPath, err)
	}

	// Parse the output
	return smi.parse(acc, data)
}

func (smi *NvidiaSMI) parse(acc telegraf.Accumulator, data []byte) error {
	schema := "v11"

	buf := bytes.NewBuffer(data)
	decoder := xml.NewDecoder(buf)
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("reading token failed: %w", err)
		}
		d, ok := token.(xml.Directive)
		if !ok {
			continue
		}
		directive := string(d)
		if !strings.HasPrefix(directive, "DOCTYPE") {
			continue
		}
		parts := strings.Split(directive, " ")
		s := strings.Trim(parts[len(parts)-1], "\" ")
		if strings.HasPrefix(s, "nvsmi_device_") && strings.HasSuffix(s, ".dtd") {
			schema = strings.TrimSuffix(strings.TrimPrefix(s, "nvsmi_device_"), ".dtd")
		} else {
			smi.Log.Debugf("Cannot find schema version in %q", directive)
		}
		break
	}
	smi.Log.Debugf("Using schema version in %s", schema)

	switch schema {
	case "v10", "v11":
		return schema_v11.Parse(acc, data)
	case "v12":
		return schema_v12.Parse(acc, data)
	}

	smi.once.Do(func() {
		smi.Log.Warnf(`Unknown schema version %q, using latest know schema for parsing.
		Please report this as an issue to https://github.com/influxdata/telegraf together
		with a sample output of 'nvidia_smi -q -x'!`, schema)
	})
	return schema_v12.Parse(acc, data)
}

func init() {
	inputs.Add("nvidia_smi", func() telegraf.Input {
		return &NvidiaSMI{
			BinPath:       "/usr/bin/nvidia-smi",
			Timeout:       config.Duration(5 * time.Second),
			nvidiaSMIArgs: []string{"-q", "-x"},
		}
	})
}
