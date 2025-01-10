//go:generate ../../../tools/readme_config_includer/generator
package opcua_listener

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/opcua"
	"github.com/influxdata/telegraf/plugins/common/opcua/input"
	"github.com/influxdata/telegraf/plugins/inputs"
)

type OpcUaListener struct {
	subscribeClientConfig
	client *subscribeClient
	Log    telegraf.Logger `toml:"-"`
}

//go:embed sample.conf
var sampleConfig string

func (*OpcUaListener) SampleConfig() string {
	return sampleConfig
}

func (o *OpcUaListener) Init() (err error) {
	switch o.ConnectFailBehavior {
	case "":
		o.ConnectFailBehavior = "error"
	case "error", "ignore", "retry":
		// Do nothing as these are valid
	default:
		return fmt.Errorf("unknown setting %q for 'connect_fail_behavior'", o.ConnectFailBehavior)
	}
	o.client, err = o.subscribeClientConfig.createSubscribeClient(o.Log)
	return err
}

func (o *OpcUaListener) Start(acc telegraf.Accumulator) error {
	return o.connect(acc)
}

func (o *OpcUaListener) Gather(acc telegraf.Accumulator) error {
	if o.client.State() == opcua.Connected || o.subscribeClientConfig.ConnectFailBehavior == "ignore" {
		return nil
	}
	return o.connect(acc)
}

func (o *OpcUaListener) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	select {
	case <-o.client.stop(ctx):
		o.Log.Infof("Unsubscribed OPC UA successfully")
	case <-ctx.Done(): // Timeout context
		o.Log.Warn("Timeout while stopping OPC UA subscription")
	}
	cancel()
}

func (o *OpcUaListener) connect(acc telegraf.Accumulator) error {
	ctx := context.Background()
	ch, err := o.client.startStreamValues(ctx)
	if err != nil {
		return err
	}

	go func() {
		for {
			m, ok := <-ch
			if !ok {
				o.Log.Debug("Metric collection stopped due to closed channel")
				return
			}
			acc.AddMetric(m)
		}
	}()

	return nil
}

func init() {
	inputs.Add("opcua_listener", func() telegraf.Input {
		return &OpcUaListener{
			subscribeClientConfig: subscribeClientConfig{
				InputClientConfig: input.InputClientConfig{
					OpcUAClientConfig: opcua.OpcUAClientConfig{
						Endpoint:       "opc.tcp://localhost:4840",
						SecurityPolicy: "auto",
						SecurityMode:   "auto",
						Certificate:    "/etc/telegraf/cert.pem",
						PrivateKey:     "/etc/telegraf/key.pem",
						AuthMethod:     "Anonymous",
						ConnectTimeout: config.Duration(5 * time.Second),
						RequestTimeout: config.Duration(10 * time.Second),
					},
					MetricName: "opcua",
					Timestamp:  input.TimestampSourceTelegraf,
				},
				SubscriptionInterval: config.Duration(100 * time.Millisecond),
			},
		}
	})
}
