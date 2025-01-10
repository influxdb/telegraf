package socket

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
)

type CallbackData func(net.Addr, []byte, time.Time)
type CallbackConnection func(net.Addr, io.ReadCloser)
type CallbackError func(error)

type listener interface {
	address() net.Addr
	listenData(CallbackData, CallbackError)
	listenConnection(CallbackConnection, CallbackError)
	close() error
}

type Config struct {
	MaxConnections       uint64           `toml:"max_connections"`
	ReadBufferSize       config.Size      `toml:"read_buffer_size"`
	ReadTimeout          config.Duration  `toml:"read_timeout"`
	KeepAlivePeriod      *config.Duration `toml:"keep_alive_period"`
	SocketMode           string           `toml:"socket_mode"`
	ContentEncoding      string           `toml:"content_encoding"`
	MaxDecompressionSize config.Size      `toml:"max_decompression_size"`
	MaxParallelParsers   int              `toml:"max_parallel_parsers"`
	common_tls.ServerConfig
}

type Socket struct {
	Config

	url           *url.URL
	interfaceName string
	tlsCfg        *tls.Config
	log           telegraf.Logger

	splitter bufio.SplitFunc
	listener listener
}

func (cfg *Config) NewSocket(address string, splitcfg *SplitConfig, logger telegraf.Logger) (*Socket, error) {
	s := &Socket{
		Config: *cfg,
		log:    logger,
	}

	// Setup the splitter if given
	if splitcfg != nil {
		splitter, err := splitcfg.NewSplitter()
		if err != nil {
			return nil, err
		}
		s.splitter = splitter
	}

	// Resolve the interface to an address if any given
	ifregex := regexp.MustCompile(`%([\w\.]+)`)
	if matches := ifregex.FindStringSubmatch(address); len(matches) == 2 {
		s.interfaceName = matches[1]
		address = strings.Replace(address, "%"+s.interfaceName, "", 1)
	}

	// Preparing TLS configuration
	tlsCfg, err := s.ServerConfig.TLSConfig()
	if err != nil {
		return nil, fmt.Errorf("getting TLS config failed: %w", err)
	}
	s.tlsCfg = tlsCfg

	// Parse and check the address
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parsing address failed: %w", err)
	}
	s.url = u

	switch s.url.Scheme {
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket",
		"udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram", "vsock":
	default:
		return nil, fmt.Errorf("unknown protocol %q in %q", u.Scheme, address)
	}

	return s, nil
}

func (s *Socket) Setup() error {
	s.MaxParallelParsers = max(s.MaxParallelParsers, 1)
	switch s.url.Scheme {
	case "tcp", "tcp4", "tcp6":
		l := newStreamListener(
			s.Config,
			s.splitter,
			s.log,
		)

		if err := l.setupTCP(s.url, s.tlsCfg); err != nil {
			return err
		}
		s.listener = l
	case "unix", "unixpacket":
		l := newStreamListener(
			s.Config,
			s.splitter,
			s.log,
		)

		if err := l.setupUnix(s.url, s.tlsCfg, s.SocketMode); err != nil {
			return err
		}
		s.listener = l
	case "udp", "udp4", "udp6":
		l := newPacketListener(s.ContentEncoding, s.MaxDecompressionSize, s.MaxParallelParsers)
		if err := l.setupUDP(s.url, s.interfaceName, int(s.ReadBufferSize)); err != nil {
			return err
		}
		s.listener = l
	case "ip", "ip4", "ip6":
		l := newPacketListener(s.ContentEncoding, s.MaxDecompressionSize, s.MaxParallelParsers)
		if err := l.setupIP(s.url); err != nil {
			return err
		}
		s.listener = l
	case "unixgram":
		l := newPacketListener(s.ContentEncoding, s.MaxDecompressionSize, s.MaxParallelParsers)
		if err := l.setupUnixgram(s.url, s.SocketMode, int(s.ReadBufferSize)); err != nil {
			return err
		}
		s.listener = l
	case "vsock":
		l := newStreamListener(
			s.Config,
			s.splitter,
			s.log,
		)

		if err := l.setupVsock(s.url); err != nil {
			return err
		}
		s.listener = l
	default:
		return fmt.Errorf("unknown protocol %q", s.url.Scheme)
	}

	return nil
}

func (s *Socket) Listen(onData CallbackData, onError CallbackError) {
	s.listener.listenData(onData, onError)
}

func (s *Socket) ListenConnection(onConnection CallbackConnection, onError CallbackError) {
	s.listener.listenConnection(onConnection, onError)
}

func (s *Socket) Close() {
	if s.listener != nil {
		// Ignore the returned error as we cannot do anything about it anyway
		if err := s.listener.close(); err != nil {
			s.log.Warnf("Closing socket failed: %v", err)
		}
		s.listener = nil
	}
}

func (s *Socket) Address() net.Addr {
	return s.listener.address()
}
