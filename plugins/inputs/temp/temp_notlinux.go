//go:build !linux
// +build !linux

package temp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/influxdata/telegraf"
)

func (t *Temperature) Init() error {
	if t.MetricFormat != "" {
		t.Log.Warn("Ignoring 'metric_format' on non-Linux platforms!")
	}

	if t.DeviceTag {
		t.Log.Warn("Ignoring 'add_device_tag' on non-Linux platforms!")
	}

	return nil
}

func (*Temperature) Gather(acc telegraf.Accumulator) error {
	temps, err := sensors.SensorsTemperatures()
	if err != nil {
		var sensorsWarnings *sensors.Warnings
		if !errors.As(err, &sensorsWarnings) {
			if strings.Contains(err.Error(), "not implemented yet") {
				return fmt.Errorf("plugin is not supported on this platform: %w", err)
			}
			return fmt.Errorf("getting temperatures failed: %w", err)
		}
	}
	for _, temp := range temps {
		acc.AddFields(
			"temp",
			map[string]interface{}{"temp": temp.Temperature},
			map[string]string{"sensor": temp.SensorKey},
		)
	}
	return nil
}
