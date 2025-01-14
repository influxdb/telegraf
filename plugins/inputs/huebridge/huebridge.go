//go:generate ../../../tools/readme_config_includer/generator
package huebridge

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/rs/zerolog"
	"github.com/tdrn-org/go-hue"
	"github.com/tdrn-org/go-log"
)

//go:embed sample.conf
var sampleConfig string

const pluginName = "huebridge"

const defaultTimeout = config.Duration(10 * time.Second)

const unrecoverableError = "unrecoverable error:"
const unrecoverableErrorPattern = unrecoverableError + " %w"

func wrapUnrecoverableError(err error) error {
	return fmt.Errorf(unrecoverableErrorPattern, err)
}

func unwrapUnrecoverableError(err error) (error, bool) {
	if err != nil && strings.HasPrefix(err.Error(), unrecoverableError) {
		return errors.Unwrap(err), true
	}
	return err, false
}

type HueBridge struct {
	Bridges                  []string        `toml:"bridges"`
	CloudInsecureSkipVerify  bool            `toml:"cloud_insecure_skip_verify"`
	RemoteClientId           string          `toml:"remote_client_id"`
	RemoteClientSecret       string          `toml:"remote_client_secret"`
	RemoteCallbackUrl        string          `toml:"remote_callback_url"`
	RemoteTokenDir           string          `toml:"remote_token_dir"`
	RoomAssignments          [][]string      `toml:"room_assignments"`
	RemoteInsecureSkipVerify bool            `toml:"remote_insecure_skip_verify"`
	Timeout                  config.Duration `toml:"timeout"`
	Debug                    bool            `toml:"debug"`
	Log                      telegraf.Logger `toml:"-"`
	resolvedBridges          map[*url.URL]hue.BridgeClient
}

func defaultHueBridge() *HueBridge {
	return &HueBridge{
		Bridges:         make([]string, 0),
		RoomAssignments: make([][]string, 0),
		Timeout:         defaultTimeout,
		resolvedBridges: make(map[*url.URL]hue.BridgeClient),
	}
}

func (*HueBridge) SampleConfig() string {
	return sampleConfig
}

func (plugin *HueBridge) Init() error {
	if plugin.Log == nil {
		plugin.Log = logger.New("inputs", pluginName, "")
	}
	if plugin.Debug {
		log.SetLevel(zerolog.DebugLevel)
	}
	for _, bridge := range plugin.Bridges {
		bridgeUrl, err := url.Parse(bridge)
		if err != nil {
			return err
		}
		err = plugin.initBridge(bridgeUrl)
		if err != nil {
			return err
		}
	}
	return nil
}

func (plugin *HueBridge) initBridge(bridgeUrl *url.URL) error {
	bridgeClient, err := plugin.resolveBridge(bridgeUrl)
	err, unrecoverable := unwrapUnrecoverableError(err)
	if unrecoverable {
		return err
	} else if err != nil {
		plugin.Log.Warnf("Unable to resolve bridge URL '%s' (reason: %s)", bridgeUrl, err)
	}
	plugin.resolvedBridges[bridgeUrl] = bridgeClient
	return nil
}

func (plugin *HueBridge) resolveBridge(bridgeUrl *url.URL) (hue.BridgeClient, error) {
	switch bridgeUrl.Scheme {
	case "address":
		return plugin.resolveBridgeViaAddress(bridgeUrl)
	case "cloud":
		return plugin.resolveBridgeViaCloud(bridgeUrl)
	case "mdns":
		return plugin.resolveBridgeViaMDNS(bridgeUrl)
	case "remote":
		return plugin.resolveBridgeViaRemote(bridgeUrl)
	}
	return nil, wrapUnrecoverableError(fmt.Errorf("unrecognized bridge URL '%s'", bridgeUrl))
}

func (plugin *HueBridge) resolveBridgeViaAddress(bridgeUrl *url.URL) (hue.BridgeClient, error) {
	locator, err := hue.NewAddressBridgeLocator(bridgeUrl.Host)
	if err != nil {
		return nil, wrapUnrecoverableError(err)
	}
	return plugin.resolveLocalBridge(bridgeUrl, locator)
}

func (plugin *HueBridge) resolveBridgeViaCloud(bridgeUrl *url.URL) (hue.BridgeClient, error) {
	locator := hue.NewCloudBridgeLocator()
	if bridgeUrl.Host != "" {
		discoveryEndpointUrl, err := url.Parse(fmt.Sprintf("https://%s/", bridgeUrl.Host))
		if err != nil {
			return nil, wrapUnrecoverableError(err)
		}
		discoveryEndpointUrl = discoveryEndpointUrl.JoinPath(bridgeUrl.Path)
		locator.DiscoveryEndpointUrl = discoveryEndpointUrl
	}
	locator.InsecureSkipVerify = plugin.CloudInsecureSkipVerify
	return plugin.resolveLocalBridge(bridgeUrl, locator)
}

func (plugin *HueBridge) resolveBridgeViaMDNS(bridgeUrl *url.URL) (hue.BridgeClient, error) {
	locator := hue.NewMDNSBridgeLocator()
	locator.Limit = 1
	return plugin.resolveLocalBridge(bridgeUrl, locator)
}

func (plugin *HueBridge) resolveLocalBridge(bridgeUrl *url.URL, locator hue.BridgeLocator) (hue.BridgeClient, error) {
	bridge, err := locator.Lookup(bridgeUrl.User.Username(), time.Duration(plugin.Timeout))
	if err != nil {
		return nil, err
	}
	bridgeUrlPassword, set := bridgeUrl.User.Password()
	if !set {
		return nil, wrapUnrecoverableError(fmt.Errorf("no password set in bridge URL '%s'", bridgeUrl))
	}
	return bridge.NewClient(hue.NewLocalBridgeAuthenticator(bridgeUrlPassword), time.Duration(plugin.Timeout))
}

func (plugin *HueBridge) resolveBridgeViaRemote(bridgeUrl *url.URL) (hue.BridgeClient, error) {
	if plugin.RemoteClientId == "" || plugin.RemoteClientSecret == "" || plugin.RemoteTokenDir == "" {
		return nil, fmt.Errorf("remote application credentials and/or token director not set")
	}
	var redirectUrl *url.URL
	if plugin.RemoteCallbackUrl != "" {
		parsedRedirectUrl, err := url.Parse(plugin.RemoteCallbackUrl)
		if err != nil {
			return nil, err
		}
		redirectUrl = parsedRedirectUrl
	}
	tokenFile := filepath.Join(plugin.RemoteTokenDir, plugin.RemoteClientId, strings.ToUpper(bridgeUrl.User.Username())+".json")
	locator, err := hue.NewRemoteBridgeLocator(plugin.RemoteClientId, plugin.RemoteClientSecret, redirectUrl, tokenFile)
	if err != nil {
		return nil, err
	}
	if bridgeUrl.Host != "" {
		endpointUrl, err := url.Parse(fmt.Sprintf("https://%s/", bridgeUrl.Host))
		if err != nil {
			return nil, wrapUnrecoverableError(err)
		}
		endpointUrl = endpointUrl.JoinPath(bridgeUrl.Path)
		locator.EndpointUrl = endpointUrl
	}
	locator.InsecureSkipVerify = plugin.RemoteInsecureSkipVerify
	return plugin.resolveRemoteBridge(bridgeUrl, locator)
}

func (plugin *HueBridge) resolveRemoteBridge(bridgeUrl *url.URL, locator *hue.RemoteBridgeLocator) (hue.BridgeClient, error) {
	bridge, err := locator.Lookup(bridgeUrl.User.Username(), time.Duration(plugin.Timeout))
	if err != nil {
		return nil, err
	}
	bridgeUrlPassword, set := bridgeUrl.User.Password()
	if !set {
		return nil, wrapUnrecoverableError(fmt.Errorf("no password set in bridge URL '%s'", bridgeUrl))
	}
	return bridge.NewClient(hue.NewRemoteBridgeAuthenticator(locator, bridgeUrlPassword), time.Duration(plugin.Timeout))
}

func (plugin *HueBridge) Gather(acc telegraf.Accumulator) error {
	for bridgeUrl, bridgeClient := range plugin.resolvedBridges {
		retry := false
		if bridgeClient == nil {
			if plugin.Debug {
				plugin.Log.Infof("Re-resolving bridge %s...", bridgeUrl.User.Username())
			}
			resolvedBridgeClient, err := plugin.resolveBridge(bridgeUrl)
			if err != nil {
				plugin.Log.Warnf("Failed to resolve bridge %s (reason %s)", bridgeUrl.User.Username(), err)
				continue
			}
			plugin.resolvedBridges[bridgeUrl] = resolvedBridgeClient
			bridgeClient = resolvedBridgeClient
			retry = true
		}
		if bridgeClient != nil {
			err := plugin.processBridge(acc, bridgeClient)
			if err != nil && !retry {
				plugin.resolvedBridges[bridgeUrl] = nil
				acc.AddError(err)
			}
		}
	}
	return nil
}

func (plugin *HueBridge) processBridge(acc telegraf.Accumulator, bridgeClient hue.BridgeClient) error {
	if plugin.Debug {
		plugin.Log.Infof("Processing bridge %s", bridgeClient.Bridge().BridgeId)
	}
	metadata, err := plugin.fetchMetadata(bridgeClient)
	if err != nil {
		return err
	}
	err = plugin.processLights(acc, bridgeClient, metadata)
	if err != nil {
		acc.AddError(err)
	}
	err = plugin.processTemperatures(acc, bridgeClient, metadata)
	if err != nil {
		acc.AddError(err)
	}
	err = plugin.processLightLevels(acc, bridgeClient, metadata)
	if err != nil {
		acc.AddError(err)
	}
	err = plugin.processMotionSensors(acc, bridgeClient, metadata)
	if err != nil {
		acc.AddError(err)
	}
	err = plugin.processDevicePowers(acc, bridgeClient, metadata)
	if err != nil {
		acc.AddError(err)
	}
	return nil
}

type bridgeMetadata struct {
	resourceTree    map[string]string
	deviceNames     map[string]string
	roomAssignments map[string]string
}

func (metadata *bridgeMetadata) resolveResourceRoom(resourceId string, resourceName string) string {
	roomName := metadata.roomAssignments[resourceName]
	if roomName == "" {
		resourceOwnerId := resourceId
		for {
			roomName = metadata.roomAssignments[resourceOwnerId]
			if roomName != "" {
				break
			}
			resourceOwnerId = metadata.resourceTree[resourceOwnerId]
			if resourceOwnerId == "" {
				break
			}
		}
	}
	if roomName == "" {
		roomName = "<unassigned>"
	}
	return roomName
}

func (metadata *bridgeMetadata) resolveDeviceName(resourceId string) string {
	deviceName := ""
	resourceOwnerId := resourceId
	for {
		deviceName = metadata.deviceNames[resourceOwnerId]
		if deviceName != "" {
			break
		}
		resourceOwnerId = metadata.resourceTree[resourceOwnerId]
		if resourceOwnerId == "" {
			break
		}
	}
	if deviceName == "" {
		deviceName = "<undefined>"
	}
	return deviceName
}

func (plugin *HueBridge) fetchMetadata(bridgeClient hue.BridgeClient) (*bridgeMetadata, error) {
	resourceTree, err := plugin.fetchResourceTree(bridgeClient)
	if err != nil {
		return nil, err
	}
	deviceNames, err := plugin.fetchDeviceNames(bridgeClient)
	if err != nil {
		return nil, err
	}
	roomAssignments, err := plugin.fetchRoomAssignments(bridgeClient)
	if err != nil {
		return nil, err
	}
	for _, manualRoomAssignment := range plugin.RoomAssignments {
		if len(manualRoomAssignment) != 2 {
			plugin.Log.Warnf("Ignoring invalid room assignment %v", manualRoomAssignment)
			continue
		}
		roomAssignments[manualRoomAssignment[0]] = manualRoomAssignment[1]
	}
	return &bridgeMetadata{resourceTree: resourceTree, deviceNames: deviceNames, roomAssignments: roomAssignments}, nil
}

func (plugin *HueBridge) fetchResourceTree(bridgeClient hue.BridgeClient) (map[string]string, error) {
	getResourcesResponse, err := bridgeClient.GetResources()
	if err != nil {
		return nil, err
	}
	if getResourcesResponse.HTTPResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch resources (status: %s)", getResourcesResponse.HTTPResponse.Status)
	}
	tree := make(map[string]string)
	responseData := getResourcesResponse.JSON200.Data
	if responseData != nil {
		for _, resource := range *responseData {
			resourceId := *resource.Id
			resourceOwnerId := ""
			resourceOwner := resource.Owner
			if resourceOwner != nil {
				resourceOwnerId = *resourceOwner.Rid
				tree[resourceId] = resourceOwnerId
			}
		}
	}
	return tree, nil
}

func (plugin *HueBridge) fetchDeviceNames(bridgeClient hue.BridgeClient) (map[string]string, error) {
	getDevicesResponse, err := bridgeClient.GetDevices()
	if err != nil {
		return nil, err
	}
	if getDevicesResponse.HTTPResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch devices (status: %s)", getDevicesResponse.HTTPResponse.Status)
	}
	names := make(map[string]string)
	responseData := getDevicesResponse.JSON200.Data
	if responseData != nil {
		for _, device := range *responseData {
			deviceId := *device.Id
			deviceName := *device.Metadata.Name
			names[deviceId] = deviceName
		}
	}
	return names, nil
}

func (plugin *HueBridge) fetchRoomAssignments(bridgeClient hue.BridgeClient) (map[string]string, error) {
	getRoomsResponse, err := bridgeClient.GetRooms()
	if err != nil {
		return nil, err
	}
	if getRoomsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch rooms (status: %s)", getRoomsResponse.HTTPResponse.Status)
	}
	assignments := make(map[string]string)
	responseData := getRoomsResponse.JSON200.Data
	if responseData != nil {
		for _, roomGet := range *responseData {
			roomName := *roomGet.Metadata.Name
			for _, children := range *roomGet.Children {
				childId := *children.Rid
				assignments[childId] = roomName
			}
		}
	}
	return assignments, nil
}

func (plugin *HueBridge) processLights(acc telegraf.Accumulator, bridgeClient hue.BridgeClient, metadata *bridgeMetadata) error {
	getLightsResponse, err := bridgeClient.GetLights()
	if err != nil {
		return err
	}
	if getLightsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch lights (status: %s)", getLightsResponse.HTTPResponse.Status)
	}
	responseData := (*getLightsResponse.JSON200).Data
	if responseData != nil {
		for _, light := range *responseData {
			tags := make(map[string]string)
			tags["huebridge_bridge_id"] = bridgeClient.Bridge().BridgeId
			tags["huebridge_room"] = metadata.resolveResourceRoom(*light.Id, *light.Metadata.Name)
			tags["huebridge_device"] = *light.Metadata.Name
			fields := make(map[string]interface{})
			if *light.On.On {
				fields["on"] = 1
			} else {
				fields["on"] = 0
			}
			acc.AddGauge("huebridge_light", fields, tags)
			if plugin.Debug {
				plugin.logMetric("huebridge_light", tags, fields)
			}
		}
	}
	return nil
}

func (plugin *HueBridge) processTemperatures(acc telegraf.Accumulator, bridgeClient hue.BridgeClient, metadata *bridgeMetadata) error {
	getTemperaturesResponse, err := bridgeClient.GetTemperatures()
	if err != nil {
		return err
	}
	if getTemperaturesResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge temperatures (status: %s)", getTemperaturesResponse.HTTPResponse.Status)
	}
	responseData := (*getTemperaturesResponse.JSON200).Data
	if responseData != nil {
		for _, temperature := range *responseData {
			temperatureName := metadata.resolveDeviceName(*temperature.Id)
			tags := make(map[string]string)
			tags["huebridge_bridge_id"] = bridgeClient.Bridge().BridgeId
			tags["huebridge_room"] = metadata.resolveResourceRoom(*temperature.Id, temperatureName)
			tags["huebridge_device"] = temperatureName
			tags["huebridge_device_enabled"] = strconv.FormatBool(*temperature.Enabled)
			fields := make(map[string]interface{})
			fields["temperature"] = *temperature.Temperature.TemperatureReport.Temperature
			acc.AddGauge("huebridge_temperature", fields, tags)
			if plugin.Debug {
				plugin.logMetric("huebridge_temperature", tags, fields)
			}
		}
	}
	return nil
}

func (plugin *HueBridge) processLightLevels(acc telegraf.Accumulator, bridgeClient hue.BridgeClient, metadata *bridgeMetadata) error {
	getLightLevelsResponse, err := bridgeClient.GetLightLevels()
	if err != nil {
		return err
	}
	if getLightLevelsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch light levels (status: %s)", getLightLevelsResponse.HTTPResponse.Status)
	}
	responseData := (*getLightLevelsResponse.JSON200).Data
	if responseData != nil {
		for _, lightLevel := range *responseData {
			lightLevelName := metadata.resolveDeviceName(*lightLevel.Id)
			tags := make(map[string]string)
			tags["huebridge_bridge_id"] = bridgeClient.Bridge().BridgeId
			tags["huebridge_room"] = metadata.resolveResourceRoom(*lightLevel.Id, lightLevelName)
			tags["huebridge_device"] = lightLevelName
			tags["huebridge_device_enabled"] = strconv.FormatBool(*lightLevel.Enabled)
			fields := make(map[string]interface{})
			fields["light_level"] = *lightLevel.Light.LightLevelReport.LightLevel
			fields["light_level_lux"] = math.Pow(10.0, (float64(*lightLevel.Light.LightLevelReport.LightLevel)-1.0)/10000.0)
			acc.AddGauge("huebridge_light_level", fields, tags)
			if plugin.Debug {
				plugin.logMetric("huebridge_light_level", tags, fields)
			}
		}
	}
	return nil
}

func (plugin *HueBridge) processMotionSensors(acc telegraf.Accumulator, bridgeClient hue.BridgeClient, metadata *bridgeMetadata) error {
	getMotionSensorsResponse, err := bridgeClient.GetMotionSensors()
	if err != nil {
		return err
	}
	if getMotionSensorsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch motion sensors (status: %s)", getMotionSensorsResponse.HTTPResponse.Status)
	}
	responseData := (*getMotionSensorsResponse.JSON200).Data
	if responseData != nil {
		for _, motionSensor := range *responseData {
			motionSensorName := metadata.resolveDeviceName(*motionSensor.Id)
			tags := make(map[string]string)
			tags["huebridge_bridge_id"] = bridgeClient.Bridge().BridgeId
			tags["huebridge_room"] = metadata.resolveResourceRoom(*motionSensor.Id, motionSensorName)
			tags["huebridge_device"] = motionSensorName
			tags["huebridge_device_enabled"] = strconv.FormatBool(*motionSensor.Enabled)
			fields := make(map[string]interface{})
			if *motionSensor.Motion.MotionReport.Motion {
				fields["motion"] = 1
			} else {
				fields["motion"] = 0
			}
			acc.AddGauge("huebridge_motion_sensor", fields, tags)
			if plugin.Debug {
				plugin.logMetric("huebridge_motion_sensor", tags, fields)
			}
		}
	}
	return nil
}

func (plugin *HueBridge) processDevicePowers(acc telegraf.Accumulator, bridgeClient hue.BridgeClient, metadata *bridgeMetadata) error {
	getDevicePowersResponse, err := bridgeClient.GetDevicePowers()
	if err != nil {
		return err
	}
	if getDevicePowersResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch motion sensors (status: %s)", getDevicePowersResponse.HTTPResponse.Status)
	}
	responseData := (*getDevicePowersResponse.JSON200).Data
	if responseData != nil {
		for _, devicePower := range *responseData {
			if devicePower.PowerState.BatteryLevel == nil && devicePower.PowerState.BatteryState == nil {
				continue
			}
			devicePowerName := metadata.resolveDeviceName(*devicePower.Id)
			tags := make(map[string]string)
			tags["huebridge_bridge_id"] = bridgeClient.Bridge().BridgeId
			tags["huebridge_room"] = metadata.resolveResourceRoom(*devicePower.Id, devicePowerName)
			tags["huebridge_device"] = devicePowerName
			fields := make(map[string]interface{})
			fields["battery_level"] = *devicePower.PowerState.BatteryLevel
			fields["battery_state"] = *devicePower.PowerState.BatteryState
			acc.AddGauge("huebridge_device_power", fields, tags)
			if plugin.Debug {
				plugin.logMetric("huebridge_device_power", tags, fields)
			}
		}
	}
	return nil
}

func (plugin *HueBridge) logMetric(name string, tags map[string]string, fields map[string]interface{}) {
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
		return defaultHueBridge()
	})
}
