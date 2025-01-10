//go:generate ../../../tools/readme_config_includer/generator
package unpivot

import (
	_ "embed"
	"fmt"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/processors"
)

//go:embed sample.conf
var sampleConfig string

type Unpivot struct {
	FieldNameAs string `toml:"use_fieldname_as"`
	TagKey      string `toml:"tag_key"`
	ValueKey    string `toml:"value_key"`
}

func (*Unpivot) SampleConfig() string {
	return sampleConfig
}

func (p *Unpivot) Init() error {
	switch p.FieldNameAs {
	case "", "tag":
		p.FieldNameAs = "tag"
	case "metric":
	default:
		return fmt.Errorf("unrecognized metric mode: %q", p.FieldNameAs)
	}

	if p.TagKey == "" {
		p.TagKey = "name"
	}
	if p.ValueKey == "" {
		p.ValueKey = "value"
	}

	return nil
}

func (p *Unpivot) Apply(metrics ...telegraf.Metric) []telegraf.Metric {
	fieldCount := 0
	for _, m := range metrics {
		fieldCount += len(m.FieldList())
	}
	results := make([]telegraf.Metric, 0, fieldCount)

	for _, src := range metrics {
		// Create a copy without fields and tracking information
		base := metric.New(src.Name(), make(map[string]string), make(map[string]interface{}), src.Time())
		for _, t := range src.TagList() {
			base.AddTag(t.Key, t.Value)
		}

		// Create a new metric per field and add it to the output
		for _, field := range src.FieldList() {
			m := base.Copy()
			m.AddField(p.ValueKey, field.Value)

			switch p.FieldNameAs {
			case "metric":
				m.SetName(field.Key)
			case "tag":
				m.AddTag(p.TagKey, field.Key)
			}

			results = append(results, m)
		}
		src.Accept()
	}
	return results
}

func init() {
	processors.Add("unpivot", func() telegraf.Processor {
		return &Unpivot{}
	})
}
