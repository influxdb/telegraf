package prometheusremotewrite

import (
	"errors"
	"fmt"

	"github.com/prometheus/prometheus/prompb"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/parsers"
)

type Parser struct {
	DefaultTags   map[string]string
	MetricVersion int `toml:"prometheus_metric_version"`
}

func (p *Parser) Parse(buf []byte) ([]telegraf.Metric, error) {
	var err error
	var metrics []telegraf.Metric
	var req prompb.WriteRequest

	if err := req.Unmarshal(buf); err != nil {
		return nil, fmt.Errorf("unable to unmarshal request body: %w", err)
	}

	for _, ts := range req.Timeseries {
		var metricsFromTs []telegraf.Metric
		switch p.MetricVersion {
		case 0, 2:
			metricsFromTs, err = p.extractMetricsV2(&ts)
		case 1:
			metricsFromTs, err = p.extractMetricsV1(&ts)
		default:
			return nil, fmt.Errorf("unknown prometheus metric version %d", p.MetricVersion)
		}
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metricsFromTs...)
	}

	return metrics, err
}

func (p *Parser) ParseLine(line string) (telegraf.Metric, error) {
	metrics, err := p.Parse([]byte(line))
	if err != nil {
		return nil, err
	}

	if len(metrics) < 1 {
		return nil, errors.New("no metrics in line")
	}

	if len(metrics) > 1 {
		return nil, errors.New("more than one metric in line")
	}

	return metrics[0], nil
}

func (p *Parser) SetDefaultTags(tags map[string]string) {
	p.DefaultTags = tags
}

func init() {
	parsers.Add("prometheusremotewrite",
		func(string) telegraf.Parser {
			return &Parser{}
		})
}
