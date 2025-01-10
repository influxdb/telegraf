package graphite

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/plugins/serializers"
)

const DefaultTemplate = "host.tags.measurement.field"

var (
	compatibleAllowedCharsName  = regexp.MustCompile(`[^ "-:\<>-\]_a-~\p{L}]`) //nolint:gocritic  // valid range for use-case
	compatibleAllowedCharsValue = regexp.MustCompile(`[^ -:<-~\p{L}]`)         //nolint:gocritic  // valid range for use-case
	compatibleLeadingTildeDrop  = regexp.MustCompile(`^[~]*(.*)`)
	hyphenChars                 = strings.NewReplacer(
		"/", "-",
		"@", "-",
		"*", "-",
	)
	dropChars = strings.NewReplacer(
		`\`, "",
		"..", ".",
	)

	fieldDeleter = strings.NewReplacer(".FIELDNAME", "", "FIELDNAME.", "")
)

type GraphiteTemplate struct {
	Filter filter.Filter
	Value  string
}

type GraphiteSerializer struct {
	Prefix          string   `toml:"prefix"`
	Template        string   `toml:"template"`
	StrictRegex     string   `toml:"graphite_strict_sanitize_regex"`
	TagSupport      bool     `toml:"graphite_tag_support"`
	TagSanitizeMode string   `toml:"graphite_tag_sanitize_mode"`
	Separator       string   `toml:"graphite_separator"`
	Templates       []string `toml:"templates"`

	tmplts             []*GraphiteTemplate
	strictAllowedChars *regexp.Regexp
}

func (s *GraphiteSerializer) Init() error {
	graphiteTemplates, defaultTemplate, err := InitGraphiteTemplates(s.Templates)
	if err != nil {
		return err
	}
	s.tmplts = graphiteTemplates

	if defaultTemplate != "" {
		s.Template = defaultTemplate
	}

	if s.TagSanitizeMode == "" {
		s.TagSanitizeMode = "strict"
	}

	if s.Separator == "" {
		s.Separator = "."
	}

	if s.StrictRegex == "" {
		s.strictAllowedChars = regexp.MustCompile(`[^a-zA-Z0-9-:._=\p{L}]`)
	} else {
		var err error
		s.strictAllowedChars, err = regexp.Compile(s.StrictRegex)
		if err != nil {
			return fmt.Errorf("invalid regex provided %q: %w", s.StrictRegex, err)
		}
	}

	return nil
}

func (s *GraphiteSerializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	var out []byte

	// Convert UnixNano to Unix timestamps
	timestamp := metric.Time().UnixNano() / 1000000000

	switch s.TagSupport {
	case true:
		for fieldName, value := range metric.Fields() {
			fieldValue := formatValue(value)
			if fieldValue == "" {
				continue
			}
			bucket := s.SerializeBucketNameWithTags(metric.Name(), metric.Tags(), s.Prefix, s.Separator, fieldName, s.TagSanitizeMode)
			metricString := fmt.Sprintf("%s %s %d\n",
				// insert "field" section of template
				bucket,
				// bucket,
				fieldValue,
				timestamp)
			point := []byte(metricString)
			out = append(out, point...)
		}
	default:
		template := s.Template
		for _, graphiteTemplate := range s.tmplts {
			if graphiteTemplate.Filter.Match(metric.Name()) {
				template = graphiteTemplate.Value
				break
			}
		}

		bucket := SerializeBucketName(metric.Name(), metric.Tags(), template, s.Prefix)
		if bucket == "" {
			return out, nil
		}

		for fieldName, value := range metric.Fields() {
			fieldValue := formatValue(value)
			if fieldValue == "" {
				continue
			}
			metricString := fmt.Sprintf("%s %s %d\n",
				// insert "field" section of template
				s.strictSanitize(InsertField(bucket, fieldName)),
				fieldValue,
				timestamp)
			point := []byte(metricString)
			out = append(out, point...)
		}
	}
	return out, nil
}

func (s *GraphiteSerializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	var batch bytes.Buffer
	for _, m := range metrics {
		buf, err := s.Serialize(m)
		if err != nil {
			return nil, err
		}
		batch.Write(buf)
	}
	return batch.Bytes(), nil
}

func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return ""
	case bool:
		if v {
			return "1"
		}
		return "0"
	case uint64:
		return strconv.FormatUint(v, 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if math.IsNaN(v) {
			return ""
		}

		if math.IsInf(v, 0) {
			return ""
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}

	return ""
}

// SerializeBucketName will take the given measurement name and tags and
// produce a graphite bucket. It will use the GraphiteSerializer.Template
// to generate this, or DefaultTemplate.
//
// NOTE: SerializeBucketName replaces the "field" portion of the template with
// FIELDNAME. It is up to the user to replace this. This is so that
// SerializeBucketName can be called just once per measurement, rather than
// once per field. See GraphiteSerializer.InsertField() function.
func SerializeBucketName(measurement string, tags map[string]string, template, prefix string) string {
	if template == "" {
		template = DefaultTemplate
	}
	tagsCopy := make(map[string]string)
	for k, v := range tags {
		tagsCopy[k] = v
	}

	var out []string
	templateParts := strings.Split(template, ".")
	for _, templatePart := range templateParts {
		switch templatePart {
		case "measurement":
			out = append(out, measurement)
		case "tags":
			// we will replace this later
			out = append(out, "TAGS")
		case "field":
			// user of SerializeBucketName needs to replace this
			out = append(out, "FIELDNAME")
		default:
			// This is a tag being applied
			if tagvalue, ok := tagsCopy[templatePart]; ok {
				out = append(out, strings.ReplaceAll(tagvalue, ".", "_"))
				delete(tagsCopy, templatePart)
			}
		}
	}

	// insert remaining tags into output name
	for i, templatePart := range out {
		if templatePart == "TAGS" {
			out[i] = buildTags(tagsCopy)
			break
		}
	}

	if len(out) == 0 {
		return ""
	}

	if prefix == "" {
		return strings.Join(out, ".")
	}
	return prefix + "." + strings.Join(out, ".")
}

func InitGraphiteTemplates(templates []string) ([]*GraphiteTemplate, string, error) {
	defaultTemplate := ""
	graphiteTemplates := make([]*GraphiteTemplate, 0, len(templates))
	for i, t := range templates {
		parts := strings.Fields(t)

		if len(parts) == 0 {
			return nil, "", fmt.Errorf("missing template at position: %d", i)
		}
		if len(parts) == 1 {
			if parts[0] == "" {
				return nil, "", fmt.Errorf("missing template at position: %d", i)
			}

			// Override default template
			defaultTemplate = t
			continue
		}

		if len(parts) > 2 {
			return nil, "", fmt.Errorf("invalid template format: %q", t)
		}

		tFilter, err := filter.Compile([]string{parts[0]})

		if err != nil {
			return nil, "", err
		}

		graphiteTemplates = append(graphiteTemplates, &GraphiteTemplate{
			Filter: tFilter,
			Value:  parts[1],
		})
	}

	return graphiteTemplates, defaultTemplate, nil
}

// SerializeBucketNameWithTags will take the given measurement name and tags and
// produce a graphite bucket. It will use the Graphite11Serializer.
// http://graphite.readthedocs.io/en/latest/tags.html
func (s *GraphiteSerializer) SerializeBucketNameWithTags(measurement string, tags map[string]string, prefix, separator, field, tagSanitizeMode string) string {
	var out string
	var tagsCopy []string
	for k, v := range tags {
		if k == "name" {
			k = "_name"
		}
		if tagSanitizeMode == "compatible" {
			tagsCopy = append(tagsCopy, compatibleSanitize(k, v))
		} else {
			tagsCopy = append(tagsCopy, s.strictSanitize(k+"="+v))
		}
	}
	sort.Strings(tagsCopy)

	if prefix != "" {
		out = prefix + separator
	}

	out += measurement

	if field != "value" {
		out += separator + field
	}

	out = s.strictSanitize(out)

	if len(tagsCopy) > 0 {
		out += ";" + strings.Join(tagsCopy, ";")
	}

	return out
}

// InsertField takes the bucket string from SerializeBucketName and replaces the
// FIELDNAME portion. If fieldName == "value", it will simply delete the
// FIELDNAME portion.
func InsertField(bucket, fieldName string) string {
	// if the field name is "value", then dont use it
	if fieldName == "value" {
		return fieldDeleter.Replace(bucket)
	}
	return strings.Replace(bucket, "FIELDNAME", fieldName, 1)
}

func buildTags(tags map[string]string) string {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var tagStr string
	for i, k := range keys {
		tagValue := strings.ReplaceAll(tags[k], ".", "_")
		if i == 0 {
			tagStr += tagValue
		} else {
			tagStr += "." + tagValue
		}
	}
	return tagStr
}

func (s *GraphiteSerializer) strictSanitize(value string) string {
	// Apply special hyphenation rules to preserve backwards compatibility
	value = hyphenChars.Replace(value)
	// Apply rule to drop some chars to preserve backwards compatibility
	value = dropChars.Replace(value)
	// Replace any remaining illegal chars
	return s.strictAllowedChars.ReplaceAllLiteralString(value, "_")
}

func compatibleSanitize(name, value string) string {
	name = compatibleAllowedCharsName.ReplaceAllLiteralString(name, "_")
	value = compatibleAllowedCharsValue.ReplaceAllLiteralString(value, "_")
	value = compatibleLeadingTildeDrop.FindStringSubmatch(value)[1]
	return name + "=" + value
}

func init() {
	serializers.Add("graphite",
		func() telegraf.Serializer {
			return &GraphiteSerializer{}
		},
	)
}
