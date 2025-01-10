# OpenMetrics Format Parser Plugin

This plugin allows to parse the [OpenMetrics Text Format][] into Telegraf
metrics. It is used internally in [prometheus input](/plugins/inputs/prometheus)
but can also be used by e.g.
[http_listener_v2](/plugins/inputs/http_listener_v2) to simulate a Pushgateway.

The plugin allows to output different metric formats as described in the
[Metric Formats section](#metric-formats).

[OpenMetrics Text Format]: https://github.com/prometheus/OpenMetrics/blob/v1.0.0/specification/OpenMetrics.md

## Configuration

```toml
[[inputs.file]]
  files = ["example"]

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ##   https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "openmetrics"

```

## Metric Formats

The metric_version setting controls how telegraf translates OpenMetrics'
metrics to Telegraf metrics. There are two options.

### `v1` format

In this version, the OpenMetrics metric-family name becomes the Telegraf metric
name, labels become tags and values become fields. The fields are named based
on the type of the OpenMetric metric. This option produces metrics that are
dense (contrary to the sparse metrics of`v2`).

Example:

```text
# TYPE acme_http_router_request_seconds summary
# UNIT acme_http_router_request_seconds seconds
# HELP acme_http_router_request_seconds Latency though all of ACME's HTTP request router.
acme_http_router_request_seconds_sum{path="/api/v1",method="GET"} 9036.32
acme_http_router_request_seconds_count{path="/api/v1",method="GET"} 807283.0
acme_http_router_request_seconds{path="/api/v1",method="GET",quantile="0.5"} 1.29854
acme_http_router_request_seconds{path="/api/v1",method="GET",quantile="0.9"} 54.85479
acme_http_router_request_seconds{path="/api/v1",method="GET",quantile="0.99"} 6884.32324
acme_http_router_request_seconds_created{path="/api/v1",method="GET"} 1605281325.0
acme_http_router_request_seconds_sum{path="/api/v2",method="POST"} 479.3
acme_http_router_request_seconds_count{path="/api/v2",method="POST"} 34.0
acme_http_router_request_seconds_created{path="/api/v2",method="POST"} 1605281325.0
acme_http_router_request_seconds{path="/api/v2",method="POST",quantile="0.5"} 0.85412
acme_http_router_request_seconds{path="/api/v2",method="POST",quantile="0.9"} 1.15429
acme_http_router_request_seconds{path="/api/v2",method="POST",quantile="0.99"} 3698.48132
# TYPE go_goroutines gauge
# HELP go_goroutines Number of goroutines that currently exist.
go_goroutines 69
# TYPE process_cpu_seconds counter
# UNIT process_cpu_seconds seconds
# HELP process_cpu_seconds Total user and system CPU time spent in seconds.
process_cpu_seconds_total 4.20072246e+06
# EOF```

becomes

```text
acme_http_router_request_seconds,unit=seconds,path=/api/v1,method=GET sum=9036.32,count=807283.0,created=1605281325.0,0.5=1.29854,0.9=54.85479,0.99=6884.32324
acme_http_router_request_seconds,unit=seconds,path=/api/v2,method=POST sum=479.3,count=34.0,created=1605281325.0,0.5=0.85412,0.9=1.15429,0.99=3698.48132
go_goroutines gauge=69
process_cpu_seconds,unit=seconds counter=4200722.46
```

This is especially useful and efficient for outputs with row-oriented data
models.

### `v2` format

In this version, each OpenMetrics MetricPoint becomes a Telegraf metric with
a few exceptions for complex types like histograms. All Telegraf metrics are
named `prometheus` with OpenMetrics' labels become tags and the field-name is
based on the OpenMetrics metric-name.

Example:

Example:

```text
# TYPE acme_http_router_request_seconds summary
# UNIT acme_http_router_request_seconds seconds
# HELP acme_http_router_request_seconds Latency though all of ACME's HTTP request router.
acme_http_router_request_seconds_sum{path="/api/v1",method="GET"} 9036.32
acme_http_router_request_seconds_count{path="/api/v1",method="GET"} 807283.0
acme_http_router_request_seconds{path="/api/v1",method="GET",quantile="0.5"} 1.29854
acme_http_router_request_seconds{path="/api/v1",method="GET",quantile="0.9"} 54.85479
acme_http_router_request_seconds{path="/api/v1",method="GET",quantile="0.99"} 6884.32324
acme_http_router_request_seconds_created{path="/api/v1",method="GET"} 1605281325.0
acme_http_router_request_seconds_sum{path="/api/v2",method="POST"} 479.3
acme_http_router_request_seconds_count{path="/api/v2",method="POST"} 34.0
acme_http_router_request_seconds_created{path="/api/v2",method="POST"} 1605281325.0
acme_http_router_request_seconds{path="/api/v2",method="POST",quantile="0.5"} 0.85412
acme_http_router_request_seconds{path="/api/v2",method="POST",quantile="0.9"} 1.15429
acme_http_router_request_seconds{path="/api/v2",method="POST",quantile="0.99"} 3698.48132
# TYPE go_goroutines gauge
# HELP go_goroutines Number of goroutines that currently exist.
go_goroutines 69
# TYPE process_cpu_seconds counter
# UNIT process_cpu_seconds seconds
# HELP process_cpu_seconds Total user and system CPU time spent in seconds.
process_cpu_seconds_total 4.20072246e+06
# EOF```

becomes

```text
prometheus,method=GET,path=/api/v1,unit=seconds acme_http_router_request_seconds_count=807283,acme_http_router_request_seconds_created=1605281325,acme_http_router_request_seconds_sum=9036.32
prometheus,method=GET,path=/api/v1,quantile=0.5,unit=seconds acme_http_router_request_seconds=1.29854
prometheus,method=GET,path=/api/v1,quantile=0.9,unit=seconds acme_http_router_request_seconds=54.85479
prometheus,method=GET,path=/api/v1,quantile=0.99,unit=seconds acme_http_router_request_seconds=6884.32324
prometheus,method=POST,path=/api/v2,unit=seconds acme_http_router_request_seconds_count=34,acme_http_router_request_seconds_created=1605281325,acme_http_router_request_seconds_sum=479.3
prometheus,method=POST,path=/api/v2,quantile=0.5,unit=seconds acme_http_router_request_seconds=0.85412
prometheus,method=POST,path=/api/v2,quantile=0.9,unit=seconds acme_http_router_request_seconds=1.15429
prometheus,method=POST,path=/api/v2,quantile=0.99,unit=seconds acme_http_router_request_seconds=3698.48132
prometheus go_goroutines=69
prometheus,unit=seconds process_cpu_seconds=4200722.46
```

The resulting metrics are sparse, but for some outputs they may be easier to
process or query, including those that are more efficient with column-oriented
data. The telegraf metric name is the same for all metrics in the input
instance. It can be set with the `name_override` setting and defaults to
"prometheus". To have multiple metric names, you can use multiple instances of
the plugin, each with its own `name_override`.

`metric_version = 2` uses the same histogram format as the histogram aggregator

## Regenerating OpenMetrics code

Download the latest version of the protocol-buffer definition

```text
wget https://raw.githubusercontent.com/OpenObservability/OpenMetrics/main/proto/openmetrics_data_model.proto
```

and generate the go-code for the definition using

```text
protoc --proto_path=. \
       --go_out=. \
       --go_opt=paths=source_relative \
       --go_opt=Mopenmetrics_data_model.proto=github.com/influxdata/telegraf/plugins/parsers/openmetrics \
       openmetrics_data_model.proto
```
