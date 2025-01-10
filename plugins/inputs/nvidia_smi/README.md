# Nvidia System Management Interface (SMI) Input Plugin

This plugin uses a query on the
[`nvidia-smi`](https://developer.nvidia.com/nvidia-system-management-interface)
binary to pull GPU stats including memory and GPU usage, temp and other.

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Startup error behavior options

In addition to the plugin-specific and global configuration settings the plugin
supports options for specifying the behavior when experiencing startup errors
using the `startup_error_behavior` setting. Available values are:

- `error`:  Telegraf with stop and exit in case of startup errors. This is the
            default behavior.
- `ignore`: Telegraf will ignore startup errors for this plugin and disables it
            but continues processing for all other plugins.
- `retry`:  NOT AVAILABLE

## Configuration

```toml @sample.conf
# Pulls statistics from nvidia GPUs attached to the host
[[inputs.nvidia_smi]]
  ## Optional: path to nvidia-smi binary, defaults "/usr/bin/nvidia-smi"
  ## We will first try to locate the nvidia-smi binary with the explicitly specified value (or default value),
  ## if it is not found, we will try to locate it on PATH(exec.LookPath), if it is still not found, an error will be returned
  # bin_path = "/usr/bin/nvidia-smi"

  ## Optional: timeout for GPU polling
  # timeout = "5s"
```

### Linux

On Linux, `nvidia-smi` is generally located at `/usr/bin/nvidia-smi`

### Windows

On Windows, `nvidia-smi` is generally located at `C:\Program Files\NVIDIA
Corporation\NVSMI\nvidia-smi.exe` On Windows 10, you may also find this located
here `C:\Windows\System32\nvidia-smi.exe`

You'll need to escape the `\` within the `telegraf.conf` like this: `C:\\Program
Files\\NVIDIA Corporation\\NVSMI\\nvidia-smi.exe`

## Metrics

- measurement: `nvidia_smi`
  - tags
    - `name` (type of GPU e.g. `GeForce GTX 1070 Ti`)
    - `compute_mode` (The compute mode of the GPU e.g. `Default`)
    - `index` (The port index where the GPU is connected to the motherboard e.g. `1`)
    - `pstate` (Overclocking state for the GPU e.g. `P0`)
    - `uuid` (A unique identifier for the GPU e.g. `GPU-f9ba66fc-a7f5-94c5-da19-019ef2f9c665`)
  - fields
    - `fan_speed` (integer, percentage)
    - `fbc_stats_session_count` (integer)
    - `fbc_stats_average_fps` (integer)
    - `fbc_stats_average_latency` (integer)
    - `memory_free` (integer, MiB)
    - `memory_used` (integer, MiB)
    - `memory_total` (integer, MiB)
    - `memory_reserved` (integer, MiB)
    - `retired_pages_multiple_single_bit` (integer)
    - `retired_pages_double_bit` (integer)
    - `retired_pages_blacklist` (string)
    - `retired_pages_pending` (string)
    - `remapped_rows_correctable` (int)
    - `remapped_rows_uncorrectable` (int)
    - `remapped_rows_pending` (string)
    - `remapped_rows_pending` (string)
    - `remapped_rows_failure` (string)
    - `power_draw` (float, W)
    - `temperature_gpu` (integer, degrees C)
    - `utilization_gpu` (integer, percentage)
    - `utilization_memory` (integer, percentage)
    - `utilization_encoder` (integer, percentage)
    - `utilization_decoder` (integer, percentage)
    - `pcie_link_gen_current` (integer)
    - `pcie_link_width_current` (integer)
    - `encoder_stats_session_count` (integer)
    - `encoder_stats_average_fps` (integer)
    - `encoder_stats_average_latency` (integer)
    - `clocks_current_graphics` (integer, MHz)
    - `clocks_current_sm` (integer, MHz)
    - `clocks_current_memory` (integer, MHz)
    - `clocks_current_video` (integer, MHz)
    - `driver_version` (string)
    - `cuda_version` (string)

## Sample Query

The below query could be used to alert on the average temperature of the your
GPUs over the last minute

```sql
SELECT mean("temperature_gpu") FROM "nvidia_smi" WHERE time > now() - 5m GROUP BY time(1m), "index", "name", "host"
```

## Troubleshooting

Check the full output by running `nvidia-smi` binary manually.

Linux:

```sh
sudo -u telegraf -- /usr/bin/nvidia-smi -q -x
```

Windows:

```sh
"C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe" -q -x
```

Please include the output of this command if opening an GitHub issue.

## Example Output

```text
nvidia_smi,compute_mode=Default,host=8218cf,index=0,name=GeForce\ GTX\ 1070,pstate=P2,uuid=GPU-823bc202-6279-6f2c-d729-868a30f14d96 fan_speed=100i,memory_free=7563i,memory_total=8112i,memory_used=549i,temperature_gpu=53i,utilization_gpu=100i,utilization_memory=90i 1523991122000000000
nvidia_smi,compute_mode=Default,host=8218cf,index=1,name=GeForce\ GTX\ 1080,pstate=P2,uuid=GPU-f9ba66fc-a7f5-94c5-da19-019ef2f9c665 fan_speed=100i,memory_free=7557i,memory_total=8114i,memory_used=557i,temperature_gpu=50i,utilization_gpu=100i,utilization_memory=85i 1523991122000000000
nvidia_smi,compute_mode=Default,host=8218cf,index=2,name=GeForce\ GTX\ 1080,pstate=P2,uuid=GPU-d4cfc28d-0481-8d07-b81a-ddfc63d74adf fan_speed=100i,memory_free=7557i,memory_total=8114i,memory_used=557i,temperature_gpu=58i,utilization_gpu=100i,utilization_memory=86i 1523991122000000000
```

## Limitations

Note that there seems to be an issue with getting current memory clock values
when the memory is overclocked.  This may or may not apply to everyone but it's
confirmed to be an issue on an EVGA 2080 Ti.

**NOTE:** For use with docker either generate your own custom docker image based
on nvidia/cuda which also installs a telegraf package or use [volume mount
binding](https://docs.docker.com/storage/bind-mounts/) to inject the required
binary into the docker container. In particular you will need to pass through
the /dev/nvidia* devices, the nvidia-smi binary and the nvidia libraries.
An minimal docker-compose example of how to do this is:

```yaml
  telegraf:
    image: telegraf
    runtime: nvidia
    devices:
      - /dev/nvidiactl:/dev/nvidiactl
      - /dev/nvidia0:/dev/nvidia0
    volumes:
      - ./telegraf/etc/telegraf.conf:/etc/telegraf/telegraf.conf:ro
      - /usr/bin/nvidia-smi:/usr/bin/nvidia-smi:ro
      - /usr/lib/x86_64-linux-gnu/nvidia:/usr/lib/x86_64-linux-gnu/nvidia:ro
    environment:
      - LD_PRELOAD=/usr/lib/x86_64-linux-gnu/nvidia/current/libnvidia-ml.so
```
