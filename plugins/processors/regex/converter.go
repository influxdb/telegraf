package regex

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
)

type converterType int

const (
	convertTags = iota
	convertFields
	convertTagRename
	convertFieldRename
	convertMetricRename
)

func (ct converterType) String() string {
	switch ct {
	case convertTags:
		return "tags"
	case convertFields:
		return "fields"
	case convertTagRename:
		return "tag_rename"
	case convertFieldRename:
		return "field_rename"
	case convertMetricRename:
		return "metric_rename"
	}
	return fmt.Sprintf("unknown %d", int(ct))
}

type converter struct {
	Key         string `toml:"key"`
	Pattern     string `toml:"pattern"`
	Replacement string `toml:"replacement"`
	ResultKey   string `toml:"result_key"`
	Append      bool   `toml:"append"`

	filter filter.Filter
	re     *regexp.Regexp
	groups []string
	apply  func(m telegraf.Metric)
}

func (c *converter) setup(ct converterType, log telegraf.Logger) error {
	// Compile the pattern
	re, err := regexp.Compile(c.Pattern)
	if err != nil {
		return err
	}
	c.re = re

	switch ct {
	case convertTags, convertFields:
		if c.Key == "" {
			return errors.New("key required")
		}
		f, err := filter.Compile([]string{c.Key})
		if err != nil {
			return err
		}
		c.filter = f

		// Check for named groups
		if c.ResultKey == "" && c.Replacement == "" {
			groups := c.re.SubexpNames()
			allNamed := len(groups) > 1
			for _, g := range groups[1:] {
				if g == "" {
					allNamed = false
					break
				}
			}
			if allNamed {
				log.Debugf("%s: Using named-group mode...", ct)
				c.groups = groups[1:]
			} else {
				msg := "Neither 'result_key' nor 'replacement' given with unnamed or mixed groups;"
				msg += " using explicit, empty replacement!"
				log.Warnf("%s: %s", ct, msg)
			}
		} else {
			log.Debugf("%s: Using explicit mode...", ct)
		}
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
	for _, tag := range m.TagList() {
		if !c.filter.Match(tag.Key) || !c.re.MatchString(tag.Value) {
			continue
		}

		// Handle named groups
		if len(c.groups) > 0 {
			matches := c.re.FindStringSubmatch(tag.Value)
			for i, match := range matches[1:] {
				if match == "" {
					continue
				}
				name := c.groups[i]
				if c.Append {
					if v, ok := m.GetTag(name); ok {
						match = v + match
					}
				}
				m.AddTag(name, match)
			}
			continue
		}

		// Handle explicit replacements
		newKey := tag.Key
		if c.ResultKey != "" {
			newKey = c.ResultKey
		}

		newValue := c.re.ReplaceAllString(tag.Value, c.Replacement)
		if c.Append {
			if v, ok := m.GetTag(newKey); ok {
				newValue = v + newValue
			}
		}
		m.AddTag(newKey, newValue)
	}
}

func (c *converter) applyFields(m telegraf.Metric) {
	for _, field := range m.FieldList() {
		if !c.filter.Match(field.Key) {
			continue
		}

		value, ok := field.Value.(string)
		if !ok || !c.re.MatchString(value) {
			continue
		}

		// Handle named groups
		if len(c.groups) > 0 {
			matches := c.re.FindStringSubmatch(value)
			for i, match := range matches[1:] {
				if match == "" {
					continue
				}
				name := c.groups[i]
				if c.Append {
					if v, ok := m.GetTag(name); ok {
						match = v + match
					}
				}
				m.AddField(name, match)
			}
			continue
		}

		// Handle explicit replacements
		newKey := field.Key
		if c.ResultKey != "" {
			newKey = c.ResultKey
		}

		newValue := c.re.ReplaceAllString(value, c.Replacement)
		m.AddField(newKey, newValue)
	}
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
