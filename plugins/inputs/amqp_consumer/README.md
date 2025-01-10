# AMQP Consumer Input Plugin

This plugin consumes messages from an Advanced Message Queuing Protocol v0.9.1
broker. A prominent implementation of this protocol is [RabbitMQ][rabbitmq].

Metrics are read from a topic exchange using the configured queue and binding
key. The message payloads must be formatted in one of the supported
[data formats][data_formats].

For an introduction check the [AMQP concepts page][amqp_concepts] and the
[RabbitMQ getting started guide][rabbitmq_getting_started].

⭐ Telegraf v1.3.0
🏷️ messaging
💻 all

[amqp_concepts]: https://www.rabbitmq.com/tutorials/amqp-concepts.html
[data_formats]: /docs/DATA_FORMATS_INPUT.md
[rabbitmq]: https://www.rabbitmq.com
[rabbitmq_getting_started]: https://www.rabbitmq.com/getstarted.html

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

This plugin supports secrets from secret-stores for the `username` and
`password` option.
See the [secret-store documentation][SECRETSTORE] for more details on how
to use them.

[SECRETSTORE]: ../../../docs/CONFIGURATION.md#secret-store-secrets

## Configuration

```toml @sample.conf
# AMQP consumer plugin
[[inputs.amqp_consumer]]
  ## Brokers to consume from.  If multiple brokers are specified a random broker
  ## will be selected anytime a connection is established.  This can be
  ## helpful for load balancing when not using a dedicated load balancer.
  brokers = ["amqp://localhost:5672/influxdb"]

  ## Authentication credentials for the PLAIN auth_method.
  # username = ""
  # password = ""

  ## Name of the exchange to declare.  If unset, no exchange will be declared.
  exchange = "telegraf"

  ## Exchange type; common types are "direct", "fanout", "topic", "header", "x-consistent-hash".
  # exchange_type = "topic"

  ## If true, exchange will be passively declared.
  # exchange_passive = false

  ## Exchange durability can be either "transient" or "durable".
  # exchange_durability = "durable"

  ## Additional exchange arguments.
  # exchange_arguments = { }
  # exchange_arguments = {"hash_property" = "timestamp"}

  ## AMQP queue name.
  queue = "telegraf"

  ## AMQP queue durability can be "transient" or "durable".
  queue_durability = "durable"

  ## If true, queue will be passively declared.
  # queue_passive = false

  ## Additional arguments when consuming from Queue
  # queue_consume_arguments = { }
  # queue_consume_arguments = {"x-stream-offset" = "first"}

  ## Additional queue arguments.
  # queue_arguments = { }
  # queue_arguments = {"x-max-length" = 100}

  ## A binding between the exchange and queue using this binding key is
  ## created.  If unset, no binding is created.
  binding_key = "#"

  ## Maximum number of messages server should give to the worker.
  # prefetch_count = 50

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

  ## Timeout for establishing the connection to a broker
  # timeout = "30s"

  ## Auth method. PLAIN and EXTERNAL are supported
  ## Using EXTERNAL requires enabling the rabbitmq_auth_mechanism_ssl plugin as
  ## described here: https://www.rabbitmq.com/plugins.html
  # auth_method = "PLAIN"

  ## Optional TLS Config
  # tls_ca = "/etc/telegraf/ca.pem"
  # tls_cert = "/etc/telegraf/cert.pem"
  # tls_key = "/etc/telegraf/key.pem"
  ## Use TLS but skip chain & host verification
  # insecure_skip_verify = false

  ## Content encoding for message payloads, can be set to
  ## "gzip", "identity" or "auto"
  ## - Use "gzip" to decode gzip
  ## - Use "identity" to apply no encoding
  ## - Use "auto" determine the encoding using the ContentEncoding header
  # content_encoding = "identity"

  ## Maximum size of decoded message.
  ## Acceptable units are B, KiB, KB, MiB, MB...
  ## Without quotes and units, interpreted as size in bytes.
  # max_decompression_size = "500MB"

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "influx"
```

## Message acknowledgement behavior

This plugin tracks metrics to report the delivery state to the broker.

Messages are **acknowledged** (ACK) in the broker if they were successfully
parsed and delivered to all corresponding output sinks.

Messages are **not acknowledged** (NACK) if parsing of the messages fails and no
metrics were created. In this case requeueing is disabled so messages will not
be sent out to any other queue. The message will then be discarded or sent to a
dead-letter exchange depending on the server configuration. See
[RabitMQ documentation][rabbitmq_doc] for more details.

Messages are **rejected** (REJECT) if the messages were parsed correctly but
could not be delivered e.g. due to output-service outages. Requeueing is
disabled in this case and messages will be discarded by the server. See
[RabitMQ documentation][rabbitmq_doc] for more details.

[rabbitmq_doc]: https://www.rabbitmq.com/docs/confirms

## Metrics

The format of metrics produced by this plugin depends on the content and
data format of received messages.

## Example Output
