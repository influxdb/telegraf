//go:generate ../../../tools/readme_config_includer/generator
package logstash

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/choice"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/inputs"
	parsers_json "github.com/influxdata/telegraf/plugins/parsers/json"
)

//go:embed sample.conf
var sampleConfig string

const (
	jvmStatsNode       = "/_node/stats/jvm"
	processStatsNode   = "/_node/stats/process"
	pipelinesStatsNode = "/_node/stats/pipelines"
	pipelineStatsNode  = "/_node/stats/pipeline"
)

type Logstash struct {
	URL string `toml:"url"`

	SinglePipeline bool     `toml:"single_pipeline"`
	Collect        []string `toml:"collect"`

	Username string            `toml:"username"`
	Password string            `toml:"password"`
	Headers  map[string]string `toml:"headers"`

	Log telegraf.Logger `toml:"-"`

	client *http.Client
	common_http.HTTPClientConfig
}

type processStats struct {
	ID      string      `json:"id"`
	Process interface{} `json:"process"`
	Name    string      `json:"name"`
	Host    string      `json:"host"`
	Version string      `json:"version"`
}

type jvmStats struct {
	ID      string      `json:"id"`
	JVM     interface{} `json:"jvm"`
	Name    string      `json:"name"`
	Host    string      `json:"host"`
	Version string      `json:"version"`
}

type pipelinesStats struct {
	ID        string              `json:"id"`
	Pipelines map[string]pipeline `json:"pipelines"`
	Name      string              `json:"name"`
	Host      string              `json:"host"`
	Version   string              `json:"version"`
}

type pipelineStats struct {
	ID       string   `json:"id"`
	Pipeline pipeline `json:"pipeline"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Version  string   `json:"version"`
}

type pipeline struct {
	Events  interface{}     `json:"events"`
	Plugins pipelinePlugins `json:"plugins"`
	Reloads interface{}     `json:"reloads"`
	Queue   pipelineQueue   `json:"queue"`
}

type plugin struct {
	ID           string                 `json:"id"`
	Events       interface{}            `json:"events"`
	Name         string                 `json:"name"`
	Failures     *int64                 `json:"failures,omitempty"`
	BulkRequests map[string]interface{} `json:"bulk_requests"`
	Documents    map[string]interface{} `json:"documents"`
}

type pipelinePlugins struct {
	Inputs  []plugin `json:"inputs"`
	Filters []plugin `json:"filters"`
	Outputs []plugin `json:"outputs"`
}

type pipelineQueue struct {
	Events              float64     `json:"events"`
	EventsCount         *float64    `json:"events_count"`
	Type                string      `json:"type"`
	Capacity            interface{} `json:"capacity"`
	Data                interface{} `json:"data"`
	QueueSizeInBytes    *float64    `json:"queue_size_in_bytes"`
	MaxQueueSizeInBytes *float64    `json:"max_queue_size_in_bytes"`
}

func (*Logstash) SampleConfig() string {
	return sampleConfig
}

func (logstash *Logstash) Init() error {
	err := choice.CheckSlice(logstash.Collect, []string{"pipelines", "process", "jvm"})
	if err != nil {
		return fmt.Errorf(`cannot verify "collect" setting: %w`, err)
	}
	return nil
}

func (*Logstash) Start(telegraf.Accumulator) error {
	return nil
}

func (logstash *Logstash) Gather(accumulator telegraf.Accumulator) error {
	if logstash.client == nil {
		client, err := logstash.createHTTPClient()

		if err != nil {
			return err
		}
		logstash.client = client
	}

	if choice.Contains("jvm", logstash.Collect) {
		jvmURL, err := url.Parse(logstash.URL + jvmStatsNode)
		if err != nil {
			return err
		}
		if err := logstash.gatherJVMStats(jvmURL.String(), accumulator); err != nil {
			return err
		}
	}

	if choice.Contains("process", logstash.Collect) {
		processURL, err := url.Parse(logstash.URL + processStatsNode)
		if err != nil {
			return err
		}
		if err := logstash.gatherProcessStats(processURL.String(), accumulator); err != nil {
			return err
		}
	}

	if choice.Contains("pipelines", logstash.Collect) {
		if logstash.SinglePipeline {
			pipelineURL, err := url.Parse(logstash.URL + pipelineStatsNode)
			if err != nil {
				return err
			}
			if err := logstash.gatherPipelineStats(pipelineURL.String(), accumulator); err != nil {
				return err
			}
		} else {
			pipelinesURL, err := url.Parse(logstash.URL + pipelinesStatsNode)
			if err != nil {
				return err
			}
			if err := logstash.gatherPipelinesStats(pipelinesURL.String(), accumulator); err != nil {
				return err
			}
		}
	}

	return nil
}

func (logstash *Logstash) Stop() {
	if logstash.client != nil {
		logstash.client.CloseIdleConnections()
	}
}

// createHTTPClient create a clients to access API
func (logstash *Logstash) createHTTPClient() (*http.Client, error) {
	ctx := context.Background()
	return logstash.HTTPClientConfig.CreateClient(ctx, logstash.Log)
}

// gatherJSONData query the data source and parse the response JSON
func (logstash *Logstash) gatherJSONData(address string, value interface{}) error {
	request, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return err
	}

	if (logstash.Username != "") || (logstash.Password != "") {
		request.SetBasicAuth(logstash.Username, logstash.Password)
	}

	for header, value := range logstash.Headers {
		if strings.EqualFold(header, "host") {
			request.Host = value
		} else {
			request.Header.Add(header, value)
		}
	}

	response, err := logstash.client.Do(request)
	if err != nil {
		return err
	}

	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		//nolint:errcheck // LimitReader returns io.EOF and we're not interested in read errors.
		body, _ := io.ReadAll(io.LimitReader(response.Body, 200))
		return fmt.Errorf("%s returned HTTP status %s: %q", address, response.Status, body)
	}

	err = json.NewDecoder(response.Body).Decode(value)
	if err != nil {
		return err
	}

	return nil
}

// gatherJVMStats gather the JVM metrics and add results to the accumulator
func (logstash *Logstash) gatherJVMStats(address string, accumulator telegraf.Accumulator) error {
	jvmStats := &jvmStats{}

	err := logstash.gatherJSONData(address, jvmStats)
	if err != nil {
		return err
	}

	tags := map[string]string{
		"node_id":      jvmStats.ID,
		"node_name":    jvmStats.Name,
		"node_version": jvmStats.Version,
		"source":       jvmStats.Host,
	}

	flattener := parsers_json.JSONFlattener{}
	err = flattener.FlattenJSON("", jvmStats.JVM)
	if err != nil {
		return err
	}
	accumulator.AddFields("logstash_jvm", flattener.Fields, tags)

	return nil
}

// gatherProcessStats gather the Process metrics and add results to the accumulator
func (logstash *Logstash) gatherProcessStats(address string, accumulator telegraf.Accumulator) error {
	processStats := &processStats{}

	err := logstash.gatherJSONData(address, processStats)
	if err != nil {
		return err
	}

	tags := map[string]string{
		"node_id":      processStats.ID,
		"node_name":    processStats.Name,
		"node_version": processStats.Version,
		"source":       processStats.Host,
	}

	flattener := parsers_json.JSONFlattener{}
	err = flattener.FlattenJSON("", processStats.Process)
	if err != nil {
		return err
	}
	accumulator.AddFields("logstash_process", flattener.Fields, tags)

	return nil
}

// gatherPluginsStats go through a list of plugins and add their metrics to the accumulator
func gatherPluginsStats(plugins []plugin, pluginType string, tags map[string]string, accumulator telegraf.Accumulator) error {
	for _, plugin := range plugins {
		pluginTags := map[string]string{
			"plugin_name": plugin.Name,
			"plugin_id":   plugin.ID,
			"plugin_type": pluginType,
		}
		for tag, value := range tags {
			pluginTags[tag] = value
		}
		flattener := parsers_json.JSONFlattener{}
		err := flattener.FlattenJSON("", plugin.Events)
		if err != nil {
			return err
		}
		accumulator.AddFields("logstash_plugins", flattener.Fields, pluginTags)
		if plugin.Failures != nil {
			failuresFields := map[string]interface{}{"failures": *plugin.Failures}
			accumulator.AddFields("logstash_plugins", failuresFields, pluginTags)
		}
		/*
			The elasticsearch & opensearch output produces additional stats
			around bulk requests and document writes (that are elasticsearch
			and opensearch specific). Collect those below:
		*/
		if pluginType == "output" && (plugin.Name == "elasticsearch" || plugin.Name == "opensearch") {
			/*
				The "bulk_requests" section has details about batch writes
				into Elasticsearch

				  "bulk_requests" : {
					"successes" : 2870,
					"responses" : {
					  "200" : 2870
					},
					"failures": 262,
					"with_errors": 9089
				  },
			*/
			flattener := parsers_json.JSONFlattener{}
			err := flattener.FlattenJSON("", plugin.BulkRequests)
			if err != nil {
				return err
			}
			for k, v := range flattener.Fields {
				if strings.HasPrefix(k, "bulk_requests") {
					continue
				}
				newKey := "bulk_requests_" + k
				flattener.Fields[newKey] = v
				delete(flattener.Fields, k)
			}
			accumulator.AddFields("logstash_plugins", flattener.Fields, pluginTags)

			/*
				The "documents" section has counts of individual documents
				written/retried/etc.
				  "documents" : {
					"successes" : 2665549,
					"retryable_failures": 13733
				  }
			*/
			flattener = parsers_json.JSONFlattener{}
			err = flattener.FlattenJSON("", plugin.Documents)
			if err != nil {
				return err
			}
			for k, v := range flattener.Fields {
				if strings.HasPrefix(k, "documents") {
					continue
				}
				newKey := "documents_" + k
				flattener.Fields[newKey] = v
				delete(flattener.Fields, k)
			}
			accumulator.AddFields("logstash_plugins", flattener.Fields, pluginTags)
		}
	}

	return nil
}

func gatherQueueStats(queue pipelineQueue, tags map[string]string, acc telegraf.Accumulator) error {
	queueTags := map[string]string{
		"queue_type": queue.Type,
	}
	for tag, value := range tags {
		queueTags[tag] = value
	}

	events := queue.Events
	if queue.EventsCount != nil {
		events = *queue.EventsCount
	}

	queueFields := map[string]interface{}{
		"events": events,
	}

	if queue.Type != "memory" {
		flattener := parsers_json.JSONFlattener{}
		err := flattener.FlattenJSON("", queue.Capacity)
		if err != nil {
			return err
		}
		err = flattener.FlattenJSON("", queue.Data)
		if err != nil {
			return err
		}
		for field, value := range flattener.Fields {
			queueFields[field] = value
		}

		if queue.MaxQueueSizeInBytes != nil {
			queueFields["max_queue_size_in_bytes"] = *queue.MaxQueueSizeInBytes
		}

		if queue.QueueSizeInBytes != nil {
			queueFields["queue_size_in_bytes"] = *queue.QueueSizeInBytes
		}
	}

	acc.AddFields("logstash_queue", queueFields, queueTags)

	return nil
}

// gatherPipelineStats gather the Pipeline metrics and add results to the accumulator (for Logstash < 6)
func (logstash *Logstash) gatherPipelineStats(address string, accumulator telegraf.Accumulator) error {
	pipelineStats := &pipelineStats{}

	err := logstash.gatherJSONData(address, pipelineStats)
	if err != nil {
		return err
	}

	tags := map[string]string{
		"node_id":      pipelineStats.ID,
		"node_name":    pipelineStats.Name,
		"node_version": pipelineStats.Version,
		"source":       pipelineStats.Host,
	}

	flattener := parsers_json.JSONFlattener{}
	err = flattener.FlattenJSON("", pipelineStats.Pipeline.Events)
	if err != nil {
		return err
	}
	accumulator.AddFields("logstash_events", flattener.Fields, tags)

	err = gatherPluginsStats(pipelineStats.Pipeline.Plugins.Inputs, "input", tags, accumulator)
	if err != nil {
		return err
	}
	err = gatherPluginsStats(pipelineStats.Pipeline.Plugins.Filters, "filter", tags, accumulator)
	if err != nil {
		return err
	}
	err = gatherPluginsStats(pipelineStats.Pipeline.Plugins.Outputs, "output", tags, accumulator)
	if err != nil {
		return err
	}

	err = gatherQueueStats(pipelineStats.Pipeline.Queue, tags, accumulator)
	if err != nil {
		return err
	}

	return nil
}

// gatherPipelinesStats gather the Pipelines metrics and add results to the accumulator (for Logstash >= 6)
func (logstash *Logstash) gatherPipelinesStats(address string, accumulator telegraf.Accumulator) error {
	pipelinesStats := &pipelinesStats{}

	err := logstash.gatherJSONData(address, pipelinesStats)
	if err != nil {
		return err
	}

	for pipelineName, pipeline := range pipelinesStats.Pipelines {
		tags := map[string]string{
			"node_id":      pipelinesStats.ID,
			"node_name":    pipelinesStats.Name,
			"node_version": pipelinesStats.Version,
			"pipeline":     pipelineName,
			"source":       pipelinesStats.Host,
		}

		flattener := parsers_json.JSONFlattener{}
		err := flattener.FlattenJSON("", pipeline.Events)
		if err != nil {
			return err
		}
		accumulator.AddFields("logstash_events", flattener.Fields, tags)

		err = gatherPluginsStats(pipeline.Plugins.Inputs, "input", tags, accumulator)
		if err != nil {
			return err
		}
		err = gatherPluginsStats(pipeline.Plugins.Filters, "filter", tags, accumulator)
		if err != nil {
			return err
		}
		err = gatherPluginsStats(pipeline.Plugins.Outputs, "output", tags, accumulator)
		if err != nil {
			return err
		}

		err = gatherQueueStats(pipeline.Queue, tags, accumulator)
		if err != nil {
			return err
		}
	}

	return nil
}

func newLogstash() *Logstash {
	return &Logstash{
		URL:     "http://127.0.0.1:9600",
		Collect: []string{"pipelines", "process", "jvm"},
		Headers: make(map[string]string),
		HTTPClientConfig: common_http.HTTPClientConfig{
			Timeout: config.Duration(5 * time.Second),
		},
	}
}

func init() {
	inputs.Add("logstash", func() telegraf.Input {
		return newLogstash()
	})
}
