//go:build all || inputs || inputs.fluentd

package all

import (
	_ "github.com/influxdata/telegraf/plugins/inputs/fluentd"
)
