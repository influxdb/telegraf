//go:generate ../../../tools/readme_config_includer/generator
package powerdns_recursor

import (
	_ "embed"
	"fmt"
	"strconv"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const defaultTimeout = 5 * time.Second

type PowerdnsRecursor struct {
	UnixSockets            []string `toml:"unix_sockets"`
	SocketDir              string   `toml:"socket_dir"`
	SocketMode             string   `toml:"socket_mode"`
	ControlProtocolVersion int      `toml:"control_protocol_version"`

	Log telegraf.Logger `toml:"-"`

	mode             uint32
	gatherFromServer func(address string, acc telegraf.Accumulator) error
}

func (*PowerdnsRecursor) SampleConfig() string {
	return sampleConfig
}

func (p *PowerdnsRecursor) Init() error {
	if p.SocketMode != "" {
		mode, err := strconv.ParseUint(p.SocketMode, 8, 32)
		if err != nil {
			return fmt.Errorf("could not parse socket_mode: %w", err)
		}

		p.mode = uint32(mode)
	}

	if p.SocketDir == "" {
		p.SocketDir = "/var/run"
	}

	switch p.ControlProtocolVersion {
	// We treat 0 the same as 1 since it's the default value if a user doesn't explicitly specify one.
	case 0, 1:
		p.gatherFromServer = p.gatherFromV1Server
	case 2:
		p.gatherFromServer = p.gatherFromV2Server
	case 3:
		p.gatherFromServer = gatherFromV3Server
	default:
		return fmt.Errorf("unknown control protocol version '%d', allowed values are 1, 2, 3", p.ControlProtocolVersion)
	}

	if len(p.UnixSockets) == 0 {
		p.UnixSockets = []string{"/var/run/pdns_recursor.controlsocket"}
	}

	return nil
}

func (p *PowerdnsRecursor) Gather(acc telegraf.Accumulator) error {
	for _, serverSocket := range p.UnixSockets {
		if err := p.gatherFromServer(serverSocket, acc); err != nil {
			acc.AddError(err)
		}
	}

	return nil
}

func init() {
	inputs.Add("powerdns_recursor", func() telegraf.Input {
		return &PowerdnsRecursor{
			mode: uint32(0666),
		}
	})
}
