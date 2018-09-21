package http_listener_ng

import (
	"bytes"
	"compress/gzip"
	"crypto/subtle"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	tlsint "github.com/influxdata/telegraf/internal/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers"
	"github.com/influxdata/telegraf/selfstat"
)

const (
	// DEFAULT_MAX_BODY_SIZE is the default maximum request body size, in bytes.
	// if the request body is over this size, we will return an HTTP 413 error.
	// 500 MB
	DEFAULT_MAX_BODY_SIZE = 500 * 1024 * 1024

	// MAX_LINE_SIZE is the maximum size, in bytes, that can be allocated for
	// a single InfluxDB point.
	// 64 KB
	DEFAULT_MAX_LINE_SIZE = 64 * 1024
)

type TimeFunc func() time.Time

type HTTPListenerNG struct {
	ServiceAddress string
	Paths          []string
	Methods        []string
	ReadTimeout    internal.Duration
	WriteTimeout   internal.Duration
	MaxBodySize    int64
	MaxLineSize    int
	Port           int

	tlsint.ServerConfig

	BasicUsername string
	BasicPassword string

	TimeFunc

	mu sync.Mutex
	wg sync.WaitGroup

	listener net.Listener

	parsers.Parser
	acc  telegraf.Accumulator
	pool *pool

	BytesRecv       selfstat.Stat
	RequestsServed  selfstat.Stat
	WritesServed    selfstat.Stat
	QueriesServed   selfstat.Stat
	PingsServed     selfstat.Stat
	RequestsRecv    selfstat.Stat
	WritesRecv      selfstat.Stat
	QueriesRecv     selfstat.Stat
	PingsRecv       selfstat.Stat
	NotFoundsServed selfstat.Stat
	BuffersCreated  selfstat.Stat
	AuthFailures    selfstat.Stat
}

const sampleConfig = `
  ## Address and port to host HTTP listener on
  service_address = ":8186"

  ## Paths to listen to.
  ## "/query" and "/ping" paths are already taken.
  ## "/query" delivers dummy response.
  ## "/ping" responds to ping requests.
  paths = ["/write"]

  ## HTTP methods to accept.
  methods = ["POST", "PUT"]

  ## maximum duration before timing out read of the request
  read_timeout = "10s"
  ## maximum duration before timing out write of the response
  write_timeout = "10s"

  ## Maximum allowed http request body size in bytes.
  ## 0 means to use the default of 536,870,912 bytes (500 mebibytes)
  max_body_size = 0

  ## Maximum line size allowed to be sent in bytes.
  ## 0 means to use the default of 65536 bytes (64 kibibytes)
  max_line_size = 0

  ## Set one or more allowed client CA certificate file names to 
  ## enable mutually authenticated TLS connections
  tls_allowed_cacerts = ["/etc/telegraf/clientca.pem"]

  ## Add service certificate and key
  tls_cert = "/etc/telegraf/cert.pem"
  tls_key = "/etc/telegraf/key.pem"

  ## Optional username and password to accept for HTTP basic authentication.
  ## You probably want to make sure you have TLS configured above for this.
  # basic_username = "foobar"
  # basic_password = "barfoo"

  ## Data format to consume.
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  # data_format = "influx"
`

func (h *HTTPListenerNG) SampleConfig() string {
	return sampleConfig
}

func (h *HTTPListenerNG) Description() string {
	return "Generic HTTP write listener"
}

func (h *HTTPListenerNG) Gather(_ telegraf.Accumulator) error {
	h.BuffersCreated.Set(h.pool.ncreated())
	return nil
}

func (h *HTTPListenerNG) SetParser(parser parsers.Parser) {
	h.Parser = parser
}

// Start starts the http listener service.
func (h *HTTPListenerNG) Start(acc telegraf.Accumulator) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	tags := map[string]string{
		"address": h.ServiceAddress,
	}
	h.BytesRecv = selfstat.Register("http_listener_ng", "bytes_received", tags)
	h.RequestsServed = selfstat.Register("http_listener_ng", "requests_served", tags)
	h.WritesServed = selfstat.Register("http_listener_ng", "writes_served", tags)
	h.QueriesServed = selfstat.Register("http_listener_ng", "queries_served", tags)
	h.PingsServed = selfstat.Register("http_listener_ng", "pings_served", tags)
	h.RequestsRecv = selfstat.Register("http_listener_ng", "requests_received", tags)
	h.WritesRecv = selfstat.Register("http_listener_ng", "writes_received", tags)
	h.QueriesRecv = selfstat.Register("http_listener_ng", "queries_received", tags)
	h.PingsRecv = selfstat.Register("http_listener_ng", "pings_received", tags)
	h.NotFoundsServed = selfstat.Register("http_listener_ng", "not_founds_served", tags)
	h.BuffersCreated = selfstat.Register("http_listener_ng", "buffers_created", tags)
	h.AuthFailures = selfstat.Register("http_listener_ng", "auth_failures", tags)

	if h.MaxBodySize == 0 {
		h.MaxBodySize = DEFAULT_MAX_BODY_SIZE
	}
	if h.MaxLineSize == 0 {
		h.MaxLineSize = DEFAULT_MAX_LINE_SIZE
	}

	if h.ReadTimeout.Duration < time.Second {
		h.ReadTimeout.Duration = time.Second * 10
	}
	if h.WriteTimeout.Duration < time.Second {
		h.WriteTimeout.Duration = time.Second * 10
	}

	h.acc = acc
	h.pool = NewPool(200, h.MaxLineSize)

	tlsConf, err := h.ServerConfig.TLSConfig()
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:         h.ServiceAddress,
		Handler:      h,
		ReadTimeout:  h.ReadTimeout.Duration,
		WriteTimeout: h.WriteTimeout.Duration,
		TLSConfig:    tlsConf,
	}

	var listener net.Listener
	if tlsConf != nil {
		listener, err = tls.Listen("tcp", h.ServiceAddress, tlsConf)
	} else {
		listener, err = net.Listen("tcp", h.ServiceAddress)
	}
	if err != nil {
		return err
	}
	h.listener = listener
	h.Port = listener.Addr().(*net.TCPAddr).Port

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		server.Serve(h.listener)
	}()

	log.Printf("I! Started HTTP listener NG service on %s\n", h.ServiceAddress)

	return nil
}

// Stop cleans up all resources
func (h *HTTPListenerNG) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.listener.Close()
	h.wg.Wait()

	log.Println("I! Stopped HTTP listener NG service on ", h.ServiceAddress)
}

func (h *HTTPListenerNG) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	h.RequestsRecv.Incr(1)
	defer h.RequestsServed.Incr(1)
	switch {
	case req.URL.Path == "/query":
		h.QueriesRecv.Incr(1)
		defer h.QueriesServed.Incr(1)
		// Deliver a dummy response to the query endpoint, as some InfluxDB
		// clients test endpoint availability with a query
		h.AuthenticateIfSet(func(res http.ResponseWriter, req *http.Request) {
			res.Header().Set("Content-Type", "application/json")
			res.Header().Set("X-Influxdb-Version", "1.0")
			res.WriteHeader(http.StatusOK)
			res.Write([]byte("{\"results\":[]}"))
		}, res, req)
	case req.URL.Path == "/ping":
		h.PingsRecv.Incr(1)
		defer h.PingsServed.Incr(1)
		// respond to ping requests
		h.AuthenticateIfSet(func(res http.ResponseWriter, req *http.Request) {
			res.WriteHeader(http.StatusNoContent)
		}, res, req)
	case contains(req.URL.Path, h.Paths):
		h.WritesRecv.Incr(1)
		defer h.WritesServed.Incr(1)
		h.AuthenticateIfSet(h.serveWrite, res, req)
	default:
		defer h.NotFoundsServed.Incr(1)
		// Don't know how to respond to calls to other endpoints
		h.AuthenticateIfSet(http.NotFound, res, req)
	}
}

func contains(value string, array []string) bool {
	for _, i := range array {
		if i == value {
			return true
		}
	}
	return false
}

func (h *HTTPListenerNG) serveWrite(res http.ResponseWriter, req *http.Request) {
	// Check that the content length is not too large for us to handle.
	if req.ContentLength > h.MaxBodySize {
		tooLarge(res)
		return
	}

	// Check if the requested HTTP method was specified in config.
	isAcceptedMethod := false
	for _, method := range h.Methods {
		if req.Method == method {
			isAcceptedMethod = true
		}
	}
	if !isAcceptedMethod {
		badRequest(res)
		return
	}

	// Handle gzip request bodies
	body := req.Body
	if req.Header.Get("Content-Encoding") == "gzip" {
		var err error
		body, err = gzip.NewReader(req.Body)
		defer body.Close()
		if err != nil {
			log.Println("E! " + err.Error())
			badRequest(res)
			return
		}
	}
	body = http.MaxBytesReader(res, body, h.MaxBodySize)

	var return400 bool
	var hangingBytes bool
	buf := h.pool.get()
	defer h.pool.put(buf)
	bufStart := 0
	for {
		n, err := io.ReadFull(body, buf[bufStart:])
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			log.Println("E! " + err.Error())
			// problem reading the request body
			badRequest(res)
			return
		}
		h.BytesRecv.Incr(int64(n))

		if err == io.EOF {
			if return400 {
				badRequest(res)
			} else {
				res.WriteHeader(http.StatusNoContent)
			}
			return
		}

		if hangingBytes {
			i := bytes.IndexByte(buf, '\n')
			if i == -1 {
				// still didn't find a newline, keep scanning
				continue
			}
			// rotate the bit remaining after the first newline to the front of the buffer
			i++ // start copying after the newline
			bufStart = len(buf) - i
			if bufStart > 0 {
				copy(buf, buf[i:])
			}
			hangingBytes = false
			continue
		}

		if err == io.ErrUnexpectedEOF {
			// finished reading the request body
			if err := h.parse(buf[:n+bufStart]); err != nil {
				log.Println("E! " + err.Error())
				return400 = true
			}
			if return400 {
				badRequest(res)
			} else {
				res.WriteHeader(http.StatusNoContent)
			}
			return
		}

		// if we got down here it means that we filled our buffer, and there
		// are still bytes remaining to be read. So we will parse up until the
		// final newline, then push the rest of the bytes into the next buffer.
		i := bytes.LastIndexByte(buf, '\n')
		if i == -1 {
			// drop any line longer than the max buffer size
			log.Printf("E! http_listener_ng received a single line longer than the maximum of %d bytes",
				len(buf))
			hangingBytes = true
			return400 = true
			bufStart = 0
			continue
		}
		if err := h.parse(buf[:i+1]); err != nil {
			log.Println("E! " + err.Error())
			return400 = true
		}
		// rotate the bit remaining after the last newline to the front of the buffer
		i++ // start copying after the newline
		bufStart = len(buf) - i
		if bufStart > 0 {
			copy(buf, buf[i:])
		}
	}
}

func (h *HTTPListenerNG) parse(b []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	metrics, err := h.Parse(b)
	if err != nil {
		return err
	}

	for _, m := range metrics {
		h.acc.AddFields(m.Name(), m.Fields(), m.Tags(), m.Time())
	}

	return err
}

func tooLarge(res http.ResponseWriter) {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("X-Influxdb-Version", "1.0")
	res.WriteHeader(http.StatusRequestEntityTooLarge)
	res.Write([]byte(`{"error":"http: request body too large"}`))
}

func badRequest(res http.ResponseWriter) {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("X-Influxdb-Version", "1.0")
	res.WriteHeader(http.StatusBadRequest)
	res.Write([]byte(`{"error":"http: bad request"}`))
}

func (h *HTTPListenerNG) AuthenticateIfSet(handler http.HandlerFunc, res http.ResponseWriter, req *http.Request) {
	if h.BasicUsername != "" && h.BasicPassword != "" {
		reqUsername, reqPassword, ok := req.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(reqUsername), []byte(h.BasicUsername)) != 1 ||
			subtle.ConstantTimeCompare([]byte(reqPassword), []byte(h.BasicPassword)) != 1 {

			h.AuthFailures.Incr(1)
			http.Error(res, "Unauthorized.", http.StatusUnauthorized)
			return
		}
		handler(res, req)
	} else {
		handler(res, req)
	}
}

func init() {
	parser, _ := parsers.NewInfluxParser()

	inputs.Add("http_listener_ng", func() telegraf.Input {
		return &HTTPListenerNG{
			ServiceAddress: ":8186",
			TimeFunc:       time.Now,
			Parser:         parser,
			Paths:          []string{"/write"},
			Methods:        []string{"POST", "PUT"},
		}
	})
}
