//go:build !custom || outputs || outputs.inlong

package all

import _ "github.com/influxdata/telegraf/plugins/outputs/inlong" // register plugin
