//go:generate ../../../tools/readme_config_includer/generator
package converter

import (
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
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
	Measurement       []string `toml:"measurement"`
	Tag               []string `toml:"tag"`
	String            []string `toml:"string"`
	Integer           []string `toml:"integer"`
	Unsigned          []string `toml:"unsigned"`
	Boolean           []string `toml:"boolean"`
	Float             []string `toml:"float"`
	Timestamp         []string `toml:"timestamp"`
	TimestampFormat   string   `toml:"timestamp_format"`
	Base64IEEEFloat32 []string `toml:"base64_ieee_float32"`
}

type Converter struct {
	Tags   *Conversion     `toml:"tags"`
	Fields *Conversion     `toml:"fields"`
	Log    telegraf.Logger `toml:"-"`

	tagConversions   *ConversionFilter
	fieldConversions *ConversionFilter
}

type ConversionFilter struct {
	Measurement       filter.Filter
	Tag               filter.Filter
	String            filter.Filter
	Integer           filter.Filter
	Unsigned          filter.Filter
	Boolean           filter.Filter
	Float             filter.Filter
	Timestamp         filter.Filter
	Base64IEEEFloat32 filter.Filter
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

	cf.Base64IEEEFloat32, err = filter.Compile(conv.Base64IEEEFloat32)
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
		switch {
		case p.tagConversions.Measurement != nil && p.tagConversions.Measurement.Match(key):
			metric.SetName(value)
		case p.tagConversions.String != nil && p.tagConversions.String.Match(key):
			metric.AddField(key, value)
		case p.tagConversions.Integer != nil && p.tagConversions.Integer.Match(key):
			if v, err := toInteger(value); err != nil {
				p.Log.Errorf("Converting to integer [%T] failed: %v", value, err)
			} else {
				metric.AddField(key, v)
			}
		case p.tagConversions.Unsigned != nil && p.tagConversions.Unsigned.Match(key):
			if v, err := toUnsigned(value); err != nil {
				p.Log.Errorf("Converting to unsigned [%T] failed: %v", value, err)
			} else {
				metric.AddField(key, v)
			}
		case p.tagConversions.Boolean != nil && p.tagConversions.Boolean.Match(key):
			if v, err := internal.ToBool(value); err != nil {
				p.Log.Errorf("Converting to boolean [%T] failed: %v", value, err)
			} else {
				metric.AddField(key, v)
			}
		case p.tagConversions.Float != nil && p.tagConversions.Float.Match(key):
			if v, err := toFloat(value); err != nil {
				p.Log.Errorf("Converting to float [%T] failed: %v", value, err)
			} else {
				metric.AddField(key, v)
			}
		case p.tagConversions.Timestamp != nil && p.tagConversions.Timestamp.Match(key):
			if time, err := internal.ParseTimestamp(p.Tags.TimestampFormat, value, nil); err != nil {
				p.Log.Errorf("Converting to timestamp [%T] failed: %v", value, err)
				continue
			} else {
				metric.SetTime(time)
			}
		default:
			continue
		}
		metric.RemoveTag(key)
	}
}

// convertFields converts fields into measurements, tags, or other field types.
func (p *Converter) convertFields(metric telegraf.Metric) {
	if p.fieldConversions == nil {
		return
	}

	for key, value := range metric.Fields() {
		switch {
		case p.fieldConversions.Measurement != nil && p.fieldConversions.Measurement.Match(key):
			if v, err := internal.ToString(value); err != nil {
				p.Log.Errorf("Converting to measurement [%T] failed: %v", value, err)
			} else {
				metric.SetName(v)
			}
			metric.RemoveField(key)
		case p.fieldConversions.Tag != nil && p.fieldConversions.Tag.Match(key):
			if v, err := internal.ToString(value); err != nil {
				p.Log.Errorf("Converting to tag [%T] failed: %v", value, err)
			} else {
				metric.AddTag(key, v)
			}
			metric.RemoveField(key)
		case p.fieldConversions.Float != nil && p.fieldConversions.Float.Match(key):
			if v, err := toFloat(value); err != nil {
				p.Log.Errorf("Converting to float [%T] failed: %v", value, err)
				metric.RemoveField(key)
			} else {
				metric.AddField(key, v)
			}
		case p.fieldConversions.Integer != nil && p.fieldConversions.Integer.Match(key):
			if v, err := toInteger(value); err != nil {
				p.Log.Errorf("Converting to integer [%T] failed: %v", value, err)
				metric.RemoveField(key)
			} else {
				metric.AddField(key, v)
			}
		case p.fieldConversions.Unsigned != nil && p.fieldConversions.Unsigned.Match(key):
			if v, err := toUnsigned(value); err != nil {
				p.Log.Errorf("Converting to unsigned [%T] failed: %v", value, err)
				metric.RemoveField(key)
			} else {
				metric.AddField(key, v)
			}
		case p.fieldConversions.Boolean != nil && p.fieldConversions.Boolean.Match(key):
			if v, err := internal.ToBool(value); err != nil {
				p.Log.Errorf("Converting to bool [%T] failed: %v", value, err)
				metric.RemoveField(key)
			} else {
				metric.AddField(key, v)
			}
		case p.fieldConversions.String != nil && p.fieldConversions.String.Match(key):
			if v, err := internal.ToString(value); err != nil {
				p.Log.Errorf("Converting to string [%T] failed: %v", value, err)
				metric.RemoveField(key)
			} else {
				metric.AddField(key, v)
			}
		case p.fieldConversions.Timestamp != nil && p.fieldConversions.Timestamp.Match(key):
			if time, err := internal.ParseTimestamp(p.Fields.TimestampFormat, value, nil); err != nil {
				p.Log.Errorf("Converting to timestamp [%T] failed: %v", value, err)
			} else {
				metric.SetTime(time)
				metric.RemoveField(key)
			}

		case p.fieldConversions.Base64IEEEFloat32 != nil && p.fieldConversions.Base64IEEEFloat32.Match(key):
			if v, err := base64ToFloat32(value.(string)); err != nil {
				p.Log.Errorf("Converting to base64_ieee_float32 [%T] failed: %v", value, err)
				metric.RemoveField(key)
			} else {
				metric.AddField(key, v)
			}
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

func toFloat(v interface{}) (float64, error) {
	if v, ok := v.(string); ok && strings.HasPrefix(v, "0x") {
		var i big.Int
		if _, success := i.SetString(v, 0); !success {
			return 0, errors.New("unable to parse string to big int")
		}

		var f big.Float
		f.SetInt(&i)
		result, _ := f.Float64()

		return result, nil
	}
	return internal.ToFloat64(v)
}

func base64ToFloat32(encoded string) (float32, error) {
	// Decode the Base64 string to bytes
	decodedBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return 0, err
	}

	// Check if the byte length matches a float32 (4 bytes)
	if len(decodedBytes) != 4 {
		return 0, errors.New("decoded byte length is not 4 bytes")
	}

	// Convert the bytes to a string representation as per IEEE 754 of the bits
	bitsStrRepresentation := fmt.Sprintf("%08b%08b%08b%08b", decodedBytes[0], decodedBytes[1], decodedBytes[2], decodedBytes[3])

	// Convert the bits to a uint32
	bits, err := strconv.ParseUint(bitsStrRepresentation, 2, 32)

	if err != nil {
		return 0, err
	}

	// Convert the uint32 (bits) to a float32 based on IEEE 754 binary representation
	return math.Float32frombits(uint32(bits)), nil
}

func init() {
	processors.Add("converter", func() telegraf.Processor {
		return &Converter{}
	})
}
