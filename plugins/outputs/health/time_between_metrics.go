package health

import (
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
)

type TimeBetweenMetrics struct {
	Field                 string          `toml:"field"`
	MaxTimeBetweenMetrics config.Duration `toml:"max_time_between_metrics"`
	LatestMetricTimestamp time.Time
	WaitingForFirstMetric bool
}

func (d *TimeBetweenMetrics) Init() {
	d.WaitingForFirstMetric = true
	d.LatestMetricTimestamp = time.Time{}
}

func (d *TimeBetweenMetrics) Check(current_time time.Time) bool {
	if d.WaitingForFirstMetric {
		return true
	}
	time_since_last_metric := current_time.Sub(d.LatestMetricTimestamp)
	return time_since_last_metric < time.Duration(d.MaxTimeBetweenMetrics)
}

func (d *TimeBetweenMetrics) Process(metrics []telegraf.Metric) {
	for _, m := range metrics {
		if !m.HasField(d.Field) {
			continue
		}

		if m.Time().After(d.LatestMetricTimestamp) {
			d.LatestMetricTimestamp = m.Time()
		}
		d.WaitingForFirstMetric = false
	}
}
