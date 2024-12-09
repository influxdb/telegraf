# OPC UA Event Monitoring Telegraf Plugin
This custom Telegraf input plugin, `opcua_event_subscription`, enables monitoring of OPC UA events by subscribing to specific node IDs and filtering events based on event_type and source_name. The plugin also supports secure OPC UA connections, allowing the use of client certificates and private keys for encrypted communication with the server.

## Features
- Connects to an OPC UA server to subscribe to a specified event_type.
- Filters events based on source_name and event_type.
- Allows configuration of specific node IDs and fields to monitor for event data.
- Supports secure OPC UA connections, including options for setting SecurityMode (None, Sign, SignAndEncrypt) and SecurityPolicy (None, Basic256Sha256).
- Allows the use of client certificates and private keys for secure communication with the OPC UA server, enabling encrypted connections.

## Requirements
- [Telegraf](https://www.influxdata.com/time-series-platform/telegraf/)
- Go environment for building the plugin.
- An accessible OPC UA server with alarms and conditions support.
- [gopcua](https://github.com/gopcua/opcua) Go client library.

## Installation
1. Place the `opcua_event_subscription` plugin in the Telegraf plugin directory.
2. Ensure the `opcua_event_subscription` directory is included in your Go path.
3. Build and install the plugin according to Telegraf’s external plugin documentation.

## Configuration
Add the following configuration block to your `telegraf.conf` file and adjust the stanzas:
For multiple event_types please use multiple Input Plugins.

```toml
[[inputs.opcua_event_subscription]]
    ## OPC UA Server Endpoint
    endpoint = "opc.tcp://localhost:4840"

    ## Polling interval
    interval = "10s"

    ## Event Type Filter
    event_type = "ns=0;i=2041"

    ## Node IDs to subscribe to
    node_ids = ["ns=2;s=0:East/Blue"]

    ## Source Name Filter (optional)
    source_names = ["SourceName1", "SourceName2"]

    ## Fields to retrieve (optional)
    fields = ["Severity", "Message", "Time"]

    ## Security mode and policy (optional)
    security_mode = "None"
    security_policy = "None"

    ## Client certificate and key (optional)
    certificate = "/path/to/cert.pem"
    private_key = "/path/to/key.pem"
```

## Configuration Parameters
- `endpoint` The OPC UA server’s endpoint URL.
- `interval` Polling interval for data collection, e.g., 10s.
- `node_ids` A list of OPC UA node identifiers (NodeIds) specifying the nodes to monitor for event notifications, which are associated with the defined event type.
- `event_type` Defines the type or level of events to capture from the monitored nodes.
- `source_names` Specifies OPCUA Event source_names to filter on
- `fields` Specifies the fields to capture from event notifications.
- `security_mode` Sets the OPC UA security mode (None, Sign, SignAndEncrypt).
- `security_policy` Specifies the OPC UA security policy (None, Basic256Sha256).
- `certificate` Path to the client certificate (in PEM format) if required.
- `private_key` Path to the private key (in PEM format) if required.

## Security
If secure connections are required, set security_mode and security_policy based on the OPC UA server’s requirements. Provide paths to certificate and private_key in PEM format.

## How it works
Once Telegraf starts with this plugin, it establishes a connection to the OPC UA server, subscribes to the specified event_type’s Node-ID, and collects events that meet the defined criteria.
The `node_ids` parameter specifies the nodes to monitor for events (monitored items). However, the actual subscription is based on the `event_type`, which determines the events that are capture.

##  Troubleshooting
	1.	Ensure this plugin directory is in Telegraf’s Go path.
	2.	Compile and run Telegraf with this plugin enabled to verify the connection and data collection.
	3.	Check the Telegraf logs for any configuration or connection errors, and troubleshoot accordingly.

## Development
For testing purposes, you can test the plugin using the `opcua_event_subscription_test` file. The tests will automatically use the `SampleConfig defined in the plugin and connect to a demo OPC UA server to perform subscriptions.
To run the tests, simply execute the following command:
```batch
    go test -v
```

## Limitations
- Does not allow multiple event_types within one subscription. To subscribe to multiple event_types use multiple input plugins within your telegraf.conf.
- Where-Filter is limited to the OPC-UA field source_name.
- This Plugin is only developed for event notifications. Data Change notifications are not supported.
- SamplingInterval is set to  10000.0 // 10 sec.
- QueueSize is set to 10.
- All retrieved fields are forwarded as `fields`, while the opcua_host and the node_id that triggers the event are forwarded as `tags`

## Contributing
For bugs or feature requests, contact frederik.moschner@sva.de.