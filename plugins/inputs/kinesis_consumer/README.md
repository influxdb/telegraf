# Kinesis Consumer Input Plugin

This plugin consumes records from [AWS Kinesis][kinesis] data stream and
creates metrics using one of the supported [data formats][data_formats].

[kinesis]: https://aws.amazon.com/kinesis/
[data_formats]: /docs/DATA_FORMATS_INPUT.md

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
# Configuration for the AWS Kinesis input.
[[inputs.kinesis_consumer]]
  ## Amazon REGION of kinesis endpoint.
  region = "ap-southeast-2"

  ## Amazon Credentials
  ## Credentials are loaded in the following order
  ## 1) Web identity provider credentials via STS if role_arn and web_identity_token_file are specified
  ## 2) Assumed credentials via STS if role_arn is specified
  ## 3) explicit credentials from 'access_key' and 'secret_key'
  ## 4) shared profile from 'profile'
  ## 5) environment variables
  ## 6) shared credentials file
  ## 7) EC2 Instance Profile
  # access_key = ""
  # secret_key = ""
  # token = ""
  # role_arn = ""
  # web_identity_token_file = ""
  # role_session_name = ""
  # profile = ""
  # shared_credential_file = ""

  ## Endpoint to make request against, the correct endpoint is automatically
  ## determined and this option should only be set if you wish to override the
  ## default.
  ##   ex: endpoint_url = "http://localhost:8000"
  # endpoint_url = ""

  ## Kinesis StreamName must exist prior to starting telegraf.
  streamname = "StreamName"

  ## Shard iterator type
  ## Available options: 'TRIM_HORIZON' (first in non-expired) and 'LATEST'
  # shard_iterator_type = "TRIM_HORIZON"

  ## Interval for checking for new records
  ## Please consider limits for getting records documented here:
  ## https://docs.aws.amazon.com/streams/latest/dev/service-sizes-and-limits.html
  # poll_interval = "250ms"

  ## Interval for scanning for new shards created when resharding
  ## If set to zero, shards are only scanned once on startup.
  # shard_update_interval = "30s"

  ## Max undelivered messages
  ## This plugin uses tracking metrics, which ensure messages are read to
  ## outputs before acknowledging them to the original broker to ensure data
  ## is not lost. This option sets the maximum messages to read from the
  ## broker that have not been written by an output.
  ##
  ## This value needs to be picked with awareness of the agent's
  ## metric_batch_size value as well. Setting max undelivered messages too high
  ## can result in a constant stream of data batches to the output. While
  ## setting it too low may never flush the broker's messages.
  # max_undelivered_messages = 1000

  ## Content encoding of the record data
  ## If you are processing a cloudwatch logs kinesis stream then set this to
  ## "gzip" as AWS compresses cloudwatch log data before it is sent to kinesis.
  # content_encoding = "identity"

  ## Data format of the records to consume
  ## See https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  # data_format = "influx"

  ## Optional: Configuration for DynamoDB backend to store positions in the stream
  # [inputs.kinesis_consumer.checkpoint_dynamodb]
  #   ## Unique name for this consumer
  #   app_name = "default"
  #   ## Table to store the sequence numbers in
  #   table_name = "default"
  #   ## Interval for persisting data to limit write operations
  #   # interval = "10s"
```

### Required AWS IAM permissions

Kinesis:

- DescribeStream
- GetRecords
- GetShardIterator

DynamoDB:

- GetItem
- PutItem

### DynamoDB Checkpoint

The DynamoDB checkpoint stores the last processed record in a DynamoDB. To
leverage this functionality, create a table with the following string type keys:

```shell
Partition key: namespace
Sort key: shard_id
```

## Metrics

## Example Output
