//go:generate ../../../tools/readme_config_includer/generator
package influxdb_listener

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
	defaultMaxBodySize = 32 * 1024 * 1024
)

type InfluxDBListener struct {
	ServiceAddress string `toml:"service_address"`
	port           int
	common_tls.ServerConfig

	ReadTimeout        config.Duration `toml:"read_timeout"`
	WriteTimeout       config.Duration `toml:"write_timeout"`
	MaxBodySize        config.Size     `toml:"max_body_size"`
	MaxLineSize        config.Size     `toml:"max_line_size" deprecated:"1.14.0;1.35.0;parser now handles lines of unlimited length and option is ignored"`
	BasicUsername      string          `toml:"basic_username"`
	BasicPassword      string          `toml:"basic_password"`
	TokenSharedSecret  string          `toml:"token_shared_secret"`
	TokenUsername      string          `toml:"token_username"`
	DatabaseTag        string          `toml:"database_tag"`
	RetentionPolicyTag string          `toml:"retention_policy_tag"`
	ParserType         string          `toml:"parser_type"`

	timeFunc influx.TimeFunc

	listener net.Listener
	server   http.Server

	acc telegraf.Accumulator

	bytesRecv       selfstat.Stat
	requestsServed  selfstat.Stat
	writesServed    selfstat.Stat
	queriesServed   selfstat.Stat
	pingsServed     selfstat.Stat
	requestsRecv    selfstat.Stat
	notFoundsServed selfstat.Stat
	buffersCreated  selfstat.Stat
	authFailures    selfstat.Stat

	Log telegraf.Logger `toml:"-"`

	mux http.ServeMux
}

func (*InfluxDBListener) SampleConfig() string {
	return sampleConfig
}

func (*InfluxDBListener) Gather(telegraf.Accumulator) error {
	return nil
}

func (h *InfluxDBListener) Init() error {
	// Check the config setting
	if (h.BasicUsername != "" || h.BasicPassword != "") && (h.TokenSharedSecret != "" || h.TokenUsername != "") {
		return errors.New("cannot use basic-auth and tokens at the same time")
	}
	if h.TokenSharedSecret != "" && h.TokenUsername == "" || h.TokenSharedSecret == "" && h.TokenUsername != "" {
		return errors.New("neither 'token_shared_secret' nor 'token_username' can be empty for token authentication")
	}

	tags := map[string]string{
		"address": h.ServiceAddress,
	}
	h.bytesRecv = selfstat.Register("influxdb_listener", "bytes_received", tags)
	h.requestsServed = selfstat.Register("influxdb_listener", "requests_served", tags)
	h.writesServed = selfstat.Register("influxdb_listener", "writes_served", tags)
	h.queriesServed = selfstat.Register("influxdb_listener", "queries_served", tags)
	h.pingsServed = selfstat.Register("influxdb_listener", "pings_served", tags)
	h.requestsRecv = selfstat.Register("influxdb_listener", "requests_received", tags)
	h.notFoundsServed = selfstat.Register("influxdb_listener", "not_founds_served", tags)
	h.buffersCreated = selfstat.Register("influxdb_listener", "buffers_created", tags)
	h.authFailures = selfstat.Register("influxdb_listener", "auth_failures", tags)
	h.routes()

	if h.MaxBodySize == 0 {
		h.MaxBodySize = config.Size(defaultMaxBodySize)
	}

	if h.ReadTimeout < config.Duration(time.Second) {
		h.ReadTimeout = config.Duration(time.Second * 10)
	}
	if h.WriteTimeout < config.Duration(time.Second) {
		h.WriteTimeout = config.Duration(time.Second * 10)
	}

	return nil
}

func (h *InfluxDBListener) Start(acc telegraf.Accumulator) error {
	h.acc = acc

	tlsConf, err := h.ServerConfig.TLSConfig()
	if err != nil {
		return err
	}

	h.server = http.Server{
		Addr:         h.ServiceAddress,
		Handler:      h,
		ReadTimeout:  time.Duration(h.ReadTimeout),
		WriteTimeout: time.Duration(h.WriteTimeout),
		TLSConfig:    tlsConf,
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

	h.Log.Infof("Started HTTP listener service on %s", h.ServiceAddress)

	return nil
}

func (h *InfluxDBListener) Stop() {
	err := h.server.Shutdown(context.Background())
	if err != nil {
		h.Log.Infof("Error shutting down HTTP server: %v", err.Error())
	}
}

func (h *InfluxDBListener) routes() {
	var authHandler func(http.Handler) http.Handler
	if h.TokenSharedSecret != "" {
		authHandler = internal.JWTAuthHandler(h.TokenSharedSecret, h.TokenUsername,
			func(_ http.ResponseWriter) {
				h.authFailures.Incr(1)
			},
		)
	} else {
		authHandler = internal.BasicAuthHandler(h.BasicUsername, h.BasicPassword, "influxdb",
			func(_ http.ResponseWriter) {
				h.authFailures.Incr(1)
			},
		)
	}

	h.mux.Handle("/write", authHandler(h.handleWrite()))
	h.mux.Handle("/query", authHandler(h.handleQuery()))
	h.mux.Handle("/ping", h.handlePing())
	h.mux.Handle("/", authHandler(h.handleDefault()))
}

func (h *InfluxDBListener) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	h.requestsRecv.Incr(1)
	h.mux.ServeHTTP(res, req)
	h.requestsServed.Incr(1)
}

func (h *InfluxDBListener) handleQuery() http.HandlerFunc {
	return func(res http.ResponseWriter, _ *http.Request) {
		defer h.queriesServed.Incr(1)
		// Deliver a dummy response to the query endpoint, as some InfluxDB
		// clients test endpoint availability with a query
		res.Header().Set("Content-Type", "application/json")
		res.Header().Set("X-Influxdb-Version", "1.0")
		res.WriteHeader(http.StatusOK)
		_, err := res.Write([]byte("{\"results\":[]}"))
		if err != nil {
			h.Log.Debugf("error writing result in handleQuery: %v", err)
		}
	}
}

func (h *InfluxDBListener) handlePing() http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		defer h.pingsServed.Incr(1)
		verbose := req.URL.Query().Get("verbose")

		// respond to ping requests
		res.Header().Set("X-Influxdb-Version", "1.0")
		if verbose != "" && verbose != "0" && verbose != "false" {
			res.Header().Set("Content-Type", "application/json")
			res.WriteHeader(http.StatusOK)
			b, err := json.Marshal(map[string]string{"version": "1.0"}) // based on header set above
			if err != nil {
				h.Log.Debugf("error marshalling json in handlePing: %v", err)
			}
			if _, err := res.Write(b); err != nil {
				h.Log.Debugf("error writing result in handlePing: %v", err)
			}
		} else {
			res.WriteHeader(http.StatusNoContent)
		}
	}
}

func (h *InfluxDBListener) handleDefault() http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		defer h.notFoundsServed.Incr(1)
		http.NotFound(res, req)
	}
}

func (h *InfluxDBListener) handleWrite() http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		if h.ParserType == "upstream" {
			h.handleWriteUpstreamParser(res, req)
		} else {
			h.handleWriteInternalParser(res, req)
		}
	}
}

func (h *InfluxDBListener) handleWriteInternalParser(res http.ResponseWriter, req *http.Request) {
	defer h.writesServed.Incr(1)
	// Check that the content length is not too large for us to handle.
	if req.ContentLength > int64(h.MaxBodySize) {
		if err := tooLarge(res); err != nil {
			h.Log.Debugf("error in too-large: %v", err)
		}
		return
	}

	db := req.URL.Query().Get("db")
	rp := req.URL.Query().Get("rp")

	body := req.Body
	body = http.MaxBytesReader(res, body, int64(h.MaxBodySize))
	// Handle gzip request bodies
	if req.Header.Get("Content-Encoding") == "gzip" {
		var err error
		body, err = gzip.NewReader(body)
		if err != nil {
			h.Log.Debugf("Error decompressing request body: %v", err.Error())
			if err := badRequest(res, err.Error()); err != nil {
				h.Log.Debugf("error in bad-request: %v", err)
			}
			return
		}
		defer body.Close()
	}

	parser := influx.NewStreamParser(body)
	parser.SetTimeFunc(h.timeFunc)

	precisionStr := req.URL.Query().Get("precision")
	if precisionStr != "" {
		precision := getPrecisionMultiplier(precisionStr)
		parser.SetTimePrecision(precision)
	}

	var m telegraf.Metric
	var err error
	var parseErrorCount int
	var lastPos int
	var firstParseErrorStr string
	for {
		select {
		case <-req.Context().Done():
			// Shutting down before parsing is finished.
			res.WriteHeader(http.StatusServiceUnavailable)
			return
		default:
		}

		m, err = parser.Next()
		pos := parser.Position()
		h.bytesRecv.Incr(int64(pos - lastPos))
		lastPos = pos

		// Continue parsing metrics even if some are malformed
		var parseErr *influx.ParseError
		if errors.As(err, &parseErr) {
			parseErrorCount++
			errStr := parseErr.Error()
			if firstParseErrorStr == "" {
				firstParseErrorStr = errStr
			}
			continue
		} else if err != nil {
			// Either we're exiting cleanly (err ==
			// influx.EOF) or there's an unexpected error
			break
		}

		if h.DatabaseTag != "" && db != "" {
			m.AddTag(h.DatabaseTag, db)
		}

		if h.RetentionPolicyTag != "" && rp != "" {
			m.AddTag(h.RetentionPolicyTag, rp)
		}

		h.acc.AddMetric(m)
	}
	if !errors.Is(err, influx.EOF) {
		h.Log.Debugf("Error parsing the request body: %v", err.Error())
		if err := badRequest(res, err.Error()); err != nil {
			h.Log.Debugf("error in bad-request: %v", err)
		}
		return
	}
	if parseErrorCount > 0 {
		var partialErrorString string
		switch parseErrorCount {
		case 1:
			partialErrorString = firstParseErrorStr
		case 2:
			partialErrorString = firstParseErrorStr + " (and 1 other parse error)"
		default:
			partialErrorString = fmt.Sprintf("%s (and %d other parse errors)", firstParseErrorStr, parseErrorCount-1)
		}
		if err := partialWrite(res, partialErrorString); err != nil {
			h.Log.Debugf("error in partial-write: %v", err)
		}
		return
	}

	// http request success
	res.WriteHeader(http.StatusNoContent)
}

func (h *InfluxDBListener) handleWriteUpstreamParser(res http.ResponseWriter, req *http.Request) {
	defer h.writesServed.Incr(1)
	// Check that the content length is not too large for us to handle.
	if req.ContentLength > int64(h.MaxBodySize) {
		if err := tooLarge(res); err != nil {
			h.Log.Debugf("error in too-large: %v", err)
		}
		return
	}

	db := req.URL.Query().Get("db")
	rp := req.URL.Query().Get("rp")

	body := req.Body
	body = http.MaxBytesReader(res, body, int64(h.MaxBodySize))
	// Handle gzip request bodies
	if req.Header.Get("Content-Encoding") == "gzip" {
		var err error
		body, err = gzip.NewReader(body)
		if err != nil {
			h.Log.Debugf("Error decompressing request body: %v", err.Error())
			if err := badRequest(res, err.Error()); err != nil {
				h.Log.Debugf("error in bad-request: %v", err)
			}
			return
		}
		defer body.Close()
	}

	parser := influx_upstream.NewStreamParser(body)
	parser.SetTimeFunc(influx_upstream.TimeFunc(h.timeFunc))

	precisionStr := req.URL.Query().Get("precision")
	if precisionStr != "" {
		precision := getPrecisionMultiplier(precisionStr)
		err := parser.SetTimePrecision(precision)
		if err != nil {
			h.Log.Debugf("error in upstream parser: %v", err)
			if err := badRequest(res, err.Error()); err != nil {
				h.Log.Debugf("error in bad-request: %v", err)
			}
			return
		}
	}

	if req.ContentLength >= 0 {
		h.bytesRecv.Incr(req.ContentLength)
	}

	var m telegraf.Metric
	var err error
	var parseErrorCount int
	var firstParseErrorStr string
	for {
		select {
		case <-req.Context().Done():
			// Shutting down before parsing is finished.
			res.WriteHeader(http.StatusServiceUnavailable)
			return
		default:
		}

		m, err = parser.Next()

		// Continue parsing metrics even if some are malformed
		var parseErr *influx_upstream.ParseError
		if errors.As(err, &parseErr) {
			parseErrorCount++
			errStr := parseErr.Error()
			if firstParseErrorStr == "" {
				firstParseErrorStr = errStr
			}
			continue
		} else if err != nil {
			// Either we're exiting cleanly (err ==
			// influx.ErrEOF) or there's an unexpected error
			break
		}

		if h.DatabaseTag != "" && db != "" {
			m.AddTag(h.DatabaseTag, db)
		}

		if h.RetentionPolicyTag != "" && rp != "" {
			m.AddTag(h.RetentionPolicyTag, rp)
		}

		h.acc.AddMetric(m)
	}
	if !errors.Is(err, io.EOF) {
		h.Log.Debugf("Error parsing the request body: %v", err.Error())
		if err := badRequest(res, err.Error()); err != nil {
			h.Log.Debugf("error in bad-request: %v", err)
		}
		return
	}
	if parseErrorCount > 0 {
		var partialErrorString string
		switch parseErrorCount {
		case 1:
			partialErrorString = firstParseErrorStr
		case 2:
			partialErrorString = firstParseErrorStr + " (and 1 other parse error)"
		default:
			partialErrorString = fmt.Sprintf("%s (and %d other parse errors)", firstParseErrorStr, parseErrorCount-1)
		}
		if err := partialWrite(res, partialErrorString); err != nil {
			h.Log.Debugf("error in partial-write: %v", err)
		}
		return
	}

	// http request success
	res.WriteHeader(http.StatusNoContent)
}

func tooLarge(res http.ResponseWriter) error {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("X-Influxdb-Version", "1.0")
	res.Header().Set("X-Influxdb-Error", "http: request body too large")
	res.WriteHeader(http.StatusRequestEntityTooLarge)
	_, err := res.Write([]byte(`{"error":"http: request body too large"}`))
	return err
}

func badRequest(res http.ResponseWriter, errString string) error {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("X-Influxdb-Version", "1.0")
	if errString == "" {
		errString = "http: bad request"
	}
	res.Header().Set("X-Influxdb-Error", errString)
	res.WriteHeader(http.StatusBadRequest)
	_, err := fmt.Fprintf(res, `{"error":%q}`, errString)
	return err
}

func partialWrite(res http.ResponseWriter, errString string) error {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("X-Influxdb-Version", "1.0")
	res.Header().Set("X-Influxdb-Error", errString)
	res.WriteHeader(http.StatusBadRequest)
	_, err := fmt.Fprintf(res, `{"error":%q}`, errString)
	return err
}

func getPrecisionMultiplier(precision string) time.Duration {
	// Influxdb defaults silently to nanoseconds if precision isn't
	// one of the following:
	var d time.Duration
	switch precision {
	case "u":
		d = time.Microsecond
	case "ms":
		d = time.Millisecond
	case "s":
		d = time.Second
	case "m":
		d = time.Minute
	case "h":
		d = time.Hour
	default:
		d = time.Nanosecond
	}
	return d
}

func init() {
	// http_listener deprecated in 1.9
	inputs.Add("http_listener", func() telegraf.Input {
		return &InfluxDBListener{
			ServiceAddress: ":8186",
			timeFunc:       time.Now,
		}
	})
	inputs.Add("influxdb_listener", func() telegraf.Input {
		return &InfluxDBListener{
			ServiceAddress: ":8186",
			timeFunc:       time.Now,
		}
	})
}
