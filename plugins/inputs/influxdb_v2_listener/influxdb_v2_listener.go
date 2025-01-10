//go:generate ../../../tools/readme_config_includer/generator
package influxdb_v2_listener

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/plugins/parsers/influx/influx_upstream"
	"github.com/influxdata/telegraf/selfstat"
)

//go:embed sample.conf
var sampleConfig string

const (
	// defaultMaxBodySize is the default maximum request body size, in bytes.
	// if the request body is over this size, we will return an HTTP 413 error.
	defaultMaxBodySize                 = 32 * 1024 * 1024
	defaultReadTimeout                 = 10 * time.Second
	defaultWriteTimeout                = 10 * time.Second
	internalError       BadRequestCode = "internal error"
	invalid             BadRequestCode = "invalid"
)

type InfluxDBV2Listener struct {
	ServiceAddress string `toml:"service_address"`
	port           int
	common_tls.ServerConfig

	MaxUndeliveredMetrics int             `toml:"max_undelivered_metrics"`
	ReadTimeout           config.Duration `toml:"read_timeout"`
	WriteTimeout          config.Duration `toml:"write_timeout"`
	MaxBodySize           config.Size     `toml:"max_body_size"`
	Token                 config.Secret   `toml:"token"`
	BucketTag             string          `toml:"bucket_tag"`
	ParserType            string          `toml:"parser_type"`

	Log telegraf.Logger `toml:"-"`

	ctx                 context.Context
	cancel              context.CancelFunc
	trackingMetricCount map[telegraf.TrackingID]int64
	countLock           sync.Mutex

	totalUndeliveredMetrics atomic.Int64

	timeFunc influx.TimeFunc
	listener net.Listener

	server http.Server
	acc    telegraf.Accumulator

	trackingAcc     telegraf.TrackingAccumulator
	bytesRecv       selfstat.Stat
	requestsServed  selfstat.Stat
	writesServed    selfstat.Stat
	readysServed    selfstat.Stat
	requestsRecv    selfstat.Stat
	notFoundsServed selfstat.Stat

	authFailures selfstat.Stat

	startTime time.Time

	mux http.ServeMux
}

// The BadRequestCode constants keep standard error messages
// see: https://v2.docs.influxdata.com/v2.0/api/#operation/PostWrite
type BadRequestCode string

func (*InfluxDBV2Listener) SampleConfig() string {
	return sampleConfig
}

func (h *InfluxDBV2Listener) Init() error {
	tags := map[string]string{
		"address": h.ServiceAddress,
	}
	h.bytesRecv = selfstat.Register("influxdb_v2_listener", "bytes_received", tags)
	h.requestsServed = selfstat.Register("influxdb_v2_listener", "requests_served", tags)
	h.writesServed = selfstat.Register("influxdb_v2_listener", "writes_served", tags)
	h.readysServed = selfstat.Register("influxdb_v2_listener", "readys_served", tags)
	h.requestsRecv = selfstat.Register("influxdb_v2_listener", "requests_received", tags)
	h.notFoundsServed = selfstat.Register("influxdb_v2_listener", "not_founds_served", tags)
	h.authFailures = selfstat.Register("influxdb_v2_listener", "auth_failures", tags)
	if err := h.routes(); err != nil {
		return err
	}

	if h.MaxBodySize == 0 {
		h.MaxBodySize = config.Size(defaultMaxBodySize)
	}

	if h.ReadTimeout < config.Duration(time.Second) {
		h.ReadTimeout = config.Duration(defaultReadTimeout)
	}
	if h.WriteTimeout < config.Duration(time.Second) {
		h.WriteTimeout = config.Duration(defaultWriteTimeout)
	}

	return nil
}

func (*InfluxDBV2Listener) Gather(telegraf.Accumulator) error {
	return nil
}

func (h *InfluxDBV2Listener) Start(acc telegraf.Accumulator) error {
	h.acc = acc
	h.ctx, h.cancel = context.WithCancel(context.Background())
	if h.MaxUndeliveredMetrics > 0 {
		h.trackingAcc = h.acc.WithTracking(h.MaxUndeliveredMetrics)
		h.trackingMetricCount = make(map[telegraf.TrackingID]int64, h.MaxUndeliveredMetrics)
		go func() {
			for {
				select {
				case <-h.ctx.Done():
					return
				case info := <-h.trackingAcc.Delivered():
					h.countLock.Lock()
					if count, ok := h.trackingMetricCount[info.ID()]; ok {
						h.totalUndeliveredMetrics.Add(-count)
						delete(h.trackingMetricCount, info.ID())
					}
					h.countLock.Unlock()
				}
			}
		}()
	}

	tlsConf, err := h.ServerConfig.TLSConfig()
	if err != nil {
		return err
	}

	h.server = http.Server{
		Addr:         h.ServiceAddress,
		Handler:      h,
		TLSConfig:    tlsConf,
		ReadTimeout:  time.Duration(h.ReadTimeout),
		WriteTimeout: time.Duration(h.WriteTimeout),
	}

	var listener net.Listener
	if tlsConf != nil {
		listener, err = tls.Listen("tcp", h.ServiceAddress, tlsConf)
		if err != nil {
			return err
		}
	} else {
		listener, err = net.Listen("tcp", h.ServiceAddress)
		if err != nil {
			return err
		}
	}
	h.listener = listener
	h.port = listener.Addr().(*net.TCPAddr).Port

	go func() {
		err = h.server.Serve(h.listener)
		if !errors.Is(err, http.ErrServerClosed) {
			h.Log.Infof("Error serving HTTP on %s", h.ServiceAddress)
		}
	}()

	h.startTime = h.timeFunc()

	h.Log.Infof("Started HTTP listener service on %s", h.ServiceAddress)

	return nil
}

func (h *InfluxDBV2Listener) Stop() {
	h.cancel()
	err := h.server.Shutdown(context.Background())
	if err != nil {
		h.Log.Infof("Error shutting down HTTP server: %v", err.Error())
	}
}

func (h *InfluxDBV2Listener) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	h.requestsRecv.Incr(1)
	h.mux.ServeHTTP(res, req)
	h.requestsServed.Incr(1)
}

func (h *InfluxDBV2Listener) routes() error {
	credentials := ""
	if !h.Token.Empty() {
		secBuf, err := h.Token.Get()
		if err != nil {
			return err
		}

		credentials = "Token " + secBuf.String()
		secBuf.Destroy()
	}

	authHandler := internal.GenericAuthHandler(credentials,
		func(_ http.ResponseWriter) {
			h.authFailures.Incr(1)
		},
	)

	h.mux.Handle("/api/v2/write", authHandler(h.handleWrite()))
	h.mux.Handle("/api/v2/ready", h.handleReady())
	h.mux.Handle("/", authHandler(h.handleDefault()))

	return nil
}

func (h *InfluxDBV2Listener) handleReady() http.HandlerFunc {
	return func(res http.ResponseWriter, _ *http.Request) {
		defer h.readysServed.Incr(1)

		// respond to ready requests
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(http.StatusOK)
		b, err := json.Marshal(map[string]string{
			"started": h.startTime.Format(time.RFC3339Nano),
			"status":  "ready",
			"up":      h.timeFunc().Sub(h.startTime).String()})
		if err != nil {
			h.Log.Debugf("error marshalling json in handleReady: %v", err)
		}
		if _, err := res.Write(b); err != nil {
			h.Log.Debugf("error writing in handleReady: %v", err)
		}
	}
}

func (h *InfluxDBV2Listener) handleDefault() http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		defer h.notFoundsServed.Incr(1)
		http.NotFound(res, req)
	}
}

func (h *InfluxDBV2Listener) handleWrite() http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		defer h.writesServed.Incr(1)

		// Check that the content length is not too large for us to handle.
		if req.ContentLength > int64(h.MaxBodySize) {
			if err := tooLarge(res, int64(h.MaxBodySize)); err != nil {
				h.Log.Debugf("error in too-large: %v", err)
			}
			return
		}

		bucket := req.URL.Query().Get("bucket")

		body := req.Body
		body = http.MaxBytesReader(res, body, int64(h.MaxBodySize))
		// Handle gzip request bodies
		if req.Header.Get("Content-Encoding") == "gzip" {
			var err error
			body, err = gzip.NewReader(body)
			if err != nil {
				h.Log.Debugf("Error decompressing request body: %v", err.Error())
				if err := badRequest(res, invalid, err.Error()); err != nil {
					h.Log.Debugf("error in bad-request: %v", err)
				}
				return
			}
			defer body.Close()
		}

		var readErr error
		var bytes []byte
		bytes, readErr = io.ReadAll(body)
		if readErr != nil {
			h.Log.Debugf("Error parsing the request body: %v", readErr.Error())
			if err := badRequest(res, internalError, readErr.Error()); err != nil {
				h.Log.Debugf("error in bad-request: %v", err)
			}
			return
		}

		precisionStr := req.URL.Query().Get("precision")

		var metrics []telegraf.Metric
		var err error
		if h.ParserType == "upstream" {
			parser := influx_upstream.Parser{}
			err = parser.Init()
			if !errors.Is(err, io.EOF) && err != nil {
				h.Log.Debugf("Error initializing parser: %v", err.Error())
				return
			}
			parser.SetTimeFunc(influx_upstream.TimeFunc(h.timeFunc))

			if precisionStr != "" {
				precision := getPrecisionMultiplier(precisionStr)
				if err = parser.SetTimePrecision(precision); err != nil {
					h.Log.Debugf("Error setting precision of parser: %v", err)
					return
				}
			}

			metrics, err = parser.Parse(bytes)
		} else {
			parser := influx.Parser{}
			err = parser.Init()
			if !errors.Is(err, io.EOF) && err != nil {
				h.Log.Debugf("Error initializing parser: %v", err.Error())
				return
			}
			parser.SetTimeFunc(h.timeFunc)

			if precisionStr != "" {
				precision := getPrecisionMultiplier(precisionStr)
				parser.SetTimePrecision(precision)
			}

			metrics, err = parser.Parse(bytes)
		}

		if !errors.Is(err, io.EOF) && err != nil {
			h.Log.Debugf("Error parsing the request body: %v", err.Error())
			if err := badRequest(res, invalid, err.Error()); err != nil {
				h.Log.Debugf("error in bad-request: %v", err)
			}
			return
		}

		for _, m := range metrics {
			// Handle bucket_tag override
			if h.BucketTag != "" && bucket != "" {
				m.AddTag(h.BucketTag, bucket)
			}
		}

		if h.MaxUndeliveredMetrics > 0 {
			h.writeWithTracking(res, metrics)
		} else {
			h.write(res, metrics)
		}
	}
}

func (h *InfluxDBV2Listener) writeWithTracking(res http.ResponseWriter, metrics []telegraf.Metric) {
	if len(metrics) > h.MaxUndeliveredMetrics {
		res.WriteHeader(http.StatusRequestEntityTooLarge)
		h.Log.Debugf("status %d, always rejecting batch of %d metrics: larger than max_undelivered_metrics %d",
			http.StatusRequestEntityTooLarge, len(metrics), h.MaxUndeliveredMetrics)
		return
	}

	pending := h.totalUndeliveredMetrics.Load()
	remainingUndeliveredMetrics := int64(h.MaxUndeliveredMetrics) - pending
	if int64(len(metrics)) > remainingUndeliveredMetrics {
		res.WriteHeader(http.StatusTooManyRequests)
		h.Log.Debugf("status %d, rejecting batch of %d metrics: larger than remaining undelivered metrics %d",
			http.StatusTooManyRequests, len(metrics), remainingUndeliveredMetrics)
		return
	}

	h.countLock.Lock()
	trackingID := h.trackingAcc.AddTrackingMetricGroup(metrics)
	h.trackingMetricCount[trackingID] = int64(len(metrics))
	h.totalUndeliveredMetrics.Add(int64(len(metrics)))
	h.countLock.Unlock()

	res.WriteHeader(http.StatusNoContent)
}

func (h *InfluxDBV2Listener) write(res http.ResponseWriter, metrics []telegraf.Metric) {
	for _, m := range metrics {
		h.acc.AddMetric(m)
	}

	res.WriteHeader(http.StatusNoContent)
}

func tooLarge(res http.ResponseWriter, maxLength int64) error {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("X-Influxdb-Error", "http: request body too large")
	res.WriteHeader(http.StatusRequestEntityTooLarge)
	b, err := json.Marshal(map[string]string{
		"code":      fmt.Sprint(invalid),
		"message":   "http: request body too large",
		"maxLength": strconv.FormatInt(maxLength, 10)})
	if err != nil {
		return err
	}
	_, err = res.Write(b)
	return err
}

func badRequest(res http.ResponseWriter, code BadRequestCode, errString string) error {
	res.Header().Set("Content-Type", "application/json")
	if errString == "" {
		errString = "http: bad request"
	}
	res.Header().Set("X-Influxdb-Error", errString)
	res.WriteHeader(http.StatusBadRequest)
	b, err := json.Marshal(map[string]string{
		"code":    fmt.Sprint(code),
		"message": errString,
		"op":      "",
		"err":     errString,
	})
	if err != nil {
		return err
	}
	_, err = res.Write(b)
	return err
}

func getPrecisionMultiplier(precision string) time.Duration {
	// Influxdb defaults silently to nanoseconds if precision isn't
	// one of the following:
	var d time.Duration
	switch precision {
	case "us":
		d = time.Microsecond
	case "ms":
		d = time.Millisecond
	case "s":
		d = time.Second
	default:
		d = time.Nanosecond
	}
	return d
}

func init() {
	inputs.Add("influxdb_v2_listener", func() telegraf.Input {
		return &InfluxDBV2Listener{
			ServiceAddress: ":8086",
			timeFunc:       time.Now,
		}
	})
}
