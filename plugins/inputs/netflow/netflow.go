//go:generate ../../../tools/readme_config_includer/generator
package netflow

import (
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
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

type NetFlow struct {
	ServiceAddress string          `toml:"service_address"`
	ReadBufferSize config.Size     `toml:"read_buffer_size"`
	Protocol       string          `toml:"protocol"`
	DumpPackets    bool            `toml:"dump_packets" deprecated:"1.35.0;use 'log_level' 'trace' instead"`
	PENFiles       []string        `toml:"private_enterprise_number_files"`
	Log            telegraf.Logger `toml:"-"`

	conn    *net.UDPConn
	decoder protocolDecoder
	wg      sync.WaitGroup
}

type protocolDecoder interface {
	init() error
	decode(net.IP, []byte) ([]telegraf.Metric, error)
}

func (*NetFlow) SampleConfig() string {
	return sampleConfig
}

func (n *NetFlow) Init() error {
	if n.ServiceAddress == "" {
		return errors.New("service_address required")
	}
	u, err := url.Parse(n.ServiceAddress)
	if err != nil {
		return fmt.Errorf("invalid service address %q: %w", n.ServiceAddress, err)
	}
	switch u.Scheme {
	case "udp", "udp4", "udp6":
	default:
		return fmt.Errorf("invalid scheme %q, should be 'udp', 'udp4' or 'udp6'", u.Scheme)
	}

	switch strings.ToLower(n.Protocol) {
	case "netflow v9":
		if len(n.PENFiles) != 0 {
			n.Log.Warn("'private_enterprise_number_files' option will be ignored in 'netflow v9'")
		}
		n.decoder = &netflowDecoder{
			log: n.Log,
		}
	case "", "ipfix":
		n.decoder = &netflowDecoder{
			penFiles: n.PENFiles,
			log:      n.Log,
		}
	case "netflow v5":
		if len(n.PENFiles) != 0 {
			n.Log.Warn("'private_enterprise_number_files' option will be ignored in 'netflow v5'")
		}
		n.decoder = &netflowv5Decoder{}
	case "sflow", "sflow v5":
		n.decoder = &sflowv5Decoder{log: n.Log}
	default:
		return fmt.Errorf("invalid protocol %q, only supports 'sflow', 'netflow v5', 'netflow v9' and 'ipfix'", n.Protocol)
	}

	return n.decoder.init()
}

func (n *NetFlow) Start(acc telegraf.Accumulator) error {
	u, err := url.Parse(n.ServiceAddress)
	if err != nil {
		return err
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

func (*NetFlow) Gather(telegraf.Accumulator) error {
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
		if count < 1 {
			continue
		}
		if n.Log.Level().Includes(telegraf.Trace) || n.DumpPackets { // for backward compatibility
			n.Log.Tracef("raw data: %s", hex.EncodeToString(buf[:count]))
		}
		metrics, err := n.decoder.decode(src.IP, buf[:count])
		if err != nil {
			errWithData := fmt.Errorf("%w; raw data: %s", err, hex.EncodeToString(buf[:count]))
			acc.AddError(errWithData)
			continue
		}
		for _, m := range metrics {
			acc.AddMetric(m)
		}
	}
}

// Register the plugin
func init() {
	inputs.Add("netflow", func() telegraf.Input {
		return &NetFlow{}
	})
}
