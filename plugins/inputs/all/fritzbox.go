//go:build !custom || inputs || inputs.fritzbox

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/fritzbox" // register plugin
