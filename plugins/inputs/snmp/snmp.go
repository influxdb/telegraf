//go:generate ../../../tools/readme_config_includer/generator
package snmp

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/snmp"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Snmp struct {
	// The SNMP agent to query. Format is [SCHEME://]ADDR[:PORT] (e.g.
	// udp://1.2.3.4:161).  If the scheme is not specified then "udp" is used.
	Agents []string `toml:"agents"`

	// The tag used to name the agent host
	AgentHostTag string `toml:"agent_host_tag"`

	snmp.ClientConfig

	Tables []snmp.Table `toml:"table"`

	// Name & Fields are the elements of a Table.
	// Telegraf chokes if we try to embed a Table. So instead we have to embed the
	// fields of a Table, and construct a Table during runtime.
	Name   string       `toml:"name"`
	Fields []snmp.Field `toml:"field"`

	Log telegraf.Logger `toml:"-"`

	connectionCache []snmp.Connection

	translator snmp.Translator
}

func (*Snmp) SampleConfig() string {
	return sampleConfig
}

func (s *Snmp) SetTranslator(name string) {
	s.Translator = name
}

func (s *Snmp) Init() error {
	var err error
	switch s.Translator {
	case "gosmi":
		s.translator, err = snmp.NewGosmiTranslator(s.Path, s.Log)
		if err != nil {
			return err
		}
	case "netsnmp":
		s.translator = snmp.NewNetsnmpTranslator(s.Log)
	default:
		return errors.New("invalid translator value")
	}

	s.connectionCache = make([]snmp.Connection, len(s.Agents))

	for i := range s.Tables {
		if err := s.Tables[i].Init(s.translator); err != nil {
			return fmt.Errorf("initializing table %s: %w", s.Tables[i].Name, err)
		}
	}

	for i := range s.Fields {
		if err := s.Fields[i].Init(s.translator); err != nil {
			return fmt.Errorf("initializing field %s: %w", s.Fields[i].Name, err)
		}
	}

	if len(s.AgentHostTag) == 0 {
		s.AgentHostTag = "agent_host"
	}
	if s.AgentHostTag != "source" {
		config.PrintOptionValueDeprecationNotice("inputs.snmp", "agent_host_tag", s.AgentHostTag, telegraf.DeprecationInfo{
			Since:  "1.29.0",
			Notice: `set to "source" for consistent usage across plugins or safely ignore this message and continue to use the current value`,
		})
	}

	return nil
}

func (s *Snmp) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	for i, agent := range s.Agents {
		wg.Add(1)
		go func(i int, agent string) {
			defer wg.Done()
			gs, err := s.getConnection(i)
			if err != nil {
				acc.AddError(fmt.Errorf("agent %s: %w", agent, err))
				return
			}

			// First is the top-level fields. We treat the fields as table prefixes with an empty index.
			t := snmp.Table{
				Name:   s.Name,
				Fields: s.Fields,
			}
			topTags := make(map[string]string)
			if err := s.gatherTable(acc, gs, t, topTags, false); err != nil {
				acc.AddError(fmt.Errorf("agent %s: %w", agent, err))
			}

			// Now is the real tables.
			for _, t := range s.Tables {
				if err := s.gatherTable(acc, gs, t, topTags, true); err != nil {
					acc.AddError(fmt.Errorf("agent %s: gathering table %s: %w", agent, t.Name, err))
				}
			}
		}(i, agent)
	}
	wg.Wait()

	return nil
}

func (s *Snmp) gatherTable(acc telegraf.Accumulator, gs snmp.Connection, t snmp.Table, topTags map[string]string, walk bool) error {
	rt, err := t.Build(gs, walk)
	if err != nil {
		return err
	}

	for _, tr := range rt.Rows {
		if !walk {
			// top-level table. Add tags to topTags.
			for k, v := range tr.Tags {
				topTags[k] = v
			}
		} else {
			// real table. Inherit any specified tags.
			for _, k := range t.InheritTags {
				if v, ok := topTags[k]; ok {
					tr.Tags[k] = v
				}
			}
		}
		if _, ok := tr.Tags[s.AgentHostTag]; !ok {
			tr.Tags[s.AgentHostTag] = gs.Host()
		}
		acc.AddFields(rt.Name, tr.Fields, tr.Tags, rt.Time)
	}

	return nil
}

// getConnection creates a snmpConnection (*gosnmp.GoSNMP) object and caches the
// result using `agentIndex` as the cache key.  This is done to allow multiple
// connections to a single address.  It is an error to use a connection in
// more than one goroutine.
func (s *Snmp) getConnection(idx int) (snmp.Connection, error) {
	if gs := s.connectionCache[idx]; gs != nil {
		if err := gs.Reconnect(); err != nil {
			return gs, fmt.Errorf("reconnecting: %w", err)
		}

		return gs, nil
	}

	agent := s.Agents[idx]

	gs, err := snmp.NewWrapper(s.ClientConfig)
	if err != nil {
		return nil, err
	}

	err = gs.SetAgent(agent)
	if err != nil {
		return nil, err
	}

	s.connectionCache[idx] = gs

	if err := gs.Connect(); err != nil {
		return nil, fmt.Errorf("setting up connection: %w", err)
	}

	return gs, nil
}

func init() {
	inputs.Add("snmp", func() telegraf.Input {
		return &Snmp{
			Name: "snmp",
			ClientConfig: snmp.ClientConfig{
				Retries:        3,
				MaxRepetitions: 10,
				Timeout:        config.Duration(5 * time.Second),
				Version:        2,
				Path:           []string{"/usr/share/snmp/mibs"},
				Community:      "public",
			},
		}
	})
}
