# Procstat Input Plugin

The procstat plugin can be used to monitor the system resource usage of one or
more processes.  The procstat_lookup metric displays the query information,
specifically the number of PIDs returned on a search

Processes can be selected for monitoring using one of several methods:

- pidfile
- exe
- pattern
- user
- systemd_unit
- cgroup
- supervisor_unit
- win_service

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Monitor process cpu and memory usage
[[inputs.procstat]]
  ## PID file to monitor process
  pid_file = "/var/run/nginx.pid"
  ## executable name (ie, pgrep <exe>)
  # exe = "nginx"
  ## pattern as argument for pgrep (ie, pgrep -f <pattern>)
  # pattern = "nginx"
  ## user as argument for pgrep (ie, pgrep -u <user>)
  # user = "nginx"
  ## Systemd unit name, supports globs when include_systemd_children is set to true
  # systemd_unit = "nginx.service"
  # include_systemd_children = false
  ## CGroup name or path, supports globs
  # cgroup = "systemd/system.slice/nginx.service"
  ## Supervisor service names of hypervisorctl management
  # supervisor_units = ["webserver", "proxy"]

  ## Windows service name
  # win_service = ""

  ## override for process_name
  ## This is optional; default is sourced from /proc/<pid>/status
  # process_name = "bar"

  ## Field name prefix
  # prefix = ""

  ## Mode to use when calculating CPU usage. Can be one of 'solaris' or 'irix'.
  # mode = "irix"

  ## Add the given information tag instead of a field
  ## This allows to create unique metrics/series when collecting processes with
  ## otherwise identical tags. However, please be careful as this can easily
  ## result in a large number of series, especially with short-lived processes,
  ## creating high cardinality at the output.
  ## Available options are:
  ##   cmdline     -- full commandline
  ##   pid         -- ID of the process
  ##   ppid        -- ID of the process' parent
  ##   status      -- state of the process
  ##   user        -- username owning the process
  ## socket only options:
  ##   protocol  -- protocol type of the process socket
  ##   state     -- state of the process socket
  ##   src       -- source address of the process socket (non-unix sockets)
  ##   src_port  -- source port of the process socket (non-unix sockets)
  ##   dest      -- destination address of the process socket (non-unix sockets)
  ##   dest_port -- destination port of the process socket (non-unix sockets)
  ##   name      -- name of the process socket (unix sockets only)
  ## tag only available for procstat_lookup
  ##   level       -- level of the process filtering
  # tag_with = []

  ## Properties to collect
  ## Available options are
  ##   cpu     -- CPU usage statistics
  ##   limits  -- set resource limits
  ##   memory  -- memory usage statistics
  ##   mmap    -- mapped memory usage statistics (caution: can cause high load)
  ##   sockets -- socket statistics for protocols in 'socket_protocols'
  # properties = ["cpu", "limits", "memory", "mmap"]

  ## Protocol filter for the sockets property
  ## Available options are
  ##   all  -- all of the protocols below
  ##   tcp4 -- TCP socket statistics for IPv4
  ##   tcp6 -- TCP socket statistics for IPv6
  ##   udp4 -- UDP socket statistics for IPv4
  ##   udp6 -- UDP socket statistics for IPv6
  ##   unix -- Unix socket statistics
  # socket_protocols = ["all"]

  ## Method to use when finding process IDs.  Can be one of 'pgrep', or
  ## 'native'.  The pgrep finder calls the pgrep executable in the PATH while
  ## the native finder performs the search directly in a manor dependent on the
  ## platform.  Default is 'pgrep'
  # pid_finder = "pgrep"

  ## New-style filtering configuration (multiple filter sections are allowed)
  # [[inputs.procstat.filter]]
  #    ## Name of the filter added as 'filter' tag
  #    name = "shell"
  #
  #    ## Service filters, only one is allowed
  #    ## Systemd unit names (wildcards are supported)
  #    # systemd_units = []
  #    ## CGroup name or path (wildcards are supported)
  #    # cgroups = []
  #    ## Supervisor service names of hypervisorctl management
  #    # supervisor_units = []
  #    ## Windows service names
  #    # win_service = []
  #
  #    ## Process filters, multiple are allowed
  #    ## Regular expressions to use for matching against the full command
  #    # patterns = ['.*']
  #    ## List of users owning the process (wildcards are supported)
  #    # users = ['*']
  #    ## List of executable paths of the process (wildcards are supported)
  #    # executables = ['*']
  #    ## List of process names (wildcards are supported)
  #    # process_names = ['*']
  #    ## Recursion depth for determining children of the matched processes
  #    ## A negative value means all children with infinite depth
  #    # recursion_depth = 0
```

### Windows support

Preliminary support for Windows has been added, however you may prefer using
the `win_perf_counters` input plugin as a more mature alternative.

### Darwin specifics

If you use this plugin with `supervisor_units` *and* `pattern` on Darwin, you
**have to** use the `pgrep` finder as the underlying library relies on `pgrep`.

### Permissions

Some files or directories may require elevated permissions. As such a user may
need to provide telegraf with higher levels of permissions to access and produce
metrics.

## Metrics

For descriptions of these tags and fields, consider reading one of the
following:

- [Linux Kernel /proc Filesystem][kernel /proc]
- [proc manpage][manpage]

[kernel /proc]: https://www.kernel.org/doc/html/latest/filesystems/proc.html
[manpage]: https://man7.org/linux/man-pages/man5/proc.5.html

Below are an example set of tags and fields:

- procstat
  - tags:
    - pid (if requested)
    - cmdline (if requested)
    - process_name
    - pidfile (when defined)
    - exe (when defined)
    - pattern (when defined)
    - user (when selected)
    - systemd_unit (when defined)
    - cgroup (when defined)
    - cgroup_full (when cgroup or systemd_unit is used with glob)
    - supervisor_unit (when defined)
    - win_service (when defined)
    - parent_pid for child processes
    - child_level for child processes
  - fields:
    - child_major_faults (int)
    - child_minor_faults (int)
    - created_at (int) [epoch in nanoseconds]
    - cpu_time (int)
    - cpu_time_iowait (float) (zero for all OSes except Linux)
    - cpu_time_system (float)
    - cpu_time_user (float)
    - cpu_usage (float)
    - disk_read_bytes (int, Linux only, *telegraf* may need to be ran as **root**)
    - disk_write_bytes (int, Linux only, *telegraf* may need to be ran as **root**)
    - involuntary_context_switches (int)
    - major_faults (int)
    - memory_anonymous (int)
    - memory_private_clean (int)
    - memory_private_dirty (int)
    - memory_pss (int)
    - memory_referenced (int)
    - memory_rss (int)
    - memory_shared_clean (int)
    - memory_shared_dirty (int)
    - memory_size (int)
    - memory_swap (int)
    - memory_usage (float)
    - memory_vms (int)
    - minor_faults (int)
    - nice_priority (int)
    - num_fds (int, *telegraf* may need to be ran as **root**)
    - num_threads (int)
    - pid (int)
    - ppid (int)
    - status (string)
    - read_bytes (int, *telegraf* may need to be ran as **root**)
    - read_count (int, *telegraf* may need to be ran as **root**)
    - realtime_priority (int)
    - rlimit_cpu_time_hard (int)
    - rlimit_cpu_time_soft (int)
    - rlimit_file_locks_hard (int)
    - rlimit_file_locks_soft (int)
    - rlimit_memory_data_hard (int)
    - rlimit_memory_data_soft (int)
    - rlimit_memory_locked_hard (int)
    - rlimit_memory_locked_soft (int)
    - rlimit_memory_rss_hard (int)
    - rlimit_memory_rss_soft (int)
    - rlimit_memory_stack_hard (int)
    - rlimit_memory_stack_soft (int)
    - rlimit_memory_vms_hard (int)
    - rlimit_memory_vms_soft (int)
    - rlimit_nice_priority_hard (int)
    - rlimit_nice_priority_soft (int)
    - rlimit_num_fds_hard (int)
    - rlimit_num_fds_soft (int)
    - rlimit_realtime_priority_hard (int)
    - rlimit_realtime_priority_soft (int)
    - rlimit_signals_pending_hard (int)
    - rlimit_signals_pending_soft (int)
    - signals_pending (int)
    - voluntary_context_switches (int)
    - write_bytes (int, *telegraf* may need to be ran as **root**)
    - write_count (int, *telegraf* may need to be ran as **root**)
- procstat_lookup
  - tags:
    - exe
    - pid_finder
    - pid_file
    - pattern
    - prefix
    - user
    - systemd_unit
    - cgroup
    - supervisor_unit
    - win_service
    - result
  - fields:
    - pid_count (int)
    - running (int)
    - result_code (int, success = 0, lookup_error = 1)
- procstat_socket (if configured, Linux only)
  - tags:
    - pid (if requested)
    - protocol (if requested)
    - cmdline (if requested)
    - process_name
    - pidfile (when defined)
    - exe (when defined)
    - pattern (when defined)
    - user (when selected)
    - systemd_unit (when defined)
    - cgroup (when defined)
    - cgroup_full (when cgroup or systemd_unit is used with glob)
    - supervisor_unit (when defined)
    - win_service (when defined)
  - fields:
    - protocol
    - state
    - pid
    - src
    - src_port (tcp and udp sockets only)
    - dest (tcp and udp sockets only)
    - dest_port (tcp and udp sockets only)
    - bytes_received (tcp sockets only)
    - bytes_sent (tcp sockets only)
    - lost (tcp sockets only)
    - retransmits (tcp sockets only)
    - rx_queue
    - tx_queue
    - inode (unix sockets only)

*NOTE: Resource limit > 2147483647 will be reported as 2147483647.*

## Example Output

```text
procstat_lookup,host=prash-laptop,pattern=influxd,pid_finder=pgrep,result=success pid_count=1i,running=1i,result_code=0i 1582089700000000000
procstat,host=prash-laptop,pattern=influxd,process_name=influxd,user=root involuntary_context_switches=151496i,child_minor_faults=1061i,child_major_faults=8i,cpu_time_user=2564.81,pid=32025i,major_faults=8609i,created_at=1580107536000000000i,voluntary_context_switches=1058996i,cpu_time_system=616.98,memory_swap=0i,memory_locked=0i,memory_usage=1.7797634601593018,num_threads=18i,cpu_time_iowait=0,memory_rss=148643840i,memory_vms=1435688960i,memory_data=0i,memory_stack=0i,minor_faults=1856550i 1582089700000000000
procstat_socket,host=prash-laptop,process_name=browser,protocol=tcp4 bytes_received=826987i,bytes_sent=32869i,dest="192.168.0.2",dest_port=443i,lost=0i,pid=32025i,retransmits=0i,rx_queue=0i,src="192.168.0.1",src_port=52106i,state="established",tx_queue=0i 1582089700000000000
```
