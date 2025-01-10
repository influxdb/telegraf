//go:generate ../../../tools/readme_config_includer/generator
package ipset

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/influxdata/telegraf"
)

type ipsetEntries struct {
	initialized bool
	setName     string
	entries     int
	ips         int
}

func getCountInCidr(cidr string) (int, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		// check if single IP
		if net.ParseIP(cidr) == nil {
			return 0, errors.New("invalid IP address format. Not CIDR format and not a single IP address")
		}
		return 1, nil // Single IP has only one address
	}

	ones, bits := ipNet.Mask.Size()
	if ones == 0 && bits == 0 {
		return 0, errors.New("invalid CIDR range")
	}
	numIps := 1 << (bits - ones)

	// exclude network and broadcast addresses if IPv4 and range > /31
	if bits == 32 && numIps > 2 {
		numIps -= 2
	}

	return numIps, nil
}

func (counter *ipsetEntries) addLine(line string, acc telegraf.Accumulator) error {
	data := strings.Fields(line)
	if len(data) < 3 {
		return fmt.Errorf("error parsing line (expected at least 3 fields): %s", line)
	}

	switch data[0] {
	case "create":
		counter.commit(acc)
		counter.initialized = true
		counter.setName = data[1]
		counter.entries = 0
		counter.ips = 0
	case "add":
		counter.entries++
		count, err := getCountInCidr(data[2])
		if err != nil {
			return err
		}
		counter.ips += count
	}
	return nil
}

func (counter *ipsetEntries) commit(acc telegraf.Accumulator) {
	if !counter.initialized {
		return
	}

	fields := map[string]interface{}{
		"entries": counter.entries,
		"ips":     counter.ips,
	}

	tags := map[string]string{
		"set": counter.setName,
	}

	acc.AddGauge(measurement, fields, tags)

	// reset counter and prepare for next usage
	counter.initialized = false
}
