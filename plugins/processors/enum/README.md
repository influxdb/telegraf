# Enum Processor Plugin

The Enum Processor allows the configuration of value mappings for metric tags or
fields.  The main use-case for this is to rewrite status codes such as _red_,
_amber_ and _green_ by numeric values such as 0, 1, 2. The plugin supports
string, int, float64 and bool types for the field values. Multiple tags or
fields can be configured with separate value mappings for each. Default mapping
values can be configured to be used for all values, which are not contained in
the value_mappings. The processor supports explicit configuration of a
destination tag or field. By default the source tag or field is overwritten.

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Map enum values according to given table.
[[processors.enum]]
  [[processors.enum.mapping]]
    ## Names of the fields to map. Globs accepted.
    fields = ["status"]

    ## Name of the tag to map. Globs accepted.
    # tag = "status"

    ## Destination tag or field to be used for the mapped value.  By default the
    ## source tag or field is used, overwriting the original value.
    dest = "status_code"

    ## Default value to be used for all values not contained in the mapping
    ## table.  When unset and no match is found, the original field will remain
    ## unmodified and the destination tag or field will not be created.
    # default = 0

    ## Table of mappings
    [processors.enum.mapping.value_mappings]
      green = 1
      amber = 2
      red = 3
```

## Example

```diff
- xyzzy status="green" 1502489900000000000
+ xyzzy status="green",status_code=1i 1502489900000000000
```

With unknown value and no default set:

```diff
- xyzzy status="black" 1502489900000000000
+ xyzzy status="black" 1502489900000000000
```
