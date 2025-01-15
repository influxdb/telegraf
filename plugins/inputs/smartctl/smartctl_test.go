package smartctl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestCasesScan(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases_scan")
	require.NoError(t, err)

	// Register the plugin
	inputs.Add("smartctl", func() telegraf.Input {
		return &Smartctl{}
	})

	for _, f := range folders {
		if !f.IsDir() {
			continue
		}
		testcasePath := filepath.Join("testcases_scan", f.Name())
		configFilename := filepath.Join(testcasePath, "telegraf.toml")
		scanFilename := filepath.Join(testcasePath, "response.json")
		expectedFilename := filepath.Join(testcasePath, "expected.out")

		t.Run(f.Name(), func(t *testing.T) {
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())

			// Read the expected output if any
			var expected int
			if _, err := os.Stat(expectedFilename); err == nil {
				var err error
				expectedBytes, err := os.ReadFile(expectedFilename)
				require.NoError(t, err)
				expected, err = strconv.Atoi(strings.TrimSpace(string(expectedBytes)))
				require.NoError(t, err)
			}

			// Update exec to return fake data.
			execCommand = fakeScanExecCommand
			defer func() { execCommand = exec.Command }()

			// Configure the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFilename))
			require.Len(t, cfg.Inputs, 1)
			plugin := cfg.Inputs[0].Input.(*Smartctl)
			require.NoError(t, plugin.Init())

			scanArgs = append(scanArgs, scanFilename)
			devices, err := plugin.scan()
			require.NoError(t, err)
			require.Len(t, devices, expected)
		})
	}
}

func fakeScanExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestScanHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func TestScanHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args

	scanBytes, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		fmt.Fprint(os.Stdout, "unknown filename")
		//nolint:revive // os.Exit called intentionally
		os.Exit(42)
	}

	fmt.Fprint(os.Stdout, string(scanBytes))
	//nolint:revive // os.Exit called intentionally
	os.Exit(0)
}

func TestCasesDevices(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases_device")
	require.NoError(t, err)

	// Register the plugin
	inputs.Add("smartctl", func() telegraf.Input {
		return &Smartctl{}
	})

	for _, f := range folders {
		if !f.IsDir() {
			continue
		}
		testcasePath := filepath.Join("testcases_device", f.Name())
		deviceFilename := filepath.Join(testcasePath, "device")
		deviceTypeFilename := filepath.Join(testcasePath, "deviceType")
		expectedFilename := filepath.Join(testcasePath, "expected.out")

		t.Run(f.Name(), func(t *testing.T) {
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())

			// Read the expected output if any
			var expected []telegraf.Metric
			if _, err := os.Stat(expectedFilename); err == nil {
				var err error
				expected, err = testutil.ParseMetricsFromFile(expectedFilename, parser)
				require.NoError(t, err)
			}

			// Read the devices to scan
			deviceBytes, err := os.ReadFile(deviceFilename)
			require.NoError(t, err)
			deviceTypeBytes, err := os.ReadFile(deviceTypeFilename)
			require.NoError(t, err)

			// Update exec to return fake data.
			execCommand = fakeDeviceExecCommand
			defer func() { execCommand = exec.Command }()

			// Configure the plugin
			plugin := Smartctl{}
			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator
			require.NoError(t,
				plugin.scanDevice(
					&acc,
					strings.TrimSpace(string(deviceBytes)),
					strings.TrimSpace(string(deviceTypeBytes)),
				),
			)

			// Check the metric nevertheless as we might get some metrics despite errors.
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
			acc.Lock()
			defer acc.Unlock()
			require.Empty(t, acc.Errors)
		})
	}
}

func fakeDeviceExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestDeviceHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func TestDeviceHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args

	var filename string
	if slices.Contains(args, "/dev/nvme0") {
		filename = "testcases_device/nvme/response.json"
	} else if slices.Contains(args, "/dev/sda") {
		filename = "testcases_device/usb/response.json"
	} else if slices.Contains(args, "/dev/bus/6") {
		filename = "testcases_device/megaraid/response.json"
	} else if slices.Contains(args, "/dev/sdb") {
		filename = "testcases_device/scsi/response.json"
	} else if slices.Contains(args, "/dev/sdaa") {
		filename = "testcases_device/scsi_extended/response.json"
	} else {
		fmt.Fprint(os.Stdout, "unknown filename")
		os.Exit(42) //nolint:revive // os.Exit called intentionally
	}

	scanBytes, err := os.ReadFile(filename)
	require.NoError(t, err)
	fmt.Fprint(os.Stdout, string(scanBytes))
	os.Exit(0) //nolint:revive // os.Exit called intentionally
}
