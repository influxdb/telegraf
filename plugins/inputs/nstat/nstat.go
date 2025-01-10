//go:generate ../../../tools/readme_config_includer/generator
package nstat

import (
	"bytes"
	_ "embed"
	"os"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	zeroByte    = []byte("0")
	newLineByte = []byte("\n")
	colonByte   = []byte(":")
)

const (
	// default file paths
	netNetstat = "/net/netstat"
	netSnmp    = "/net/snmp"
	netSnmp6   = "/net/snmp6"
	netProc    = "/proc"

	// env variable names
	envNetstat = "PROC_NET_NETSTAT"
	envSnmp    = "PROC_NET_SNMP"
	envSnmp6   = "PROC_NET_SNMP6"
	envRoot    = "PROC_ROOT"
)

type Nstat struct {
	ProcNetNetstat string `toml:"proc_net_netstat"`
	ProcNetSNMP    string `toml:"proc_net_snmp"`
	ProcNetSNMP6   string `toml:"proc_net_snmp6"`
	DumpZeros      bool   `toml:"dump_zeros"`
}

func (*Nstat) SampleConfig() string {
	return sampleConfig
}

func (ns *Nstat) Gather(acc telegraf.Accumulator) error {
	// load paths, get from env if config values are empty
	ns.loadPaths()

	netstat, err := os.ReadFile(ns.ProcNetNetstat)
	if err != nil {
		return err
	}

	// collect netstat data
	ns.gatherNetstat(netstat, acc)

	// collect SNMP data
	snmp, err := os.ReadFile(ns.ProcNetSNMP)
	if err != nil {
		return err
	}
	ns.gatherSNMP(snmp, acc)

	// collect SNMP6 data, if SNMP6 directory exists (IPv6 enabled)
	snmp6, err := os.ReadFile(ns.ProcNetSNMP6)
	if err == nil {
		ns.gatherSNMP6(snmp6, acc)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (ns *Nstat) gatherNetstat(data []byte, acc telegraf.Accumulator) {
	metrics := ns.loadUglyTable(data)
	tags := map[string]string{
		"name": "netstat",
	}
	acc.AddFields("nstat", metrics, tags)
}

func (ns *Nstat) gatherSNMP(data []byte, acc telegraf.Accumulator) {
	metrics := ns.loadUglyTable(data)
	tags := map[string]string{
		"name": "snmp",
	}
	acc.AddFields("nstat", metrics, tags)
}

func (ns *Nstat) gatherSNMP6(data []byte, acc telegraf.Accumulator) {
	metrics := ns.loadGoodTable(data)
	tags := map[string]string{
		"name": "snmp6",
	}
	acc.AddFields("nstat", metrics, tags)
}

// loadPaths can be used to read paths firstly from config
// if it is empty then try read from env variables
func (ns *Nstat) loadPaths() {
	if ns.ProcNetNetstat == "" {
		ns.ProcNetNetstat = proc(envNetstat, netNetstat)
	}
	if ns.ProcNetSNMP == "" {
		ns.ProcNetSNMP = proc(envSnmp, netSnmp)
	}
	if ns.ProcNetSNMP6 == "" {
		ns.ProcNetSNMP6 = proc(envSnmp6, netSnmp6)
	}
}

// loadGoodTable can be used to parse string heap that
// headers and values are arranged in right order
func (ns *Nstat) loadGoodTable(table []byte) map[string]interface{} {
	entries := make(map[string]interface{})
	fields := bytes.Fields(table)
	var value int64
	var err error
	// iterate over two values each time
	// first value is header, second is value
	for i := 0; i < len(fields); i = i + 2 {
		// counter is zero
		if bytes.Equal(fields[i+1], zeroByte) {
			if !ns.DumpZeros {
				continue
			}

			entries[string(fields[i])] = int64(0)
			continue
		}
		// the counter is not zero, so parse it.
		value, err = strconv.ParseInt(string(fields[i+1]), 10, 64)
		if err == nil {
			entries[string(fields[i])] = value
		}
	}
	return entries
}

// loadUglyTable can be used to parse string heap that
// the headers and values are split with a newline
func (ns *Nstat) loadUglyTable(table []byte) map[string]interface{} {
	entries := make(map[string]interface{})
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
				if !ns.DumpZeros {
					continue
				}

				entries[string(append(prefix, headers[j]...))] = int64(0)
				continue
			}
			// the counter is not zero, so parse it.
			value, err = strconv.ParseInt(string(metrics[j]), 10, 64)
			if err == nil {
				entries[string(append(prefix, headers[j]...))] = value
			}
		}
	}
	return entries
}

// proc can be used to read file paths from env
func proc(env, path string) string {
	// try to read full file path
	if p := os.Getenv(env); p != "" {
		return p
	}
	// try to read root path, or use default root path
	root := os.Getenv(envRoot)
	if root == "" {
		root = netProc
	}
	return root + path
}

func init() {
	inputs.Add("nstat", func() telegraf.Input {
		return &Nstat{}
	})
}
