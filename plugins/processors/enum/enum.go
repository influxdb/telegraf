//go:generate ../../../tools/readme_config_includer/generator
package enum

import (
	_ "embed"
	"fmt"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/plugins/processors"
)

//go:embed sample.conf
var sampleConfig string

type EnumMapper struct {
	Mappings []Mapping `toml:"mapping"`

	FieldFilter filter.Filter
	TagFilter   filter.Filter
}

type Mapping struct {
	Tag     string      `toml:"tag"`
	Field   string      `toml:"field" deprecated:"1.33.0;1.40.0;use 'fields' instead"`
	Fields  []string    `toml:"fields"`
	Dest    string      `toml:"dest"`
	Default interface{} `toml:"default"`

	ValueMappings map[string]interface{}
}

func (*EnumMapper) SampleConfig() string {
	return sampleConfig
}

func (mapper *EnumMapper) Init() error {
	for _, mapping := range mapper.Mappings {
		// Handle deprecated field option
		if mapping.Field != "" {
			mapping.Fields = append(mapping.Fields, mapping.Field)
		}
		if len(mapping.Fields) != 0 {
			fieldFilter, err := filter.Compile(mapping.Fields)
			if err != nil {
				return fmt.Errorf("failed to create new field filter: %w", err)
			}
			mapper.FieldFilter = fieldFilter
		}
		if mapping.Tag != "" {
			tagFilter, err := filter.Compile([]string{mapping.Tag})
			if err != nil {
				return fmt.Errorf("failed to create new tag filter: %w", err)
			}
			mapper.TagFilter = tagFilter
		}
	}

	return nil
}

func (mapper *EnumMapper) Apply(in ...telegraf.Metric) []telegraf.Metric {
	for i := 0; i < len(in); i++ {
		in[i] = mapper.applyMappings(in[i])
	}
	return in
}

func (mapper *EnumMapper) applyMappings(metric telegraf.Metric) telegraf.Metric {
	newFields := make(map[string]interface{})
	newTags := make(map[string]string)

	for _, mapping := range mapper.Mappings {
		if len(mapping.Fields) > 0 {
			mapper.fieldMapping(metric, mapping, newFields)
		}
		if mapping.Tag != "" {
			mapper.tagMapping(metric, mapping, newTags)
		}
	}

	for k, v := range newFields {
		writeField(metric, k, v)
	}

	for k, v := range newTags {
		writeTag(metric, k, v)
	}

	return metric
}

func (mapper *EnumMapper) fieldMapping(metric telegraf.Metric, mapping Mapping, newFields map[string]interface{}) {
	fields := metric.FieldList()
	for _, f := range fields {
		if !mapper.FieldFilter.Match(f.Key) {
			continue
		}
		if adjustedValue, isString := adjustValue(f.Value).(string); isString {
			if mappedValue, isMappedValuePresent := mapping.mapValue(adjustedValue); isMappedValuePresent {
				newFields[mapping.getDestination(f.Key)] = mappedValue
			}
		}
	}
}

func (mapper *EnumMapper) tagMapping(metric telegraf.Metric, mapping Mapping, newTags map[string]string) {
	tags := metric.TagList()
	for _, t := range tags {
		if !mapper.TagFilter.Match(t.Key) {
			continue
		}
		if mappedValue, isMappedValuePresent := mapping.mapValue(t.Value); isMappedValuePresent {
			switch val := mappedValue.(type) {
			case string:
				newTags[mapping.getDestination(t.Key)] = val
			default:
				newTags[mapping.getDestination(t.Key)] = fmt.Sprintf("%v", val)
			}
		}
	}
}

func adjustValue(in interface{}) interface{} {
	switch val := in.(type) {
	case bool:
		return strconv.FormatBool(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case uint64:
		return strconv.FormatUint(val, 10)
	default:
		return in
	}
}

func (mapping *Mapping) mapValue(original string) (interface{}, bool) {
	if mapped, found := mapping.ValueMappings[original]; found {
		return mapped, true
	}
	if mapping.Default != nil {
		return mapping.Default, true
	}
	return original, false
}

func (mapping *Mapping) getDestination(defaultDest string) string {
	if mapping.Dest != "" {
		return mapping.Dest
	}
	return defaultDest
}

func writeField(metric telegraf.Metric, name string, value interface{}) {
	metric.RemoveField(name)
	metric.AddField(name, value)
}

func writeTag(metric telegraf.Metric, name string, value string) {
	metric.RemoveTag(name)
	metric.AddTag(name, value)
}

func init() {
	processors.Add("enum", func() telegraf.Processor {
		return &EnumMapper{}
	})
}
