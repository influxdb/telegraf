//go:generate ../../../tools/config_includer/generator
//go:generate ../../../tools/readme_config_includer/generator
package syslog

import (
	_ "embed"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/nontransparent"
	"github.com/leodido/go-syslog/v4/octetcounting"
	"github.com/leodido/go-syslog/v4/rfc3164"
	"github.com/leodido/go-syslog/v4/rfc5424"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/socket"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const readTimeoutMsg = "Read timeout set! Connections, inactive for the set duration, will be closed!"

type Syslog struct {
	Address        string                     `toml:"server"`
	Framing        string                     `toml:"framing"`
	SyslogStandard string                     `toml:"syslog_standard"`
	Trailer        nontransparent.TrailerType `toml:"trailer"`
	BestEffort     bool                       `toml:"best_effort"`
	Separator      string                     `toml:"sdparam_separator"`
	Log            telegraf.Logger            `toml:"-"`
	socket.Config

	mu sync.Mutex
	wg sync.WaitGroup

	url    *url.URL
	socket *socket.Socket
}

func (*Syslog) SampleConfig() string {
	return sampleConfig
}

func (s *Syslog) Init() error {
	// Check settings and set defaults
	switch s.Framing {
	case "":
		s.Framing = "octet-counting"
	case "octet-counting", "non-transparent":
	default:
		return fmt.Errorf("invalid 'framing' %q", s.Framing)
	}

	switch s.SyslogStandard {
	case "":
		s.SyslogStandard = "RFC5424"
	case "RFC3164", "RFC5424":
	default:
		return fmt.Errorf("invalid 'syslog_standard' %q", s.SyslogStandard)
	}

	if s.Separator == "" {
		s.Separator = "_"
	}

	// Check and parse address, set default if necessary
	if s.Address == "" {
		s.Address = "tcp://127.0.0.1:6514"
	}

	if !strings.Contains(s.Address, "://") {
		return fmt.Errorf("missing protocol within address %q", s.Address)
	}

	u, err := url.Parse(s.Address)
	if err != nil {
		return fmt.Errorf("parsing address %q failed: %w", s.Address, err)
	}

	// Check if we do have a port and add the default one if not
	if u.Port() == "" {
		u.Host += ":6514"
	}
	s.url = u

	switch s.url.Scheme {
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
		if s.ReadTimeout > 0 {
			s.Log.Warn(readTimeoutMsg)
		}
	case "udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram":
	default:
		return fmt.Errorf("unknown protocol %q in %q", u.Scheme, s.Address)
	}

	// Create a socket
	sock, err := s.Config.NewSocket(u.String(), nil, s.Log)
	if err != nil {
		return err
	}
	s.socket = sock

	return nil
}

func (s *Syslog) Start(acc telegraf.Accumulator) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Setup the listener
	if err := s.socket.Setup(); err != nil {
		return err
	}
	addr := s.socket.Address()
	s.Log.Infof("Listening on %s://%s", addr.Network(), addr.String())

	// Setup the callbacks and start listening
	onError := func(err error) {
		acc.AddError(err)
	}
	switch s.url.Scheme {
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
		onConnection := s.createStreamDataHandler(acc)
		s.socket.ListenConnection(onConnection, onError)
	case "udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram":
		onData := s.createDatagramDataHandler(acc)
		s.socket.Listen(onData, onError)
	default:
		return fmt.Errorf("unknown protocol %q in %q", s.url.Scheme, s.Address)
	}

	return nil
}

func (*Syslog) Gather(telegraf.Accumulator) error {
	return nil
}

func (s *Syslog) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.socket.Close()
	s.wg.Wait()
}

func (s *Syslog) createStreamDataHandler(acc telegraf.Accumulator) socket.CallbackConnection {
	// Create parser options
	var opts []syslog.ParserOption
	if s.BestEffort {
		opts = append(opts, syslog.WithBestEffort())
	}
	if s.Framing == "non-transparent" {
		opts = append(opts, nontransparent.WithTrailer(s.Trailer))
	}

	return func(src net.Addr, reader io.ReadCloser) {
		// Create the parser depending on transport framing and other settings
		var parser syslog.Parser
		switch s.Framing {
		case "octet-counting":
			parser = octetcounting.NewParser(opts...)
		case "non-transparent":
			parser = nontransparent.NewParser(opts...)
		}

		// Remove port from address
		var addr string
		if src.Network() != "unix" {
			var err error
			if addr, _, err = net.SplitHostPort(src.String()); err != nil {
				addr = src.String()
			}
		}

		parser.WithListener(func(r *syslog.Result) {
			if r.Error != nil {
				acc.AddError(r.Error)
			}
			if r.Message == nil {
				return
			}

			// Extract message information
			acc.AddFields("syslog", fields(r.Message, s.Separator), tags(r.Message, addr))
		})
		parser.Parse(reader)
	}
}

func (s *Syslog) createDatagramDataHandler(acc telegraf.Accumulator) socket.CallbackData {
	// Create the parser depending on syslog standard and other settings
	var parser syslog.Machine
	switch s.SyslogStandard {
	case "RFC3164":
		parser = rfc3164.NewParser(rfc3164.WithYear(rfc3164.CurrentYear{}))
	case "RFC5424":
		parser = rfc5424.NewParser()
	}
	if s.BestEffort {
		parser.WithBestEffort()
	}

	// Return the OnData function
	return func(src net.Addr, data []byte, _ time.Time) {
		message, err := parser.Parse(data)
		if err != nil {
			acc.AddError(err)
		} else if message == nil {
			acc.AddError(fmt.Errorf("unable to parse message: %s", string(data)))
		}
		if message == nil {
			return
		}

		// Extract message information
		var addr string
		if src.Network() != "unixgram" {
			var err error
			if addr, _, err = net.SplitHostPort(src.String()); err != nil {
				addr = src.String()
			}
		}
		acc.AddFields("syslog", fields(message, s.Separator), tags(message, addr))
	}
}

func tags(msg syslog.Message, src string) map[string]string {
	// Extract message information
	tags := map[string]string{
		"severity": *msg.SeverityShortLevel(),
		"facility": *msg.FacilityLevel(),
	}

	if src != "" {
		tags["source"] = src
	}

	switch msg := msg.(type) {
	case *rfc5424.SyslogMessage:
		if msg.Hostname != nil {
			tags["hostname"] = *msg.Hostname
		}
		if msg.Appname != nil {
			tags["appname"] = *msg.Appname
		}
	case *rfc3164.SyslogMessage:
		if msg.Hostname != nil {
			tags["hostname"] = *msg.Hostname
		}
		if msg.Appname != nil {
			tags["appname"] = *msg.Appname
		}
	}

	return tags
}

func fields(msg syslog.Message, separator string) map[string]interface{} {
	var fields map[string]interface{}
	switch msg := msg.(type) {
	case *rfc5424.SyslogMessage:
		fields = map[string]interface{}{
			"facility_code": int(*msg.Facility),
			"severity_code": int(*msg.Severity),
			"version":       msg.Version,
		}
		if msg.Timestamp != nil {
			fields["timestamp"] = (*msg.Timestamp).UnixNano()
		}
		if msg.ProcID != nil {
			fields["procid"] = *msg.ProcID
		}
		if msg.MsgID != nil {
			fields["msgid"] = *msg.MsgID
		}
		if msg.Message != nil {
			fields["message"] = strings.TrimRightFunc(*msg.Message, func(r rune) bool {
				return unicode.IsSpace(r)
			})
		}
		if msg.StructuredData != nil {
			for sdid, sdparams := range *msg.StructuredData {
				if len(sdparams) == 0 {
					// When SD-ID does not have params we indicate its presence with a bool
					fields[sdid] = true
					continue
				}
				for k, v := range sdparams {
					fields[sdid+separator+k] = v
				}
			}
		}
	case *rfc3164.SyslogMessage:
		fields = map[string]interface{}{
			"facility_code": int(*msg.Facility),
			"severity_code": int(*msg.Severity),
		}
		if msg.Timestamp != nil {
			fields["timestamp"] = (*msg.Timestamp).UnixNano()
		}
		if msg.ProcID != nil {
			fields["procid"] = *msg.ProcID
		}
		if msg.MsgID != nil {
			fields["msgid"] = *msg.MsgID
		}
		if msg.Message != nil {
			fields["message"] = strings.TrimRightFunc(*msg.Message, func(r rune) bool {
				return unicode.IsSpace(r)
			})
		}
	}

	return fields
}

func init() {
	inputs.Add("syslog", func() telegraf.Input {
		return &Syslog{
			Trailer: nontransparent.LF,
		}
	})
}
