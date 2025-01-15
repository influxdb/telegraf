//go:build !custom || inputs || inputs.infiniband_hw

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/infiniband_hw" // register plugin
