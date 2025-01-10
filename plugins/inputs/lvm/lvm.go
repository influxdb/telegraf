//go:generate ../../../tools/readme_config_includer/generator
package lvm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	execCommand = exec.Command
)

type LVM struct {
	UseSudo   bool   `toml:"use_sudo"`
	PVSBinary string `toml:"pvs_binary"`
	VGSBinary string `toml:"vgs_binary"`
	LVSBinary string `toml:"lvs_binary"`
}

func (*LVM) SampleConfig() string {
	return sampleConfig
}

func (lvm *LVM) Gather(acc telegraf.Accumulator) error {
	if err := lvm.gatherPhysicalVolumes(acc); err != nil {
		return err
	} else if err := lvm.gatherVolumeGroups(acc); err != nil {
		return err
	} else if err := lvm.gatherLogicalVolumes(acc); err != nil {
		return err
	}

	return nil
}

func (lvm *LVM) gatherPhysicalVolumes(acc telegraf.Accumulator) error {
	args := []string{
		"--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "pv_name,vg_name,pv_size,pv_free,pv_used",
	}
	out, err := lvm.runCmd(lvm.PVSBinary, args)
	if err != nil {
		return err
	}

	var report pvsReport
	err = json.Unmarshal(out, &report)
	if err != nil {
		return fmt.Errorf("failed to unmarshal physical volume JSON: %w", err)
	}

	if len(report.Report) > 0 {
		for _, pv := range report.Report[0].Pv {
			tags := map[string]string{
				"path":      pv.Name,
				"vol_group": pv.VolGroup,
			}

			size, err := strconv.ParseUint(pv.Size, 10, 64)
			if err != nil {
				return err
			}

			free, err := strconv.ParseUint(pv.Free, 10, 64)
			if err != nil {
				return err
			}

			used, err := strconv.ParseUint(pv.Used, 10, 64)
			if err != nil {
				return err
			}

			usedPercent := float64(used) / float64(size) * 100

			fields := map[string]interface{}{
				"size":         size,
				"free":         free,
				"used":         used,
				"used_percent": usedPercent,
			}

			acc.AddFields("lvm_physical_vol", fields, tags)
		}
	}

	return nil
}

func (lvm *LVM) gatherVolumeGroups(acc telegraf.Accumulator) error {
	args := []string{
		"--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "vg_name,pv_count,lv_count,snap_count,vg_size,vg_free",
	}
	out, err := lvm.runCmd(lvm.VGSBinary, args)
	if err != nil {
		return err
	}

	var report vgsReport
	err = json.Unmarshal(out, &report)
	if err != nil {
		return fmt.Errorf("failed to unmarshal vol group JSON: %w", err)
	}

	if len(report.Report) > 0 {
		for _, vg := range report.Report[0].Vg {
			tags := map[string]string{
				"name": vg.Name,
			}

			size, err := strconv.ParseUint(vg.Size, 10, 64)
			if err != nil {
				return err
			}

			free, err := strconv.ParseUint(vg.Free, 10, 64)
			if err != nil {
				return err
			}

			pvCount, err := strconv.ParseUint(vg.PvCount, 10, 64)
			if err != nil {
				return err
			}
			lvCount, err := strconv.ParseUint(vg.LvCount, 10, 64)
			if err != nil {
				return err
			}
			snapCount, err := strconv.ParseUint(vg.SnapCount, 10, 64)
			if err != nil {
				return err
			}

			usedPercent := (float64(size) - float64(free)) / float64(size) * 100

			fields := map[string]interface{}{
				"size":                  size,
				"free":                  free,
				"used_percent":          usedPercent,
				"physical_volume_count": pvCount,
				"logical_volume_count":  lvCount,
				"snapshot_count":        snapCount,
			}

			acc.AddFields("lvm_vol_group", fields, tags)
		}
	}

	return nil
}

func (lvm *LVM) gatherLogicalVolumes(acc telegraf.Accumulator) error {
	args := []string{
		"--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "lv_name,vg_name,lv_size,data_percent,metadata_percent",
	}
	out, err := lvm.runCmd(lvm.LVSBinary, args)
	if err != nil {
		return err
	}

	var report lvsReport
	err = json.Unmarshal(out, &report)
	if err != nil {
		return fmt.Errorf("failed to unmarshal logical vol JSON: %w", err)
	}

	if len(report.Report) > 0 {
		for _, lv := range report.Report[0].Lv {
			tags := map[string]string{
				"name":      lv.Name,
				"vol_group": lv.VolGroup,
			}

			size, err := strconv.ParseUint(lv.Size, 10, 64)
			if err != nil {
				return err
			}

			// Does not apply to all logical volumes, set default value
			if lv.DataPercent == "" {
				lv.DataPercent = "0.0"
			}
			dataPercent, err := strconv.ParseFloat(lv.DataPercent, 32)
			if err != nil {
				return err
			}

			// Does not apply to all logical volumes, set default value
			if lv.MetadataPercent == "" {
				lv.MetadataPercent = "0.0"
			}
			metadataPercent, err := strconv.ParseFloat(lv.MetadataPercent, 32)
			if err != nil {
				return err
			}

			fields := map[string]interface{}{
				"size":             size,
				"data_percent":     dataPercent,
				"metadata_percent": metadataPercent,
			}

			acc.AddFields("lvm_logical_vol", fields, tags)
		}
	}

	return nil
}

func (lvm *LVM) runCmd(cmd string, args []string) ([]byte, error) {
	execCmd := execCommand(cmd, args...)
	if lvm.UseSudo {
		execCmd = execCommand("sudo", append([]string{"-n", cmd}, args...)...)
	}

	out, err := internal.StdOutputTimeout(execCmd, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to run command %s: %w - %s", strings.Join(execCmd.Args, " "), err, string(out),
		)
	}

	return out, nil
}

// Represents info about physical volume command, pvs, output
type pvsReport struct {
	Report []struct {
		Pv []struct {
			Name     string `json:"pv_name"`
			VolGroup string `json:"vg_name"`
			Size     string `json:"pv_size"`
			Free     string `json:"pv_free"`
			Used     string `json:"pv_used"`
		} `json:"pv"`
	} `json:"report"`
}

// Represents info about volume group command, vgs, output
type vgsReport struct {
	Report []struct {
		Vg []struct {
			Name      string `json:"vg_name"`
			Size      string `json:"vg_size"`
			Free      string `json:"vg_free"`
			LvCount   string `json:"lv_count"`
			PvCount   string `json:"pv_count"`
			SnapCount string `json:"snap_count"`
		} `json:"vg"`
	} `json:"report"`
}

// Represents info about logical volume command, lvs, output
type lvsReport struct {
	Report []struct {
		Lv []struct {
			Name            string `json:"lv_name"`
			VolGroup        string `json:"vg_name"`
			Size            string `json:"lv_size"`
			DataPercent     string `json:"data_percent"`
			MetadataPercent string `json:"metadata_percent"`
		} `json:"lv"`
	} `json:"report"`
}

func init() {
	inputs.Add("lvm", func() telegraf.Input {
		return &LVM{
			PVSBinary: "/usr/sbin/pvs",
			VGSBinary: "/usr/sbin/vgs",
			LVSBinary: "/usr/sbin/lvs",
		}
	})
}
