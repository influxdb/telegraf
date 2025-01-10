//go:generate ../../../tools/readme_config_includer/generator
package lanz

import (
	_ "embed"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/aristanetworks/goarista/lanz"
	pb "github.com/aristanetworks/goarista/lanz/proto"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Lanz struct {
	Servers []string `toml:"servers"`
	clients []lanz.Client
	wg      sync.WaitGroup
}

func (*Lanz) SampleConfig() string {
	return sampleConfig
}

func (l *Lanz) Start(acc telegraf.Accumulator) error {
	if len(l.Servers) == 0 {
		l.Servers = append(l.Servers, "tcp://127.0.0.1:50001")
	}

	for _, server := range l.Servers {
		deviceURL, err := url.Parse(server)
		if err != nil {
			return err
		}
		client := lanz.New(
			lanz.WithAddr(deviceURL.Host),
			lanz.WithBackoff(1*time.Second),
			lanz.WithTimeout(10*time.Second),
		)
		l.clients = append(l.clients, client)

		in := make(chan *pb.LanzRecord)
		go func() {
			client.Run(in)
		}()
		l.wg.Add(1)
		go func() {
			l.wg.Done()
			receive(acc, in, deviceURL)
		}()
	}
	return nil
}

func (*Lanz) Gather(telegraf.Accumulator) error {
	return nil
}

func (l *Lanz) Stop() {
	for _, client := range l.clients {
		client.Stop()
	}
	l.wg.Wait()
}

func receive(acc telegraf.Accumulator, in <-chan *pb.LanzRecord, deviceURL *url.URL) {
	//nolint:gosimple // for-select used on purpose
	for {
		select {
		case msg, ok := <-in:
			if !ok {
				return
			}
			msgToAccumulator(acc, msg, deviceURL)
		}
	}
}

func msgToAccumulator(acc telegraf.Accumulator, msg *pb.LanzRecord, deviceURL *url.URL) {
	cr := msg.GetCongestionRecord()
	if cr != nil {
		vals := map[string]interface{}{
			"timestamp":        int64(cr.GetTimestamp()),
			"queue_size":       int64(cr.GetQueueSize()),
			"time_of_max_qlen": int64(cr.GetTimeOfMaxQLen()),
			"tx_latency":       int64(cr.GetTxLatency()),
			"q_drop_count":     int64(cr.GetQDropCount()),
		}
		tags := map[string]string{
			"intf_name":             cr.GetIntfName(),
			"switch_id":             strconv.FormatInt(int64(cr.GetSwitchId()), 10),
			"port_id":               strconv.FormatInt(int64(cr.GetPortId()), 10),
			"entry_type":            strconv.FormatInt(int64(cr.GetEntryType()), 10),
			"traffic_class":         strconv.FormatInt(int64(cr.GetTrafficClass()), 10),
			"fabric_peer_intf_name": cr.GetFabricPeerIntfName(),
			"source":                deviceURL.Hostname(),
			"port":                  deviceURL.Port(),
		}
		acc.AddFields("lanz_congestion_record", vals, tags)
	}

	gbur := msg.GetGlobalBufferUsageRecord()
	if gbur != nil {
		vals := map[string]interface{}{
			"timestamp":   int64(gbur.GetTimestamp()),
			"buffer_size": int64(gbur.GetBufferSize()),
			"duration":    int64(gbur.GetDuration()),
		}
		tags := map[string]string{
			"entry_type": strconv.FormatInt(int64(gbur.GetEntryType()), 10),
			"source":     deviceURL.Hostname(),
			"port":       deviceURL.Port(),
		}
		acc.AddFields("lanz_global_buffer_usage_record", vals, tags)
	}
}

func init() {
	inputs.Add("lanz", func() telegraf.Input {
		return &Lanz{}
	})
}
