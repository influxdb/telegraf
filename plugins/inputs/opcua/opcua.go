//go:generate ../../../tools/readme_config_includer/generator
package opcua

import (
	_ "embed"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/opcua"
	"github.com/influxdata/telegraf/plugins/common/opcua/input"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type OpcUA struct {
	readClientConfig
	Log telegraf.Logger `toml:"-"`

	client *readClient
}

func (*OpcUA) SampleConfig() string {
	return sampleConfig
}

func (o *OpcUA) Init() (err error) {
	o.client, err = o.readClientConfig.createReadClient(o.Log)
	return err
}

func (o *OpcUA) Gather(acc telegraf.Accumulator) error {
	// Will (re)connect if the client is disconnected
	metrics, err := o.client.currentValues()
	if err != nil {
		return err
	}

	// Parse the resulting data into metrics
	for _, m := range metrics {
		acc.AddMetric(m)
	}
	return nil
}

// Add this plugin to telegraf
func init() {
	inputs.Add("opcua", func() telegraf.Input {
		return &OpcUA{
			readClientConfig: readClientConfig{
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
			},
		}
	})
}
