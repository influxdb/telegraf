package mqtt_consumer

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
)

type topicParsingConfig struct {
	Topic       string            `toml:"topic"`
	Measurement string            `toml:"measurement"`
	Tags        string            `toml:"tags"`
	Fields      string            `toml:"fields"`
	FieldTypes  map[string]string `toml:"types"`
}

type topicParser struct {
	topicIndices   map[string]int
	topicVarLength bool
	topicMinLength int

	extractMeasurement bool
	measurementIndex   int
	tagIndices         map[string]int
	fieldIndices       map[string]int
	fieldTypes         map[string]string
}

func (cfg *topicParsingConfig) newParser() (*topicParser, error) {
	p := &topicParser{
		fieldTypes: cfg.FieldTypes,
	}

	// Build a check list for topic elements
	var topicMinLength int
	var topicInvert bool
	topicParts := strings.Split(cfg.Topic, "/")
	p.topicIndices = make(map[string]int, len(topicParts))
	for i, k := range topicParts {
		switch k {
		case "+":
			topicMinLength++
		case "#":
			if p.topicVarLength {
				return nil, errors.New("topic can only contain one hash")
			}
			p.topicVarLength = true
			topicInvert = true
		default:
			if !topicInvert {
				p.topicIndices[k] = i
			} else {
				p.topicIndices[k] = i - len(topicParts)
			}
			topicMinLength++
		}
	}

	// Determine metric name selection
	var measurementMinLength int
	var measurementInvert bool
	measurementParts := strings.Split(cfg.Measurement, "/")
	for i, k := range measurementParts {
		if k == "_" || k == "" {
			measurementMinLength++
			continue
		}

		if k == "#" {
			measurementInvert = true
			continue
		}

		if p.extractMeasurement {
			return nil, errors.New("measurement can only contain one element")
		}

		if !measurementInvert {
			p.measurementIndex = i
		} else {
			p.measurementIndex = i - len(measurementParts)
		}
		p.extractMeasurement = true
		measurementMinLength++
	}

	// Determine tag selections
	var tagMinLength int
	var tagInvert bool
	tagParts := strings.Split(cfg.Tags, "/")
	p.tagIndices = make(map[string]int, len(tagParts))
	for i, k := range tagParts {
		if k == "_" || k == "" {
			tagMinLength++
			continue
		}
		if k == "#" {
			tagInvert = true
			continue
		}
		if !tagInvert {
			p.tagIndices[k] = i
		} else {
			p.tagIndices[k] = i - len(tagParts)
		}
		tagMinLength++
	}

	// Determine tag selections
	var fieldMinLength int
	var fieldInvert bool
	fieldParts := strings.Split(cfg.Fields, "/")
	p.fieldIndices = make(map[string]int, len(fieldParts))
	for i, k := range fieldParts {
		if k == "_" || k == "" {
			fieldMinLength++
			continue
		}
		if k == "#" {
			fieldInvert = true
			continue
		}
		if !fieldInvert {
			p.fieldIndices[k] = i
		} else {
			p.fieldIndices[k] = i - len(fieldParts)
		}
		fieldMinLength++
	}

	if !p.topicVarLength {
		if measurementMinLength != topicMinLength && p.extractMeasurement {
			return nil, errors.New("measurement length does not equal topic length")
		}

		if fieldMinLength != topicMinLength && cfg.Fields != "" {
			return nil, errors.New("fields length does not equal topic length")
		}

		if tagMinLength != topicMinLength && cfg.Tags != "" {
			return nil, errors.New("tags length does not equal topic length")
		}
	}

	p.topicMinLength = max(topicMinLength, measurementMinLength, tagMinLength, fieldMinLength)

	return p, nil
}

func (p *topicParser) parse(metric telegraf.Metric, topic string) error {
	// Split the actual topic into its elements and check for a match
	topicParts := strings.Split(topic, "/")
	if p.topicVarLength && len(topicParts) < p.topicMinLength || !p.topicVarLength && len(topicParts) != p.topicMinLength {
		return nil
	}
	for expected, i := range p.topicIndices {
		if i >= 0 && topicParts[i] != expected || i < 0 && topicParts[len(topicParts)+i] != expected {
			return nil
		}
	}

	// Extract the measurement name
	var measurement string
	if p.extractMeasurement {
		if p.measurementIndex >= 0 {
			measurement = topicParts[p.measurementIndex]
		} else {
			measurement = topicParts[len(topicParts)+p.measurementIndex]
		}
		metric.SetName(measurement)
	}

	// Extract the tags
	for k, i := range p.tagIndices {
		if i >= 0 {
			metric.AddTag(k, topicParts[i])
		} else {
			metric.AddTag(k, topicParts[len(topicParts)+i])
		}
	}

	// Extract the fields
	for k, i := range p.fieldIndices {
		var raw string
		if i >= 0 {
			raw = topicParts[i]
		} else {
			raw = topicParts[len(topicParts)+i]
		}
		v, err := p.convertToFieldType(raw, k)
		if err != nil {
			return err
		}
		metric.AddField(k, v)
	}

	return nil
}

func (p *topicParser) convertToFieldType(value, key string) (interface{}, error) {
	// If the user configured inputs.mqtt_consumer.topic.types, check for the desired type
	desiredType, ok := p.fieldTypes[key]
	if !ok {
		return value, nil
	}

	var v interface{}
	var err error
	switch desiredType {
	case "uint":
		if v, err = strconv.ParseUint(value, 10, 64); err != nil {
			return nil, fmt.Errorf("unable to convert field %q to type uint: %w", value, err)
		}
	case "int":
		if v, err = strconv.ParseInt(value, 10, 64); err != nil {
			return nil, fmt.Errorf("unable to convert field %q to type int: %w", value, err)
		}
	case "float":
		if v, err = strconv.ParseFloat(value, 64); err != nil {
			return nil, fmt.Errorf("unable to convert field %q to type float: %w", value, err)
		}
	default:
		return nil, fmt.Errorf("converting to the type %s is not supported: use int, uint, or float", desiredType)
	}

	return v, nil
}
