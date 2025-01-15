//go:build !custom || inputs || inputs.mavlink

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/mavlink" // register plugin
