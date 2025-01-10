// Package uwsgi implements a telegraf plugin for collecting uwsgi stats from
// the uwsgi stats server.
//
//go:generate ../../../tools/readme_config_includer/generator
package uwsgi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// Uwsgi server struct
type Uwsgi struct {
	Servers []string        `toml:"servers"`
	Timeout config.Duration `toml:"timeout"`

	client *http.Client
}

func (*Uwsgi) SampleConfig() string {
	return sampleConfig
}

// Gather collect data from uWSGI Server
func (u *Uwsgi) Gather(acc telegraf.Accumulator) error {
	if u.client == nil {
		u.client = &http.Client{
			Timeout: time.Duration(u.Timeout),
		}
	}
	wg := &sync.WaitGroup{}

	for _, s := range u.Servers {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			n, err := url.Parse(s)
			if err != nil {
				acc.AddError(fmt.Errorf("could not parse uWSGI Stats Server url %q: %w", s, err))
				return
			}

			if err := u.gatherServer(acc, n); err != nil {
				acc.AddError(err)
				return
			}
		}(s)
	}

	wg.Wait()

	return nil
}

func (u *Uwsgi) gatherServer(acc telegraf.Accumulator, address *url.URL) error {
	var err error
	var r io.ReadCloser
	var s StatsServer

	switch address.Scheme {
	case "tcp":
		r, err = net.DialTimeout(address.Scheme, address.Host, time.Duration(u.Timeout))
		if err != nil {
			return err
		}
		s.source = address.Host
	case "unix":
		r, err = net.DialTimeout(address.Scheme, address.Path, time.Duration(u.Timeout))
		if err != nil {
			return err
		}
		s.source, err = os.Hostname()
		if err != nil {
			s.source = ""
		}
	case "http":
		resp, err := u.client.Get(address.String()) //nolint:bodyclose // response body is closed after switch
		if err != nil {
			return err
		}
		r = resp.Body
		s.source = address.Host
	default:
		return fmt.Errorf("%q is not a supported scheme", address.Scheme)
	}

	defer r.Close()

	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return fmt.Errorf("failed to decode json payload from %q: %w", address.String(), err)
	}

	gatherStatServer(acc, &s)

	return err
}

func gatherStatServer(acc telegraf.Accumulator, s *StatsServer) {
	fields := map[string]interface{}{
		"listen_queue":        s.ListenQueue,
		"listen_queue_errors": s.ListenQueueErrors,
		"signal_queue":        s.SignalQueue,
		"load":                s.Load,
		"pid":                 s.PID,
	}

	tags := map[string]string{
		"source":  s.source,
		"uid":     strconv.Itoa(s.UID),
		"gid":     strconv.Itoa(s.GID),
		"version": s.Version,
	}
	acc.AddFields("uwsgi_overview", fields, tags)

	gatherWorkers(acc, s)
	gatherApps(acc, s)
	gatherCores(acc, s)
}

func gatherWorkers(acc telegraf.Accumulator, s *StatsServer) {
	for _, w := range s.Workers {
		fields := map[string]interface{}{
			"requests":       w.Requests,
			"accepting":      w.Accepting,
			"delta_request":  w.DeltaRequests,
			"exceptions":     w.Exceptions,
			"harakiri_count": w.HarakiriCount,
			"pid":            w.PID,
			"signals":        w.Signals,
			"signal_queue":   w.SignalQueue,
			"status":         w.Status,
			"rss":            w.Rss,
			"vsz":            w.Vsz,
			"running_time":   w.RunningTime,
			"last_spawn":     w.LastSpawn,
			"respawn_count":  w.RespawnCount,
			"tx":             w.Tx,
			"avg_rt":         w.AvgRt,
		}
		tags := map[string]string{
			"worker_id": strconv.Itoa(w.WorkerID),
			"source":    s.source,
		}

		acc.AddFields("uwsgi_workers", fields, tags)
	}
}

func gatherApps(acc telegraf.Accumulator, s *StatsServer) {
	for _, w := range s.Workers {
		for _, a := range w.Apps {
			fields := map[string]interface{}{
				"modifier1":    a.Modifier1,
				"requests":     a.Requests,
				"startup_time": a.StartupTime,
				"exceptions":   a.Exceptions,
			}
			tags := map[string]string{
				"app_id":    strconv.Itoa(a.AppID),
				"worker_id": strconv.Itoa(w.WorkerID),
				"source":    s.source,
			}
			acc.AddFields("uwsgi_apps", fields, tags)
		}
	}
}

func gatherCores(acc telegraf.Accumulator, s *StatsServer) {
	for _, w := range s.Workers {
		for _, c := range w.Cores {
			fields := map[string]interface{}{
				"requests":           c.Requests,
				"static_requests":    c.StaticRequests,
				"routed_requests":    c.RoutedRequests,
				"offloaded_requests": c.OffloadedRequests,
				"write_errors":       c.WriteErrors,
				"read_errors":        c.ReadErrors,
				"in_request":         c.InRequest,
			}
			tags := map[string]string{
				"core_id":   strconv.Itoa(c.CoreID),
				"worker_id": strconv.Itoa(w.WorkerID),
				"source":    s.source,
			}
			acc.AddFields("uwsgi_cores", fields, tags)
		}
	}
}

func init() {
	inputs.Add("uwsgi", func() telegraf.Input {
		return &Uwsgi{
			Timeout: config.Duration(5 * time.Second),
		}
	})
}

// StatsServer defines the stats server structure.
type StatsServer struct {
	// Tags
	source  string
	PID     int    `json:"pid"`
	UID     int    `json:"uid"`
	GID     int    `json:"gid"`
	Version string `json:"version"`

	// Fields
	ListenQueue       int `json:"listen_queue"`
	ListenQueueErrors int `json:"listen_queue_errors"`
	SignalQueue       int `json:"signal_queue"`
	Load              int `json:"load"`

	Workers []*Worker `json:"workers"`
}

// Worker defines the worker metric structure.
type Worker struct {
	// Tags
	WorkerID int `json:"id"`
	PID      int `json:"pid"`

	// Fields
	Accepting     int    `json:"accepting"`
	Requests      int    `json:"requests"`
	DeltaRequests int    `json:"delta_requests"`
	Exceptions    int    `json:"exceptions"`
	HarakiriCount int    `json:"harakiri_count"`
	Signals       int    `json:"signals"`
	SignalQueue   int    `json:"signal_queue"`
	Status        string `json:"status"`
	Rss           int    `json:"rss"`
	Vsz           int    `json:"vsz"`
	RunningTime   int    `json:"running_time"`
	LastSpawn     int    `json:"last_spawn"`
	RespawnCount  int    `json:"respawn_count"`
	Tx            int    `json:"tx"`
	AvgRt         int    `json:"avg_rt"`

	Apps  []*App  `json:"apps"`
	Cores []*Core `json:"cores"`
}

// App defines the app metric structure.
type App struct {
	// Tags
	AppID int `json:"id"`

	// Fields
	Modifier1   int `json:"modifier1"`
	Requests    int `json:"requests"`
	StartupTime int `json:"startup_time"`
	Exceptions  int `json:"exceptions"`
}

// Core defines the core metric structure.
type Core struct {
	// Tags
	CoreID int `json:"id"`

	// Fields
	Requests          int `json:"requests"`
	StaticRequests    int `json:"static_requests"`
	RoutedRequests    int `json:"routed_requests"`
	OffloadedRequests int `json:"offloaded_requests"`
	WriteErrors       int `json:"write_errors"`
	ReadErrors        int `json:"read_errors"`
	InRequest         int `json:"in_request"`
}
