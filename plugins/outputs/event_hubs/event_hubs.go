//go:generate ../../../tools/readme_config_includer/generator
package event_hubs

import (
	"context"
	_ "embed"
	"time"

	eventhub "github.com/Azure/azure-event-hubs-go/v3"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

/*
** Wrapper interface for eventhub.Hub
 */

type EventHubInterface interface {
	GetHub(s string) error
	Close(ctx context.Context) error
	SendBatch(ctx context.Context, iterator eventhub.BatchIterator, opts ...eventhub.BatchOption) error
}

type eventHub struct {
	hub *eventhub.Hub
}

func (eh *eventHub) GetHub(s string) error {
	hub, err := eventhub.NewHubFromConnectionString(s)

	if err != nil {
		return err
	}

	eh.hub = hub

	return nil
}

func (eh *eventHub) Close(ctx context.Context) error {
	return eh.hub.Close(ctx)
}

func (eh *eventHub) SendBatch(ctx context.Context, iterator eventhub.BatchIterator, opts ...eventhub.BatchOption) error {
	return eh.hub.SendBatch(ctx, iterator, opts...)
}

/* End wrapper interface */

type EventHubs struct {
	Log              telegraf.Logger `toml:"-"`
	ConnectionString string          `toml:"connection_string"`
	Timeout          config.Duration `toml:"timeout"`
	PartitionKey     string          `toml:"partition_key"`
	MaxMessageSize   int             `toml:"max_message_size"`

	Hub          EventHubInterface
	batchOptions []eventhub.BatchOption
	serializer   telegraf.Serializer
}

const (
	defaultRequestTimeout = time.Second * 30
)

func (*EventHubs) SampleConfig() string {
	return sampleConfig
}

func (e *EventHubs) Init() error {
	err := e.Hub.GetHub(e.ConnectionString)

	if err != nil {
		return err
	}

	if e.MaxMessageSize > 0 {
		e.batchOptions = append(e.batchOptions, eventhub.BatchWithMaxSizeInBytes(e.MaxMessageSize))
	}

	return nil
}

func (e *EventHubs) Connect() error {
	return nil
}

func (e *EventHubs) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	err := e.Hub.Close(ctx)

	if err != nil {
		return err
	}

	return nil
}

func (e *EventHubs) SetSerializer(serializer telegraf.Serializer) {
	e.serializer = serializer
}

func (e *EventHubs) Write(metrics []telegraf.Metric) error {
	events := make([]*eventhub.Event, 0, len(metrics))
	for _, metric := range metrics {
		payload, err := e.serializer.Serialize(metric)

		if err != nil {
			e.Log.Debugf("Could not serialize metric: %v", err)
			continue
		}

		event := eventhub.NewEvent(payload)
		if e.PartitionKey != "" {
			if key, ok := metric.GetTag(e.PartitionKey); ok {
				event.PartitionKey = &key
			} else if key, ok := metric.GetField(e.PartitionKey); ok {
				if strKey, ok := key.(string); ok {
					event.PartitionKey = &strKey
				}
			}
		}

		events = append(events, event)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	err := e.Hub.SendBatch(ctx, eventhub.NewEventBatchIterator(events...), e.batchOptions...)

	if err != nil {
		return err
	}

	return nil
}

func init() {
	outputs.Add("event_hubs", func() telegraf.Output {
		return &EventHubs{
			Hub:     &eventHub{},
			Timeout: config.Duration(defaultRequestTimeout),
		}
	})
}
