//go:generate ../../../tools/readme_config_includer/generator
package event_hubs

import (
	_ "embed"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	eh_commons "github.com/influxdata/telegraf/plugins/common/eventhub"
	"github.com/influxdata/telegraf/plugins/outputs"
)

/* End wrapper interface */

type EventHubs struct {
	eh_commons.EventHubs
}

const (
	defaultRequestTimeout = time.Second * 30
)

//go:embed sample.conf
var sampleConfig string

func (e *EventHubs) SampleConfig() string {
	return sampleConfig
}

func (e *EventHubs) Init() error {
	return e.EventHubs.Init()
}

func (e *EventHubs) Connect() error {
	return nil
}

func (e *EventHubs) Close() error {
	return e.EventHubs.Close()
}

func (e *EventHubs) SetSerializer(serializer telegraf.Serializer) {
	e.EventHubs.SetSerializer(serializer)
}

func (e *EventHubs) Write(metrics []telegraf.Metric) error {
	return e.EventHubs.Write(metrics)
}

func init() {
	outputs.Add("event_hubs", func() telegraf.Output {
		return &EventHubs{
			EventHubs: eh_commons.EventHubs{
				Hub:     &eh_commons.EventHub{},
				Timeout: config.Duration(defaultRequestTimeout),
			},
		}
	})
}
