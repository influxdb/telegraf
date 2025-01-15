package stackdriver

import (
	"context"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/api/distribution"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/testutil"
)

type call struct {
	name string
	args []interface{}
}

type mockStackdriverClient struct {
	listMetricDescriptorsF func() (<-chan *metricpb.MetricDescriptor, error)
	listTimeSeriesF        func() (<-chan *monitoringpb.TimeSeries, error)
	closeF                 func() error

	calls []*call
	sync.Mutex
}

func (m *mockStackdriverClient) listMetricDescriptors(
	ctx context.Context,
	req *monitoringpb.ListMetricDescriptorsRequest,
) (<-chan *metricpb.MetricDescriptor, error) {
	call := &call{name: "listMetricDescriptors", args: []interface{}{ctx, req}}
	m.Lock()
	m.calls = append(m.calls, call)
	m.Unlock()
	return m.listMetricDescriptorsF()
}

func (m *mockStackdriverClient) listTimeSeries(
	ctx context.Context,
	req *monitoringpb.ListTimeSeriesRequest,
) (<-chan *monitoringpb.TimeSeries, error) {
	call := &call{name: "listTimeSeries", args: []interface{}{ctx, req}}
	m.Lock()
	m.calls = append(m.calls, call)
	m.Unlock()
	return m.listTimeSeriesF()
}

func (m *mockStackdriverClient) close() error {
	call := &call{name: "close", args: make([]interface{}, 0)}
	m.Lock()
	m.calls = append(m.calls, call)
	m.Unlock()
	return m.closeF()
}

func TestInitAndRegister(t *testing.T) {
	expected := &Stackdriver{
		CacheTTL:                     defaultCacheTTL,
		RateLimit:                    defaultRateLimit,
		Delay:                        defaultDelay,
		GatherRawDistributionBuckets: true,
	}
	require.Equal(t, expected, inputs.Inputs["stackdriver"]())
}

func createTimeSeries(
	point *monitoringpb.Point, valueType metricpb.MetricDescriptor_ValueType,
) *monitoringpb.TimeSeries {
	return &monitoringpb.TimeSeries{
		Metric: &metricpb.Metric{Labels: make(map[string]string)},
		Resource: &monitoredres.MonitoredResource{
			Type: "global",
			Labels: map[string]string{
				"project_id": "test",
			},
		},
		Points:    []*monitoringpb.Point{point},
		ValueType: valueType,
	}
}

func TestGather(t *testing.T) {
	now := time.Now().Round(time.Second)
	tests := []struct {
		name       string
		descriptor *metricpb.MetricDescriptor
		timeseries *monitoringpb.TimeSeries
		expected   []telegraf.Metric
		wantAccErr bool
	}{
		{
			name: "no_bucket",
			descriptor: &metricpb.MetricDescriptor{
				ValueType: metricpb.MetricDescriptor_DISTRIBUTION,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_DistributionValue{
							DistributionValue: &distribution.Distribution{
								Count: 2,
							},
						},
					},
				},
				metricpb.MetricDescriptor_DISTRIBUTION,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("",
					map[string]string{
						"project_id":    "test",
						"resource_type": "global",
					},
					map[string]interface{}{
						"value_count":                    2,
						"value_mean":                     float64(0),
						"value_sum_of_squared_deviation": float64(0),
					},
					now),
			},
			wantAccErr: true,
		},
		{
			name: "int64",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_INT64,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_Int64Value{
							Int64Value: 42,
						},
					},
				},
				metricpb.MetricDescriptor_INT64,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage": 42,
					},
					now),
			},
		},
		{
			name: "double",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_DoubleValue{
							DoubleValue: 42.0,
						},
					},
				},
				metricpb.MetricDescriptor_DOUBLE,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage": 42.0,
					},
					now),
			},
		},
		{
			name: "int64",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_INT64,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_Int64Value{
							Int64Value: 42,
						},
					},
				},
				metricpb.MetricDescriptor_INT64,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage": 42,
					},
					now),
			},
		},
		{
			name: "bool",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_BOOL,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_BoolValue{
							BoolValue: true,
						},
					},
				},
				metricpb.MetricDescriptor_BOOL,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage": true,
					},
					now),
			},
		},
		{
			name: "string",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_STRING,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_StringValue{
							StringValue: "foo",
						},
					},
				},
				metricpb.MetricDescriptor_STRING,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage": "foo",
					},
					now),
			},
		},
		{
			name: "metric labels",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			timeseries: &monitoringpb.TimeSeries{
				Metric: &metricpb.Metric{
					Labels: map[string]string{
						"resource_type": "instance",
					},
				},
				Resource: &monitoredres.MonitoredResource{
					Type: "global",
					Labels: map[string]string{
						"project_id": "test",
					},
				},
				Points: []*monitoringpb.Point{
					{
						Interval: &monitoringpb.TimeInterval{
							EndTime: &timestamppb.Timestamp{
								Seconds: now.Unix(),
							},
						},
						Value: &monitoringpb.TypedValue{
							Value: &monitoringpb.TypedValue_DoubleValue{
								DoubleValue: 42.0,
							},
						},
					},
				},
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "instance",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage": 42.0,
					},
					now),
			},
		},
		{
			name: "linear buckets",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DISTRIBUTION,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_DistributionValue{
							DistributionValue: &distribution.Distribution{
								Count:                 2,
								Mean:                  2.0,
								SumOfSquaredDeviation: 1.0,
								Range: &distribution.Distribution_Range{
									Min: 0.0,
									Max: 3.0,
								},
								BucketCounts: []int64{0, 1, 3, 0},
								BucketOptions: &distribution.Distribution_BucketOptions{
									Options: &distribution.Distribution_BucketOptions_LinearBuckets{
										LinearBuckets: &distribution.Distribution_BucketOptions_Linear{
											NumFiniteBuckets: 2,
											Width:            1,
											Offset:           1,
										},
									},
								},
							},
						},
					},
				},
				metricpb.MetricDescriptor_DISTRIBUTION,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage_count":                    int64(2),
						"usage_range_min":                0.0,
						"usage_range_max":                3.0,
						"usage_mean":                     2.0,
						"usage_sum_of_squared_deviation": 1.0,
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "1",
					},
					map[string]interface{}{
						"usage_bucket": int64(0),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "2",
					},
					map[string]interface{}{
						"usage_bucket": int64(1),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "3",
					},
					map[string]interface{}{
						"usage_bucket": int64(4),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "+Inf",
					},
					map[string]interface{}{
						"usage_bucket": int64(4),
					},
					now),
			},
		},
		{
			name: "exponential buckets",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DISTRIBUTION,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_DistributionValue{
							DistributionValue: &distribution.Distribution{
								Count:                 2,
								Mean:                  2.0,
								SumOfSquaredDeviation: 1.0,
								Range: &distribution.Distribution_Range{
									Min: 0.0,
									Max: 3.0,
								},
								BucketCounts: []int64{0, 1, 3, 0},
								BucketOptions: &distribution.Distribution_BucketOptions{
									Options: &distribution.Distribution_BucketOptions_ExponentialBuckets{
										ExponentialBuckets: &distribution.Distribution_BucketOptions_Exponential{
											NumFiniteBuckets: 2,
											GrowthFactor:     2,
											Scale:            1,
										},
									},
								},
							},
						},
					},
				},
				metricpb.MetricDescriptor_DISTRIBUTION,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage_count":                    int64(2),
						"usage_range_min":                0.0,
						"usage_range_max":                3.0,
						"usage_mean":                     2.0,
						"usage_sum_of_squared_deviation": 1.0,
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "1",
					},
					map[string]interface{}{
						"usage_bucket": int64(0),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "2",
					},
					map[string]interface{}{
						"usage_bucket": int64(1),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "4",
					},
					map[string]interface{}{
						"usage_bucket": int64(4),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "+Inf",
					},
					map[string]interface{}{
						"usage_bucket": int64(4),
					},
					now),
			},
		},
		{
			name: "explicit buckets",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DISTRIBUTION,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_DistributionValue{
							DistributionValue: &distribution.Distribution{
								Count:                 4,
								Mean:                  2.0,
								SumOfSquaredDeviation: 1.0,
								Range: &distribution.Distribution_Range{
									Min: 0.0,
									Max: 3.0,
								},
								BucketCounts: []int64{0, 1, 3},
								BucketOptions: &distribution.Distribution_BucketOptions{
									Options: &distribution.Distribution_BucketOptions_ExplicitBuckets{
										ExplicitBuckets: &distribution.Distribution_BucketOptions_Explicit{
											Bounds: []float64{1.0, 2.0},
										},
									},
								},
							},
						},
					},
				},
				metricpb.MetricDescriptor_DISTRIBUTION,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage_count":                    int64(4),
						"usage_range_min":                0.0,
						"usage_range_max":                3.0,
						"usage_mean":                     2.0,
						"usage_sum_of_squared_deviation": 1.0,
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "1",
					},
					map[string]interface{}{
						"usage_bucket": int64(0),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "2",
					},
					map[string]interface{}{
						"usage_bucket": int64(1),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "+Inf",
					},
					map[string]interface{}{
						"usage_bucket": int64(4),
					},
					now),
			},
		},
		{
			name: "implicit buckets are zero",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DISTRIBUTION,
			},
			timeseries: createTimeSeries(
				&monitoringpb.Point{
					Interval: &monitoringpb.TimeInterval{
						EndTime: &timestamppb.Timestamp{
							Seconds: now.Unix(),
						},
					},
					Value: &monitoringpb.TypedValue{
						Value: &monitoringpb.TypedValue_DistributionValue{
							DistributionValue: &distribution.Distribution{
								Count:                 2,
								Mean:                  2.0,
								SumOfSquaredDeviation: 1.0,
								Range: &distribution.Distribution_Range{
									Min: 0.0,
									Max: 3.0,
								},
								BucketCounts: []int64{0, 1},
								BucketOptions: &distribution.Distribution_BucketOptions{
									Options: &distribution.Distribution_BucketOptions_LinearBuckets{
										LinearBuckets: &distribution.Distribution_BucketOptions_Linear{
											NumFiniteBuckets: 2,
											Width:            1,
											Offset:           1,
										},
									},
								},
							},
						},
					},
				},
				metricpb.MetricDescriptor_DISTRIBUTION,
			),
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage_count":                    int64(2),
						"usage_range_min":                0.0,
						"usage_range_max":                3.0,
						"usage_mean":                     2.0,
						"usage_sum_of_squared_deviation": 1.0,
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "1",
					},
					map[string]interface{}{
						"usage_bucket": int64(0),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "2",
					},
					map[string]interface{}{
						"usage_bucket": int64(1),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "3",
					},
					map[string]interface{}{
						"usage_bucket": int64(1),
					},
					now),
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
						"lt":            "+Inf",
					},
					map[string]interface{}{
						"usage_bucket": int64(1),
					},
					now),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			listMetricDescriptorsF := func() (<-chan *metricpb.MetricDescriptor, error) {
				ch := make(chan *metricpb.MetricDescriptor, 1)
				ch <- tt.descriptor
				close(ch)
				return ch, nil
			}
			listTimeSeriesF := func() (<-chan *monitoringpb.TimeSeries, error) {
				ch := make(chan *monitoringpb.TimeSeries, 1)
				ch <- tt.timeseries
				close(ch)
				return ch, nil
			}

			s := &Stackdriver{
				Log:                          testutil.Logger{},
				Project:                      "test",
				RateLimit:                    10,
				GatherRawDistributionBuckets: true,
				client: &mockStackdriverClient{
					listMetricDescriptorsF: listMetricDescriptorsF,
					listTimeSeriesF:        listTimeSeriesF,
					closeF: func() error {
						return nil
					},
				},
			}

			err := s.Gather(&acc)
			require.NoError(t, err)
			require.Equalf(t, tt.wantAccErr, len(acc.Errors) > 0,
				"Accumulator errors. got=%v, want=%t", acc.Errors, tt.wantAccErr)

			actual := make([]telegraf.Metric, 0, len(acc.Metrics))
			for _, m := range acc.Metrics {
				actual = append(actual, testutil.FromTestMetric(m))
			}

			testutil.RequireMetricsEqual(t, tt.expected, actual)
		})
	}
}

func TestGatherAlign(t *testing.T) {
	now := time.Now().Round(time.Second)
	tests := []struct {
		name       string
		descriptor *metricpb.MetricDescriptor
		timeseries []*monitoringpb.TimeSeries
		expected   []telegraf.Metric
	}{
		{
			name: "align",
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DISTRIBUTION,
			},
			timeseries: []*monitoringpb.TimeSeries{
				createTimeSeries(
					&monitoringpb.Point{
						Interval: &monitoringpb.TimeInterval{
							EndTime: &timestamppb.Timestamp{
								Seconds: now.Unix(),
							},
						},
						Value: &monitoringpb.TypedValue{
							Value: &monitoringpb.TypedValue_DoubleValue{
								DoubleValue: 42.0,
							},
						},
					},
					metricpb.MetricDescriptor_DOUBLE,
				),
				createTimeSeries(
					&monitoringpb.Point{
						Interval: &monitoringpb.TimeInterval{
							EndTime: &timestamppb.Timestamp{
								Seconds: now.Unix(),
							},
						},
						Value: &monitoringpb.TypedValue{
							Value: &monitoringpb.TypedValue_DoubleValue{
								DoubleValue: 42.0,
							},
						},
					},
					metricpb.MetricDescriptor_DOUBLE,
				),
				createTimeSeries(
					&monitoringpb.Point{
						Interval: &monitoringpb.TimeInterval{
							EndTime: &timestamppb.Timestamp{
								Seconds: now.Unix(),
							},
						},
						Value: &monitoringpb.TypedValue{
							Value: &monitoringpb.TypedValue_DoubleValue{
								DoubleValue: 42.0,
							},
						},
					},
					metricpb.MetricDescriptor_DOUBLE,
				),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric("telegraf/cpu",
					map[string]string{
						"resource_type": "global",
						"project_id":    "test",
					},
					map[string]interface{}{
						"usage_align_percentile_99": 42.0,
						"usage_align_percentile_95": 42.0,
						"usage_align_percentile_50": 42.0,
					},
					now),
			},
		},
	}
	for listCall, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			client := &mockStackdriverClient{
				listMetricDescriptorsF: func() (<-chan *metricpb.MetricDescriptor, error) {
					ch := make(chan *metricpb.MetricDescriptor, 1)
					ch <- tt.descriptor
					close(ch)
					return ch, nil
				},
				listTimeSeriesF: func() (<-chan *monitoringpb.TimeSeries, error) {
					ch := make(chan *monitoringpb.TimeSeries, 1)
					ch <- tt.timeseries[listCall]
					close(ch)
					return ch, nil
				},
				closeF: func() error {
					return nil
				},
			}

			s := &Stackdriver{
				Log:                          testutil.Logger{},
				Project:                      "test",
				RateLimit:                    10,
				GatherRawDistributionBuckets: false,
				DistributionAggregationAligners: []string{
					"ALIGN_PERCENTILE_99",
					"ALIGN_PERCENTILE_95",
					"ALIGN_PERCENTILE_50",
				},
				client: client,
			}

			err := s.Gather(&acc)
			require.NoError(t, err)

			actual := make([]telegraf.Metric, 0, len(acc.Metrics))
			for _, m := range acc.Metrics {
				actual = append(actual, testutil.FromTestMetric(m))
			}

			testutil.RequireMetricsEqual(t, tt.expected, actual)
		})
	}
}

func TestListMetricDescriptorFilter(t *testing.T) {
	type call struct {
		name   string
		filter string
	}
	now := time.Now().Round(time.Second)
	tests := []struct {
		name        string
		stackdriver *Stackdriver
		descriptor  *metricpb.MetricDescriptor
		calls       []call
	}{
		{
			name: "simple",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				RateLimit:               1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name:   "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage"`,
				},
			},
		},
		{
			name: "single resource labels string",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				Filter: &listTimeSeriesFilter{
					ResourceLabels: []*label{
						{
							Key:   "instance_name",
							Value: `localhost`,
						},
					},
				},
				RateLimit: 1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name:   "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage" AND resource.labels.instance_name = "localhost"`,
				},
			},
		},
		{
			name: "single resource labels function",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				Filter: &listTimeSeriesFilter{
					ResourceLabels: []*label{
						{
							Key:   "instance_name",
							Value: `starts_with("localhost")`,
						},
					},
				},
				RateLimit: 1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name:   "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage" AND resource.labels.instance_name = starts_with("localhost")`,
				},
			},
		},
		{
			name: "multiple resource labels",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				Filter: &listTimeSeriesFilter{
					ResourceLabels: []*label{
						{
							Key:   "instance_name",
							Value: `localhost`,
						},
						{
							Key:   "zone",
							Value: `starts_with("us-")`,
						},
					},
				},
				RateLimit: 1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name:   "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage" AND (resource.labels.instance_name = "localhost" OR resource.labels.zone = starts_with("us-"))`,
				},
			},
		},
		{
			name: "single metric label string",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				Filter: &listTimeSeriesFilter{
					MetricLabels: []*label{
						{
							Key:   "resource_type",
							Value: `instance`,
						},
					},
				},
				RateLimit: 1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name:   "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage" AND metric.labels.resource_type = "instance"`,
				},
			},
		},
		{
			name: "single metric label function",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				Filter: &listTimeSeriesFilter{
					MetricLabels: []*label{
						{
							Key:   "resource_id",
							Value: `starts_with("abc-")`,
						},
					},
				},
				RateLimit: 1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name:   "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage" AND metric.labels.resource_id = starts_with("abc-")`,
				},
			},
		},
		{
			name: "multiple metric labels",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				Filter: &listTimeSeriesFilter{
					MetricLabels: []*label{
						{
							Key:   "resource_type",
							Value: "instance",
						},
						{
							Key:   "resource_id",
							Value: `starts_with("abc-")`,
						},
					},
				},
				RateLimit: 1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name: "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage" AND ` +
						`(metric.labels.resource_type = "instance" OR metric.labels.resource_id = starts_with("abc-"))`,
				},
			},
		},
		{
			name: "all labels filters",
			stackdriver: &Stackdriver{
				Project:                 "test",
				MetricTypePrefixInclude: []string{"telegraf/cpu/usage"},
				Filter: &listTimeSeriesFilter{
					ResourceLabels: []*label{
						{
							Key:   "instance_name",
							Value: `localhost`,
						},
						{
							Key:   "zone",
							Value: `starts_with("us-")`,
						},
					},
					MetricLabels: []*label{
						{
							Key:   "resource_type",
							Value: "instance",
						},
						{
							Key:   "resource_id",
							Value: `starts_with("abc-")`,
						},
					},
					UserLabels: []*label{
						{
							Key:   "team",
							Value: "badgers",
						},
						{
							Key:   "environment",
							Value: `starts_with("prod-")`,
						},
					},
					SystemLabels: []*label{
						{
							Key:   "machine_type",
							Value: "e2",
						},
						{
							Key:   "machine_type",
							Value: `starts_with("n2")`,
						},
					},
				},
				RateLimit: 1,
			},
			descriptor: &metricpb.MetricDescriptor{
				Type:      "telegraf/cpu/usage",
				ValueType: metricpb.MetricDescriptor_DOUBLE,
			},
			calls: []call{
				{
					name:   "listMetricDescriptors",
					filter: `metric.type = starts_with("telegraf/cpu/usage")`,
				}, {
					name: "listTimeSeries",
					filter: `metric.type = "telegraf/cpu/usage" AND ` +
						`(resource.labels.instance_name = "localhost" OR resource.labels.zone = starts_with("us-")) AND ` +
						`(metric.labels.resource_type = "instance" OR metric.labels.resource_id = starts_with("abc-")) AND ` +
						`(metadata.user_labels."team" = "badgers" OR metadata.user_labels."environment" = starts_with("prod-")) AND ` +
						`(metadata.system_labels."machine_type" = "e2" OR metadata.system_labels."machine_type" = starts_with("n2"))`,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			client := &mockStackdriverClient{
				listMetricDescriptorsF: func() (<-chan *metricpb.MetricDescriptor, error) {
					ch := make(chan *metricpb.MetricDescriptor, 1)
					ch <- tt.descriptor
					close(ch)
					return ch, nil
				},
				listTimeSeriesF: func() (<-chan *monitoringpb.TimeSeries, error) {
					ch := make(chan *monitoringpb.TimeSeries, 1)
					ch <- createTimeSeries(
						&monitoringpb.Point{
							Interval: &monitoringpb.TimeInterval{
								EndTime: &timestamppb.Timestamp{
									Seconds: now.Unix(),
								},
							},
							Value: &monitoringpb.TypedValue{
								Value: &monitoringpb.TypedValue_DoubleValue{
									DoubleValue: 42.0,
								},
							},
						},
						metricpb.MetricDescriptor_DOUBLE,
					)
					close(ch)
					return ch, nil
				},
				closeF: func() error {
					return nil
				},
			}

			s := tt.stackdriver
			s.client = client

			err := s.Gather(&acc)
			require.NoError(t, err)

			require.Equal(t, len(client.calls), len(tt.calls))
			for i, expected := range tt.calls {
				actual := client.calls[i]
				require.Equal(t, expected.name, actual.name)

				switch req := actual.args[1].(type) {
				case *monitoringpb.ListMetricDescriptorsRequest:
					require.Equal(t, expected.filter, req.Filter)
				case *monitoringpb.ListTimeSeriesRequest:
					require.Equal(t, expected.filter, req.Filter)
				default:
					panic("unknown request type")
				}
			}
		})
	}
}

func TestNewListTimeSeriesFilter(_ *testing.T) {
}

func TestTimeSeriesConfCacheIsValid(_ *testing.T) {
}
