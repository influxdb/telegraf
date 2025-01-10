package templating

import (
	"fmt"
	"strings"
)

// Template represents a pattern and tags to map a metric string to an influxdb Point
type Template struct {
	separator         string
	parts             []string
	defaultTags       map[string]string
	greedyField       bool
	greedyMeasurement bool
}

// Apply extracts the template fields from the given line and returns the measurement
// name, tags and field name
//
//nolint:revive //function-result-limit conditionally 4 return results allowed
func (t *Template) Apply(line string, joiner string) (measurementName string, tags map[string]string, field string, err error) {
	allFields := strings.Split(line, t.separator)
	var (
		measurements []string
		tagsMap      = make(map[string][]string)
		fields       []string
	)

	// Set any default tags
	for k, v := range t.defaultTags {
		tagsMap[k] = append(tagsMap[k], v)
	}

	// See if an invalid combination has been specified in the template:
	for _, tag := range t.parts {
		if tag == "measurement*" {
			t.greedyMeasurement = true
		} else if tag == "field*" {
			t.greedyField = true
		}
	}
	if t.greedyField && t.greedyMeasurement {
		return "", nil, "",
			fmt.Errorf("either 'field*' or 'measurement*' can be used in each "+
				"template (but not both together): %q",
				strings.Join(t.parts, joiner))
	}

	for i, tag := range t.parts {
		if i >= len(allFields) {
			continue
		}
		if tag == "" {
			continue
		}

		switch tag {
		case "measurement":
			measurements = append(measurements, allFields[i])
		case "field":
			fields = append(fields, allFields[i])
		case "field*":
			fields = append(fields, allFields[i:]...)
		case "measurement*":
			measurements = append(measurements, allFields[i:]...)
		default:
			tagsMap[tag] = append(tagsMap[tag], allFields[i])
		}
	}

	// Convert to map of strings.
	tags = make(map[string]string)
	for k, values := range tagsMap {
		tags[k] = strings.Join(values, joiner)
	}

	return strings.Join(measurements, joiner), tags, strings.Join(fields, joiner), nil
}

func NewDefaultTemplateWithPattern(pattern string) (*Template, error) {
	return NewTemplate(DefaultSeparator, pattern, nil)
}

// NewTemplate returns a new template ensuring it has a measurement specified.
func NewTemplate(separator, pattern string, defaultTags map[string]string) (*Template, error) {
	parts := strings.Split(pattern, separator)
	hasMeasurement := false
	template := &Template{
		separator:   separator,
		parts:       parts,
		defaultTags: defaultTags,
	}

	for _, part := range parts {
		if strings.HasPrefix(part, "measurement") {
			hasMeasurement = true
		}
		if part == "measurement*" {
			template.greedyMeasurement = true
		} else if part == "field*" {
			template.greedyField = true
		}
	}

	if !hasMeasurement {
		return nil, fmt.Errorf("no measurement specified for template. %q", pattern)
	}

	return template, nil
}

// templateSpec is a template string split in its constituent parts
type templateSpec struct {
	separator string
	filter    string
	template  string
	tagstring string
}

// templateSpecs is simply an array of template specs implementing the sorting interface
type templateSpecs []templateSpec

// Less reports whether the element with
// index j should sort before the element with index k.
func (e templateSpecs) Less(j, k int) bool {
	jlen := len(e[j].filter)
	klen := len(e[k].filter)
	if jlen == 0 && klen != 0 {
		return true
	}
	if klen == 0 && jlen != 0 {
		return false
	}
	return strings.Count(e[j].template, e[j].separator) <
		strings.Count(e[k].template, e[k].separator)
}

// Swap swaps the elements with indexes i and j.
func (e templateSpecs) Swap(i, j int) { e[i], e[j] = e[j], e[i] }

// Len is the number of elements in the collection.
func (e templateSpecs) Len() int { return len(e) }
