# HTTP Listener v2 Input Plugin

This plugin listens for metrics sent via HTTP in any of the supported
[data formats][data_formats].

> [!NOTE]
> If you would like Telegraf to act as a proxy/relay for InfluxDB v1 or
> InfluxDB v2 it is recommended to use the
> [influxdb__listener][influxdb_listener] or
> [influxdb_v2_listener][influxdb_v2_listener] plugin instead.

⭐ Telegraf v1.9.0
🏷️ server
💻 all

[data_formats]: /docs/DATA_FORMATS_INPUT.md
[influxdb_listener]: /plugins/inputs/influxdb_listener/README.md
[influxdb_v2_listener]: /plugins/inputs/influxdb_v2_listener/README.md

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

## Configuration

```toml @sample.conf
# Generic HTTP write listener
[[inputs.http_listener_v2]]
  ## Address to host HTTP listener on
  ## can be prefixed by protocol tcp, or unix if not provided defaults to tcp
  ## if unix network type provided it should be followed by absolute path for unix socket
  service_address = "tcp://:8080"
  ## service_address = "tcp://:8443"
  ## service_address = "unix:///tmp/telegraf.sock"

  ## Permission for unix sockets (only available for unix sockets)
  ## This setting may not be respected by some platforms. To safely restrict
  ## permissions it is recommended to place the socket into a previously
  ## created directory with the desired permissions.
  ##   ex: socket_mode = "777"
  # socket_mode = ""

  ## Paths to listen to.
  # paths = ["/telegraf"]

  ## Save path as http_listener_v2_path tag if set to true
  # path_tag = false

  ## Save query parameters as tagg if set to true
  # query_tag = false

  ## HTTP methods to accept.
  # methods = ["POST", "PUT"]

  ## Optional HTTP headers
  ## These headers are applied to the server that is listening for HTTP
  ## requests and included in responses.
  # http_headers = {"HTTP_HEADER" = "TAG_NAME"}

  ## HTTP Return Success Code
  ## This is the HTTP code that will be returned on success
  # http_success_code = 204

  ## maximum duration before timing out read of the request
  # read_timeout = "10s"
  ## maximum duration before timing out write of the response
  # write_timeout = "10s"

  ## Maximum allowed http request body size in bytes.
  ## 0 means to use the default of 524,288,000 bytes (500 mebibytes)
  # max_body_size = "500MB"

  ## Part of the request to consume.  Available options are "body" and
  ## "query".
  # data_source = "body"

  ## Set one or more allowed client CA certificate file names to
  ## enable mutually authenticated TLS connections
  # tls_allowed_cacerts = ["/etc/telegraf/clientca.pem"]

  ## Add service certificate and key
  # tls_cert = "/etc/telegraf/cert.pem"
  # tls_key = "/etc/telegraf/key.pem"

  ## Minimal TLS version accepted by the server
  # tls_min_version = "TLS12"

  ## Optional username and password to accept for HTTP basic authentication.
  ## You probably want to make sure you have TLS configured above for this.
  # basic_username = "foobar"
  # basic_password = "barfoo"

  ## Optional setting to map http headers into tags
  ## If the http header is not present on the request, no corresponding tag will be added
  ## If multiple instances of the http header are present, only the first value will be used
  # http_header_tags = {"HTTP_HEADER" = "TAG_NAME"}

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "influx"
```

## Metrics

Metrics are collected from the part of the request specified by the
`data_source` param and are parsed depending on the value of `data_format`.

## Example Output

## Troubleshooting

Send Line Protocol:

```shell
curl -i -XPOST 'http://localhost:8080/telegraf' --data-binary 'cpu_load_short,host=server01,region=us-west value=0.64 1434055562000000000'
```

Send JSON:

```shell
curl -i -XPOST 'http://localhost:8080/telegraf' --data-binary '{"value1": 42, "value2": 42}'
```

Send query params:

```shell
curl -i -XGET 'http://localhost:8080/telegraf?host=server01&value=0.42'
```
