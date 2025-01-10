//go:generate ../../../tools/readme_config_includer/generator
package elasticsearch_query

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	elastic5 "gopkg.in/olivere/elastic.v5"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type ElasticsearchQuery struct {
	URLs                []string        `toml:"urls"`
	Username            string          `toml:"username"`
	Password            string          `toml:"password"`
	EnableSniffer       bool            `toml:"enable_sniffer"`
	HealthCheckInterval config.Duration `toml:"health_check_interval"`
	Aggregations        []esAggregation `toml:"aggregation"`

	Log telegraf.Logger `toml:"-"`

	httpclient *http.Client
	common_http.HTTPClientConfig

	esClient *elastic5.Client
}

type esAggregation struct {
	Index                string          `toml:"index"`
	MeasurementName      string          `toml:"measurement_name"`
	DateField            string          `toml:"date_field"`
	DateFieldFormat      string          `toml:"date_field_custom_format"`
	QueryPeriod          config.Duration `toml:"query_period"`
	FilterQuery          string          `toml:"filter_query"`
	MetricFields         []string        `toml:"metric_fields"`
	MetricFunction       string          `toml:"metric_function"`
	Tags                 []string        `toml:"tags"`
	IncludeMissingTag    bool            `toml:"include_missing_tag"`
	MissingTagValue      string          `toml:"missing_tag_value"`
	mapMetricFields      map[string]string
	aggregationQueryList []aggregationQueryData
}

func (*ElasticsearchQuery) SampleConfig() string {
	return sampleConfig
}

func (e *ElasticsearchQuery) Init() error {
	if e.URLs == nil {
		return errors.New("elasticsearch urls is not defined")
	}

	err := e.connectToES()
	if err != nil {
		e.Log.Errorf("error connecting to elasticsearch: %s", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	for i, agg := range e.Aggregations {
		if agg.MeasurementName == "" {
			return errors.New("field 'measurement_name' is not set")
		}
		if agg.DateField == "" {
			return errors.New("field 'date_field' is not set")
		}
		err = e.initAggregation(ctx, agg, i)
		if err != nil {
			e.Log.Error(err.Error())
			return nil
		}
	}
	return nil
}

func (*ElasticsearchQuery) Start(telegraf.Accumulator) error {
	return nil
}

// Gather writes the results of the queries from Elasticsearch to the Accumulator.
func (e *ElasticsearchQuery) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	err := e.connectToES()
	if err != nil {
		return err
	}

	for i, agg := range e.Aggregations {
		wg.Add(1)
		go func(agg esAggregation, i int) {
			defer wg.Done()
			err := e.esAggregationQuery(acc, agg, i)
			if err != nil {
				acc.AddError(fmt.Errorf("elasticsearch query aggregation %s: %w", agg.MeasurementName, err))
			}
		}(agg, i)
	}

	wg.Wait()
	return nil
}

func (e *ElasticsearchQuery) Stop() {
	if e.httpclient != nil {
		e.httpclient.CloseIdleConnections()
	}
}

func (e *ElasticsearchQuery) initAggregation(ctx context.Context, agg esAggregation, i int) (err error) {
	// retrieve field mapping and build queries only once
	agg.mapMetricFields, err = e.getMetricFields(ctx, agg)
	if err != nil {
		return fmt.Errorf("not possible to retrieve fields: %w", err)
	}

	for _, metricField := range agg.MetricFields {
		if _, ok := agg.mapMetricFields[metricField]; !ok {
			return fmt.Errorf("metric field %q not found on index %q", metricField, agg.Index)
		}
	}

	err = agg.buildAggregationQuery()
	if err != nil {
		return err
	}

	e.Aggregations[i] = agg
	return nil
}

func (e *ElasticsearchQuery) connectToES() error {
	var clientOptions []elastic5.ClientOptionFunc

	if e.esClient != nil {
		if e.esClient.IsRunning() {
			return nil
		}
	}

	if e.httpclient == nil {
		httpclient, err := e.createHTTPClient()
		if err != nil {
			return err
		}
		e.httpclient = httpclient
	}

	clientOptions = append(clientOptions,
		elastic5.SetHttpClient(e.httpclient),
		elastic5.SetSniff(e.EnableSniffer),
		elastic5.SetURL(e.URLs...),
		elastic5.SetHealthcheckInterval(time.Duration(e.HealthCheckInterval)),
	)

	if e.Username != "" {
		clientOptions = append(clientOptions, elastic5.SetBasicAuth(e.Username, e.Password))
	}

	if time.Duration(e.HealthCheckInterval) == 0 {
		clientOptions = append(clientOptions, elastic5.SetHealthcheck(false))
	}

	client, err := elastic5.NewClient(clientOptions...)
	if err != nil {
		return err
	}

	// check for ES version on first node
	esVersion, err := client.ElasticsearchVersion(e.URLs[0])
	if err != nil {
		return fmt.Errorf("elasticsearch version check failed: %w", err)
	}

	esVersionSplit := strings.Split(esVersion, ".")

	// quit if ES version is not supported
	if len(esVersionSplit) == 0 {
		return errors.New("elasticsearch version check failed")
	}

	i, err := strconv.Atoi(esVersionSplit[0])
	if err != nil || i < 5 || i > 6 {
		return fmt.Errorf("elasticsearch version %s not supported (currently supported versions are 5.x and 6.x)", esVersion)
	}

	e.esClient = client
	return nil
}

func (e *ElasticsearchQuery) createHTTPClient() (*http.Client, error) {
	ctx := context.Background()
	return e.HTTPClientConfig.CreateClient(ctx, e.Log)
}

func (e *ElasticsearchQuery) esAggregationQuery(acc telegraf.Accumulator, aggregation esAggregation, i int) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.Timeout))
	defer cancel()

	// try to init the aggregation query if it is not done already
	if aggregation.aggregationQueryList == nil {
		err := e.initAggregation(ctx, aggregation, i)
		if err != nil {
			return err
		}
		aggregation = e.Aggregations[i]
	}

	searchResult, err := e.runAggregationQuery(ctx, aggregation)
	if err != nil {
		return err
	}

	if searchResult.Aggregations == nil {
		parseSimpleResult(acc, aggregation.MeasurementName, searchResult)
		return nil
	}

	return parseAggregationResult(acc, aggregation.aggregationQueryList, searchResult)
}

func init() {
	inputs.Add("elasticsearch_query", func() telegraf.Input {
		return &ElasticsearchQuery{
			HealthCheckInterval: config.Duration(time.Second * 10),
			HTTPClientConfig: common_http.HTTPClientConfig{
				ResponseHeaderTimeout: config.Duration(5 * time.Second),
				Timeout:               config.Duration(5 * time.Second),
			},
		}
	})
}
