//go:generate ../../../tools/readme_config_includer/generator
package riemann_listener

import (
	"bytes"
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	riemann "github.com/riemann/riemann-go-client"
	rieman_proto "github.com/riemann/riemann-go-client/proto"
	"google.golang.org/protobuf/proto"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type RiemannSocketListener struct {
	ServiceAddress  string           `toml:"service_address"`
	MaxConnections  int              `toml:"max_connections"`
	ReadBufferSize  config.Size      `toml:"read_buffer_size"`
	ReadTimeout     *config.Duration `toml:"read_timeout"`
	KeepAlivePeriod *config.Duration `toml:"keep_alive_period"`
	SocketMode      string           `toml:"socket_mode"`
	common_tls.ServerConfig

	Log telegraf.Logger `toml:"-"`

	wg sync.WaitGroup
	telegraf.Accumulator
}

type setReadBufferer interface {
	SetReadBuffer(sizeInBytes int) error
}

type riemannListener struct {
	net.Listener
	*RiemannSocketListener

	sockType string

	connections    map[string]net.Conn
	connectionsMtx sync.Mutex
}

func (*RiemannSocketListener) SampleConfig() string {
	return sampleConfig
}

func (rsl *RiemannSocketListener) Start(acc telegraf.Accumulator) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	go rsl.processOsSignals(cancelFunc)
	rsl.Accumulator = acc
	if rsl.ServiceAddress == "" {
		rsl.Log.Warnf("Using default service_address tcp://:5555")
		rsl.ServiceAddress = "tcp://:5555"
	}
	spl := strings.SplitN(rsl.ServiceAddress, "://", 2)
	if len(spl) != 2 {
		return fmt.Errorf("invalid service address: %s", rsl.ServiceAddress)
	}

	protocol := spl[0]
	addr := spl[1]

	switch protocol {
	case "tcp", "tcp4", "tcp6":
		tlsCfg, err := rsl.ServerConfig.TLSConfig()
		if err != nil {
			return err
		}

		var l net.Listener
		if tlsCfg == nil {
			l, err = net.Listen(protocol, addr)
		} else {
			l, err = tls.Listen(protocol, addr, tlsCfg)
		}
		if err != nil {
			return err
		}

		rsl.Log.Infof("Listening on %s://%s", protocol, l.Addr())

		rsl := &riemannListener{
			Listener:              l,
			RiemannSocketListener: rsl,
			sockType:              spl[0],
		}

		rsl.wg = sync.WaitGroup{}
		rsl.wg.Add(1)
		go func() {
			defer rsl.wg.Done()
			rsl.listen(ctx)
		}()
	default:
		return fmt.Errorf("unknown protocol %q in %q", protocol, rsl.ServiceAddress)
	}

	return nil
}

func (*RiemannSocketListener) Gather(telegraf.Accumulator) error {
	return nil
}

func (rsl *RiemannSocketListener) Stop() {
	rsl.wg.Done()
	rsl.wg.Wait()
}

func (rsl *riemannListener) listen(ctx context.Context) {
	rsl.connections = make(map[string]net.Conn)

	wg := sync.WaitGroup{}

	select {
	case <-ctx.Done():
		rsl.closeAllConnections()
		wg.Wait()
		return
	default:
		for {
			c, err := rsl.Accept()
			if err != nil {
				if !strings.HasSuffix(err.Error(), ": use of closed network connection") {
					rsl.Log.Error(err.Error())
				}
				break
			}

			if rsl.ReadBufferSize > 0 {
				if srb, ok := c.(setReadBufferer); ok {
					if err := srb.SetReadBuffer(int(rsl.ReadBufferSize)); err != nil {
						rsl.Log.Warnf("Setting read buffer failed: %v", err)
					}
				} else {
					rsl.Log.Warnf("Unable to set read buffer on a %s socket", rsl.sockType)
				}
			}

			rsl.connectionsMtx.Lock()
			if rsl.MaxConnections > 0 && len(rsl.connections) >= rsl.MaxConnections {
				rsl.connectionsMtx.Unlock()
				if err := c.Close(); err != nil {
					rsl.Log.Warnf("Closing the connection failed: %v", err)
				}
				continue
			}
			rsl.connections[c.RemoteAddr().String()] = c
			rsl.connectionsMtx.Unlock()

			if err := rsl.setKeepAlive(c); err != nil {
				rsl.Log.Errorf("Unable to configure keep alive %q: %s", rsl.ServiceAddress, err.Error())
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				rsl.read(c)
			}()
		}
		rsl.closeAllConnections()
		wg.Wait()
	}
}

func (rsl *riemannListener) closeAllConnections() {
	rsl.connectionsMtx.Lock()
	for _, c := range rsl.connections {
		if err := c.Close(); err != nil {
			rsl.Log.Warnf("Closing the connection failed: %v", err.Error())
		}
	}
	rsl.connectionsMtx.Unlock()
}

func (rsl *riemannListener) setKeepAlive(c net.Conn) error {
	if rsl.KeepAlivePeriod == nil {
		return nil
	}
	tcpc, ok := c.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("cannot set keep alive on a %s socket", strings.SplitN(rsl.ServiceAddress, "://", 2)[0])
	}
	if *rsl.KeepAlivePeriod == 0 {
		return tcpc.SetKeepAlive(false)
	}
	if err := tcpc.SetKeepAlive(true); err != nil {
		return err
	}
	return tcpc.SetKeepAlivePeriod(time.Duration(*rsl.KeepAlivePeriod))
}

func (rsl *riemannListener) removeConnection(c net.Conn) {
	rsl.connectionsMtx.Lock()
	delete(rsl.connections, c.RemoteAddr().String())
	rsl.connectionsMtx.Unlock()
}

/*
readMessages will read Riemann messages in binary format
from the TCP connection. byte Array p size will depend on the size
of the riemann  message as sent by the client
*/
func readMessages(r io.Reader, p []byte) error {
	for len(p) > 0 {
		n, err := r.Read(p)
		p = p[n:]
		if err != nil {
			return err
		}
	}
	return nil
}

func (rsl *riemannListener) read(conn net.Conn) {
	defer rsl.removeConnection(conn)
	defer conn.Close()
	var err error

	for {
		if rsl.ReadTimeout != nil && *rsl.ReadTimeout > 0 {
			if err := conn.SetDeadline(time.Now().Add(time.Duration(*rsl.ReadTimeout))); err != nil {
				rsl.Log.Warnf("Setting deadline failed: %v", err)
			}
		}

		messagePb := &rieman_proto.Msg{}
		var header uint32
		// First obtain the size of the riemann event from client and acknowledge
		if err = binary.Read(conn, binary.BigEndian, &header); err != nil {
			if err.Error() != "EOF" {
				rsl.Log.Debugf("Failed to read header")
				rsl.riemannReturnErrorResponse(conn, err.Error())
				return
			}
			return
		}
		data := make([]byte, header)

		if err = readMessages(conn, data); err != nil {
			rsl.Log.Debugf("Failed to read body: %s", err.Error())
			rsl.riemannReturnErrorResponse(conn, "Failed to read body")
			return
		}
		if err = proto.Unmarshal(data, messagePb); err != nil {
			rsl.Log.Debugf("Failed to unmarshal: %s", err.Error())
			rsl.riemannReturnErrorResponse(conn, "Failed to unmarshal")
			return
		}
		riemannEvents := riemann.ProtocolBuffersToEvents(messagePb.Events)

		for _, m := range riemannEvents {
			if m.Service == "" {
				rsl.riemannReturnErrorResponse(conn, "No Service Name")
				return
			}
			tags := make(map[string]string, len(m.Tags)+3)
			for _, tag := range m.Tags {
				tags[strings.ReplaceAll(tag, " ", "_")] = tag
			}
			tags["Host"] = m.Host
			tags["Description"] = m.Description
			tags["State"] = m.State
			fieldValues := map[string]interface{}{
				"Metric": m.Metric,
				"TTL":    m.TTL.Seconds(),
			}
			singleMetric := metric.New(m.Service, tags, fieldValues, m.Time, telegraf.Untyped)
			rsl.AddMetric(singleMetric)
		}
		rsl.riemannReturnResponse(conn)
	}
}

func (rsl *riemannListener) riemannReturnResponse(conn net.Conn) {
	t := true
	message := new(rieman_proto.Msg)
	message.Ok = &t
	returnData, err := proto.Marshal(message)
	if err != nil {
		rsl.Log.Errorf("The error is: %v", err)
		return
	}
	b := new(bytes.Buffer)
	if err = binary.Write(b, binary.BigEndian, uint32(len(returnData))); err != nil {
		rsl.Log.Errorf("The error is: %v", err)
	}
	// send the msg length
	if _, err = conn.Write(b.Bytes()); err != nil {
		rsl.Log.Errorf("The error is: %v", err)
	}
	if _, err = conn.Write(returnData); err != nil {
		rsl.Log.Errorf("The error is: %v", err)
	}
}

func (rsl *riemannListener) riemannReturnErrorResponse(conn net.Conn, errorMessage string) {
	t := false
	message := new(rieman_proto.Msg)
	message.Ok = &t
	message.Error = &errorMessage
	returnData, err := proto.Marshal(message)
	if err != nil {
		rsl.Log.Errorf("The error is: %v", err)
		return
	}
	b := new(bytes.Buffer)
	if err = binary.Write(b, binary.BigEndian, uint32(len(returnData))); err != nil {
		rsl.Log.Errorf("The error is: %v", err)
	}
	// send the msg length
	if _, err = conn.Write(b.Bytes()); err != nil {
		rsl.Log.Errorf("The error is: %v", err)
	}
	if _, err = conn.Write(returnData); err != nil {
		rsl.Log.Errorf("The error is: %v", err)
	}
}

// Handle cancellations from the process
func (rsl *RiemannSocketListener) processOsSignals(cancelFunc context.CancelFunc) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	for {
		sig := <-signalChan
		if sig == os.Interrupt {
			rsl.Log.Warn("Signal SIGINT is received, probably due to `Ctrl-C`, exiting...")
			cancelFunc()
			return
		}
	}
}

func newRiemannSocketListener() *RiemannSocketListener {
	return &RiemannSocketListener{}
}

func init() {
	inputs.Add("riemann_listener", func() telegraf.Input { return newRiemannSocketListener() })
}
