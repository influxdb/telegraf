# JSON

The `json` output data format converts metrics into JSON documents.

## Configuration

```toml
[[outputs.file]]
  ## Files to write to, "stdout" is a specially handled file.
  files = ["stdout", "/tmp/metrics.out"]

  ## Data format to output.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_OUTPUT.md
  data_format = "json"

  ## The resolution to use for the metric timestamp.  Must be a duration string
  ## such as "1ns", "1us", "1ms", "10ms", "1s".  Durations are truncated to
  ## the power of 10 less than the specified units.
  json_timestamp_units = "1s"

  ## The default timestamp format is Unix epoch time, subject to the
  # resolution configured in json_timestamp_units.
  # Other timestamp layout can be configured using the Go language time
  # layout specification from https://golang.org/pkg/time/#Time.Format
  # e.g.: json_timestamp_format = "2006-01-02T15:04:05Z07:00"
  #json_timestamp_format = ""

  ## A [JSONata](https://jsonata.org/) transformation of the JSON in
  ## [standard-form](#examples). Please note that only version 1.5.4 of the
  ## JSONata is supported due to the underlying library used.
  ## This allows to generate an arbitrary output form based on the metric(s). Please use
  ## multiline strings (starting and ending with three single-quotes) if needed.
  #json_transformation = ""

  ## Filter for fields that contain nested JSON data.
  ## The serializer will try to decode matching STRING fields containing
  ## valid JSON. This is done BEFORE any JSON transformation. The filters
  ## can contain wildcards.
  #json_nested_fields_include = []
  #json_nested_fields_exclude = []
```

## Examples

Standard form:

```json
{
    "fields": {
        "field_1": 30,
        "field_2": 4,
        "field_N": 59,
        "n_images": 660
    },
    "name": "docker",
    "tags": {
        "host": "raynor"
    },
    "timestamp": 1458229140
}
```

When an output plugin needs to emit multiple metrics at one time, it may use
the batch format.  The use of batch format is determined by the plugin,
reference the documentation for the specific plugin.

```json
{
    "metrics": [
        {
            "fields": {
                "field_1": 30,
                "field_2": 4,
                "field_N": 59,
                "n_images": 660
            },
            "name": "docker",
            "tags": {
                "host": "raynor"
            },
            "timestamp": 1458229140
        },
        {
            "fields": {
                "field_1": 30,
                "field_2": 4,
                "field_N": 59,
                "n_images": 660
            },
            "name": "docker",
            "tags": {
                "host": "raynor"
            },
            "timestamp": 1458229140
        }
    ]
}
```

## Transformations

Transformations using the [JSONata standard](https://jsonata.org/) can be specified with
the `json_transformation` parameter. The input to the transformation is the serialized
metric in the standard-form above.

**Note**: There is a difference in batch and non-batch serialization mode!
The former adds a `metrics` field containing the metric array, while the later
serializes the metric directly.

**Note**: Please note that the JSONata support is limited to version 1.5.4 due
to the underlying library used by Telegraf. When using the online playground
below ensure that you have selected 1.5.4 when trying examples or building
transformations.

In the following sections, some rudimentary examples for transformations are shown.
For more elaborated JSONata expressions please consult the
[documentation](https://docs.jsonata.org) or the
[online playground](https://try.jsonata.org).

### Non-batch mode

In the following examples, we will use the following input to the transformation:

```json
{
    "fields": {
        "field_1": 30,
        "field_2": 4,
        "field_N": 59,
        "n_images": 660
    },
    "name": "docker",
    "tags": {
        "host": "raynor"
    },
    "timestamp": 1458229140
}
```

If you want to flatten the above metric, you can use

```json
$merge([{"name": name, "timestamp": timestamp}, tags, fields])
```

to get

```json
{
  "name": "docker",
  "timestamp": 1458229140,
  "host": "raynor",
  "field_1": 30,
  "field_2": 4,
  "field_N": 59,
  "n_images": 660
}
```

It is also possible to do arithmetic or renaming

```json
{
    "capacity": $sum($sift($.fields,function($value,$key){$key~>/^field_/}).*),
    "images": fields.n_images,
    "host": tags.host,
    "time": $fromMillis(timestamp*1000)
}
```

will result in

```json
{
  "capacity": 93,
  "images": 660,
  "host": "raynor",
  "time": "2016-03-17T15:39:00.000Z"
}
```

### Batch mode

When an output plugin emits multiple metrics in a batch fashion it might be useful
to restructure and/or combine the metric elements. We will use the following input
example in this section

```json
{
    "metrics": [
        {
            "fields": {
                "field_1": 30,
                "field_2": 4,
                "field_N": 59,
                "n_images": 660
            },
            "name": "docker",
            "tags": {
                "host": "raynor"
            },
            "timestamp": 1458229140
        },
        {
            "fields": {
                "field_1": 12,
                "field_2": 43,
                "field_3": 0,
                "field_4": 5,
                "field_5": 7,
                "field_N": 27,
                "n_images": 72
            },
            "name": "docker",
            "tags": {
                "host": "amaranth"
            },
            "timestamp": 1458229140
        },
        {
            "fields": {
                "field_1": 5,
                "field_N": 34,
                "n_images": 0
            },
            "name": "storage",
            "tags": {
                "host": "amaranth"
            },
            "timestamp": 1458229140
        }
    ]
}
```

We can do the same computation as above, iterating over the metrics

```json
metrics.{
    "capacity": $sum($sift($.fields,function($value,$key){$key~>/^field_/}).*),
    "images": fields.n_images,
    "service": (name & "(" & tags.host & ")"),
    "time": $fromMillis(timestamp*1000)
}

```

resulting in

```json
[
  {
    "capacity": 93,
    "images": 660,
    "service": "docker(raynor)",
    "time": "2016-03-17T15:39:00.000Z"
  },
  {
    "capacity": 94,
    "images": 72,
    "service": "docker(amaranth)",
    "time": "2016-03-17T15:39:00.000Z"
  },
  {
    "capacity": 39,
    "images": 0,
    "service": "storage(amaranth)",
    "time": "2016-03-17T15:39:00.000Z"
  }
]
```

However, the more interesting use-case is to restructure and **combine** the metrics, e.g. by grouping by `host`

```json
{
    "time": $min(metrics.timestamp) * 1000 ~> $fromMillis(),
    "images": metrics{
        tags.host: {
            name: fields.n_images
        }
    },
    "capacity alerts": metrics[fields.n_images < 10].[(tags.host & " " & name)]
}
```

resulting in

```json
{
  "time": "2016-03-17T15:39:00.000Z",
  "images": {
    "raynor": {
      "docker": 660
    },
    "amaranth": {
      "docker": 72,
      "storage": 0
    }
  },
  "capacity alerts": [
    "amaranth storage"
  ]
}
```

Please consult the JSONata documentation for more examples and details.
