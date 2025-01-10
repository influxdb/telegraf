package binary

import (
	"encoding/binary"
	"fmt"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/serializers"
)

type Serializer struct {
	Entries    []*Entry `toml:"entries"`
	Endianness string   `toml:"endianness"`

	converter binary.ByteOrder
}

func (s *Serializer) Init() error {
	switch s.Endianness {
	case "big":
		s.converter = binary.BigEndian
	case "little":
		s.converter = binary.LittleEndian
	case "", "host":
		s.Endianness = "host"
		s.converter = internal.HostEndianness
	default:
		return fmt.Errorf("invalid endianness %q", s.Endianness)
	}

	for i, entry := range s.Entries {
		if err := entry.fillDefaults(); err != nil {
			return fmt.Errorf("entry %d check failed: %w", i, err)
		}
	}

	return nil
}

func (s *Serializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	serialized := make([]byte, 0)

	for _, entry := range s.Entries {
		switch entry.ReadFrom {
		case "field":
			field, found := metric.GetField(entry.Name)
			if !found {
				return nil, fmt.Errorf("field %s not found", entry.Name)
			}

			entryBytes, err := entry.serializeValue(field, s.converter)
			if err != nil {
				return nil, err
			}
			serialized = append(serialized, entryBytes...)
		case "tag":
			tag, found := metric.GetTag(entry.Name)
			if !found {
				return nil, fmt.Errorf("tag %s not found", entry.Name)
			}

			entryBytes, err := entry.serializeValue(tag, s.converter)
			if err != nil {
				return nil, err
			}
			serialized = append(serialized, entryBytes...)
		case "time":
			entryBytes, err := entry.serializeValue(metric.Time(), s.converter)
			if err != nil {
				return nil, err
			}
			serialized = append(serialized, entryBytes...)
		case "name":
			entryBytes, err := entry.serializeValue(metric.Name(), s.converter)
			if err != nil {
				return nil, err
			}
			serialized = append(serialized, entryBytes...)
		}
	}

	return serialized, nil
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	serialized := make([]byte, 0)

	for _, metric := range metrics {
		m, err := s.Serialize(metric)

		if err != nil {
			return nil, err
		}

		serialized = append(serialized, m...)
	}

	return serialized, nil
}

func init() {
	serializers.Add("binary",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
