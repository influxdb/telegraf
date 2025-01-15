//go:generate ../../../tools/readme_config_includer/generator
package mavlink

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

type Mavlink struct {
	URL                    string   `toml:"url"`
	SystemID               uint8    `toml:"system_id"`
	FilterPattern          []string `toml:"filter"`
	StreamRequestEnable    bool     `toml:"stream_request_enable"`
	StreamRequestFrequency int      `toml:"stream_request_frequency"`

	Log telegraf.Logger `toml:"-"`

	filter         filter.Filter
	connection     *gomavlib.Node
	endpointConfig []gomavlib.EndpointConf
	cancel         context.CancelFunc
}

//go:embed sample.conf
var sampleConfig string

func (*Mavlink) SampleConfig() string {
	return sampleConfig
}

func (s *Mavlink) Init() error {
	// Parse out the Mavlink endpoint.
	// Try to parse the URL
	u, err := url.Parse(s.URL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	// Split host and port, and use default port if it was not specified
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		// Use default port if we could not parse out the port.
		host = u.Host
		port = "14550"
	}

	switch u.Scheme {
	case "serial":
		// Serial client
		// Parse serial URL by hand, because it is not a compliant URL.
		baudRate := 57600
		device, rate, found := strings.Cut(strings.TrimPrefix(s.URL, "serial://"), ":")
		if found {
			r, err := strconv.Atoi(rate)
			if err != nil {
				return fmt.Errorf("serial baud rate not valid: %w", err)
			}
			baudRate = r
		}

		s.endpointConfig = []gomavlib.EndpointConf{
			gomavlib.EndpointSerial{
				Device: device,
				Baud:   baudRate,
			},
		}
	case "tcp":
		if len(host) > 0 {
			s.endpointConfig = []gomavlib.EndpointConf{
				gomavlib.EndpointTCPClient{
					Address: host + ":" + port,
				},
			}
		} else {
			s.endpointConfig = []gomavlib.EndpointConf{
				gomavlib.EndpointTCPServer{
					Address: ":" + port,
				},
			}
		}

	case "udp":
		if len(host) > 0 {
			s.endpointConfig = []gomavlib.EndpointConf{
				gomavlib.EndpointUDPClient{
					Address: host + ":" + port,
				},
			}
		} else {
			s.endpointConfig = []gomavlib.EndpointConf{
				gomavlib.EndpointUDPServer{
					Address: ":" + port,
				},
			}
		}

	default:
		return fmt.Errorf("could not parse url %s", s.URL)
	}

	// Compile filter
	s.filter, err = filter.Compile(s.FilterPattern)
	if err != nil {
		return err
	}

	return nil
}

func (s *Mavlink) Start(acc telegraf.Accumulator) error {
	// Start MAVLink endpoint
	connection, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints:              s.endpointConfig,
		Dialect:                ardupilotmega.Dialect,
		OutVersion:             gomavlib.V2,
		OutSystemID:            s.SystemID,
		StreamRequestEnable:    s.StreamRequestEnable,
		StreamRequestFrequency: s.StreamRequestFrequency,
	})
	if err != nil {
		return &internal.StartupError{
			Err:   fmt.Errorf("connecting to mavlink endpoint %s failed: %w", s.URL, err),
			Retry: true,
		}
	}
	s.connection = connection
	ctx, cancelFunc := context.WithCancel(context.Background())
	s.cancel = cancelFunc

	// Start routine to connect to Mavlink and stream out data async
	go func(ctx context.Context) {
		defer s.connection.Close()
		if ctx.Err() != nil {
			return
		}

		// Process MAVLink messages
		// Use reflection to retrieve and handle all message types.
		// (There are several hundred Mavlink message types)
		for evt := range s.connection.Events() {
			if ctx.Err() != nil {
				return
			}
			switch evt := evt.(type) {
			case *gomavlib.EventFrame:
				result := convertEventFrameToMetric(evt, s.filter)

				if result != nil {
					result.AddTag("source", s.URL)
					acc.AddMetric(result)
				}

			case *gomavlib.EventChannelOpen:
				s.Log.Debugf("Mavlink channel opened")

			case *gomavlib.EventChannelClose:
				s.Log.Debugf("Mavlink channel closed")
			}
		}
	}(ctx)

	return nil
}

func (*Mavlink) Gather(telegraf.Accumulator) error {
	return nil
}

func (s *Mavlink) Stop() {
	s.cancel()
}

func init() {
	inputs.Add("mavlink", func() telegraf.Input {
		return &Mavlink{
			URL:                    "udp://:14540",
			FilterPattern:          make([]string, 0),
			SystemID:               254,
			StreamRequestEnable:    true,
			StreamRequestFrequency: 4,
		}
	})
}
