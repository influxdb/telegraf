//go:build all || inputs || inputs.redis_sentinel

package all

import (
	_ "github.com/influxdata/telegraf/plugins/inputs/redis_sentinel"
)
