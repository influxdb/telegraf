# InfiniBand Hardware Input Plugin

This plugin gathers statistics for all InfiniBand devices and ports on the
system. These are the hardware counters that can be found in
`/sys/class/infiniband/<dev>/ports/<port>/hw_counters/`

**Supported Platforms**: Linux

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Gets hardware counters from all InfiniBand cards and ports installed
# This plugin ONLY supports Linux
[[inputs.infiniband_hw]]
  # no configuration
```

## Metrics

Actual metrics depend on the InfiniBand devices, the plugin uses a simple
mapping from hw_counter -> hw_counter value.

[Information about hw_counters][hw_counters] collected is provided by Nvidia.

[hw_counters]: https://enterprise-support.nvidia.com/s/article/understanding-mlx5-linux-counters-and-status-parameters

- infiniband
  - tags:
    - device
    - port
  - fields:
    - duplicate_request (integer)
    - implied_nak_seq_err (integer)
    - lifespan (integer)
    - local_ack_timeout_err (integer)
    - np_cnp_sent (integer)
    - np_ecn_marked_roce_packets (integer)
    - out_of_buffer (integer)
    - out_of_sequence (integer)
    - packet_seq_err (integer)
    - req_cqe_error (integer)
    - req_cqe_flush_error (integer)
    - req_remote_access_errors (integer)
    - req_remote_invalid_request (integer)
    - resp_cqe_error (integer)
    - resp_cqe_flush_error (integer)
    - resp_local_length_error (integer)
    - resp_remote_access_errors (integer)
    - rnr_nak_retry_err (integer)
    - roce_adp_retrans (integer)
    - roce_adp_retrans_to (integer)
    - roce_slow_restart (integer)
    - roce_slow_restart_cnps (integer)
    - roce_slow_restart_trans (integer)
    - rp_cnp_handled (integer)
    - rp_cnp_ignored (integer)
    - rx_atomic_requests (integer)
    - rx_icrc_encapsulated (integer)
    - rx_read_requests (integer)
    - rx_write_requests (integer)

## Example Output

```text
infiniband_hw,device=mlx5_4,host=host1,port=1 local_ack_timeout_err=0i,req_cqe_error=0i,roce_slow_restart=0i,roce_adp_retrans=0i,rx_atomic_requests=0i,np_ecn_marked_roce_packets=0i,rp_cnp_handled=0i,req_remote_access_errors=0i,np_cnp_sent=0i,resp_cqe_error=0i,out_of_sequence=0i,roce_slow_restart_cnps=0i,req_remote_invalid_request=0i,implied_nak_seq_err=0i,rp_cnp_ignored=0i,resp_local_length_error=0i,lifespan=10i,out_of_buffer=0i,rx_write_requests=0i,resp_cqe_flush_error=0i,rx_icrc_encapsulated=0i,rx_read_requests=0i,resp_remote_access_errors=0i,roce_adp_retrans_to=0i,roce_slow_restart_trans=0i,rnr_nak_retry_err=0i,req_cqe_flush_error=0i,packet_seq_err=0i,duplicate_request=0i 1734520190000000000
```
