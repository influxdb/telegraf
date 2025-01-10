//go:build linux
// +build linux

package temp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
)

const scalingFactor = float64(1000.0)

type TemperatureStat struct {
	Name        string
	Label       string
	Device      string
	Temperature float64
	Additional  map[string]interface{}
}

func (t *Temperature) Init() error {
	switch t.MetricFormat {
	case "":
		t.MetricFormat = "v2"
	case "v1", "v2":
		// Do nothing as those are valid
	default:
		return fmt.Errorf("invalid 'metric_format' %q", t.MetricFormat)
	}
	return nil
}

func (t *Temperature) Gather(acc telegraf.Accumulator) error {
	// Get all sensors and honor the HOST_SYS environment variable
	path := internal.GetSysPath()

	// Try to use the hwmon interface
	temperatures, err := t.gatherHwmon(path)
	if err != nil {
		return fmt.Errorf("getting temperatures failed: %w", err)
	}

	if len(temperatures) == 0 {
		// There is no hwmon interface, fallback to thermal-zone parsing
		temperatures, err = t.gatherThermalZone(path)
		if err != nil {
			return fmt.Errorf("getting temperatures (via fallback) failed: %w", err)
		}
	}

	switch t.MetricFormat {
	case "v1":
		t.createMetricsV1(acc, temperatures)
	case "v2":
		t.createMetricsV2(acc, temperatures)
	}

	return nil
}

func (t *Temperature) createMetricsV1(acc telegraf.Accumulator, temperatures []TemperatureStat) {
	for _, temp := range temperatures {
		sensor := temp.Name
		if temp.Label != "" {
			sensor += "_" + strings.ReplaceAll(temp.Label, " ", "")
		}

		// Mandatory measurement value
		tags := map[string]string{"sensor": sensor + "_input"}
		if t.DeviceTag {
			tags["device"] = temp.Device
		}
		acc.AddFields("temp", map[string]interface{}{"temp": temp.Temperature}, tags)

		// Optional values values
		for measurement, value := range temp.Additional {
			tags := map[string]string{"sensor": sensor + "_" + measurement}
			if t.DeviceTag {
				tags["device"] = temp.Device
			}
			acc.AddFields("temp", map[string]interface{}{"temp": value}, tags)
		}
	}
}

func (t *Temperature) createMetricsV2(acc telegraf.Accumulator, temperatures []TemperatureStat) {
	for _, temp := range temperatures {
		sensor := temp.Name
		if temp.Label != "" {
			sensor += "_" + strings.ReplaceAll(temp.Label, " ", "_")
		}

		// Mandatory measurement value
		tags := map[string]string{"sensor": sensor}
		if t.DeviceTag {
			tags["device"] = temp.Device
		}
		acc.AddFields("temp", map[string]interface{}{"temp": temp.Temperature}, tags)
	}
}

func (t *Temperature) gatherHwmon(syspath string) ([]TemperatureStat, error) {
	// Get all hwmon devices
	sensors, err := filepath.Glob(filepath.Join(syspath, "class", "hwmon", "hwmon*", "temp*_input"))
	if err != nil {
		return nil, fmt.Errorf("getting sensors failed: %w", err)
	}

	// Handle CentOS special path containing an additional "device" directory
	// see https://github.com/shirou/gopsutil/blob/master/host/host_linux.go
	if len(sensors) == 0 {
		sensors, err = filepath.Glob(filepath.Join(syspath, "class", "hwmon", "hwmon*", "device", "temp*_input"))
		if err != nil {
			return nil, fmt.Errorf("getting sensors on CentOS failed: %w", err)
		}
	}

	// Exit early if we cannot find any device
	if len(sensors) == 0 {
		return nil, nil
	}

	// Collect the sensor information
	stats := make([]TemperatureStat, 0, len(sensors))
	for _, s := range sensors {
		// Get the sensor directory and the temperature prefix from the path
		path := filepath.Dir(s)
		prefix := strings.SplitN(filepath.Base(s), "_", 2)[0]

		// Read the sensor and device name
		deviceName, err := os.Readlink(filepath.Join(path, "device"))
		if err == nil {
			deviceName = filepath.Base(deviceName)
		}

		// Read the sensor name and use the device name as fallback
		name := deviceName
		n, err := os.ReadFile(filepath.Join(path, "name"))
		if err == nil {
			name = strings.TrimSpace(string(n))
		}

		// Get the sensor label
		var label string
		if buf, err := os.ReadFile(filepath.Join(path, prefix+"_label")); err == nil {
			label = strings.TrimSpace(string(buf))
		}

		// Do the actual sensor readings
		temp := TemperatureStat{
			Name:       name,
			Label:      strings.ToLower(label),
			Device:     deviceName,
			Additional: make(map[string]interface{}),
		}

		// Temperature (mandatory)
		fn := filepath.Join(path, prefix+"_input")
		buf, err := os.ReadFile(fn)
		if err != nil {
			t.Log.Debugf("Couldn't read temperature from %q: %v", fn, err)
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(buf)), 64); err == nil {
			temp.Temperature = v / scalingFactor
		}

		// Read all possible values of the sensor
		matches, err := filepath.Glob(filepath.Join(path, prefix+"_*"))
		if err != nil {
			t.Log.Warnf("Couldn't read files from %q: %v", filepath.Join(path, prefix+"_*"), err)
			continue
		}
		for _, fn := range matches {
			buf, err = os.ReadFile(fn)
			if err != nil {
				continue
			}
			parts := strings.SplitN(filepath.Base(fn), "_", 2)
			if len(parts) != 2 {
				continue
			}
			measurement := parts[1]

			// Skip already added values
			switch measurement {
			case "label", "input":
				continue
			}

			v, err := strconv.ParseFloat(strings.TrimSpace(string(buf)), 64)
			if err != nil {
				continue
			}
			temp.Additional[measurement] = v / scalingFactor
		}

		stats = append(stats, temp)
	}

	return stats, nil
}

func (t *Temperature) gatherThermalZone(syspath string) ([]TemperatureStat, error) {
	// For file layout see https://www.kernel.org/doc/Documentation/thermal/sysfs-api.txt
	zones, err := filepath.Glob(filepath.Join(syspath, "class", "thermal", "thermal_zone*"))
	if err != nil {
		return nil, fmt.Errorf("getting thermal zones failed: %w", err)
	}

	// Exit early if we cannot find any zone
	if len(zones) == 0 {
		return nil, nil
	}

	// Collect the sensor information
	stats := make([]TemperatureStat, 0, len(zones))
	for _, path := range zones {
		// Type of the zone corresponding to the sensor name in our nomenclature
		buf, err := os.ReadFile(filepath.Join(path, "type"))
		if err != nil {
			t.Log.Errorf("Cannot read name of zone %q", path)
			continue
		}
		name := strings.TrimSpace(string(buf))

		// Actual temperature
		buf, err = os.ReadFile(filepath.Join(path, "temp"))
		if err != nil {
			t.Log.Errorf("Cannot read temperature of zone %q", path)
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(string(buf)), 64)
		if err != nil {
			continue
		}

		temp := TemperatureStat{Name: name, Temperature: v / scalingFactor}
		stats = append(stats, temp)
	}

	return stats, nil
}
