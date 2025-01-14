//go:build !custom || inputs || inputs.huebridge

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/huebridge" // register plugin
