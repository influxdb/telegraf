//go:generate ../../../tools/readme_config_includer/generator
package rethinkdb

import (
	_ "embed"
	"fmt"
	"net/url"
	"sync"

	"gopkg.in/gorethink/gorethink.v3"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type RethinkDB struct {
	Servers []string
}

var localhost = &Server{URL: &url.URL{Host: "127.0.0.1:28015"}}

func (*RethinkDB) SampleConfig() string {
	return sampleConfig
}

// Reads stats from all configured servers accumulates stats.
// Returns one of the errors encountered while gather stats (if any).
func (r *RethinkDB) Gather(acc telegraf.Accumulator) error {
	if len(r.Servers) == 0 {
		return gatherServer(localhost, acc)
	}

	var wg sync.WaitGroup

	for _, serv := range r.Servers {
		u, err := url.Parse(serv)
		if err != nil {
			acc.AddError(fmt.Errorf("unable to parse to address %q: %w", serv, err))
			continue
		} else if u.Scheme == "" {
			// fallback to simple string based address (i.e. "10.0.0.1:10000")
			u.Host = serv
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc.AddError(gatherServer(&Server{URL: u}, acc))
		}()
	}

	wg.Wait()

	return nil
}

func gatherServer(server *Server, acc telegraf.Accumulator) error {
	var err error
	connectOpts := gorethink.ConnectOpts{
		Address:       server.URL.Host,
		DiscoverHosts: false,
	}
	if server.URL.User != nil {
		pwd, set := server.URL.User.Password()
		if set && pwd != "" {
			connectOpts.AuthKey = pwd
			connectOpts.HandshakeVersion = gorethink.HandshakeV0_4
		}
	}
	if server.URL.Scheme == "rethinkdb2" && server.URL.User != nil {
		pwd, set := server.URL.User.Password()
		if set && pwd != "" {
			connectOpts.Username = server.URL.User.Username()
			connectOpts.Password = pwd
			connectOpts.HandshakeVersion = gorethink.HandshakeV1_0
		}
	}

	server.session, err = gorethink.Connect(connectOpts)
	if err != nil {
		return fmt.Errorf("unable to connect to RethinkDB: %w", err)
	}
	defer server.session.Close()

	return server.gatherData(acc)
}

func init() {
	inputs.Add("rethinkdb", func() telegraf.Input {
		return &RethinkDB{}
	})
}
