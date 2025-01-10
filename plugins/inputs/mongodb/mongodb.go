//go:generate ../../../tools/readme_config_includer/generator
package mongodb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var disconnectedServersBehaviors = []string{"error", "skip"}

type MongoDB struct {
	Servers                     []string `toml:"servers"`
	GatherClusterStatus         bool     `toml:"gather_cluster_status"`
	GatherPerdbStats            bool     `toml:"gather_perdb_stats"`
	GatherColStats              bool     `toml:"gather_col_stats"`
	GatherTopStat               bool     `toml:"gather_top_stat"`
	DisconnectedServersBehavior string   `toml:"disconnected_servers_behavior"`
	ColStatsDbs                 []string `toml:"col_stats_dbs"`
	common_tls.ClientConfig
	Ssl ssl

	Log telegraf.Logger `toml:"-"`

	clients   []*server
	tlsConfig *tls.Config
}

type ssl struct {
	Enabled bool     `toml:"ssl_enabled" deprecated:"1.3.0;1.35.0;use 'tls_*' options instead"`
	CaCerts []string `toml:"cacerts" deprecated:"1.3.0;1.35.0;use 'tls_ca' instead"`
}

func (*MongoDB) SampleConfig() string {
	return sampleConfig
}

func (m *MongoDB) Init() error {
	if m.DisconnectedServersBehavior == "" {
		m.DisconnectedServersBehavior = "error"
	}

	if err := choice.Check(m.DisconnectedServersBehavior, disconnectedServersBehaviors); err != nil {
		return fmt.Errorf("disconnected_servers_behavior: %w", err)
	}

	if m.Ssl.Enabled {
		// Deprecated TLS config
		m.tlsConfig = &tls.Config{
			InsecureSkipVerify: m.ClientConfig.InsecureSkipVerify,
		}
		if len(m.Ssl.CaCerts) == 0 {
			return errors.New("you must explicitly set insecure_skip_verify to skip certificate validation")
		}

		roots := x509.NewCertPool()
		for _, caCert := range m.Ssl.CaCerts {
			if ok := roots.AppendCertsFromPEM([]byte(caCert)); !ok {
				return errors.New("failed to parse root certificate")
			}
		}
		m.tlsConfig.RootCAs = roots
	} else {
		var err error
		m.tlsConfig, err = m.ClientConfig.TLSConfig()
		if err != nil {
			return err
		}
	}

	if len(m.Servers) == 0 {
		m.Servers = []string{"mongodb://127.0.0.1:27017"}
	}

	return nil
}

// Start runs after init and setup mongodb connections
func (m *MongoDB) Start(telegraf.Accumulator) error {
	for _, connURL := range m.Servers {
		if err := m.setupConnection(connURL); err != nil {
			return err
		}
	}

	return nil
}

func (m *MongoDB) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	for _, client := range m.clients {
		wg.Add(1)
		go func(srv *server) {
			defer wg.Done()
			if m.DisconnectedServersBehavior == "skip" {
				if err := srv.ping(); err != nil {
					m.Log.Debugf("Failed to ping server: %s", err)
					return
				}
			}

			err := srv.gatherData(acc, m.GatherClusterStatus, m.GatherPerdbStats, m.GatherColStats, m.GatherTopStat, m.ColStatsDbs)
			if err != nil {
				m.Log.Errorf("Failed to gather data: %s", err)
			}
		}(client)
	}

	wg.Wait()
	return nil
}

// Stop disconnects mongo connections when stop or reload
func (m *MongoDB) Stop() {
	for _, server := range m.clients {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := server.client.Disconnect(ctx); err != nil {
			m.Log.Errorf("Disconnecting from %q failed: %v", server.hostname, err)
		}
		cancel()
	}
}

func (m *MongoDB) setupConnection(connURL string) error {
	if !strings.HasPrefix(connURL, "mongodb://") && !strings.HasPrefix(connURL, "mongodb+srv://") {
		// Preserve backwards compatibility for hostnames without a
		// scheme, broken in go 1.8. Remove in Telegraf 2.0
		connURL = "mongodb://" + connURL
		m.Log.Warnf("Using %q as connection URL; please update your configuration to use an URL", connURL)
	}

	u, err := url.Parse(connURL)
	if err != nil {
		return fmt.Errorf("unable to parse connection URL: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := options.Client().ApplyURI(connURL)
	if m.tlsConfig != nil {
		opts.TLSConfig = m.tlsConfig
	}
	if opts.ReadPreference == nil {
		opts.ReadPreference = readpref.Nearest()
	}

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return fmt.Errorf("unable to connect to MongoDB: %w", err)
	}

	err = client.Ping(ctx, opts.ReadPreference)
	if err != nil {
		if m.DisconnectedServersBehavior == "error" {
			return fmt.Errorf("unable to ping MongoDB: %w", err)
		}

		m.Log.Errorf("Unable to ping MongoDB: %s", err)
	}

	server := &server{
		client:   client,
		hostname: u.Host,
		log:      m.Log,
	}
	m.clients = append(m.clients, server)
	return nil
}

func init() {
	inputs.Add("mongodb", func() telegraf.Input {
		return &MongoDB{
			GatherClusterStatus: true,
			GatherPerdbStats:    false,
			GatherColStats:      false,
			GatherTopStat:       false,
			ColStatsDbs:         []string{"local"},
		}
	})
}
