package minecraft

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type mockConnection struct {
	commands map[string]string
}

func (c *mockConnection) Execute(command string) (string, error) {
	return c.commands[command], nil
}

type mockConnector struct {
	conn *mockConnection
}

func (c *mockConnector) connect() (connection, error) {
	return c.conn, nil
}

func TestClient_Player(t *testing.T) {
	tests := []struct {
		name     string
		commands map[string]string
		expected []string
	}{
		{
			name: "minecraft 1.12 no players",
			commands: map[string]string{
				"scoreboard players list": "There are no tracked players on the scoreboard",
			},
		},
		{
			name: "minecraft 1.12 single player",
			commands: map[string]string{
				"scoreboard players list": "Showing 1 tracked players on the scoreboard:Etho",
			},
			expected: []string{"Etho"},
		},
		{
			name: "minecraft 1.12 two players",
			commands: map[string]string{
				"scoreboard players list": "Showing 2 tracked players on the scoreboard:Etho and torham",
			},
			expected: []string{"Etho", "torham"},
		},
		{
			name: "minecraft 1.12 three players",
			commands: map[string]string{
				"scoreboard players list": "Showing 3 tracked players on the scoreboard:Etho, notch and torham",
			},
			expected: []string{"Etho", "notch", "torham"},
		},
		{
			name: "minecraft 1.12 players space in username",
			commands: map[string]string{
				"scoreboard players list": "Showing 4 tracked players on the scoreboard:with space, Etho, notch and torham",
			},
			expected: []string{"with space", "Etho", "notch", "torham"},
		},
		{
			name: "minecraft 1.12 players and in username",
			commands: map[string]string{
				"scoreboard players list": "Showing 5 tracked players on the scoreboard:left and right, with space,Etho, notch and torham",
			},
			expected: []string{"left and right", "with space", "Etho", "notch", "torham"},
		},
		{
			name: "minecraft 1.13 no players",
			commands: map[string]string{
				"scoreboard players list": "There are no tracked entities",
			},
		},
		{
			name: "minecraft 1.13 single player",
			commands: map[string]string{
				"scoreboard players list": "There are 1 tracked entities: torham",
			},
			expected: []string{"torham"},
		},
		{
			name: "minecraft 1.13 multiple player",
			commands: map[string]string{
				"scoreboard players list": "There are 3 tracked entities: Etho, notch, torham",
			},
			expected: []string{"Etho", "notch", "torham"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector := &mockConnector{
				conn: &mockConnection{commands: tt.commands},
			}

			client := newClient(connector)
			actual, err := client.players()
			require.NoError(t, err)

			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestClient_Scores(t *testing.T) {
	tests := []struct {
		name     string
		player   string
		commands map[string]string
		expected []score
	}{
		{
			name:   "minecraft 1.12 player with no scores",
			player: "Etho",
			commands: map[string]string{
				"scoreboard players list Etho": "Player Etho has no scores recorded",
			},
		},
		{
			name:   "minecraft 1.12 player with one score",
			player: "Etho",
			commands: map[string]string{
				"scoreboard players list Etho": "Showing 1 tracked objective(s) for Etho:- jump: 2 (jump)",
			},
			expected: []score{
				{name: "jump", value: 2},
			},
		},
		{
			name:   "minecraft 1.12 player with many scores",
			player: "Etho",
			commands: map[string]string{
				"scoreboard players list Etho": "Showing 3 tracked objective(s) for Etho:- hopper: 2 (hopper)- dropper: 2 (dropper)- redstone: 1 (redstone)",
			},
			expected: []score{
				{name: "hopper", value: 2},
				{name: "dropper", value: 2},
				{name: "redstone", value: 1},
			},
		},
		{
			name:   "minecraft 1.13 player with no scores",
			player: "Etho",
			commands: map[string]string{
				"scoreboard players list Etho": "Etho has no scores to show",
			},
		},
		{
			name:   "minecraft 1.13 player with one score",
			player: "Etho",
			commands: map[string]string{
				"scoreboard players list Etho": "Etho has 1 scores:[jumps]: 1",
			},
			expected: []score{
				{name: "jumps", value: 1},
			},
		},
		{
			name:   "minecraft 1.13 player with many scores",
			player: "Etho",
			commands: map[string]string{
				"scoreboard players list Etho": "Etho has 3 scores:[hopper]: 2[dropper]: 2[redstone]: 1",
			},
			expected: []score{
				{name: "hopper", value: 2},
				{name: "dropper", value: 2},
				{name: "redstone", value: 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector := &mockConnector{
				conn: &mockConnection{commands: tt.commands},
			}

			client := newClient(connector)
			actual, err := client.scores(tt.player)
			require.NoError(t, err)

			require.Equal(t, tt.expected, actual)
		})
	}
}
