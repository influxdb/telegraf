//go:build linux && amd64

package intel_powerstat

import (
	"slices"
	"time"

	ptel "github.com/intel/powertelemetry"

	"github.com/influxdata/telegraf"
)

// optConfig represents plugin configuration fields needed to generate options.
type optConfig struct {
	cpuMetrics     []cpuMetricType
	packageMetrics []packageMetricType
	includedCPUs   []int
	excludedCPUs   []int
	perfEventFile  string
	msrReadTimeout time.Duration
	log            telegraf.Logger
}

// optionGenerator takes a struct with the plugin configuration, and generates options
// needed to gather metrics.
type optionGenerator interface {
	generate(cfg optConfig) []ptel.Option
}

// optGenerator implements optionGenerator interface.
type optGenerator struct{}

// generate takes plugin configuration options and generates options needed
// to gather requested metrics.
func (*optGenerator) generate(cfg optConfig) []ptel.Option {
	opts := make([]ptel.Option, 0)
	if len(cfg.includedCPUs) != 0 {
		opts = append(opts, ptel.WithIncludedCPUs(cfg.includedCPUs))
	}

	if len(cfg.excludedCPUs) != 0 {
		opts = append(opts, ptel.WithExcludedCPUs(cfg.excludedCPUs))
	}

	if needsMsrCPU(cfg.cpuMetrics) || needsMsrPackage(cfg.packageMetrics) {
		if cfg.msrReadTimeout == 0 {
			opts = append(opts, ptel.WithMsr())
		} else {
			opts = append(opts, ptel.WithMsrTimeout(cfg.msrReadTimeout))
		}
	}

	if needsRapl(cfg.packageMetrics) {
		opts = append(opts, ptel.WithRapl())
	}

	if needsCoreFreq(cfg.cpuMetrics) {
		opts = append(opts, ptel.WithCoreFrequency())
	}

	if needsUncoreFreq(cfg.packageMetrics) {
		opts = append(opts, ptel.WithUncoreFrequency())
	}

	if needsPerf(cfg.cpuMetrics) {
		opts = append(opts, ptel.WithPerf(cfg.perfEventFile))
	}

	if cfg.log != nil {
		opts = append(opts, ptel.WithLogger(cfg.log))
	}

	return opts
}

// needsMsr takes a slice of strings, representing supported metrics, and
// returns true if any relies on msr registers.
func needsMsrCPU(metrics []cpuMetricType) bool {
	for _, m := range metrics {
		switch m {
		case cpuTemperature:
		case cpuC0StateResidency:
		case cpuC1StateResidency:
		case cpuC3StateResidency:
		case cpuC6StateResidency:
		case cpuC7StateResidency:
		case cpuBusyCycles:
		case cpuBusyFrequency:
		default:
			continue
		}

		return true
	}
	return false
}

// needsMsrPackage takes a slice of strings, representing supported metrics, and
// returns true if any relies on msr registers.
func needsMsrPackage(metrics []packageMetricType) bool {
	for _, m := range metrics {
		switch m {
		case packageCPUBaseFrequency:
		case packageTurboLimit:
		case packageUncoreFrequency:
			// Fallback mechanism retrieves this metric from MSR registers.
		default:
			continue
		}

		return true
	}
	return false
}

// needsTimeRelatedMsr takes a slice of strings, representing supported metrics, and
// returns true if any relies on time-related reads of msr registers.
func needsTimeRelatedMsr(metrics []cpuMetricType) bool {
	for _, m := range metrics {
		switch m {
		case cpuC0StateResidency:
		case cpuC1StateResidency:
		case cpuC3StateResidency:
		case cpuC6StateResidency:
		case cpuC7StateResidency:
		case cpuBusyCycles:
		case cpuBusyFrequency:
		default:
			continue
		}

		return true
	}
	return false
}

// needsRapl takes a slice of strings, representing supported metrics, and
// returns true if any relies on intel-rapl control zone.
func needsRapl(metrics []packageMetricType) bool {
	for _, m := range metrics {
		switch m {
		case packageCurrentPowerConsumption:
		case packageCurrentDramPowerConsumption:
		case packageThermalDesignPower:
		default:
			continue
		}

		return true
	}
	return false
}

// needsCoreFreq takes a slice of strings, representing supported metrics, and
// returns true if any relies on sysfs "/sys/devices/system/cpu/" with global and
// individual CPU attributes.
func needsCoreFreq(metrics []cpuMetricType) bool {
	return slices.Contains(metrics, cpuFrequency)
}

// needsUncoreFreq takes a slice of strings, representing supported metrics, and returns
// true if any relies on sysfs interface "/sys/devices/system/cpu/intel_uncore_frequency/"
// provided by intel_uncore_frequency kernel module.
func needsUncoreFreq(metrics []packageMetricType) bool {
	return slices.Contains(metrics, packageUncoreFrequency)
}

// needsPerf takes a slice of strings, representing supported metrics, and
// returns true if any relies on perf_events interface.
func needsPerf(metrics []cpuMetricType) bool {
	for _, m := range metrics {
		switch m {
		case cpuC0SubstateC01Percent:
		case cpuC0SubstateC02Percent:
		case cpuC0SubstateC0WaitPercent:
		default:
			continue
		}

		return true
	}
	return false
}
