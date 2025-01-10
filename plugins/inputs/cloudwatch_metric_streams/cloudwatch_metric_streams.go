//go:generate ../../../tools/readme_config_includer/generator
package cloudwatch_metric_streams

import (
	"compress/gzip"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/choice"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/selfstat"
)

//go:embed sample.conf
var sampleConfig string

// defaultMaxBodySize is the default maximum request body size, in bytes.
// if the request body is over this size, we will return an HTTP 413 error.
// 500 MB
const defaultMaxBodySize = 500 * 1024 * 1024

type CloudWatchMetricStreams struct {
	ServiceAddress   string          `toml:"service_address"`
	Paths            []string        `toml:"paths"`
	MaxBodySize      config.Size     `toml:"max_body_size"`
	ReadTimeout      config.Duration `toml:"read_timeout"`
	WriteTimeout     config.Duration `toml:"write_timeout"`
	AccessKey        string          `toml:"access_key"`
	APICompatability bool            `toml:"api_compatability"`

	requestsReceived selfstat.Stat
	writesServed     selfstat.Stat
	requestTime      selfstat.Stat
	ageMax           selfstat.Stat
	ageMin           selfstat.Stat

	Log telegraf.Logger
	common_tls.ServerConfig
	wg       sync.WaitGroup
	close    chan struct{}
	listener net.Listener
	acc      telegraf.Accumulator
}

type request struct {
	RequestID string `json:"requestId"`
	Timestamp int64  `json:"timestamp"`
	Records   []struct {
		Data string `json:"data"`
	} `json:"records"`
}

type data struct {
	MetricStreamName string             `json:"metric_stream_name"`
	AccountID        string             `json:"account_id"`
	Region           string             `json:"region"`
	Namespace        string             `json:"namespace"`
	MetricName       string             `json:"metric_name"`
	Dimensions       map[string]string  `json:"dimensions"`
	Timestamp        int64              `json:"timestamp"`
	Value            map[string]float64 `json:"value"`
	Unit             string             `json:"unit"`
}

type response struct {
	RequestID string `json:"requestId"`
	Timestamp int64  `json:"timestamp"`
}

type age struct {
	max time.Duration
	min time.Duration
}

func (*CloudWatchMetricStreams) SampleConfig() string {
	return sampleConfig
}

func (cms *CloudWatchMetricStreams) Init() error {
	tags := map[string]string{
		"address": cms.ServiceAddress,
	}
	cms.requestsReceived = selfstat.Register("cloudwatch_metric_streams", "requests_received", tags)
	cms.writesServed = selfstat.Register("cloudwatch_metric_streams", "writes_served", tags)
	cms.requestTime = selfstat.Register("cloudwatch_metric_streams", "request_time", tags)
	cms.ageMax = selfstat.Register("cloudwatch_metric_streams", "age_max", tags)
	cms.ageMin = selfstat.Register("cloudwatch_metric_streams", "age_min", tags)

	if cms.MaxBodySize == 0 {
		cms.MaxBodySize = config.Size(defaultMaxBodySize)
	}

	if cms.ReadTimeout < config.Duration(time.Second) {
		cms.ReadTimeout = config.Duration(time.Second * 10)
	}

	if cms.WriteTimeout < config.Duration(time.Second) {
		cms.WriteTimeout = config.Duration(time.Second * 10)
	}

	return nil
}

// Start starts the http listener service.
func (cms *CloudWatchMetricStreams) Start(acc telegraf.Accumulator) error {
	cms.acc = acc
	server := cms.createHTTPServer()

	var err error
	server.TLSConfig, err = cms.ServerConfig.TLSConfig()
	if err != nil {
		return err
	}
	if server.TLSConfig != nil {
		cms.listener, err = tls.Listen("tcp", cms.ServiceAddress, server.TLSConfig)
	} else {
		cms.listener, err = net.Listen("tcp", cms.ServiceAddress)
	}
	if err != nil {
		return err
	}

	cms.wg.Add(1)
	go func() {
		defer cms.wg.Done()
		if err := server.Serve(cms.listener); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				cms.Log.Errorf("Serve failed: %v", err)
			}
			close(cms.close)
		}
	}()

	cms.Log.Infof("Listening on %s", cms.listener.Addr().String())

	return nil
}

func (*CloudWatchMetricStreams) Gather(telegraf.Accumulator) error {
	return nil
}

func (cms *CloudWatchMetricStreams) Stop() {
	if cms.listener != nil {
		cms.listener.Close()
	}
	cms.wg.Wait()
}

func (cms *CloudWatchMetricStreams) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	cms.requestsReceived.Incr(1)
	start := time.Now()
	defer cms.recordRequestTime(start)

	handler := cms.serveWrite

	if !choice.Contains(req.URL.Path, cms.Paths) {
		handler = http.NotFound
	}

	cms.authenticateIfSet(handler, res, req)
}

func (a *age) record(t time.Duration) {
	if t > a.max {
		a.max = t
	}

	if t < a.min {
		a.min = t
	}
}

func (a *age) submitMax(stat selfstat.Stat) {
	stat.Incr(a.max.Nanoseconds())
}

func (a *age) submitMin(stat selfstat.Stat) {
	stat.Incr(a.min.Nanoseconds())
}

func (cms *CloudWatchMetricStreams) createHTTPServer() *http.Server {
	return &http.Server{
		Addr:         cms.ServiceAddress,
		Handler:      cms,
		ReadTimeout:  time.Duration(cms.ReadTimeout),
		WriteTimeout: time.Duration(cms.WriteTimeout),
	}
}

func (cms *CloudWatchMetricStreams) recordRequestTime(start time.Time) {
	elapsed := time.Since(start)
	cms.requestTime.Incr(elapsed.Nanoseconds())
}

func (cms *CloudWatchMetricStreams) serveWrite(res http.ResponseWriter, req *http.Request) {
	select {
	case <-cms.close:
		res.WriteHeader(http.StatusGone)
		return
	default:
	}

	defer cms.writesServed.Incr(1)

	// Check that the content length is not too large for us to handle.
	if req.ContentLength > int64(cms.MaxBodySize) {
		cms.Log.Errorf("content length exceeded maximum body size")
		if err := tooLarge(res); err != nil {
			cms.Log.Debugf("error in too-large: %v", err)
		}
		return
	}

	// Check that the method is a POST
	if req.Method != "POST" {
		cms.Log.Errorf("incompatible request method")
		if err := methodNotAllowed(res); err != nil {
			cms.Log.Debugf("error in method-not-allowed: %v", err)
		}
		return
	}

	// Decode GZIP
	var body = req.Body
	encoding := req.Header.Get("Content-Encoding")

	if encoding == "gzip" {
		reader, err := gzip.NewReader(req.Body)
		if err != nil {
			cms.Log.Errorf("unable to uncompress metric-streams data: %v", err)
			if err := badRequest(res); err != nil {
				cms.Log.Debugf("error in bad-request: %v", err)
			}
			return
		}
		body = reader
		defer reader.Close()
	}

	// Decode the request
	var r request
	err := json.NewDecoder(body).Decode(&r)
	if err != nil {
		cms.Log.Errorf("unable to decode metric-streams request: %v", err)
		if err := badRequest(res); err != nil {
			cms.Log.Debugf("error in bad-request: %v", err)
		}
		return
	}

	agesInRequest := &age{max: 0, min: math.MaxInt32}
	defer agesInRequest.submitMax(cms.ageMax)
	defer agesInRequest.submitMin(cms.ageMin)

	// For each record, decode the base64 data and store it in a data struct
	// Metrics from Metric Streams are Base64 encoded JSON
	// https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html
	for _, record := range r.Records {
		b, err := base64.StdEncoding.DecodeString(record.Data)
		if err != nil {
			cms.Log.Errorf("unable to base64 decode metric-streams data: %v", err)
			if err := badRequest(res); err != nil {
				cms.Log.Debugf("error in bad-request: %v", err)
			}
			return
		}

		list := strings.Split(string(b), "\n")

		// If the last element is empty, remove it to avoid unexpected JSON
		if len(list) > 0 {
			if list[len(list)-1] == "" {
				list = list[:len(list)-1]
			}
		}

		for _, js := range list {
			var d data
			err = json.Unmarshal([]byte(js), &d)
			if err != nil {
				cms.Log.Errorf("unable to unmarshal metric-streams data: %v", err)
				if err := badRequest(res); err != nil {
					cms.Log.Debugf("error in bad-request: %v", err)
				}
				return
			}
			cms.composeMetrics(d)
			agesInRequest.record(time.Since(time.Unix(d.Timestamp/1000, 0)))
		}
	}

	// Compose the response to AWS using the request's requestId
	// https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html#responseformat
	response := response{
		RequestID: r.RequestID,
		Timestamp: time.Now().UnixNano() / 1000000,
	}

	marshalled, err := json.Marshal(response)
	if err != nil {
		cms.Log.Errorf("unable to compose response: %v", err)
		if err := badRequest(res); err != nil {
			cms.Log.Debugf("error in bad-request: %v", err)
		}
		return
	}

	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(http.StatusOK)
	_, err = res.Write(marshalled)
	if err != nil {
		cms.Log.Debugf("Error writing response to AWS: %s", err.Error())
		return
	}
}

func (cms *CloudWatchMetricStreams) composeMetrics(data data) {
	fields := make(map[string]interface{})
	tags := make(map[string]string)
	timestamp := time.Unix(data.Timestamp/1000, 0)

	namespace := strings.Replace(data.Namespace, "/", "_", -1)
	measurement := strings.ToLower(namespace + "_" + data.MetricName)

	for field, value := range data.Value {
		fields[field] = value
	}

	// Rename Statistics to match the CloudWatch API if in API Compatability mode
	if cms.APICompatability {
		if v, ok := fields["max"]; ok {
			fields["maximum"] = v
			delete(fields, "max")
		}

		if v, ok := fields["min"]; ok {
			fields["minimum"] = v
			delete(fields, "min")
		}

		if v, ok := fields["count"]; ok {
			fields["samplecount"] = v
			delete(fields, "count")
		}
	}

	tags["accountId"] = data.AccountID
	tags["region"] = data.Region

	for dimension, value := range data.Dimensions {
		tags[dimension] = value
	}

	cms.acc.AddFields(measurement, fields, tags, timestamp)
}

func tooLarge(res http.ResponseWriter) error {
	tags := map[string]string{
		"status_code": strconv.Itoa(http.StatusRequestEntityTooLarge),
	}
	selfstat.Register("cloudwatch_metric_streams", "bad_requests", tags).Incr(1)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(http.StatusRequestEntityTooLarge)
	_, err := res.Write([]byte(`{"error":"http: request body too large"}`))
	return err
}

func methodNotAllowed(res http.ResponseWriter) error {
	tags := map[string]string{
		"status_code": strconv.Itoa(http.StatusMethodNotAllowed),
	}
	selfstat.Register("cloudwatch_metric_streams", "bad_requests", tags).Incr(1)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(http.StatusMethodNotAllowed)
	_, err := res.Write([]byte(`{"error":"http: method not allowed"}`))
	return err
}

func badRequest(res http.ResponseWriter) error {
	tags := map[string]string{
		"status_code": strconv.Itoa(http.StatusBadRequest),
	}
	selfstat.Register("cloudwatch_metric_streams", "bad_requests", tags).Incr(1)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(http.StatusBadRequest)
	_, err := res.Write([]byte(`{"error":"http: bad request"}`))
	return err
}

func (cms *CloudWatchMetricStreams) authenticateIfSet(handler http.HandlerFunc, res http.ResponseWriter, req *http.Request) {
	if cms.AccessKey != "" {
		auth := req.Header.Get("X-Amz-Firehose-Access-Key")
		if auth == "" || auth != cms.AccessKey {
			http.Error(res, "Unauthorized.", http.StatusUnauthorized)
			return
		}
		handler(res, req)
	} else {
		handler(res, req)
	}
}

func init() {
	inputs.Add("cloudwatch_metric_streams", func() telegraf.Input {
		return &CloudWatchMetricStreams{
			ServiceAddress: ":443",
			Paths:          []string{"/telegraf"},
		}
	})
}
