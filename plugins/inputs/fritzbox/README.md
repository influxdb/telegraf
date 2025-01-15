# Fritzbox Input Plugin

This input plugin gathers status from [AVM][1] devices (routers, repeaters,
...). It uses the device's [TR-064][2] interfaces to retrieve the status.

[1]: https://avm.de/
[2]: https://avm.de/service/schnittstellen/

Retrieved status are:

- Device info (model, HW/SW version, uptime, ...)
- WAN info (bit rates, transferred bytes, ...)
- PPP info (bit rates, connection uptime, ...)
- DSL info (bit rates, DSL statistics, ...)
- WLAN info (numbrer of clients per network, ...)
- Hosts info (mesh nodes, bit rates, ...)

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Gather fritzbox status
[[inputs.fritzbox]]
  ## The devices to query. For each device the corresponding URL including the
  ## user and passwort needed to login must be set.
  ## E.g.
  ## devices = [
  ##   "http://boxuser:boxpassword@fritz.box:49000/",
  ##   "http://:repeaterpassword@fritz.repeater:49000/",
  ## ]
  devices = [
  ]

  ## The information to query (see README for further details).
  ## Hosts info is disabled by default, as it generates an extensive amount
  ## of data.
  # device_info = true
  # wan_info = true
  # ppp_info = true
  # dsl_info = true
  # wlan_info = true
  # hosts_info = false

  ## Some metric queries are time-consuming and not collected on every query
  ## cycle. This counter defines how often these low-traffic queries
  ## are excuted. The default value 30 means, on every 30th query corresponding
  ## to every 5 minutes (assuming the standard query interval of 10s).
  ## If this option is set to 1 or below, all metrics are collected on every
  ## query cycle.
  # full_query_cycle = 30

  ## The http timeout to use.
  # timeout = "10s"

  ## Skip TLS verification. Is needed to query devices with the default
  ## self-signed certificate.
  # tls_skip_verify = false

  ## Enable debug output
  # debug = false
```

## Metrics

By default field names are directly derived from the corresponding [interface
specification][1].

- `fritzbox_device`
  - tags
    - `fritz_device` - The device name (this metric has been queried from)
    - `fritz_service` - The service id used to query this metric
  - fields
    - `uptime` (uint) - Device's uptime in seconds.
    - `model_name` (string) - Device's model name.
    - `serial_number` (string) - Device's serial number.
    - `hardware_version` (string) - Device's hardware version.
    - `software_version` (string) - Device's software version.
- `fritzbox_wan`
  - tags
    - `fritz_device` - The device name (this metric has been queried from)
    - `fritz_service` - The service id used to query this metric
  - fields
    - `layer1_upstream_max_bit_rate` (uint) - The WAN interface's maximum upstream bit rate (bits/sec)
    - `layer1_downstream_max_bit_rate` (uint) - The WAN interface's maximum downstream bit rate (bits/sec)
    - `upstream_current_max_speed` (uint) - The WAN interface's current maximum upstream transfer rate (bytes/sec)
    - `downstream_current_max_speed` (uint) - The WAN interface's current maximum downstream data rate (bytes/sec)
    - `total_bytes_sent` (uint) - The total number of bytes sent via the WAN interface (bytes)
    - `total_bytes_received` (uint) - The total number of bytes received via the WAN interface (bytes)
- `fritzbox_ppp`
  - tags
    - `fritz_device` - The device name (this metric has been queried from)
    - `fritz_service` - The service id used to query this metric
  - fields
    - `uptime` (uint) - The current uptime of the PPP connection in seconds
    - `upstream_max_bit_rate` (uint) - The current maximum upstream bit rate negotiated for the PPP connection (bits/sec)
    - `downstream_max_bit_rate` (uint) - The current maximum downstream bit rate negotiated for the PPP connection (bits/sec)
- `fritzbox_dsl`
  - tags
    - `fritz_device` - The device name (this metric has been queried from)
    - `fritz_service` - The service id used to query this metric
  - fields
    - `upstream_curr_rate` (uint) - Current DSL upstream rate (kilobits/sec)
    - `downstream_curr_rate` (uint) - Current DSL downstream rate (kilobits/sec)
    - `upstream_max_rate` (uint) - Maximum DSL upstream rate (kilobits/sec)
    - `downstream_max_rate` (uint) - Maximum DSL downstream rate (kilobits/sec)
    - `upstream_noise_margin` (uint) - Upstream noise margin (db)
    - `downstream_noise_margin` (uint) - Downstream noise margin (db)
    - `upstream_attenuation` (uint) - Upstream attenuation (db)
    - `downstream_attenuation` (uint) - Downstream attenuation (db)
    - `upstream_power` (uint) - Upstream power
    - `downstream_power` (uint) - Downstream power
    - `receive_blocks` (uint) - Received blocks
    - `transmit_blocks` (uint) - Transmitted blocks
    - `cell_delin` (uint) - Cell delineation count
    - `link_retrain` (uint) - Link retrains
    - `init_errors` (uint) - Initialization errors
    - `init_timeouts` (uint) - Initialization timeouts
    - `loss_of_framing` (uint) - Loss of frame errors
    - `errored_secs` (uint) - Continuous seconds with errors
    - `severly_errored_secs` (uint) - Continuous seconds with severe errors
    - `fec_errors` (uint) - Local (Modem) FEC (Forward Error Correction) errors
    - `atuc_fec_errors` (uint) - Remote (DSLAM) FEC (Forward Error Correction) errors
    - `hec_errors` (uint) - Local (Modem) HEC (Header Error Control) errors
    - `atuc_hec_errors` (uint) - Remote (DSLAM) HEC (Header Error Control) errors
    - `crc_errors` (uint) - Local (Modem) CRC (Cyclic Redundancy Check) error
    - `atuc_crc_errors` (uint) - Remote (DSLAM) CRC (Cyclic Redundancy Check) errors
- `fritzbox_wlan`
  - tags
    - `fritz_device` - The device name (this metric has been queried from)
    - `fritz_service` - The service id used to query this metric
    - `fritz_wlan` - The WLAN SSID (name)
    - `fritz_wlan_channel` - The channel used by this WLAN
    - `fritz_wlan_band` - The band (in MHz) used by this WLAN
  - fields
    - `total_associations` (uint) - The number of devices connected to this WLAN.
- `fritzbox_host`
  - tags
    - `fritz_device` - The device name (this metric has been queried from)
    - `fritz_service` - The service id used to query this metric
    - `fritz_host` - The host connected to the network
    - `fritz_host_role` - The host's role ("master" = mesh master, "slave" = mesh slave, "client") in the network
    - `fritz_host_peer` - The name of the peer this host is connected to
    - `fritz_host_peer_role` - The peer's role ("master" = mesh master, "slave" = mesh slave, never "client") in the network
    - `fritz_link_type` - The link type ("WLAN" or "LAN") of the peer connection
    - `fritz_link_name` - The link name of the connection
  - fields
    - `max_data_rate_tx` (uint) - The connection's maximum transmit rate (kilobits/sec)
    - `max_data_rate_rx` (uint) - The connection's maximum receive rate (kilobits/sec)
    - `cur_data_rate_tx` (uint) - The connection's maximum transmit rate (kilobits/sec)
    - `cur_data_rate_rx` (uint) - The connection's current receive rate (kilobits/sec)

## Example Output

<!-- markdownlint-disable MD013 -->

```text
fritzbox_device,fritz_device=127.0.0.1,fritz_service=DeviceInfo1 model_name=Mock 1234,serial_number=123456789,hardware_version=Mock 1234,software_version=1.02.03,uptime=2058438 1736529975

fritzbox_wan,fritz_device=127.0.0.1,fritz_service=WANCommonInterfaceConfig1 total_bytes_received=554484531337,layer1_upstream_max_bit_rate=48816000,layer1_downstream_max_bit_rate=253247000,upstream_current_max_speed=511831,downstream_current_max_speed=1304268,total_bytes_sent=129497283207 1736530024

fritzbox_ppp,fritz_device=127.0.0.1,fritz_service=WANPPPConnection1 uptime=369434,upstream_max_bit_rate=44213433,downstream_max_bit_rate=68038668 1736530058

fritzbox_dsl,fritz_device=127.0.0.1,fritz_service=WANDSLInterfaceConfig1 downstream_noise_margin=60,upstream_power=498,downstream_power=513,upstream_curr_rate=46719,downstream_curr_rate=249065,upstream_max_rate=48873,downstream_max_rate=249065,upstream_noise_margin=80,severly_errored_secs=0,upstream_attenuation=80,transmit_blocks=254577751,init_timeouts=0,atuc_crc_errors=13,receive_blocks=490282831,errored_secs=25,fec_errors=0,atuc_hec_errors=0,atuc_fec_errors=0,hec_errors=0,crc_errors=53,downstream_attenuation=140,cell_delin=0,link_retrain=2,init_errors=0,loss_of_framing=0 1736530092

fritzbox_wlan,fritz_device=127.0.0.1,fritz_service=WLANConfiguration1,fritz_wlan=MOCK1234,fritz_wlan_channel=13,fritz_wlan_band=2400 total_associations=11 1736530130

fritzbox_host,fritz_host_peer_role=master,fritz_link_type=WLAN,fritz_link_name=AP:2G:0,fritz_device=127.0.0.1,fritz_service=Hosts1,fritz_host=device#17,fritz_host_role=slave,fritz_host_peer=device#1 max_data_rate_tx=216000,max_data_rate_rx=216000,cur_data_rate_tx=216000,cur_data_rate_rx=216000 1736530165
fritzbox_host,fritz_device=127.0.0.1,fritz_service=Hosts1,fritz_host=device#24,fritz_host_role=client,fritz_host_peer=device#17,fritz_host_peer_role=slave,fritz_link_type=LAN,fritz_link_name=LAN:1 cur_data_rate_tx=0,cur_data_rate_rx=0,max_data_rate_tx=1000000,max_data_rate_rx=1000000 1736530165
```

<!-- markdownlint-enable MD013 -->.
