package nstat

import (
	"bytes"
	"io/ioutil"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

var (
	zeroByte    = []byte("0")
	newLineByte = []byte("\n")
	colonByte   = []byte(":")
)

type Nstat struct {
	ProcNetNetstat string `toml:"proc_net_netstat"`
	ProcNetSNMP    string `toml:"proc_net_snmp"`
	ProcNetSNMP6   string `toml:"proc_net_snmp6"`
	DumpZeros      bool   `toml:"dump_zeros"`
}

var sampleConfig = `
	# file paths
	proc_net_netstat 	= 	"/proc/net/netstat"
	proc_net_snmp 		= 	"/proc/net/snmp"
	proc_net_snmp6 		= 	"/proc/net/snmp6"
	# dump metrics with 0 values too
	dump_zeros			= 	true
`

func (ns *Nstat) Description() string {
	return "Collect network metrics from '/proc/net/netstat', '/proc/net/snmp' & '/proc/net/snmp6' files"
}

func (ns *Nstat) SampleConfig() string {
	return sampleConfig
}

func (ns *Nstat) Gather(acc telegraf.Accumulator) error {
	netstat, err := ioutil.ReadFile(ns.ProcNetNetstat)
	if err != nil {
		return err
	}

	// collect netstat data
	err = ns.gatherNetstat(netstat, acc)
	if err != nil {
		return err
	}

	// collect SNMP data
	snmp, err := ioutil.ReadFile(ns.ProcNetSNMP)
	if err != nil {
		return err
	}
	err = ns.gatherSNMP(snmp, acc)
	if err != nil {
		return err
	}

	// collect SNMP6 data
	snmp6, err := ioutil.ReadFile(ns.ProcNetSNMP6)
	if err != nil {
		return err
	}
	err = ns.gatherSNMP6(snmp6, acc)
	if err != nil {
		return err
	}
	return nil
}

func (ns *Nstat) gatherNetstat(data []byte, acc telegraf.Accumulator) error {
	metrics, err := loadUglyTable(data, ns.DumpZeros)
	if err != nil {
		return err
	}
	tags := map[string]string{
		"name": "netstat",
	}
	acc.AddFields("nstat", metrics, tags)
	return nil
}

func (ns *Nstat) gatherSNMP(data []byte, acc telegraf.Accumulator) error {
	metrics, err := loadUglyTable(data, ns.DumpZeros)
	if err != nil {
		return err
	}
	tags := map[string]string{
		"name": "snmp",
	}
	acc.AddFields("nstat", metrics, tags)
	return nil
}

func (ns *Nstat) gatherSNMP6(data []byte, acc telegraf.Accumulator) error {
	metrics, err := loadGoodTable(data, ns.DumpZeros)
	if err != nil {
		return err
	}
	tags := map[string]string{
		"name": "snmp6",
	}
	acc.AddFields("nstat", metrics, tags)
	return nil
}

// loadGoodTable can be used to parse string heap that
// headers and values are arranged in right order
func loadGoodTable(table []byte, dumpZeros bool) (map[string]interface{}, error) {
	entries := map[string]interface{}{}
	fields := bytes.Fields(table)
	var value int64
	var err error
	// iterate over two values each time
	// first value is header, second is value
	for i := 0; i < len(fields); i = i + 2 {
		// counter is zero
		if bytes.Equal(fields[i+1], zeroByte) {
			if !dumpZeros {
				continue
			} else {
				entries[string(fields[i])] = int64(0)
				continue
			}
		}
		// the counter is not zero, so parse it.
		value, err = strconv.ParseInt(string(fields[i+1]), 10, 64)
		if err == nil {
			entries[string(fields[i])] = value
		}
	}
	return entries, nil
}

// loadUglyTable can be used to parse string heap that
// the headers and values are splitted with a newline
func loadUglyTable(table []byte, dumpZeros bool) (map[string]interface{}, error) {
	entries := map[string]interface{}{}
	// split the lines by newline
	lines := bytes.Split(table, newLineByte)
	var value int64
	var err error
	// iterate over lines, take 2 lines each time
	// first line contains header names
	// second line contains values
	for i := 0; i < len(lines); i = i + 2 {
		if len(lines[i]) == 0 {
			continue
		}
		headers := bytes.Fields(lines[i])
		prefix := bytes.TrimSuffix(headers[0], colonByte)
		metrics := bytes.Fields(lines[i+1])

		for j := 1; j < len(headers); j++ {
			// counter is zero
			if bytes.Equal(metrics[j], zeroByte) {
				if !dumpZeros {
					continue
				} else {
					entries[string(append(prefix, headers[j]...))] = int64(0)
					continue
				}
			}
			// the counter is not zero, so parse it.
			value, err = strconv.ParseInt(string(metrics[j]), 10, 64)
			if err == nil {
				entries[string(append(prefix, headers[j]...))] = value
			}
		}
	}
	return entries, nil
}

func init() {
	inputs.Add("nstat", func() telegraf.Input {
		return &Nstat{}
	})
}
