package topk

import (
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"time"
)

var metric11, _ = metric.New(
	"1one",
	map[string]string{"tag_name": "tag_value1"},
	map[string]interface{}{
		"a": float64(15.3),
		"b": float64(40),
	},
	time.Now(),
)

var metric12, _ = metric.New(
	"1two",
	map[string]string{"tag_name": "tag_value1"},
	map[string]interface{}{
		"a": float64(50),
	},
	time.Now(),
)

var metric13, _ = metric.New(
	"1three",
	map[string]string{"tag_name": "tag_value1"},
	map[string]interface{}{
		"a": float64(0.3),
		"c": float64(400),
	},
	time.Now(),
)

var metric14, _ = metric.New(
	"1four",
	map[string]string{"tag_name": "tag_value1"},
	map[string]interface{}{
		"a": float64(24.12),
		"b": float64(40),
	},
	time.Now(),
)

var metric15, _ = metric.New(
	"1five",
	map[string]string{"tag_name": "tag_value1"},
	map[string]interface{}{
		"a": float64(50.5),
		"h": float64(1),
		"u": float64(2.4),
	},
	time.Now(),
)

var MetricsSet1 = []telegraf.Metric{metric11, metric12, metric13, metric14, metric15}

var ansMean11 = metric11.Copy()
var ansMean12 = metric12.Copy()
var ansMean13 = metric13.Copy()
var ansMean14 = metric14.Copy()
var ansMean15 = metric15.Copy()
var MeanAddAggregateFieldAns = []telegraf.Metric{ansMean11, ansMean12, ansMean13, ansMean14, ansMean15}

var ansSum11 = metric11.Copy()
var ansSum12 = metric12.Copy()
var ansSum13 = metric13.Copy()
var ansSum14 = metric14.Copy()
var ansSum15 = metric15.Copy()
var SumAddAggregateFieldAns = []telegraf.Metric{ansSum11, ansSum12, ansSum13, ansSum14, ansSum15}

var ansMax11 = metric11.Copy()
var ansMax12 = metric12.Copy()
var ansMax13 = metric13.Copy()
var ansMax14 = metric14.Copy()
var ansMax15 = metric15.Copy()
var MaxAddAggregateFieldAns = []telegraf.Metric{ansMax11, ansMax12, ansMax13, ansMax14, ansMax15}

var ansMin11 = metric11.Copy()
var ansMin12 = metric12.Copy()
var ansMin13 = metric13.Copy()
var ansMin14 = metric14.Copy()
var ansMin15 = metric15.Copy()
var MinAddAggregateFieldAns = []telegraf.Metric{ansMin11, ansMin12, ansMin13, ansMin14, ansMin15}

func setupTestSet1() {
	// Expected answer for the TopkMeanAggretationField test
	ansMean11.AddField("a_meanag", float64(28.044))
	ansMean12.AddField("a_meanag", float64(28.044))
	ansMean13.AddField("a_meanag", float64(28.044))
	ansMean14.AddField("a_meanag", float64(28.044))
	ansMean15.AddField("a_meanag", float64(28.044))

	// Expected answer for the TopkSumAggretationField test
	ansSum11.AddField("a_sumag", float64(140.22))
	ansSum12.AddField("a_sumag", float64(140.22))
	ansSum13.AddField("a_sumag", float64(140.22))
	ansSum14.AddField("a_sumag", float64(140.22))
	ansSum15.AddField("a_sumag", float64(140.22))

	// Expected answer for the TopkMaxAggretationField test
	ansMax11.AddField("a_maxag", float64(50.5))
	ansMax12.AddField("a_maxag", float64(50.5))
	ansMax13.AddField("a_maxag", float64(50.5))
	ansMax14.AddField("a_maxag", float64(50.5))
	ansMax15.AddField("a_maxag", float64(50.5))

	// Expected answer for the TopkMinAggretationField test
	ansMin11.AddField("a_minag", float64(0.3))
	ansMin12.AddField("a_minag", float64(0.3))
	ansMin13.AddField("a_minag", float64(0.3))
	ansMin14.AddField("a_minag", float64(0.3))
	ansMin15.AddField("a_minag", float64(0.3))
}
