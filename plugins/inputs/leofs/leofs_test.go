package leofs

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

var fakeSNMP4Manager = `
package main

import "fmt"

const output = ` + "`" + `.1.3.6.1.4.1.35450.15.1.0 = STRING: "manager_888@127.0.0.1"
.1.3.6.1.4.1.35450.15.2.0 = Gauge32: 186
.1.3.6.1.4.1.35450.15.3.0 = Gauge32: 46235519
.1.3.6.1.4.1.35450.15.4.0 = Gauge32: 32168525
.1.3.6.1.4.1.35450.15.5.0 = Gauge32: 14066068
.1.3.6.1.4.1.35450.15.6.0 = Gauge32: 5512968
.1.3.6.1.4.1.35450.15.7.0 = Gauge32: 186
.1.3.6.1.4.1.35450.15.8.0 = Gauge32: 46269006
.1.3.6.1.4.1.35450.15.9.0 = Gauge32: 32202867
.1.3.6.1.4.1.35450.15.10.0 = Gauge32: 14064995
.1.3.6.1.4.1.35450.15.11.0 = Gauge32: 5492634
.1.3.6.1.4.1.35450.15.12.0 = Gauge32: 60
.1.3.6.1.4.1.35450.15.13.0 = Gauge32: 43515904
.1.3.6.1.4.1.35450.15.14.0 = Gauge32: 60
.1.3.6.1.4.1.35450.15.15.0 = Gauge32: 43533983` + "`" +
	`
func main() {
	fmt.Println(output)
}
`

var fakeSNMP4Storage = `
package main

import "fmt"

const output = ` + "`" + `.1.3.6.1.4.1.35450.56.1.0 = STRING: "storage_0@127.0.0.1"
.1.3.6.1.4.1.35450.56.2.0 = Gauge32: 512
.1.3.6.1.4.1.35450.56.3.0 = Gauge32: 38126307
.1.3.6.1.4.1.35450.56.4.0 = Gauge32: 22308716
.1.3.6.1.4.1.35450.56.5.0 = Gauge32: 15816448
.1.3.6.1.4.1.35450.56.6.0 = Gauge32: 5232008
.1.3.6.1.4.1.35450.56.7.0 = Gauge32: 512
.1.3.6.1.4.1.35450.56.8.0 = Gauge32: 38113176
.1.3.6.1.4.1.35450.56.9.0 = Gauge32: 22313398
.1.3.6.1.4.1.35450.56.10.0 = Gauge32: 15798779
.1.3.6.1.4.1.35450.56.11.0 = Gauge32: 5237315
.1.3.6.1.4.1.35450.56.12.0 = Gauge32: 191
.1.3.6.1.4.1.35450.56.13.0 = Gauge32: 824
.1.3.6.1.4.1.35450.56.14.0 = Gauge32: 0
.1.3.6.1.4.1.35450.56.15.0 = Gauge32: 50105
.1.3.6.1.4.1.35450.56.16.0 = Gauge32: 196654
.1.3.6.1.4.1.35450.56.17.0 = Gauge32: 0
.1.3.6.1.4.1.35450.56.18.0 = Gauge32: 2052
.1.3.6.1.4.1.35450.56.19.0 = Gauge32: 50296
.1.3.6.1.4.1.35450.56.20.0 = Gauge32: 35
.1.3.6.1.4.1.35450.56.21.0 = Gauge32: 898
.1.3.6.1.4.1.35450.56.22.0 = Gauge32: 0
.1.3.6.1.4.1.35450.56.23.0 = Gauge32: 0
.1.3.6.1.4.1.35450.56.24.0 = Gauge32: 0
.1.3.6.1.4.1.35450.56.31.0 = Gauge32: 51
.1.3.6.1.4.1.35450.56.32.0 = Gauge32: 53219328
.1.3.6.1.4.1.35450.56.33.0 = Gauge32: 51
.1.3.6.1.4.1.35450.56.34.0 = Gauge32: 53351083
.1.3.6.1.4.1.35450.56.41.0 = Gauge32: 101
.1.3.6.1.4.1.35450.56.42.0 = Gauge32: 216
.1.3.6.1.4.1.35450.56.43.0 = Gauge32: 313
.1.3.6.1.4.1.35450.56.44.0 = Gauge32: 421
.1.3.6.1.4.1.35450.56.45.0 = Gauge32: 597
.1.3.6.1.4.1.35450.56.46.0 = Gauge32: 628
.1.3.6.1.4.1.35450.56.51.0 = Gauge32: 1
.1.3.6.1.4.1.35450.56.52.0 = Gauge32: 1522154118
.1.3.6.1.4.1.35450.56.53.0 = Gauge32: 1522196496
.1.3.6.1.4.1.35450.56.54.0 = Gauge32: 1
.1.3.6.1.4.1.35450.56.55.0 = Gauge32: 7
.1.3.6.1.4.1.35450.56.56.0 = Gauge32: 0` + "`" +
	`
func main() {
	fmt.Println(output)
}
`

var fakeSNMP4Gateway = `
package main

import "fmt"

const output = ` + "`" + `.1.3.6.1.4.1.35450.34.1.0 = STRING: "gateway_0@127.0.0.1"
.1.3.6.1.4.1.35450.34.2.0 = Gauge32: 465
.1.3.6.1.4.1.35450.34.3.0 = Gauge32: 61676335
.1.3.6.1.4.1.35450.34.4.0 = Gauge32: 46890415
.1.3.6.1.4.1.35450.34.5.0 = Gauge32: 14785011
.1.3.6.1.4.1.35450.34.6.0 = Gauge32: 5578855
.1.3.6.1.4.1.35450.34.7.0 = Gauge32: 465
.1.3.6.1.4.1.35450.34.8.0 = Gauge32: 61644426
.1.3.6.1.4.1.35450.34.9.0 = Gauge32: 46880358
.1.3.6.1.4.1.35450.34.10.0 = Gauge32: 14763002
.1.3.6.1.4.1.35450.34.11.0 = Gauge32: 5582125
.1.3.6.1.4.1.35450.34.12.0 = Gauge32: 191
.1.3.6.1.4.1.35450.34.13.0 = Gauge32: 827
.1.3.6.1.4.1.35450.34.14.0 = Gauge32: 0
.1.3.6.1.4.1.35450.34.15.0 = Gauge32: 50105
.1.3.6.1.4.1.35450.34.16.0 = Gauge32: 196650
.1.3.6.1.4.1.35450.34.17.0 = Gauge32: 0
.1.3.6.1.4.1.35450.34.18.0 = Gauge32: 30256
.1.3.6.1.4.1.35450.34.19.0 = Gauge32: 532158
.1.3.6.1.4.1.35450.34.20.0 = Gauge32: 34
.1.3.6.1.4.1.35450.34.21.0 = Gauge32: 1
.1.3.6.1.4.1.35450.34.31.0 = Gauge32: 53
.1.3.6.1.4.1.35450.34.32.0 = Gauge32: 55050240
.1.3.6.1.4.1.35450.34.33.0 = Gauge32: 53
.1.3.6.1.4.1.35450.34.34.0 = Gauge32: 55186538` + "`" +
	`
func main() {
	fmt.Println(output)
}
`

func testMain(t *testing.T, code, endpoint string, serverType serverType) {
	executable := "snmpwalk"
	if runtime.GOOS == "windows" {
		executable = "snmpwalk.exe"
	}

	// Build the fake snmpwalk for test
	src := os.TempDir() + "/test.go"
	require.NoError(t, os.WriteFile(src, []byte(code), 0600))
	defer os.Remove(src)

	require.NoError(t, exec.Command("go", "build", "-o", executable, src).Run())
	defer os.Remove("./" + executable)

	currentWorkingDirectory, err := os.Getwd()
	require.NoError(t, err)

	// Refer to the fake snmpwalk
	t.Setenv("PATH", currentWorkingDirectory)

	l := &LeoFS{
		Servers: []string{endpoint},
	}

	var acc testutil.Accumulator
	acc.SetDebug(true)

	err = acc.GatherError(l.Gather)
	require.NoError(t, err)

	floatMetrics := keyMapping[serverType]

	for _, metric := range floatMetrics {
		require.True(t, acc.HasFloatField("leofs", metric), metric)
	}
}

func TestLeoFSManagerMasterMetricsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testMain(t, fakeSNMP4Manager, "localhost:4020", serverTypeManagerMaster)
}

func TestLeoFSManagerSlaveMetricsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testMain(t, fakeSNMP4Manager, "localhost:4021", serverTypeManagerSlave)
}

func TestLeoFSStorageMetricsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testMain(t, fakeSNMP4Storage, "localhost:4010", serverTypeStorage)
}

func TestLeoFSGatewayMetricsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testMain(t, fakeSNMP4Gateway, "localhost:4000", serverTypeGateway)
}
