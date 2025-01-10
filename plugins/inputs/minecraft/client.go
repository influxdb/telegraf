package minecraft

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gorcon/rcon"
)

var (
	scoreboardRegexLegacy = regexp.MustCompile(`(?U):\s(?P<value>\d+)\s\((?P<name>.*)\)`)
	scoreboardRegex       = regexp.MustCompile(`\[(?P<name>[^\]]+)\]: (?P<value>\d+)`)
)

// connection is an established connection to the Minecraft server.
type connection interface {
	// Execute runs a command.
	Execute(command string) (string, error)
}

// conn is used to create connections to the Minecraft server.
type conn interface {
	// connect establishes a connection to the server.
	connect() (connection, error)
}

func newConnector(hostname, port, password string) *connector {
	return &connector{
		hostname: hostname,
		port:     port,
		password: password,
	}
}

type connector struct {
	hostname string
	port     string
	password string
}

func (c *connector) connect() (connection, error) {
	client, err := rcon.Dial(c.hostname+":"+c.port, c.password)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func newClient(connector conn) *client {
	return &client{connector: connector}
}

type client struct {
	connector conn
	conn      connection
}

func (c *client) connect() error {
	conn, err := c.connector.connect()
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *client) players() ([]string, error) {
	if c.conn == nil {
		err := c.connect()
		if err != nil {
			return nil, err
		}
	}

	resp, err := c.conn.Execute("scoreboard players list")
	if err != nil {
		c.conn = nil
		return nil, err
	}

	return parsePlayers(resp), nil
}

func (c *client) scores(player string) ([]score, error) {
	if c.conn == nil {
		err := c.connect()
		if err != nil {
			return nil, err
		}
	}

	resp, err := c.conn.Execute("scoreboard players list " + player)
	if err != nil {
		c.conn = nil
		return nil, err
	}

	return parseScores(resp), nil
}

func parsePlayers(input string) []string {
	parts := strings.SplitAfterN(input, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	names := strings.Split(parts[1], ",")

	// Detect Minecraft <= 1.12
	if strings.Contains(parts[0], "players on the scoreboard") && len(names) > 0 {
		// Split the last two player names: ex: "notch and dinnerbone"
		head := names[:len(names)-1]
		tail := names[len(names)-1]
		names = append(head, strings.SplitN(tail, " and ", 2)...)
	}

	players := make([]string, 0, len(names))
	for _, name := range names {
		name := strings.TrimSpace(name)
		if name == "" {
			continue
		}
		players = append(players, name)
	}
	return players
}

// score is an individual tracked scoreboard stat.
type score struct {
	name  string
	value int64
}

func parseScores(input string) []score {
	if strings.Contains(input, "has no scores") {
		return nil
	}

	// Detect Minecraft <= 1.12
	var re *regexp.Regexp
	if strings.Contains(input, "tracked objective") {
		re = scoreboardRegexLegacy
	} else {
		re = scoreboardRegex
	}

	matches := re.FindAllStringSubmatch(input, -1)
	scores := make([]score, 0, len(matches))
	for _, match := range matches {
		score := score{}
		for i, subexp := range re.SubexpNames() {
			switch subexp {
			case "name":
				score.name = match[i]
			case "value":
				value, err := strconv.ParseInt(match[i], 10, 64)
				if err != nil {
					continue
				}
				score.value = value
			default:
				continue
			}
		}
		scores = append(scores, score)
	}

	return scores
}
