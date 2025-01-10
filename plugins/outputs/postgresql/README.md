# PostgreSQL Output Plugin

This plugin writes metrics to a [PostgreSQL][postgresql] (or compatible) server
managing the schema and automatically updating missing columns.

⭐ Telegraf v1.24.0
🏷️ datastore
💻 all

[postgresql]: https://www.postgresql.org/

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Startup error behavior options <!-- @/docs/includes/startup_error_behavior.md -->

In addition to the plugin-specific and global configuration settings the plugin
supports options for specifying the behavior when experiencing startup errors
using the `startup_error_behavior` setting. Available values are:

- `error`:  Telegraf with stop and exit in case of startup errors. This is the
            default behavior.
- `ignore`: Telegraf will ignore startup errors for this plugin and disables it
            but continues processing for all other plugins.
- `retry`:  Telegraf will try to startup the plugin in every gather or write
            cycle in case of startup errors. The plugin is disabled until
            the startup succeeds.

## Secret-store support

This plugin supports secrets from secret-stores for the `connection` option.
See the [secret-store documentation][SECRETSTORE] for more details on how
to use them.

[SECRETSTORE]: ../../../docs/CONFIGURATION.md#secret-store-secrets

## Configuration

```toml @sample.conf
# Publishes metrics to a postgresql database
[[outputs.postgresql]]
  ## Specify connection address via the standard libpq connection string:
  ##   host=... user=... password=... sslmode=... dbname=...
  ## Or a URL:
  ##   postgres://[user[:password]]@localhost[/dbname]?sslmode=[disable|verify-ca|verify-full]
  ## See https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING
  ##
  ## All connection parameters are optional. Environment vars are also supported.
  ## e.g. PGPASSWORD, PGHOST, PGUSER, PGDATABASE
  ## All supported vars can be found here:
  ##  https://www.postgresql.org/docs/current/libpq-envars.html
  ##
  ## Non-standard parameters:
  ##   pool_max_conns (default: 1) - Maximum size of connection pool for parallel (per-batch per-table) inserts.
  ##   pool_min_conns (default: 0) - Minimum size of connection pool.
  ##   pool_max_conn_lifetime (default: 0s) - Maximum connection age before closing.
  ##   pool_max_conn_idle_time (default: 0s) - Maximum idle time of a connection before closing.
  ##   pool_health_check_period (default: 0s) - Duration between health checks on idle connections.
  # connection = ""

  ## Postgres schema to use.
  # schema = "public"

  ## Store tags as foreign keys in the metrics table. Default is false.
  # tags_as_foreign_keys = false

  ## Suffix to append to table name (measurement name) for the foreign tag table.
  # tag_table_suffix = "_tag"

  ## Deny inserting metrics if the foreign tag can't be inserted.
  # foreign_tag_constraint = false

  ## Store all tags as a JSONB object in a single 'tags' column.
  # tags_as_jsonb = false

  ## Store all fields as a JSONB object in a single 'fields' column.
  # fields_as_jsonb = false

  ## Name of the timestamp column
  ## NOTE: Some tools (e.g. Grafana) require the default name so be careful!
  # timestamp_column_name = "time"

  ## Type of the timestamp column
  ## Currently, "timestamp without time zone" and "timestamp with time zone"
  ## are supported
  # timestamp_column_type = "timestamp without time zone"

  ## Templated statements to execute when creating a new table.
  # create_templates = [
  #   '''CREATE TABLE {{ .table }} ({{ .columns }})''',
  # ]

  ## Templated statements to execute when adding columns to a table.
  ## Set to an empty list to disable. Points containing tags for which there is
  ## no column will be skipped. Points containing fields for which there is no
  ## column will have the field omitted.
  # add_column_templates = [
  #   '''ALTER TABLE {{ .table }} ADD COLUMN IF NOT EXISTS {{ .columns|join ", ADD COLUMN IF NOT EXISTS " }}''',
  # ]

  ## Templated statements to execute when creating a new tag table.
  # tag_table_create_templates = [
  #   '''CREATE TABLE {{ .table }} ({{ .columns }}, PRIMARY KEY (tag_id))''',
  # ]

  ## Templated statements to execute when adding columns to a tag table.
  ## Set to an empty list to disable. Points containing tags for which there is
  ## no column will be skipped.
  # tag_table_add_column_templates = [
  #   '''ALTER TABLE {{ .table }} ADD COLUMN IF NOT EXISTS {{ .columns|join ", ADD COLUMN IF NOT EXISTS " }}''',
  # ]

  ## The postgres data type to use for storing unsigned 64-bit integer values
  ## (Postgres does not have a native unsigned 64-bit integer type).
  ## The value can be one of:
  ##   numeric - Uses the PostgreSQL "numeric" data type.
  ##   uint8 - Requires pguint extension (https://github.com/petere/pguint)
  # uint64_type = "numeric"

  ## When using pool_max_conns > 1, and a temporary error occurs, the query is
  ## retried with an incremental backoff. This controls the maximum duration.
  # retry_max_backoff = "15s"

  ## Approximate number of tag IDs to store in in-memory cache (when using
  ## tags_as_foreign_keys). This is an optimization to skip inserting known
  ## tag IDs. Each entry consumes approximately 34 bytes of memory.
  # tag_cache_size = 100000

  ## Cut column names at the given length to not exceed PostgreSQL's
  ## 'identifier length' limit (default: no limit)
  ## (see https://www.postgresql.org/docs/current/limits.html)
  ## Be careful to not create duplicate column names!
  # column_name_length_limit = 0

  ## Enable & set the log level for the Postgres driver.
  # log_level = "warn" # trace, debug, info, warn, error, none
```

### Concurrency

By default the postgresql plugin does not utilize any concurrency. However it
can for increased throughput. When concurrency is off, telegraf core handles
things like retrying on failure, buffering, etc. When concurrency is used,
these aspects have to be handled by the plugin.

To enable concurrent writes to the database, set the `pool_max_conns`
connection parameter to a value >1. When enabled, incoming batches will be
split by measurement/table name. In addition, if a batch comes in and the
previous batch has not completed, concurrency will be used for the new batch
as well.

If all connections are utilized and the pool is exhausted, further incoming
batches will be buffered within telegraf core.

### Foreign tags

When using `tags_as_foreign_keys`, tags will be written to a separate table
with a `tag_id` column used for joins. Each series (unique combination of tag
values) gets its own entry in the tags table, and a unique `tag_id`.

## Data types

By default the postgresql plugin maps Influx data types to the following
PostgreSQL types:

| Influx                                                                                                       | PostgreSQL                                                                                         |
|--------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------|
| [float](https://docs.influxdata.com/influxdb/latest/reference/syntax/line-protocol/#float)                   | [double precision](https://www.postgresql.org/docs/current/datatype-numeric.html#DATATYPE-FLOAT)   |
| [integer](https://docs.influxdata.com/influxdb/latest/reference/syntax/line-protocol/#integer)               | [bigint](https://www.postgresql.org/docs/current/datatype-numeric.html#DATATYPE-INT)               |
| [uinteger](https://docs.influxdata.com/influxdb/latest/reference/syntax/line-protocol/#uinteger)             | [numeric](https://www.postgresql.org/docs/current/datatype-numeric.html#DATATYPE-NUMERIC-DECIMAL)* |
| [string](https://docs.influxdata.com/influxdb/latest/reference/syntax/line-protocol/#string)                 | [text](https://www.postgresql.org/docs/current/datatype-character.html)                            |
| [boolean](https://docs.influxdata.com/influxdb/latest/reference/syntax/line-protocol/#boolean)               | [boolean](https://www.postgresql.org/docs/current/datatype-boolean.html)                           |
| [unix timestamp](https://docs.influxdata.com/influxdb/latest/reference/syntax/line-protocol/#unix-timestamp) | [timestamp](https://www.postgresql.org/docs/current/datatype-datetime.html)                        |

It is important to note that `uinteger` (unsigned 64-bit integer) is mapped to
the `numeric` PostgreSQL data type. The `numeric` data type is an arbitrary
precision decimal data type that is less efficient than `bigint`. This is
necessary as the range of values for the Influx `uinteger` data type can
exceed `bigint`, and thus cause errors when inserting data.

### pguint

As a solution to the `uinteger`/`numeric` data type problem, there is a
PostgreSQL extension that offers unsigned 64-bit integer support:
[https://github.com/petere/pguint](https://github.com/petere/pguint).

If this extension is installed, you can enable the `unsigned_integers` config
parameter which will cause the plugin to use the `uint8` datatype instead of
`numeric`.

## Templating

The postgresql plugin uses templates for the schema modification SQL
statements. This allows for complete control of the schema by the user.

Documentation on how to write templates can be found [sqltemplate docs][1]

[1]: https://pkg.go.dev/github.com/influxdata/telegraf/plugins/outputs/postgresql/sqltemplate

## Long Column Names

Postgres imposes a limit on the length of column identifiers, which can be found
in the [official docs](https://www.postgresql.org/docs/current/limits.html). By
default Telegraf does not enforce this limit as this limit can be modified on
the server side. Furthermore, cutting off column names could lead to collisions
if the columns are only different after the cut-off.

> [!WARNING]
> Make sure you will not cause column name collisions when setting
> `column_name_length_limit`! If in doubt, explicitly shorten the field and tag
> names using e.g. the regexp processor.

### Samples

#### TimescaleDB

```toml
tags_as_foreign_keys = true
create_templates = [
    '''CREATE TABLE {{ .table }} ({{ .columns }})''',
    '''SELECT create_hypertable({{ .table|quoteLiteral }}, 'time', chunk_time_interval => INTERVAL '7d')''',
    '''ALTER TABLE {{ .table }} SET (timescaledb.compress, timescaledb.compress_segmentby = 'tag_id')''',
]
```

##### Multi-node

```toml
tags_as_foreign_keys = true
create_templates = [
    '''CREATE TABLE {{ .table }} ({{ .columns }})''',
    '''SELECT create_distributed_hypertable({{ .table|quoteLiteral }}, 'time', partitioning_column => 'tag_id', number_partitions => (SELECT count(*) FROM timescaledb_information.data_nodes)::integer, replication_factor => 2, chunk_time_interval => INTERVAL '7d')''',
    '''ALTER TABLE {{ .table }} SET (timescaledb.compress, timescaledb.compress_segmentby = 'tag_id')''',
]
```

#### Tag table with view

This example enables `tags_as_foreign_keys`, but creates a postgres view to
automatically join the metric & tag tables. The metric & tag tables are stored
in a "telegraf" schema, with the view in the "public" schema.

```toml
tags_as_foreign_keys = true
schema = "telegraf"
create_templates = [
    '''CREATE TABLE {{ .table }} ({{ .columns }})''',
    '''CREATE VIEW {{ .table.WithSchema "public" }} AS SELECT time, {{ (.tagTable.Columns.Tags.Concat .allColumns.Fields).Identifiers | join "," }} FROM {{ .table }} t, {{ .tagTable }} tt WHERE t.tag_id = tt.tag_id''',
]
add_column_templates = [
    '''ALTER TABLE {{ .table }} ADD COLUMN IF NOT EXISTS {{ .columns|join ", ADD COLUMN IF NOT EXISTS " }}''',
    '''DROP VIEW IF EXISTS {{ .table.WithSchema "public" }}''',
    '''CREATE VIEW {{ .table.WithSchema "public" }} AS SELECT time, {{ (.tagTable.Columns.Tags.Concat .allColumns.Fields).Identifiers | join "," }} FROM {{ .table }} t, {{ .tagTable }} tt WHERE t.tag_id = tt.tag_id''',
]
tag_table_add_column_templates = [
    '''ALTER TABLE {{.table}} ADD COLUMN IF NOT EXISTS {{.columns|join ", ADD COLUMN IF NOT EXISTS "}}''',
    '''DROP VIEW IF EXISTS {{ .metricTable.WithSchema "public" }}''',
    '''CREATE VIEW {{ .metricTable.WithSchema "public" }} AS SELECT time, {{ (.allColumns.Tags.Concat .metricTable.Columns.Fields).Identifiers | join "," }} FROM {{ .metricTable }} t, {{ .tagTable }} tt WHERE t.tag_id = tt.tag_id''',
]
```

#### Immutable data table

Some PostgreSQL-compatible databases don't allow modification of table schema
after initial creation. This example works around the limitation by creating
a new table and then using a view to join them together.

```toml
tags_as_foreign_keys = true
schema = 'telegraf'
create_templates = [
    '''CREATE TABLE {{ .table }} ({{ .allColumns }})''',
    '''SELECT create_hypertable({{ .table|quoteLiteral }}, 'time', chunk_time_interval => INTERVAL '7d')''',
    '''ALTER TABLE {{ .table }} SET (timescaledb.compress, timescaledb.compress_segmentby = 'tag_id')''',
    '''SELECT add_compression_policy({{ .table|quoteLiteral }}, INTERVAL '14d')''',
    '''CREATE VIEW {{ .table.WithSuffix "_data" }} AS SELECT {{ .allColumns.Selectors | join "," }} FROM {{ .table }}''',
    '''CREATE VIEW {{ .table.WithSchema "public" }} AS SELECT time, {{ (.tagTable.Columns.Tags.Concat .allColumns.Fields).Identifiers | join "," }} FROM {{ .table.WithSuffix "_data" }} t, {{ .tagTable }} tt WHERE t.tag_id = tt.tag_id''',
]
add_column_templates = [
    '''ALTER TABLE {{ .table }} RENAME TO {{ (.table.WithSuffix "_" .table.Columns.Hash).WithSchema "" }}''',
    '''ALTER VIEW {{ .table.WithSuffix "_data" }} RENAME TO {{ (.table.WithSuffix "_" .table.Columns.Hash "_data").WithSchema "" }}''',
    '''DROP VIEW {{ .table.WithSchema "public" }}''',

    '''CREATE TABLE {{ .table }} ({{ .allColumns }})''',
    '''SELECT create_hypertable({{ .table|quoteLiteral }}, 'time', chunk_time_interval => INTERVAL '7d')''',
    '''ALTER TABLE {{ .table }} SET (timescaledb.compress, timescaledb.compress_segmentby = 'tag_id')''',
    '''SELECT add_compression_policy({{ .table|quoteLiteral }}, INTERVAL '14d')''',
    '''CREATE VIEW {{ .table.WithSuffix "_data" }} AS SELECT {{ .allColumns.Selectors | join "," }} FROM {{ .table }} UNION ALL SELECT {{ (.allColumns.Union .table.Columns).Selectors | join "," }} FROM {{ .table.WithSuffix "_" .table.Columns.Hash "_data" }}''',
    '''CREATE VIEW {{ .table.WithSchema "public" }} AS SELECT time, {{ (.tagTable.Columns.Tags.Concat .allColumns.Fields).Identifiers | join "," }} FROM {{ .table.WithSuffix "_data" }} t, {{ .tagTable }} tt WHERE t.tag_id = tt.tag_id''',
]
tag_table_add_column_templates = [
    '''ALTER TABLE {{ .table }} ADD COLUMN IF NOT EXISTS {{ .columns|join ", ADD COLUMN IF NOT EXISTS " }}''',
    '''DROP VIEW {{ .metricTable.WithSchema "public" }}''',
    '''CREATE VIEW {{ .metricTable.WithSchema "public" }} AS SELECT time, {{ (.allColumns.Tags.Concat .metricTable.Columns.Fields).Identifiers | join "," }} FROM {{ .metricTable.WithSuffix "_data" }} t, {{ .table }} tt WHERE t.tag_id = tt.tag_id''',
]
```

#### Index

Create an index on time and tag columns for faster querying of data.

```toml
create_templates = [
    '''CREATE TABLE {{ .table }} ({{ .columns }})''',
    '''CREATE INDEX ON {{ .table }} USING btree({{ .columns.Keys.Identifiers | join "," }})'''
  ]
```

## Error handling

When the plugin encounters an error writing to the database, it attempts to
determine whether the error is temporary or permanent. An error is considered
temporary if it's possible that retrying the write will succeed. Some examples
of temporary errors are things like connection interruption, deadlocks, etc.
Permanent errors are things like invalid data type, insufficient permissions,
etc.

When an error is determined to be temporary, the plugin will retry the write
with an incremental backoff.

When an error is determined to be permanent, the plugin will discard the
sub-batch. The "sub-batch" is the portion of the input batch that is being
written to the same table.
