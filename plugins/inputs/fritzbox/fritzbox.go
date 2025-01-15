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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/logger"
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
	Devices           []string        `toml:"devices"`
	DeviceInfo        bool            `toml:"device_info"`
	WanInfo           bool            `toml:"wan_info"`
	PppInfo           bool            `toml:"ppp_info"`
	DslInfo           bool            `toml:"dsl_info"`
	WlanInfo          bool            `toml:"wlan_info"`
	HostsInfo         bool            `toml:"hosts_info"`
	FullQueryCycle    int             `toml:"full_query_cycle"`
	Timeout           config.Duration `toml:"timeout"`
	TlsSkipVerify     bool            `toml:"tls_skip_verify"`
	Debug             bool            `toml:"debug"`
	Log               telegraf.Logger `toml:"-"`
	deviceClients     []*tr064.Client
	serviceHandlers   map[string]serviceHandlerFunc
	queryCycleCounter int
}

func defaultFritzbox() *Fritzbox {
	return &Fritzbox{
		Devices:           make([]string, 0),
		DeviceInfo:        true,
		WanInfo:           true,
		PppInfo:           true,
		DslInfo:           true,
		WlanInfo:          true,
		HostsInfo:         false,
		FullQueryCycle:    30,
		Timeout:           defaultTimeout,
		deviceClients:     make([]*tr064.Client, 0),
		serviceHandlers:   make(map[string]serviceHandlerFunc),
		queryCycleCounter: 0,
	}
}

func (*Fritzbox) SampleConfig() string {
	return sampleConfig
}

func (plugin *Fritzbox) Init() error {
	if plugin.Log == nil {
		plugin.Log = logger.New("inputs", pluginName, "")
	}
	err := plugin.initDeviceClients()
	if err != nil {
		return err
	}
	plugin.initServiceHandlers()
	return nil
}

func (plugin *Fritzbox) initDeviceClients() error {
	for _, device := range plugin.Devices {
		deviceUrl, err := url.Parse(device)
		if err != nil {
			return err
		}
		client := tr064.NewClient(deviceUrl)
		client.Debug = plugin.Debug
		client.Timeout = time.Duration(plugin.Timeout)
		client.InsecureSkipVerify = plugin.TlsSkipVerify
		plugin.deviceClients = append(plugin.deviceClients, client)
	}
	return nil
}

func (plugin *Fritzbox) initServiceHandlers() {
	if plugin.DeviceInfo {
		plugin.serviceHandlers[deviceinfo.ServiceShortType] = plugin.gatherDeviceInfo
	}
	if plugin.WanInfo {
		plugin.serviceHandlers[wancommonifconfig.ServiceShortType] = plugin.gatherWanInfo
	}
	if plugin.PppInfo {
		plugin.serviceHandlers[wanpppconn.ServiceShortType] = plugin.gatherPppInfo
	}
	if plugin.DslInfo {
		plugin.serviceHandlers[wandslifconfig.ServiceShortType] = plugin.gatherDslInfo
	}
	if plugin.WlanInfo {
		plugin.serviceHandlers[wlanconfig.ServiceShortType] = plugin.gatherWlanInfo
	}
	if plugin.HostsInfo {
		plugin.serviceHandlers[hosts.ServiceShortType] = plugin.gatherHostsInfo
	}
}

func (plugin *Fritzbox) Gather(acc telegraf.Accumulator) error {
	start := time.Now()
	for _, deviceClient := range plugin.deviceClients {
		plugin.gatherDevice(acc, deviceClient)
	}
	if plugin.Debug {
		elapsed := time.Since(start)
		plugin.Log.Info("took: ", elapsed)
	}
	plugin.queryCycleCounter++
	if plugin.queryCycleCounter >= plugin.FullQueryCycle {
		plugin.queryCycleCounter = 0
	}
	return nil
}

func (plugin *Fritzbox) gatherDevice(acc telegraf.Accumulator, deviceClient *tr064.Client) {
	if plugin.Debug {
		plugin.Log.Infof("Querying %s", deviceClient.DeviceUrl.Redacted())
	}
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
	tags["fritz_device"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["fritz_service"] = serviceClient.Service.ShortId()
	fields := make(map[string]interface{})
	fields["uptime"] = info.NewUpTime
	fields["model_name"] = info.NewModelName
	fields["serial_number"] = info.NewSerialNumber
	fields["hardware_version"] = info.NewHardwareVersion
	fields["software_version"] = info.NewSoftwareVersion
	acc.AddFields("fritzbox_device", fields, tags)
	if plugin.Debug {
		plugin.logMetric("fritzbox_device", tags, fields)
	}
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
	// Use public IGD service instead of the WANCommonInfo one for total bytes stats, because IGD supports uint64 counters
	igdServices, err := deviceClient.ServicesByType(tr064.IgdServiceSpec, "WANCommonInterfaceConfig")
	if err != nil {
		return err
	}
	if len(igdServices) == 0 {
		return fmt.Errorf("")
	}
	igdServiceClient := &igdicfg.ServiceClient{
		TR064Client: deviceClient,
		Service:     igdServices[0],
	}
	addonInfos := &igdicfg.GetAddonInfosResponse{}
	err = igdServiceClient.GetAddonInfos(addonInfos)
	if err != nil {
		return err
	}
	strconv.ParseUint(addonInfos.NewX_AVM_DE_TotalBytesSent64, 10, 64)
	totalBytesSent, err := strconv.ParseUint(addonInfos.NewX_AVM_DE_TotalBytesSent64, 10, 64)
	if err != nil {
		return err
	}
	totalBytesReceived, err := strconv.ParseUint(addonInfos.NewX_AVM_DE_TotalBytesReceived64, 10, 64)
	if err != nil {
		return err
	}
	tags := make(map[string]string)
	tags["fritz_device"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["fritz_service"] = serviceClient.Service.ShortId()
	fields := make(map[string]interface{})
	fields["layer1_upstream_max_bit_rate"] = commonLinkProperties.NewLayer1UpstreamMaxBitRate
	fields["layer1_downstream_max_bit_rate"] = commonLinkProperties.NewLayer1DownstreamMaxBitRate
	fields["upstream_current_max_speed"] = commonLinkProperties.NewX_AVM_DE_UpstreamCurrentMaxSpeed
	fields["downstream_current_max_speed"] = commonLinkProperties.NewX_AVM_DE_DownstreamCurrentMaxSpeed
	fields["total_bytes_sent"] = totalBytesSent
	fields["total_bytes_received"] = totalBytesReceived
	acc.AddFields("fritzbox_wan", fields, tags)
	if plugin.Debug {
		plugin.logMetric("fritzbox_wan", tags, fields)
	}
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
	tags["fritz_device"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["fritz_service"] = serviceClient.Service.ShortId()
	fields := make(map[string]interface{})
	fields["uptime"] = info.NewUptime
	fields["upstream_max_bit_rate"] = info.NewUpstreamMaxBitRate
	fields["downstream_max_bit_rate"] = info.NewDownstreamMaxBitRate
	acc.AddFields("fritzbox_ppp", fields, tags)
	if plugin.Debug {
		plugin.logMetric("fritzbox_ppp", tags, fields)
	}
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
	if info.NewStatus != "Up" {
		return nil
	}
	statisticsTotal := &wandslifconfig.GetStatisticsTotalResponse{}
	err = serviceClient.GetStatisticsTotal(statisticsTotal)
	if err != nil {
		return err
	}
	tags := make(map[string]string)
	tags["fritz_device"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["fritz_service"] = serviceClient.Service.ShortId()
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
	if plugin.Debug {
		plugin.logMetric("fritzbox_dsl", tags, fields)
	}
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
	if info.NewStatus != "Up" {
		return nil
	}
	totalAssociations := &wlanconfig.GetTotalAssociationsResponse{}
	err = serviceClient.GetTotalAssociations(totalAssociations)
	if err != nil {
		return err
	}
	tags := make(map[string]string)
	tags["fritz_device"] = serviceClient.TR064Client.DeviceUrl.Hostname()
	tags["fritz_service"] = serviceClient.Service.ShortId()
	tags["fritz_wlan"] = info.NewSSID
	tags["fritz_wlan_channel"] = strconv.Itoa(int(info.NewChannel))
	tags["fritz_wlan_band"] = plugin.wlanBandFromInfo(info)
	fields := make(map[string]interface{})
	fields["total_associations"] = totalAssociations.NewTotalAssociations
	acc.AddGauge("fritzbox_wlan", fields, tags)
	if plugin.Debug {
		plugin.logMetric("fritzbox_wlan", tags, fields)
	}
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
	if plugin.queryCycleCounter != 0 {
		return nil
	}
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
		tags["fritz_device"] = serviceClient.TR064Client.DeviceUrl.Hostname()
		tags["fritz_service"] = serviceClient.Service.ShortId()
		tags["fritz_host"] = connection.RightDeviceName
		tags["fritz_host_role"] = plugin.hostRole(connection.RightMeshRole)
		tags["fritz_host_peer"] = connection.LeftDeviceName
		tags["fritz_host_peer_role"] = plugin.hostRole(connection.LeftMeshRole)
		tags["fritz_link_type"] = connection.InterfaceType
		tags["fritz_link_name"] = connection.InterfaceName
		fields := make(map[string]interface{})
		fields["max_data_rate_tx"] = connection.MaxDataRateTx
		fields["max_data_rate_rx"] = connection.MaxDataRateRx
		fields["cur_data_rate_tx"] = connection.CurDataRateTx
		fields["cur_data_rate_rx"] = connection.CurDataRateRx
		acc.AddGauge("fritzbox_host", fields, tags)
		if plugin.Debug {
			plugin.logMetric("fritzbox_host", tags, fields)
		}
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
		return nil, fmt.Errorf("failed to fetch mesh list '%s' (status: %s)", meshListPath.NewX_AVM_DE_MeshListPath, meshListResponse.Status)
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

func (plugin *Fritzbox) logMetric(name string, tags map[string]string, fields map[string]interface{}) {
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
		return defaultFritzbox()
	})
}
