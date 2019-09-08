package wireguard

import (
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

const (
	measurementDevice = "wireguard_device"
	measurementPeer   = "wireguard_peer"
)

var (
	deviceTypeNames = map[wgtypes.DeviceType]string{
		wgtypes.Unknown:       "unknown",
		wgtypes.LinuxKernel:   "linux_kernel",
		wgtypes.OpenBSDKernel: "openbsd_kernel",
		wgtypes.Userspace:     "userspace",
	}
)

// Wireguard is an input that enumerates all Wireguard interfaces/devices on the host, and reports
// gauge metrics for the device itself and its peers.
type Wireguard struct {
	Devices []string `toml:"devices"`

	client      *wgctrl.Client
	initialized bool
}

func (wg *Wireguard) Description() string {
	return "Collect Wireguard server interface and peer statistics"
}

func (wg *Wireguard) SampleConfig() string {
	return `
  ## Optional list of Wireguard device/interface names to query.
  ## If omitted, all Wireguard interfaces are queried.
  # devices = ["wg0"]
`
}

func (wg *Wireguard) Init() error {
	var err error

	if wg.initialized {
		return nil
	}

	if wg.client, err = wgctrl.New(); err != nil {
		return err
	}

	wg.initialized = true
	return nil
}

func (wg *Wireguard) Gather(acc telegraf.Accumulator) error {
	if err := wg.Init(); err != nil {
		return err
	}

	devices, err := wg.enumerateDevices()
	if err != nil {
		acc.AddError(err)
		return nil
	}

	for _, device := range devices {
		wg.gatherDeviceMetrics(acc, device)

		for _, peer := range device.Peers {
			wg.gatherDevicePeerMetrics(acc, device, peer)
		}
	}

	return nil
}

func (wg *Wireguard) enumerateDevices() ([]*wgtypes.Device, error) {
	var devices []*wgtypes.Device

	// If no device names are specified, defer to the library to enumerate all of them
	if len(wg.Devices) == 0 {
		return wg.client.Devices()
	}

	// Otherwise, explicitly populate only those device names specified in config
	for _, name := range wg.Devices {
		dev, err := wg.client.Device(name)
		if err != nil {
			return nil, err
		}

		devices = append(devices, dev)
	}

	return devices, nil
}

func (wg *Wireguard) gatherDeviceMetrics(acc telegraf.Accumulator, device *wgtypes.Device) {
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

func (wg *Wireguard) gatherDevicePeerMetrics(acc telegraf.Accumulator, device *wgtypes.Device, peer wgtypes.Peer) {
	fields := map[string]interface{}{
		"persistent_keepalive_interval": int64(peer.PersistentKeepaliveInterval.Seconds()),
		"protocol_version":              peer.ProtocolVersion,
		"allowed_ips":                   len(peer.AllowedIPs),
	}

	gauges := map[string]interface{}{
		"last_handshake_time": peer.LastHandshakeTime.Unix(),
		"rx_bytes":            peer.ReceiveBytes,
		"tx_bytes":            peer.TransmitBytes,
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
