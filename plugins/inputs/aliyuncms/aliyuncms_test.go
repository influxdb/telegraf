package aliyuncms

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials/providers"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/responses"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cms"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/testutil"
)

const inputTitle = "inputs.aliyuncms"

type mockGatherAliyunCMSClient struct{}

func (*mockGatherAliyunCMSClient) DescribeMetricList(request *cms.DescribeMetricListRequest) (*cms.DescribeMetricListResponse, error) {
	resp := new(cms.DescribeMetricListResponse)

	// switch request.Metric {
	switch request.MetricName {
	case "InstanceActiveConnection":
		resp.Code = "200"
		resp.Period = "60"
		resp.Datapoints = `
		[{
			"timestamp": 1490152860000,
			"Maximum": 200,
			"userId": "1234567898765432",
			"Minimum": 100,
			"instanceId": "i-abcdefgh123456",
			"Average": 150,
			"Value": 300
		}]`
	case "ErrorCode":
		resp.Code = "404"
		resp.Message = "ErrorCode"
	case "ErrorDatapoint":
		resp.Code = "200"
		resp.Period = "60"
		resp.Datapoints = `
		[{
			"timestamp": 1490152860000,
			"Maximum": 200,
			"userId": "1234567898765432",
			"Minimum": 100,
			"instanceId": "i-abcdefgh123456",
			"Average": 150,
		}]`
	case "EmptyDatapoint":
		resp.Code = "200"
		resp.Period = "60"
		resp.Datapoints = `[]`
	case "ErrorResp":
		return nil, errors.New("error response")
	}
	return resp, nil
}

type mockAliyunSDKCli struct {
	resp *responses.CommonResponse
}

func (m *mockAliyunSDKCli) ProcessCommonRequest(_ *requests.CommonRequest) (response *responses.CommonResponse, err error) {
	return m.resp, nil
}

func getDiscoveryTool(project string, discoverRegions []string) (*discoveryTool, error) {
	var (
		err        error
		credential auth.Credential
	)

	configuration := &providers.Configuration{
		AccessKeyID:     "dummyKey",
		AccessKeySecret: "dummySecret",
	}
	credentialProviders := []providers.Provider{
		providers.NewConfigurationCredentialProvider(configuration),
		providers.NewEnvCredentialProvider(),
		providers.NewInstanceMetadataProvider(),
	}
	credential, err = providers.NewChainProvider(credentialProviders).Retrieve()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve credential: %w", err)
	}

	dt, err := newDiscoveryTool(discoverRegions, project, testutil.Logger{Name: inputTitle}, credential, 1, time.Minute*2)

	if err != nil {
		return nil, fmt.Errorf("can't create discovery tool object: %w", err)
	}
	return dt, nil
}

func getMockSdkCli(httpResp *http.Response) (mockAliyunSDKCli, error) {
	resp := responses.NewCommonResponse()
	if err := responses.Unmarshal(resp, httpResp, "JSON"); err != nil {
		return mockAliyunSDKCli{}, fmt.Errorf("can't parse response: %w", err)
	}
	return mockAliyunSDKCli{resp: resp}, nil
}

func TestPluginDefaults(t *testing.T) {
	require.Equal(t, &AliyunCMS{RateLimit: 200,
		DiscoveryInterval: config.Duration(time.Minute),
		dimensionKey:      "instanceId",
	}, inputs.Inputs["aliyuncms"]())
}

func TestPluginInitialize(t *testing.T) {
	var err error

	plugin := new(AliyunCMS)
	plugin.Log = testutil.Logger{Name: inputTitle}
	plugin.Regions = []string{"cn-shanghai"}
	plugin.dt, err = getDiscoveryTool("acs_slb_dashboard", plugin.Regions)
	if err != nil {
		t.Fatalf("Can't create discovery tool object: %v", err)
	}

	httpResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(bytes.NewBufferString(
			`{
						"LoadBalancers":
						 {
						  "LoadBalancer": [
 							 {"LoadBalancerId":"bla"}
                           ]
                         },
						"TotalCount": 1,
						"PageSize": 1,
						"PageNumber": 1
						}`)),
	}
	mockCli, err := getMockSdkCli(httpResp)
	if err != nil {
		t.Fatalf("Can't create mock sdk cli: %v", err)
	}
	plugin.dt.cli = map[string]aliyunSdkClient{plugin.Regions[0]: &mockCli}

	tests := []struct {
		name                string
		project             string
		accessKeyID         string
		accessKeySecret     string
		expectedErrorString string
		regions             []string
		discoveryRegions    []string
	}{
		{
			name:                "Empty project",
			expectedErrorString: "project is not set",
			regions:             []string{"cn-shanghai"},
		},
		{
			name:            "Valid project",
			project:         "acs_slb_dashboard",
			regions:         []string{"cn-shanghai"},
			accessKeyID:     "dummy",
			accessKeySecret: "dummy",
		},
		{
			name:            "'regions' is not set",
			project:         "acs_slb_dashboard",
			accessKeyID:     "dummy",
			accessKeySecret: "dummy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin.Project = tt.project
			plugin.AccessKeyID = tt.accessKeyID
			plugin.AccessKeySecret = tt.accessKeySecret
			plugin.Regions = tt.regions

			if tt.expectedErrorString != "" {
				require.EqualError(t, plugin.Init(), tt.expectedErrorString)
			} else {
				require.NoError(t, plugin.Init())
			}
			if len(tt.regions) == 0 { // Check if set to default
				require.Equal(t, plugin.Regions, aliyunRegionList)
			}
		})
	}
}

func TestPluginMetricsInitialize(t *testing.T) {
	var err error

	plugin := new(AliyunCMS)
	plugin.Log = testutil.Logger{Name: inputTitle}
	plugin.Regions = []string{"cn-shanghai"}
	plugin.dt, err = getDiscoveryTool("acs_slb_dashboard", plugin.Regions)
	if err != nil {
		t.Fatalf("Can't create discovery tool object: %v", err)
	}

	httpResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(bytes.NewBufferString(
			`{
				"LoadBalancers":
					{
						"LoadBalancer": [
 							{"LoadBalancerId":"bla"}
                        ]
                    },
				"TotalCount": 1,
				"PageSize": 1,
				"PageNumber": 1
			}`)),
	}
	mockCli, err := getMockSdkCli(httpResp)
	if err != nil {
		t.Fatalf("Can't create mock sdk cli: %v", err)
	}
	plugin.dt.cli = map[string]aliyunSdkClient{plugin.Regions[0]: &mockCli}

	tests := []struct {
		name                string
		project             string
		accessKeyID         string
		accessKeySecret     string
		expectedErrorString string
		regions             []string
		discoveryRegions    []string
		metrics             []*metric
	}{
		{
			name:            "Valid project",
			project:         "acs_slb_dashboard",
			regions:         []string{"cn-shanghai"},
			accessKeyID:     "dummy",
			accessKeySecret: "dummy",
			metrics: []*metric{
				{
					Dimensions: `{"instanceId": "i-abcdefgh123456"}`,
				},
			},
		},
		{
			name:            "Valid project",
			project:         "acs_slb_dashboard",
			regions:         []string{"cn-shanghai"},
			accessKeyID:     "dummy",
			accessKeySecret: "dummy",
			metrics: []*metric{
				{
					Dimensions: `[{"instanceId": "p-example"},{"instanceId": "q-example"}]`,
				},
			},
		},
		{
			name:                "Valid project",
			project:             "acs_slb_dashboard",
			regions:             []string{"cn-shanghai"},
			accessKeyID:         "dummy",
			accessKeySecret:     "dummy",
			expectedErrorString: `cannot parse dimensions (neither obj, nor array) "[": unexpected end of JSON input`,
			metrics: []*metric{
				{
					Dimensions: `[`,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin.Project = tt.project
			plugin.AccessKeyID = tt.accessKeyID
			plugin.AccessKeySecret = tt.accessKeySecret
			plugin.Regions = tt.regions
			plugin.Metrics = tt.metrics

			if tt.expectedErrorString != "" {
				require.EqualError(t, plugin.Init(), tt.expectedErrorString)
			} else {
				require.NoError(t, plugin.Init())
			}
		})
	}
}

func TestUpdateWindow(t *testing.T) {
	duration, err := time.ParseDuration("1m")
	require.NoError(t, err)
	internalDuration := config.Duration(duration)

	plugin := &AliyunCMS{
		Project: "acs_slb_dashboard",
		Period:  internalDuration,
		Delay:   internalDuration,
		Log:     testutil.Logger{Name: inputTitle},
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

func TestGatherMetric(t *testing.T) {
	plugin := &AliyunCMS{
		Project:     "acs_slb_dashboard",
		client:      new(mockGatherAliyunCMSClient),
		measurement: formatMeasurement("acs_slb_dashboard"),
		Log:         testutil.Logger{Name: inputTitle},
		Regions:     []string{"cn-shanghai"},
	}

	metric := &metric{
		Dimensions: `"instanceId": "i-abcdefgh123456"`,
	}

	tests := []struct {
		name                string
		metricName          string
		expectedErrorString string
	}{
		{
			name:                "Datapoint with corrupted JSON",
			metricName:          "ErrorDatapoint",
			expectedErrorString: `failed to decode response datapoints: invalid character '}' looking for beginning of object key string`,
		},
		{
			name:                "General CMS response error",
			metricName:          "ErrorResp",
			expectedErrorString: "failed to query metricName list: error response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc telegraf.Accumulator
			require.EqualError(t, plugin.gatherMetric(acc, tt.metricName, metric), tt.expectedErrorString)
		})
	}
}

func TestGather(t *testing.T) {
	m := &metric{
		Dimensions: `{"instanceId": "i-abcdefgh123456"}`,
	}
	plugin := &AliyunCMS{
		AccessKeyID:     "my_access_key_id",
		AccessKeySecret: "my_access_key_secret",
		Project:         "acs_slb_dashboard",
		Metrics:         []*metric{m},
		RateLimit:       200,
		measurement:     formatMeasurement("acs_slb_dashboard"),
		Regions:         []string{"cn-shanghai"},
		client:          new(mockGatherAliyunCMSClient),
		Log:             testutil.Logger{Name: inputTitle},
	}

	// test table:
	tests := []struct {
		name           string
		hasMeasurement bool
		metricNames    []string
		expected       []telegraf.Metric
	}{
		{
			name:        "Empty data point",
			metricNames: []string{"EmptyDatapoint"},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"aliyuncms_acs_slb_dashboard",
					nil,
					nil,
					time.Time{}),
			},
		},
		{
			name:           "Data point with fields & tags",
			hasMeasurement: true,
			metricNames:    []string{"InstanceActiveConnection"},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"aliyuncms_acs_slb_dashboard",
					map[string]string{
						"instanceId": "i-abcdefgh123456",
						"userId":     "1234567898765432",
					},
					map[string]interface{}{
						"instance_active_connection_minimum": float64(100),
						"instance_active_connection_maximum": float64(200),
						"instance_active_connection_average": float64(150),
						"instance_active_connection_value":   float64(300),
					},
					time.Unix(1490152860000, 0)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			plugin.Metrics[0].MetricNames = tt.metricNames
			require.NoError(t, acc.GatherError(plugin.Gather))
			require.Equal(t, acc.HasMeasurement("aliyuncms_acs_slb_dashboard"), tt.hasMeasurement)
			if tt.hasMeasurement {
				acc.AssertContainsTaggedFields(t, "aliyuncms_acs_slb_dashboard", tt.expected[0].Fields(), tt.expected[0].Tags())
			}
		})
	}
}

func TestGetDiscoveryDataAcrossRegions(t *testing.T) {
	// test table:
	tests := []struct {
		name                string
		project             string
		region              string
		httpResp            *http.Response
		discData            map[string]interface{}
		totalCount          int
		pageSize            int
		pageNumber          int
		expectedErrorString string
	}{
		{
			name:    "No root key in discovery response",
			project: "acs_slb_dashboard",
			region:  "cn-hongkong",
			httpResp: &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
			},
			totalCount:          0,
			pageSize:            0,
			pageNumber:          0,
			expectedErrorString: `didn't find root key "LoadBalancers" in discovery response`,
		},
		{
			name:    "1 object discovered",
			project: "acs_slb_dashboard",
			region:  "cn-hongkong",
			httpResp: &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(bytes.NewBufferString(
					`{
						"LoadBalancers":
						 {
						  "LoadBalancer": [
 							 {"LoadBalancerId":"bla"}
                           ]
                         },
						"TotalCount": 1,
						"PageSize": 1,
						"PageNumber": 1
						}`)),
			},
			discData:            map[string]interface{}{"bla": map[string]interface{}{"LoadBalancerId": "bla"}},
			totalCount:          1,
			pageSize:            1,
			pageNumber:          1,
			expectedErrorString: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt, err := getDiscoveryTool(tt.project, []string{tt.region})
			if err != nil {
				t.Fatalf("Can't create discovery tool object: %v", err)
			}

			mockCli, err := getMockSdkCli(tt.httpResp)
			if err != nil {
				t.Fatalf("Can't create mock sdk cli: %v", err)
			}
			dt.cli = map[string]aliyunSdkClient{tt.region: &mockCli}
			data, err := dt.getDiscoveryDataAcrossRegions(nil)

			require.Equal(t, tt.discData, data)
			if err != nil {
				require.EqualError(t, err, tt.expectedErrorString)
			}
		})
	}
}
