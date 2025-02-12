//go:build !custom || inputs || inputs.whois

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/whois" // register plugin
