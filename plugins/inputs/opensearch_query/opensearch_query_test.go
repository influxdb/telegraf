package opensearch_query

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/opensearch-project/opensearch-go/v2/opensearchutil"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/testutil"
)

const (
	servicePort = "9200"
	testindex   = "test-opensearch"
)

type osAggregationQueryTest struct {
	queryName                 string
	testAggregationQueryInput osAggregation
	expectedMetrics           []telegraf.Metric
	wantQueryResErr           bool
	wantInitErr               bool
}

var queryPeriod = config.Duration(time.Second * 600)

func testData() []osAggregationQueryTest {
	return []osAggregationQueryTest{
		{
			queryName: "query 1 (avg)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement1",
				MetricFields:    []string{"size"},
				FilterQuery:     "product_1",
				MetricFunction:  "avg",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"URI.keyword"},
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement1",
					map[string]string{"URI_keyword": "/downloads/product_1"},
					map[string]interface{}{"size_avg_value": float64(202.30038022813687), "doc_count": int64(263)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 2 (avg)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement2",
				MetricFields:    []string{"size"},
				FilterQuery:     "downloads",
				MetricFunction:  "max",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"URI.keyword"},
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement2",
					map[string]string{"URI_keyword": "/downloads/product_1"},
					map[string]interface{}{"size_max_value": float64(3301), "doc_count": int64(263)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement2",
					map[string]string{"URI_keyword": "/downloads/product_2"},
					map[string]interface{}{"size_max_value": float64(3318), "doc_count": int64(237)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 3 (sum)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement3",
				MetricFields:    []string{"size"},
				FilterQuery:     "downloads",
				MetricFunction:  "sum",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"response.keyword"},
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement3",
					map[string]string{"response_keyword": "200"},
					map[string]interface{}{"size_sum_value": float64(22790), "doc_count": int64(22)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement3",
					map[string]string{"response_keyword": "304"},
					map[string]interface{}{"size_sum_value": float64(0), "doc_count": int64(219)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement3",
					map[string]string{"response_keyword": "404"},
					map[string]interface{}{"size_sum_value": float64(86932), "doc_count": int64(259)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 4 (min, 2 fields, 3 tags)",
			testAggregationQueryInput: osAggregation{
				Index:             testindex,
				MeasurementName:   "measurement4",
				MetricFields:      []string{"size", "response_time"},
				FilterQuery:       "downloads",
				MetricFunction:    "min",
				DateField:         "@timestamp",
				QueryPeriod:       queryPeriod,
				IncludeMissingTag: true,
				MissingTagValue:   "missing",
				Tags:              []string{"response.keyword", "URI.keyword", "method.keyword"},
				mapMetricFields:   map[string]string{"size": "long", "response_time": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "404", "URI_keyword": "/downloads/product_1", "method_keyword": "GET"},
					map[string]interface{}{"size_min_value": float64(318), "response_time_min_value": float64(126), "doc_count": int64(146)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "304", "URI_keyword": "/downloads/product_1", "method_keyword": "GET"},
					map[string]interface{}{"size_min_value": float64(0), "response_time_min_value": float64(71), "doc_count": int64(113)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "200", "URI_keyword": "/downloads/product_1", "method_keyword": "GET"},
					map[string]interface{}{"size_min_value": float64(490), "response_time_min_value": float64(1514), "doc_count": int64(3)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "404", "URI_keyword": "/downloads/product_2", "method_keyword": "GET"},
					map[string]interface{}{"size_min_value": float64(318), "response_time_min_value": float64(237), "doc_count": int64(113)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "304", "URI_keyword": "/downloads/product_2", "method_keyword": "GET"},
					map[string]interface{}{"size_min_value": float64(0), "response_time_min_value": float64(134), "doc_count": int64(106)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "200", "URI_keyword": "/downloads/product_2", "method_keyword": "GET"},
					map[string]interface{}{"size_min_value": float64(490), "response_time_min_value": float64(2), "doc_count": int64(13)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "200", "URI_keyword": "/downloads/product_1", "method_keyword": "HEAD"},
					map[string]interface{}{"size_min_value": float64(0), "response_time_min_value": float64(8479), "doc_count": int64(1)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement4",
					map[string]string{"response_keyword": "200", "URI_keyword": "/downloads/product_2", "method_keyword": "HEAD"},
					map[string]interface{}{"size_min_value": float64(0), "response_time_min_value": float64(1059), "doc_count": int64(5)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 5 (no fields)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement5",
				FilterQuery:     "product_2",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"URI.keyword"},
				mapMetricFields: map[string]string{},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement5",
					map[string]string{"URI_keyword": "/downloads/product_2"},
					map[string]interface{}{"doc_count": int64(237)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 6 (no fields, to tags)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement6",
				FilterQuery:     "response: 200",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"URI.keyword", "response.keyword"},
				mapMetricFields: map[string]string{},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement6",
					map[string]string{"response_keyword": "200", "URI_keyword": "/downloads/product_1"},
					map[string]interface{}{"doc_count": int64(4)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement6",
					map[string]string{"response_keyword": "200", "URI_keyword": "/downloads/product_2"},
					map[string]interface{}{"doc_count": int64(18)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 7 (simple query)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement7",
				FilterQuery:     "response: 200",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				mapMetricFields: map[string]string{},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement7",
					map[string]string{},
					map[string]interface{}{"doc_count": int64(22)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 8 (max, no tags)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement8",
				MetricFields:    []string{"size"},
				FilterQuery:     "downloads",
				MetricFunction:  "max",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement8",
					map[string]string{},
					map[string]interface{}{"size_max_value": float64(3318), "doc_count": int64(500)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 9 (invalid function)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement9",
				MetricFields:    []string{"size"},
				FilterQuery:     "downloads",
				MetricFunction:  "average",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				mapMetricFields: map[string]string{"size": "long"},
			},
			wantInitErr: true,
		},
		{
			queryName: "query 10 (non-existing metric field)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement10",
				MetricFields:    []string{"none"},
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				mapMetricFields: map[string]string{},
			},
			wantQueryResErr: true,
			wantInitErr:     true,
		},
		{
			queryName: "query 11 (non-existing index field)",
			testAggregationQueryInput: osAggregation{
				Index:           "notanindex",
				MeasurementName: "measurement11",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				mapMetricFields: map[string]string{},
			},
			wantQueryResErr: true,
		},
		{
			queryName: "query 12 (non-existing timestamp field)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement12",
				MetricFields:    []string{"size"},
				MetricFunction:  "avg",
				DateField:       "@notatimestamp",
				QueryPeriod:     queryPeriod,
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement12",
					map[string]string{},
					map[string]interface{}{"doc_count": int64(0)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 13 (non-existing tag field)",
			testAggregationQueryInput: osAggregation{
				Index:             testindex,
				MeasurementName:   "measurement13",
				MetricFields:      []string{"size"},
				MetricFunction:    "avg",
				DateField:         "@timestamp",
				QueryPeriod:       queryPeriod,
				IncludeMissingTag: false,
				Tags:              []string{"nothere"},
				mapMetricFields:   map[string]string{"size": "long"},
			},
		},
		{
			queryName: "query 14 (non-existing custom date/time format)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement14",
				DateField:       "@timestamp",
				DateFieldFormat: "yyyy",
				QueryPeriod:     queryPeriod,
				mapMetricFields: map[string]string{},
			},
			wantQueryResErr: true,
		},
		{
			queryName: "query 15 (stats)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement15",
				MetricFields:    []string{"size"},
				FilterQuery:     "downloads",
				MetricFunction:  "stats",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"URI.keyword"},
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement15",
					map[string]string{"URI_keyword": "/downloads/product_1"},
					map[string]interface{}{
						"size_stats_sum":   float64(53205),
						"size_stats_min":   float64(0),
						"size_stats_max":   float64(3301),
						"size_stats_avg":   float64(202.30038022813687),
						"size_stats_count": float64(263),
						"doc_count":        int64(263)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement15",
					map[string]string{"URI_keyword": "/downloads/product_2"},
					map[string]interface{}{
						"size_stats_sum":   float64(56517),
						"size_stats_min":   float64(0),
						"size_stats_max":   float64(3318),
						"size_stats_avg":   float64(238.46835443037975),
						"size_stats_count": float64(237),
						"doc_count":        int64(237)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 16 (extended_stats)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement16",
				MetricFields:    []string{"size"},
				FilterQuery:     "downloads",
				MetricFunction:  "extended_stats",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"URI.keyword"},
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement16",
					map[string]string{"URI_keyword": "/downloads/product_1"},
					map[string]interface{}{
						"size_extended_stats_avg":                                   float64(202.30038022813687),
						"size_extended_stats_count":                                 float64(263),
						"size_extended_stats_max":                                   float64(3301),
						"size_extended_stats_min":                                   float64(0),
						"size_extended_stats_sum":                                   float64(53205),
						"size_extended_stats_std_deviation":                         float64(254.33673728231705),
						"size_extended_stats_std_deviation_population":              float64(254.33673728231705),
						"size_extended_stats_std_deviation_sampling":                float64(254.8216504723906),
						"size_extended_stats_std_deviation_bounds_upper":            float64(710.9738547927709),
						"size_extended_stats_std_deviation_bounds_lower":            float64(-306.3730943364972),
						"size_extended_stats_std_deviation_bounds_upper_population": float64(710.9738547927709),
						"size_extended_stats_std_deviation_bounds_lower_population": float64(-306.3730943364972),
						"size_extended_stats_std_deviation_bounds_upper_sampling":   float64(711.9436811729181),
						"size_extended_stats_std_deviation_bounds_lower_sampling":   float64(-307.3429207166443),
						"size_extended_stats_variance":                              float64(64687.17593141436),
						"size_extended_stats_variance_sampling":                     float64(64934.07354947319),
						"size_extended_stats_variance_population":                   float64(64687.17593141436),
						"size_extended_stats_sum_of_squares":                        float64(27776119),
						"doc_count":                                                 int64(263)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement16",
					map[string]string{"URI_keyword": "/downloads/product_2"},
					map[string]interface{}{
						"size_extended_stats_avg":                                   float64(238.46835443037975),
						"size_extended_stats_count":                                 float64(237),
						"size_extended_stats_max":                                   float64(3318),
						"size_extended_stats_min":                                   float64(0),
						"size_extended_stats_sum":                                   float64(56517),
						"size_extended_stats_std_deviation":                         float64(411.39122310768215),
						"size_extended_stats_std_deviation_population":              float64(411.39122310768215),
						"size_extended_stats_std_deviation_sampling":                float64(412.2618933368743),
						"size_extended_stats_std_deviation_bounds_upper":            float64(1061.250800645744),
						"size_extended_stats_std_deviation_bounds_lower":            float64(-584.3140917849846),
						"size_extended_stats_std_deviation_bounds_upper_population": float64(1061.250800645744),
						"size_extended_stats_std_deviation_bounds_lower_population": float64(-584.3140917849846),
						"size_extended_stats_std_deviation_bounds_upper_sampling":   float64(1062.9921411041285),
						"size_extended_stats_std_deviation_bounds_lower_sampling":   float64(-586.0554322433688),
						"size_extended_stats_variance":                              float64(169242.7384500347),
						"size_extended_stats_variance_sampling":                     float64(169959.86869770434),
						"size_extended_stats_variance_population":                   float64(169242.7384500347),
						"size_extended_stats_sum_of_squares":                        float64(53588045),
						"doc_count":                                                 int64(237)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			queryName: "query 17 (percentiles)",
			testAggregationQueryInput: osAggregation{
				Index:           testindex,
				MeasurementName: "measurement16",
				MetricFields:    []string{"size"},
				FilterQuery:     "downloads",
				MetricFunction:  "percentiles",
				DateField:       "@timestamp",
				QueryPeriod:     queryPeriod,
				Tags:            []string{"URI.keyword"},
				mapMetricFields: map[string]string{"size": "long"},
			},
			expectedMetrics: []telegraf.Metric{
				testutil.MustMetric(
					"measurement16",
					map[string]string{"URI_keyword": "/downloads/product_1"},
					map[string]interface{}{
						"size_percentiles_values_1.0":  float64(0),
						"size_percentiles_values_5.0":  float64(0),
						"size_percentiles_values_25.0": float64(0),
						"size_percentiles_values_50.0": float64(324),
						"size_percentiles_values_75.0": float64(337),
						"size_percentiles_values_95.0": float64(341),
						"size_percentiles_values_99.0": float64(471.28000000000065),
						"doc_count":                    int64(263)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
				testutil.MustMetric(
					"measurement16",
					map[string]string{"URI_keyword": "/downloads/product_2"},
					map[string]interface{}{
						"size_percentiles_values_1.0":  float64(0),
						"size_percentiles_values_5.0":  float64(0),
						"size_percentiles_values_25.0": float64(0),
						"size_percentiles_values_50.0": float64(324),
						"size_percentiles_values_75.0": float64(339),
						"size_percentiles_values_95.0": float64(490),
						"size_percentiles_values_99.0": float64(2677.419999999997),
						"doc_count":                    int64(237)},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
	}
}

func opensearchTestImages() []string {
	return []string{"opensearchproject/opensearch:2.5.0", "opensearchproject/opensearch:1.3.7"}
}

func newOpensearchQuery(url string) *OpensearchQuery {
	return &OpensearchQuery{
		URLs:         []string{url},
		Timeout:      config.Duration(time.Second * 30),
		Log:          testutil.Logger{},
		Username:     config.NewSecret([]byte("admin")),
		Password:     config.NewSecret([]byte("admin")),
		ClientConfig: tls.ClientConfig{InsecureSkipVerify: true},
	}
}

func setupIntegrationTest(t *testing.T, image string) (*testutil.Container, *OpensearchQuery, error) {
	var err error

	type nginxlog struct {
		IPaddress    string    `json:"IP"`
		Timestamp    time.Time `json:"@timestamp"`
		Method       string    `json:"method"`
		URI          string    `json:"URI"`
		Httpversion  string    `json:"http_version"`
		Response     string    `json:"response"`
		Size         float64   `json:"size"`
		ResponseTime float64   `json:"response_time"`
	}

	container := testutil.Container{
		Image:        image,
		ExposedPorts: []string{servicePort},
		Env: map[string]string{
			"discovery.type":                         "single-node",
			"DISABLE_PERFORMANCE_ANALYZER_AGENT_CLI": "true",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog(".opendistro_security is used as internal security index."),
			wait.ForListeningPort(nat.Port(servicePort)),
		),
	}
	err = container.Start()
	require.NoError(t, err, "failed to start container")

	url := fmt.Sprintf("https://%s:%s", container.Address, container.Ports[servicePort])

	o := newOpensearchQuery(url)

	err = o.newClient()
	if err != nil {
		return &container, o, err
	}

	// parse and build query
	file, err := os.Open("testdata/nginx_logs")
	if err != nil {
		return &container, o, err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	indexer, err := opensearchutil.NewBulkIndexer(opensearchutil.BulkIndexerConfig{
		Client:  o.osClient,
		Index:   testindex,
		Refresh: "true",
	})
	if err != nil {
		return &container, o, err
	}

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), " ")
		size, err := strconv.Atoi(parts[9])
		require.NoError(t, err)
		responseTime, err := strconv.Atoi(parts[len(parts)-1])
		require.NoError(t, err)

		logline := nginxlog{
			IPaddress:    parts[0],
			Timestamp:    time.Now().UTC(),
			Method:       strings.ReplaceAll(parts[5], `"`, ""),
			URI:          parts[6],
			Httpversion:  strings.ReplaceAll(parts[7], `"`, ""),
			Response:     parts[8],
			Size:         float64(size),
			ResponseTime: float64(responseTime),
		}

		body, e := json.Marshal(logline)
		if e != nil {
			return &container, o, e
		}

		e = indexer.Add(
			context.Background(),
			opensearchutil.BulkIndexerItem{
				Index:  testindex,
				Action: "index",
				Body:   strings.NewReader(string(body)),
			})
		if e != nil {
			return &container, o, e
		}
	}

	if scanner.Err() != nil {
		return &container, o, err
	}

	if err := indexer.Close(context.Background()); err != nil {
		return &container, o, err
	}

	return &container, o, nil
}

func TestOpensearchQueryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	for _, image := range opensearchTestImages() {
		func() {
			container, o, err := setupIntegrationTest(t, image)
			require.NoError(t, err)
			defer container.Terminate()

			for _, tt := range testData() {
				t.Run(tt.queryName, func(t *testing.T) {
					var err error
					var acc testutil.Accumulator

					o.Aggregations = []osAggregation{tt.testAggregationQueryInput}
					err = o.Init()
					if (err != nil) != tt.wantInitErr {
						t.Errorf("OpensearchQuery.Init() error = %v, wantInitErr %v", err, tt.wantInitErr)
						return
					} else if err != nil {
						// Init() failures mean we're done
						return
					}

					err = o.Gather(&acc)
					if (len(acc.Errors) > 0) != tt.wantQueryResErr {
						for _, err = range acc.Errors {
							t.Errorf("OpensearchQuery.Gather() error: %v, wantQueryResErr %v", err, tt.wantQueryResErr)
						}
						return
					}

					require.NoError(t, err)

					testutil.RequireMetricsEqual(t, tt.expectedMetrics, acc.GetTelegrafMetrics(), testutil.SortMetrics(), testutil.IgnoreTime())
				})
			}
		}()
	}
}

func TestMetricAggregationMarshal(t *testing.T) {
	agg := &metricAggregationRequest{}
	err := agg.addAggregation("sum_taxful_total_price", "sum", "taxful_total_price")
	require.NoError(t, err)

	_, err = json.Marshal(agg)
	require.NoError(t, err)

	bucket := &bucketAggregationRequest{}
	err = bucket.addAggregation("terms_by_currency", "terms", "currency")
	require.NoError(t, err)

	bucket.addNestedAggregation("terms_by_currency", agg)
	_, err = json.Marshal(bucket)
	require.NoError(t, err)
}
