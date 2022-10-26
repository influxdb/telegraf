//go:generate ../../../tools/readme_config_includer/generator
package netflow

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/netsampler/goflow2/decoders/netflow"
)

//go:embed sample.conf
var sampleConfig string

type protocolDecoder interface {
	Init() error
	Decode(net.IP, []byte) ([]telegraf.Metric, error)
}

type NetFlow struct {
	ServiceAddress string          `toml:"service_address"`
	ReadBufferSize config.Size     `toml:"read_buffer_size"`
	DumpPackets    bool            `toml:"dump_packets"`
	Log            telegraf.Logger `toml:"-"`

	conn    *net.UDPConn
	decoder protocolDecoder
	wg      sync.WaitGroup
}

func (*NetFlow) SampleConfig() string {
	return sampleConfig
}

func (n *NetFlow) Init() error {
	n.decoder = &netflowDecoder{Log: n.Log}
	return n.decoder.Init()
}

func (n *NetFlow) Start(acc telegraf.Accumulator) error {
	u, err := url.Parse(n.ServiceAddress)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "udp", "udp4", "udp6":
	default:
		return fmt.Errorf("invalid scheme %q, should be 'udp', 'udp4' or 'udp6'", u.Scheme)
	}
	addr, err := net.ResolveUDPAddr(u.Scheme, u.Host)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP(u.Scheme, addr)
	if err != nil {
		return err
	}
	n.conn = conn

	if n.ReadBufferSize > 0 {
		if err := conn.SetReadBuffer(int(n.ReadBufferSize)); err != nil {
			return err
		}
	}
	n.Log.Infof("Listening on %s://%s", n.conn.LocalAddr().Network(), n.conn.LocalAddr().String())

	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		n.read(acc)
	}()

	return nil
}

func (n *NetFlow) Stop() {
	if n.conn != nil {
		_ = n.conn.Close()
	}
	n.wg.Wait()
}

func (n *NetFlow) read(acc telegraf.Accumulator) {
	buf := make([]byte, 64*1024) // 64kB
	for {
		count, src, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			if !strings.HasSuffix(err.Error(), ": use of closed network connection") {
				acc.AddError(err)
			}
			break
		}
		n.Log.Debugf("received %d bytes\n", count)
		if n.DumpPackets {
			n.Log.Debugf("raw data: %s", hex.EncodeToString(buf[:count]))
		}
		metrics, err := n.decoder.Decode(src.IP, buf[:count])
		if err != nil {
			switch err.(type) {
			case *netflow.ErrorTemplateNotFound:
				fmt.Printf("template error: %v\n", err)
			default:
				fmt.Printf("decoding error: %v\n", err)
			}
			acc.AddError(err)
			continue
		}
		for _, m := range metrics {
			acc.AddMetric(m)
		}
	}
}

func (n *NetFlow) Gather(acc telegraf.Accumulator) error {
	return nil
}

// Register the plugin
func init() {
	inputs.Add("netflow", func() telegraf.Input {
		return &NetFlow{}
	})
}
