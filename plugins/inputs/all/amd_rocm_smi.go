//go:build all || inputs || inputs.amd_rocm_smi

package all

import (
	_ "github.com/influxdata/telegraf/plugins/inputs/amd_rocm_smi"
)
