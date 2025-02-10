//go:build !custom || outputs || outputs.influxdb_v3

package all

import _ "github.com/influxdata/telegraf/plugins/outputs/influxdb_v3" // register plugin
