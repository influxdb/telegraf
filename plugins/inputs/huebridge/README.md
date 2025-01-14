# HueBridge Input Plugin

This input plugin gathers status from [Hue Bridge][1] devices.
It uses the device's [CLIP API][2] interface to retrieve the status.

[1]: https://www.philips-hue.com/
[2]: https://developers.meethue.com/develop/hue-api-v2/

Retrieved status are:

- Light status (on|off)
- Temperatures
- Light levels
- Motion sensors
- Device power status (battery level and state)

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Gather Hue smart home status
[[inputs.huebridge]]
  ## The Hue bridges to query, each identified via an URL of the following form:
  ## <locator scheme>://<bridge id>:<user name>@<locator dependent address>/
  ## where:
  ## <locator scheme> is one of
  ## - address: To identify the bridge via the DNS name or ip address within the
  ##            URLs address part (see example below).
  ## - cloud:   To identify the bridge via its cloud registration. The address
  ##            part defines the discovery endpoint. If empty the standard endpoint
  ##            https://discovery.meethue.com/ is used.
  ## - mdns:    To identify the bridge via mDNS. The URL's address part is always
  #             empty in this case.
  ## - remote:  To identify the bridge via the Cloud Remote API. The address part
  ##            defines the cloud API endpoint. If empty the standard endpoint
  ##            https://api.meethue.com/ is used.
  ## <bridge id> is the unique bridge id as returned in
  ##   curl -k https://<bridge address>/api/config/0
  ## <user name> is the secret user name returned during application authentication.
  ##   To create a new user name issue the following command after pressing the
  ##   bridge's link button:
  ##   curl -k -X POST http://<bridge address>/api \
  ##     -H 'Content-Type: application/json' \
  ##     -d '{"devicetype":"huebridge-telegraf-plugin"}'
  ## Examples:
  ##   - "address://0123456789ABCDEF:sFlEGnMAFXO6RtZV17aViNUB95G2uXWw64texDzD@mybridge/"
  ##   - "cloud://0123456789ABCDEF:sFlEGnMAFXO6RtZV17aViNUB95G2uXWw64texDzD@/"
  ##   - "mdns://0123456789ABCDEF:sFlEGnMAFXO6RtZV17aViNUB95G2uXWw64texDzD@/"
  ##   - "remote://0123456789ABCDEF:sFlEGnMAFXO6RtZV17aViNUB95G2uXWw64texDzD@/"
  bridges = [
  ]
  
  ## Ignore invalid certificates while accessing the cloud discovery endpoint.
  ## Used for testing purposes only.
  # cloud_insecure_skip_verify = false
  
  ## The remote parameters to use to access a bridge remotely.
  ## To access a bridge remotely a Hue Developer Account is required, a Remote
  ## App must be registered and the corresponding Authorization flow must be
  ## completed. See https://developers.meethue.com/develop/hue-api-v2/cloud2cloud-getting-started/
  ## for further details.
  ## The Remote App's client id, client secret and callback url must be entered
  ## here exactly as used within the App registration.
  ## The remote_token_dir points to the directory receiving the token data.
  ## Setting remote_insecure_skip_verify to true disables certificate checking.
  ## This used for testing purposes only.
  # remote_client_id = ""
  # remote_client_secret = ""
  # remote_callback_url = ""
  # remote_token_dir = ""
  # remote_insecure_skip_verify = false
  
  ## Manual device to room assignments to consider during status evaluation.
  ## In case a device cannot be assigned to a room (e.g. a motion sensor),
  ## this table allows manual assignment.
  ## Each entry consists of two names. First is the name of the device and 2nd
  ## is the name of the room, the device is assigned to.
  ## Example:
  ##   [ ["Device 1", "Room A"] ]
  room_assignments = [
  ]
  
  ## The http timeout to use (in seconds).
  # timeout = "10s"
  
  ## Enable debug output
  # debug = false
```

## Metrics

- `huebridge_light`
  - tags
    - `huebridge_bridge_id` - The bridge id (this metrics has been queried from)
    - `huebridge_room` - The name of the room
    - `huebridge_device` - The name of the device
  - fields
    - `on` (int) - 0: light is off 1: light is on
- `huebridge_temperature`
  - tags
    - `huebridge_bridge_id` - The bridge id (this metrics has been queried from)
    - `huebridge_room` - The name of the room
    - `huebridge_device` - The name of the device
    - `huebridge_device_enabled` - The current status of sensor (active: true|false)
  - fields
    - `temperature` (float) - The current temperatue (in °Celsius)
- `huebridge_light_level`
  - tags
    - `huebridge_bridge_id` - The bridge id (this metrics has been queried from)
    - `huebridge_room` - The name of the room
    - `huebridge_device` - The name of the device
    - `huebridge_device_enabled` - The current status of sensor (active: true|false)
  - fields
    - `light_level` (int) - The current light level (in human friendly scale 10.000*log10(lux)+1)
    - `light_level_lux` (float) - The current light level (in lux)
- `huebridge_motion_sensor`
  - tags
    - `huebridge_bridge_id` - The bridge id (this metrics has been queried from)
    - `huebridge_room` - The name of the room
    - `huebridge_device` - The name of the device
    - `huebridge_device_enabled` - The current status of sensor (active: true|false)
  - fields
    - `motion` (int) - 0: no motion detected 1: motion detected
- `huebridge_device_power`
  - tags
    - `huebridge_bridge_id` - The bridge id (this metrics has been queried from)
    - `huebridge_room` - The name of the room
    - `huebridge_device` - The name of the device
  - fields
    - `battery_level` (int) - Power source status (normal, low, critical)
    - `battery_state` (string) - Battery charge level (in %)

## Example Output

```text
<!-- markdownlint-disable MD013 -->
huebridge_light,huebridge_bridge_id=0123456789ABCDEF,huebridge_room=Name#15,huebridge_device=Name#3 on=0 1734880329

huebridge_temperature,huebridge_room=Name#15,huebridge_device=Name#7,huebridge_device_enabled=true,huebridge_bridge_id=0123456789ABCDEF temperature=17.63 1734880329

huebridge_light_level,huebridge_bridge_id=0123456789ABCDEF,huebridge_room=Name#15,huebridge_device=Name#7,huebridge_device_enabled=true light_level=18948,light_level_lux=78.46934003526889 1734880329

huebridge_motion_sensor,huebridge_bridge_id=0123456789ABCDEF,huebridge_room=Name#15,huebridge_device=Name#7,huebridge_device_enabled=true motion=0 1734880329

huebridge_device_power,huebridge_bridge_id=0123456789ABCDEF,huebridge_room=Name#15,huebridge_device=Name#7 battery_level=100,battery_state=normal 1734880329
<!-- markdownlint-enable MD013 -->.
```
