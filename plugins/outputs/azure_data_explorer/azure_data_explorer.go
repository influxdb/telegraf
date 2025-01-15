//go:generate ../../../tools/readme_config_includer/generator
package azure_data_explorer

import (
	_ "embed"

	adx_commons "github.com/influxdata/telegraf/plugins/common/adx"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

type AzureDataExplorer struct {
	adx_commons.AzureDataExplorer
}

func (*AzureDataExplorer) SampleConfig() string {
	return sampleConfig
}

// Initialize the client and the ingestor
func (adx *AzureDataExplorer) Connect() error {
	return adx.AzureDataExplorer.Connect()
}

// Clean up and close the ingestor
func (adx *AzureDataExplorer) Close() error {
	return adx.AzureDataExplorer.Close()
}

func (adx *AzureDataExplorer) Write(metrics []telegraf.Metric) error {
	return adx.AzureDataExplorer.Write(metrics)
}

func (adx *AzureDataExplorer) Init() error {
	return adx.AzureDataExplorer.Init()
}
func init() {
	outputs.Add("azure_data_explorer", func() telegraf.Output {
		return &AzureDataExplorer{
			AzureDataExplorer: adx_commons.AzureDataExplorer{
				CreateTables: true,
				AppName:      "Kusto.Telegraf",
			},
		}
	})
}
