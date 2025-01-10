package nowmetric

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/serializers"
)

type Serializer struct {
	Format string `toml:"nowmetric_format"`
}

type OIMetric struct {
	Metric    string            `json:"metric_type"`
	Resource  string            `json:"resource"`
	Node      string            `json:"node"`
	Value     interface{}       `json:"value"`
	Timestamp int64             `json:"timestamp"`
	CiMapping map[string]string `json:"ci2metric_id"`
	Source    string            `json:"source"`
}

type OIMetrics []OIMetric
type OIMetricsObj struct {
	Records []OIMetric `json:"records"`
}

func (s *Serializer) Init() error {
	switch s.Format {
	case "":
		s.Format = "oi"
	case "oi", "jsonv2":
	default:
		return fmt.Errorf("invalid format %q", s.Format)
	}

	return nil
}

func (s *Serializer) Serialize(metric telegraf.Metric) (out []byte, err error) {
	m := s.createObject(metric)

	if s.Format == "jsonv2" {
		obj := OIMetricsObj{Records: m}
		return json.Marshal(obj)
	}
	return json.Marshal(m)
}

func (s *Serializer) SerializeBatch(metrics []telegraf.Metric) (out []byte, err error) {
	objects := make([]OIMetric, 0)
	for _, metric := range metrics {
		objects = append(objects, s.createObject(metric)...)
	}

	if s.Format == "jsonv2" {
		obj := OIMetricsObj{Records: objects}
		return json.Marshal(obj)
	}

	return json.Marshal(objects)
}

func (s *Serializer) createObject(metric telegraf.Metric) OIMetrics {
	/*  ServiceNow Operational Intelligence supports an array of JSON objects.
	** Following elements accepted in the request body:
		 ** metric_type: 	The name of the metric
		 ** resource:   	Information about the resource for which metric data is being collected.
	                                In the example below, C:\ is the resource for which metric data is collected
		 ** node:       	IP, FQDN, name of the CI, or host
		 ** value:      	Value of the metric
		 ** timestamp: 		Epoch timestamp of the metric in milliseconds
		 ** ci2metric_id:	List of key-value pairs to identify the CI.
		 ** source:			Data source monitoring the metric type
	*/
	var allmetrics OIMetrics //nolint:prealloc // Pre-allocating may change format of marshaled JSON
	var oimetric OIMetric
	oimetric.Source = "Telegraf"

	// Process Tags to extract node & resource name info
	for _, tag := range metric.TagList() {
		if tag.Key == "" || tag.Value == "" {
			continue
		}

		if tag.Key == "objectname" {
			oimetric.Resource = tag.Value
		}

		if tag.Key == "host" {
			oimetric.Node = tag.Value
		}
	}

	// Format timestamp to UNIX epoch
	oimetric.Timestamp = metric.Time().UnixNano() / int64(time.Millisecond)

	// Loop of fields value pair and build datapoint for each of them
	for _, field := range metric.FieldList() {
		if !verifyValue(field.Value) {
			// Ignore String
			continue
		}

		if field.Key == "" {
			// Ignore Empty Key
			continue
		}

		oimetric.Metric = field.Key
		oimetric.Value = field.Value

		if oimetric.Node != "" {
			oimetric.CiMapping = map[string]string{"node": oimetric.Node}
		}

		allmetrics = append(allmetrics, oimetric)
	}

	return allmetrics
}

func verifyValue(v interface{}) bool {
	_, ok := v.(string)
	return !ok
}

func init() {
	serializers.Add("nowmetric",
		func() telegraf.Serializer {
			return &Serializer{}
		},
	)
}
