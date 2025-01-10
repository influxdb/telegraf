# Intel PowerStat Input Plugin

This input plugin monitors power statistics on Intel-based platforms and
assumes presence of Linux based OS.

Not all CPUs are supported, please see the software and hardware dependencies
sections below to ensure platform support.

Main use cases are power saving and workload migration. Telemetry frameworks
allow users to monitor critical platform level metrics. Key source of platform
telemetry is power domain that is beneficial for MANO Monitoring&Analytics
systems to take preventive/corrective actions based on platform busyness, CPU
temperature, actual CPU utilization and power statistics.

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Intel PowerStat plugin enables monitoring of platform metrics (power, TDP)
# and per-CPU metrics like temperature, power and utilization. Please see the
# plugin readme for details on software and hardware compatibility.
# This plugin ONLY supports Linux.
[[inputs.intel_powerstat]]
  ## The user can choose which package metrics are monitored by the plugin with
  ## the package_metrics setting:
  ## - The default, will collect "current_power_consumption",
  ##   "current_dram_power_consumption" and "thermal_design_power".
  ## - Leaving this setting empty means no package metrics will be collected.
  ## - Finally, a user can specify individual metrics to capture from the
  ##   supported options list.
  ## Supported options:
  ##   "current_power_consumption", "current_dram_power_consumption",
  ##   "thermal_design_power", "max_turbo_frequency", "uncore_frequency",
  ##   "cpu_base_frequency"
  # package_metrics = ["current_power_consumption", "current_dram_power_consumption", "thermal_design_power"]

  ## The user can choose which per-CPU metrics are monitored by the plugin in
  ## cpu_metrics array.
  ## Empty or missing array means no per-CPU specific metrics will be collected
  ## by the plugin.
  ## Supported options:
  ##   "cpu_frequency", "cpu_c0_state_residency", "cpu_c1_state_residency",
  ##   "cpu_c3_state_residency", "cpu_c6_state_residency", "cpu_c7_state_residency",
  ##   "cpu_temperature", "cpu_busy_frequency", "cpu_c0_substate_c01",
  ##   "cpu_c0_substate_c02", "cpu_c0_substate_c0_wait"
  # cpu_metrics = []

  ## Optionally the user can choose for which CPUs metrics configured in cpu_metrics array should be gathered.
  ## Can't be combined with excluded_cpus.
  ## Empty or missing array means CPU metrics are gathered for all CPUs.
  ## e.g. ["0-3", "4,5,6"] or ["1-3,4"]
  # included_cpus = []

  ## Optionally the user can choose which CPUs should be excluded from gathering metrics configured in cpu_metrics array.
  ## Can't be combined with included_cpus.
  ## Empty or missing array means CPU metrics are gathered for all CPUs.
  ## e.g. ["0-3", "4,5,6"] or ["1-3,4"]
  # excluded_cpus = []

  ## Filesystem location of JSON file that contains PMU event definitions.
  ## Mandatory only for perf-related metrics (cpu_c0_substate_c01, cpu_c0_substate_c02, cpu_c0_substate_c0_wait).
  # event_definitions = ""

  ## The user can set the timeout duration for MSR reading.
  ## Enabling this timeout can be useful in situations where, on heavily loaded systems,
  ## the code waits too long for a kernel response to MSR read requests.
  ## 0 disables the timeout (default).
  # msr_read_timeout = "0ms"
```

### Configuration notes

1. The configuration of `included_cpus` or `excluded_cpus` may affect the ability to collect `package_metrics`.
   Some of them (`max_turbo_frequency`, `cpu_base_frequency`, and `uncore_frequency`) need to read data
   from exactly one processor for each package. If `included_cpus` or `excluded_cpus` exclude all processors
   from the package, reading the mentioned metrics for that package will not be possible.
2. `event_definitions` JSON file for specific architecture can be found at [perfmon](https://github.com/intel/perfmon).
   A script to download the event definition that is appropriate for current environment (`event_download.py`) is
   available at [pmu-tools](https://github.com/andikleen/pmu-tools).
   For perf-related metrics supported by this plugin, an event definition JSON file
   with events for the `core` is required.

   For example: `sapphirerapids_core.json` or `GenuineIntel-6-8F-core.json`.

### Example: Configuration with no per-CPU telemetry

This configuration allows getting default processor package specific metrics,
no per-CPU metrics are collected:

```toml
[[inputs.intel_powerstat]]
  cpu_metrics = []
```

### Example: Configuration with no per-CPU telemetry - equivalent case

This configuration allows getting default processor package specific metrics,
no per-CPU metrics are collected:

```toml
[[inputs.intel_powerstat]]
```

### Example: Configuration for CPU Temperature and CPU Frequency

This configuration allows getting default processor package specific metrics,
plus subset of per-CPU metrics (CPU Temperature and CPU Frequency) which will be
gathered only for `cpu_id = 0`:

```toml
[[inputs.intel_powerstat]]
  cpu_metrics = ["cpu_frequency", "cpu_temperature"]
  included_cpus = ["0"]
```

### Example: Configuration for CPU Temperature and CPU Frequency without default package metrics

This configuration allows getting only a subset of per-CPU metrics
(CPU Temperature and CPU Frequency) which will be gathered for
all `cpus` except `cpu_id = ["1-3"]`:

```toml
[[inputs.intel_powerstat]]
  package_metrics = []
  cpu_metrics = ["cpu_frequency", "cpu_temperature"]
  excluded_cpus = ["1-3"]
```

### Example: Configuration with all available metrics

This configuration allows getting all processor package specific metrics and
all per-CPU metrics:

```toml
[[inputs.intel_powerstat]]
  package_metrics = ["current_power_consumption", "current_dram_power_consumption", "thermal_design_power", "max_turbo_frequency", "uncore_frequency", "cpu_base_frequency"]
  cpu_metrics = ["cpu_frequency", "cpu_c0_state_residency", "cpu_c1_state_residency", "cpu_c3_state_residency", "cpu_c6_state_residency", "cpu_c7_state_residency", "cpu_temperature", "cpu_busy_frequency", "cpu_c0_substate_c01", "cpu_c0_substate_c02", "cpu_c0_substate_c0_wait"]
  event_definitions = "/home/telegraf/.cache/pmu-events/GenuineIntel-6-8F-core.json"
```

## SW Dependencies

### Kernel modules

Plugin is mostly based on Linux Kernel modules that expose specific metrics over
`sysfs` or `devfs` interfaces. The following dependencies are expected by
plugin:

- `intel-rapl` kernel module which exposes Intel Runtime Power Limiting metrics over
  `sysfs` (`/sys/devices/virtual/powercap/intel-rapl`),
- `msr` kernel module that provides access to processor model specific
  registers over `devfs` (`/dev/cpu/cpu%d/msr`),
- `cpufreq` kernel module - which exposes per-CPU Frequency over `sysfs`
 (`/sys/devices/system/cpu/cpu%d/cpufreq/scaling_cur_freq`),
- `intel-uncore-frequency` kernel module exposes Intel uncore frequency metrics
  over `sysfs` (`/sys/devices/system/cpu/intel_uncore_frequency`).

Make sure that required kernel modules are loaded and running.
Modules might have to be manually enabled by using `modprobe`.
Depending on the kernel version, run commands:

```sh
# rapl modules:
## kernel < 4.0
sudo modprobe intel_rapl
## kernel >= 4.0
sudo modprobe rapl
sudo modprobe intel_rapl_common
sudo modprobe intel_rapl_msr

# msr module:
sudo modprobe msr

# cpufreq module:
### integrated in kernel 

# intel-uncore-frequency module:
## only for kernel >= 5.6.0
sudo modprobe intel-uncore-frequency
```

### Kernel's perf interface

For perf-related metrics, when Telegraf is not running as root,
the following capability should be added to the Telegraf executable:

```sh
sudo setcap cap_sys_admin+ep <path_to_telegraf_binary>
```

Alternatively, `/proc/sys/kernel/perf_event_paranoid` has to be set to
value less than 1.

Depending on environment and configuration (number of monitored CPUs
and number of enabled metrics), it might be required to increase
the limit on the number of open file descriptors allowed.
This can be done for example by using `ulimit -n` command.

### Dependencies of metrics on system configuration

Details of these dependencies are discussed above:

| Configuration option                                                                | Type              | Dependency                                     |
|-------------------------------------------------------------------------------------|-------------------|------------------------------------------------|
| `current_power_consumption`                                                         | `package_metrics` | `rapl` kernel module(s)                        |
| `current_dram_power_consumption`                                                    | `package_metrics` | `rapl` kernel module(s)                        |
| `thermal_design_power`                                                              | `package_metrics` | `rapl` kernel module(s)                        |
| `max_turbo_frequency`                                                               | `package_metrics` | `msr` kernel module                            |
| `uncore_frequency`                                                                  | `package_metrics` | `intel-uncore-frequency`/`msr` kernel modules* |
| `cpu_base_frequency`                                                                | `package_metrics` | `msr` kernel module                            |
| `cpu_frequency`                                                                     | `cpu_metrics`     | `cpufreq` kernel module                        |
| `cpu_c0_state_residency`                                                            | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_c1_state_residency`                                                            | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_c3_state_residency`                                                            | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_c6_state_residency`                                                            | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_c7_state_residency`                                                            | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_busy_cycles` (**DEPRECATED** - superseded by `cpu_c0_state_residency_percent`) | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_temperature`                                                                   | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_busy_frequency`                                                                | `cpu_metrics`     | `msr` kernel module                            |
| `cpu_c0_substate_c01`                                                               | `cpu_metrics`     | kernel's `perf` interface                      |
| `cpu_c0_substate_c02`                                                               | `cpu_metrics`     | kernel's `perf` interface                      |
| `cpu_c0_substate_c0_wait`                                                           | `cpu_metrics`     | kernel's `perf` interface                      |

*for all metrics enabled by the configuration option `uncore_frequency`,
starting from kernel version 5.18, only the `intel-uncore-frequency` module
is required. For older kernel versions, the metric `uncore_frequency_mhz_cur`
requires the `msr` module to be enabled.

### Root privileges

**Telegraf with Intel PowerStat plugin enabled may require
root privileges to read all the metrics**
(depending on OS type or configuration).

Alternatively, the following capabilities can be added to
the Telegraf executable:

```sh
#without perf-related metrics:
sudo setcap cap_sys_rawio,cap_dac_read_search+ep <path_to_telegraf_binary>

#with perf-related metrics:
sudo setcap cap_sys_rawio,cap_dac_read_search,cap_sys_admin+ep <path_to_telegraf_binary>
```

## HW Dependencies

Specific metrics require certain processor features to be present, otherwise
Intel PowerStat plugin won't be able to read them. The user can detect supported
processor features by reading `/proc/cpuinfo` file.
Plugin assumes crucial properties are the same for all CPU cores in the system.

The following `processor` properties are examined in more detail
in this section:

- `vendor_id`
- `cpu family`
- `model`
- `flags`

The following processor properties are required by the plugin:

- Processor `vendor_id` must be `GenuineIntel` and `cpu family` must be `6` -
  since data used by the plugin are Intel-specific.
- The following processor flags shall be present:
  - `msr` shall be present for plugin to read platform data from processor
    model specific registers and collect the following metrics:
    - `cpu_c0_state_residency`
    - `cpu_c1_state_residency`
    - `cpu_c3_state_residency`
    - `cpu_c6_state_residency`
    - `cpu_c7_state_residency`
    - `cpu_busy_cycles` (**DEPRECATED** - superseded by `cpu_c0_state_residency_percent`)
    - `cpu_busy_frequency`
    - `cpu_temperature`
    - `cpu_base_frequency`
    - `max_turbo_frequency`
    - `uncore_frequency` (for kernel < 5.18)
  - `aperfmperf` shall be present to collect the following metrics:
    - `cpu_c0_state_residency`
    - `cpu_c1_state_residency`
    - `cpu_busy_cycles` (**DEPRECATED** - superseded by `cpu_c0_state_residency_percent`)
    - `cpu_busy_frequency`
  - `dts` shall be present to collect:
    - `cpu_temperature`
- Please consult the table of [supported CPU models](#supported-cpu-models) to see which metrics are supported by your `model`. The following metrics exist:
  - `cpu_c1_state_residency`
  - `cpu_c3_state_residency`
  - `cpu_c6_state_residency`
  - `cpu_c7_state_residency`
  - `cpu_temperature`
  - `cpu_base_frequency`
  - `uncore_frequency`

## Metrics

All metrics collected by Intel PowerStat plugin are collected in fixed
intervals. Metrics that reports processor C-state residency or power are
calculated over elapsed intervals.

**The following measurements are supported by Intel PowerStat plugin:**

- `powerstat_core`
  - The following tags are returned by plugin with
    `powerstat_core` measurements:

      | Tag          | Description                    |
      |--------------|--------------------------------|
      | `package_id` | ID of platform package/socket. |
      | `core_id`    | ID of physical processor core. |
      | `cpu_id`     | ID of logical processor core.  |

    Measurement `powerstat_core` metrics are collected per-CPU (`cpu_id` is the key)
    while `core_id` and `package_id` tags are additional topology information.

  - Available metrics for `powerstat_core` measurement:

      | Metric name (field)               | Description                                                                                                                                                               | Units           |
      |-----------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------|
      | `cpu_frequency_mhz`               | Current operational frequency of CPU Core.                                                                                                                                | MHz             |
      | `cpu_busy_frequency_mhz`          | CPU Core Busy Frequency measured as frequency adjusted to CPU Core busy cycles.                                                                                           | MHz             |
      | `cpu_temperature_celsius`         | Current temperature of CPU Core.                                                                                                                                          | Celsius degrees |
      | `cpu_c0_state_residency_percent`  | Percentage of time that CPU Core spent in C0 Core residency state.                                                                                                        | %               |
      | `cpu_c1_state_residency_percent`  | Percentage of time that CPU Core spent in C1 Core residency state.                                                                                                        | %               |
      | `cpu_c3_state_residency_percent`  | Percentage of time that CPU Core spent in C3 Core residency state.                                                                                                        | %               |
      | `cpu_c6_state_residency_percent`  | Percentage of time that CPU Core spent in C6 Core residency state.                                                                                                        | %               |
      | `cpu_c7_state_residency_percent`  | Percentage of time that CPU Core spent in C7 Core residency state.                                                                                                        | %               |
      | `cpu_c0_substate_c01_percent`     | Percentage of time that CPU Core spent in C0.1 substate out of the total time in the C0 state.                                                                            | %               |
      | `cpu_c0_substate_c02_percent`     | Percentage of time that CPU Core spent in C0.2 substate out of the total time in the C0 state.                                                                            | %               |
      | `cpu_c0_substate_c0_wait_percent` | Percentage of time that CPU Core spent in C0_Wait substate out of the total time in the C0 state.                                                                         | %               |
      | `cpu_busy_cycles_percent`         | (**DEPRECATED** - superseded by cpu_c0_state_residency_percent) CPU Core Busy cycles as a ratio of Cycles spent in C0 state residency to all cycles executed by CPU Core. | %               |

- `powerstat_package`
  - The following tags are returned by plugin with `powerstat_package` measurements:

      | Tag            | Description                                                                                                                                                                                                                    |
      |----------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
      | `package_id`   | ID of platform package/socket.                                                                                                                                                                                                 |
      | `active_cores` | Specific tag for `max_turbo_frequency_mhz` metric. The maximum number of activated cores for reachable turbo frequency.                                                                                                        |
      | `hybrid`       | Specific tag for `max_turbo_frequency_mhz` metric. Available only for hybrid processors. Will be set to `primary` for primary cores of a hybrid architecture, and to `secondary` for secondary cores of a hybrid architecture. |
      | `die`          | Specific tag for all `uncore_frequency` metrics. Id of die.                                                                                                                                                                    |
      | `type`         | Specific tag for all `uncore_frequency` metrics. Type of uncore frequency (`current` or `initial`).                                                                                                                            |

    Measurement `powerstat_package` metrics are collected per processor package
    `package_id` tag indicates which package metric refers to.

  - Available metrics for `powerstat_package` measurement:

      | Metric name (field)                    | Description                                                                                                                                                                                                                                                                                                                                                                  | Units |
      |----------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-------|
      | `thermal_design_power_watts`           | Maximum Thermal Design Power (TDP) available for processor package.                                                                                                                                                                                                                                                                                                          | Watts |
      | `current_power_consumption_watts`      | Current power consumption of processor package.                                                                                                                                                                                                                                                                                                                              | Watts |
      | `current_dram_power_consumption_watts` | Current power consumption of processor package DRAM subsystem.                                                                                                                                                                                                                                                                                                               | Watts |
      | `max_turbo_frequency_mhz`              | Maximum reachable turbo frequency for number of cores active.                                                                                                                                                                                                                                                                                                                | MHz   |
      | `uncore_frequency_limit_mhz_min`       | Minimum uncore frequency limit for die in processor package.                                                                                                                                                                                                                                                                                                                 | MHz   |
      | `uncore_frequency_limit_mhz_max`       | Maximum uncore frequency limit for die in processor package.                                                                                                                                                                                                                                                                                                                 | MHz   |
      | `uncore_frequency_mhz_cur`             | Current uncore frequency for die in processor package. Available only with tag `current`. This value is available from `intel-uncore-frequency` module for kernel >= 5.18. For older kernel versions it needs to be accessed via MSR. In case of lack of loaded `msr`, only `uncore_frequency_limit_mhz_min` and `uncore_frequency_limit_mhz_max` metrics will be collected. | MHz   |
      | `cpu_base_frequency_mhz`               | CPU Base Frequency (maximum non-turbo frequency) for the processor package.                                                                                                                                                                                                                                                                                                  | MHz   |

### Known issues

Starting from Linux kernel version v5.4.77, due to
[this kernel change][19f6d91b], resources such as
`/sys/devices/virtual/powercap/intel-rapl//*/energy_uj`
can only be accessed by the root user for security reasons.
Therefore, this plugin requires root privileges to gather
`rapl` metrics correctly.

If such strict security restrictions are not relevant, reading permissions for
files in the `/sys/devices/virtual/powercap/intel-rapl/` directory can be
manually altered, for example, using the chmod command with custom parameters.
For instance, read and execute permissions for all files in the
intel-rapl directory can be granted to all users using:

```bash
sudo chmod -R a+rx /sys/devices/virtual/powercap/intel-rapl/
```

[19f6d91b]: https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/commit/?h=v5.4.77&id=19f6d91bdad42200aac557a683c17b1f65ee6c94

## Example Output

```text
powerstat_package,host=ubuntu,package_id=0 thermal_design_power_watts=160 1606494744000000000
powerstat_package,host=ubuntu,package_id=0 current_power_consumption_watts=35 1606494744000000000
powerstat_package,host=ubuntu,package_id=0 cpu_base_frequency_mhz=2400i 1669118424000000000
powerstat_package,host=ubuntu,package_id=0 current_dram_power_consumption_watts=13.94 1606494744000000000
powerstat_package,host=ubuntu,package_id=0,active_cores=0 max_turbo_frequency_mhz=3000i 1606494744000000000
powerstat_package,host=ubuntu,package_id=0,active_cores=1 max_turbo_frequency_mhz=2800i 1606494744000000000
powerstat_package,die=0,host=ubuntu,package_id=0,type=initial uncore_frequency_limit_mhz_min=800,uncore_frequency_limit_mhz_max=2400 1606494744000000000
powerstat_package,die=0,host=ubuntu,package_id=0,type=current uncore_frequency_mhz_cur=800i,uncore_frequency_limit_mhz_min=800,uncore_frequency_limit_mhz_max=2400 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_frequency_mhz=1200.29 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_temperature_celsius=34i 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c0_state_residency_percent=0.8 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c1_state_residency_percent=6.68 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c3_state_residency_percent=0 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c6_state_residency_percent=92.52 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c7_state_residency_percent=0 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_busy_frequency_mhz=1213.24 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c0_substate_c01_percent=0 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c0_substate_c02_percent=5.68 1606494744000000000
powerstat_core,core_id=0,cpu_id=0,host=ubuntu,package_id=0 cpu_c0_substate_c0_wait_percent=43.74 1606494744000000000
```

## Supported CPU models

| Model number | Processor name                  | `cpu_c1_state_residency`<br/>`cpu_c6_state_residency`<br/>`cpu_temperature`<br/>`cpu_base_frequency` | `cpu_c3_state_residency` | `cpu_c7_state_residency` | `uncore_frequency` |
|--------------|---------------------------------|:----------------------------------------------------------------------------------------------------:|:------------------------:|:------------------------:|:------------------:|
| 0x1E         | Intel Nehalem                   |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x1F         | Intel Nehalem-G                 |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x1A         | Intel Nehalem-EP                |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x2E         | Intel Nehalem-EX                |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x25         | Intel Westmere                  |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x2C         | Intel Westmere-EP               |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x2F         | Intel Westmere-EX               |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x2A         | Intel Sandybridge               |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x2D         | Intel Sandybridge-X             |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x3A         | Intel Ivybridge                 |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x3E         | Intel Ivybridge-X               |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x3C         | Intel Haswell                   |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x3F         | Intel Haswell-X                 |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x45         | Intel Haswell-L                 |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x46         | Intel Haswell-G                 |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x3D         | Intel Broadwell                 |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x47         | Intel Broadwell-G               |                                                  ✓                                                   |            ✓             |            ✓             |         ✓          |
| 0x4F         | Intel Broadwell-X               |                                                  ✓                                                   |            ✓             |                          |         ✓          |
| 0x56         | Intel Broadwell-D               |                                                  ✓                                                   |            ✓             |                          |         ✓          |
| 0x4E         | Intel Skylake-L                 |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x5E         | Intel Skylake                   |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x55         | Intel Skylake-X                 |                                                  ✓                                                   |                          |                          |         ✓          |
| 0x8E         | Intel KabyLake-L                |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x9E         | Intel KabyLake                  |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0xA5         | Intel CometLake                 |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0xA6         | Intel CometLake-L               |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x66         | Intel CannonLake-L              |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x6A         | Intel IceLake-X                 |                                                  ✓                                                   |                          |                          |         ✓          |
| 0x6C         | Intel IceLake-D                 |                                                  ✓                                                   |                          |                          |         ✓          |
| 0x7D         | Intel IceLake                   |                                                  ✓                                                   |                          |                          |                    |
| 0x7E         | Intel IceLake-L                 |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x9D         | Intel IceLake-NNPI              |                                                  ✓                                                   |                          |            ✓             |                    |
| 0xA7         | Intel RocketLake                |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x8C         | Intel TigerLake-L               |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x8D         | Intel TigerLake                 |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x8F         | Intel Sapphire Rapids X         |                                                  ✓                                                   |                          |                          |         ✓          |
| 0xCF         | Intel Emerald Rapids X          |                                                  ✓                                                   |                          |                          |         ✓          |
| 0xAD         | Intel Granite Rapids X          |                                                  ✓                                                   |                          |                          |                    |
| 0xAE         | Intel Granite Rapids D          |                                                  ✓                                                   |                          |                          |                    |
| 0x8A         | Intel Lakefield                 |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x97         | Intel AlderLake                 |                                                  ✓                                                   |                          |            ✓             |         ✓          |
| 0x9A         | Intel AlderLake-L               |                                                  ✓                                                   |                          |            ✓             |         ✓          |
| 0xB7         | Intel RaptorLake                |                                                  ✓                                                   |                          |            ✓             |         ✓          |
| 0xBA         | Intel RaptorLake-P              |                                                  ✓                                                   |                          |            ✓             |         ✓          |
| 0xBF         | Intel RaptorLake-S              |                                                  ✓                                                   |                          |            ✓             |         ✓          |
| 0xAC         | Intel MeteorLake                |                                                  ✓                                                   |                          |            ✓             |         ✓          |
| 0xAA         | Intel MeteorLake-L              |                                                  ✓                                                   |                          |            ✓             |         ✓          |
| 0xC6         | Intel ArrowLake                 |                                                  ✓                                                   |                          |            ✓             |                    |
| 0xBD         | Intel LunarLake                 |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x37         | Intel Atom® Bay Trail           |                                                  ✓                                                   |                          |                          |                    |
| 0x4D         | Intel Atom® Avaton              |                                                  ✓                                                   |                          |                          |                    |
| 0x4A         | Intel Atom® Merrifield          |                                                  ✓                                                   |                          |                          |                    |
| 0x5A         | Intel Atom® Moorefield          |                                                  ✓                                                   |                          |                          |                    |
| 0x4C         | Intel Atom® Airmont             |                                                  ✓                                                   |            ✓             |                          |                    |
| 0x5C         | Intel Atom® Apollo Lake         |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x5F         | Intel Atom® Denverton           |                                                  ✓                                                   |                          |                          |                    |
| 0x7A         | Intel Atom® Goldmont            |                                                  ✓                                                   |            ✓             |            ✓             |                    |
| 0x86         | Intel Atom® Jacobsville         |                                                  ✓                                                   |                          |                          |                    |
| 0x96         | Intel Atom® Elkhart Lake        |                                                  ✓                                                   |                          |            ✓             |                    |
| 0x9C         | Intel Atom® Jasper Lake         |                                                  ✓                                                   |                          |            ✓             |                    |
| 0xBE         | Intel AlderLake-N               |                                                  ✓                                                   |                          |            ✓             |                    |
| 0xAF         | Intel Sierra Forest             |                                                  ✓                                                   |                          |                          |                    |
| 0xB6         | Intel Grand Ridge               |                                                  ✓                                                   |                          |                          |                    |
| 0x57         | Intel Xeon® PHI Knights Landing |                                                  ✓                                                   |                          |                          |                    |
| 0x85         | Intel Xeon® PHI Knights Mill    |                                                  ✓                                                   |                          |                          |                    |
