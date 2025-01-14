//go:generate ../../../tools/readme_config_includer/generator
package nsdp

import (
	_ "embed"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/logger"
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
	Debug       bool            `toml:"debug"`

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
	if plugin.Log == nil {
		plugin.Log = logger.New("inputs", pluginName, "")
	}
	return nil
}

func (plugin *NSDP) Gather(acc telegraf.Accumulator) error {
	start := time.Now()
	if plugin.Debug {
		plugin.Log.Infof("Querying %s", plugin.Target)
	}
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
		if plugin.Debug {
			plugin.Log.Infof("Processing device: %s", device)
		}
		plugin.gatherDevice(acc, device, response)
	}
	if plugin.Debug {
		elapsed := time.Since(start)
		plugin.Log.Info("took: ", elapsed)
	}
	return nil
}

func (plugin *NSDP) prepareGatherRequest() (*nsdp.Conn, *nsdp.Message, error) {
	conn, err := nsdp.NewConn(plugin.Target, plugin.Debug)
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
			tags["nsdp_device"] = device
			tags["nsdp_device_ip"] = deviceIP.String()
			tags["nsdp_device_name"] = deviceName
			tags["nsdp_device_model"] = deviceModel
			tags["nsdp_device_port"] = strconv.FormatUint(uint64(port), 10)
			fields := make(map[string]interface{})
			fields["bytes_sent"] = statistic.Sent
			fields["bytes_recv"] = statistic.Received
			fields["packets_total"] = statistic.Packets
			fields["broadcasts_total"] = statistic.Broadcasts
			fields["multicasts_total"] = statistic.Multicasts
			fields["errors_total"] = statistic.Errors
			acc.AddCounter("nsdp_device_port", fields, tags)
			if plugin.Debug {
				plugin.logMetric("nsdp_device_port", tags, fields)
			}
		}
	}
}

func (plugin *NSDP) logMetric(name string, tags map[string]string, fields map[string]interface{}) {
	buffer := strings.Builder{}
	buffer.WriteString(name)
	for tagKey, tagValue := range tags {
		buffer.WriteRune(',')
		buffer.WriteString(tagKey)
		buffer.WriteRune('=')
		buffer.WriteString(fmt.Sprint(tagValue))
	}
	writeSpace := true
	for fieldKey, fieldValue := range fields {
		if writeSpace {
			buffer.WriteRune(' ')
			writeSpace = false
		} else {
			buffer.WriteRune(',')
		}
		buffer.WriteString(fieldKey)
		buffer.WriteRune('=')
		buffer.WriteString(fmt.Sprint(fieldValue))
	}
	buffer.WriteRune(' ')
	buffer.WriteString(fmt.Sprint(time.Now().Unix()))
	plugin.Log.Info(buffer.String())
}

func init() {
	inputs.Add(pluginName, func() telegraf.Input {
		return defaultNSDP()
	})
}
