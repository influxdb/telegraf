package serializers

import (
	"fmt"
	"time"

	"github.com/influxdata/telegraf"

	"github.com/influxdata/telegraf/plugins/serializers/graphite"
	"github.com/influxdata/telegraf/plugins/serializers/influx"
	"github.com/influxdata/telegraf/plugins/serializers/json"
)

// SerializerOutput is an interface for output plugins that are able to
// serialize telegraf metrics into arbitrary data formats.
type SerializerOutput interface {
	// SetSerializer sets the serializer function for the interface.
	SetSerializer(serializer Serializer)
}

// Serializer is an interface defining functions that a serializer plugin must
// satisfy.
type Serializer interface {
	// Serialize takes a single telegraf metric and turns it into a byte buffer.
	// separate metrics should be separated by a newline, and there should be
	// a newline at the end of the buffer.
	Serialize(metric telegraf.Metric) ([]byte, error)
}

// Config is a struct that covers the data types needed for all serializer types,
// and can be used to instantiate _any_ of the serializers.
type Config struct {
	// Dataformat can be one of: influx, graphite, or json
	DataFormat string

	// Maximum line length in bytes; influx format only
	InfluxMaxLineBytes int

	// Sort field keys, set to true only when debugging as it less performant
	// than unsorted fields; influx format only
	InfluxSortFields bool

	// Prefix to add to all measurements, only supports Graphite
	Prefix string

	// Template for converting telegraf metrics into Graphite
	// only supports Graphite
	Template string

	// Timestamp units to use for JSON formatted output
	TimestampUnits time.Duration
}

// NewSerializer a Serializer interface based on the given config.
func NewSerializer(config *Config) (Serializer, error) {
	var err error
	var serializer Serializer
	switch config.DataFormat {
	case "influx":
		serializer, err = NewInfluxSerializerConfig(config)
	case "graphite":
		serializer, err = NewGraphiteSerializer(config.Prefix, config.Template)
	case "json":
		serializer, err = NewJsonSerializer(config.TimestampUnits)
	default:
		err = fmt.Errorf("Invalid data format: %s", config.DataFormat)
	}
	return serializer, err
}

func NewJsonSerializer(timestampUnits time.Duration) (Serializer, error) {
	return &json.JsonSerializer{TimestampUnits: timestampUnits}, nil
}

func NewInfluxSerializerConfig(config *Config) (Serializer, error) {
	var sort influx.FieldSortOrder
	if config.InfluxSortFields {
		sort = influx.SortFields
	}
	s := influx.NewSerializer()
	s.SetMaxLineBytes(config.InfluxMaxLineBytes)
	s.SetFieldSortOrder(sort)
	return s, nil
}

func NewInfluxSerializer() (Serializer, error) {
	return influx.NewSerializer(), nil
}

func NewGraphiteSerializer(prefix, template string) (Serializer, error) {
	return &graphite.GraphiteSerializer{
		Prefix:   prefix,
		Template: template,
	}, nil
}
