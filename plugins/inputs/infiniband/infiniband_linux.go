//go:build linux

package infiniband

import (
	"errors"
	"strconv"

	"github.com/Mellanox/rdmamap"

	"github.com/influxdata/telegraf"
)

// Gather statistics from our infiniband cards
func (*Infiniband) Gather(acc telegraf.Accumulator) error {
	rdmaDevices := rdmamap.GetRdmaDeviceList()

	if len(rdmaDevices) == 0 {
		return errors.New("no InfiniBand devices found in /sys/class/infiniband/")
	}

	for _, dev := range rdmaDevices {
		devicePorts := rdmamap.GetPorts(dev)
		for _, port := range devicePorts {
			portInt, err := strconv.Atoi(port)
			if err != nil {
				return err
			}

			stats, err := rdmamap.GetRdmaSysfsStats(dev, portInt)
			if err != nil {
				continue
			}

			addStats(dev, port, stats, acc)
		}
	}

	return nil
}

// Add the statistics to the accumulator
func addStats(dev, port string, stats []rdmamap.RdmaStatEntry, acc telegraf.Accumulator) {
	// Allow users to filter by card and port
	tags := map[string]string{"device": dev, "port": port}
	fields := make(map[string]interface{})

	for _, entry := range stats {
		fields[entry.Name] = entry.Value
	}

	acc.AddFields("infiniband", fields, tags)
}
