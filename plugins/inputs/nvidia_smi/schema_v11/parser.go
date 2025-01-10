package schema_v11

import (
	"encoding/xml"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs/nvidia_smi/common"
)

// Parse parses the XML-encoded data from nvidia-smi and adds measurements.
func Parse(acc telegraf.Accumulator, buf []byte) error {
	var s smi
	if err := xml.Unmarshal(buf, &s); err != nil {
		return err
	}

	for i := range s.GPU {
		gpu := &s.GPU[i]

		tags := map[string]string{
			"index": strconv.Itoa(i),
		}
		fields := make(map[string]interface{}, 39)

		common.SetTagIfUsed(tags, "pstate", gpu.PState)
		common.SetTagIfUsed(tags, "name", gpu.ProdName)
		common.SetTagIfUsed(tags, "uuid", gpu.UUID)
		common.SetTagIfUsed(tags, "compute_mode", gpu.ComputeMode)

		common.SetIfUsed("str", fields, "driver_version", s.DriverVersion)
		common.SetIfUsed("str", fields, "cuda_version", s.CUDAVersion)
		common.SetIfUsed("str", fields, "serial", gpu.Serial)
		common.SetIfUsed("str", fields, "vbios_version", gpu.VbiosVersion)
		common.SetIfUsed("str", fields, "display_active", gpu.DisplayActive)
		common.SetIfUsed("str", fields, "display_mode", gpu.DisplayMode)
		common.SetIfUsed("str", fields, "current_ecc", gpu.EccMode.CurrentEcc)
		common.SetIfUsed("int", fields, "fan_speed", gpu.FanSpeed)
		common.SetIfUsed("int", fields, "memory_total", gpu.Memory.Total)
		common.SetIfUsed("int", fields, "memory_used", gpu.Memory.Used)
		common.SetIfUsed("int", fields, "memory_free", gpu.Memory.Free)
		common.SetIfUsed("int", fields, "memory_reserved", gpu.Memory.Reserved)
		common.SetIfUsed("int", fields, "retired_pages_multiple_single_bit", gpu.RetiredPages.MultipleSingleBit.Count)
		common.SetIfUsed("int", fields, "retired_pages_double_bit", gpu.RetiredPages.DoubleBit.Count)
		common.SetIfUsed("str", fields, "retired_pages_blacklist", gpu.RetiredPages.PendingBlacklist)
		common.SetIfUsed("str", fields, "retired_pages_pending", gpu.RetiredPages.PendingRetirement)
		common.SetIfUsed("int", fields, "remapped_rows_correctable", gpu.RemappedRows.Correctable)
		common.SetIfUsed("int", fields, "remapped_rows_uncorrectable", gpu.RemappedRows.Uncorrectable)
		common.SetIfUsed("str", fields, "remapped_rows_pending", gpu.RemappedRows.Pending)
		common.SetIfUsed("str", fields, "remapped_rows_failure", gpu.RemappedRows.Failure)
		common.SetIfUsed("int", fields, "temperature_gpu", gpu.Temp.GPUTemp)
		common.SetIfUsed("int", fields, "utilization_gpu", gpu.Utilization.GPU)
		common.SetIfUsed("int", fields, "utilization_memory", gpu.Utilization.Memory)
		common.SetIfUsed("int", fields, "utilization_encoder", gpu.Utilization.Encoder)
		common.SetIfUsed("int", fields, "utilization_decoder", gpu.Utilization.Decoder)
		common.SetIfUsed("int", fields, "pcie_link_gen_current", gpu.PCI.LinkInfo.PCIEGen.CurrentLinkGen)
		common.SetIfUsed("int", fields, "pcie_link_width_current", gpu.PCI.LinkInfo.LinkWidth.CurrentLinkWidth)
		common.SetIfUsed("int", fields, "encoder_stats_session_count", gpu.Encoder.SessionCount)
		common.SetIfUsed("int", fields, "encoder_stats_average_fps", gpu.Encoder.AverageFPS)
		common.SetIfUsed("int", fields, "encoder_stats_average_latency", gpu.Encoder.AverageLatency)
		common.SetIfUsed("int", fields, "fbc_stats_session_count", gpu.FBC.SessionCount)
		common.SetIfUsed("int", fields, "fbc_stats_average_fps", gpu.FBC.AverageFPS)
		common.SetIfUsed("int", fields, "fbc_stats_average_latency", gpu.FBC.AverageLatency)
		common.SetIfUsed("int", fields, "clocks_current_graphics", gpu.Clocks.Graphics)
		common.SetIfUsed("int", fields, "clocks_current_sm", gpu.Clocks.SM)
		common.SetIfUsed("int", fields, "clocks_current_memory", gpu.Clocks.Memory)
		common.SetIfUsed("int", fields, "clocks_current_video", gpu.Clocks.Video)

		common.SetIfUsed("float", fields, "power_draw", gpu.Power.PowerDraw)
		common.SetIfUsed("float", fields, "power_limit", gpu.Power.PowerLimit)
		acc.AddFields("nvidia_smi", fields, tags)
	}

	return nil
}
