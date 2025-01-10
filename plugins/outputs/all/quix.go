//go:build !custom || outputs || outputs.quix

package all

import _ "github.com/influxdata/telegraf/plugins/outputs/quix" // register plugin
