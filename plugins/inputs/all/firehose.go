//go:build !custom || inputs || inputs.firehose

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/firehose" // register plugin
