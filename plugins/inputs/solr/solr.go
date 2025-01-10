//go:generate ../../../tools/readme_config_includer/generator
package solr

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// Solr is a plugin to read stats from one or many Solr servers
type Solr struct {
	Servers     []string        `toml:"servers"`
	Username    string          `toml:"username"`
	Password    string          `toml:"password"`
	HTTPTimeout config.Duration `toml:"timeout"`
	Cores       []string        `toml:"cores"`
	Log         telegraf.Logger `toml:"-"`

	client  *http.Client
	configs map[string]*apiConfig
	filter  filter.Filter
}

func (*Solr) SampleConfig() string {
	return sampleConfig
}

func (s *Solr) Init() error {
	// Setup client to do the queries
	s.client = &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: time.Duration(s.HTTPTimeout),
		},
		Timeout: time.Duration(s.HTTPTimeout),
	}

	// Prepare filter for the cores to query
	f, err := filter.Compile(s.Cores)
	if err != nil {
		return err
	}
	s.filter = f

	// Allocate config cache
	s.configs = make(map[string]*apiConfig, len(s.Servers))

	return nil
}

func (s *Solr) Start(_ telegraf.Accumulator) error {
	for _, server := range s.Servers {
		// Simply fill the cache for all available servers
		_ = s.getAPIConfig(server)
	}
	return nil
}

func (*Solr) Stop() {}

func (s *Solr) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	for _, srv := range s.Servers {
		wg.Add(1)
		go func(server string) {
			defer wg.Done()

			// Check the server version from cache or query one
			cfg := s.getAPIConfig(server)
			s.collect(acc, cfg, server)
		}(srv)
	}
	wg.Wait()

	return nil
}

func (s *Solr) getAPIConfig(server string) *apiConfig {
	if cfg, found := s.configs[server]; found {
		return cfg
	}

	version, err := s.determineServerAPIVersion(server)
	if err != nil {
		s.Log.Errorf("Getting version for %q failed: %v", server, err)
		// Exit early and do not fill the cache as the server might not be
		// reachable yet.
		return newAPIv1Config()
	}
	s.Log.Debugf("Found API version %d for server %q...", version, server)

	switch version {
	case 0:
		s.Log.Warn("Unable to determine API version! Using API v1...")
		s.configs[server] = newAPIv1Config()
	case 1:
		s.configs[server] = newAPIv1Config()
	case 2:
		s.configs[server] = newAPIv2Config()
	default:
		s.Log.Warnf("Unknown API version %q! Using latest known", version)
		s.configs[server] = newAPIv2Config()
	}

	return s.configs[server]
}

func (s *Solr) collect(acc telegraf.Accumulator, cfg *apiConfig, server string) {
	now := time.Now()

	var coreStatus adminCoresStatus
	if err := s.query(cfg.adminEndpoint(server), &coreStatus); err != nil {
		acc.AddError(err)
		return
	}

	var wg sync.WaitGroup
	for core, metrics := range coreStatus.Status {
		fields := map[string]interface{}{
			"deleted_docs":  metrics.Index.DeletedDocs,
			"max_docs":      metrics.Index.MaxDoc,
			"num_docs":      metrics.Index.NumDocs,
			"size_in_bytes": metrics.Index.SizeInBytes,
		}
		tags := map[string]string{"core": core}
		acc.AddFields("solr_admin", fields, tags, now)

		if s.filter != nil && !s.filter.Match(core) {
			continue
		}

		wg.Add(1)
		go func(server string, core string) {
			defer wg.Done()

			var data mBeansData
			if err := s.query(cfg.mbeansEndpoint(server, core), &data); err != nil {
				acc.AddError(err)
				return
			}

			cfg.parseCore(acc, core, &data, now)
			cfg.parseQueryHandler(acc, core, &data, now)
			cfg.parseUpdateHandler(acc, core, &data, now)
			cfg.parseCache(acc, core, &data, now)
		}(server, core)
	}
	wg.Wait()
}

func (s *Solr) query(endpoint string, v interface{}) error {
	req, reqErr := http.NewRequest(http.MethodGet, endpoint, nil)
	if reqErr != nil {
		return reqErr
	}

	if s.Username != "" {
		req.SetBasicAuth(s.Username, s.Password)
	}

	req.Header.Set("User-Agent", internal.ProductToken())

	r, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("solr: API endpoint %q responded with %q", endpoint, r.Status)
	}

	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Solr) determineServerAPIVersion(server string) (int, error) {
	endpoint := server + "/solr/admin/info/system?wt=json"
	var info map[string]interface{}
	if err := s.query(endpoint, &info); err != nil {
		return 0, err
	}

	lraw, found := info["lucene"]
	if !found {
		return 0, nil
	}
	lucene, ok := lraw.(map[string]interface{})
	if !ok {
		return 0, nil
	}
	vraw, ok := lucene["solr-spec-version"]
	if !ok {
		return 0, nil
	}
	v, ok := vraw.(string)
	if !ok {
		return 0, nil
	}

	// API version 1 is required until v7.x
	version := semver.New(v)
	if version.LessThan(semver.Version{Major: 7}) {
		return 1, nil
	}

	// Starting from 7.0 API version 2 has to be used to get the UPDATE and
	// QUERY metrics.
	return 2, nil
}

func init() {
	inputs.Add("solr", func() telegraf.Input {
		return &Solr{
			HTTPTimeout: config.Duration(time.Second * 5),
		}
	})
}
