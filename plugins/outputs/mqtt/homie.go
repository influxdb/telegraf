package mqtt

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
)

var idRe = regexp.MustCompile(`([^a-z0-9]+)`)

func (m *MQTT) collectHomieDeviceMessages(topic string, metric telegraf.Metric) ([]message, string, error) {
	var messages []message

	// Check if the device-id is already registered
	if _, found := m.homieSeen[topic]; !found {
		deviceName, err := m.homieDeviceNameGenerator.Generate(metric)
		if err != nil {
			return nil, "", fmt.Errorf("generating device name failed: %w", err)
		}
		messages = append(messages,
			message{topic + "/$homie", []byte("4.0")},
			message{topic + "/$name", []byte(deviceName)},
			message{topic + "/$state", []byte("ready")},
		)
		m.homieSeen[topic] = make(map[string]bool)
	}

	// Generate the node-ID from the metric and fixup invalid characters
	nodeName, err := m.homieNodeIDGenerator.Generate(metric)
	if err != nil {
		return nil, "", fmt.Errorf("generating device ID failed: %w", err)
	}
	nodeID := normalizeID(nodeName)

	if !m.homieSeen[topic][nodeID] {
		m.homieSeen[topic][nodeID] = true
		nodeIDs := make([]string, 0, len(m.homieSeen[topic]))
		for id := range m.homieSeen[topic] {
			nodeIDs = append(nodeIDs, id)
		}
		sort.Strings(nodeIDs)
		messages = append(messages,
			message{topic + "/$nodes", []byte(strings.Join(nodeIDs, ","))},
			message{topic + "/" + nodeID + "/$name", []byte(nodeName)},
		)
	}

	properties := make([]string, 0, len(metric.TagList())+len(metric.FieldList()))
	for _, tag := range metric.TagList() {
		properties = append(properties, normalizeID(tag.Key))
	}
	for _, field := range metric.FieldList() {
		properties = append(properties, normalizeID(field.Key))
	}
	sort.Strings(properties)

	messages = append(messages, message{
		topic + "/" + nodeID + "/$properties",
		[]byte(strings.Join(properties, ",")),
	})

	return messages, nodeID, nil
}

func normalizeID(raw string) string {
	// IDs in Home can only contain lowercase letters and hyphens
	// see https://homieiot.github.io/specification/#topic-ids
	id := strings.ToLower(raw)
	id = idRe.ReplaceAllString(id, "-")
	return strings.Trim(id, "-")
}

func convertType(value interface{}) (val, dtype string, err error) {
	v, err := internal.ToString(value)
	if err != nil {
		return "", "", err
	}

	switch value.(type) {
	case int8, int16, int32, int64, uint8, uint16, uint32, uint64:
		return v, "integer", nil
	case float32, float64:
		return v, "float", nil
	case []byte, string, fmt.Stringer:
		return v, "string", nil
	case bool:
		return v, "boolean", nil
	}
	return "", "", fmt.Errorf("unknown type %T", value)
}

type HomieGenerator struct {
	PluginName string
	metric     telegraf.Metric
	template   *template.Template
}

func NewHomieGenerator(tmpl string) (*HomieGenerator, error) {
	tt, err := template.New("topic_name").Funcs(sprig.TxtFuncMap()).Parse(tmpl)
	if err != nil {
		return nil, err
	}

	return &HomieGenerator{template: tt}, nil
}

func (t *HomieGenerator) Tag(key string) string {
	tagString, _ := t.metric.GetTag(key)
	return tagString
}

func (t *HomieGenerator) Generate(m telegraf.Metric) (string, error) {
	t.PluginName = m.Name()
	t.metric = m

	var b strings.Builder
	if err := t.template.Execute(&b, t); err != nil {
		return "", err
	}

	result := b.String()
	if strings.Contains(result, "/") {
		return "", errors.New("cannot contain /")
	}

	return result, nil
}
