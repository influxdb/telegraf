package nvidia_smi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestErrorBehaviorDefault(t *testing.T) {
	// make sure we can't find nvidia-smi in $PATH somewhere
	os.Unsetenv("PATH")
	plugin := &NvidiaSMI{
		BinPath: "/random/non-existent/path",
		Log:     &testutil.Logger{},
	}
	model := models.NewRunningInput(plugin, &models.InputConfig{
		Name: "nvidia_smi",
	})
	require.NoError(t, model.Init())

	var acc testutil.Accumulator
	var ferr *internal.FatalError
	require.False(t, errors.As(model.Start(&acc), &ferr))
	require.ErrorIs(t, model.Gather(&acc), internal.ErrNotConnected)
}

func TestErrorBehaviorError(t *testing.T) {
	// make sure we can't find nvidia-smi in $PATH somewhere
	os.Unsetenv("PATH")
	plugin := &NvidiaSMI{
		BinPath: "/random/non-existent/path",
		Log:     &testutil.Logger{},
	}
	model := models.NewRunningInput(plugin, &models.InputConfig{
		Name:                 "nvidia_smi",
		StartupErrorBehavior: "error",
	})
	require.NoError(t, model.Init())

	var acc testutil.Accumulator
	var ferr *internal.FatalError
	require.False(t, errors.As(model.Start(&acc), &ferr))
	require.ErrorIs(t, model.Gather(&acc), internal.ErrNotConnected)
}

func TestErrorBehaviorRetry(t *testing.T) {
	// make sure we can't find nvidia-smi in $PATH somewhere
	os.Unsetenv("PATH")
	plugin := &NvidiaSMI{
		BinPath: "/random/non-existent/path",
		Log:     &testutil.Logger{},
	}
	model := models.NewRunningInput(plugin, &models.InputConfig{
		Name:                 "nvidia_smi",
		StartupErrorBehavior: "retry",
	})
	require.NoError(t, model.Init())

	var acc testutil.Accumulator
	var ferr *internal.FatalError
	require.False(t, errors.As(model.Start(&acc), &ferr))
	require.ErrorIs(t, model.Gather(&acc), internal.ErrNotConnected)
}

func TestErrorBehaviorIgnore(t *testing.T) {
	// make sure we can't find nvidia-smi in $PATH somewhere
	os.Unsetenv("PATH")
	plugin := &NvidiaSMI{
		BinPath: "/random/non-existent/path",
		Log:     &testutil.Logger{},
	}
	model := models.NewRunningInput(plugin, &models.InputConfig{
		Name:                 "nvidia_smi",
		StartupErrorBehavior: "ignore",
	})
	require.NoError(t, model.Init())

	var acc testutil.Accumulator
	var ferr *internal.FatalError
	require.ErrorAs(t, model.Start(&acc), &ferr)
	require.ErrorIs(t, model.Gather(&acc), internal.ErrNotConnected)
}

func TestGatherValidXML(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected []telegraf.Metric
	}{
		{
			name:     "GeForce GTX 1070 Ti",
			filename: "gtx-1070-ti.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"name":         "GeForce GTX 1070 Ti",
						"compute_mode": "Default",
						"index":        "0",
						"pstate":       "P8",
						"uuid":         "GPU-f9ba66fc-a7f5-94c5-da19-019ef2f9c665",
					},
					map[string]interface{}{
						"clocks_current_graphics":       135,
						"clocks_current_memory":         405,
						"clocks_current_sm":             135,
						"clocks_current_video":          405,
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fan_speed":                     100,
						"memory_free":                   4054,
						"memory_total":                  4096,
						"memory_used":                   42,
						"pcie_link_gen_current":         1,
						"pcie_link_width_current":       16,
						"temperature_gpu":               39,
						"utilization_gpu":               0,
						"utilization_memory":            0,
					},
					time.Unix(0, 0)),
			},
		},
		{
			name:     "GeForce GTX 1660 Ti",
			filename: "gtx-1660-ti.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"compute_mode": "Default",
						"index":        "0",
						"name":         "Graphics Device",
						"pstate":       "P8",
						"uuid":         "GPU-304a277d-3545-63b8-3a36-dfde3c992989",
					},
					map[string]interface{}{
						"clocks_current_graphics":       300,
						"clocks_current_memory":         405,
						"clocks_current_sm":             300,
						"clocks_current_video":          540,
						"cuda_version":                  "10.1",
						"display_active":                "Disabled",
						"display_mode":                  "Disabled",
						"driver_version":                "418.43",
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fbc_stats_average_fps":         0,
						"fbc_stats_average_latency":     0,
						"fbc_stats_session_count":       0,
						"fan_speed":                     0,
						"memory_free":                   5912,
						"memory_total":                  5912,
						"memory_used":                   0,
						"pcie_link_gen_current":         1,
						"pcie_link_width_current":       16,
						"power_draw":                    8.93,
						"power_limit":                   130.0,
						"temperature_gpu":               40,
						"utilization_gpu":               0,
						"utilization_memory":            1,
						"utilization_encoder":           0,
						"utilization_decoder":           0,
						"vbios_version":                 "90.16.25.00.4C",
					},
					time.Unix(0, 0)),
			},
		},
		{
			name:     "Quadro P400",
			filename: "quadro-p400.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"compute_mode": "Default",
						"index":        "0",
						"name":         "Quadro P400",
						"pstate":       "P8",
						"uuid":         "GPU-8f750be4-dfbc-23b9-b33f-da729a536494",
					},
					map[string]interface{}{
						"clocks_current_graphics":       139,
						"clocks_current_memory":         405,
						"clocks_current_sm":             139,
						"clocks_current_video":          544,
						"cuda_version":                  "10.1",
						"display_active":                "Disabled",
						"display_mode":                  "Disabled",
						"driver_version":                "418.43",
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fbc_stats_average_fps":         0,
						"fbc_stats_average_latency":     0,
						"fbc_stats_session_count":       0,
						"fan_speed":                     34,
						"memory_free":                   1998,
						"memory_total":                  1998,
						"memory_used":                   0,
						"pcie_link_gen_current":         1,
						"pcie_link_width_current":       16,
						"serial":                        "0424418054852",
						"temperature_gpu":               33,
						"utilization_gpu":               0,
						"utilization_memory":            3,
						"utilization_encoder":           0,
						"utilization_decoder":           0,
						"vbios_version":                 "86.07.3B.00.4A",
					},
					time.Unix(0, 0)),
			},
		},
		{
			name:     "Quadro P2000",
			filename: "quadro-p2000-v12.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"arch":         "Pascal",
						"compute_mode": "Default",
						"index":        "0",
						"name":         "Quadro P2000",
						"pstate":       "P8",
						"uuid":         "GPU-396caaed-39ca-3199-2e68-717cdb786ec6",
					},
					map[string]interface{}{

						"clocks_current_graphics":       139,
						"clocks_current_memory":         405,
						"clocks_current_sm":             139,
						"clocks_current_video":          544,
						"cuda_version":                  "12.0",
						"display_active":                "Disabled",
						"display_mode":                  "Disabled",
						"driver_version":                "525.125.06",
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fbc_stats_average_fps":         0,
						"fbc_stats_average_latency":     0,
						"fbc_stats_session_count":       0,
						"fan_speed":                     46,
						"memory_free":                   5051,
						"memory_reserved":               66,
						"memory_total":                  5120,
						"memory_used":                   1,
						"pcie_link_gen_current":         1,
						"pcie_link_width_current":       8,
						"power_draw":                    4.61,
						"power_limit":                   75.0,
						"serial":                        "0322218049033",
						"temperature_gpu":               34,
						"utilization_gpu":               0,
						"utilization_memory":            0,
						"utilization_encoder":           0,
						"utilization_decoder":           0,
						"vbios_version":                 "86.06.3F.00.30",
					},
					time.Unix(0, 0)),
			},
		},
		{
			name:     "Tesla T4",
			filename: "tesla-t4.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"compute_mode": "Default",
						"index":        "0",
						"name":         "Tesla T4",
						"pstate":       "P0",
						"uuid":         "GPU-d37e67a5-91dd-3774-a5cb-99096249601a",
					},
					map[string]interface{}{
						"clocks_current_graphics":           585,
						"clocks_current_memory":             5000,
						"clocks_current_sm":                 585,
						"clocks_current_video":              810,
						"cuda_version":                      "11.7",
						"current_ecc":                       "Enabled",
						"display_active":                    "Disabled",
						"display_mode":                      "Disabled",
						"driver_version":                    "515.105.01",
						"encoder_stats_average_fps":         0,
						"encoder_stats_average_latency":     0,
						"encoder_stats_session_count":       0,
						"fbc_stats_average_fps":             0,
						"fbc_stats_average_latency":         0,
						"fbc_stats_session_count":           0,
						"power_draw":                        26.78,
						"power_limit":                       70.0,
						"memory_free":                       13939,
						"memory_total":                      15360,
						"memory_used":                       1032,
						"memory_reserved":                   388,
						"retired_pages_multiple_single_bit": 0,
						"retired_pages_double_bit":          0,
						"retired_pages_blacklist":           "No",
						"retired_pages_pending":             "No",
						"pcie_link_gen_current":             3,
						"pcie_link_width_current":           8,
						"serial":                            "0000000000000",
						"temperature_gpu":                   40,
						"utilization_gpu":                   0,
						"utilization_memory":                0,
						"utilization_encoder":               0,
						"utilization_decoder":               0,
						"vbios_version":                     "90.04.84.00.06",
					},
					time.Unix(0, 0)),
			},
		},
		{
			name:     "A10G",
			filename: "a10g.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"compute_mode": "Default",
						"index":        "0",
						"name":         "NVIDIA A10G",
						"pstate":       "P8",
						"uuid":         "GPU-9a9a6c50-2a47-2f51-a902-b82c3b127e94",
					},
					map[string]interface{}{
						"clocks_current_graphics":       210,
						"clocks_current_memory":         405,
						"clocks_current_sm":             210,
						"clocks_current_video":          555,
						"cuda_version":                  "11.7",
						"current_ecc":                   "Enabled",
						"display_active":                "Disabled",
						"display_mode":                  "Disabled",
						"driver_version":                "515.105.01",
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fbc_stats_average_fps":         0,
						"fbc_stats_average_latency":     0,
						"fbc_stats_session_count":       0,
						"fan_speed":                     0,
						"power_draw":                    25.58,
						"power_limit":                   300.0,
						"memory_free":                   22569,
						"memory_total":                  23028,
						"memory_used":                   22,
						"memory_reserved":               435,
						"remapped_rows_correctable":     0,
						"remapped_rows_uncorrectable":   0,
						"remapped_rows_pending":         "No",
						"remapped_rows_failure":         "No",
						"pcie_link_gen_current":         1,
						"pcie_link_width_current":       8,
						"serial":                        "0000000000000",
						"temperature_gpu":               17,
						"utilization_gpu":               0,
						"utilization_memory":            0,
						"utilization_encoder":           0,
						"utilization_decoder":           0,
						"vbios_version":                 "94.02.75.00.01",
					},
					time.Unix(0, 0)),
			},
		},
		{
			name:     "RTC 3080 schema v12",
			filename: "rtx-3080-v12.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"compute_mode": "Default",
						"index":        "0",
						"name":         "NVIDIA GeForce RTX 3080",
						"arch":         "Ampere",
						"pstate":       "P8",
						"uuid":         "GPU-19d6d965-2acc-f646-00f8-4c76979aabb4",
					},
					map[string]interface{}{
						"clocks_current_graphics":       210,
						"clocks_current_memory":         405,
						"clocks_current_sm":             210,
						"clocks_current_video":          555,
						"cuda_version":                  "12.2",
						"display_active":                "Enabled",
						"display_mode":                  "Enabled",
						"driver_version":                "536.40",
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fbc_stats_average_fps":         0,
						"fbc_stats_average_latency":     0,
						"fbc_stats_session_count":       0,
						"fan_speed":                     0,
						"power_draw":                    22.78,
						"memory_free":                   8938,
						"memory_total":                  10240,
						"memory_used":                   1128,
						"memory_reserved":               173,
						"pcie_link_gen_current":         4,
						"pcie_link_width_current":       16,
						"temperature_gpu":               31,
						"utilization_gpu":               0,
						"utilization_jpeg":              0,
						"utilization_memory":            37,
						"utilization_encoder":           0,
						"utilization_decoder":           0,
						"utilization_ofa":               0,
						"vbios_version":                 "94.02.71.40.72",
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_process",
					map[string]string{
						"name": "/usr/lib/Xorg",
						"type": "G",
					},
					map[string]interface{}{
						"pid":         int64(835),
						"used_memory": int64(550),
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_process",
					map[string]string{
						"name": "/usr/bin/gnome-shell",
						"type": "G",
					},
					map[string]interface{}{
						"pid":         int64(1481),
						"used_memory": int64(18),
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_process",
					map[string]string{
						"name": "/opt/microsoft/msedge/msedge --type=gpu-process " +
							"--crashpad-handler-pid=2176 --enable-crash-reporter=, " +
							"--change-stack-guard-on-fork=enable --gpu-preferences=" +
							"WAAAAAAAAAAgAAAEAAAAAAAAAAAAAAAAAABgAAAAAAA4AAAAAAAAAAA" +
							"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAGAAAAAAAAAAYAA" +
							"AAAAAAAAgAAAAAAAAACAAAAAAAAAAIAAAAAAAAAA== --shared-files " +
							"--field-trial-handle=0,i,3110290512380155730," +
							"7457693378709978105,262144 --variations-seed-version",
						"type": "G",
					},
					map[string]interface{}{
						"pid":         int64(2214),
						"used_memory": int64(79),
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_process",
					map[string]string{
						"name": "/usr/lib/firefox/firefox",
						"type": "G",
					},
					map[string]interface{}{
						"pid":         int64(4044),
						"used_memory": int64(541),
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_process",
					map[string]string{
						"name": "/opt/visual-studio-code/code --type=gpu-process " +
							"--enable-crash-reporter=6f39585a-ecc4-42e2-b899-9456cbe56b44" +
							",no_channel --user-data-dir=/home/powersj/.config/Code " +
							"--gpu-preferences=WAAAAAAAAAAgAAAEAAAAAAAAAAAAAAAAAABg" +
							"AAAAAAA4AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" +
							"ABAAAAGAAAAAAAAAAYAAAAAAAAAAgAAAAAAAAACAAAAAAAAAAIAAAAAAAAAA== " +
							"--shared-files --field-trial-handle=0,i,685715063932313394," +
							"4769839452661094675,262144 --disable-features=" +
							"CalculateNativeWinOcclusion,SpareRendererForSitePerProcess",
						"type": "G",
					},
					map[string]interface{}{
						"pid":         int64(42416),
						"used_memory": int64(159),
					},
					time.Unix(1689872450, 0)),
			},
		},
		{
			name:     "RTC 3090 schema v12",
			filename: "rtx-3090-v12.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"compute_mode": "Default",
						"index":        "0",
						"name":         "NVIDIA GeForce RTX 3090",
						"arch":         "Ampere",
						"pstate":       "P8",
						"uuid":         "GPU-12345678-aaaa-bbbb-cccc-0123456789ab",
					},
					map[string]interface{}{
						"clocks_current_graphics":       0,
						"clocks_current_memory":         405,
						"clocks_current_sm":             0,
						"clocks_current_video":          555,
						"cuda_version":                  "12.0",
						"display_active":                "Disabled",
						"display_mode":                  "Disabled",
						"driver_version":                "525.147.05",
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fbc_stats_average_fps":         0,
						"fbc_stats_average_latency":     0,
						"fbc_stats_session_count":       0,
						"fan_speed":                     0,
						"power_draw":                    27.23,
						"power_limit":                   200.0,
						"memory_free":                   24258,
						"memory_total":                  24576,
						"memory_used":                   1,
						"memory_reserved":               316,
						"pcie_link_gen_current":         1,
						"pcie_link_width_current":       16,
						"temperature_gpu":               37,
						"utilization_gpu":               0,
						"utilization_memory":            0,
						"utilization_encoder":           0,
						"utilization_decoder":           0,
						"vbios_version":                 "94.02.71.40.72",
					},
					time.Unix(1689872450, 0)),
			},
		},
		{
			name:     "A100-SXM4 schema v12",
			filename: "a100-sxm4-v12.xml",
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"nvidia_smi",
					map[string]string{
						"compute_mode": "Default",
						"index":        "0",
						"name":         "NVIDIA A100-SXM4-80GB",
						"arch":         "Ampere",
						"pstate":       "P0",
						"uuid":         "GPU-513536b6-7d19-9063-b049-1e69664bb298",
					},
					map[string]interface{}{
						"clocks_current_graphics":       1275,
						"clocks_current_memory":         1593,
						"clocks_current_sm":             1275,
						"clocks_current_video":          1275,
						"cuda_version":                  "12.2",
						"current_ecc":                   "Enabled",
						"display_active":                "Disabled",
						"display_mode":                  "Enabled",
						"driver_version":                "535.54.03",
						"encoder_stats_average_fps":     0,
						"encoder_stats_average_latency": 0,
						"encoder_stats_session_count":   0,
						"fbc_stats_average_fps":         0,
						"fbc_stats_average_latency":     0,
						"fbc_stats_session_count":       0,
						"power_draw":                    67.03,
						"memory_free":                   80999,
						"memory_total":                  81920,
						"memory_used":                   50,
						"memory_reserved":               869,
						"pcie_link_gen_current":         4,
						"pcie_link_width_current":       16,
						"serial":                        "1650522003820",
						"temperature_gpu":               27,
						"vbios_version":                 "92.00.36.00.02",
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_mig",
					map[string]string{
						"compute_mode":  "Default",
						"index":         "0",
						"name":          "NVIDIA A100-SXM4-80GB",
						"arch":          "Ampere",
						"pstate":        "P0",
						"uuid":          "GPU-513536b6-7d19-9063-b049-1e69664bb298",
						"compute_index": "0",
						"gpu_index":     "3",
					},
					map[string]interface{}{
						"memory_bar1_free":   32767,
						"memory_bar1_total":  32767,
						"memory_bar1_used":   0,
						"memory_fb_free":     19955,
						"memory_fb_reserved": 0,
						"memory_fb_total":    19968,
						"memory_fb_used":     12,
						"sram_uncorrectable": 0,
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_mig",
					map[string]string{
						"compute_mode":  "Default",
						"index":         "1",
						"name":          "NVIDIA A100-SXM4-80GB",
						"arch":          "Ampere",
						"pstate":        "P0",
						"uuid":          "GPU-513536b6-7d19-9063-b049-1e69664bb298",
						"compute_index": "0",
						"gpu_index":     "4",
					},
					map[string]interface{}{
						"memory_bar1_free":   32767,
						"memory_bar1_total":  32767,
						"memory_bar1_used":   0,
						"memory_fb_free":     19955,
						"memory_fb_reserved": 0,
						"memory_fb_total":    19968,
						"memory_fb_used":     12,
						"sram_uncorrectable": 0,
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_mig",
					map[string]string{
						"compute_mode":  "Default",
						"index":         "2",
						"name":          "NVIDIA A100-SXM4-80GB",
						"arch":          "Ampere",
						"pstate":        "P0",
						"uuid":          "GPU-513536b6-7d19-9063-b049-1e69664bb298",
						"compute_index": "0",
						"gpu_index":     "5",
					},
					map[string]interface{}{
						"memory_bar1_free":   32767,
						"memory_bar1_total":  32767,
						"memory_bar1_used":   0,
						"memory_fb_free":     19955,
						"memory_fb_reserved": 0,
						"memory_fb_total":    19968,
						"memory_fb_used":     12,
						"sram_uncorrectable": 0,
					},
					time.Unix(1689872450, 0)),
				testutil.MustMetric(
					"nvidia_smi_mig",
					map[string]string{
						"compute_mode":  "Default",
						"index":         "3",
						"name":          "NVIDIA A100-SXM4-80GB",
						"arch":          "Ampere",
						"pstate":        "P0",
						"uuid":          "GPU-513536b6-7d19-9063-b049-1e69664bb298",
						"compute_index": "0",
						"gpu_index":     "6",
					},
					map[string]interface{}{
						"memory_bar1_free":   32767,
						"memory_bar1_total":  32767,
						"memory_bar1_used":   0,
						"memory_fb_free":     19955,
						"memory_fb_reserved": 0,
						"memory_fb_total":    19968,
						"memory_fb_used":     12,
						"sram_uncorrectable": 0,
					},
					time.Unix(1689872450, 0)),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			octets, err := os.ReadFile(filepath.Join("testdata", tt.filename))
			require.NoError(t, err)

			plugin := &NvidiaSMI{Log: &testutil.Logger{}}

			var acc testutil.Accumulator
			require.NoError(t, plugin.parse(&acc, octets))
			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}
