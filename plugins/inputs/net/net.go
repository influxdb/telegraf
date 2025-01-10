//go:generate ../../../tools/readme_config_includer/generator
package net

import (
	_ "embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/system"
)

//go:embed sample.conf
var sampleConfig string

type Net struct {
	Interfaces          []string `toml:"interfaces"`
	IgnoreProtocolStats bool     `toml:"ignore_protocol_stats"`

	filter     filter.Filter
	ps         system.PS
	skipChecks bool
}

func (*Net) SampleConfig() string {
	return sampleConfig
}

func (n *Net) Init() error {
	if !n.IgnoreProtocolStats {
		config.PrintOptionValueDeprecationNotice("inputs.net", "ignore_protocol_stats", "false",
			telegraf.DeprecationInfo{
				Since:     "1.27.3",
				RemovalIn: "1.36.0",
				Notice:    "use the 'inputs.nstat' plugin instead for protocol stats",
			},
		)
	}

	return nil
}

func (n *Net) Gather(acc telegraf.Accumulator) error {
	netio, err := n.ps.NetIO()
	if err != nil {
		return fmt.Errorf("error getting net io info: %w", err)
	}

	if n.filter == nil {
		if n.filter, err = filter.Compile(n.Interfaces); err != nil {
			return fmt.Errorf("error compiling filter: %w", err)
		}
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("error getting list of interfaces: %w", err)
	}
	interfacesByName := make(map[string]net.Interface, len(interfaces))
	for _, iface := range interfaces {
		interfacesByName[iface.Name] = iface
	}

	for _, io := range netio {
		if len(n.Interfaces) != 0 {
			var found bool

			if n.filter.Match(io.Name) {
				found = true
			}

			if !found {
				continue
			}
		} else if !n.skipChecks {
			iface, ok := interfacesByName[io.Name]
			if !ok {
				continue
			}

			if iface.Flags&net.FlagLoopback == net.FlagLoopback {
				continue
			}

			if iface.Flags&net.FlagUp == 0 {
				continue
			}
		}

		tags := map[string]string{
			"interface": io.Name,
		}

		fields := map[string]interface{}{
			"bytes_sent":   io.BytesSent,
			"bytes_recv":   io.BytesRecv,
			"packets_sent": io.PacketsSent,
			"packets_recv": io.PacketsRecv,
			"err_in":       io.Errin,
			"err_out":      io.Errout,
			"drop_in":      io.Dropin,
			"drop_out":     io.Dropout,
			"speed":        getInterfaceSpeed(io.Name),
		}
		acc.AddCounter("net", fields, tags)
	}

	// Get system wide stats for different network protocols
	// (ignore these stats if the call fails)
	if !n.IgnoreProtocolStats {
		//nolint:errcheck // stats ignored on fail
		netprotos, _ := n.ps.NetProto()
		fields := make(map[string]interface{})
		for _, proto := range netprotos {
			for stat, value := range proto.Stats {
				name := fmt.Sprintf("%s_%s", strings.ToLower(proto.Protocol),
					strings.ToLower(stat))
				fields[name] = value
			}
		}
		tags := map[string]string{
			"interface": "all",
		}
		acc.AddFields("net", fields, tags)
	}

	return nil
}

// Get the interface speed from /sys/class/net/*/speed file. returns -1 if unsupported
func getInterfaceSpeed(ioName string) int64 {
	sysPath := internal.GetSysPath()

	raw, err := os.ReadFile(filepath.Join(sysPath, "class", "net", ioName, "speed"))
	if err != nil {
		return -1
	}

	speed, err := strconv.ParseInt(strings.TrimSuffix(string(raw), "\n"), 10, 64)
	if err != nil {
		return -1
	}
	return speed
}

func init() {
	inputs.Add("net", func() telegraf.Input {
		return &Net{ps: system.NewSystemPS()}
	})
}
