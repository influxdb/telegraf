package sflow

import (
	"strconv"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

func makeMetrics(p *v5Format) []telegraf.Metric {
	now := time.Now()
	metrics := make([]telegraf.Metric, 0)
	tags := map[string]string{
		"agent_address": p.agentAddress.String(),
	}
	fields := make(map[string]interface{}, 2)
	for _, sample := range p.samples {
		tags["input_ifindex"] = strconv.FormatUint(uint64(sample.smplData.inputIfIndex), 10)
		tags["output_ifindex"] = strconv.FormatUint(uint64(sample.smplData.outputIfIndex), 10)
		tags["sample_direction"] = sample.smplData.sampleDirection
		tags["source_id_index"] = strconv.FormatUint(uint64(sample.smplData.sourceIDIndex), 10)
		tags["source_id_type"] = strconv.FormatUint(uint64(sample.smplData.sourceIDType), 10)
		fields["drops"] = sample.smplData.drops
		fields["sampling_rate"] = sample.smplData.samplingRate

		for _, flowRecord := range sample.smplData.flowRecords {
			if flowRecord.flowData != nil {
				tags2 := flowRecord.flowData.getTags()
				fields2 := flowRecord.flowData.getFields()
				for k, v := range tags {
					tags2[k] = v
				}
				for k, v := range fields {
					fields2[k] = v
				}
				m := metric.New("sflow", tags2, fields2, now)
				metrics = append(metrics, m)
			}
		}
	}
	return metrics
}
