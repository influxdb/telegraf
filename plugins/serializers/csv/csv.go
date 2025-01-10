package csv

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/serializers"
)

type Serializer struct {
	TimestampFormat string   `toml:"csv_timestamp_format"`
	Separator       string   `toml:"csv_separator"`
	Header          bool     `toml:"csv_header"`
	Prefix          bool     `toml:"csv_column_prefix"`
	Columns         []string `toml:"csv_columns"`

	buffer bytes.Buffer
	writer *csv.Writer
}

func (s *Serializer) Init() error {
	// Setting defaults
	if s.Separator == "" {
		s.Separator = ","
	}

	// Check inputs
	if len(s.Separator) > 1 {
		return fmt.Errorf("invalid separator %q", s.Separator)
	}
	switch s.TimestampFormat {
	case "":
		s.TimestampFormat = "unix"
	case "unix", "unix_ms", "unix_us", "unix_ns":
	default:
		if time.Now().Format(s.TimestampFormat) == s.TimestampFormat {
			return fmt.Errorf("invalid timestamp format %q", s.TimestampFormat)
		}
	}

	// Check columns if any
	for _, name := range s.Columns {
		switch {
		case name == "timestamp", name == "name",
			strings.HasPrefix(name, "tag."),
			strings.HasPrefix(name, "field."):
		default:
			return fmt.Errorf("invalid column reference %q", name)
		}
	}

	// Initialize the writer
	s.writer = csv.NewWriter(&s.buffer)
	s.writer.Comma, _ = utf8.DecodeRuneInString(s.Separator)
	s.writer.UseCRLF = runtime.GOOS == "windows"

	return nil
}

func (s *Serializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	return s.SerializeBatch([]telegraf.Metric{metric})
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	if len(metrics) < 1 {
		return nil, nil
	}

	// Clear the buffer
	s.buffer.Truncate(0)

	// Write the header if the user wants us to
	if s.Header {
		if len(s.Columns) > 0 {
			if err := s.writeHeaderOrdered(); err != nil {
				return nil, fmt.Errorf("writing header failed: %w", err)
			}
		} else {
			if err := s.writeHeader(metrics[0]); err != nil {
				return nil, fmt.Errorf("writing header failed: %w", err)
			}
		}
		s.Header = false
	}

	for _, m := range metrics {
		if len(s.Columns) > 0 {
			if err := s.writeDataOrdered(m); err != nil {
				return nil, fmt.Errorf("writing data failed: %w", err)
			}
		} else {
			if err := s.writeData(m); err != nil {
				return nil, fmt.Errorf("writing data failed: %w", err)
			}
		}
	}

	// Finish up
	s.writer.Flush()
	return s.buffer.Bytes(), nil
}

func (s *Serializer) writeHeader(metric telegraf.Metric) error {
	columns := []string{
		"timestamp",
		"measurement",
	}
	for _, tag := range metric.TagList() {
		if s.Prefix {
			columns = append(columns, "tag_"+tag.Key)
		} else {
			columns = append(columns, tag.Key)
		}
	}

	// Sort the fields by name
	sort.Slice(metric.FieldList(), func(i, j int) bool {
		return metric.FieldList()[i].Key < metric.FieldList()[j].Key
	})
	for _, field := range metric.FieldList() {
		if s.Prefix {
			columns = append(columns, "field_"+field.Key)
		} else {
			columns = append(columns, field.Key)
		}
	}

	return s.writer.Write(columns)
}

func (s *Serializer) writeHeaderOrdered() error {
	columns := make([]string, 0, len(s.Columns))
	for _, name := range s.Columns {
		if s.Prefix {
			name = strings.ReplaceAll(name, ".", "_")
		} else {
			name = strings.TrimPrefix(name, "tag.")
			name = strings.TrimPrefix(name, "field.")
		}
		columns = append(columns, name)
	}

	return s.writer.Write(columns)
}

func (s *Serializer) writeData(metric telegraf.Metric) error {
	var timestamp string

	// Format the time
	switch s.TimestampFormat {
	case "unix":
		timestamp = strconv.FormatInt(metric.Time().Unix(), 10)
	case "unix_ms":
		timestamp = strconv.FormatInt(metric.Time().UnixNano()/1_000_000, 10)
	case "unix_us":
		timestamp = strconv.FormatInt(metric.Time().UnixNano()/1_000, 10)
	case "unix_ns":
		timestamp = strconv.FormatInt(metric.Time().UnixNano(), 10)
	default:
		timestamp = metric.Time().UTC().Format(s.TimestampFormat)
	}

	columns := []string{
		timestamp,
		metric.Name(),
	}
	for _, tag := range metric.TagList() {
		columns = append(columns, tag.Value)
	}

	// Sort the fields by name
	sort.Slice(metric.FieldList(), func(i, j int) bool {
		return metric.FieldList()[i].Key < metric.FieldList()[j].Key
	})
	for _, field := range metric.FieldList() {
		v, err := internal.ToString(field.Value)
		if err != nil {
			return fmt.Errorf("converting field %q to string failed: %w", field.Key, err)
		}
		columns = append(columns, v)
	}

	return s.writer.Write(columns)
}

func (s *Serializer) writeDataOrdered(metric telegraf.Metric) error {
	var timestamp string

	// Format the time
	switch s.TimestampFormat {
	case "unix":
		timestamp = strconv.FormatInt(metric.Time().Unix(), 10)
	case "unix_ms":
		timestamp = strconv.FormatInt(metric.Time().UnixNano()/1_000_000, 10)
	case "unix_us":
		timestamp = strconv.FormatInt(metric.Time().UnixNano()/1_000, 10)
	case "unix_ns":
		timestamp = strconv.FormatInt(metric.Time().UnixNano(), 10)
	default:
		timestamp = metric.Time().UTC().Format(s.TimestampFormat)
	}

	columns := make([]string, 0, len(s.Columns))
	for _, name := range s.Columns {
		switch {
		case name == "timestamp":
			columns = append(columns, timestamp)
		case name == "name":
			columns = append(columns, metric.Name())
		case strings.HasPrefix(name, "tag."):
			v, _ := metric.GetTag(strings.TrimPrefix(name, "tag."))
			columns = append(columns, v)
		case strings.HasPrefix(name, "field."):
			var v string
			field := strings.TrimPrefix(name, "field.")
			if raw, ok := metric.GetField(field); ok {
				var err error
				v, err = internal.ToString(raw)
				if err != nil {
					return fmt.Errorf("converting field %q to string failed: %w", field, err)
				}
			}
			columns = append(columns, v)
		}
	}

	return s.writer.Write(columns)
}

func init() {
	serializers.Add("csv",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
