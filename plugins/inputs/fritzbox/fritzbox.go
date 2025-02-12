//go:generate ../../../tools/readme_config_includer/generator
package fritzbox

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/tdrn-org/go-tr064"
	"github.com/tdrn-org/go-tr064/mesh"
	"github.com/tdrn-org/go-tr064/services/igddesc/igdicfg"
	"github.com/tdrn-org/go-tr064/services/tr64desc/deviceinfo"
	"github.com/tdrn-org/go-tr064/services/tr64desc/hosts"
	"github.com/tdrn-org/go-tr064/services/tr64desc/wancommonifconfig"
	"github.com/tdrn-org/go-tr064/services/tr64desc/wandslifconfig"
	"github.com/tdrn-org/go-tr064/services/tr64desc/wanpppconn"
	"github.com/tdrn-org/go-tr064/services/tr64desc/wlanconfig"
)

//go:embed sample.conf
var sampleConfig string

const pluginName = "fritzbox"

const defaultTimeout = config.Duration(10 * time.Second)

type Fritzbox struct {
	URLs    []string        `toml:"urls"`
	Collect []string        `toml:"collect"`
	Timeout config.Duration `toml:"timeout"`
	tls.ClientConfig
	Log             telegraf.Logger `toml:"-"`
	deviceClients   []*tr064.Client
	serviceHandlers map[string]serviceHandlerFunc
}

func defaultFritzbox() *Fritzbox {
	return &Fritzbox{
		URLs:            make([]string, 0),
		Collect:         []string{"device", "wan", "ppp", "dsl", "wlan"},
		Timeout:         defaultTimeout,
		deviceClients:   make([]*tr064.Client, 0),
		serviceHandlers: make(map[string]serviceHandlerFunc),
	}
}

func (*Fritzbox) SampleConfig() string {
	return sampleConfig
}

func (plugin *Fritzbox) Init() error {
	err := plugin.initDeviceClients()
	if err != nil {
		return err
	}
	plugin.initServiceHandlers()
	return nil
}

func (plugin *Fritzbox) initDeviceClients() error {
	for _, rawUrl := range plugin.URLs {
		parsedUrl, err := url.Parse(rawUrl)
		if err != nil {
			return err
		}
		client := tr064.NewClient(parsedUrl)
		client.Debug = plugin.Log.Level().Includes(telegraf.Debug)
		client.Timeout = time.Duration(plugin.Timeout)
		tlsConfig, err := plugin.TLSConfig()
		if err != nil {
			return err
		}
		client.TlsConfig = tlsConfig
		plugin.deviceClients = append(plugin.deviceClients, client)
	}
	return nil
}

func (plugin *Fritzbox) initServiceHandlers() {
	if choice.Contains("device", plugin.Collect) {
		plugin.serviceHandlers[deviceinfo.ServiceShortType] = plugin.gatherDeviceInfo
	}
	if choice.Contains("wan", plugin.Collect) {
		plugin.serviceHandlers[wancommonifconfig.ServiceShortType] = plugin.gatherWanInfo
	}
	if choice.Contains("ppp", plugin.Collect) {
		plugin.serviceHandlers[wanpppconn.ServiceShortType] = plugin.gatherPppInfo
	}
	if choice.Contains("dsl", plugin.Collect) {
		plugin.serviceHandlers[wandslifconfig.ServiceShortType] = plugin.gatherDslInfo
	}
	if choice.Contains("wlan", plugin.Collect) {
		plugin.serviceHandlers[wlanconfig.ServiceShortType] = plugin.gatherWlanInfo
	}
	if choice.Contains("hosts", plugin.Collect) {
		plugin.serviceHandlers[hosts.ServiceShortType] = plugin.gatherHostsInfo
	}
}

func (plugin *Fritzbox) Gather(acc telegraf.Accumulator) error {
	var waitComplete sync.WaitGroup
	for _, deviceClient := range plugin.deviceClients {
		waitComplete.Add(1)
		go func() {
			defer waitComplete.Done()
			plugin.gatherDevice(acc, deviceClient)
		}()
	}
	waitComplete.Wait()
	return nil
}

func (plugin *Fritzbox) gatherDevice(acc telegraf.Accumulator, deviceClient *tr064.Client) {
	plugin.Log.Debugf("Querying %s", deviceClient.DeviceUrl.Redacted())
	services, err := deviceClient.Services(tr064.DefaultServiceSpec)
	if err != nil {
		acc.AddError(err)
		return
	}
	for _, service := range services {
		serviceHandler := plugin.serviceHandlers[service.ShortType()]
		if serviceHandler == nil {
			continue
		}
		err := serviceHandler(acc, deviceClient, service)
		if err != nil {
			acc.AddError(err)
			return
		}
	}
}

type serviceHandlerFunc func(telegraf.Accumulator, *tr064.Client, tr064.ServiceDescriptor) error

func (plugin *Fritzbox) gatherDeviceInfo(acc telegraf.Accumulator, deviceClient *tr064.Client, service tr064.ServiceDescriptor) error {
	serviceClient := deviceinfo.ServiceClient{
		TR064Client: deviceClient,
		Service:     service,
	}
	info := &deviceinfo.GetInfoResponse{}
	err := serviceClient.GetInfo(info)
	if err != nil {
		return err
	}
	tags := make(map[string]string)
	tags["source"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["service"] = serviceClient.Service.ShortId()
	fields := make(map[string]interface{})
	fields["uptime"] = info.NewUpTime
	fields["model_name"] = info.NewModelName
	fields["serial_number"] = info.NewSerialNumber
	fields["hardware_version"] = info.NewHardwareVersion
	fields["software_version"] = info.NewSoftwareVersion
	acc.AddFields("fritzbox_device", fields, tags)
	return nil
}

func (plugin *Fritzbox) gatherWanInfo(acc telegraf.Accumulator, deviceClient *tr064.Client, service tr064.ServiceDescriptor) error {
	serviceClient := wancommonifconfig.ServiceClient{
		TR064Client: deviceClient,
		Service:     service,
	}
	commonLinkProperties := &wancommonifconfig.GetCommonLinkPropertiesResponse{}
	err := serviceClient.GetCommonLinkProperties(commonLinkProperties)
	if err != nil {
		return err
	}
	// Prefer igdicfg service over wancommonifconfig one for total bytes stats, because igdicfg supports uint64 counters
	igdServices, err := deviceClient.ServicesByType(tr064.IgdServiceSpec, igdicfg.ServiceShortType)
	if err != nil {
		return err
	}
	var totalBytesSent uint64 = 0
	var totalBytesReceived uint64 = 0
	if len(igdServices) > 0 {
		igdServiceClient := &igdicfg.ServiceClient{
			TR064Client: deviceClient,
			Service:     igdServices[0],
		}
		addonInfos := &igdicfg.GetAddonInfosResponse{}
		err = igdServiceClient.GetAddonInfos(addonInfos)
		if err != nil {
			return err
		}
		totalBytesSent, err = strconv.ParseUint(addonInfos.NewX_AVM_DE_TotalBytesSent64, 10, 64)
		if err != nil {
			return err
		}
		totalBytesReceived, err = strconv.ParseUint(addonInfos.NewX_AVM_DE_TotalBytesReceived64, 10, 64)
		if err != nil {
			return err
		}
	} else {
		// Fall back to wancommonifconfig service in case igdicfg is not available (only uint32 based)
		totalBytesSentResponse := &wancommonifconfig.GetTotalBytesSentResponse{}
		err = serviceClient.GetTotalBytesSent(totalBytesSentResponse)
		if err != nil {
			return err
		}
		totalBytesSent = uint64(totalBytesSentResponse.NewTotalBytesSent)
		totalBytesReceivedResponse := &wancommonifconfig.GetTotalBytesReceivedResponse{}
		err = serviceClient.GetTotalBytesReceived(totalBytesReceivedResponse)
		if err != nil {
			return err
		}
		totalBytesReceived = uint64(totalBytesReceivedResponse.NewTotalBytesReceived)
	}
	tags := make(map[string]string)
	tags["source"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["service"] = serviceClient.Service.ShortId()
	fields := make(map[string]interface{})
	fields["layer1_upstream_max_bit_rate"] = commonLinkProperties.NewLayer1UpstreamMaxBitRate
	fields["layer1_downstream_max_bit_rate"] = commonLinkProperties.NewLayer1DownstreamMaxBitRate
	fields["upstream_current_max_speed"] = commonLinkProperties.NewX_AVM_DE_UpstreamCurrentMaxSpeed
	fields["downstream_current_max_speed"] = commonLinkProperties.NewX_AVM_DE_DownstreamCurrentMaxSpeed
	fields["total_bytes_sent"] = totalBytesSent
	fields["total_bytes_received"] = totalBytesReceived
	acc.AddFields("fritzbox_wan", fields, tags)
	return nil
}

func (plugin *Fritzbox) gatherPppInfo(acc telegraf.Accumulator, deviceClient *tr064.Client, service tr064.ServiceDescriptor) error {
	serviceClient := wanpppconn.ServiceClient{
		TR064Client: deviceClient,
		Service:     service,
	}
	info := &wanpppconn.GetInfoResponse{}
	err := serviceClient.GetInfo(info)
	if err != nil {
		return err
	}
	tags := make(map[string]string)
	tags["source"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["service"] = serviceClient.Service.ShortId()
	fields := make(map[string]interface{})
	fields["uptime"] = info.NewUptime
	fields["upstream_max_bit_rate"] = info.NewUpstreamMaxBitRate
	fields["downstream_max_bit_rate"] = info.NewDownstreamMaxBitRate
	acc.AddFields("fritzbox_ppp", fields, tags)
	return nil
}

func (plugin *Fritzbox) gatherDslInfo(acc telegraf.Accumulator, deviceClient *tr064.Client, service tr064.ServiceDescriptor) error {
	serviceClient := wandslifconfig.ServiceClient{
		TR064Client: deviceClient,
		Service:     service,
	}
	info := &wandslifconfig.GetInfoResponse{}
	err := serviceClient.GetInfo(info)
	if err != nil {
		return err
	}
	statisticsTotal := &wandslifconfig.GetStatisticsTotalResponse{}
	if info.NewStatus == "Up" {
		err = serviceClient.GetStatisticsTotal(statisticsTotal)
		if err != nil {
			return err
		}
	}
	tags := make(map[string]string)
	tags["source"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["service"] = serviceClient.Service.ShortId()
	tags["status"] = info.NewStatus
	fields := make(map[string]interface{})
	fields["upstream_curr_rate"] = info.NewUpstreamCurrRate
	fields["downstream_curr_rate"] = info.NewDownstreamCurrRate
	fields["upstream_max_rate"] = info.NewUpstreamMaxRate
	fields["downstream_max_rate"] = info.NewDownstreamMaxRate
	fields["upstream_noise_margin"] = info.NewUpstreamNoiseMargin
	fields["downstream_noise_margin"] = info.NewDownstreamNoiseMargin
	fields["upstream_attenuation"] = info.NewUpstreamAttenuation
	fields["downstream_attenuation"] = info.NewDownstreamAttenuation
	fields["upstream_power"] = info.NewUpstreamPower
	fields["downstream_power"] = info.NewDownstreamPower
	fields["receive_blocks"] = statisticsTotal.NewReceiveBlocks
	fields["transmit_blocks"] = statisticsTotal.NewTransmitBlocks
	fields["cell_delin"] = statisticsTotal.NewCellDelin
	fields["link_retrain"] = statisticsTotal.NewLinkRetrain
	fields["init_errors"] = statisticsTotal.NewInitErrors
	fields["init_timeouts"] = statisticsTotal.NewInitTimeouts
	fields["loss_of_framing"] = statisticsTotal.NewLossOfFraming
	fields["errored_secs"] = statisticsTotal.NewErroredSecs
	fields["severly_errored_secs"] = statisticsTotal.NewSeverelyErroredSecs
	fields["fec_errors"] = statisticsTotal.NewFECErrors
	fields["atuc_fec_errors"] = statisticsTotal.NewATUCFECErrors
	fields["hec_errors"] = statisticsTotal.NewHECErrors
	fields["atuc_hec_errors"] = statisticsTotal.NewATUCHECErrors
	fields["crc_errors"] = statisticsTotal.NewCRCErrors
	fields["atuc_crc_errors"] = statisticsTotal.NewATUCCRCErrors
	acc.AddFields("fritzbox_dsl", fields, tags)
	return nil
}

func (plugin *Fritzbox) gatherWlanInfo(acc telegraf.Accumulator, deviceClient *tr064.Client, service tr064.ServiceDescriptor) error {
	serviceClient := wlanconfig.ServiceClient{
		TR064Client: deviceClient,
		Service:     service,
	}
	info := &wlanconfig.GetInfoResponse{}
	err := serviceClient.GetInfo(info)
	if err != nil {
		return err
	}
	totalAssociations := &wlanconfig.GetTotalAssociationsResponse{}
	if info.NewStatus == "Up" {
		err = serviceClient.GetTotalAssociations(totalAssociations)
		if err != nil {
			return err
		}
	}
	tags := make(map[string]string)
	tags["source"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["service"] = serviceClient.Service.ShortId()
	tags["status"] = info.NewStatus
	tags["ssid"] = info.NewSSID
	tags["channel"] = strconv.Itoa(int(info.NewChannel))
	tags["band"] = plugin.wlanBandFromInfo(info)
	fields := make(map[string]interface{})
	fields["total_associations"] = totalAssociations.NewTotalAssociations
	acc.AddGauge("fritzbox_wlan", fields, tags)
	return nil
}

func (plugin *Fritzbox) wlanBandFromInfo(info *wlanconfig.GetInfoResponse) string {
	band := info.NewX_AVM_DE_FrequencyBand
	if band != "" {
		return band
	}
	if 1 <= info.NewChannel && info.NewChannel <= 14 {
		return "2400"
	}
	return "5000"
}

func (plugin *Fritzbox) gatherHostsInfo(acc telegraf.Accumulator, deviceClient *tr064.Client, service tr064.ServiceDescriptor) error {
	serviceClient := hosts.ServiceClient{
		TR064Client: deviceClient,
		Service:     service,
	}
	connections, err := plugin.fetchHostsConnections(&serviceClient)
	if err != nil {
		return err
	}
	for _, connection := range connections {
		_, err = uuid.Parse(connection.RightDeviceName)
		if err == nil {
			continue
		}
		tags := make(map[string]string)
		tags["source"] = serviceClient.TR064Client.DeviceUrl.Hostname()
		tags["service"] = serviceClient.Service.ShortId()
		tags["node"] = connection.RightDeviceName
		tags["node_role"] = plugin.hostRole(connection.RightMeshRole)
		tags["node_ap"] = connection.LeftDeviceName
		tags["node_ap_role"] = plugin.hostRole(connection.LeftMeshRole)
		tags["link_type"] = connection.InterfaceType
		tags["link_name"] = connection.InterfaceName
		fields := make(map[string]interface{})
		fields["max_data_rate_tx"] = connection.MaxDataRateTx
		fields["max_data_rate_rx"] = connection.MaxDataRateRx
		fields["cur_data_rate_tx"] = connection.CurDataRateTx
		fields["cur_data_rate_rx"] = connection.CurDataRateRx
		acc.AddGauge("fritzbox_hosts", fields, tags)
	}
	return nil
}

func (plugin *Fritzbox) hostRole(role string) string {
	if role == "unknown" {
		return "client"
	}
	return role
}

func (plugin *Fritzbox) fetchHostsConnections(serviceClient *hosts.ServiceClient) ([]*mesh.Connection, error) {
	meshListPath := &hosts.X_AVM_DE_GetMeshListPathResponse{}
	err := serviceClient.X_AVM_DE_GetMeshListPath(meshListPath)
	if err != nil {
		return nil, err
	}
	meshListResponse, err := serviceClient.TR064Client.Get(meshListPath.NewX_AVM_DE_MeshListPath)
	if err != nil {
		return nil, err
	}
	if meshListResponse.StatusCode == http.StatusNotFound {
		return make([]*mesh.Connection, 0), nil
	}
	if meshListResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch mesh list %q (status: %s)", meshListPath.NewX_AVM_DE_MeshListPath, meshListResponse.Status)
	}
	defer meshListResponse.Body.Close()
	meshListBytes, err := io.ReadAll(meshListResponse.Body)
	if err != nil {
		return nil, err
	}
	meshList := &mesh.List{}
	err = json.Unmarshal(meshListBytes, meshList)
	if err != nil {
		return nil, err
	}
	return meshList.Connections(), nil
}

func init() {
	inputs.Add(pluginName, func() telegraf.Input {
		return defaultFritzbox()
	})
}
