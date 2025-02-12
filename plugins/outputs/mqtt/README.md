# MQTT Producer Output Plugin

This plugin writes metrics to a [MQTT broker][mqtt] acting as a MQTT producer.
The plugin supports the MQTT protocols `3.1.1` and `5`.

> [!NOTE]
> In v2.0.12+ of the mosquitto MQTT server, there is a [bug][mosquitto_bug]
> requiring the `keep_alive` value to be set non-zero in Telegraf. Otherwise,
> the server will return with `identifier rejected`.
> As a reference `eclipse/paho.golang` sets the `keep_alive` to 30.

⭐ Telegraf v0.2.0
🏷️ messaging
💻 all

[mqtt]: http://http://mqtt.org/
[mosquitto_bug]: https://github.com/eclipse/mosquitto/issues/2117

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support
additional global and plugin configuration settings. These settings are used to
modify metrics, tags, and field or create aliases and configure ordering, etc.
See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md#plugins

## Secret-store support

This plugin supports secrets from secret-stores for the `username` and
`password` option.
See the [secret-store documentation][SECRETSTORE] for more details on how
to use them.

[SECRETSTORE]: ../../../docs/CONFIGURATION.md#secret-store-secrets

## Configuration

```toml @sample.conf
# Configuration for MQTT server to send metrics to
[[outputs.mqtt]]
  ## MQTT Brokers
  ## The list of brokers should only include the hostname or IP address and the
  ## port to the broker. This should follow the format `[{scheme}://]{host}:{port}`. For
  ## example, `localhost:1883` or `mqtt://localhost:1883`.
  ## Scheme can be any of the following: tcp://, mqtt://, tls://, mqtts://
  ## non-TLS and TLS servers can not be mix-and-matched.
  servers = ["localhost:1883", ] # or ["mqtts://tls.example.com:1883"]

  ## Protocol can be `3.1.1` or `5`. Default is `3.1.1`
  # protocol = "3.1.1"

  ## MQTT Topic for Producer Messages
  ## MQTT outputs send metrics to this topic format:
  ## {{ .TopicPrefix }}/{{ .Hostname }}/{{ .PluginName }}/{{ .Tag "tag_key" }}
  ## (e.g. prefix/web01.example.com/mem/some_tag_value)
  ## Each path segment accepts either a template placeholder, an environment variable, or a tag key
  ## of the form `{{.Tag "tag_key_name"}}`. All the functions provided by the Sprig library
  ## (http://masterminds.github.io/sprig/) are available. Empty path elements as well as special MQTT
  ## characters (such as `+` or `#`) are invalid to form the topic name and will lead to an error.
  ## In case a tag is missing in the metric, that path segment omitted for the final topic.
  topic = "telegraf/{{ .Hostname }}/{{ .PluginName }}"

  ## QoS policy for messages
  ## The mqtt QoS policy for sending messages.
  ## See https://www.ibm.com/support/knowledgecenter/en/SSFKSJ_9.0.0/com.ibm.mq.dev.doc/q029090_.htm
  ##   0 = at most once
  ##   1 = at least once
  ##   2 = exactly once
  # qos = 2

  ## Keep Alive
  ## Defines the maximum length of time that the broker and client may not
  ## communicate. Defaults to 0 which turns the feature off.
  ##
  ## For version v2.0.12 and later mosquitto there is a bug
  ## (see https://github.com/eclipse/mosquitto/issues/2117), which requires
  ## this to be non-zero. As a reference eclipse/paho.mqtt.golang defaults to 30.
  # keep_alive = 0

  ## username and password to connect MQTT server.
  # username = "telegraf"
  # password = "metricsmetricsmetricsmetrics"

  ## client ID
  ## The unique client id to connect MQTT server. If this parameter is not set
  ## then a random ID is generated.
  # client_id = ""

  ## Timeout for write operations. default: 5s
  # timeout = "5s"

  ## Optional TLS Config
  # tls_ca = "/etc/telegraf/ca.pem"
  # tls_cert = "/etc/telegraf/cert.pem"
  # tls_key = "/etc/telegraf/key.pem"

  ## Use TLS but skip chain & host verification
  # insecure_skip_verify = false

  ## When true, metrics will be sent in one MQTT message per flush. Otherwise,
  ## metrics are written one metric per MQTT message.
  ## DEPRECATED: Use layout option instead
  # batch = false

  ## When true, metric will have RETAIN flag set, making broker cache entries until someone
  ## actually reads it
  # retain = false

  ## Client trace messages
  ## When set to true, and debug mode enabled in the agent settings, the MQTT
  ## client's messages are included in telegraf logs. These messages are very
  ## noisey, but essential for debugging issues.
  # client_trace = false

  ## Layout of the topics published.
  ## The following choices are available:
  ##   non-batch -- send individual messages, one for each metric
  ##   batch     -- send all metric as a single message per MQTT topic
  ## NOTE: The following options will ignore the 'data_format' option and send single values
  ##   field     -- send individual messages for each field, appending its name to the metric topic
  ##   homie-v4  -- send metrics with fields and tags according to the 4.0.0 specs
  ##                see https://homieiot.github.io/specification/
  # layout = "non-batch"

  ## HOMIE specific settings
  ## The following options provide templates for setting the device name
  ## and the node-ID for the topics. Both options are MANDATORY and can contain
  ## {{ .PluginName }} (metric name), {{ .Tag "key"}} (tag reference to 'key')
  ## or constant strings. The templays MAY NOT contain slashes!
  # homie_device_name = ""
  # homie_node_id = ""

  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_OUTPUT.md
  data_format = "influx"

  ## NOTE: Due to the way TOML is parsed, tables must be at the END of the
  ## plugin definition, otherwise additional config options are read as part of
  ## the table

  ## Optional MQTT 5 publish properties
  ## These setting only apply if the "protocol" property is set to 5. This must
  ## be defined at the end of the plugin settings, otherwise TOML will assume
  ## anything else is part of this table. For more details on publish properties
  ## see the spec:
  ## https://docs.oasis-open.org/mqtt/mqtt/v5.0/os/mqtt-v5.0-os.html#_Toc3901109
  # [outputs.mqtt.v5]
  #   content_type = ""
  #   response_topic = ""
  #   message_expiry = "0s"
  #   topic_alias = 0
  # [outputs.mqtt.v5.user_properties]
  #   "key1" = "value 1"
  #   "key2" = "value 2"
```

### `field` layout

This layout will publish one topic per metric __field__, only containing the
value as string. This means that the `data_format` option will be ignored.

For example writing the metrics

```text
modbus,location=main\ building,source=device\ 1,status=ok,type=Machine\ A temperature=21.4,serial\ number="324nlk234r5u9834t",working\ hours=123i,supplied=true 1676522982000000000
modbus,location=main\ building,source=device\ 2,status=offline,type=Machine\ B temperature=25.0,supplied=true 1676522982000000000
```

with configuration

```toml
[[outputs.mqtt]]
  topic = 'telegraf/{{ .PluginName }}/{{ .Tag "source" }}'
  layout = "field"
  ...
```

will result in the following topics and values

```text
telegraf/modbus/device 1/temperature    21.4
telegraf/modbus/device 1/serial number  324nlk234r5u9834t
telegraf/modbus/device 1/supplied       true
telegraf/modbus/device 1/working hours  123
telegraf/modbus/device 2/temperature    25
telegraf/modbus/device 2/supplied       false
```

__NOTE__: Only fields will be output, tags and the timestamp are omitted. To
also output those, please convert them to fields first.

### `homie-v4` layout

This layout will publish metrics according to the
[Homie v4.0 specification][HomieSpecV4]. Here, the `topic` template will be
used to specify the `device-id` path. The __mandatory__ options
`homie_device_name` will specify the content of the `$name` topic of the device,
while `homie_node_id` will provide a template for the `node-id` part of the
topic. Both options can contain [Go templates][GoTemplates] similar to `topic`
with `{{ .PluginName }}` referencing the metric name and `{{ .Tag "key"}}`
referencing the tag with the name `key`.

For example writing the metrics

```text
modbus,source=device\ 1,location=main\ building,type=Machine\ A,status=ok temperature=21.4,serial\ number="324nlk234r5u9834t",working\ hours=123i,supplied=true 1676522982000000000
modbus,source=device\ 2,location=main\ building,type=Machine\ B,status=offline supplied=false 1676522982000000000
modbus,source=device\ 2,location=main\ building,type=Machine\ B,status=online supplied=true,Throughput=12345i,Load\ [%]=81.2,account\ no="T3L3GrAf",Temperature=25.38,Voltage=24.1,Current=100 1676542982000000000
```

with configuration

```toml
[[outputs.mqtt]]
  topic = 'telegraf/{{ .PluginName }}'
  layout = "homie-v4"

  homie_device_name ='{{.PluginName}} plugin'
  homie_node_id = '{{.Tag "source"}}'
  ...
```

will result in the following topics and values

```text
telegraf/modbus/$homie                            4.0
telegraf/modbus/$name                             modbus plugin
telegraf/modbus/$state                            ready
telegraf/modbus/$nodes                            device-1

telegraf/modbus/device-1/$name                    device 1
telegraf/modbus/device-1/$properties              location,serial-number,source,status,supplied,temperature,type,working-hours

telegraf/modbus/device-1/location                 main building
telegraf/modbus/device-1/location/$name           location
telegraf/modbus/device-1/location/$datatype       string
telegraf/modbus/device-1/status                   ok
telegraf/modbus/device-1/status/$name             status
telegraf/modbus/device-1/status/$datatype         string
telegraf/modbus/device-1/type                     Machine A
telegraf/modbus/device-1/type/$name               type
telegraf/modbus/device-1/type/$datatype           string
telegraf/modbus/device-1/source                   device 1
telegraf/modbus/device-1/source/$name             source
telegraf/modbus/device-1/source/$datatype         string
telegraf/modbus/device-1/temperature              21.4
telegraf/modbus/device-1/temperature/$name        temperature
telegraf/modbus/device-1/temperature/$datatype    float
telegraf/modbus/device-1/serial-number            324nlk234r5u9834t
telegraf/modbus/device-1/serial-number/$name      serial number
telegraf/modbus/device-1/serial-number/$datatype  string
telegraf/modbus/device-1/working-hours            123
telegraf/modbus/device-1/working-hours/$name      working hours
telegraf/modbus/device-1/working-hours/$datatype  integer
telegraf/modbus/device-1/supplied                 true
telegraf/modbus/device-1/supplied/$name           supplied
telegraf/modbus/device-1/supplied/$datatype       boolean

telegraf/modbus/$nodes                            device-1,device-2

telegraf/modbus/device-2/$name                    device 2
telegraf/modbus/device-2/$properties              location,source,status,supplied,type

telegraf/modbus/device-2/location                 main building
telegraf/modbus/device-2/location/$name           location
telegraf/modbus/device-2/location/$datatype       string
telegraf/modbus/device-2/status                   offline
telegraf/modbus/device-2/status/$name             status
telegraf/modbus/device-2/status/$datatype         string
telegraf/modbus/device-2/type                     Machine B
telegraf/modbus/device-2/type/$name               type
telegraf/modbus/device-2/type/$datatype           string
telegraf/modbus/device-2/source                   device 2
telegraf/modbus/device-2/source/$name             source
telegraf/modbus/device-2/source/$datatype         string
telegraf/modbus/device-2/supplied                 false
telegraf/modbus/device-2/supplied/$name           supplied
telegraf/modbus/device-2/supplied/$datatype       boolean

telegraf/modbus/device-2/$properties              account-no,current,load,location,source,status,supplied,temperature,throughput,type,voltage

telegraf/modbus/device-2/location                 main building
telegraf/modbus/device-2/location/$name           location
telegraf/modbus/device-2/location/$datatype       string
telegraf/modbus/device-2/status                   online
telegraf/modbus/device-2/status/$name             status
telegraf/modbus/device-2/status/$datatype         string
telegraf/modbus/device-2/type                     Machine B
telegraf/modbus/device-2/type/$name               type
telegraf/modbus/device-2/type/$datatype           string
telegraf/modbus/device-2/source                   device 2
telegraf/modbus/device-2/source/$name             source
telegraf/modbus/device-2/source/$datatype         string
telegraf/modbus/device-2/temperature              25.38
telegraf/modbus/device-2/temperature/$name        Temperature
telegraf/modbus/device-2/temperature/$datatype    float
telegraf/modbus/device-2/voltage                  24.1
telegraf/modbus/device-2/voltage/$name            Voltage
telegraf/modbus/device-2/voltage/$datatype        float
telegraf/modbus/device-2/current                  100
telegraf/modbus/device-2/current/$name            Current
telegraf/modbus/device-2/current/$datatype        float
telegraf/modbus/device-2/throughput               12345
telegraf/modbus/device-2/throughput/$name         Throughput
telegraf/modbus/device-2/throughput/$datatype     integer
telegraf/modbus/device-2/load                     81.2
telegraf/modbus/device-2/load/$name               Load [%]
telegraf/modbus/device-2/load/$datatype           float
telegraf/modbus/device-2/account-no               T3L3GrAf
telegraf/modbus/device-2/account-no/$name         account no
telegraf/modbus/device-2/account-no/$datatype     string
telegraf/modbus/device-2/supplied                 true
telegraf/modbus/device-2/supplied/$name           supplied
telegraf/modbus/device-2/supplied/$datatype       boolean
```

#### Important notes and limitations

It is important to notice that the __"devices" and "nodes" are dynamically
changing__ in Telegraf as the metrics and their structure is not known a-priori.
As a consequence, the content of both `$nodes` and `$properties` topics are
changing as new `device-id`s, `node-id`s and `properties` (i.e. tags and fields)
appear. Best effort is made to limit the number of changes by keeping a
superset of all devices and nodes seen, however especially during startup those
topics will change more often. Both `topic` and `homie_node_id` should be chosen
in a way to group metrics with identical structure!

Furthermore, __lifecycle management of devices is very limited__! Devices will
only be in `ready` state due to the dynamic nature of Telegraf. Due to
limitations in the MQTT client library, it is not possible to set a "will"
dynamically. In consequence, devices are only marked `lost` when exiting
Telegraf normally and might not change in abnormal aborts.

Note that __all field- and tag-names are automatically converted__ to adhere to
the [Homie topic ID specification][HomieSpecV4TopicIDs]. In that process, the
names are converted to lower-case and forbidden character sequences (everything
not being a lower-case character, digit or hyphen) will be replaces by a hyphen.
Finally, leading and trailing hyphens are removed.
This is important as there is a __risk of name collisions__ between fields and
tags of the same node especially after the conversion to ID. Please __make sure
to avoid those collisions__ as otherwise property topics will be sent multiple
times for the colliding items.

[HomieSpecV4]: https://homieiot.github.io/specification/spec-core-v4_0_0
[GoTemplates]: https://pkg.go.dev/text/template
[HomieSpecV4TopicIDs]: https://homieiot.github.io/specification/#topic-ids
