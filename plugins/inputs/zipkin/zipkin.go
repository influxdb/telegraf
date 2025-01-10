//go:generate ../../../tools/readme_config_includer/generator
package zipkin

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/zipkin/trace"
)

//go:embed sample.conf
var sampleConfig string

const (
	// defaultPort is the default port zipkin listens on, which zipkin implementations expect.
	defaultPort = 9411

	// defaultRoute is the default route zipkin uses, and zipkin implementations expect.
	defaultRoute = "/api/v1/spans"

	// defaultShutdownTimeout is the max amount of time telegraf will wait for the plugin to shut down
	defaultShutdownTimeout = 5 * time.Second

	defaultReadTimeout  = 10 * time.Second
	defaultWriteTimeout = 10 * time.Second
)

var (
	// defaultNetwork is the network to listen on; use only in tests.
	defaultNetwork = "tcp"
)

// Recorder represents a type which can record zipkin trace data as well as
// any accompanying errors, and process that data.
type Recorder interface {
	Record(trace.Trace) error
	Error(error)
}

// Handler represents a type which can register itself with a router for
// http routing, and a Recorder for trace data collection.
type Handler interface {
	Register(router *mux.Router, recorder Recorder) error
}

// Zipkin is a telegraf configuration structure for the zipkin input plugin,
// but it also contains fields for the management of a separate, concurrent
// zipkin http server
type Zipkin struct {
	Port         int             `toml:"port"`
	Path         string          `toml:"path"`
	ReadTimeout  config.Duration `toml:"read_timeout"`
	WriteTimeout config.Duration `toml:"write_timeout"`

	Log telegraf.Logger

	address   string
	handler   Handler
	server    *http.Server
	waitGroup *sync.WaitGroup
}

func (*Zipkin) SampleConfig() string {
	return sampleConfig
}

// Gather is empty for the zipkin plugin; all gathering is done through
// the separate goroutine launched in (*Zipkin).Start()
func (*Zipkin) Gather(telegraf.Accumulator) error { return nil }

// Start launches a separate goroutine for collecting zipkin client http requests,
// passing in a telegraf.Accumulator such that data can be collected.
func (z *Zipkin) Start(acc telegraf.Accumulator) error {
	if z.ReadTimeout < config.Duration(time.Second) {
		z.ReadTimeout = config.Duration(defaultReadTimeout)
	}
	if z.WriteTimeout < config.Duration(time.Second) {
		z.WriteTimeout = config.Duration(defaultWriteTimeout)
	}

	z.handler = NewSpanHandler(z.Path)

	var wg sync.WaitGroup
	z.waitGroup = &wg

	router := mux.NewRouter()
	converter := NewLineProtocolConverter(acc)
	if err := z.handler.Register(router, converter); err != nil {
		return err
	}

	z.server = &http.Server{
		Handler:      router,
		ReadTimeout:  time.Duration(z.ReadTimeout),
		WriteTimeout: time.Duration(z.WriteTimeout),
	}

	addr := ":" + strconv.Itoa(z.Port)
	ln, err := net.Listen(defaultNetwork, addr)
	if err != nil {
		return err
	}

	z.address = ln.Addr().String()
	z.Log.Infof("Started the zipkin listener on %s", z.address)

	wg.Add(1)
	go func() {
		defer wg.Done()

		z.Listen(ln, acc)
	}()

	return nil
}

// Stop shuts the internal http server down with via context.Context
func (z *Zipkin) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)

	defer z.waitGroup.Wait()
	defer cancel()

	z.server.Shutdown(ctx) //nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway
}

// Listen creates a http server on the zipkin instance it is called with, and
// serves http until it is stopped by Zipkin's (*Zipkin).Stop()  method.
func (z *Zipkin) Listen(ln net.Listener, acc telegraf.Accumulator) {
	if err := z.server.Serve(ln); err != nil {
		// Because of the clean shutdown in `(*Zipkin).Stop()`
		// We're expecting a server closed error at some point
		// So we don't want to display it as an error.
		// This interferes with telegraf's internal data collection,
		// by making it appear as if a serious error occurred.
		if err != http.ErrServerClosed {
			acc.AddError(fmt.Errorf("error listening: %w", err))
		}
	}
}

func init() {
	inputs.Add("zipkin", func() telegraf.Input {
		return &Zipkin{
			Path: defaultRoute,
			Port: defaultPort,
		}
	})
}
