//go:generate ../../../tools/readme_config_includer/generator
package synproxy

import (
	_ "embed"
	"path"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Synproxy struct {
	Log telegraf.Logger `toml:"-"`

	// Synproxy stats filename (proc filesystem)
	statFile string
}

func (*Synproxy) SampleConfig() string {
	return sampleConfig
}

func init() {
	inputs.Add("synproxy", func() telegraf.Input {
		return &Synproxy{
			statFile: path.Join(internal.GetProcPath(), "/net/stat/synproxy"),
		}
	})
}
