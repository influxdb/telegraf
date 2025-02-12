package huebridge

import (
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/tdrn-org/go-hue"
)

type bridge struct {
	url                   *url.URL
	configRoomAssignments map[string]string
	rcc                   *RemoteClientConfig
	tcc                   *tls.ClientConfig
	timeout               config.Duration
	log                   telegraf.Logger
	resolvedClient        hue.BridgeClient
	resourceTree          map[string]string
	deviceNames           map[string]string
	roomAssignments       map[string]string
}

func newBridge(rawUrl string, roomAssignments map[string]string, rcc *RemoteClientConfig, tcc *tls.ClientConfig, timeout config.Duration, log telegraf.Logger) (*bridge, error) {
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bridge URL %s: %w", rawUrl, err)
	}
	switch parsedUrl.Scheme {
	case "address", "cloud", "mdns", "remote":
		// Do nothing, those are valid
	default:
		return nil, fmt.Errorf("unrecognized scheme %s in URL %s", parsedUrl.Scheme, parsedUrl)
	}
	// All schemes require a password in the URL
	_, passwordSet := parsedUrl.User.Password()
	if !passwordSet {
		return nil, fmt.Errorf("missing password in URL %s", parsedUrl)
	}
	// Remote scheme also requires a configured rcc
	if parsedUrl.Scheme == "remote" {
		if rcc.RemoteClientId == "" || rcc.RemoteClientSecret == "" || rcc.RemoteTokenDir == "" {
			return nil, fmt.Errorf("missing remote application credentials and/or token director not configured")
		}
	}
	return &bridge{
		url:                   parsedUrl,
		configRoomAssignments: roomAssignments,
		rcc:                   rcc,
		tcc:                   tcc,
		timeout:               timeout,
		log:                   log,
	}, nil
}

func (b *bridge) String() string {
	return b.url.Redacted()
}

func (b *bridge) process(acc telegraf.Accumulator) error {
	if b.resolvedClient == nil {
		if err := b.resolve(); err != nil {
			return err
		}
	}
	b.log.Tracef("Processing bridge %s", b)
	err := b.fetchMetadata()
	if err != nil {
		// Discard previously resolved client and re-resolve on next process call
		b.resolvedClient = nil
		return err
	}
	acc.AddError(b.processLights(acc))
	acc.AddError(b.processTemperatures(acc))
	acc.AddError(b.processLightLevels(acc))
	acc.AddError(b.processMotionSensors(acc))
	acc.AddError(b.processDevicePowers(acc))
	return nil
}

func (b *bridge) processLights(acc telegraf.Accumulator) error {
	getLightsResponse, err := b.resolvedClient.GetLights()
	if err != nil {
		return fmt.Errorf("failed to access bridge lights on %s: %w", b, err)
	}
	if getLightsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge lights from %s: %s", b, getLightsResponse.HTTPResponse.Status)
	}
	responseData := getLightsResponse.JSON200.Data
	if responseData != nil {
		for _, light := range *responseData {
			tags := make(map[string]string)
			tags["bridge_id"] = b.resolvedClient.Bridge().BridgeId
			tags["room"] = b.resolveResourceRoom(*light.Id, *light.Metadata.Name)
			tags["device"] = *light.Metadata.Name
			fields := make(map[string]interface{})
			if *light.On.On {
				fields["on"] = 1
			} else {
				fields["on"] = 0
			}
			acc.AddGauge("huebridge_light", fields, tags)
		}
	}
	return nil
}

func (b *bridge) processTemperatures(acc telegraf.Accumulator) error {
	getTemperaturesResponse, err := b.resolvedClient.GetTemperatures()
	if err != nil {
		return fmt.Errorf("failed to access bridge temperatures on %s: %w", b, err)
	}
	if getTemperaturesResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge temperatures from %s: %s", b, getTemperaturesResponse.HTTPResponse.Status)
	}
	responseData := getTemperaturesResponse.JSON200.Data
	if responseData != nil {
		for _, temperature := range *responseData {
			temperatureName := b.resolveDeviceName(*temperature.Id)
			tags := make(map[string]string)
			tags["bridge_id"] = b.resolvedClient.Bridge().BridgeId
			tags["room"] = b.resolveResourceRoom(*temperature.Id, temperatureName)
			tags["device"] = temperatureName
			tags["enabled"] = strconv.FormatBool(*temperature.Enabled)
			fields := make(map[string]interface{})
			fields["temperature"] = *temperature.Temperature.TemperatureReport.Temperature
			acc.AddGauge("huebridge_temperature", fields, tags)
		}
	}
	return nil
}

func (b *bridge) processLightLevels(acc telegraf.Accumulator) error {
	getLightLevelsResponse, err := b.resolvedClient.GetLightLevels()
	if err != nil {
		return fmt.Errorf("failed to access bridge lights levels on %s: %w", b, err)
	}
	if getLightLevelsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge light levels from %s: %s", b, getLightLevelsResponse.HTTPResponse.Status)
	}
	responseData := getLightLevelsResponse.JSON200.Data
	if responseData != nil {
		for _, lightLevel := range *responseData {
			lightLevelName := b.resolveDeviceName(*lightLevel.Id)
			tags := make(map[string]string)
			tags["bridge_id"] = b.resolvedClient.Bridge().BridgeId
			tags["room"] = b.resolveResourceRoom(*lightLevel.Id, lightLevelName)
			tags["device"] = lightLevelName
			tags["enabled"] = strconv.FormatBool(*lightLevel.Enabled)
			fields := make(map[string]interface{})
			fields["light_level"] = *lightLevel.Light.LightLevelReport.LightLevel
			fields["light_level_lux"] = math.Pow(10.0, (float64(*lightLevel.Light.LightLevelReport.LightLevel)-1.0)/10000.0)
			acc.AddGauge("huebridge_light_level", fields, tags)
		}
	}
	return nil
}

func (b *bridge) processMotionSensors(acc telegraf.Accumulator) error {
	getMotionSensorsResponse, err := b.resolvedClient.GetMotionSensors()
	if err != nil {
		return fmt.Errorf("failed to access bridge motion sensors on %s: %w", b, err)
	}
	if getMotionSensorsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge motion sensors from %s: %s", b, getMotionSensorsResponse.HTTPResponse.Status)
	}
	responseData := getMotionSensorsResponse.JSON200.Data
	if responseData != nil {
		for _, motionSensor := range *responseData {
			motionSensorName := b.resolveDeviceName(*motionSensor.Id)
			tags := make(map[string]string)
			tags["bridge_id"] = b.resolvedClient.Bridge().BridgeId
			tags["room"] = b.resolveResourceRoom(*motionSensor.Id, motionSensorName)
			tags["device"] = motionSensorName
			tags["enabled"] = strconv.FormatBool(*motionSensor.Enabled)
			fields := make(map[string]interface{})
			if *motionSensor.Motion.MotionReport.Motion {
				fields["motion"] = 1
			} else {
				fields["motion"] = 0
			}
			acc.AddGauge("huebridge_motion_sensor", fields, tags)
		}
	}
	return nil
}

func (b *bridge) processDevicePowers(acc telegraf.Accumulator) error {
	getDevicePowersResponse, err := b.resolvedClient.GetDevicePowers()
	if err != nil {
		return fmt.Errorf("failed to access bridge device powers on %s: %w", b, err)
	}
	if getDevicePowersResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge device powers from %s: %s", b, getDevicePowersResponse.HTTPResponse.Status)
	}
	responseData := getDevicePowersResponse.JSON200.Data
	if responseData != nil {
		for _, devicePower := range *responseData {
			if devicePower.PowerState.BatteryLevel == nil && devicePower.PowerState.BatteryState == nil {
				continue
			}
			devicePowerName := b.resolveDeviceName(*devicePower.Id)
			tags := make(map[string]string)
			tags["bridge_id"] = b.resolvedClient.Bridge().BridgeId
			tags["room"] = b.resolveResourceRoom(*devicePower.Id, devicePowerName)
			tags["device"] = devicePowerName
			fields := make(map[string]interface{})
			fields["battery_level"] = *devicePower.PowerState.BatteryLevel
			fields["battery_state"] = *devicePower.PowerState.BatteryState
			acc.AddGauge("huebridge_device_power", fields, tags)
		}
	}
	return nil
}

func (b *bridge) resolve() error {
	if b.resolvedClient != nil {
		return nil
	}
	switch b.url.Scheme {
	case "address":
		return b.resolveViaAddress()
	case "cloud":
		return b.resolveViaCloud()
	case "mdns":
		return b.resolveViaMDNS()
	case "remote":
		return b.resolveViaRemote()
	}
	return fmt.Errorf("unrecognized bridge URL %s", b)
}

func (b *bridge) resolveViaAddress() error {
	locator, err := hue.NewAddressBridgeLocator(b.url.Host)
	if err != nil {
		return err
	}
	return b.resolveLocalBridge(locator)
}

func (b *bridge) resolveViaCloud() error {
	locator := hue.NewCloudBridgeLocator()
	if b.url.Host != "" {
		discoveryEndpointUrl, err := url.Parse(fmt.Sprintf("https://%s/", b.url.Host))
		if err != nil {
			return err
		}
		discoveryEndpointUrl = discoveryEndpointUrl.JoinPath(b.url.Path)
		locator.DiscoveryEndpointUrl = discoveryEndpointUrl
	}
	tlsConfig, err := b.tcc.TLSConfig()
	if err != nil {
		return err
	}
	locator.TlsConfig = tlsConfig
	return b.resolveLocalBridge(locator)
}

func (b *bridge) resolveViaMDNS() error {
	locator := hue.NewMDNSBridgeLocator()
	return b.resolveLocalBridge(locator)
}

func (b *bridge) resolveLocalBridge(locator hue.BridgeLocator) error {
	hueBridge, err := locator.Lookup(b.url.User.Username(), time.Duration(b.timeout))
	if err != nil {
		return err
	}
	urlPassword, _ := b.url.User.Password()
	bridgeClient, err := hueBridge.NewClient(hue.NewLocalBridgeAuthenticator(urlPassword), time.Duration(b.timeout))
	if err != nil {
		return err
	}
	b.resolvedClient = bridgeClient
	return nil
}

func (b *bridge) resolveViaRemote() error {
	var redirectUrl *url.URL
	if b.rcc.RemoteCallbackUrl != "" {
		parsedRedirectUrl, err := url.Parse(b.rcc.RemoteCallbackUrl)
		if err != nil {
			return err
		}
		redirectUrl = parsedRedirectUrl
	}
	tokenFile := filepath.Join(b.rcc.RemoteTokenDir, b.rcc.RemoteClientId, strings.ToUpper(b.url.User.Username())+".json")
	locator, err := hue.NewRemoteBridgeLocator(b.rcc.RemoteClientId, b.rcc.RemoteClientSecret, redirectUrl, tokenFile)
	if err != nil {
		return err
	}
	if b.url.Host != "" {
		endpointUrl, err := url.Parse(fmt.Sprintf("https://%s/", b.url.Host))
		if err != nil {
			return err
		}
		endpointUrl = endpointUrl.JoinPath(b.url.Path)
		locator.EndpointUrl = endpointUrl
	}
	tlsConfig, err := b.tcc.TLSConfig()
	if err != nil {
		return err
	}
	locator.TlsConfig = tlsConfig.Clone()
	return b.resolveRemoteBridge(locator)
}

func (b *bridge) resolveRemoteBridge(locator *hue.RemoteBridgeLocator) error {
	hueBridge, err := locator.Lookup(b.url.User.Username(), time.Duration(b.timeout))
	if err != nil {
		return err
	}
	urlPassword, _ := b.url.User.Password()
	bridgeClient, err := hueBridge.NewClient(hue.NewRemoteBridgeAuthenticator(locator, urlPassword), time.Duration(b.timeout))
	if err != nil {
		return err
	}
	b.resolvedClient = bridgeClient
	return nil
}

func (b *bridge) fetchMetadata() error {
	err := b.fetchResourceTree()
	if err != nil {
		return err
	}
	err = b.fetchDeviceNames()
	if err != nil {
		return err
	}
	return b.fetchRoomAssignments()
}

func (b *bridge) fetchResourceTree() error {
	getResourcesResponse, err := b.resolvedClient.GetResources()
	if err != nil {
		return fmt.Errorf("failed to access bridge resources on %s: %w", b, err)
	}
	if getResourcesResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge resources from %s: %s", b, getResourcesResponse.HTTPResponse.Status)
	}
	responseData := getResourcesResponse.JSON200.Data
	if responseData == nil {
		b.resourceTree = make(map[string]string)
		return nil
	}
	b.resourceTree = make(map[string]string, len(*responseData))
	for _, resource := range *responseData {
		if resource.Owner != nil {
			b.resourceTree[*resource.Id] = *resource.Owner.Rid
		}
	}
	return nil
}

func (b *bridge) fetchDeviceNames() error {
	getDevicesResponse, err := b.resolvedClient.GetDevices()
	if err != nil {
		return fmt.Errorf("failed to access bridge devices on %s: %w", b, err)
	}
	if getDevicesResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge devices from %s: %s", b, getDevicesResponse.HTTPResponse.Status)
	}
	responseData := getDevicesResponse.JSON200.Data
	if responseData == nil {
		b.deviceNames = make(map[string]string)
		return nil
	}
	b.deviceNames = make(map[string]string, len(*responseData))
	for _, device := range *responseData {
		b.deviceNames[*device.Id] = *device.Metadata.Name
	}
	return nil
}

func (b *bridge) fetchRoomAssignments() error {
	getRoomsResponse, err := b.resolvedClient.GetRooms()
	if err != nil {
		return fmt.Errorf("failed to access bridge rooms on %s: %w", b, err)
	}
	if getRoomsResponse.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch bridge rooms from %s: %s", b, getRoomsResponse.HTTPResponse.Status)
	}
	responseData := getRoomsResponse.JSON200.Data
	if responseData == nil {
		b.roomAssignments = maps.Clone(b.configRoomAssignments)
		return nil
	}
	b.roomAssignments = make(map[string]string, len(*responseData))
	for _, roomGet := range *responseData {
		for _, children := range *roomGet.Children {
			b.roomAssignments[*children.Rid] = *roomGet.Metadata.Name
		}
	}
	maps.Copy(b.roomAssignments, b.configRoomAssignments)
	return nil
}

func (b *bridge) resolveResourceRoom(resourceId string, resourceName string) string {
	roomName := b.roomAssignments[resourceName]
	if roomName != "" {
		return roomName
	}
	// If resource does not have a room assigned directly, iterate upwards via
	// its owners until we find a room or there is no more owner. The latter
	// may happen (e.g. for Motion Sensors) resulting in room name
	// "<unassigned>".
	currentResourceId := resourceId
	for {
		// Try next owner
		currentResourceId = b.resourceTree[currentResourceId]
		if currentResourceId == "" {
			// No owner left but no room found
			break
		}
		roomName = b.roomAssignments[currentResourceId]
		if roomName != "" {
			// Room name found, done
			return roomName
		}
	}
	return "<unassigned>"
}

func (b *bridge) resolveDeviceName(resourceId string) string {
	deviceName := b.deviceNames[resourceId]
	if deviceName != "" {
		return deviceName
	}
	// If resource does not have a device name assigned directly, iterate
	// upwards via its owners until we find a room or there is no more
	// owner. The latter may happen resulting in device name "<undefined>".
	currentResourceId := resourceId
	for {
		// Try next owner
		currentResourceId = b.resourceTree[currentResourceId]
		if currentResourceId == "" {
			// No owner left but no device found
			break
		}
		deviceName = b.deviceNames[currentResourceId]
		if deviceName != "" {
			// Device name found, done
			return deviceName
		}
	}
	return "<undefined>"
}
