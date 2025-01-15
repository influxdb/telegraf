package proxmox

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

var nodeSearchDomainTestData = `{"data":{"search":"test.example.com","dns1":"1.0.0.1"}}`
var qemuTestData = `{"data":[{"name":"qemu1","status":"running","maxdisk":10737418240,"cpu":0.029336643550795,"vmid":"113","uptime":2159739,` +
	`"disk":0,"maxmem":2147483648,"mem":1722451796}]}`
var qemuConfigTestData = `{"data":{"hostname":"qemu1","searchdomain":"test.example.com"}}`
var lxcTestData = `{"data":[{"vmid":"111","type":"lxc","uptime":2078164,"swap":9412608,"disk":"744189952","maxmem":536870912,"mem":98500608,` +
	`"maxswap":536870912,"cpu":0.00371567669193613,"status":"running","maxdisk":"5217320960","name":"container1"},{"vmid":112,"type":"lxc",` +
	`"uptime":2078164,"swap":9412608,"disk":"744189952","maxmem":536870912,"mem":98500608,"maxswap":536870912,"cpu":0.00371567669193613,` +
	`"status":"running","maxdisk":"5217320960","name":"container2"}]}`
var lxcConfigTestData = `{"data":{"hostname":"container1","searchdomain":"test.example.com"}}`
var lxcCurrentStatusTestData = `{"data":{"vmid":"111","type":"lxc","uptime":2078164,"swap":9412608,"disk":"744189952","maxmem":536870912,` +
	`"mem":98500608,"maxswap":536870912,"cpu":0.00371567669193613,"status":"running","maxdisk":"5217320960","name":"container1"}}`
var qemuCurrentStatusTestData = `{"data":{"name":"qemu1","status":"running","maxdisk":10737418240,"cpu":0.029336643550795,"vmid":"113",` +
	`"uptime":2159739,"disk":0,"maxmem":2147483648,"mem":1722451796}}`

func performTestRequest(apiURL, _ string, _ url.Values) ([]byte, error) {
	var bytedata = []byte("")

	if strings.HasSuffix(apiURL, "dns") {
		bytedata = []byte(nodeSearchDomainTestData)
	} else if strings.HasSuffix(apiURL, "qemu") {
		bytedata = []byte(qemuTestData)
	} else if strings.HasSuffix(apiURL, "113/config") {
		bytedata = []byte(qemuConfigTestData)
	} else if strings.HasSuffix(apiURL, "lxc") {
		bytedata = []byte(lxcTestData)
	} else if strings.HasSuffix(apiURL, "111/config") {
		bytedata = []byte(lxcConfigTestData)
	} else if strings.HasSuffix(apiURL, "111/status/current") {
		bytedata = []byte(lxcCurrentStatusTestData)
	} else if strings.HasSuffix(apiURL, "113/status/current") {
		bytedata = []byte(qemuCurrentStatusTestData)
	}

	return bytedata, nil
}

func TestGetNodeSearchDomain(t *testing.T) {
	px := &Proxmox{
		NodeName: "testnode",
		Log:      testutil.Logger{},
	}
	require.NoError(t, px.Init())
	px.requestFunction = performTestRequest

	require.NoError(t, px.getNodeSearchDomain())
	require.Equal(t, "test.example.com", px.nodeSearchDomain)
}

func TestGatherLxcData(t *testing.T) {
	px := &Proxmox{
		NodeName:         "testnode",
		Log:              testutil.Logger{},
		nodeSearchDomain: "test.example.com",
	}
	require.NoError(t, px.Init())
	px.requestFunction = performTestRequest

	var acc testutil.Accumulator
	px.gatherVMData(&acc, lxc)

	expected := []telegraf.Metric{
		metric.New(
			"proxmox",
			map[string]string{
				"node_fqdn": "testnode.test.example.com",
				"vm_name":   "container1",
				"vm_fqdn":   "container1.test.example.com",
				"vm_type":   "lxc",
			},
			map[string]interface{}{
				"status":               "running",
				"uptime":               int64(2078164),
				"cpuload":              float64(0.00371567669193613),
				"mem_used":             int64(98500608),
				"mem_total":            int64(536870912),
				"mem_free":             int64(438370304),
				"mem_used_percentage":  float64(18.34716796875),
				"swap_used":            int64(9412608),
				"swap_total":           int64(536870912),
				"swap_free":            int64(527458304),
				"swap_used_percentage": float64(1.75323486328125),
				"disk_used":            int64(744189952),
				"disk_total":           int64(5217320960),
				"disk_free":            int64(4473131008),
				"disk_used_percentage": float64(14.26383306117322),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestGatherQemuData(t *testing.T) {
	px := &Proxmox{
		NodeName:         "testnode",
		Log:              testutil.Logger{},
		nodeSearchDomain: "test.example.com",
	}
	require.NoError(t, px.Init())
	px.requestFunction = performTestRequest

	var acc testutil.Accumulator
	px.gatherVMData(&acc, qemu)

	expected := []telegraf.Metric{
		metric.New(
			"proxmox",
			map[string]string{
				"node_fqdn": "testnode.test.example.com",
				"vm_name":   "qemu1",
				"vm_fqdn":   "qemu1.test.example.com",
				"vm_type":   "qemu",
			},
			map[string]interface{}{
				"status":               "running",
				"uptime":               int64(2159739),
				"cpuload":              float64(0.029336643550795),
				"mem_used":             int64(1722451796),
				"mem_total":            int64(2147483648),
				"mem_free":             int64(425031852),
				"mem_used_percentage":  float64(80.20791206508875),
				"swap_used":            int64(0),
				"swap_total":           int64(0),
				"swap_free":            int64(0),
				"swap_used_percentage": float64(0),
				"disk_used":            int64(0),
				"disk_total":           int64(10737418240),
				"disk_free":            int64(10737418240),
				"disk_used_percentage": float64(0),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestGatherLxcDataWithID(t *testing.T) {
	px := &Proxmox{
		NodeName:              "testnode",
		AdditionalVmstatsTags: []string{"vmid"},
		Log:                   testutil.Logger{},
		nodeSearchDomain:      "test.example.com",
	}
	require.NoError(t, px.Init())
	px.requestFunction = performTestRequest

	var acc testutil.Accumulator
	px.gatherVMData(&acc, lxc)

	expected := []telegraf.Metric{
		metric.New(
			"proxmox",
			map[string]string{
				"node_fqdn": "testnode.test.example.com",
				"vm_name":   "container1",
				"vm_fqdn":   "container1.test.example.com",
				"vm_type":   "lxc",
				"vm_id":     "111",
			},
			map[string]interface{}{
				"status":               "running",
				"uptime":               int64(2078164),
				"cpuload":              float64(0.00371567669193613),
				"mem_used":             int64(98500608),
				"mem_total":            int64(536870912),
				"mem_free":             int64(438370304),
				"mem_used_percentage":  float64(18.34716796875),
				"swap_used":            int64(9412608),
				"swap_total":           int64(536870912),
				"swap_free":            int64(527458304),
				"swap_used_percentage": float64(1.75323486328125),
				"disk_used":            int64(744189952),
				"disk_total":           int64(5217320960),
				"disk_free":            int64(4473131008),
				"disk_used_percentage": float64(14.26383306117322),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestGatherQemuDataWithID(t *testing.T) {
	px := &Proxmox{
		NodeName:              "testnode",
		AdditionalVmstatsTags: []string{"vmid"},
		Log:                   testutil.Logger{},
		nodeSearchDomain:      "test.example.com",
	}
	require.NoError(t, px.Init())
	px.requestFunction = performTestRequest

	var acc testutil.Accumulator
	px.gatherVMData(&acc, qemu)

	expected := []telegraf.Metric{
		metric.New(
			"proxmox",
			map[string]string{
				"node_fqdn": "testnode.test.example.com",
				"vm_name":   "qemu1",
				"vm_fqdn":   "qemu1.test.example.com",
				"vm_type":   "qemu",
				"vm_id":     "113",
			},
			map[string]interface{}{
				"status":               "running",
				"uptime":               int64(2159739),
				"cpuload":              float64(0.029336643550795),
				"mem_used":             int64(1722451796),
				"mem_total":            int64(2147483648),
				"mem_free":             int64(425031852),
				"mem_used_percentage":  float64(80.20791206508875),
				"swap_used":            int64(0),
				"swap_total":           int64(0),
				"swap_free":            int64(0),
				"swap_used_percentage": float64(0),
				"disk_used":            int64(0),
				"disk_total":           int64(10737418240),
				"disk_free":            int64(10737418240),
				"disk_used_percentage": float64(0),
			},
			time.Unix(0, 0),
		),
	}
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestGather(t *testing.T) {
	px := &Proxmox{
		NodeName: "testnode",
		Log:      testutil.Logger{},
	}
	require.NoError(t, px.Init())
	px.requestFunction = performTestRequest

	var acc testutil.Accumulator
	require.NoError(t, px.Gather(&acc))

	// Results from both tests above
	require.Equal(t, 30, acc.NFields())
}
