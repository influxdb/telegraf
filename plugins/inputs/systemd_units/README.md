# Systemd-Units Input Plugin

This plugin gathers the status of systemd-units on Linux, using systemd's DBus
interface.

Please note: At least systemd v230 is required!

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Gather information about systemd-unit states
# This plugin ONLY supports Linux
[[inputs.systemd_units]]
  ## Pattern of units to collect
  ## A space-separated list of unit-patterns including wildcards determining
  ## the units to collect.
  ##  ex: pattern = "telegraf* influxdb* user@*"
  # pattern = "*"

  ## Filter for a specific unit type
  ## Available settings are: service, socket, target, device, mount,
  ## automount, swap, timer, path, slice and scope
  # unittype = "service"

  ## Collect system or user scoped units
  ##  ex: scope = "user"
  # scope = "system"

  ## Collect also units not loaded by systemd, i.e. disabled or static units
  ## Enabling this feature might introduce significant load when used with
  ## unspecific patterns (such as '*') as systemd will need to load all
  ## matching unit files.
  # collect_disabled_units = false

  ## Collect detailed information for the units
  # details = false

  ## Timeout for state-collection
  # timeout = "5s"
```

This plugin supports two modes of operation:

### Non-detailed mode

This is the default mode, collecting data on the unit's status only without
further details on the unit.

### Detailed mode

This mode can be enabled by setting the configuration option `details` to
`true`. In this mode the plugin collects all information of the non-detailed
mode but provides additional unit information such as memory usage,
restart-counts, PID, etc. See the [metrics section](#metrics) below for a list
of all properties collected.

## Metrics

These metrics are available in both modes:

- systemd_units:
  - tags:
    - name (string, unit name)
    - load (string, load state)
    - active (string, active state)
    - sub (string, sub state)
    - user (string, username only for user scope)
  - fields:
    - load_code (int, see below)
    - active_code (int, see below)
    - sub_code (int, see below)

The following *additional* metrics are available with `details = true`:

- systemd_units:
  - tags:
    - state (string, unit file state)
    - preset (string, unit file preset state)
  - fields:
    - status_errno (int, last error)
    - restarts (int, number of restarts)
    - pid (int, pid of the main process)
    - mem_current (uint, current memory usage)
    - mem_peak (uint, peak memory usage)
    - swap_current (uint, current swap usage)
    - swap_peak (uint, peak swap usage)
    - mem_avail (uint, available memory for this unit)
    - active_enter_timestamp_us (uint, timestamp in us when entered the state)

### Load

Enumeration of [unit_load_state_table][1]

| Value | Meaning     | Description                     |
| ----- | -------     | -----------                     |
| 0     | loaded      | unit is ~                       |
| 1     | stub        | unit is ~                       |
| 2     | not-found   | unit is ~                       |
| 3     | bad-setting | unit is ~                       |
| 4     | error       | unit is ~                       |
| 5     | merged      | unit is ~                       |
| 6     | masked      | unit is ~                       |

[1]: https://github.com/systemd/systemd/blob/c87700a1335f489be31cd3549927da68b5638819/src/basic/unit-def.c#L87

### Active

Enumeration of [unit_active_state_table][2]

| Value | Meaning   | Description                        |
| ----- | -------   | -----------                        |
| 0     | active       | unit is ~                       |
| 1     | reloading    | unit is ~                       |
| 2     | inactive     | unit is ~                       |
| 3     | failed       | unit is ~                       |
| 4     | activating   | unit is ~                       |
| 5     | deactivating | unit is ~                       |

[2]: https://github.com/systemd/systemd/blob/c87700a1335f489be31cd3549927da68b5638819/src/basic/unit-def.c#L99

### Sub

enumeration of sub states, see various [unittype_state_tables][3]; duplicates
were removed, tables are hex aligned to keep some space for future values

| Value  | Meaning               | Description                         |
| -----  | -------               | -----------                         |
|        |                       | service_state_table start at 0x0000 |
| 0x0000 | running               | unit is ~                           |
| 0x0001 | dead                  | unit is ~                           |
| 0x0002 | start-pre             | unit is ~                           |
| 0x0003 | start                 | unit is ~                           |
| 0x0004 | exited                | unit is ~                           |
| 0x0005 | reload                | unit is ~                           |
| 0x0006 | stop                  | unit is ~                           |
| 0x0007 | stop-watchdog         | unit is ~                           |
| 0x0008 | stop-sigterm          | unit is ~                           |
| 0x0009 | stop-sigkill          | unit is ~                           |
| 0x000a | stop-post             | unit is ~                           |
| 0x000b | final-sigterm         | unit is ~                           |
| 0x000c | failed                | unit is ~                           |
| 0x000d | auto-restart          | unit is ~                           |
| 0x000e | condition             | unit is ~                           |
| 0x000f | cleaning              | unit is ~                           |
|        |                       | service_state_table start at 0x0010 |
| 0x0010 | waiting               | unit is ~                           |
| 0x0011 | reload-signal         | unit is ~                           |
| 0x0012 | reload-notify         | unit is ~                           |
| 0x0013 | final-watchdog        | unit is ~                           |
| 0x0014 | dead-before-auto-restart    | unit is ~                     |
| 0x0015 | failed-before-auto-restart  | unit is ~                     |
| 0x0016 | dead-resources-pinned | unit is ~                           |
| 0x0017 | auto-restart-queued   | unit is ~                           |
|        |                       | service_state_table start at 0x0020 |
| 0x0020 | tentative             | unit is ~                           |
| 0x0021 | plugged               | unit is ~                           |
|        |                       | service_state_table start at 0x0030 |
| 0x0030 | mounting              | unit is ~                           |
| 0x0031 | mounting-done         | unit is ~                           |
| 0x0032 | mounted               | unit is ~                           |
| 0x0033 | remounting            | unit is ~                           |
| 0x0034 | unmounting            | unit is ~                           |
| 0x0035 | remounting-sigterm    | unit is ~                           |
| 0x0036 | remounting-sigkill    | unit is ~                           |
| 0x0037 | unmounting-sigterm    | unit is ~                           |
| 0x0038 | unmounting-sigkill    | unit is ~                           |
|        |                       | service_state_table start at 0x0040 |
|        |                       | service_state_table start at 0x0050 |
| 0x0050 | abandoned             | unit is ~                           |
|        |                       | service_state_table start at 0x0060 |
| 0x0060 | active                | unit is ~                           |
|        |                       | service_state_table start at 0x0070 |
| 0x0070 | start-chown           | unit is ~                           |
| 0x0071 | start-post            | unit is ~                           |
| 0x0072 | listening             | unit is ~                           |
| 0x0073 | stop-pre              | unit is ~                           |
| 0x0074 | stop-pre-sigterm      | unit is ~                           |
| 0x0075 | stop-pre-sigkill      | unit is ~                           |
| 0x0076 | final-sigkill         | unit is ~                           |
|        |                       | service_state_table start at 0x0080 |
| 0x0080 | activating            | unit is ~                           |
| 0x0081 | activating-done       | unit is ~                           |
| 0x0082 | deactivating          | unit is ~                           |
| 0x0083 | deactivating-sigterm  | unit is ~                           |
| 0x0084 | deactivating-sigkill  | unit is ~                           |
|        |                       | service_state_table start at 0x0090 |
|        |                       | service_state_table start at 0x00a0 |
| 0x00a0 | elapsed               | unit is ~                           |
|        |                       |                                     |

[3]: https://github.com/systemd/systemd/blob/c87700a1335f489be31cd3549927da68b5638819/src/basic/unit-def.c#L163

## Example Output

### Output in non-detailed mode

```text
systemd_units,host=host1.example.com,name=dbus.service,load=loaded,active=active,sub=running,user=telegraf load_code=0i,active_code=0i,sub_code=0i 1533730725000000000
systemd_units,host=host1.example.com,name=networking.service,load=loaded,active=failed,sub=failed,user=telegraf load_code=0i,active_code=3i,sub_code=12i 1533730725000000000
systemd_units,host=host1.example.com,name=ssh.service,load=loaded,active=active,sub=running,user=telegraf load_code=0i,active_code=0i,sub_code=0i 1533730725000000000
```

### Output in detailed mode

```text
systemd_units,active=active,host=host1.example.com,load=loaded,name=dbus.service,sub=running,preset=disabled,state=static,user=telegraf active_code=0i,load_code=0i,mem_avail=6470856704i,mem_current=2691072i,mem_peak=3895296i,pid=481i,restarts=0i,status_errno=0i,sub_code=0i,swap_current=794624i,swap_peak=884736i 1533730725000000000
systemd_units,active=inactive,host=host1.example.com,load=not-found,name=networking.service,sub=dead,user=telegraf active_code=2i,load_code=2i,pid=0i,restarts=0i,status_errno=0i,sub_code=1i 1533730725000000000
systemd_units,active=active,host=host1.example.com,load=loaded,name=pcscd.service,sub=running,preset=disabled,state=indirect,user=telegraf active_code=0i,load_code=0i,mem_avail=6370541568i,mem_current=512000i,mem_peak=4399104i,pid=1673i,restarts=0i,status_errno=0i,sub_code=0i,swap_current=3149824i,swap_peak=3149824i 1533730725000000000
```
