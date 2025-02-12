# NSDP (Netgear Switch Discovery Protocol) Input Plugin

This input plugin gathers status from NSDP
([Netgear Switch Discovery Protocol][1]) capable devices.

[1]: https://en.wikipedia.org/wiki/Netgear_Switch_Discovery_Protocol

Retrieved status are:

- Bytes (sent and received)
- Total amount of packates processed
- Total amount of broadcasts processed
- Total amount of multicast processed
- Total amount of errors occurred

Each per switch and port.

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Gather NSDP (Netgear Switch Discovery Protocol) status
[[inputs.nsdp]]
  ## The target address to use for NSDP gathering the status.
  # target = "255.255.255.255:63322"

  ## The maximum number of device responses to wait for. 0 means no limit.
  ## NSDP works asynchronously. Without a limit (0) the plugin always waits
  ## the amount given in timeout for possible responses. By setting this
  ## option to the known number of devices, the plugin completes
  ## processing as soon as the last device has answered.
  # device_limit = 0

  ## The maximum duration to wait for device responses.
  # timeout = "2s"
```

## Metrics

- `nsdp_device_port`
  - tags
    - `device` - The device identifier (MAC/HW address)
    - `device_ip` - The device's IP address
    - `device_name` - The device's name
    - `device_model` - The device's model
    - `device_port` - The port id the fields are referring to
  - fields
    - `bytes_sent` (uint) - Number of bytes sent via this port
    - `bytes_recv` (uint) - Number of bytes received via this port
    - `packets_total` (uint) - Total number of packets processed on this port
    - `broadcasts_total` (uint) - Total number of broadcasts processed on this port
    - `multicasts_total` (uint) - Total number of multicasts processed on this port
    - `errors_total` (uint) - Total number of errors encountered on this port

## Example Output

<!-- markdownlint-disable MD013 -->

```text
nsdp_device_port,device=12:34:56:78:9a:bc,device_ip=10.1.0.4,device_model=GS108Ev3,device_name=switch2,device_port=1 broadcasts_total=0u,bytes_recv=3879427866u,bytes_sent=506548796u,errors_total=0u,multicasts_total=0u,packets_total=0u 1737152505014578000
```

<!-- markdownlint-enable MD013 -->.
