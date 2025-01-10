package cloudevents

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/gofrs/uuid/v5"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/serializers"
)

const (
	EventTypeSingle = "com.influxdata.telegraf.metric"
	EventTypeBatch  = "com.influxdata.telegraf.metrics"
)

type Serializer struct {
	Version     string          `toml:"cloudevents_version"`
	Source      string          `toml:"cloudevents_source"`
	SourceTag   string          `toml:"cloudevents_source_tag"`
	EventType   string          `toml:"cloudevents_event_type"`
	EventTime   string          `toml:"cloudevents_event_time"`
	BatchFormat string          `toml:"cloudevents_batch_format"`
	Log         telegraf.Logger `toml:"-"`

	idgen uuid.Generator
}

func (s *Serializer) Init() error {
	switch s.Version {
	case "":
		s.Version = event.CloudEventsVersionV1
	case event.CloudEventsVersionV03, event.CloudEventsVersionV1:
	default:
		return errors.New("invalid 'cloudevents_version'")
	}

	switch s.EventTime {
	case "":
		s.EventTime = "latest"
	case "none", "earliest", "latest", "creation":
	default:
		return errors.New("invalid 'cloudevents_event_time'")
	}

	switch s.BatchFormat {
	case "":
		s.BatchFormat = "events"
	case "metrics", "events":
	default:
		return errors.New("invalid 'cloudevents_batch_format'")
	}

	if s.Source == "" {
		s.Source = "telegraf"
	}

	s.idgen = uuid.NewGen()

	return nil
}

func (s *Serializer) Serialize(m telegraf.Metric) ([]byte, error) {
	// Create the event that forms the envelop around the metric
	evt, err := s.createEvent(m)
	if err != nil {
		return nil, err
	}
	return evt.MarshalJSON()
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	switch s.BatchFormat {
	case "metrics":
		return s.batchMetrics(metrics)
	case "events":
		return s.batchEvents(metrics)
	}
	return nil, fmt.Errorf("unexpected batch-format %q", s.BatchFormat)
}

func (s *Serializer) batchMetrics(metrics []telegraf.Metric) ([]byte, error) {
	// Determine the necessary information
	eventType := EventTypeBatch
	if s.EventType != "" {
		eventType = s.EventType
	}
	id, err := s.idgen.NewV1()
	if err != nil {
		return nil, fmt.Errorf("generating ID failed: %w", err)
	}

	// Serialize the metrics
	var earliest, latest time.Time
	data := make([]map[string]interface{}, 0, len(metrics))
	for _, m := range metrics {
		ts := m.Time()
		data = append(data, map[string]interface{}{
			"name":      m.Name(),
			"tags":      m.Tags(),
			"fields":    m.Fields(),
			"timestamp": ts.UnixNano(),
		})
		if ts.Before(earliest) {
			earliest = ts
		}
		if ts.After(latest) {
			latest = ts
		}
	}

	// Create the event that forms the envelop around the metric
	evt := cloudevents.NewEvent(s.Version)
	evt.SetSource(s.Source)
	evt.SetID(id.String())
	evt.SetType(eventType)
	if err := evt.SetData(cloudevents.ApplicationJSON, data); err != nil {
		return nil, fmt.Errorf("setting data failed: %w", err)
	}
	switch s.EventTime {
	case "creation":
		evt.SetTime(time.Now())
	case "earliest":
		evt.SetTime(earliest)
	case "latest":
		evt.SetTime(latest)
	}

	return json.Marshal(evt)
}

func (s *Serializer) batchEvents(metrics []telegraf.Metric) ([]byte, error) {
	events := make([]*cloudevents.Event, 0, len(metrics))
	for _, m := range metrics {
		e, err := s.createEvent(m)
		if err != nil {
			s.Log.Errorf("Creating event for %v failed: %v", m, err)
			continue
		}
		events = append(events, e)
	}
	return json.Marshal(events)
}

func (s *Serializer) createEvent(m telegraf.Metric) (*cloudevents.Event, error) {
	// Determine the necessary information
	source := s.Source
	if s.SourceTag != "" {
		if v, ok := m.GetTag(s.SourceTag); ok {
			source = v
		}
	}
	eventType := EventTypeSingle
	if s.EventType != "" {
		eventType = s.EventType
	}
	id, err := s.idgen.NewV1()
	if err != nil {
		return nil, fmt.Errorf("generating ID failed: %w", err)
	}

	// Serialize the metric
	data := map[string]interface{}{
		"name":      m.Name(),
		"tags":      m.Tags(),
		"fields":    m.Fields(),
		"timestamp": m.Time().UnixNano(),
	}

	// Create the event that forms the envelop around the metric
	evt := cloudevents.NewEvent(s.Version)
	evt.SetSource(source)
	evt.SetID(id.String())
	evt.SetType(eventType)
	if err := evt.SetData(cloudevents.ApplicationJSON, data); err != nil {
		return nil, fmt.Errorf("setting data failed: %w", err)
	}
	switch s.EventTime {
	case "creation":
		evt.SetTime(time.Now())
	case "earliest", "latest":
		evt.SetTime(m.Time())
	}

	return &evt, nil
}

func init() {
	serializers.Add("cloudevents",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
