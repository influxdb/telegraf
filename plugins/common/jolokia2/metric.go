package jolokia2

import "strings"

// A MetricConfig represents a TOML form of
// a Metric with some optional fields.
type MetricConfig struct {
	Name           string
	Mbean          string
	Paths          []string
	FieldName      *string
	FieldPrefix    *string
	FieldSeparator *string
	TagPrefix      *string
	TagKeys        []string
}

// A Metric represents a specification for a
// Jolokia read request, and the transformations
// to apply to points generated from the responses.
type Metric struct {
	Name           string
	Mbean          string
	Paths          []string
	FieldName      string
	FieldPrefix    string
	FieldSeparator string
	TagPrefix      string
	TagKeys        []string

	mbeanDomain     string
	mbeanProperties []string
}

func NewMetric(config MetricConfig, defaultFieldPrefix, defaultFieldSeparator, defaultTagPrefix string) Metric {
	metric := Metric{
		Name:    config.Name,
		Mbean:   config.Mbean,
		Paths:   config.Paths,
		TagKeys: config.TagKeys,
	}

	if config.FieldName != nil {
		metric.FieldName = *config.FieldName
	}

	if config.FieldPrefix == nil {
		metric.FieldPrefix = defaultFieldPrefix
	} else {
		metric.FieldPrefix = *config.FieldPrefix
	}

	if config.FieldSeparator == nil {
		metric.FieldSeparator = defaultFieldSeparator
	} else {
		metric.FieldSeparator = *config.FieldSeparator
	}

	if config.TagPrefix == nil {
		metric.TagPrefix = defaultTagPrefix
	} else {
		metric.TagPrefix = *config.TagPrefix
	}

	mbeanDomain, mbeanProperties := parseMbeanObjectName(config.Mbean)
	metric.mbeanDomain = mbeanDomain
	metric.mbeanProperties = mbeanProperties

	return metric
}

func (m Metric) MatchObjectName(name string) bool {
	if name == m.Mbean {
		return true
	}

	mbeanDomain, mbeanProperties := parseMbeanObjectName(name)
	if mbeanDomain != m.mbeanDomain {
		return false
	}

	if len(mbeanProperties) != len(m.mbeanProperties) {
		return false
	}

NEXT_PROPERTY:
	for _, mbeanProperty := range m.mbeanProperties {
		for i := range mbeanProperties {
			if mbeanProperties[i] == mbeanProperty {
				continue NEXT_PROPERTY
			}
		}

		return false
	}

	return true
}

func (m Metric) MatchAttributeAndPath(attribute, innerPath string) bool {
	path := attribute
	if innerPath != "" {
		path = path + "/" + innerPath
	}

	for i := range m.Paths {
		if path == m.Paths[i] {
			return true
		}
	}

	return false
}

func parseMbeanObjectName(name string) (string, []string) {
	index := strings.Index(name, ":")
	if index == -1 {
		return name, nil
	}

	domain := name[:index]

	if index+1 > len(name) {
		return domain, nil
	}

	return domain, strings.Split(name[index+1:], ",")
}
