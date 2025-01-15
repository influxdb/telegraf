//go:build linux

package sensors

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestGatherDefault(t *testing.T) {
	s := Sensors{
		RemoveNumbers: true,
		Timeout:       defaultTimeout,
		path:          "sensors",
	}
	// overwriting exec commands with mock commands
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()
	var acc testutil.Accumulator

	require.NoError(t, s.Init())
	require.NoError(t, s.Gather(&acc))

	var tests = []struct {
		tags   map[string]string
		fields map[string]interface{}
	}{
		{
			map[string]string{
				"chip":    "acpitz-virtual-0",
				"feature": "temp1",
			},
			map[string]interface{}{
				"temp_input": 8.3,
				"temp_crit":  31.3,
			},
		},
		{
			map[string]string{
				"chip":    "power_meter-acpi-0",
				"feature": "power1",
			},
			map[string]interface{}{
				"power_average":          0.0,
				"power_average_interval": 300.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0000",
				"feature": "physical_id_0",
			},
			map[string]interface{}{
				"temp_input":      77.0,
				"temp_max":        82.0,
				"temp_crit":       92.0,
				"temp_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0000",
				"feature": "core_0",
			},
			map[string]interface{}{
				"temp_input":      75.0,
				"temp_max":        82.0,
				"temp_crit":       92.0,
				"temp_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0000",
				"feature": "core_1",
			},
			map[string]interface{}{
				"temp_input":      77.0,
				"temp_max":        82.0,
				"temp_crit":       92.0,
				"temp_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0001",
				"feature": "physical_id_1",
			},
			map[string]interface{}{
				"temp_input":      70.0,
				"temp_max":        82.0,
				"temp_crit":       92.0,
				"temp_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0001",
				"feature": "core_0",
			},
			map[string]interface{}{
				"temp_input":      66.0,
				"temp_max":        82.0,
				"temp_crit":       92.0,
				"temp_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0001",
				"feature": "core_1",
			},
			map[string]interface{}{
				"temp_input":      70.0,
				"temp_max":        82.0,
				"temp_crit":       92.0,
				"temp_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "atk0110-acpi-0",
				"feature": "vcore_voltage",
			},
			map[string]interface{}{
				"in_input": 1.136,
				"in_min":   0.800,
				"in_max":   1.600,
			},
		},
		{
			map[string]string{
				"chip":    "atk0110-acpi-0",
				"feature": "+3.3_voltage",
			},
			map[string]interface{}{
				"in_input": 3.360,
				"in_min":   2.970,
				"in_max":   3.630,
			},
		},
	}

	for _, test := range tests {
		acc.AssertContainsTaggedFields(t, "sensors", test.fields, test.tags)
	}
}

func TestGatherNotRemoveNumbers(t *testing.T) {
	s := Sensors{
		RemoveNumbers: false,
		Timeout:       defaultTimeout,
		path:          "sensors",
	}
	// overwriting exec commands with mock commands
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()
	var acc testutil.Accumulator

	require.NoError(t, s.Init())
	require.NoError(t, s.Gather(&acc))

	var tests = []struct {
		tags   map[string]string
		fields map[string]interface{}
	}{
		{
			map[string]string{
				"chip":    "acpitz-virtual-0",
				"feature": "temp1",
			},
			map[string]interface{}{
				"temp1_input": 8.3,
				"temp1_crit":  31.3,
			},
		},
		{
			map[string]string{
				"chip":    "power_meter-acpi-0",
				"feature": "power1",
			},
			map[string]interface{}{
				"power1_average":          0.0,
				"power1_average_interval": 300.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0000",
				"feature": "physical_id_0",
			},
			map[string]interface{}{
				"temp1_input":      77.0,
				"temp1_max":        82.0,
				"temp1_crit":       92.0,
				"temp1_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0000",
				"feature": "core_0",
			},
			map[string]interface{}{
				"temp2_input":      75.0,
				"temp2_max":        82.0,
				"temp2_crit":       92.0,
				"temp2_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0000",
				"feature": "core_1",
			},
			map[string]interface{}{
				"temp3_input":      77.0,
				"temp3_max":        82.0,
				"temp3_crit":       92.0,
				"temp3_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0001",
				"feature": "physical_id_1",
			},
			map[string]interface{}{
				"temp1_input":      70.0,
				"temp1_max":        82.0,
				"temp1_crit":       92.0,
				"temp1_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0001",
				"feature": "core_0",
			},
			map[string]interface{}{
				"temp2_input":      66.0,
				"temp2_max":        82.0,
				"temp2_crit":       92.0,
				"temp2_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "coretemp-isa-0001",
				"feature": "core_1",
			},
			map[string]interface{}{
				"temp3_input":      70.0,
				"temp3_max":        82.0,
				"temp3_crit":       92.0,
				"temp3_crit_alarm": 0.0,
			},
		},
		{
			map[string]string{
				"chip":    "atk0110-acpi-0",
				"feature": "vcore_voltage",
			},
			map[string]interface{}{
				"in0_input": 1.136,
				"in0_min":   0.800,
				"in0_max":   1.600,
			},
		},
		{
			map[string]string{
				"chip":    "atk0110-acpi-0",
				"feature": "+3.3_voltage",
			},
			map[string]interface{}{
				"in1_input": 3.360,
				"in1_min":   2.970,
				"in1_max":   3.630,
			},
		},
	}
	for _, test := range tests {
		acc.AssertContainsTaggedFields(t, "sensors", test.fields, test.tags)
	}
}

// fakeExecCommand is a helper function that mock
// the exec.Command call (and call the test binary)
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

// TestHelperProcess isn't a real test. It's used to mock exec.Command
// For example, if you run:
// GO_WANT_HELPER_PROCESS=1 go test -test.run=TestHelperProcess -- chrony tracking
// it returns below mockData.
func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mockData := `acpitz-virtual-0
temp1:
  temp1_input: 8.300
  temp1_crit: 31.300

power_meter-acpi-0
power1:
  power1_average: 0.000
  power1_average_interval: 300.000

coretemp-isa-0000
Physical id 0:
  temp1_input: 77.000
  temp1_max: 82.000
  temp1_crit: 92.000
  temp1_crit_alarm: 0.000
Core 0:
  temp2_input: 75.000
  temp2_max: 82.000
  temp2_crit: 92.000
  temp2_crit_alarm: 0.000
Core 1:
  temp3_input: 77.000
  temp3_max: 82.000
  temp3_crit: 92.000
  temp3_crit_alarm: 0.000

coretemp-isa-0001
Physical id 1:
  temp1_input: 70.000
  temp1_max: 82.000
  temp1_crit: 92.000
  temp1_crit_alarm: 0.000
Core 0:
  temp2_input: 66.000
  temp2_max: 82.000
  temp2_crit: 92.000
  temp2_crit_alarm: 0.000
Core 1:
  temp3_input: 70.000
  temp3_max: 82.000
  temp3_crit: 92.000
  temp3_crit_alarm: 0.000

atk0110-acpi-0
Vcore Voltage:
  in0_input: 1.136
  in0_min: 0.800
  in0_max: 1.600
 +3.3 Voltage:
  in1_input: 3.360
  in1_min: 2.970
  in1_max: 3.630
`

	args := os.Args

	// Previous arguments are tests stuff, that looks like :
	// /tmp/go-build970079519/…/_test/integration.test -test.run=TestHelperProcess --
	cmd, _ := args[3], args[4:]

	if cmd != "sensors" {
		fmt.Fprint(os.Stdout, "command not found")
		//nolint:revive // error code is important for this "test"
		os.Exit(1)
	}

	fmt.Fprint(os.Stdout, mockData)
	//nolint:revive // error code is important for this "test"
	os.Exit(0)
}
