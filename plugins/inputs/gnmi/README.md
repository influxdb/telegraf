# gNMI (gRPC Network Management Interface) Input Plugin

This plugin consumes telemetry data based on [gNMI][gnmi] subscriptions. TLS is
supported for authentication and encryption. This plugin is vendor-agnostic and
is supported on any platform that supports the gNMI specification.

For Cisco devices the plugin has been optimized to support gNMI telemetry as
produced by Cisco IOS XR (64-bit) version 6.5.1, Cisco NX-OS 9.3 and
Cisco IOS XE 16.12 and later.

⭐ Telegraf v1.15.0
🏷️ network
💻 all

[gnmi]: https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md

## Service Input <!-- @/docs/includes/service_input.md -->

This plugin is a service input. Normal plugins gather metrics determined by the
interval setting. Service plugins start a service to listens and waits for
metrics or events to occur. Service plugins have two key differences from
normal plugins:

1. The global or plugin specific `interval` setting may not apply
2. The CLI options of `--test`, `--test-wait`, and `--once` may not produce
   output for this plugin

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Secret-store support

This plugin supports secrets from secret-stores for the `username` and
`password` options. See the [secret-store documentation][SECRETSTORE] for more
details on how to use them.

[SECRETSTORE]: ../../../docs/CONFIGURATION.md#secret-store-secrets

## Configuration

```toml @sample.conf
# gNMI telemetry input plugin
[[inputs.gnmi]]
  ## Address and port of the gNMI GRPC server
  addresses = ["10.49.234.114:57777"]

  ## define credentials
  username = "cisco"
  password = "cisco"

  ## gNMI encoding requested (one of: "proto", "json", "json_ietf", "bytes")
  # encoding = "proto"

  ## redial in case of failures after
  # redial = "10s"

  ## gRPC Keepalive settings
  ## See https://pkg.go.dev/google.golang.org/grpc/keepalive
  ## The client will ping the server to see if the transport is still alive if it has
  ## not see any activity for the given time.
  ## If not set, none of the keep-alive setting (including those below) will be applied.
  ## If set and set below 10 seconds, the gRPC library will apply a minimum value of 10s will be used instead.
  # keepalive_time = ""

  ## Timeout for seeing any activity after the keep-alive probe was
  ## sent. If no activity is seen the connection is closed.
  # keepalive_timeout = ""

  ## gRPC Maximum Message Size
  # max_msg_size = "4MB"

  ## Subtree depth for depth extension (disables if < 1)
  ## see https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-depth.md
  # depth = 0

  ## Enable to get the canonical path as field-name
  # canonical_field_names = false

  ## Remove leading slashes and dots in field-name
  # trim_field_names = false

  ## Only receive updates for the state, also suppresses receiving the initial state
  # updates_only = false

  ## Guess the path-tag if an update does not contain a prefix-path
  ## Supported values are
  ##   none         -- do not add a 'path' tag
  ##   common path  -- use the common path elements of all fields in an update
  ##   subscription -- use the subscription path
  # path_guessing_strategy = "none"

  ## Prefix tags from path keys with the path element
  # prefix_tag_key_with_path = false

  ## Optional client-side TLS to authenticate the device
  ## Set to true/false to enforce TLS being enabled/disabled. If not set,
  ## enable TLS only if any of the other options are specified.
  # tls_enable =
  ## Trusted root certificates for server
  # tls_ca = "/path/to/cafile"
  ## Used for TLS client certificate authentication
  # tls_cert = "/path/to/certfile"
  ## Used for TLS client certificate authentication
  # tls_key = "/path/to/keyfile"
  ## Password for the key file if it is encrypted
  # tls_key_pwd = ""
  ## Send the specified TLS server name via SNI
  # tls_server_name = "kubernetes.example.com"
  ## Minimal TLS version to accept by the client
  # tls_min_version = "TLS12"
  ## List of ciphers to accept, by default all secure ciphers will be accepted
  ## See https://pkg.go.dev/crypto/tls#pkg-constants for supported values.
  ## Use "all", "secure" and "insecure" to add all support ciphers, secure
  ## suites or insecure suites respectively.
  # tls_cipher_suites = ["secure"]
  ## Renegotiation method, "never", "once" or "freely"
  # tls_renegotiation_method = "never"
  ## Use TLS but skip chain & host verification
  # insecure_skip_verify = false

  ## gNMI subscription prefix (optional, can usually be left empty)
  ## See: https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md#222-paths
  # origin = ""
  # prefix = ""
  # target = ""

  ## Vendor specific options
  ## This defines what vendor specific options to load.
  ## * Juniper Header Extension (juniper_header): some sensors are directly managed by
  ##   Linecard, which adds the Juniper GNMI Header Extension. Enabling this
  ##   allows the decoding of the Extension header if present. Currently this knob
  ##   adds component, component_id & sub_component_id as additional tags
  # vendor_specific = []

  ## YANG model paths for decoding IETF JSON payloads
  ## Model files are loaded recursively from the given directories. Disabled if
  ## no models are specified.
  # yang_model_paths = []

  ## Define additional aliases to map encoding paths to measurement names
  # [inputs.gnmi.aliases]
  #   ifcounters = "openconfig:/interfaces/interface/state/counters"

  [[inputs.gnmi.subscription]]
    ## Name of the measurement that will be emitted
    name = "ifcounters"

    ## Origin and path of the subscription
    ## See: https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md#222-paths
    ##
    ## origin usually refers to a (YANG) data model implemented by the device
    ## and path to a specific substructure inside it that should be subscribed
    ## to (similar to an XPath). YANG models can be found e.g. here:
    ## https://github.com/YangModels/yang/tree/master/vendor/cisco/xr
    origin = "openconfig-interfaces"
    path = "/interfaces/interface/state/counters"

    ## Subscription mode ("target_defined", "sample", "on_change") and interval
    subscription_mode = "sample"
    sample_interval = "10s"

    ## Suppress redundant transmissions when measured values are unchanged
    # suppress_redundant = false

    ## If suppression is enabled, send updates at least every X seconds anyway
    # heartbeat_interval = "60s"

  ## Tag subscriptions are applied as tags to other subscriptions.
  # [[inputs.gnmi.tag_subscription]]
  #  ## When applying this value as a tag to other metrics, use this tag name
  #  name = "descr"
  #
  #  ## All other subscription fields are as normal
  #  origin = "openconfig-interfaces"
  #  path = "/interfaces/interface/state"
  #  subscription_mode = "on_change"
  #
  #  ## Match strategy to use for the tag.
  #  ## Tags are only applied for metrics of the same address. The following
  #  ## settings are valid:
  #  ##   unconditional -- always match
  #  ##   name          -- match by the "name" key
  #  ##                    This resembles the previous 'tag-only' behavior.
  #  ##   elements      -- match by the keys in the path filtered by the path
  #  ##                    parts specified `elements` below
  #  ## By default, 'elements' is used if the 'elements' option is provided,
  #  ## otherwise match by 'name'.
  #  # match = ""
  #
  #  ## For the 'elements' match strategy, at least one path-element name must
  #  ## be supplied containing at least one key to match on. Multiple path
  #  ## elements can be specified in any order. All given keys must be equal
  #  ## for a match.
  #  # elements = ["description", "interface"]
```

## Metrics

Each configured subscription will emit a different measurement.  Each leaf in a
GNMI SubscribeResponse Update message will produce a field reading in the
measurement. GNMI PathElement keys for leaves will attach tags to the field(s).

## Example Output

```text
ifcounters,path=openconfig-interfaces:/interfaces/interface/state/counters,host=linux,name=MgmtEth0/RP0/CPU0/0,source=10.49.234.115,descr/description=Foo in-multicast-pkts=0i,out-multicast-pkts=0i,out-errors=0i,out-discards=0i,in-broadcast-pkts=0i,out-broadcast-pkts=0i,in-discards=0i,in-unknown-protos=0i,in-errors=0i,out-unicast-pkts=0i,in-octets=0i,out-octets=0i,last-clear="2019-05-22T16:53:21Z",in-unicast-pkts=0i 1559145777425000000
ifcounters,path=openconfig-interfaces:/interfaces/interface/state/counters,host=linux,name=GigabitEthernet0/0/0/0,source=10.49.234.115,descr/description=Bar out-multicast-pkts=0i,out-broadcast-pkts=0i,in-errors=0i,out-errors=0i,in-discards=0i,out-octets=0i,in-unknown-protos=0i,in-unicast-pkts=0i,in-octets=0i,in-multicast-pkts=0i,in-broadcast-pkts=0i,last-clear="2019-05-22T16:54:50Z",out-unicast-pkts=0i,out-discards=0i 1559145777425000000
```

## Troubleshooting

### Empty metric-name warning

Some devices (e.g. Juniper) report spurious data with response paths not
corresponding to any subscription. In those cases, Telegraf will not be able
to determine the metric name for the response and you get an
*empty metric-name warning*

For example if you subscribe to `/junos/system/linecard/cpu/memory` but the
corresponding response arrives with path
`/components/component/properties/property/...` To avoid those issues, you can
manually map the response to a metric name using the `aliases` option like

```toml
[[inputs.gnmi]]
  addresses     = ["..."]

  [inputs.gnmi.aliases]
    memory = "/components"

  [[inputs.gnmi.subscription]]
    name = "memory"
    origin = "openconfig"
    path = "/junos/system/linecard/cpu/memory"
    subscription_mode = "sample"
    sample_interval = "60s"
```

If this does *not* solve the issue, please follow the warning instructions and
open an issue with the response, your configuration and the metric you expect.

### Missing `path` tag

Some devices (e.g. Arista) omit the prefix and specify the path in the update
if there is only one value reported. This leads to a missing `path` tag for
the resulting metrics. In those cases you should set `path_guessing_strategy`
to `subscription` to use the subscription path as `path` tag.

Other devices might omit the prefix in updates altogether. Here setting
`path_guessing_strategy` to `common path` can help to infer the `path` tag by
using the part of the path that is common to all values in the update.

### TLS handshake failure

When receiving an error like

```text
2024-01-01T00:00:00Z E! [inputs.gnmi] Error in plugin: failed to setup subscription: rpc error: code = Unavailable desc = connection error: desc = "transport: authentication handshake failed: remote error: tls: handshake failure"
```

this might be due to insecure TLS configurations in the GNMI server. Please
check the minimum TLS version provided by the server as well as the cipher suite
used. You might want to use the `tls_min_version` or `tls_cipher_suites` setting
respectively to work-around the issue. Please be careful to not undermine the
security of the connection between the plugin and the device!
