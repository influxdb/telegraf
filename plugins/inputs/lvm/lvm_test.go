package lvm

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestGather(t *testing.T) {
	var acc testutil.Accumulator

	lvm := LVM{
		PVSBinary: "/usr/sbin/pvs",
		VGSBinary: "/usr/sbin/vgs",
		LVSBinary: "/usr/sbin/lvs",
	}

	// overwriting exec commands with mock commands
	execCommand = fakeExecCommand
	err := lvm.Gather(&acc)
	require.NoError(t, err)

	pvsTags := map[string]string{
		"path":      "/dev/sdb",
		"vol_group": "docker",
	}
	pvsFields := map[string]interface{}{
		"size":         uint64(128316342272),
		"free":         uint64(3858759680),
		"used":         uint64(124457582592),
		"used_percent": 96.99277612525741,
	}
	acc.AssertContainsTaggedFields(t, "lvm_physical_vol", pvsFields, pvsTags)

	vgsTags := map[string]string{
		"name": "docker",
	}
	vgsFields := map[string]interface{}{
		"size":                  uint64(128316342272),
		"free":                  uint64(3858759680),
		"used_percent":          96.99277612525741,
		"physical_volume_count": uint64(1),
		"logical_volume_count":  uint64(1),
		"snapshot_count":        uint64(0),
	}
	acc.AssertContainsTaggedFields(t, "lvm_vol_group", vgsFields, vgsTags)

	lvsTags := map[string]string{
		"name":      "thinpool",
		"vol_group": "docker",
	}
	lvsFields := map[string]interface{}{
		"size":             uint64(121899057152),
		"data_percent":     0.36000001430511475,
		"metadata_percent": 1.3300000429153442,
	}
	acc.AssertContainsTaggedFields(t, "lvm_logical_vol", lvsFields, lvsTags)
}

// Used as a helper function that mock the exec.Command call
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

// Used to mock exec.Command output
func TestHelperProcess(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mockPVSData := `{
		"report": [
			{
				"pv": [
					{"pv_name":"/dev/sdb", "vg_name":"docker", "pv_size":"128316342272", "pv_free":"3858759680", "pv_used":"124457582592"}
				]
			}
		]
	}
`

	mockVGSData := `{
		"report": [
			{
				"vg": [
					{"vg_name":"docker", "pv_count":"1", "lv_count":"1", "snap_count":"0", "vg_size":"128316342272", "vg_free":"3858759680"}
				]
			}
		]
	}
`

	mockLVSData := `{
		"report": [
			{
				"lv": [
					{"lv_name":"thinpool", "vg_name":"docker", "lv_size":"121899057152", "data_percent":"0.36", "metadata_percent":"1.33"}
				]
			}
		]
	}
`

	// Previous arguments are tests stuff, that looks like :
	// /tmp/go-build970079519/…/_test/integration.test -test.run=TestHelperProcess --
	args := os.Args
	cmd := args[3]
	if cmd == "/usr/sbin/pvs" {
		fmt.Fprint(os.Stdout, mockPVSData)
	} else if cmd == "/usr/sbin/vgs" {
		fmt.Fprint(os.Stdout, mockVGSData)
	} else if cmd == "/usr/sbin/lvs" {
		fmt.Fprint(os.Stdout, mockLVSData)
	} else {
		fmt.Fprint(os.Stdout, "command not found")
		//nolint:revive // error code is important for this "test"
		os.Exit(1)
	}
	//nolint:revive // error code is important for this "test"
	os.Exit(0)
}

// test when no lvm devices exist
func TestGatherNoLVM(t *testing.T) {
	var acc testutil.Accumulator

	noLVM := LVM{
		PVSBinary: "/usr/sbin/pvs",
		VGSBinary: "/usr/sbin/vgs",
		LVSBinary: "/usr/sbin/lvs",
	}

	// overwriting exec commands with mock commands
	execCommand = fakeExecCommandNoLVM
	err := noLVM.Gather(&acc)
	require.NoError(t, err)

	acc.AssertDoesNotContainMeasurement(t, "lvm_physical_vol")
	acc.AssertDoesNotContainMeasurement(t, "lvm_vol_group")
	acc.AssertDoesNotContainMeasurement(t, "lvm_logical_vol")
}

// Used as a helper function that mock the exec.Command call
func fakeExecCommandNoLVM(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcessNoLVM", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

// Used to mock exec.Command output
func TestHelperProcessNoLVM(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mockPVSData := `{
		"report": [
			{
				"pv": [
				]
			}
		]
	}
`

	mockVGSData := `{
		"report": [
			{
				"vg": [
				]
			}
		]
	}
`

	mockLVSData := `{
		"report": [
			{
				"lv": [
				]
			}
		]
	}
`

	// Previous arguments are tests stuff, that looks like :
	// /tmp/go-build970079519/…/_test/integration.test -test.run=TestHelperProcess --
	args := os.Args
	cmd := args[3]
	if cmd == "/usr/sbin/pvs" {
		fmt.Fprint(os.Stdout, mockPVSData)
	} else if cmd == "/usr/sbin/vgs" {
		fmt.Fprint(os.Stdout, mockVGSData)
	} else if cmd == "/usr/sbin/lvs" {
		fmt.Fprint(os.Stdout, mockLVSData)
	} else {
		fmt.Fprint(os.Stdout, "command not found")
		//nolint:revive // error code is important for this "test"
		os.Exit(1)
	}
	//nolint:revive // error code is important for this "test"
	os.Exit(0)
}
