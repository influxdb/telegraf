//go:build all || inputs || inputs.x509_cert

package all

import (
	_ "github.com/influxdata/telegraf/plugins/inputs/x509_cert"
)
