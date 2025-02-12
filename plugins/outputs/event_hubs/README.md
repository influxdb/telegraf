# Azure Event Hubs Output Plugin

This plugin writes metrics to the [Azure Event Hubs][event_hubs] service in any
of the supported [data formats][data_formats]. Metrics are sent as batches with
each message payload containing one metric object, preferably as JSON as this
eases integration with downstream components.

Each patch is sent to a single Event Hub within a namespace. In case no
partition key is specified the batches will be automatically load-balanced
(round-robin) across all the Event Hub partitions.

⭐ Telegraf v1.21.0
🏷️ cloud,datastores
💻 all

[event_hubs]: https://azure.microsoft.com/en-gb/services/event-hubs/
[data_formats]: /docs/DATA_FORMATS_OUTPUT.md

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Configuration

```toml @sample.conf
# Configuration for Event Hubs output plugin
[[outputs.event_hubs]]
  ## Full connection string to the Event Hub instance. The shared access key
  ## must have "Send" permissions on the target Event Hub.
  connection_string = "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=superSecret1234=;EntityPath=hubName"

  ## Partition key to use for the event
  ## Metric tag or field name to use for the event partition key. The value of
  ## this tag or field is set as the key for events if it exists. If both, tag
  ## and field, exist the tag is preferred.
  # partition_key = ""

  ## Set the maximum batch message size in bytes
  ## The allowable size depends on the Event Hub tier, see
  ##   https://learn.microsoft.com/azure/event-hubs/event-hubs-quotas#basic-vs-standard-vs-premium-vs-dedicated-tiers
  ## for details. If unset the default size defined by Azure Event Hubs is
  ## used (currently 1,000,000 bytes)
  # max_message_size = "1MB"

  ## Timeout for sending the data
  # timeout = "30s"

  ## Data format to output.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_OUTPUT.md
  data_format = "json"
```
