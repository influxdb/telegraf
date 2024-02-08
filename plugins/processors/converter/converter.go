//go:generate ../../../tools/readme_config_includer/generator
package converter

import (
	_ "embed"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/processors"
)

//go:embed sample.conf
var sampleConfig string

type Conversion struct {
	Measurement     []string `toml:"measurement"`
	Tag             []string `toml:"tag"`
	String          []string `toml:"string"`
	Integer         []string `toml:"integer"`
	Unsigned        []string `toml:"unsigned"`
	Boolean         []string `toml:"boolean"`
	Float           []string `toml:"float"`
	Timestamp       []string `toml:"timestamp"`
	TimestampFormat string   `toml:"timestamp_format"`
}

type Converter struct {
	Tags   *Conversion     `toml:"tags"`
	Fields *Conversion     `toml:"fields"`
	Log    telegraf.Logger `toml:"-"`

	tagConversions   *ConversionFilter
	fieldConversions *ConversionFilter
}

type ConversionFilter struct {
	Measurement filter.Filter
	Tag         filter.Filter
	String      filter.Filter
	Integer     filter.Filter
	Unsigned    filter.Filter
	Boolean     filter.Filter
	Float       filter.Filter
	Timestamp   filter.Filter
}

func (*Converter) SampleConfig() string {
	return sampleConfig
}

func (p *Converter) Init() error {
	return p.compile()
}

func (p *Converter) Apply(metrics ...telegraf.Metric) []telegraf.Metric {
	for _, metric := range metrics {
		p.convertTags(metric)
		p.convertFields(metric)
	}
	return metrics
}

func (p *Converter) compile() error {
	tf, err := compileFilter(p.Tags)
	if err != nil {
		return err
	}

	ff, err := compileFilter(p.Fields)
	if err != nil {
		return err
	}

	if tf == nil && ff == nil {
		return errors.New("no filters found")
	}

	p.tagConversions = tf
	p.fieldConversions = ff
	return nil
}

func compileFilter(conv *Conversion) (*ConversionFilter, error) {
	if conv == nil {
		return nil, nil
	}

	var err error
	cf := &ConversionFilter{}
	cf.Measurement, err = filter.Compile(conv.Measurement)
	if err != nil {
		return nil, err
	}

	cf.Tag, err = filter.Compile(conv.Tag)
	if err != nil {
		return nil, err
	}

	cf.String, err = filter.Compile(conv.String)
	if err != nil {
		return nil, err
	}

	cf.Integer, err = filter.Compile(conv.Integer)
	if err != nil {
		return nil, err
	}

	cf.Unsigned, err = filter.Compile(conv.Unsigned)
	if err != nil {
		return nil, err
	}

	cf.Boolean, err = filter.Compile(conv.Boolean)
	if err != nil {
		return nil, err
	}

	cf.Float, err = filter.Compile(conv.Float)
	if err != nil {
		return nil, err
	}

	cf.Timestamp, err = filter.Compile(conv.Timestamp)
	if err != nil {
		return nil, err
	}

	return cf, nil
}

// convertTags converts tags into measurements or fields.
func (p *Converter) convertTags(metric telegraf.Metric) {
	if p.tagConversions == nil {
		return
	}

	for key, value := range metric.Tags() {
		if p.tagConversions.Measurement != nil && p.tagConversions.Measurement.Match(key) {
			metric.RemoveTag(key)
			metric.SetName(value)
			continue
		}

		if p.tagConversions.String != nil && p.tagConversions.String.Match(key) {
			metric.RemoveTag(key)
			metric.AddField(key, value)
			continue
		}

		if p.tagConversions.Integer != nil && p.tagConversions.Integer.Match(key) {
			metric.RemoveTag(key)
			v, err := toInteger(value)
			if err != nil {
				p.Log.Errorf("Converting to integer [%T] failed: %v", value, err)
				continue
			}

			metric.AddField(key, v)
			continue
		}

		if p.tagConversions.Unsigned != nil && p.tagConversions.Unsigned.Match(key) {
			metric.RemoveTag(key)
			v, err := toUnsigned(value)
			if err != nil {
				p.Log.Errorf("Converting to unsigned [%T] failed: %v", value, err)
				continue
			}

			metric.AddField(key, v)
			continue
		}

		if p.tagConversions.Boolean != nil && p.tagConversions.Boolean.Match(key) {
			metric.RemoveTag(key)
			v, err := internal.ToBool(value)
			if err != nil {
				p.Log.Errorf("Converting to boolean [%T] failed: %v", value, err)
				continue
			}

			metric.AddField(key, v)
			continue
		}

		if p.tagConversions.Float != nil && p.tagConversions.Float.Match(key) {
			v, ok := toFloat(value)
			if !ok {
				metric.RemoveTag(key)
				p.Log.Errorf("error converting to float [%T]: %v", value, value)
				continue
			}

			metric.RemoveTag(key)
			metric.AddField(key, v)
			continue
		}

		if p.tagConversions.Timestamp != nil && p.tagConversions.Timestamp.Match(key) {
			time, err := internal.ParseTimestamp(p.Tags.TimestampFormat, value, nil)
			if err != nil {
				p.Log.Errorf("error converting to timestamp [%T]: %v", value, value)
				continue
			}

			metric.RemoveTag(key)
			metric.SetTime(time)
			continue
		}
	}
}

// convertFields converts fields into measurements, tags, or other field types.
func (p *Converter) convertFields(metric telegraf.Metric) {
	if p.fieldConversions == nil {
		return
	}

	for key, value := range metric.Fields() {
		if p.fieldConversions.Measurement != nil && p.fieldConversions.Measurement.Match(key) {
			metric.RemoveField(key)
			v, err := internal.ToString(value)
			if err != nil {
				p.Log.Errorf("Converting to measurement [%T] failed: %v", value, err)
				continue
			}
			metric.SetName(v)
			continue
		}

		if p.fieldConversions.Tag != nil && p.fieldConversions.Tag.Match(key) {
			metric.RemoveField(key)
			v, err := internal.ToString(value)
			if err != nil {
				p.Log.Errorf("Converting to tag [%T] failed: %v", value, err)
				continue
			}
			metric.AddTag(key, v)
			continue
		}

		if p.fieldConversions.Float != nil && p.fieldConversions.Float.Match(key) {
			v, ok := toFloat(value)
			if !ok {
				metric.RemoveField(key)
				p.Log.Errorf("error converting to float [%T]: %v", value, value)
				continue
			}

			metric.RemoveField(key)
			metric.AddField(key, v)
			continue
		}

		if p.fieldConversions.Integer != nil && p.fieldConversions.Integer.Match(key) {
			metric.RemoveField(key)
			v, err := toInteger(value)
			if err != nil {
				p.Log.Errorf("Converting to integer [%T] failed: %v", value, err)
				continue
			}

			metric.AddField(key, v)
			continue
		}

		if p.fieldConversions.Unsigned != nil && p.fieldConversions.Unsigned.Match(key) {
			metric.RemoveField(key)
			v, err := toUnsigned(value)
			if err != nil {
				p.Log.Errorf("error converting to unsigned [%T]: %v", value, value)
				continue
			}

			metric.AddField(key, v)
			continue
		}

		if p.fieldConversions.Boolean != nil && p.fieldConversions.Boolean.Match(key) {
			metric.RemoveField(key)
			v, err := internal.ToBool(value)
			if err != nil {
				p.Log.Errorf("Converting to bool [%T] failed: %v", value, err)
				continue
			}

			metric.AddField(key, v)
			continue
		}

		if p.fieldConversions.String != nil && p.fieldConversions.String.Match(key) {
			metric.RemoveField(key)
			v, err := internal.ToString(value)
			if err != nil {
				p.Log.Errorf("Converting to string [%T] failed: %v", value, err)
				continue
			}
			metric.AddField(key, v)
			continue
		}

		if p.fieldConversions.Timestamp != nil && p.fieldConversions.Timestamp.Match(key) {
			time, err := internal.ParseTimestamp(p.Fields.TimestampFormat, value, nil)
			if err != nil {
				p.Log.Errorf("error converting to timestamp [%T]: %v", value, value)
				continue
			}

			metric.RemoveField(key)
			metric.SetTime(time)
			continue
		}
	}
}

func toInteger(v interface{}) (int64, error) {
	switch value := v.(type) {
	case float32:
		if value < float32(math.MinInt64) {
			return math.MinInt64, nil
		}
		if value > float32(math.MaxInt64) {
			return math.MaxInt64, nil
		}
		return int64(math.Round(float64(value))), nil
	case float64:
		if value < float64(math.MinInt64) {
			return math.MinInt64, nil
		}
		if value > float64(math.MaxInt64) {
			return math.MaxInt64, nil
		}
		return int64(math.Round(value)), nil
	default:
		if v, err := internal.ToInt64(value); err == nil {
			return v, nil
		}

		v, err := internal.ToFloat64(value)
		if err != nil {
			return 0, err
		}

		if v < float64(math.MinInt64) {
			return math.MinInt64, nil
		}
		if v > float64(math.MaxInt64) {
			return math.MaxInt64, nil
		}
		return int64(math.Round(v)), nil
	}
}

func toUnsigned(v interface{}) (uint64, error) {
	switch value := v.(type) {

	case float32:
		if value < 0 {
			return 0, nil
		}
		if value > float32(math.MaxUint64) {
			return math.MaxUint64, nil
		}
		return uint64(math.Round(float64(value))), nil
	case float64:
		if value < 0 {
			return 0, nil
		}
		if value > float64(math.MaxUint64) {
			return math.MaxUint64, nil
		}
		return uint64(math.Round(value)), nil
	default:
		if v, err := internal.ToUint64(value); err == nil {
			return v, nil
		}

		v, err := internal.ToFloat64(value)
		if err != nil {
			return 0, err
		}

		if v < 0 {
			return 0, nil
		}
		if v > float64(math.MaxUint64) {
			return math.MaxUint64, nil
		}
		return uint64(math.Round(v)), nil
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch value := v.(type) {
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float64:
		return value, true
	case bool:
		if value {
			return 1.0, true
		}
		return 0.0, true
	case string:
		if isHexadecimal(value) {
			result, err := parseHexadecimal(value)
			return result, err == nil
		}

		result, err := strconv.ParseFloat(value, 64)
		return result, err == nil
	}
	return 0.0, false
}

func parseHexadecimal(value string) (float64, error) {
	i := new(big.Int)

	_, success := i.SetString(value, 0)
	if !success {
		return 0, errors.New("unable to parse string to big int")
	}

	f := new(big.Float).SetInt(i)
	result, _ := f.Float64()

	return result, nil
}

func isHexadecimal(value string) bool {
	return len(value) >= 3 && strings.ToLower(value)[1] == 'x'
}

func init() {
	processors.Add("converter", func() telegraf.Processor {
		return &Converter{}
	})
}
