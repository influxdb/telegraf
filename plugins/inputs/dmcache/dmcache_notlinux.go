//go:build !linux

package dmcache

import (
	"github.com/influxdata/telegraf"
)

func (*DMCache) Gather(_ telegraf.Accumulator) error {
	return nil
}

func dmSetupStatus() ([]string, error) {
	return make([]string, 0), nil
}
