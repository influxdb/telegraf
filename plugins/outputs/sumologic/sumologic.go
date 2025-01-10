//go:generate ../../../tools/readme_config_includer/generator
package sumologic

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/plugins/serializers/carbon2"
	"github.com/influxdata/telegraf/plugins/serializers/graphite"
	"github.com/influxdata/telegraf/plugins/serializers/prometheus"
)

//go:embed sample.conf
var sampleConfig string

const (
	defaultClientTimeout      = 5 * time.Second
	defaultMethod             = http.MethodPost
	defaultMaxRequestBodySize = 1000000

	contentTypeHeader     = "Content-Type"
	carbon2ContentType    = "application/vnd.sumologic.carbon2"
	graphiteContentType   = "application/vnd.sumologic.graphite"
	prometheusContentType = "application/vnd.sumologic.prometheus"
)

type header string

const (
	sourceNameHeader     header = `X-Sumo-Name`
	sourceHostHeader     header = `X-Sumo-Host`
	sourceCategoryHeader header = `X-Sumo-Category`
	dimensionsHeader     header = `X-Sumo-Dimensions`
)

type SumoLogic struct {
	URL                string          `toml:"url"`
	Timeout            config.Duration `toml:"timeout"`
	MaxRequestBodySize config.Size     `toml:"max_request_body_size"`

	SourceName     string `toml:"source_name"`
	SourceHost     string `toml:"source_host"`
	SourceCategory string `toml:"source_category"`
	Dimensions     string `toml:"dimensions"`

	Log telegraf.Logger `toml:"-"`

	client     *http.Client
	serializer telegraf.Serializer

	headers map[string]string
}

func (*SumoLogic) SampleConfig() string {
	return sampleConfig
}

func (s *SumoLogic) SetSerializer(serializer telegraf.Serializer) {
	s.serializer = serializer
}

func (s *SumoLogic) createClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
		Timeout: time.Duration(s.Timeout),
	}
}

func (s *SumoLogic) Connect() error {
	s.headers = make(map[string]string)

	var serializer telegraf.Serializer
	if unwrapped, ok := s.serializer.(*models.RunningSerializer); ok {
		serializer = unwrapped.Serializer
	} else {
		serializer = s.serializer
	}

	switch serializer.(type) {
	case *carbon2.Serializer:
		s.headers[contentTypeHeader] = carbon2ContentType
	case *graphite.GraphiteSerializer:
		s.headers[contentTypeHeader] = graphiteContentType
	case *prometheus.Serializer:
		s.headers[contentTypeHeader] = prometheusContentType
	default:
		return fmt.Errorf("unsupported serializer %T", serializer)
	}

	if s.Timeout == 0 {
		s.Timeout = config.Duration(defaultClientTimeout)
	}

	s.client = s.createClient()

	return nil
}

func (s *SumoLogic) Close() error {
	return nil
}

func (s *SumoLogic) Write(metrics []telegraf.Metric) error {
	if s.serializer == nil {
		return errors.New("sumologic: serializer unset")
	}
	if len(metrics) == 0 {
		return nil
	}

	reqBody, err := s.serializer.SerializeBatch(metrics)
	if err != nil {
		return err
	}

	if l := len(reqBody); l > int(s.MaxRequestBodySize) {
		chunks, err := s.splitIntoChunks(metrics)
		if err != nil {
			return err
		}

		return s.writeRequestChunks(chunks)
	}

	return s.writeRequestChunk(reqBody)
}

func (s *SumoLogic) writeRequestChunks(chunks [][]byte) error {
	for _, reqChunk := range chunks {
		if err := s.writeRequestChunk(reqChunk); err != nil {
			s.Log.Errorf("Error sending chunk: %v", err)
		}
	}
	return nil
}

func (s *SumoLogic) writeRequestChunk(reqBody []byte) error {
	var (
		err  error
		buff bytes.Buffer
		gz   = gzip.NewWriter(&buff)
	)

	if _, err = gz.Write(reqBody); err != nil {
		return err
	}

	if err := gz.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(defaultMethod, s.URL, &buff)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("User-Agent", internal.ProductToken())

	// Set headers coming from the configuration.
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	setHeaderIfSetInConfig(req, sourceNameHeader, s.SourceName)
	setHeaderIfSetInConfig(req, sourceHostHeader, s.SourceHost)
	setHeaderIfSetInConfig(req, sourceCategoryHeader, s.SourceCategory)
	setHeaderIfSetInConfig(req, dimensionsHeader, s.Dimensions)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sumologic: failed sending request to %q: %w", s.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sumologic: when writing to %q received status code: %d", s.URL, resp.StatusCode)
	}

	return nil
}

// splitIntoChunks splits metrics to be sent into chunks so that every request
// is smaller than s.MaxRequestBodySize unless it was configured so small so that
// even a single metric cannot fit.
// In such a situation metrics will be sent one by one with a warning being logged
// for every request sent even though they don't fit in s.MaxRequestBodySize bytes.
func (s *SumoLogic) splitIntoChunks(metrics []telegraf.Metric) ([][]byte, error) {
	var (
		numMetrics = len(metrics)
		chunks     = make([][]byte, 0)
	)

	for i := 0; i < numMetrics; {
		var toAppend []byte
		for i < numMetrics {
			chunkBody, err := s.serializer.Serialize(metrics[i])
			if err != nil {
				return nil, err
			}

			la := len(toAppend)
			if la != 0 {
				// We already have something to append ...
				if la+len(chunkBody) > int(s.MaxRequestBodySize) {
					// ... and it's just the right size, without currently processed chunk.
					break
				}
				// ... we can try appending more.
				i++
				toAppend = append(toAppend, chunkBody...)
				continue
			}

			// la == 0
			i++
			toAppend = chunkBody

			if len(chunkBody) > int(s.MaxRequestBodySize) {
				s.Log.Warnf(
					"max_request_body_size set to %d which is too small even for a single metric (len: %d), sending without split",
					s.MaxRequestBodySize, len(chunkBody),
				)

				// The serialized metric is too big, but we have no choice
				// but to send it.
				// max_request_body_size was set so small that it wouldn't
				// even accommodate a single metric.
				break
			}

			continue
		}

		if toAppend == nil {
			break
		}

		chunks = append(chunks, toAppend)
	}

	return chunks, nil
}

func setHeaderIfSetInConfig(r *http.Request, h header, value string) {
	if value != "" {
		r.Header.Set(string(h), value)
	}
}

func Default() *SumoLogic {
	return &SumoLogic{
		Timeout:            config.Duration(defaultClientTimeout),
		MaxRequestBodySize: defaultMaxRequestBodySize,
		headers:            make(map[string]string),
	}
}

func init() {
	outputs.Add("sumologic", func() telegraf.Output {
		return Default()
	})
}
