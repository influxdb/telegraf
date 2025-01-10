package prometheus

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/parsers"
)

func AcceptsContent(header http.Header) bool {
	return expfmt.ResponseFormat(header).FormatType() != expfmt.TypeUnknown
}

type Parser struct {
	IgnoreTimestamp bool              `toml:"prometheus_ignore_timestamp"`
	MetricVersion   int               `toml:"prometheus_metric_version"`
	Header          http.Header       `toml:"-"` // set by the prometheus input
	DefaultTags     map[string]string `toml:"-"`
	Log             telegraf.Logger   `toml:"-"`
}

func (p *Parser) SetDefaultTags(tags map[string]string) {
	p.DefaultTags = tags
}

func (p *Parser) Parse(data []byte) ([]telegraf.Metric, error) {
	// Determine the metric transport-type derived from the response header and
	// create a matching decoder.
	format := expfmt.NewFormat(expfmt.TypeProtoCompact)
	if len(p.Header) > 0 {
		format = expfmt.ResponseFormat(p.Header)
		switch format.FormatType() {
		case expfmt.TypeProtoText:
			// Make sure we have a finishing newline but no trailing one
			data = bytes.TrimPrefix(data, []byte("\n"))
			if !bytes.HasSuffix(data, []byte("\n")) {
				data = append(data, []byte("\n")...)
			}
		case expfmt.TypeUnknown:
			p.Log.Debugf("Unknown format %q... Trying to continue...", p.Header.Get("Content-Type"))
		}
	}
	buf := bytes.NewBuffer(data)
	decoder := expfmt.NewDecoder(buf, format)

	// Decode the input data into prometheus metrics
	var metrics []telegraf.Metric
	for {
		var mf dto.MetricFamily
		if err := decoder.Decode(&mf); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding response failed: %w", err)
		}

		switch p.MetricVersion {
		case 0, 2:
			metrics = append(metrics, p.extractMetricsV2(&mf)...)
		case 1:
			metrics = append(metrics, p.extractMetricsV1(&mf)...)
		default:
			return nil, fmt.Errorf("unknown prometheus metric version %d", p.MetricVersion)
		}
	}
	return metrics, nil
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

func init() {
	parsers.Add("prometheus",
		func(string) telegraf.Parser {
			return &Parser{}
		},
	)
}
