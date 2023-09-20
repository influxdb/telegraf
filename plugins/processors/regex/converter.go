package regex

import (
	"fmt"
	"regexp"

	"github.com/influxdata/telegraf"
)

func (c *converter) setup(ct converterType) error {
	switch ct {
	case convertTagRename, convertFieldRename:
		switch c.ResultKey {
		case "":
			c.ResultKey = "keep"
		case "overwrite", "keep":
			// Do nothing as those are valid choices
		default:
			return fmt.Errorf("invalid metrics result_key %q", c.ResultKey)
		}
	}

	// Compile the pattern
	re, err := regexp.Compile(c.Pattern)
	if err != nil {
		return err
	}
	c.re = re

	// Select the application function
	switch ct {
	case convertTags:
		c.apply = c.applyTags
	case convertFields:
		c.apply = c.applyFields
	case convertTagRename:
		c.apply = c.applyTagRename
	case convertFieldRename:
		c.apply = c.applyFieldRename
	case convertMetricRename:
		c.apply = c.applyMetricRename
	}

	return nil
}

func (c *converter) applyTags(m telegraf.Metric) {
	if c.Key == "*" {
		for _, tag := range m.TagList() {
			if c.re.MatchString(tag.Value) {
				newValue := c.re.ReplaceAllString(tag.Value, c.Replacement)
				if c.Append {
					if v, ok := m.GetTag(tag.Key); ok {
						newValue = v + newValue
					}
				}
				m.AddTag(tag.Key, newValue)
			}
		}
		return
	}

	newKey := c.Key
	if c.ResultKey != "" {
		newKey = c.ResultKey
	}

	value, ok := m.GetTag(c.Key)
	if !ok || !c.re.MatchString(value) {
		return
	}
	newValue := c.re.ReplaceAllString(value, c.Replacement)

	if c.Append {
		if v, ok := m.GetTag(newKey); ok {
			newValue = v + newValue
		}
	}
	m.AddTag(newKey, newValue)
}

func (c *converter) applyFields(m telegraf.Metric) {
	value, ok := m.GetField(c.Key)
	if !ok {
		return
	}

	v, ok := value.(string)
	if !ok || !c.re.MatchString(v) {
		return
	}

	newKey := c.Key
	if c.ResultKey != "" {
		newKey = c.ResultKey
	}

	newValue := c.re.ReplaceAllString(v, c.Replacement)
	m.AddField(newKey, newValue)
}

func (c *converter) applyTagRename(m telegraf.Metric) {
	replacements := make(map[string]string)
	for _, tag := range m.TagList() {
		name := tag.Key
		if c.re.MatchString(name) {
			newName := c.re.ReplaceAllString(name, c.Replacement)

			if !m.HasTag(newName) {
				// There is no colliding tag, we can just change the name.
				tag.Key = newName
				continue
			}

			if c.ResultKey == "overwrite" {
				// We got a colliding tag, remember the replacement and do it later
				replacements[name] = newName
			}
		}
	}
	// We needed to postpone the replacement as we cannot modify the tag-list
	// while iterating it as this will result in invalid memory dereference panic.
	for oldName, newName := range replacements {
		value, ok := m.GetTag(oldName)
		if !ok {
			// Just in case the tag got removed in the meantime
			continue
		}
		m.AddTag(newName, value)
		m.RemoveTag(oldName)
	}
}

func (c *converter) applyFieldRename(m telegraf.Metric) {
	replacements := make(map[string]string)
	for _, field := range m.FieldList() {
		name := field.Key
		if c.re.MatchString(name) {
			newName := c.re.ReplaceAllString(name, c.Replacement)

			if !m.HasField(newName) {
				// There is no colliding field, we can just change the name.
				field.Key = newName
				continue
			}

			if c.ResultKey == "overwrite" {
				// We got a colliding field, remember the replacement and do it later
				replacements[name] = newName
			}
		}
	}
	// We needed to postpone the replacement as we cannot modify the field-list
	// while iterating it as this will result in invalid memory dereference panic.
	for oldName, newName := range replacements {
		value, ok := m.GetField(oldName)
		if !ok {
			// Just in case the field got removed in the meantime
			continue
		}
		m.AddField(newName, value)
		m.RemoveField(oldName)
	}
}

func (c *converter) applyMetricRename(m telegraf.Metric) {
	value := m.Name()
	if c.re.MatchString(value) {
		newValue := c.re.ReplaceAllString(value, c.Replacement)
		m.SetName(newValue)
	}
}
