package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/blues/jsonata-go"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/plugins/serializers"
)

type Serializer struct {
	TimestampUnits      config.Duration `toml:"json_timestamp_units"`
	TimestampFormat     string          `toml:"json_timestamp_format"`
	Transformation      string          `toml:"json_transformation"`
	NestedFieldsInclude []string        `toml:"json_nested_fields_include"`
	NestedFieldsExclude []string        `toml:"json_nested_fields_exclude"`

	nestedfields filter.Filter
}

func (s *Serializer) Init() error {
	// Default precision is 1s
	if s.TimestampUnits <= 0 {
		s.TimestampUnits = config.Duration(time.Second)
	}

	// Search for the power of ten less than the duration
	d := time.Nanosecond
	t := time.Duration(s.TimestampUnits)
	for {
		if d*10 > t {
			t = d
			break
		}
		d = d * 10
	}
	s.TimestampUnits = config.Duration(t)

	if len(s.NestedFieldsInclude) > 0 || len(s.NestedFieldsExclude) > 0 {
		f, err := filter.NewIncludeExcludeFilter(s.NestedFieldsInclude, s.NestedFieldsExclude)
		if err != nil {
			return err
		}
		s.nestedfields = f
	}

	return nil
}

func (s *Serializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	var obj interface{}
	obj = s.createObject(metric)

	if s.Transformation != "" {
		var err error
		if obj, err = s.transform(obj); err != nil {
			if errors.Is(err, jsonata.ErrUndefined) {
				return nil, fmt.Errorf("%w (maybe configured for batch mode?)", err)
			}
			return nil, err
		}
	}

	serialized, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	serialized = append(serialized, '\n')

	return serialized, nil
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	objects := make([]interface{}, 0, len(metrics))
	for _, metric := range metrics {
		m := s.createObject(metric)
		objects = append(objects, m)
	}

	var obj interface{}
	obj = map[string]interface{}{
		"metrics": objects,
	}

	if s.Transformation != "" {
		var err error
		if obj, err = s.transform(obj); err != nil {
			if errors.Is(err, jsonata.ErrUndefined) {
				return nil, fmt.Errorf("%w (maybe configured for non-batch mode?)", err)
			}
			return nil, err
		}
	}

	serialized, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	serialized = append(serialized, '\n')

	return serialized, nil
}

func (s *Serializer) createObject(metric telegraf.Metric) map[string]interface{} {
	m := make(map[string]interface{}, 4)

	tags := make(map[string]string, len(metric.TagList()))
	for _, tag := range metric.TagList() {
		tags[tag.Key] = tag.Value
	}
	m["tags"] = tags

	fields := make(map[string]interface{}, len(metric.FieldList()))
	for _, field := range metric.FieldList() {
		val := field.Value
		switch fv := field.Value.(type) {
		case float64:
			// JSON does not support these special values
			if math.IsNaN(fv) || math.IsInf(fv, 0) {
				continue
			}
		case string:
			// Check for nested fields if any
			if s.nestedfields != nil && s.nestedfields.Match(field.Key) {
				bv := []byte(fv)
				if json.Valid(bv) {
					var nested interface{}
					if err := json.Unmarshal(bv, &nested); err == nil {
						val = nested
					}
				}
			}
		}
		fields[field.Key] = val
	}
	m["fields"] = fields

	m["name"] = metric.Name()
	if s.TimestampFormat == "" {
		m["timestamp"] = metric.Time().UnixNano() / int64(s.TimestampUnits)
	} else {
		m["timestamp"] = metric.Time().UTC().Format(s.TimestampFormat)
	}
	return m
}

func (s *Serializer) transform(obj interface{}) (interface{}, error) {
	transformation, err := jsonata.Compile(s.Transformation)
	if err != nil {
		return nil, err
	}

	return transformation.Eval(obj)
}

func init() {
	serializers.Add("json",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
