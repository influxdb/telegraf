package stackdriver

import (
	"context"
	"fmt"
	"log"
	"path"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/outputs"

	// Imports the Stackdriver Monitoring client package.
	monitoring "cloud.google.com/go/monitoring/apiv3"
	googlepb "github.com/golang/protobuf/ptypes/timestamp"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// GCPStackdriver is the Google Stackdriver config info.
type GCPStackdriver struct {
	Project   string
	Namespace string

	client *monitoring.MetricClient
}

const (
	// StartTime for cumulative metrics.
	StartTime = int64(1)
	// MaxInt is the max int64 value.
	MaxInt = int(^uint(0) >> 1)
)

var sampleConfig = `
  # GCP Project
  project = "erudite-bloom-151019"

  # The namespace for the metric descriptor
  namespace = "telegraf"
`

// Connect initiates the primary connection to the GCP project.
func (s *GCPStackdriver) Connect() error {
	if s.Project == "" {
		return fmt.Errorf("Project is a required field for stackdriver output")
	}

	if s.Namespace == "" {
		return fmt.Errorf("Namespace is a required field for stackdriver output")
	}

	if s.client == nil {
		ctx := context.Background()

		// Creates a client
		client, err := monitoring.NewMetricClient(ctx)
		if err != nil {
			return err
		}

		s.client = client
	}

	return nil
}

// Write the metrics to Google Cloud Stackdriver.
func (s *GCPStackdriver) Write(metrics []telegraf.Metric) error {
	ctx := context.Background()

	for _, m := range metrics {
		for k, v := range m.Fields() {
			value, err := getStackdriverTypedValue(v)
			if err != nil {
				log.Printf("E! Error writing to output [stackdriver]: %s", err)
				continue
			}

			metricKind, err := getStackdriverMetricKind(telegraf.Histogram)
			if err != nil {
				log.Printf("E! Error writing to output [stackdriver]: %s", err)
				continue
			}

			timeInterval, err := getStackdriverTimeInterval(metricKind, StartTime, m.Time().Unix())
			if err != nil {
				log.Printf("E! Error writing to output [stackdriver]: %s", err)
				continue
			}

			// Prepare an individual data point.
			dataPoint := &monitoringpb.Point{
				Interval: timeInterval,
				Value:    value,
			}

			// Prepare time series.
			timeSeries := &monitoringpb.CreateTimeSeriesRequest{
				Name: monitoring.MetricProjectPath(s.Project),
				TimeSeries: []*monitoringpb.TimeSeries{
					{
						Metric: &metricpb.Metric{
							Type:   path.Join("custom.googleapis.com", s.Namespace, m.Name(), k),
							Labels: m.Tags(),
						},
						MetricKind: metricKind,
						Resource: &monitoredrespb.MonitoredResource{
							Type: "global",
							Labels: map[string]string{
								"project_id": s.Project,
							},
						},
						Points: []*monitoringpb.Point{
							dataPoint,
						},
					},
				}}

			// Create the time series in Stackdriver.
			err = s.client.CreateTimeSeries(ctx, timeSeries)
			if err != nil {
				log.Printf("E! Error writing to output [stackdriver]: %s", err)
				continue
			}
		}
	}

	return nil
}

func getStackdriverTimeInterval(
	m metricpb.MetricDescriptor_MetricKind,
	start int64,
	end int64,
) (*monitoringpb.TimeInterval, error) {
	switch m {
	case metricpb.MetricDescriptor_GAUGE:
		return &monitoringpb.TimeInterval{
			EndTime: &googlepb.Timestamp{
				Seconds: end,
			},
		}, nil
	case metricpb.MetricDescriptor_CUMULATIVE:
		return &monitoringpb.TimeInterval{
			StartTime: &googlepb.Timestamp{
				Seconds: start,
			},
			EndTime: &googlepb.Timestamp{
				Seconds: end,
			},
		}, nil
	case metricpb.MetricDescriptor_DELTA, metricpb.MetricDescriptor_METRIC_KIND_UNSPECIFIED:
		fallthrough
	default:
		return nil, fmt.Errorf("Unsupported metric kind %T", m)
	}
}

func getStackdriverMetricKind(vt telegraf.ValueType) (metricpb.MetricDescriptor_MetricKind, error) {
	switch vt {
	case telegraf.Untyped:
		return metricpb.MetricDescriptor_GAUGE, nil
	case telegraf.Gauge:
		return metricpb.MetricDescriptor_GAUGE, nil
	case telegraf.Counter:
		return metricpb.MetricDescriptor_CUMULATIVE, nil
	case telegraf.Histogram, telegraf.Summary:
		fallthrough
	default:
		return metricpb.MetricDescriptor_METRIC_KIND_UNSPECIFIED, fmt.Errorf("unsupported telegraf value type")
	}
}

func getStackdriverTypedValue(value interface{}) (*monitoringpb.TypedValue, error) {
	switch v := value.(type) {
	case uint64:
		if v <= uint64(MaxInt) {
			return &monitoringpb.TypedValue{
				Value: &monitoringpb.TypedValue_Int64Value{
					Int64Value: int64(v),
				},
			}, nil
		}
		return &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_Int64Value{
				Int64Value: int64(MaxInt),
			},
		}, nil
	case int64:
		return &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_Int64Value{
				Int64Value: int64(v),
			},
		}, nil
	case float64:
		return &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_DoubleValue{
				DoubleValue: float64(v),
			},
		}, nil
	case bool:
		return &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_BoolValue{
				BoolValue: bool(v),
			},
		}, nil
	default:
		return nil, fmt.Errorf("the value type \"%T\" is not supported for custom metrics", v)
	}
}

// Close will terminate the session to the backend, returning error if an issue arises.
func (s *GCPStackdriver) Close() error {
	return nil
}

// SampleConfig returns the formatted sample configuration for the plugin.
func (s *GCPStackdriver) SampleConfig() string {
	return sampleConfig
}

// Description returns the human-readable function definition of the plugin.
func (s *GCPStackdriver) Description() string {
	return "Configuration for Google Cloud Stackdriver to send metrics to"
}

func newGCPStackdriver() *GCPStackdriver {
	return &GCPStackdriver{}
}

func init() {
	outputs.Add("stackdriver", func() telegraf.Output {
		return newGCPStackdriver()
	})
}
