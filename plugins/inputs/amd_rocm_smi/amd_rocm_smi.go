//go:generate ../../../tools/readme_config_includer/generator
package amd_rocm_smi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const measurement = "amd_rocm_smi"

type ROCmSMI struct {
	BinPath string          `toml:"bin_path"`
	Timeout config.Duration `toml:"timeout"`
	Log     telegraf.Logger `toml:"-"`
}

type gpu struct {
	DeviceID                     string `json:"Device ID"`
	GpuID                        string `json:"GPU ID"`
	GpuUniqueID                  string `json:"Unique ID"`
	GpuVBIOSVersion              string `json:"VBIOS version"`
	GpuTemperatureSensorEdge     string `json:"Temperature (Sensor edge) (C)"`
	GpuTemperatureSensorJunction string `json:"Temperature (Sensor junction) (C)"`
	GpuTemperatureSensorMemory   string `json:"Temperature (Sensor memory) (C)"`
	GpuDcefClkClockSpeed         string `json:"dcefclk clock speed:"`
	GpuDcefClkClockLevel         string `json:"dcefclk clock level:"`
	GpuFclkClockSpeed            string `json:"fclk clock speed:"`
	GpuFclkClockLevel            string `json:"fclk clock level:"`
	GpuMclkClockSpeed            string `json:"mclk clock speed:"`
	GpuMclkClockLevel            string `json:"mclk clock level:"`
	GpuSclkClockSpeed            string `json:"sclk clock speed:"`
	GpuSclkClockLevel            string `json:"sclk clock level:"`
	GpuSocclkClockSpeed          string `json:"socclk clock speed:"`
	GpuSocclkClockLevel          string `json:"socclk clock level:"`
	GpuPcieClock                 string `json:"pcie clock level"`
	GpuFanSpeedLevel             string `json:"Fan speed (level)"`
	GpuFanSpeedPercentage        string `json:"Fan speed (%)"`
	GpuFanRPM                    string `json:"Fan RPM"`
	GpuPerformanceLevel          string `json:"Performance Level"`
	GpuOverdrive                 string `json:"GPU OverDrive value (%)"`
	GpuMaxPower                  string `json:"Max Graphics Package Power (W)"`
	GpuAveragePower              string `json:"Average Graphics Package Power (W)"`
	GpuUsePercentage             string `json:"GPU use (%)"`
	GpuMemoryAllocatedPercentage string `json:"GPU Memory Allocated (VRAM%)"`
	GpuMemoryUsePercentage       string `json:"GPU memory use (%)"`
	GpuMemoryVendor              string `json:"GPU memory vendor"`
	GpuPCIeReplay                string `json:"PCIe Replay Count"`
	GpuSerialNumber              string `json:"Serial Number"`
	GpuVoltagemV                 string `json:"Voltage (mV)"`
	GpuPCIBus                    string `json:"PCI Bus"`
	GpuASDDirmware               string `json:"ASD firmware version"`
	GpuCEFirmware                string `json:"CE firmware version"`
	GpuDMCUFirmware              string `json:"DMCU firmware version"`
	GpuMCFirmware                string `json:"MC firmware version"`
	GpuMEFirmware                string `json:"ME firmware version"`
	GpuMECFirmware               string `json:"MEC firmware version"`
	GpuMEC2Firmware              string `json:"MEC2 firmware version"`
	GpuPFPFirmware               string `json:"PFP firmware version"`
	GpuRLCFirmware               string `json:"RLC firmware version"`
	GpuRLCSRLC                   string `json:"RLC SRLC firmware version"`
	GpuRLCSRLG                   string `json:"RLC SRLG firmware version"`
	GpuRLCSRLS                   string `json:"RLC SRLS firmware version"`
	GpuSDMAFirmware              string `json:"SDMA firmware version"`
	GpuSDMA2Firmware             string `json:"SDMA2 firmware version"`
	GpuSMCFirmware               string `json:"SMC firmware version"`
	GpuSOSFirmware               string `json:"SOS firmware version"`
	GpuTARAS                     string `json:"TA RAS firmware version"`
	GpuTAXGMI                    string `json:"TA XGMI firmware version"`
	GpuUVDFirmware               string `json:"UVD firmware version"`
	GpuVCEFirmware               string `json:"VCE firmware version"`
	GpuVCNFirmware               string `json:"VCN firmware version"`
	GpuCardSeries                string `json:"Card series"`
	GpuCardModel                 string `json:"Card model"`
	GpuCardVendor                string `json:"Card vendor"`
	GpuCardSKU                   string `json:"Card SKU"`
	GpuNUMANode                  string `json:"(Topology) Numa Node"`
	GpuNUMAAffinity              string `json:"(Topology) Numa Affinity"`
	GpuVisVRAMTotalMemory        string `json:"VIS_VRAM Total Memory (B)"`
	GpuVisVRAMTotalUsedMemory    string `json:"VIS_VRAM Total Used Memory (B)"`
	GpuVRAMTotalMemory           string `json:"VRAM Total Memory (B)"`
	GpuVRAMTotalUsedMemory       string `json:"VRAM Total Used Memory (B)"`
	GpuGTTTotalMemory            string `json:"GTT Total Memory (B)"`
	GpuGTTTotalUsedMemory        string `json:"GTT Total Used Memory (B)"`
}

type sysInfo struct {
	DriverVersion string `json:"Driver version"`
}

type metric struct {
	tags   map[string]string
	fields map[string]interface{}
}

func (*ROCmSMI) SampleConfig() string {
	return sampleConfig
}

func (rsmi *ROCmSMI) Start(telegraf.Accumulator) error {
	if _, err := os.Stat(rsmi.BinPath); os.IsNotExist(err) {
		binPath, err := exec.LookPath("rocm-smi")
		if err != nil {
			return &internal.StartupError{Err: err}
		}
		rsmi.BinPath = binPath
	}

	return nil
}

func (rsmi *ROCmSMI) Gather(acc telegraf.Accumulator) error {
	data, err := rsmi.pollROCmSMI()
	if err != nil {
		return fmt.Errorf("failed to execute command in pollROCmSMI: %w", err)
	}

	return gatherROCmSMI(data, acc)
}

func (*ROCmSMI) Stop() {}

func (rsmi *ROCmSMI) pollROCmSMI() ([]byte, error) {
	// Construct and execute metrics query, there currently exist (ROCm v4.3.x) a "-a" option
	// that does not provide all the information, so each needed parameter is set manually
	cmd := exec.Command(rsmi.BinPath,
		"-o",
		"-l",
		"-m",
		"-M",
		"-g",
		"-c",
		"-t",
		"-u",
		"-i",
		"-f",
		"-p",
		"-P",
		"-s",
		"-S",
		"-v",
		"--showreplaycount",
		"--showpids",
		"--showdriverversion",
		"--showmemvendor",
		"--showfwinfo",
		"--showproductname",
		"--showserial",
		"--showuniqueid",
		"--showbus",
		"--showpendingpages",
		"--showpagesinfo",
		"--showmeminfo",
		"all",
		"--showretiredpages",
		"--showunreservablepages",
		"--showmemuse",
		"--showvoltage",
		"--showtopo",
		"--showtopoweight",
		"--showtopohops",
		"--showtopotype",
		"--showtoponuma",
		"--json")

	return internal.StdOutputTimeout(cmd, time.Duration(rsmi.Timeout))
}

func genTagsFields(gpus map[string]gpu, system map[string]sysInfo) []metric {
	metrics := make([]metric, 0, len(gpus))
	for cardID := range gpus {
		if strings.Contains(cardID, "card") {
			tags := map[string]string{
				"name": cardID,
			}

			payload := gpus[cardID]
			//nolint:errcheck // silently treat as zero if malformed
			totVRAM, _ := strconv.ParseInt(payload.GpuVRAMTotalMemory, 10, 64)
			//nolint:errcheck // silently treat as zero if malformed
			usdVRAM, _ := strconv.ParseInt(payload.GpuVRAMTotalUsedMemory, 10, 64)
			strFree := strconv.FormatInt(totVRAM-usdVRAM, 10)

			// Try using value found in Device ID first. If not found, try GPU
			// ID for backwards compatibility.
			setTagIfUsed(tags, "gpu_id", payload.DeviceID)
			setTagIfUsed(tags, "gpu_id", payload.GpuID)

			setTagIfUsed(tags, "gpu_unique_id", payload.GpuUniqueID)

			fields := make(map[string]interface{}, 20)
			setIfUsed("int", fields, "driver_version", strings.ReplaceAll(system["system"].DriverVersion, ".", ""))
			setIfUsed("int", fields, "fan_speed", payload.GpuFanSpeedPercentage)
			setIfUsed("int64", fields, "memory_total", payload.GpuVRAMTotalMemory)
			setIfUsed("int64", fields, "memory_used", payload.GpuVRAMTotalUsedMemory)
			setIfUsed("int64", fields, "memory_free", strFree)
			setIfUsed("float", fields, "temperature_sensor_edge", payload.GpuTemperatureSensorEdge)
			setIfUsed("float", fields, "temperature_sensor_junction", payload.GpuTemperatureSensorJunction)
			setIfUsed("float", fields, "temperature_sensor_memory", payload.GpuTemperatureSensorMemory)
			setIfUsed("int", fields, "utilization_gpu", payload.GpuUsePercentage)
			// Try using allocated percentage first.
			setIfUsed("int", fields, "utilization_memory", payload.GpuMemoryAllocatedPercentage)
			setIfUsed("int", fields, "utilization_memory", payload.GpuMemoryUsePercentage)
			setIfUsed("int", fields, "clocks_current_sm", strings.Trim(payload.GpuSclkClockSpeed, "(Mhz)"))
			setIfUsed("int", fields, "clocks_current_memory", strings.Trim(payload.GpuMclkClockSpeed, "(Mhz)"))
			setIfUsed("int", fields, "clocks_current_display", strings.Trim(payload.GpuDcefClkClockSpeed, "(Mhz)"))
			setIfUsed("int", fields, "clocks_current_fabric", strings.Trim(payload.GpuFclkClockSpeed, "(Mhz)"))
			setIfUsed("int", fields, "clocks_current_system", strings.Trim(payload.GpuSocclkClockSpeed, "(Mhz)"))
			setIfUsed("float", fields, "power_draw", payload.GpuAveragePower)
			setIfUsed("str", fields, "card_series", payload.GpuCardSeries)
			setIfUsed("str", fields, "card_model", payload.GpuCardModel)
			setIfUsed("str", fields, "card_vendor", payload.GpuCardVendor)

			metrics = append(metrics, metric{tags, fields})
		}
	}
	return metrics
}

func gatherROCmSMI(ret []byte, acc telegraf.Accumulator) error {
	var gpus map[string]gpu
	var sys map[string]sysInfo

	err1 := json.Unmarshal(ret, &gpus)
	if err1 != nil {
		return err1
	}

	err2 := json.Unmarshal(ret, &sys)
	if err2 != nil {
		return err2
	}

	metrics := genTagsFields(gpus, sys)
	for _, metric := range metrics {
		acc.AddFields(measurement, metric.fields, metric.tags)
	}

	return nil
}

func setTagIfUsed(m map[string]string, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func setIfUsed(t string, m map[string]interface{}, k, v string) {
	vals := strings.Fields(v)
	if len(vals) < 1 {
		return
	}

	val := vals[0]

	switch t {
	case "float":
		if val != "" {
			f, err := strconv.ParseFloat(val, 64)
			if err == nil {
				m[k] = f
			}
		}
	case "int":
		if val != "" {
			i, err := strconv.Atoi(val)
			if err == nil {
				m[k] = i
			}
		}
	case "int64":
		if val != "" {
			i, err := strconv.ParseInt(val, 10, 64)
			if err == nil {
				m[k] = i
			}
		}
	case "str":
		if val != "" {
			m[k] = val
		}
	}
}

func init() {
	inputs.Add("amd_rocm_smi", func() telegraf.Input {
		return &ROCmSMI{
			BinPath: "/opt/rocm/bin/rocm-smi",
			Timeout: config.Duration(5 * time.Second),
		}
	})
}
