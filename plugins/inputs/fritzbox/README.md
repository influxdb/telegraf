# Fritzbox Input Plugin

This input plugin gathers status from [AVM][1] devices (routers, repeaters,
...). It uses the device's [TR-064][2] interfaces to retrieve the status.

[1]: https://en.avm.de/
[2]: https://avm.de/service/schnittstellen/

Retrieved status are:

- Device info (model, HW/SW version, uptime, ...)
- WAN info (bit rates, transferred bytes, ...)
- PPP info (bit rates, connection uptime, ...)
- DSL info (bit rates, DSL statistics, ...)
- WLAN info (number of clients per network, ...)
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
  ## The URLs of the devices to query. For each device the corresponding URL
  ## including the login credentials must be set. E.g.
  ## urls = [
  ##   "http://boxuser:boxpassword@fritz.box:49000/",
  ##   "http://:repeaterpassword@fritz.repeater:49000/",
  ## ]
  urls = [
  ]

  ## The information to collect (see README for further details).
  # collect = [
  #   "device",
  #   "wan",
  #   "ppp",
  #   "dsl",
  #   "wlan",
  # ]

  ## The http timeout to use.
  # timeout = "10s"

  ## Optional TLS Config
  # tls_ca = "/etc/telegraf/ca.pem"
  # tls_cert = "/etc/telegraf/cert.pem"
  # tls_key = "/etc/telegraf/key.pem"
  ## Use TLS but skip chain & host verification
  # insecure_skip_verify = false
```

### Collect options

The following collect options are available:

`device` metric: fritzbox_device - Collect device infos like model name,
SW version, uptime for the configured devices.

`wan` metric: fritzbox_wan - Collect generic WAN connection status like
bit rates, transferred bytes.

`ppp` metric: fritzbox_ppp - Collect PPP connection parameters like bit
rates, uptime.

`dsl` metric: fritzbox_dsl - Collect DSL line status and statistics.

`wlan` metric: fritzbox_wlan - Collect status and number of associated
devices for all WLANs.

`hosts` metric: fritzbox_hosts - Collect detailed information of the mesh
network including connected nodes, there role in the network as well as
their connection bandwidth.

**Note**: Collecting this metric is time consuming and it generates
very detailed data. If you activate this option, consider increasing
the plugin's query interval to avoid interval overruns and to minimize
the amount of collected data. You can adapt the interval per collect
otpion as follows:

```toml
[[inputs.fritzbox]]
  collect = [ "device", "wan", "ppp", "dsl", "wlan" ]
  ...

[[inputs.fritzbox]]
  interval = "5m"
  collect = [ "hosts" ]
  ...
```

## Metrics

By default field names are directly derived from the corresponding [interface
specification][1].

- `fritzbox_device`
  - tags
    - `source` - The name of the device (this metric has been queried from)
    - `service` - The service id used to query this metric
  - fields
    - `uptime` (uint) - Device's uptime in seconds.
    - `model_name` (string) - Device's model name.
    - `serial_number` (string) - Device's serial number.
    - `hardware_version` (string) - Device's hardware version.
    - `software_version` (string) - Device's software version.
- `fritzbox_wan`
  - tags
    - `source` - The name of the device (this metric has been queried from)
    - `service` - The service id used to query this metric
  - fields
    - `layer1_upstream_max_bit_rate` (uint) - The WAN interface's maximum upstream bit rate (bits/sec)
    - `layer1_downstream_max_bit_rate` (uint) - The WAN interface's maximum downstream bit rate (bits/sec)
    - `upstream_current_max_speed` (uint) - The WAN interface's current maximum upstream transfer rate (bytes/sec)
    - `downstream_current_max_speed` (uint) - The WAN interface's current maximum downstream data rate (bytes/sec)
    - `total_bytes_sent` (uint) - The total number of bytes sent via the WAN interface (bytes)
    - `total_bytes_received` (uint) - The total number of bytes received via the WAN interface (bytes)
- `fritzbox_ppp`
  - tags
    - `source` - The name of the device (this metric has been queried from)
    - `service` - The service id used to query this metric
  - fields
    - `uptime` (uint) - The current uptime of the PPP connection in seconds
    - `upstream_max_bit_rate` (uint) - The current maximum upstream bit rate negotiated for the PPP connection (bits/sec)
    - `downstream_max_bit_rate` (uint) - The current maximum downstream bit rate negotiated for the PPP connection (bits/sec)
- `fritzbox_dsl`
  - tags
    - `source` - The name of the device (this metric has been queried from)
    - `service` - The service id used to query this metric
    - `status` - The status of the DLS line (Up or Down)
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
    - `source` - The name of the device (this metric has been queried from)
    - `service` - The service id used to query this metric
    - `wlan` - The WLAN SSID (name)
    - `channel` - The channel used by this WLAN
    - `band` - The band (in MHz) used by this WLAN
    - `status` - The status of the WLAN line (Up or Down)
  - fields
    - `total_associations` (uint) - The number of devices connected to this WLAN.
- `fritzbox_hosts`
  - tags
    - `source` - The name of the device (this metric has been queried from)
    - `service` - The service id used to query this metric
    - `node` - The name of the node connected to the mesh network
    - `node_role` - The node's role ("master" = mesh master, "slave" = mesh slave, "client") in the network
    - `node_ap` - The name of the access point this node is connected to
    - `node_ap_role` - The access point's role ("master" = mesh master, "slave" = mesh slave, never "client") in the network
    - `link_type` - The link type ("WLAN" or "LAN") of the peer connection
    - `link_name` - The link name of the connection
  - fields
    - `max_data_rate_tx` (uint) - The connection's maximum transmit rate (kilobits/sec)
    - `max_data_rate_rx` (uint) - The connection's maximum receive rate (kilobits/sec)
    - `cur_data_rate_tx` (uint) - The connection's maximum transmit rate (kilobits/sec)
    - `cur_data_rate_rx` (uint) - The connection's current receive rate (kilobits/sec)

## Example Output

<!-- markdownlint-disable MD013 -->

```text
fritzbox_device,service=DeviceInfo1,source=fritz.box uptime=2058438i,model_name="Mock 1234",serial_number="123456789",hardware_version="Mock 1234",software_version="1.02.03" 1737003520174438000

fritzbox_wan,service=WANCommonInterfaceConfig1,source=fritz.box layer1_upstream_max_bit_rate=48816000i,layer1_downstream_max_bit_rate=253247000i,upstream_current_max_speed=511831i,downstream_current_max_speed=1304268i,total_bytes_sent=129497283207i,total_bytes_received=554484531337i 1737003587690504000

fritzbox_ppp,service=WANPPPConnection1,source=fritz.box uptime=369434i,upstream_max_bit_rate=44213433i,downstream_max_bit_rate=68038668i 1737003622308149000

fritzbox_dsl,service=WANDSLInterfaceConfig1,source=fritz.box,status=Up downstream_curr_rate=249065i,downstream_max_rate=249065i,downstream_power=513i,init_timeouts=0i,atuc_crc_errors=13i,errored_secs=25i,atuc_hec_errors=0i,upstream_noise_margin=80i,downstream_noise_margin=60i,downstream_attenuation=140i,receive_blocks=490282831i,transmit_blocks=254577751i,init_errors=0i,crc_errors=53i,fec_errors=0i,hec_errors=0i,upstream_max_rate=48873i,upstream_attenuation=80i,upstream_power=498i,cell_delin=0i,link_retrain=2i,loss_of_framing=0i,upstream_curr_rate=46719i,severly_errored_secs=0i,atuc_fec_errors=0i 1737003645769642000

fritzbox_wlan,band=2400,channel=13,service=WLANConfiguration1,source=fritz.box,ssid=MOCK1234,status=Up total_associations=11i 1737003673561198000

fritzbox_hosts,node=device#17,node_ap=device#1,node_ap_role=master,node_role=slave,link_name=AP:2G:0,link_type=WLAN,service=Hosts1,source=fritz.box cur_data_rate_tx=216000i,cur_data_rate_rx=216000i,max_data_rate_tx=216000i,max_data_rate_rx=216000i 1737003707257394000
fritzbox_hosts,node=device#24,node_ap=device#17,node_ap_role=slave,node_role=client,link_name=LAN:1,link_type=LAN,service=Hosts1,source=fritz.box max_data_rate_tx=1000000i,max_data_rate_rx=1000000i,cur_data_rate_tx=0i,cur_data_rate_rx=0i 1737003707257248000
```

<!-- markdownlint-enable MD013 -->
