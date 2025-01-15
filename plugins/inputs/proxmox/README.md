# Proxmox Input Plugin

The proxmox plugin gathers metrics about containers and VMs using the Proxmox
API.

Telegraf minimum version: Telegraf 1.16.0

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Provides metrics from Proxmox nodes (Proxmox Virtual Environment > 6.2).
[[inputs.proxmox]]
  ## API connection configuration. The API token was introduced in Proxmox v6.2.
  ## Required permissions for user and token: PVEAuditor role on /.
  base_url = "https://localhost:8006/api2/json"
  api_token = "USER@REALM!TOKENID=UUID"

  ## Node name, defaults to OS hostname
  ## Unless Telegraf is on the same host as Proxmox, setting this is required
  ## for Telegraf to successfully connect to Proxmox. If not on the same host,
  ## leaving this empty will often lead to a "search domain is not set" error.
  # node_name = ""

  ## Additional tags of the VM stats data to add as a tag
  ## Supported values are "vmid" and "status"
  # additional_vmstats_tags = []

  ## Optional TLS Config
  # tls_ca = "/etc/telegraf/ca.pem"
  # tls_cert = "/etc/telegraf/cert.pem"
  # tls_key = "/etc/telegraf/key.pem"
  ## Use TLS but skip chain & host verification
  # insecure_skip_verify = false

  ## HTTP response timeout (default: 5s)
  # response_timeout = "5s"
```

### Permissions

The plugin will need to have access to the Proxmox API. In Proxmox API tokens
are a subset of the corresponding user. This means an API token cannot execute
commands that the user cannot either.

For Telegraf, an API token and user must be provided with at least the
PVEAuditor role on /. Below is an example of creating a telegraf user and token
and then ensuring the user and token have the correct role:

```s
## Create a influx user with PVEAuditor role
pveum user add influx@pve
pveum acl modify / -role PVEAuditor -user influx@pve
## Create a token with the PVEAuditor role
pveum user token add influx@pve monitoring -privsep 1
pveum acl modify / -role PVEAuditor -token 'influx@pve!monitoring'
```

See this [Proxmox docs example][1] for further details.

[1]: https://pve.proxmox.com/wiki/User_Management#_limited_api_token_for_monitoring

## Metrics

- proxmox
  - status
  - uptime
  - cpuload
  - mem_used
  - mem_total
  - mem_free
  - mem_used_percentage
  - swap_used
  - swap_total
  - swap_free
  - swap_used_percentage
  - disk_used
  - disk_total
  - disk_free
  - disk_used_percentage

### Tags

- node_fqdn - FQDN of the node telegraf is running on
- vm_name - Name of the VM/container
- vm_fqdn - FQDN of the VM/container
- vm_type - Type of the VM/container (lxc, qemu)
- vm_id - ID of the VM/container

## Example Output

```text
proxmox,host=pxnode,node_fqdn=pxnode.example.com,vm_fqdn=vm1.example.com,vm_id=112,vm_name=vm1,vm_type=lxc cpuload=0.147998116735236,disk_free=4461129728i,disk_total=5217320960i,disk_used=756191232i,disk_used_percentage=14,mem_free=1046827008i,mem_total=1073741824i,mem_used=26914816i,mem_used_percentage=2,status="running",swap_free=536698880i,swap_total=536870912i,swap_used=172032i,swap_used_percentage=0,uptime=1643793i 1595457277000000000
```
