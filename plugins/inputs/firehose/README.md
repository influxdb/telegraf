# AWS Data Firehose Input Plugin

This plugin listens for metrics sent via HTTP from [AWS Data Firehose][firehose]
in one of the supported [data formats][data_formats].
The plugin strictly follows the request-response schema as describe in the
official [documentation][response_spec].

⭐ Telegraf v1.34.0
🏷️ cloud, messaging
💻 all

[firehose]: https://aws.amazon.com/de/firehose/
[data_formats]: /docs/DATA_FORMATS_INPUT.md
[response_spec]: https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html

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
# AWS Data Firehose listener
[[inputs.firehose]]
  ## Address and port to host HTTP listener on
  service_address = ":8080"

  ## Paths to listen to.
  # paths = ["/telegraf"]

  ## maximum duration before timing out read of the request
  # read_timeout = "5s"
  ## maximum duration before timing out write of the response
  # write_timeout = "5s"

  ## Set one or more allowed client CA certificate file names to
  ## enable mutually authenticated TLS connections
  # tls_allowed_cacerts = ["/etc/telegraf/clientca.pem"]

  ## Add service certificate and key
  # tls_cert = "/etc/telegraf/cert.pem"
  # tls_key = "/etc/telegraf/key.pem"

  ## Minimal TLS version accepted by the server
  # tls_min_version = "TLS12"

  ## Optional access key to accept for authentication.
  ## AWS Data Firehose uses "x-amz-firehose-access-key" header to set the access key.
  ## If no access_key is provided (default), authentication is completely disabled and
  ## this plugin will accept all request ignoring the provided access-key in the request!
  # access_key = "foobar"

  ## Optional setting to add parameters as tags
  ## If the http header "x-amz-firehose-common-attributes" is not present on the
  ## request, no corresponding tag will be added. The header value should be a
  ## json and should follow the schema as describe in the official documentation:
  ## https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html#requestformat
  # parameter_tags = ["env"]

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  # data_format = "influx"
```

## Metrics

Metrics are collected from the `records.[*].data` field in the request body.
The data must be base64 encoded and may be sent in any supported
[data format][data_formats].

## Example Output

When run with this configuration:

```toml
[[inputs.firehose]]
  service_address = ":8080"
  paths = ["/telegraf"]
  data_format = "value"
  data_type = "string"
```

the following curl command:

```sh
curl -i -XPOST 'localhost:8080/telegraf' \
--header 'x-amz-firehose-request-id: ed4acda5-034f-9f42-bba1-f29aea6d7d8f' \
--header 'Content-Type: application/json' \
--data '{
    "requestId": "ed4acda5-034f-9f42-bba1-f29aea6d7d8f",
    "timestamp": 1578090901599,
    "records": [
        {
          "data": "aGVsbG8gd29ybGQK" // "hello world"
        }
    ]
}'
```

produces:

```text
firehose,firehose_http_path=/telegraf value="hello world" 1725001851000000000
```
