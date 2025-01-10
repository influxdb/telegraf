//go:generate ../../../tools/readme_config_includer/generator
package minecraft

import (
	_ "embed"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Minecraft struct {
	Server   string `toml:"server"`
	Port     string `toml:"port"`
	Password string `toml:"password"`

	client cli
}

// cli is a client for the Minecraft server.
type cli interface {
	// connect establishes a connection to the server.
	connect() error

	// players returns the players on the scoreboard.
	players() ([]string, error)

	// scores returns the objective scores for a player.
	scores(player string) ([]score, error)
}

func (*Minecraft) SampleConfig() string {
	return sampleConfig
}

func (s *Minecraft) Gather(acc telegraf.Accumulator) error {
	if s.client == nil {
		connector := newConnector(s.Server, s.Port, s.Password)
		s.client = newClient(connector)
	}

	players, err := s.client.players()
	if err != nil {
		return err
	}

	for _, player := range players {
		scores, err := s.client.scores(player)
		if err != nil {
			return err
		}

		tags := map[string]string{
			"player": player,
			"server": s.Server + ":" + s.Port,
			"source": s.Server,
			"port":   s.Port,
		}

		var fields = make(map[string]interface{}, len(scores))
		for _, score := range scores {
			fields[score.name] = score.value
		}

		acc.AddFields("minecraft", fields, tags)
	}

	return nil
}

func init() {
	inputs.Add("minecraft", func() telegraf.Input {
		return &Minecraft{
			Server: "localhost",
			Port:   "25575",
		}
	})
}
