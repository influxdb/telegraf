package multifile

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"path"
	"strconv"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

// `toml:"name_override"`

type MultiFile struct {
	BaseDir   string
	FailEarly bool
	Files     []File `toml:"file"`
	Tags      map[string]string

	initialized bool
}

type File struct {
	Name       string `toml:"file"`
	Dest       string
	Conversion string
}

const sampleConfig = `
  name_override = "sensor"
  base_dir = "/sys/bus/i2c/devices/1-0076/iio:device0"
  [inputs.multifile.tags]
    location = "server_room"
  [[inputs.multifile.file]]
    file = "name"
    dest = "type"
    conversion = "tag"
  [[inputs.multifile.file]]
    file = "in_pressure_input"
    dest = "pressure"
    conversion = "float"
  [[inputs.multifile.file]]
    file = "in_temp_input"
    dest = "temperature"
    conversion = "float(3)"
  [[inputs.multifile.file]]
    file = "in_humidityrelative_input"
    dest = "humidityrelative"
    conversion = "float(3)"
`

// SampleConfig returns the default configuration of the Input
func (m *MultiFile) SampleConfig() string {
	return sampleConfig
}

func (m *MultiFile) Description() string {
	return "Aggregates the contents of multiple files into a single point"
}

func (m *MultiFile) init() {
	if m.initialized {
		return
	}

	for i, file := range m.Files {
		if m.BaseDir != "" {
			m.Files[i].Name = path.Join(m.BaseDir, file.Name)
		}
		if file.Dest == "" {
			m.Files[i].Dest = path.Base(file.Name)
		}
	}

	m.initialized = true
}

func (m *MultiFile) Gather(acc telegraf.Accumulator) error {
	m.init()
	now := time.Now()
	fields := make(map[string]interface{})
	tags := make(map[string]string)

	for key, value := range m.Tags {
		tags[key] = value
	}

	for _, file := range m.Files {
		fileContents, err := ioutil.ReadFile(file.Name)

		if err != nil {
			if m.FailEarly {
				return err
			}
			continue
		}

		vStr := string(bytes.TrimSpace(bytes.Trim(fileContents, "\x00")))

		if file.Conversion == "tag" {
			tags[file.Dest] = vStr
			continue
		}

		var value interface{}

		var d int = 0
		if _, err := fmt.Sscanf(file.Conversion, "float(%d)", &d); err == nil || file.Conversion == "float" {
			var v float64
			v, err = strconv.ParseFloat(vStr, 64)
			value = v / math.Pow10(d)
		}

		if file.Conversion == "int" {
			value, err = strconv.ParseInt(vStr, 10, 64)
		}

		if file.Conversion == "bool" {
			value, err = strconv.ParseBool(vStr)
		}

		if file.Conversion == "string" || file.Conversion == "" {
			value = vStr
		}

		if file.Conversion == "bool" {
			value, err = strconv.ParseBool(vStr)
		}

		if err != nil {
			if m.FailEarly {
				return err
			}
			continue
		}

		if value == nil {
			return errors.New(fmt.Sprintf("invalid conversion %v", file.Conversion))
		}

		fields[file.Dest] = value
	}

	acc.AddGauge("multifile", fields, tags, now)
	return nil
}

func init() {
	inputs.Add("multifile", func() telegraf.Input {
		return &MultiFile{
			FailEarly: true,
			Tags:      make(map[string]string),
		}
	})
}
