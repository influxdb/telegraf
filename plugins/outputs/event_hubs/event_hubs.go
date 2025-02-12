//go:generate ../../../tools/readme_config_includer/generator
package event_hubs

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

type EventHubs struct {
	ConnectionString string          `toml:"connection_string"`
	PartitionKey     string          `toml:"partition_key"`
	MaxMessageSize   config.Size     `toml:"max_message_size"`
	Timeout          config.Duration `toml:"timeout"`
	Log              telegraf.Logger `toml:"-"`

	client     *azeventhubs.ProducerClient
	options    azeventhubs.EventDataBatchOptions
	serializer telegraf.Serializer
}

func (*EventHubs) SampleConfig() string {
	return sampleConfig
}

func (e *EventHubs) Init() error {
	if e.MaxMessageSize > 0 {
		e.options.MaxBytes = uint64(e.MaxMessageSize)
	}

	return nil
}

func (e *EventHubs) Connect() error {
	cfg := &azeventhubs.ProducerClientOptions{
		ApplicationID: internal.FormatFullVersion(),
		RetryOptions:  azeventhubs.RetryOptions{MaxRetries: -1},
	}

	client, err := azeventhubs.NewProducerClientFromConnectionString(e.ConnectionString, "", cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	e.client = client

	return nil
}

func (e *EventHubs) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	return e.client.Close(ctx)
}

func (e *EventHubs) SetSerializer(serializer telegraf.Serializer) {
	e.serializer = serializer
}

func (e *EventHubs) Write(metrics []telegraf.Metric) error {
	ctx := context.Background()

	batchOptions := e.options
	batches := make(map[string]*azeventhubs.EventDataBatch)
	for i := 0; i < len(metrics); i++ {
		m := metrics[i]

		// Prepare the payload
		payload, err := e.serializer.Serialize(m)
		if err != nil {
			e.Log.Errorf("Could not serialize metric: %v", err)
			e.Log.Tracef("metric: %+v", m)
			continue
		}

		// Get the batcher for the chosen partition
		partition := "<default>"
		batchOptions.PartitionKey = nil
		if e.PartitionKey != "" {
			if key, ok := m.GetTag(e.PartitionKey); ok {
				partition = key
				batchOptions.PartitionKey = &partition
			} else if key, ok := m.GetField(e.PartitionKey); ok {
				if k, ok := key.(string); ok {
					partition = k
					batchOptions.PartitionKey = &partition
				}
			}
		}
		if _, found := batches[partition]; !found {
			batches[partition], err = e.client.NewEventDataBatch(ctx, &batchOptions)
			if err != nil {
				return fmt.Errorf("creating batch for partition %q failed: %w", partition, err)
			}
		}

		// Add the event to the partition and send it if the batch is full
		err = batches[partition].AddEventData(&azeventhubs.EventData{Body: payload}, nil)
		if err == nil {
			continue
		}

		// If the event doesn't fit into the batch anymore, send the batch
		if !errors.Is(err, azeventhubs.ErrEventDataTooLarge) {
			return fmt.Errorf("adding metric to batch for partition %q failed: %w", partition, err)
		}

		// The event is larger than the maximum allowed size so there
		// is nothing we can do here but have to drop the metric.
		if batches[partition].NumEvents() == 0 {
			e.Log.Errorf("Metric with %d bytes exceeds the maximum allowed size and must be dropped!", len(payload))
			e.Log.Tracef("metric: %+v", m)
			continue
		}
		if err := e.send(batches[partition]); err != nil {
			return fmt.Errorf("sending batch for partition %q failed: %w", partition, err)
		}

		// Create a new metric and reiterate over the current metric to be
		// added in the next iteration of the for loop.
		batches[partition], err = e.client.NewEventDataBatch(ctx, &e.options)
		if err != nil {
			return fmt.Errorf("creating batch for partition %q failed: %w", partition, err)
		}
		i--
	}

	// Send the remaining batches that never exceeded the batch size
	for partition, batch := range batches {
		if batch.NumBytes() == 0 {
			continue
		}
		if err := e.send(batch); err != nil {
			return fmt.Errorf("sending batch for partition %q failed: %w", partition, err)
		}
	}
	return nil
}

func (e *EventHubs) send(batch *azeventhubs.EventDataBatch) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	return e.client.SendEventDataBatch(ctx, batch, nil)
}

func init() {
	outputs.Add("event_hubs", func() telegraf.Output {
		return &EventHubs{
			Timeout: config.Duration(30 * time.Second),
		}
	})
}
