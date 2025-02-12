//go:build !custom || inputs || inputs.nsdp

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/nsdp" // register plugin
