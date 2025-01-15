# SQL Input Plugin

This plugin reads metrics from performing SQL queries against a SQL
server. Different server types are supported and their settings might differ
(especially the connection parameters).  Please check the list of [supported SQL
drivers](../../../docs/SQL_DRIVERS_INPUT.md) for the `driver` name and options
for the data-source-name (`dsn`) options.

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Secret-store support

This plugin supports secrets from secret-stores for the `dsn` option.
See the [secret-store documentation][SECRETSTORE] for more details on how
to use them.

[SECRETSTORE]: ../../../docs/CONFIGURATION.md#secret-store-secrets

## Configuration

```toml @sample.conf
# Read metrics from SQL queries
[[inputs.sql]]
  ## Database Driver
  ## See https://github.com/influxdata/telegraf/blob/master/docs/SQL_DRIVERS_INPUT.md for
  ## a list of supported drivers.
  driver = "mysql"

  ## Data source name for connecting
  ## The syntax and supported options depends on selected driver.
  dsn = "username:password@tcp(mysqlserver:3307)/dbname?param=value"

  ## Timeout for any operation
  ## Note that the timeout for queries is per query not per gather.
  # timeout = "5s"

  ## Connection time limits
  ## By default the maximum idle time and maximum lifetime of a connection is unlimited, i.e. the connections
  ## will not be closed automatically. If you specify a positive time, the connections will be closed after
  ## idleing or existing for at least that amount of time, respectively.
  # connection_max_idle_time = "0s"
  # connection_max_life_time = "0s"

  ## Connection count limits
  ## By default the number of open connections is not limited and the number of maximum idle connections
  ## will be inferred from the number of queries specified. If you specify a positive number for any of the
  ## two options, connections will be closed when reaching the specified limit. The number of idle connections
  ## will be clipped to the maximum number of connections limit if any.
  # connection_max_open = 0
  # connection_max_idle = auto

  ## Specifies plugin behavior regarding disconnected servers
  ## Available choices :
  ##   - error: telegraf will return an error on startup if one the servers is unreachable
  ##   - ignore: telegraf will ignore unreachable servers on both startup and gather
  # disconnected_servers_behavior = "error"

  [[inputs.sql.query]]
    ## Query to perform on the server
    query="SELECT user,state,latency,score FROM Scoreboard WHERE application > 0"
    ## Alternatively to specifying the query directly you can select a file here containing the SQL query.
    ## Only one of 'query' and 'query_script' can be specified!
    # query_script = "/path/to/sql/script.sql"

    ## Name of the measurement
    ## In case both measurement and 'measurement_col' are given, the latter takes precedence.
    # measurement = "sql"

    ## Column name containing the name of the measurement
    ## If given, this will take precedence over the 'measurement' setting. In case a query result
    ## does not contain the specified column, we fall-back to the 'measurement' setting.
    # measurement_column = ""

    ## Column name containing the time of the measurement
    ## If omitted, the time of the query will be used.
    # time_column = ""

    ## Format of the time contained in 'time_col'
    ## The time must be 'unix', 'unix_ms', 'unix_us', 'unix_ns', or a golang time format.
    ## See https://golang.org/pkg/time/#Time.Format for details.
    # time_format = "unix"

    ## Column names containing tags
    ## An empty include list will reject all columns and an empty exclude list will not exclude any column.
    ## I.e. by default no columns will be returned as tag and the tags are empty.
    # tag_columns_include = []
    # tag_columns_exclude = []

    ## Column names containing fields (explicit types)
    ## Convert the given columns to the corresponding type. Explicit type conversions take precedence over
    ## the automatic (driver-based) conversion below.
    ## NOTE: Columns should not be specified for multiple types or the resulting type is undefined.
    # field_columns_float = []
    # field_columns_int = []
    # field_columns_uint = []
    # field_columns_bool = []
    # field_columns_string = []

    ## Column names containing fields (automatic types)
    ## An empty include list is equivalent to '[*]' and all returned columns will be accepted. An empty
    ## exclude list will not exclude any column. I.e. by default all columns will be returned as fields.
    ## NOTE: We rely on the database driver to perform automatic datatype conversion.
    # field_columns_include = []
    # field_columns_exclude = []
```

## Options

### Driver

The `driver` and `dsn` options specify how to connect to the database. As
especially the `dsn` format and values vary with the `driver` refer to the list
of [supported SQL drivers](../../../docs/SQL_DRIVERS_INPUT.md) for possible
values and more details.

### Connection limits

With these options you can limit the number of connections kept open by this
plugin. Details about the exact workings can be found in the [golang sql
documentation](https://golang.org/pkg/database/sql/#DB.SetConnMaxIdleTime).

### Query sections

Multiple `query` sections can be specified for this plugin. Each specified query
will first be prepared on the server and then executed in every interval using
the column mappings specified. Please note that `tag` and `field` columns are
not exclusive, i.e. a column can be added to both. When using both `include` and
`exclude` lists, the `exclude` list takes precedence over the `include`
list. I.e. given you specify `foo` in both lists, `foo` will _never_ pass the
filter. In case any the columns specified in `measurement_col` or `time_col` are
_not_ returned by the query, the plugin falls-back to the documented
defaults. Fields or tags specified in the includes of the options but missing in
the returned query are silently ignored.

## Types

This plugin relies on the driver to do the type conversion. For the different
properties of the metric the following types are accepted.

### Measurement

Only columns of type `string`  are accepted.

### Time

For the metric time columns of type `time` are accepted directly. For numeric
columns, `time_format` should be set to any of `unix`, `unix_ms`, `unix_ns` or
`unix_us` accordingly. By default the a timestamp in `unix` format is
expected. For string columns, please specify the `time_format` accordingly.  See
the [golang time documentation](https://golang.org/pkg/time/#Time.Format) for
details.

### Tags

For tags columns with textual values (`string` and `bytes`), signed and unsigned
integers (8, 16, 32 and 64 bit), floating-point (32 and 64 bit), `boolean` and
`time` values are accepted. Those values will be converted to string.

### Fields

For fields columns with textual values (`string` and `bytes`), signed and
unsigned integers (8, 16, 32 and 64 bit), floating-point (32 and 64 bit),
`boolean` and `time` values are accepted. Here `bytes` will be converted to
`string`, signed and unsigned integer values will be converted to `int64` or
`uint64` respectively. Floating-point values are converted to `float64` and
`time` is converted to a nanosecond timestamp of type `int64`.

## Example Output

Using the [MariaDB sample database][maria-sample] and the configuration

```toml
[[inputs.sql]]
  driver = "mysql"
  dsn = "root:password@/nation"

  [[inputs.sql.query]]
    query="SELECT * FROM guests"
    measurement = "nation"
    tag_columns_include = ["name"]
    field_columns_exclude = ["name"]
```

Telegraf will output the following metrics

```text
nation,host=Hugin,name=John guest_id=1i 1611332164000000000
nation,host=Hugin,name=Jane guest_id=2i 1611332164000000000
nation,host=Hugin,name=Jean guest_id=3i 1611332164000000000
nation,host=Hugin,name=Storm guest_id=4i 1611332164000000000
nation,host=Hugin,name=Beast guest_id=5i 1611332164000000000
```

[maria-sample]: https://www.mariadbtutorial.com/getting-started/mariadb-sample-database

## Metrics

The format of metrics produced by this plugin depends on the content and data
format of the file.
