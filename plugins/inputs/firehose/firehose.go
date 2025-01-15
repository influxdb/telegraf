//go:generate ../../../tools/readme_config_includer/generator
package firehose

import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Firehose struct {
	ServiceAddress string          `toml:"service_address"`
	Paths          []string        `toml:"paths"`
	ReadTimeout    config.Duration `toml:"read_timeout"`
	WriteTimeout   config.Duration `toml:"write_timeout"`
	AccessKey      config.Secret   `toml:"access_key"`
	ParameterTags  []string        `toml:"parameter_tags"`
	Log            telegraf.Logger `toml:"-"`

	common_tls.ServerConfig
	tlsConf *tls.Config

	once sync.Once

	listener net.Listener
	server   http.Server

	parser telegraf.Parser
	acc    telegraf.Accumulator
}

func (*Firehose) SampleConfig() string {
	return sampleConfig
}

func (f *Firehose) SetParser(parser telegraf.Parser) {
	f.parser = parser
}

func (f *Firehose) Init() error {
	if f.ServiceAddress == "" {
		f.ServiceAddress = ":8080"
	}
	if len(f.Paths) == 0 {
		f.Paths = []string{"/telegraf"}
	}

	var err error
	f.tlsConf, err = f.ServerConfig.TLSConfig()
	return err
}

// Start starts the http listener service.
func (f *Firehose) Start(acc telegraf.Accumulator) error {
	f.acc = acc

	var err error
	if f.tlsConf != nil {
		f.listener, err = tls.Listen("tcp", f.ServiceAddress, f.tlsConf)
	} else {
		f.listener, err = net.Listen("tcp", f.ServiceAddress)
	}
	if err != nil {
		return fmt.Errorf("creating listener failed: %w", err)
	}

	f.server = http.Server{
		Addr:         f.ServiceAddress,
		Handler:      f,
		ReadTimeout:  time.Duration(f.ReadTimeout),
		WriteTimeout: time.Duration(f.WriteTimeout),
		TLSConfig:    f.tlsConf,
	}

	go func() {
		if err := f.server.Serve(f.listener); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				f.Log.Errorf("Server failed: %v", err)
			}
		}
	}()

	f.Log.Infof("Listening on %s", f.listener.Addr().String())

	return nil
}

// Stop cleans up all resources
func (f *Firehose) Stop() {
	if err := f.server.Shutdown(context.Background()); err != nil {
		f.Log.Errorf("Shutting down server failed: %v", err)
	}
}

func (*Firehose) Gather(telegraf.Accumulator) error {
	return nil
}

func (f *Firehose) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	if !slices.Contains(f.Paths, req.URL.Path) {
		res.WriteHeader(http.StatusNotFound)
		return
	}

	msg, err := f.handleRequest(req)
	if err != nil {
		f.acc.AddError(err)
	}
	if err := msg.sendResponse(res); err != nil {
		f.acc.AddError(fmt.Errorf("sending response failed: %w", err))
	}
}

func (f *Firehose) handleRequest(req *http.Request) (*message, error) {
	// Create a request with a default response status code
	msg := &message{
		responseCode: http.StatusInternalServerError,
	}

	// Extract the request ID used to reference the request
	msg.id = req.Header.Get("x-amz-firehose-request-id")
	if msg.id == "" {
		msg.responseCode = http.StatusBadRequest
		return msg, errors.New("x-amz-firehose-request-id header is not set")
	}

	// Check the maximum body size which can be up to 64 MiB according to
	// https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html
	if req.ContentLength > int64(64*1024*1024) {
		msg.responseCode = http.StatusRequestEntityTooLarge
		return msg, errors.New("content length is too large")
	}

	// Check the HTTP method used
	switch req.Method {
	case http.MethodPost, http.MethodPut:
		// Do nothing, those methods are allowed
	default:
		msg.responseCode = http.StatusMethodNotAllowed
		return msg, fmt.Errorf("method %q is not allowed", req.Method)
	}

	if req.Header.Get("content-type") != "application/json" {
		msg.responseCode = http.StatusUnsupportedMediaType
		return msg, fmt.Errorf("content type %q is not allowed", req.Header.Get("content-type"))
	}

	// Decode the content if necessary and parse the JSON message
	encoding := req.Header.Get("content-encoding")
	body, err := internal.NewStreamContentDecoder(encoding, req.Body)
	if err != nil {
		msg.responseCode = http.StatusUnsupportedMediaType
		return msg, fmt.Errorf("creating %q decoder for request %q failed: %w", encoding, msg.id, err)
	}
	defer req.Body.Close()

	var reqbody requestBody
	if err := json.NewDecoder(body).Decode(&reqbody); err != nil {
		msg.responseCode = http.StatusBadRequest
		return msg, fmt.Errorf("decode body for request %q failed: %w", msg.id, err)
	}

	// Validate the body content
	if msg.id != reqbody.RequestID {
		msg.responseCode = http.StatusBadRequest
		return msg, fmt.Errorf("mismatch between request ID in header (%q) and body (%q)", msg.id, reqbody.RequestID)
	}

	// Authenticate the request
	if err := msg.authenticate(req, f.AccessKey); err != nil {
		return msg, fmt.Errorf("authentication for request %q failed: %w", msg.id, err)
	}

	// Extract the records and parameters for tagging
	records, err := msg.decodeData(&reqbody)
	if err != nil {
		return msg, fmt.Errorf("decode base64 data from request %q failed: %w", msg.id, err)
	}

	tags, err := msg.extractTagsFromCommonAttributes(req, f.ParameterTags)
	if err != nil {
		return msg, fmt.Errorf("extracting parameter tags for request %q failed: %w", msg.id, err)
	}

	// Parse the metrics
	var metrics []telegraf.Metric
	for _, record := range records {
		m, err := f.parser.Parse(record)
		if err != nil {
			// respond with bad request status code to inform firehose about the failure
			msg.responseCode = http.StatusBadRequest
			return msg, fmt.Errorf("parsing data of request %q failed: %w", msg.id, err)
		}
		metrics = append(metrics, m...)
	}

	if len(metrics) == 0 {
		f.once.Do(func() {
			f.Log.Info(internal.NoMetricsCreatedMsg)
		})
	}

	// Add the extracted tags and the path
	for _, m := range metrics {
		for k, v := range tags {
			m.AddTag(k, v)
		}
		m.AddTag("path", req.URL.Path)
		f.acc.AddMetric(m)
	}

	msg.responseCode = http.StatusOK
	return msg, nil
}

func init() {
	inputs.Add("firehose", func() telegraf.Input {
		return &Firehose{
			ReadTimeout:  config.Duration(time.Second * 5),
			WriteTimeout: config.Duration(time.Second * 5),
		}
	})
}
