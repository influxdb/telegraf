package wavefront

import (
	"log"
	"strconv"
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/serializers"
)

type Serializer struct {
	Prefix                   string   `toml:"prefix"`
	UseStrict                bool     `toml:"wavefront_use_strict"`
	SourceOverride           []string `toml:"wavefront_source_override"`
	DisablePrefixConversions bool     `toml:"wavefront_disable_prefix_conversion"`

	scratch buffer
	mu      sync.Mutex // buffer mutex
}

type MetricPoint struct {
	Metric    string
	Value     float64
	Timestamp int64
	Source    string
	Tags      map[string]string
}

func (s *Serializer) serializeMetric(m telegraf.Metric) {
	const metricSeparator = "."

	for fieldName, value := range m.Fields() {
		var name string

		if fieldName == "value" {
			name = s.Prefix + m.Name()
		} else {
			name = s.Prefix + m.Name() + metricSeparator + fieldName
		}

		name = Sanitize(s.UseStrict, name)

		if !s.DisablePrefixConversions {
			name = pathReplacer.Replace(name)
		}

		metricValue, valid := buildValue(value, name)
		if !valid {
			// bad value continue to next metric
			continue
		}
		source, tags := s.buildTags(m.Tags())
		metric := MetricPoint{
			Metric:    name,
			Timestamp: m.Time().Unix(),
			Value:     metricValue,
			Source:    source,
			Tags:      tags,
		}
		formatMetricPoint(&s.scratch, &metric, s)
	}
}

// Serialize : Serialize based on Wavefront format
func (s *Serializer) Serialize(m telegraf.Metric) ([]byte, error) {
	s.mu.Lock()
	s.scratch.Reset()
	s.serializeMetric(m)
	out := s.scratch.Copy()
	s.mu.Unlock()
	return out, nil
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	s.mu.Lock()
	s.scratch.Reset()
	for _, m := range metrics {
		s.serializeMetric(m)
	}
	out := s.scratch.Copy()
	s.mu.Unlock()
	return out, nil
}

func (s *Serializer) findSourceTag(mTags map[string]string) string {
	if src, ok := mTags["source"]; ok {
		delete(mTags, "source")
		return src
	}
	for _, src := range s.SourceOverride {
		if source, ok := mTags[src]; ok {
			delete(mTags, src)
			mTags["telegraf_host"] = mTags["host"]
			return source
		}
	}
	return mTags["host"]
}

func (s *Serializer) buildTags(mTags map[string]string) (string, map[string]string) {
	// Remove all empty tags.
	for k, v := range mTags {
		if v == "" {
			delete(mTags, k)
		}
	}
	source := s.findSourceTag(mTags)
	delete(mTags, "host")
	return tagValueReplacer.Replace(source), mTags
}

func buildValue(v interface{}, name string) (val float64, valid bool) {
	switch p := v.(type) {
	case bool:
		if p {
			return 1, true
		}
		return 0, true
	case int64:
		return float64(p), true
	case uint64:
		return float64(p), true
	case float64:
		return p, true
	case string:
		// return false but don't log
		return 0, false
	default:
		// log a debug message
		log.Printf("D! Serializer [wavefront] unexpected type: %T, with value: %v, for :%s\n",
			v, v, name)
		return 0, false
	}
}

func formatMetricPoint(b *buffer, metricPoint *MetricPoint, s *Serializer) []byte {
	b.WriteChar('"')
	b.WriteString(metricPoint.Metric)
	b.WriteString(`" `)
	b.WriteFloat64(metricPoint.Value)
	b.WriteChar(' ')
	b.WriteUint64(uint64(metricPoint.Timestamp))
	b.WriteString(` source="`)
	b.WriteString(metricPoint.Source)
	b.WriteChar('"')

	for k, v := range metricPoint.Tags {
		b.WriteString(` "`)
		b.WriteString(Sanitize(s.UseStrict, k))
		b.WriteString(`"="`)
		b.WriteString(tagValueReplacer.Replace(v))
		b.WriteChar('"')
	}

	b.WriteChar('\n')

	return *b
}

type buffer []byte

func (b *buffer) Reset() { *b = (*b)[:0] }

func (b *buffer) Copy() []byte {
	p := make([]byte, 0, len(*b))
	return append(p, *b...)
}

func (b *buffer) WriteString(s string) {
	*b = append(*b, s...)
}

// WriteChar has this name instead of WriteByte because the 'stdmethods' check
// of 'go vet' wants WriteByte to have the signature:
//
//	func (b *buffer) WriteByte(c byte) error { ... }
func (b *buffer) WriteChar(c byte) {
	*b = append(*b, c)
}

func (b *buffer) WriteUint64(val uint64) {
	*b = strconv.AppendUint(*b, val, 10)
}

func (b *buffer) WriteFloat64(val float64) {
	*b = strconv.AppendFloat(*b, val, 'f', 6, 64)
}

func init() {
	serializers.Add("wavefront",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
