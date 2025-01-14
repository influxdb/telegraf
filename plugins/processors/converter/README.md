# Converter Processor Plugin

The converter processor is used to change the type of tag or field values. In
addition to changing field types it can convert between fields and tags.

Values that cannot be converted are dropped.

**Note:** When converting tags to fields, take care to ensure the series is
still uniquely identifiable. Fields with the same series key (measurement +
tags) will overwrite one another.

**Note on large strings being converted to numeric types:** When converting a
string value to a numeric type, precision may be lost if the number is too
large. The largest numeric type this plugin supports is `float64`, and if a
string 'number' exceeds its size limit, accuracy may be lost.

**Note on multiple measurement or timestamps:** Users can provide multiple
tags or fields to use as the measurement name or timestamp. However, note that
the order in the array is not guaranteed!

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Convert values to another metric value type
[[processors.converter]]
  ## Tags to convert
  ##
  ## The table key determines the target type, and the array of key-values
  ## select the keys to convert.  The array may contain globs.
  ##   <target-type> = [<tag-key>...]
  [processors.converter.tags]
    measurement = []
    string = []
    integer = []
    unsigned = []
    boolean = []
    float = []

    ## Optional tag to use as metric timestamp
    # timestamp = []

    ## Format of the timestamp determined by the tag above. This can be any of
    ## "unix", "unix_ms", "unix_us", "unix_ns", or a valid Golang time format.
    ## It is required, when using the timestamp option.
    # timestamp_format = ""

  ## Fields to convert
  ##
  ## The table key determines the target type, and the array of key-values
  ## select the keys to convert.  The array may contain globs.
  ##   <target-type> = [<field-key>...]
  [processors.converter.fields]
    measurement = []
    tag = []
    string = []
    integer = []
    unsigned = []
    boolean = []
    float = []

    ## Optional field to use for converting base64 encoding of IEEE 754 Float32 values 
    ## i.e. data_json_content_state_openconfig-platform-psu:output-power":"RKeAAA=="
    ## into a float32 value 1340

    # base64_ieee_float32 = []

    ## Optional field to use as metric timestamp
    # timestamp = []

    ## Format of the timestamp determined by the field above. This can be any
    ## of "unix", "unix_ms", "unix_us", "unix_ns", or a valid Golang time
    ## format. It is required, when using the timestamp option.
    # timestamp_format = ""
```

### Example

Convert `port` tag to a string field:

```toml
[[processors.converter]]
  [processors.converter.tags]
    string = ["port"]
```

```diff
- apache,port=80,server=debian-stretch-apache BusyWorkers=1,BytesPerReq=0
+ apache,server=debian-stretch-apache port="80",BusyWorkers=1,BytesPerReq=0
```

Convert all `scboard_*` fields to an integer:

```toml
[[processors.converter]]
  [processors.converter.fields]
    integer = ["scboard_*"]
```

```diff
- apache scboard_closing=0,scboard_dnslookup=0,scboard_finishing=0,scboard_idle_cleanup=0,scboard_keepalive=0,scboard_logging=0,scboard_open=100,scboard_reading=0,scboard_sending=1,scboard_starting=0,scboard_waiting=49
+ apache scboard_closing=0i,scboard_dnslookup=0i,scboard_finishing=0i,scboard_idle_cleanup=0i,scboard_keepalive=0i,scboard_logging=0i,scboard_open=100i,scboard_reading=0i,scboard_sending=1i,scboard_starting=0i,scboard_waiting=49i
```

Rename the measurement from a tag value:

```toml
[[processors.converter]]
  [processors.converter.tags]
    measurement = ["topic"]
```

```diff
- mqtt_consumer,topic=sensor temp=42
+ sensor temp=42
```

Set the metric timestamp from a tag:

```toml
[[processors.converter]]
  [processors.converter.tags]
    timestamp = ["time"]
    timestamp_format = "unix
```

```diff
- metric,time="1677610769" temp=42
+ metric temp=42 1677610769
```

This is also possible via the fields converter.
