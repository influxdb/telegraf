//go:generate ../../../tools/readme_config_includer/generator
package sflow

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	maxPacketSize = 64 * 1024
)

type SFlow struct {
	ServiceAddress string      `toml:"service_address"`
	ReadBufferSize config.Size `toml:"read_buffer_size"`

	Log telegraf.Logger `toml:"-"`

	addr    net.Addr
	decoder *packetDecoder
	closer  io.Closer
	wg      sync.WaitGroup
}

func (*SFlow) SampleConfig() string {
	return sampleConfig
}

func (s *SFlow) Init() error {
	s.decoder = newDecoder()
	s.decoder.Log = s.Log
	return nil
}

// Start starts this sFlow listener listening on the configured network for sFlow packets
func (s *SFlow) Start(acc telegraf.Accumulator) error {
	s.decoder.onPacket(func(p *v5Format) {
		metrics := makeMetrics(p)
		for _, m := range metrics {
			acc.AddMetric(m)
		}
	})

	u, err := url.Parse(s.ServiceAddress)
	if err != nil {
		return err
	}

	conn, err := listenUDP(u.Scheme, u.Host)
	if err != nil {
		return err
	}
	s.closer = conn
	s.addr = conn.LocalAddr()

	if s.ReadBufferSize > 0 {
		if err := conn.SetReadBuffer(int(s.ReadBufferSize)); err != nil {
			return err
		}
	}

	s.Log.Infof("Listening on %s://%s", s.addr.Network(), s.addr.String())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.read(acc, conn)
	}()

	return nil
}

// Gather is a NOOP for sFlow as it receives, asynchronously, sFlow network packets
func (*SFlow) Gather(telegraf.Accumulator) error {
	return nil
}

func (s *SFlow) Stop() {
	if s.closer != nil {
		s.closer.Close()
	}
	s.wg.Wait()
}

func (s *SFlow) address() net.Addr {
	return s.addr
}

func (s *SFlow) read(acc telegraf.Accumulator, conn net.PacketConn) {
	buf := make([]byte, maxPacketSize)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if !strings.HasSuffix(err.Error(), ": use of closed network connection") {
				acc.AddError(err)
			}
			break
		}
		s.process(acc, buf[:n])
	}
}

func (s *SFlow) process(acc telegraf.Accumulator, buf []byte) {
	if err := s.decoder.decode(bytes.NewBuffer(buf)); err != nil {
		acc.AddError(fmt.Errorf("unable to parse incoming packet: %w", err))
	}
}

func listenUDP(network, address string) (*net.UDPConn, error) {
	switch network {
	case "udp", "udp4", "udp6":
		addr, err := net.ResolveUDPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return net.ListenUDP(network, addr)
	default:
		return nil, fmt.Errorf("unsupported network type: %s", network)
	}
}

func init() {
	inputs.Add("sflow", func() telegraf.Input {
		return &SFlow{}
	})
}
