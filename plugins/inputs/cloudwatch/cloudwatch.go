//go:generate ../../../tools/readme_config_includer/generator
package cloudwatch

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal/limiter"
	"github.com/influxdata/telegraf/metric"
	common_aws "github.com/influxdata/telegraf/plugins/common/aws"
	"github.com/influxdata/telegraf/plugins/common/proxy"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type CloudWatch struct {
	StatisticExclude []string        `toml:"statistic_exclude"`
	StatisticInclude []string        `toml:"statistic_include"`
	Timeout          config.Duration `toml:"timeout"`

	proxy.HTTPProxy

	Period                config.Duration     `toml:"period"`
	Delay                 config.Duration     `toml:"delay"`
	Namespace             string              `toml:"namespace" deprecated:"1.25.0;1.35.0;use 'namespaces' instead"`
	Namespaces            []string            `toml:"namespaces"`
	Metrics               []*cloudwatchMetric `toml:"metrics"`
	CacheTTL              config.Duration     `toml:"cache_ttl"`
	RateLimit             int                 `toml:"ratelimit"`
	RecentlyActive        string              `toml:"recently_active"`
	BatchSize             int                 `toml:"batch_size"`
	IncludeLinkedAccounts bool                `toml:"include_linked_accounts"`
	MetricFormat          string              `toml:"metric_format"`
	Log                   telegraf.Logger     `toml:"-"`
	common_aws.CredentialConfig

	client          cloudwatchClient
	nsFilter        filter.Filter
	statFilter      filter.Filter
	cache           *metricCache
	queryDimensions map[string]*map[string]string
	windowStart     time.Time
	windowEnd       time.Time
}

type cloudwatchMetric struct {
	MetricNames      []string     `toml:"names"`
	Dimensions       []*dimension `toml:"dimensions"`
	StatisticExclude *[]string    `toml:"statistic_exclude"`
	StatisticInclude *[]string    `toml:"statistic_include"`
}

type dimension struct {
	Name         string `toml:"name"`
	Value        string `toml:"value"`
	valueMatcher filter.Filter
}

type metricCache struct {
	ttl     time.Duration
	built   time.Time
	metrics []filteredMetric
	queries map[string][]types.MetricDataQuery
}

type filteredMetric struct {
	metrics    []types.Metric
	accounts   []string
	statFilter filter.Filter
}

type cloudwatchClient interface {
	ListMetrics(context.Context, *cloudwatch.ListMetricsInput, ...func(*cloudwatch.Options)) (*cloudwatch.ListMetricsOutput, error)
	GetMetricData(context.Context, *cloudwatch.GetMetricDataInput, ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
}

func (*CloudWatch) SampleConfig() string {
	return sampleConfig
}

func (c *CloudWatch) Init() error {
	// For backward compatibility
	if len(c.Namespace) != 0 {
		c.Namespaces = append(c.Namespaces, c.Namespace)
	}

	// Check user settings
	switch c.MetricFormat {
	case "":
		c.MetricFormat = "sparse"
	case "dense", "sparse":
	default:
		return fmt.Errorf("invalid metric_format: %s", c.MetricFormat)
	}

	// Setup the cloudwatch client
	proxyFunc, err := c.HTTPProxy.Proxy()
	if err != nil {
		return fmt.Errorf("creating proxy failed: %w", err)
	}

	creds, err := c.CredentialConfig.Credentials()
	if err != nil {
		return fmt.Errorf("getting credentials failed: %w", err)
	}

	c.client = cloudwatch.NewFromConfig(creds, func(options *cloudwatch.Options) {
		if c.CredentialConfig.EndpointURL != "" && c.CredentialConfig.Region != "" {
			options.BaseEndpoint = &c.CredentialConfig.EndpointURL
		}

		options.ClientLogMode = 0
		options.HTTPClient = &http.Client{
			// use values from DefaultTransport
			Transport: &http.Transport{
				Proxy: proxyFunc,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
			Timeout: time.Duration(c.Timeout),
		}
	})

	// Initialize filter for metric dimensions to include
	for _, m := range c.Metrics {
		for _, dimension := range m.Dimensions {
			matcher, err := filter.NewIncludeExcludeFilter([]string{dimension.Value}, nil)
			if err != nil {
				return fmt.Errorf("creating dimension filter for dimension %q failed: %w", dimension, err)
			}
			dimension.valueMatcher = matcher
		}
	}

	// Initialize statistics-type filter
	c.statFilter, err = filter.NewIncludeExcludeFilter(c.StatisticInclude, c.StatisticExclude)
	if err != nil {
		return fmt.Errorf("creating statistics filter failed: %w", err)
	}

	// Initialize namespace filter
	c.nsFilter, err = filter.Compile(c.Namespaces)
	if err != nil {
		return fmt.Errorf("creating namespace filter failed: %w", err)
	}

	return nil
}

func (c *CloudWatch) Gather(acc telegraf.Accumulator) error {
	filteredMetrics, err := c.getFilteredMetrics()
	if err != nil {
		return err
	}
	c.updateWindow(time.Now())

	// Get all of the possible queries so we can send groups of 100.
	queries := c.getDataQueries(filteredMetrics)
	if len(queries) == 0 {
		return nil
	}

	// Limit concurrency or we can easily exhaust user connection limit.
	// See cloudwatch API request limits:
	// http://docs.aws.amazon.com/AmazonCloudWatch/latest/DeveloperGuide/cloudwatch_limits.html
	lmtr := limiter.NewRateLimiter(c.RateLimit, time.Second)
	defer lmtr.Stop()
	wg := sync.WaitGroup{}
	rLock := sync.Mutex{}

	results := make(map[string][]types.MetricDataResult)
	for namespace, namespacedQueries := range queries {
		var batches [][]types.MetricDataQuery

		for c.BatchSize < len(namespacedQueries) {
			namespacedQueries, batches = namespacedQueries[c.BatchSize:], append(batches, namespacedQueries[0:c.BatchSize:c.BatchSize])
		}
		batches = append(batches, namespacedQueries)

		for i := range batches {
			wg.Add(1)
			<-lmtr.C
			go func(n string, inm []types.MetricDataQuery) {
				defer wg.Done()
				result, err := c.gatherMetrics(inm)
				if err != nil {
					acc.AddError(err)
					return
				}

				rLock.Lock()
				results[n] = append(results[n], result...)
				rLock.Unlock()
			}(namespace, batches[i])
		}
	}

	wg.Wait()
	c.aggregateMetrics(acc, results)
	return nil
}

func (c *CloudWatch) getFilteredMetrics() ([]filteredMetric, error) {
	if c.cache != nil && c.cache.metrics != nil && time.Since(c.cache.built) < c.cache.ttl {
		return c.cache.metrics, nil
	}

	// Get all metrics from cloudwatch for filtering
	params := &cloudwatch.ListMetricsInput{
		IncludeLinkedAccounts: &c.IncludeLinkedAccounts,
	}
	if c.RecentlyActive == "PT3H" {
		params.RecentlyActive = types.RecentlyActivePt3h
	}

	// Return the subset of metrics matching the namespace and at one of the
	// metric definitions if any
	var metrics []types.Metric
	var accounts []string
	for {
		resp, err := c.client.ListMetrics(context.Background(), params)
		if err != nil {
			c.Log.Errorf("failed to list metrics: %v", err)
			break
		}
		c.Log.Tracef("got %d metrics with %d accounts", len(resp.Metrics), len(resp.OwningAccounts))
		for i, m := range resp.Metrics {
			if c.Log.Level().Includes(telegraf.Trace) {
				dims := make([]string, 0, len(m.Dimensions))
				for _, d := range m.Dimensions {
					dims = append(dims, *d.Name+"="+*d.Value)
				}
				a := "none"
				if len(resp.OwningAccounts) > 0 {
					a = resp.OwningAccounts[i]
				}
				c.Log.Tracef("  metric %3d: %s (%s): %s [%s]\n", i, *m.MetricName, *m.Namespace, strings.Join(dims, ", "), a)
			}

			if c.nsFilter != nil && !c.nsFilter.Match(*m.Namespace) {
				c.Log.Trace("  -> rejected by namespace")
				continue
			}

			if len(c.Metrics) > 0 && !slices.ContainsFunc(c.Metrics, func(cm *cloudwatchMetric) bool {
				return metricMatch(cm, m)
			}) {
				c.Log.Trace("  -> rejected by metric mismatch")
				continue
			}
			c.Log.Trace("  -> keeping metric")

			metrics = append(metrics, m)
			if len(resp.OwningAccounts) > 0 {
				accounts = append(accounts, resp.OwningAccounts[i])
			}
		}

		if resp.NextToken == nil {
			break
		}
		params.NextToken = resp.NextToken
	}

	var filtered []filteredMetric
	if len(c.Metrics) == 0 {
		filtered = append(filtered, filteredMetric{
			metrics:    metrics,
			accounts:   accounts,
			statFilter: c.statFilter,
		})
	} else {
		for idx, cm := range c.Metrics {
			var entry filteredMetric
			if cm.StatisticInclude == nil && cm.StatisticExclude == nil {
				entry.statFilter = c.statFilter
			} else {
				f, err := filter.NewIncludeExcludeFilter(*cm.StatisticInclude, *cm.StatisticExclude)
				if err != nil {
					return nil, fmt.Errorf("creating statistics filter for metric %d failed: %w", idx+1, err)
				}
				entry.statFilter = f
			}

			for i, m := range metrics {
				if metricMatch(cm, m) {
					entry.metrics = append(entry.metrics, m)
					if len(accounts) > 0 {
						entry.accounts = append(entry.accounts, accounts[i])
					}
				}
			}
			filtered = append(filtered, entry)
		}
	}

	c.cache = &metricCache{
		metrics: filtered,
		built:   time.Now(),
		ttl:     time.Duration(c.CacheTTL),
	}

	return filtered, nil
}

func (c *CloudWatch) updateWindow(relativeTo time.Time) {
	windowEnd := relativeTo.Add(-time.Duration(c.Delay))

	if c.windowEnd.IsZero() {
		// this is the first run, no window info, so just get a single period
		c.windowStart = windowEnd.Add(-time.Duration(c.Period))
	} else {
		// subsequent window, start where last window left off
		c.windowStart = c.windowEnd
	}

	c.windowEnd = windowEnd
}

// getDataQueries gets all of the possible queries so we can maximize the request payload.
func (c *CloudWatch) getDataQueries(filteredMetrics []filteredMetric) map[string][]types.MetricDataQuery {
	if c.cache != nil && c.cache.queries != nil && c.cache.metrics != nil && time.Since(c.cache.built) < c.cache.ttl {
		return c.cache.queries
	}

	c.queryDimensions = make(map[string]*map[string]string)
	dataQueries := make(map[string][]types.MetricDataQuery)
	for i, filtered := range filteredMetrics {
		for j, singleMetric := range filtered.metrics {
			id := strconv.Itoa(j) + "_" + strconv.Itoa(i)
			dimension := ctod(singleMetric.Dimensions)
			var accountID *string
			if c.IncludeLinkedAccounts && len(filtered.accounts) > j {
				accountID = aws.String(filtered.accounts[j])
				(*dimension)["account"] = filtered.accounts[j]
			}

			statisticTypes := map[string]string{
				"average":      "Average",
				"maximum":      "Maximum",
				"minimum":      "Minimum",
				"sum":          "Sum",
				"sample_count": "SampleCount",
			}

			for statisticType, statistic := range statisticTypes {
				if !filtered.statFilter.Match(statisticType) {
					continue
				}
				queryID := statisticType + "_" + id
				c.queryDimensions[queryID] = dimension
				dataQueries[*singleMetric.Namespace] = append(dataQueries[*singleMetric.Namespace], types.MetricDataQuery{
					Id:        aws.String(queryID),
					AccountId: accountID,
					Label:     aws.String(snakeCase(*singleMetric.MetricName + "_" + statisticType)),
					MetricStat: &types.MetricStat{
						Metric: &filtered.metrics[j],
						Period: aws.Int32(int32(time.Duration(c.Period).Seconds())),
						Stat:   aws.String(statistic),
					},
				})
			}
		}
	}

	if len(dataQueries) == 0 {
		c.Log.Debug("no metrics found to collect")
		return nil
	}

	if c.cache == nil {
		c.cache = &metricCache{
			queries: dataQueries,
			built:   time.Now(),
			ttl:     time.Duration(c.CacheTTL),
		}
	} else {
		c.cache.queries = dataQueries
	}

	return dataQueries
}

func (c *CloudWatch) gatherMetrics(queries []types.MetricDataQuery) ([]types.MetricDataResult, error) {
	params := &cloudwatch.GetMetricDataInput{
		StartTime:         aws.Time(c.windowStart),
		EndTime:           aws.Time(c.windowEnd),
		MetricDataQueries: queries,
	}

	results := make([]types.MetricDataResult, 0)
	for {
		resp, err := c.client.GetMetricData(context.Background(), params)
		if err != nil {
			return nil, fmt.Errorf("failed to get metric data: %w", err)
		}

		results = append(results, resp.MetricDataResults...)
		if resp.NextToken == nil {
			break
		}
		params.NextToken = resp.NextToken
	}

	return results, nil
}

func (c *CloudWatch) aggregateMetrics(acc telegraf.Accumulator, metricDataResults map[string][]types.MetricDataResult) {
	grouper := metric.NewSeriesGrouper()
	for namespace, results := range metricDataResults {
		namespace = sanitizeMeasurement(namespace)

		for _, result := range results {
			tags := make(map[string]string)
			if dimensions, ok := c.queryDimensions[*result.Id]; ok {
				tags = *dimensions
			}
			tags["region"] = c.Region

			for i := range result.Values {
				if c.MetricFormat == "dense" {
					// Remove the IDs from the result ID to get the statistic type
					// e.g. "average" from "average_0_0"
					re := regexp.MustCompile(`_\d+_\d+$`)
					statisticType := re.ReplaceAllString(*result.Id, "")

					// Remove the statistic type from the label to get the AWS Metric name
					// e.g. "CPUUtilization" from "CPUUtilization_average"
					re = regexp.MustCompile(`_?` + regexp.QuoteMeta(statisticType) + `$`)
					tags["metric_name"] = re.ReplaceAllString(*result.Label, "")

					grouper.Add(namespace, tags, result.Timestamps[i], statisticType, result.Values[i])
				} else {
					grouper.Add(namespace, tags, result.Timestamps[i], *result.Label, result.Values[i])
				}
			}
		}
	}

	for _, singleMetric := range grouper.Metrics() {
		acc.AddMetric(singleMetric)
	}
}

func init() {
	inputs.Add("cloudwatch", func() telegraf.Input {
		return &CloudWatch{
			CacheTTL:  config.Duration(time.Hour),
			RateLimit: 25,
			Timeout:   config.Duration(time.Second * 5),
			BatchSize: 500,
		}
	})
}
