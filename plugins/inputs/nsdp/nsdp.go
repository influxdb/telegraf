//go:generate ../../../tools/readme_config_includer/generator
package nsdp

import (
	_ "embed"
	"net"
	"strconv"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/tdrn-org/go-nsdp"
)

//go:embed sample.conf
var sampleConfig string

const pluginName = "nsdp"

const defaultTimeout = config.Duration(2 * time.Second)

type NSDP struct {
	Target      string          `toml:"target"`
	DeviceLimit uint            `toml:"device_limit"`
	Timeout     config.Duration `toml:"timeout"`

	Log telegraf.Logger `toml:"-"`
}

func defaultNSDP() *NSDP {
	return &NSDP{
		Target:  nsdp.IPv4BroadcastTarget,
		Timeout: defaultTimeout,
	}
}

func (*NSDP) SampleConfig() string {
	return sampleConfig
}

func (plugin *NSDP) Init() error {
	return nil
}

func (plugin *NSDP) Gather(acc telegraf.Accumulator) error {
	plugin.Log.Debugf("Querying %s", plugin.Target)
	conn, request, err := plugin.prepareGatherRequest()
	if err != nil {
		return err
	}
	defer conn.Close()
	responses, err := conn.SendReceiveMessage(request)
	if err != nil {
		return err
	}
	for device, response := range responses {
		plugin.Log.Debugf("Processing device: %s", device)
		plugin.gatherDevice(acc, device, response)
	}
	return nil
}

func (plugin *NSDP) prepareGatherRequest() (*nsdp.Conn, *nsdp.Message, error) {
	conn, err := nsdp.NewConn(plugin.Target, plugin.Log.Level().Includes(telegraf.Debug))
	if err != nil {
		return nil, nil, err
	}
	conn.ReceiveDeviceLimit = plugin.DeviceLimit
	conn.ReceiveTimeout = time.Duration(plugin.Timeout)
	request := nsdp.NewMessage(nsdp.ReadRequest)
	request.AppendTLV(nsdp.EmptyDeviceModel())
	request.AppendTLV(nsdp.EmptyDeviceName())
	request.AppendTLV(nsdp.EmptyDeviceIP())
	request.AppendTLV(nsdp.EmptyPortStatistic())
	return conn, request, nil
}

func (plugin *NSDP) gatherDevice(acc telegraf.Accumulator, device string, response *nsdp.Message) {
	var deviceModel string
	var deviceName string
	var deviceIP net.IP
	portStatistics := make(map[uint8]*nsdp.PortStatistic, 0)
	for _, tlv := range response.Body {
		switch tlv.Type() {
		case nsdp.TypeDeviceModel:
			deviceModel = tlv.(*nsdp.DeviceModel).Model
		case nsdp.TypeDeviceName:
			deviceName = tlv.(*nsdp.DeviceName).Name
		case nsdp.TypeDeviceIP:
			deviceIP = tlv.(*nsdp.DeviceIP).IP
		case nsdp.TypePortStatistic:
			portStatistic := tlv.(*nsdp.PortStatistic)
			portStatistics[portStatistic.Port] = portStatistic
		}
	}
	for port, statistic := range portStatistics {
		if statistic.Received != 0 || statistic.Sent != 0 {
			tags := make(map[string]string)
			tags["device"] = device
			tags["device_ip"] = deviceIP.String()
			tags["device_name"] = deviceName
			tags["device_model"] = deviceModel
			tags["device_port"] = strconv.FormatUint(uint64(port), 10)
			fields := make(map[string]interface{})
			fields["bytes_sent"] = statistic.Sent
			fields["bytes_recv"] = statistic.Received
			fields["packets_total"] = statistic.Packets
			fields["broadcasts_total"] = statistic.Broadcasts
			fields["multicasts_total"] = statistic.Multicasts
			fields["errors_total"] = statistic.Errors
			acc.AddCounter("nsdp_device_port", fields, tags)
		}
	}
}

func init() {
	inputs.Add(pluginName, func() telegraf.Input {
		return defaultNSDP()
	})
}
