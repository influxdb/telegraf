//go:generate ../../../tools/readme_config_includer/generator
package microsoft_fabric

import (
	_ "embed" // embed is used to embed sample configuration file in the binary
	"errors"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	adx_commons "github.com/influxdata/telegraf/plugins/common/adx"
	eh_commons "github.com/influxdata/telegraf/plugins/common/eventhub"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/plugins/serializers/json"
)

//go:embed sample.conf
var sampleConfig string

type MicrosoftFabric struct {
	ConnectionString  string                         `toml:"connection_string"`
	Log               telegraf.Logger                `toml:"-"`
	EventHouseConf    *adx_commons.AzureDataExplorer `toml:"eh_conf"`
	EventStreamConf   *eh_commons.EventHubs          `toml:"es_conf"`
	FabricSinkService telegraf.Output
}

// Close implements telegraf.Output.
func (m *MicrosoftFabric) Close() error {
	return m.FabricSinkService.Close()
}

// Connect implements telegraf.Output.
func (m *MicrosoftFabric) Connect() error {
	return m.FabricSinkService.Connect()
}

// SampleConfig implements telegraf.Output.
func (m *MicrosoftFabric) SampleConfig() string {
	return sampleConfig
}

// Write implements telegraf.Output.
func (m *MicrosoftFabric) Write(metrics []telegraf.Metric) error {
	return m.FabricSinkService.Write(metrics)
}

func (m *MicrosoftFabric) Init() error {
	connectionString := m.ConnectionString

	if connectionString == "" {
		return errors.New("endpoint must not be empty. For EventHouse refer : " +
			"https://learn.microsoft.com/kusto/api/connection-strings/kusto?view=microsoft-fabric " +
			"for EventStream refer : https://learn.microsoft.com/fabric/real-time-intelligence/event-streams/add-manage-eventstream-sources" +
			"?pivots=enhanced-capabilities")
	}

	if strings.HasPrefix(connectionString, "Endpoint=sb") {
		m.Log.Info("Detected EventStream endpoint, using EventStream output plugin")

		// Need discussion on it
		serializer := &json.Serializer{
			TimestampUnits:  config.Duration(time.Nanosecond),
			TimestampFormat: time.RFC3339Nano,
		}
		m.EventStreamConf.ConnectionString = connectionString
		m.EventStreamConf.Log = m.Log
		m.EventStreamConf.SetSerializer(serializer)
		err := m.EventStreamConf.Init()
		if err != nil {
			return errors.New("error initializing EventStream plugin: " + err.Error())
		}
		m.FabricSinkService = m.EventStreamConf
	} else if isKustoEndpoint(strings.ToLower(connectionString)) {
		m.Log.Info("Detected EventHouse endpoint, using EventHouse output plugin")
		// Setting up the AzureDataExplorer plugin initial properties
		m.EventHouseConf.Endpoint = connectionString
		m.EventHouseConf.Log = m.Log
		err := m.EventHouseConf.Init()
		if err != nil {
			return errors.New("error initializing EventHouse plugin: " + err.Error())
		}
		m.FabricSinkService = m.EventHouseConf
	} else {
		return errors.New("invalid connection string. For EventHouse refer : " +
			"https://learn.microsoft.com/kusto/api/connection-strings/kusto?view=microsoft-fabric" +
			" for EventStream refer : https://learn.microsoft.com/fabric/real-time-intelligence/event-streams/" +
			"add-manage-eventstream-sources?pivots=enhanced-capabilities")
	}
	return nil
}

func isKustoEndpoint(endpoint string) bool {
	prefixes := []string{
		"data source=",
		"addr=",
		"address=",
		"network address=",
		"server=",
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(endpoint, prefix) {
			return true
		}
	}
	return false
}

func init() {
	outputs.Add("microsoft_fabric", func() telegraf.Output {
		return &MicrosoftFabric{
			EventHouseConf: &adx_commons.AzureDataExplorer{
				Timeout:      config.Duration(20 * time.Second),
				CreateTables: true,
				AppName:      "Fabric.Telegraf",
			},
			EventStreamConf: &eh_commons.EventHubs{
				Hub:     &eh_commons.EventHub{},
				Timeout: config.Duration(30 * time.Second),
			},
		}
	})
}
