//go:build all || inputs || inputs.ipvs

package all

import (
	_ "github.com/influxdata/telegraf/plugins/inputs/ipvs"
)
