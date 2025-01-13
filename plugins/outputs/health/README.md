# Health Output Plugin

This plugin provides a HTTP health check endpoint that can be configured to
return failure status codes based on the value of a metric.

When the plugin is healthy it will return a 200 response; when unhealthy it
will return a 503 response. The default state is healthy, one or more checks
must fail in order for the resource to enter the failed state.

⭐ Telegraf v1.11.0
🏷️ applications
💻 all

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Configurable HTTP health check resource based on metrics
[[outputs.health]]
  ## Address and port to listen on.
  ##   ex: service_address = "http://localhost:8080"
  ##       service_address = "unix:///var/run/telegraf-health.sock"
  # service_address = "http://:8080"

  ## The maximum duration for reading the entire request.
  # read_timeout = "5s"
  ## The maximum duration for writing the entire response.
  # write_timeout = "5s"

  ## Username and password to accept for HTTP basic authentication.
  # basic_username = "user1"
  # basic_password = "secret"

  ## Allowed CA certificates for client certificates.
  # tls_allowed_cacerts = ["/etc/telegraf/clientca.pem"]

  ## TLS server certificate and private key.
  # tls_cert = "/etc/telegraf/cert.pem"
  # tls_key = "/etc/telegraf/key.pem"

  ## NOTE: Due to the way TOML is parsed, tables must be at the END of the
  ## plugin definition, otherwise additional config options are read as part of
  ## the table

  ## One or more check sub-tables should be defined, it is also recommended to
  ## use metric filtering to limit the metrics that flow into this output.
  ##
  ## When using the default buffer sizes, this example will fail when the
  ## metric buffer is half full.
  ##
  ## namepass = ["internal_write"]
  ## tagpass = { output = ["influxdb"] }
  ##
  ## [[outputs.health.compares]]
  ##   field = "buffer_size"
  ##   lt = 5000.0
  ##
  ## [[outputs.health.contains]]
  ##   field = "buffer_size"
  ##
  ## [[outputs.health.time_between_metrics]]
  ##  field = "buffer_size"
  ##  max_time_between_metrics = "1m"
```

### compares

The `compares` check is used to assert basic mathematical relationships.  Use
it by choosing a field key and one or more comparisons that must hold true.  If
the field is not found on a metric no comparison will be made.

Comparisons must be hold true on all metrics for the check to pass.

### contains

The `contains` check can be used to require a field key to exist on at least
one metric.

If the field is found on any metric the check passes.

### time_between_metrics

The `time_between_metrics` check can be used to require a certain period between metrics with the provided fields.
The check will be healthy until the first metric that matches is received, and will later fail if the time passed
since the last message received is greated than `max_time_between_metrics`.
