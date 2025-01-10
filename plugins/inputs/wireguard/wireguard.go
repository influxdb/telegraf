//go:generate ../../../tools/readme_config_includer/generator
package wireguard

import (
	_ "embed"
	"fmt"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	measurementDevice = "wireguard_device"
	measurementPeer   = "wireguard_peer"
)

var (
	deviceTypeNames = map[wgtypes.DeviceType]string{
		wgtypes.Unknown:     "unknown",
		wgtypes.LinuxKernel: "linux_kernel",
		wgtypes.Userspace:   "userspace",
	}
)

// Wireguard is an input that enumerates all Wireguard interfaces/devices on
// the host, and reports gauge metrics for the device itself and its peers.
type Wireguard struct {
	Devices []string        `toml:"devices"`
	Log     telegraf.Logger `toml:"-"`

	client *wgctrl.Client
}

func (*Wireguard) SampleConfig() string {
	return sampleConfig
}

func (wg *Wireguard) Init() error {
	var err error

	wg.client, err = wgctrl.New()

	return err
}

func (wg *Wireguard) Gather(acc telegraf.Accumulator) error {
	devices, err := wg.enumerateDevices()
	if err != nil {
		return fmt.Errorf("error enumerating Wireguard devices: %w", err)
	}

	for _, device := range devices {
		gatherDeviceMetrics(acc, device)

		for _, peer := range device.Peers {
			gatherDevicePeerMetrics(acc, device, peer)
		}
	}

	return nil
}

func (wg *Wireguard) enumerateDevices() ([]*wgtypes.Device, error) {
	// If no device names are specified, defer to the library to enumerate
	// all of them
	if len(wg.Devices) == 0 {
		return wg.client.Devices()
	}

	// Otherwise, explicitly populate only device names specified in config
	devices := make([]*wgtypes.Device, 0, len(wg.Devices))
	for _, name := range wg.Devices {
		dev, err := wg.client.Device(name)
		if err != nil {
			wg.Log.Warnf("No Wireguard device found with name %s", name)
			continue
		}

		devices = append(devices, dev)
	}

	return devices, nil
}

func gatherDeviceMetrics(acc telegraf.Accumulator, device *wgtypes.Device) {
	fields := map[string]interface{}{
		"listen_port":   device.ListenPort,
		"firewall_mark": device.FirewallMark,
	}

	gauges := map[string]interface{}{
		"peers": len(device.Peers),
	}

	tags := map[string]string{
		"name": device.Name,
		"type": deviceTypeNames[device.Type],
	}

	acc.AddFields(measurementDevice, fields, tags)
	acc.AddGauge(measurementDevice, gauges, tags)
}

func gatherDevicePeerMetrics(acc telegraf.Accumulator, device *wgtypes.Device, peer wgtypes.Peer) {
	fields := map[string]interface{}{
		"persistent_keepalive_interval_ns": peer.PersistentKeepaliveInterval.Nanoseconds(),
		"protocol_version":                 peer.ProtocolVersion,
		"allowed_ips":                      len(peer.AllowedIPs),
	}

	if len(peer.AllowedIPs) > 0 {
		cidrs := make([]string, 0, len(peer.AllowedIPs))
		for _, ip := range peer.AllowedIPs {
			cidrs = append(cidrs, ip.String())
		}
		fields["allowed_peer_cidr"] = strings.Join(cidrs, ",")
	}

	gauges := map[string]interface{}{
		"last_handshake_time_ns": peer.LastHandshakeTime.UnixNano(),
		"rx_bytes":               peer.ReceiveBytes,
		"tx_bytes":               peer.TransmitBytes,
	}

	tags := map[string]string{
		"device":     device.Name,
		"public_key": peer.PublicKey.String(),
	}

	acc.AddFields(measurementPeer, fields, tags)
	acc.AddGauge(measurementPeer, gauges, tags)
}

func init() {
	inputs.Add("wireguard", func() telegraf.Input {
		return &Wireguard{}
	})
}
