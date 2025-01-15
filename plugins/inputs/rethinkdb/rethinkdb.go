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

var localhost = &server{url: &url.URL{Host: "127.0.0.1:28015"}}

type RethinkDB struct {
	Servers []string `toml:"servers"`
}

func (*RethinkDB) SampleConfig() string {
	return sampleConfig
}

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
			acc.AddError(gatherServer(&server{url: u}, acc))
		}()
	}

	wg.Wait()

	return nil
}

func gatherServer(server *server, acc telegraf.Accumulator) error {
	var err error
	connectOpts := gorethink.ConnectOpts{
		Address:       server.url.Host,
		DiscoverHosts: false,
	}
	if server.url.User != nil {
		pwd, set := server.url.User.Password()
		if set && pwd != "" {
			connectOpts.AuthKey = pwd
			connectOpts.HandshakeVersion = gorethink.HandshakeV0_4
		}
	}
	if server.url.Scheme == "rethinkdb2" && server.url.User != nil {
		pwd, set := server.url.User.Password()
		if set && pwd != "" {
			connectOpts.Username = server.url.User.Username()
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
