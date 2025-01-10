package carbon2

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/serializers"
)

const sanitizedChars = "!@#$%^&*()+`'\"[]{};<>,?/\\|="

type Serializer struct {
	Format              string          `toml:"carbon2_format"`
	SanitizeReplaceChar string          `toml:"carbon2_sanitize_replace_char"`
	Log                 telegraf.Logger `toml:"-"`

	sanitizeReplacer *strings.Replacer
	template         string
}

func (s *Serializer) Init() error {
	if s.SanitizeReplaceChar == "" {
		s.SanitizeReplaceChar = ":"
	}

	if len(s.SanitizeReplaceChar) > 1 {
		return errors.New("sanitize replace char has to be a singular character")
	}

	// Create replacer to replacing all characters requiring sanitization with the user-specified replacement
	pairs := make([]string, 0, 2*len(sanitizedChars))
	for _, c := range sanitizedChars {
		pairs = append(pairs, string(c), s.SanitizeReplaceChar)
	}
	s.sanitizeReplacer = strings.NewReplacer(pairs...)

	switch s.Format {
	case "", "field_separate":
		s.Format = "field_separate"
		s.template = "metric=%s field=%s "
	case "metric_includes_field":
		s.template = "metric=%s_%s "
	default:
		return fmt.Errorf("unknown carbon2 format: %s", s.Format)
	}

	return nil
}

func (s *Serializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	return s.createObject(metric), nil
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	var batch bytes.Buffer
	for _, metric := range metrics {
		batch.Write(s.createObject(metric))
	}
	return batch.Bytes(), nil
}

func (s *Serializer) createObject(metric telegraf.Metric) []byte {
	var m bytes.Buffer

	for fieldName, fieldValue := range metric.Fields() {
		if _, ok := fieldValue.(string); ok {
			continue
		}

		name := s.sanitizeReplacer.Replace(metric.Name())

		var value string
		if v, ok := fieldValue.(bool); ok {
			if v {
				value = "1"
			} else {
				value = "0"
			}
		} else {
			var err error
			value, err = internal.ToString(fieldValue)
			if err != nil {
				s.Log.Warnf("Cannot convert %v (%T) to string", fieldValue, fieldValue)
				continue
			}
		}

		m.WriteString(fmt.Sprintf(s.template, strings.ReplaceAll(name, " ", "_"), strings.ReplaceAll(fieldName, " ", "_")))
		for _, tag := range metric.TagList() {
			m.WriteString(strings.ReplaceAll(tag.Key, " ", "_"))
			m.WriteString("=")
			value := tag.Value
			if len(value) == 0 {
				value = "null"
			}
			m.WriteString(strings.ReplaceAll(value, " ", "_"))
			m.WriteString(" ")
		}
		m.WriteString(" ")
		m.WriteString(value)
		m.WriteString(" ")
		m.WriteString(strconv.FormatInt(metric.Time().Unix(), 10))
		m.WriteString("\n")
	}
	return m.Bytes()
}

func init() {
	serializers.Add("carbon2",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
