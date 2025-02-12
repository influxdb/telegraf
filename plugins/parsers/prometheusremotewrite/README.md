# Prometheus Remote Write Parser Plugin

Converts prometheus remote write samples directly into Telegraf metrics. It can
be used with [http_listener_v2](/plugins/inputs/http_listener_v2). There are no
additional configuration options for Prometheus Remote Write Samples.

## Configuration

```toml
[[inputs.http_listener_v2]]
  ## Address and port to host HTTP listener on
  service_address = ":1234"

  ## Paths to listen to.
  paths = ["/receive"]

  ## Data format to consume.
  data_format = "prometheusremotewrite"

  ## Metric version to use. 
  ## Valid options: 1, 2
  ## Default: 2
  metric_version = 2
```

## Example Input

```json
prompb.WriteRequest{
        Timeseries: []*prompb.TimeSeries{
            {
                Labels: []*prompb.Label{
                    {Name: "__name__", Value: "go_gc_duration_seconds"},
                    {Name: "instance", Value: "localhost:9090"},
                    {Name: "job", Value: "prometheus"},
                    {Name: "quantile", Value: "0.99"},
                },
                Samples: []prompb.Sample{
                    {Value: 4.63, Timestamp: time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC).UnixNano()},
                },
            },
        },
    }

```

## Example Output (v1)

```text
go_gc_duration_seconds,instance=localhost:9090,job=prometheus,quantile=0.99 value=4.63 1614889298859000000
```

## Example Output (v2)

```text
prometheus_remote_write,instance=localhost:9090,job=prometheus,quantile=0.99 go_gc_duration_seconds=4.63 1614889298859000000
```

## For alignment with the [InfluxDB v1.x Prometheus Remote Write Spec](https://docs.influxdata.com/influxdb/v1.8/supported_protocols/prometheus/#how-prometheus-metrics-are-parsed-in-influxdb)

- V1: already aligned, it parses metrics according to the spec.
- V2: Use the [Starlark processor rename prometheus remote write script](https://github.com/influxdata/telegraf/blob/master/plugins/processors/starlark/testdata/rename_prometheus_remote_write.star) to rename the measurement name to the fieldname and rename the fieldname to value.
