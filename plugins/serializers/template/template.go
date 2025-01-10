package template

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/serializers"
)

type Serializer struct {
	Template      string          `toml:"template"`
	BatchTemplate string          `toml:"batch_template"`
	Log           telegraf.Logger `toml:"-"`

	tmplMetric *template.Template
	tmplBatch  *template.Template
}

func (s *Serializer) Init() error {
	// Setting defaults
	var err error

	s.tmplMetric, err = template.New("template").Funcs(sprig.TxtFuncMap()).Parse(s.Template)
	if err != nil {
		return fmt.Errorf("creating template failed: %w", err)
	}
	if s.BatchTemplate == "" {
		s.BatchTemplate = fmt.Sprintf("{{range .}}%s{{end}}", s.Template)
	}
	s.tmplBatch, err = template.New("batch template").Funcs(sprig.TxtFuncMap()).Parse(s.BatchTemplate)
	if err != nil {
		return fmt.Errorf("creating batch template failed: %w", err)
	}
	return nil
}

func (s *Serializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	metricPlain := metric
	if wm, ok := metric.(telegraf.UnwrappableMetric); ok {
		metricPlain = wm.Unwrap()
	}
	m, ok := metricPlain.(telegraf.TemplateMetric)
	if !ok {
		s.Log.Errorf("metric of type %T is not a template metric", metricPlain)
		return nil, nil
	}
	var b bytes.Buffer
	// The template was defined for one metric, just execute it
	if s.Template != "" {
		if err := s.tmplMetric.Execute(&b, &m); err != nil {
			s.Log.Errorf("failed to execute template: %v", err)
			return nil, nil
		}
		return b.Bytes(), nil
	}

	// The template was defined for a batch of metrics, so wrap the metric into a slice
	if s.BatchTemplate != "" {
		metrics := []telegraf.TemplateMetric{m}
		if err := s.tmplBatch.Execute(&b, &metrics); err != nil {
			s.Log.Errorf("failed to execute batch template: %v", err)
			return nil, nil
		}
		return b.Bytes(), nil
	}

	// No template was defined
	return nil, nil
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	newMetrics := make([]telegraf.TemplateMetric, 0, len(metrics))

	for _, metric := range metrics {
		m, ok := metric.(telegraf.TemplateMetric)
		if !ok {
			s.Log.Errorf("metric of type %T is not a template metric", metric)
			return nil, nil
		}
		newMetrics = append(newMetrics, m)
	}

	var b bytes.Buffer
	if err := s.tmplBatch.Execute(&b, &newMetrics); err != nil {
		s.Log.Errorf("failed to execute batch template: %v", err)
		return nil, nil
	}

	return b.Bytes(), nil
}

func init() {
	serializers.Add("template",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
