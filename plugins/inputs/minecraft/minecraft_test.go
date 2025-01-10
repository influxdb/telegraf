package minecraft

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

type mockClient struct {
	connectF func() error
	playersF func() ([]string, error)
	scoresF  func(player string) ([]score, error)
}

func (c *mockClient) connect() error {
	return c.connectF()
}

func (c *mockClient) players() ([]string, error) {
	return c.playersF()
}

func (c *mockClient) scores(player string) ([]score, error) {
	return c.scoresF(player)
}

func TestGather(t *testing.T) {
	now := time.Unix(0, 0)

	tests := []struct {
		name    string
		client  *mockClient
		metrics []telegraf.Metric
		err     error
	}{
		{
			name: "no players",
			client: &mockClient{
				connectF: func() error {
					return nil
				},
				playersF: func() ([]string, error) {
					return nil, nil
				},
			},
		},
		{
			name: "one player without scores",
			client: &mockClient{
				connectF: func() error {
					return nil
				},
				playersF: func() ([]string, error) {
					return []string{"Etho"}, nil
				},
				scoresF: func(player string) ([]score, error) {
					switch player {
					case "Etho":
						return nil, nil
					default:
						panic("unknown player")
					}
				},
			},
		},
		{
			name: "one player with scores",
			client: &mockClient{
				connectF: func() error {
					return nil
				},
				playersF: func() ([]string, error) {
					return []string{"Etho"}, nil
				},
				scoresF: func(player string) ([]score, error) {
					switch player {
					case "Etho":
						return []score{{name: "jumps", value: 42}}, nil
					default:
						panic("unknown player")
					}
				},
			},
			metrics: []telegraf.Metric{
				testutil.MustMetric(
					"minecraft",
					map[string]string{
						"player": "Etho",
						"server": "example.org:25575",
						"source": "example.org",
						"port":   "25575",
					},
					map[string]interface{}{
						"jumps": 42,
					},
					now,
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Minecraft{
				Server:   "example.org",
				Port:     "25575",
				Password: "xyzzy",
				client:   tt.client,
			}

			var acc testutil.Accumulator
			acc.TimeFunc = func() time.Time { return now }

			err := plugin.Gather(&acc)

			require.Equal(t, tt.err, err)
			testutil.RequireMetricsEqual(t, tt.metrics, acc.GetTelegrafMetrics())
		})
	}
}
