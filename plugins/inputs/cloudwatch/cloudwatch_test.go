package cloudwatch

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/metric"
	common_aws "github.com/influxdata/telegraf/plugins/common/aws"
	"github.com/influxdata/telegraf/plugins/common/proxy"
	"github.com/influxdata/telegraf/testutil"
)

func TestSnakeCase(t *testing.T) {
	require.Equal(t, "cluster_name", snakeCase("Cluster Name"))
	require.Equal(t, "broker_id", snakeCase("Broker ID"))
}

func TestGather(t *testing.T) {
	plugin := &CloudWatch{
		CredentialConfig: common_aws.CredentialConfig{
			Region: "us-east-1",
		},
		Namespace: "AWS/ELB",
		Delay:     config.Duration(1 * time.Minute),
		Period:    config.Duration(1 * time.Minute),
		RateLimit: 200,
		BatchSize: 500,
		Log:       testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	plugin.client = defaultMockClient("AWS/ELB")

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example1",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          123.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example2",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          124.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestGatherDenseMetric(t *testing.T) {
	plugin := &CloudWatch{
		CredentialConfig: common_aws.CredentialConfig{
			Region: "us-east-1",
		},
		Namespace:    "AWS/ELB",
		Delay:        config.Duration(1 * time.Minute),
		Period:       config.Duration(1 * time.Minute),
		RateLimit:    200,
		BatchSize:    500,
		MetricFormat: "dense",
		Log:          testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	plugin.client = defaultMockClient("AWS/ELB")

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example1",
				"metric_name":        "latency",
			},
			map[string]interface{}{
				"minimum":      0.1,
				"maximum":      0.3,
				"average":      0.2,
				"sum":          123.0,
				"sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example2",
				"metric_name":        "latency",
			},
			map[string]interface{}{
				"minimum":      0.1,
				"maximum":      0.3,
				"average":      0.2,
				"sum":          124.0,
				"sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestMultiAccountGather(t *testing.T) {
	plugin := &CloudWatch{
		CredentialConfig: common_aws.CredentialConfig{
			Region: "us-east-1",
		},
		Namespace:             "AWS/ELB",
		Delay:                 config.Duration(1 * time.Minute),
		Period:                config.Duration(1 * time.Minute),
		RateLimit:             200,
		BatchSize:             500,
		Log:                   testutil.Logger{},
		IncludeLinkedAccounts: true,
	}
	require.NoError(t, plugin.Init())
	plugin.client = defaultMockClient("AWS/ELB")

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example1",
				"account":            "123456789012",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          123.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example2",
				"account":            "923456789017",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          124.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestGatherMultipleNamespaces(t *testing.T) {
	plugin := &CloudWatch{
		CredentialConfig: common_aws.CredentialConfig{
			Region: "us-east-1",
		},
		Namespaces: []string{"AWS/ELB", "AWS/EC2"},
		Delay:      config.Duration(1 * time.Minute),
		Period:     config.Duration(1 * time.Minute),
		RateLimit:  200,
		BatchSize:  500,
		Log:        testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	plugin.client = defaultMockClient("AWS/ELB", "AWS/EC2")

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	expected := []telegraf.Metric{
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example1",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          123.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"cloudwatch_aws_elb",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example2",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          124.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"cloudwatch_aws_ec2",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example1",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          123.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
		metric.New(
			"cloudwatch_aws_ec2",
			map[string]string{
				"region":             "us-east-1",
				"load_balancer_name": "p-example2",
			},
			map[string]interface{}{
				"latency_minimum":      0.1,
				"latency_maximum":      0.3,
				"latency_average":      0.2,
				"latency_sum":          124.0,
				"latency_sample_count": 100.0,
			},
			time.Unix(0, 0),
		),
	}

	option := []cmp.Option{
		testutil.IgnoreTime(),
		testutil.SortMetrics(),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), option...)
}

func TestSelectMetrics(t *testing.T) {
	plugin := &CloudWatch{
		CredentialConfig: common_aws.CredentialConfig{
			Region: "us-east-1",
		},
		Namespace: "AWS/ELB",
		Delay:     config.Duration(1 * time.Minute),
		Period:    config.Duration(1 * time.Minute),
		RateLimit: 200,
		BatchSize: 500,
		Metrics: []*cloudwatchMetric{
			{
				MetricNames: []string{"Latency", "RequestCount"},
				Dimensions: []*dimension{
					{
						Name:  "LoadBalancerName",
						Value: "lb*",
					},
					{
						Name:  "AvailabilityZone",
						Value: "us-east*",
					},
				},
			},
		},
		Log: testutil.Logger{},
	}
	require.NoError(t, plugin.Init())
	plugin.client = selectedMockClient()
	filtered, err := plugin.getFilteredMetrics()
	// We've asked for 2 (out of 4) metrics, over all 3 load balancers in all 2
	// AZs. We should get 12 metrics.
	require.Len(t, filtered[0].metrics, 12)
	require.NoError(t, err)
}

func TestGenerateStatisticsInputParams(t *testing.T) {
	d := types.Dimension{
		Name:  aws.String("LoadBalancerName"),
		Value: aws.String("p-example"),
	}

	namespace := "AWS/ELB"
	m := types.Metric{
		MetricName: aws.String("Latency"),
		Dimensions: []types.Dimension{d},
		Namespace:  aws.String(namespace),
	}

	plugin := &CloudWatch{
		Namespaces: []string{namespace},
		Delay:      config.Duration(1 * time.Minute),
		Period:     config.Duration(1 * time.Minute),
		BatchSize:  500,
		Log:        testutil.Logger{},
	}
	require.NoError(t, plugin.Init())

	now := time.Now()

	plugin.updateWindow(now)

	statFilter, err := filter.NewIncludeExcludeFilter(nil, nil)
	require.NoError(t, err)
	queries := plugin.getDataQueries([]filteredMetric{{metrics: []types.Metric{m}, statFilter: statFilter}})
	params := &cloudwatch.GetMetricDataInput{
		StartTime:         aws.Time(plugin.windowStart),
		EndTime:           aws.Time(plugin.windowEnd),
		MetricDataQueries: queries[namespace],
	}

	require.EqualValues(t, *params.EndTime, now.Add(-time.Duration(plugin.Delay)))
	require.EqualValues(t, *params.StartTime, now.Add(-time.Duration(plugin.Period)).Add(-time.Duration(plugin.Delay)))
	require.Len(t, params.MetricDataQueries, 5)
	require.Len(t, params.MetricDataQueries[0].MetricStat.Metric.Dimensions, 1)
	require.EqualValues(t, 60, *params.MetricDataQueries[0].MetricStat.Period)
}

func TestGenerateStatisticsInputParamsFiltered(t *testing.T) {
	d := types.Dimension{
		Name:  aws.String("LoadBalancerName"),
		Value: aws.String("p-example"),
	}

	namespace := "AWS/ELB"
	m := types.Metric{
		MetricName: aws.String("Latency"),
		Dimensions: []types.Dimension{d},
		Namespace:  aws.String(namespace),
	}

	plugin := &CloudWatch{
		Namespaces: []string{namespace},
		Delay:      config.Duration(1 * time.Minute),
		Period:     config.Duration(1 * time.Minute),
		BatchSize:  500,
		Log:        testutil.Logger{},
	}
	require.NoError(t, plugin.Init())

	now := time.Now()

	plugin.updateWindow(now)

	statFilter, err := filter.NewIncludeExcludeFilter([]string{"average", "sample_count"}, nil)
	require.NoError(t, err)
	queries := plugin.getDataQueries([]filteredMetric{{metrics: []types.Metric{m}, statFilter: statFilter}})
	params := &cloudwatch.GetMetricDataInput{
		StartTime:         aws.Time(plugin.windowStart),
		EndTime:           aws.Time(plugin.windowEnd),
		MetricDataQueries: queries[namespace],
	}

	require.EqualValues(t, *params.EndTime, now.Add(-time.Duration(plugin.Delay)))
	require.EqualValues(t, *params.StartTime, now.Add(-time.Duration(plugin.Period)).Add(-time.Duration(plugin.Delay)))
	require.Len(t, params.MetricDataQueries, 2)
	require.Len(t, params.MetricDataQueries[0].MetricStat.Metric.Dimensions, 1)
	require.EqualValues(t, 60, *params.MetricDataQueries[0].MetricStat.Period)
}

func TestMetricsCacheTimeout(t *testing.T) {
	cache := &metricCache{
		metrics: make([]filteredMetric, 0),
		built:   time.Now(),
		ttl:     time.Minute,
	}

	require.True(t, cache.metrics != nil && time.Since(cache.built) < cache.ttl)
	cache.built = time.Now().Add(-time.Minute)
	require.False(t, cache.metrics != nil && time.Since(cache.built) < cache.ttl)
}

func TestUpdateWindow(t *testing.T) {
	plugin := &CloudWatch{
		Namespace: "AWS/ELB",
		Delay:     config.Duration(1 * time.Minute),
		Period:    config.Duration(1 * time.Minute),
		BatchSize: 500,
		Log:       testutil.Logger{},
	}

	now := time.Now()

	require.True(t, plugin.windowEnd.IsZero())
	require.True(t, plugin.windowStart.IsZero())

	plugin.updateWindow(now)

	newStartTime := plugin.windowEnd

	// initial window just has a single period
	require.EqualValues(t, plugin.windowEnd, now.Add(-time.Duration(plugin.Delay)))
	require.EqualValues(t, plugin.windowStart, now.Add(-time.Duration(plugin.Delay)).Add(-time.Duration(plugin.Period)))

	now = time.Now()
	plugin.updateWindow(now)

	// subsequent window uses previous end time as start time
	require.EqualValues(t, plugin.windowEnd, now.Add(-time.Duration(plugin.Delay)))
	require.EqualValues(t, plugin.windowStart, newStartTime)
}

func TestProxyFunction(t *testing.T) {
	proxyCfg := proxy.HTTPProxy{HTTPProxyURL: "http://www.penguins.com"}

	proxyFunction, err := proxyCfg.Proxy()
	require.NoError(t, err)

	u, err := url.Parse("https://monitoring.us-west-1.amazonaws.com/")
	require.NoError(t, err)

	proxyResult, err := proxyFunction(&http.Request{URL: u})
	require.NoError(t, err)
	require.Equal(t, "www.penguins.com", proxyResult.Host)
}

func TestCombineNamespaces(t *testing.T) {
	plugin := &CloudWatch{
		Namespace:  "AWS/ELB",
		Namespaces: []string{"AWS/EC2", "AWS/Billing"},
		BatchSize:  500,
		Log:        testutil.Logger{},
	}

	require.NoError(t, plugin.Init())
	require.Equal(t, []string{"AWS/EC2", "AWS/Billing", "AWS/ELB"}, plugin.Namespaces)
}

// INTERNAL mock client implementation
type mockClient struct {
	metrics []types.Metric
}

func defaultMockClient(namespaces ...string) *mockClient {
	c := &mockClient{
		metrics: make([]types.Metric, 0, len(namespaces)),
	}

	for _, namespace := range namespaces {
		c.metrics = append(c.metrics,
			types.Metric{
				Namespace:  aws.String(namespace),
				MetricName: aws.String("Latency"),
				Dimensions: []types.Dimension{
					{
						Name:  aws.String("LoadBalancerName"),
						Value: aws.String("p-example1"),
					},
				},
			},
			types.Metric{
				Namespace:  aws.String(namespace),
				MetricName: aws.String("Latency"),
				Dimensions: []types.Dimension{
					{
						Name:  aws.String("LoadBalancerName"),
						Value: aws.String("p-example2"),
					},
				},
			})
	}
	return c
}

func selectedMockClient() *mockClient {
	c := &mockClient{
		metrics: make([]types.Metric, 0, 4*3*2),
	}
	// 4 metrics for 3 ELBs  in 2 AZs
	for _, m := range []string{"Latency", "RequestCount", "HealthyHostCount", "UnHealthyHostCount"} {
		for _, lb := range []string{"lb-1", "lb-2", "lb-3"} {
			// For each metric/ELB pair, we get an aggregate value across all AZs.
			c.metrics = append(c.metrics, types.Metric{
				Namespace:  aws.String("AWS/ELB"),
				MetricName: aws.String(m),
				Dimensions: []types.Dimension{
					{
						Name:  aws.String("LoadBalancerName"),
						Value: aws.String(lb),
					},
				},
			})
			for _, az := range []string{"us-east-1a", "us-east-1b"} {
				// We get a metric for each metric/ELB/AZ triplet.
				c.metrics = append(c.metrics, types.Metric{
					Namespace:  aws.String("AWS/ELB"),
					MetricName: aws.String(m),
					Dimensions: []types.Dimension{
						{
							Name:  aws.String("LoadBalancerName"),
							Value: aws.String(lb),
						},
						{
							Name:  aws.String("AvailabilityZone"),
							Value: aws.String(az),
						},
					},
				})
			}
		}
	}

	return c
}

func (c *mockClient) ListMetrics(
	_ context.Context,
	params *cloudwatch.ListMetricsInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.ListMetricsOutput, error) {
	response := &cloudwatch.ListMetricsOutput{
		Metrics: c.metrics,
	}

	if params.IncludeLinkedAccounts != nil && *params.IncludeLinkedAccounts {
		response.OwningAccounts = []string{"123456789012", "923456789017"}
	}
	return response, nil
}

func (*mockClient) GetMetricData(
	_ context.Context,
	params *cloudwatch.GetMetricDataInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.GetMetricDataOutput, error) {
	return &cloudwatch.GetMetricDataOutput{
		MetricDataResults: []types.MetricDataResult{
			{
				Id:         aws.String("minimum_0_0"),
				Label:      aws.String("latency_minimum"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{0.1},
			},
			{
				Id:         aws.String("maximum_0_0"),
				Label:      aws.String("latency_maximum"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{0.3},
			},
			{
				Id:         aws.String("average_0_0"),
				Label:      aws.String("latency_average"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{0.2},
			},
			{
				Id:         aws.String("sum_0_0"),
				Label:      aws.String("latency_sum"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{123},
			},
			{
				Id:         aws.String("sample_count_0_0"),
				Label:      aws.String("latency_sample_count"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{100},
			},
			{
				Id:         aws.String("minimum_1_0"),
				Label:      aws.String("latency_minimum"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{0.1},
			},
			{
				Id:         aws.String("maximum_1_0"),
				Label:      aws.String("latency_maximum"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{0.3},
			},
			{
				Id:         aws.String("average_1_0"),
				Label:      aws.String("latency_average"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{0.2},
			},
			{
				Id:         aws.String("sum_1_0"),
				Label:      aws.String("latency_sum"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{124},
			},
			{
				Id:         aws.String("sample_count_1_0"),
				Label:      aws.String("latency_sample_count"),
				StatusCode: types.StatusCodeComplete,
				Timestamps: []time.Time{
					*params.EndTime,
				},
				Values: []float64{100},
			},
		},
	}, nil
}
