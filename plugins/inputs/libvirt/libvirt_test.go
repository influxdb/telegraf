package libvirt

import (
	"errors"
	"testing"
	"time"

	golibvirt "github.com/digitalocean/go-libvirt"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestLibvirt_Init(t *testing.T) {
	t.Run("throw error when user provided duplicated state metric name", func(t *testing.T) {
		l := Libvirt{
			StatisticsGroups: []string{"state", "state"},
			Log:              testutil.Logger{},
		}
		err := l.Init()
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicated statistics group in config")
	})

	t.Run("throw error when user provided wrong metric name", func(t *testing.T) {
		l := Libvirt{
			StatisticsGroups: []string{"statusQvo"},
			Log:              testutil.Logger{},
		}
		err := l.Init()
		require.Error(t, err)
		require.Contains(t, err.Error(), "unrecognized metrics name")
	})

	t.Run("throw error when user provided invalid uri", func(t *testing.T) {
		mockUtils := mockLibvirtUtils{}
		l := Libvirt{
			LibvirtURI: "this/is/wrong/uri",
			utils:      &mockUtils,
			Log:        testutil.Logger{},
		}
		err := l.Init()
		require.Error(t, err)
		require.Contains(t, err.Error(), "can't parse")
	})

	t.Run("successfully initialize libvirt on correct user input", func(t *testing.T) {
		mockUtils := mockLibvirtUtils{}
		l := Libvirt{
			StatisticsGroups: []string{"state", "cpu_total", "vcpu", "interface"},
			utils:            &mockUtils,
			LibvirtURI:       defaultLibvirtURI,
			Log:              testutil.Logger{},
		}
		err := l.Init()
		require.NoError(t, err)
	})
}

func TestLibvirt_Gather(t *testing.T) {
	t.Run("wrong uri throws error", func(t *testing.T) {
		var acc testutil.Accumulator
		mockUtils := mockLibvirtUtils{}
		l := Libvirt{
			LibvirtURI: "this/is/wrong/uri",
			Log:        testutil.Logger{},
			utils:      &mockUtils,
		}
		mockUtils.On("ensureConnected", mock.Anything).Return(errors.New("failed to connect")).Once()
		err := l.Gather(&acc)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to connect")
		mockUtils.AssertExpectations(t)
	})

	t.Run("error when read error happened in gathering domains", func(t *testing.T) {
		var acc testutil.Accumulator
		mockUtils := mockLibvirtUtils{}
		l := Libvirt{
			utils:            &mockUtils,
			Log:              testutil.Logger{},
			StatisticsGroups: []string{"state"},
		}
		mockUtils.On("ensureConnected", mock.Anything).Return(nil).Once().
			On("gatherAllDomains", mock.Anything).Return(nil, errors.New("gather domain error")).Once().
			On("disconnect").Return(nil).Once()

		err := l.Gather(&acc)
		require.Error(t, err)
		require.Contains(t, err.Error(), "gather domain error")
		mockUtils.AssertExpectations(t)
	})

	t.Run("no error when empty list of domains is returned", func(t *testing.T) {
		var acc testutil.Accumulator
		mockUtils := mockLibvirtUtils{}
		l := Libvirt{
			utils:            &mockUtils,
			Log:              testutil.Logger{},
			StatisticsGroups: []string{"state"},
		}
		mockUtils.On("ensureConnected", mock.Anything).Return(nil).Once().
			On("gatherAllDomains", mock.Anything).Return(nil, nil).Once()

		err := l.Gather(&acc)
		require.NoError(t, err)
		mockUtils.AssertExpectations(t)
	})

	t.Run("error when gathering metrics by number", func(t *testing.T) {
		var acc testutil.Accumulator
		mockUtils := mockLibvirtUtils{}
		l := Libvirt{
			utils:            &mockUtils,
			Log:              testutil.Logger{},
			StatisticsGroups: []string{"state"},
		}
		mockUtils.On("ensureConnected", mock.Anything).Return(nil).Once().
			On("gatherAllDomains", mock.Anything).Return(domains, nil).Once().
			On("gatherStatsForDomains", mock.Anything, mock.Anything).
			Return(nil, errors.New("gathering metric by number error")).Once().
			On("disconnect").Return(nil).Once()

		err := l.Init()
		require.NoError(t, err)

		err = l.Gather(&acc)
		require.Error(t, err)
		require.Contains(t, err.Error(), "gathering metric by number error")
		mockUtils.AssertExpectations(t)
	})

	var successfulTests = []struct {
		testName        string
		allDomains      interface{}
		excludeDomains  []string
		statsForDomains interface{}
		expectedMetrics []telegraf.Metric
		vcpuMapping     []vcpuAffinity
	}{
		{"successfully gather from host that has domains", domains, nil, domainStats, append(expectedMetrics, expectedVcpuAffinityMetrics...), vcpusMapping},
		{
			"successfully gather from host for excluded domain",
			domains,
			[]string{"Droplet-33436"},
			domainStats[1:],
			append(expectedMetrics[1:], expectedVcpuAffinityMetrics[2:]...),
			vcpusMapping,
		},
	}
	for _, test := range successfulTests {
		t.Run(test.testName, func(t *testing.T) {
			var acc testutil.Accumulator
			mockUtils := mockLibvirtUtils{}
			l := Libvirt{
				utils:                &mockUtils,
				Log:                  testutil.Logger{},
				StatisticsGroups:     []string{"state"},
				Domains:              test.excludeDomains,
				AdditionalStatistics: []string{"vcpu_mapping"},
			}
			mockUtils.On("ensureConnected", mock.Anything).Return(nil).Once().
				On("gatherAllDomains", mock.Anything).Return(test.allDomains, nil).Once().
				On("gatherVcpuMapping", domains[0], mock.Anything, mock.Anything).Return(test.vcpuMapping, nil).Maybe().
				On("gatherVcpuMapping", domains[1], mock.Anything, mock.Anything).Return(test.vcpuMapping, nil).Once().
				On("gatherNumberOfPCPUs").Return(4, nil).Once().
				On("gatherStatsForDomains", mock.Anything, mock.Anything).Return(test.statsForDomains, nil).Once()

			err := l.Init()
			require.NoError(t, err)

			err = l.Gather(&acc)
			require.NoError(t, err)

			actual := acc.GetTelegrafMetrics()
			expected := test.expectedMetrics
			testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics(), testutil.IgnoreTime())
			mockUtils.AssertExpectations(t)
		})
	}
}

func TestLibvirt_GatherMetrics(t *testing.T) {
	var successfulTests = []struct {
		testName        string
		allDomains      interface{}
		excludeDomains  []string
		statsForDomains interface{}
		expectedMetrics []telegraf.Metric
		vcpuMapping     []vcpuAffinity
	}{
		{"successfully gather memory metrics from host that has domains", domains, nil, memoryStats, expectedMemoryMetrics, nil},
		{"successfully gather balloon metrics from host that has domains", domains, nil, balloonStats, expectedBalloonMetrics, nil},
		{"successfully gather perf metrics from host that has domains", domains, nil, perfStats, expectedPerfMetrics, nil},
		{"successfully gather cpu metrics from host that has domains", domains, nil, cpuStats, expectedCPUMetrics, nil},
		{"successfully gather interface metrics from host that has domains", domains, nil, interfaceStats, expectedInterfaceMetrics, nil},
		{"successfully gather block metrics from host that has domains", domains, nil, blockStats, expectedBlockMetrics, nil},
		{"successfully gather iothread metrics from host that has domains", domains, nil, iothreadStats, expectedIOThreadMetrics, nil},
		{"successfully gather dirtyrate metrics from host that has domains", domains, nil, dirtyrateStats, expectedDirtyrateMetrics, nil},
		{"successfully gather vcpu metrics from host that has domains", domains, nil, vcpuStats, expectedVCPUMetrics, nil},
		{"successfully gather vcpu metrics with vCPU from host that has domains", domains, nil, vcpuStats, expectedExtendedVCPUMetrics, vcpusMapping},
	}
	for _, test := range successfulTests {
		t.Run(test.testName, func(t *testing.T) {
			var acc testutil.Accumulator
			mockUtils := mockLibvirtUtils{}
			l := Libvirt{
				utils:   &mockUtils,
				Log:     testutil.Logger{},
				Domains: test.excludeDomains,
			}

			mockUtils.On("ensureConnected", mock.Anything).Return(nil).Once().
				On("gatherAllDomains", mock.Anything).Return(test.allDomains, nil).Once().
				On("gatherStatsForDomains", mock.Anything, mock.Anything).Return(test.statsForDomains, nil).Once()

			if test.vcpuMapping != nil {
				l.vcpuMappingEnabled = true
				l.metricNumber = domainStatsVCPU
				mockUtils.On("gatherNumberOfPCPUs").Return(4, nil).Once().
					On("gatherVcpuMapping", domains[0], mock.Anything, mock.Anything).Return(test.vcpuMapping, nil).Once().
					On("gatherVcpuMapping", domains[1], mock.Anything, mock.Anything).Return(nil, nil).Once()
			}

			err := l.Gather(&acc)
			require.NoError(t, err)

			actual := acc.GetTelegrafMetrics()
			expected := test.expectedMetrics
			testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics(), testutil.IgnoreTime())
			mockUtils.AssertExpectations(t)
		})
	}
}

func TestLibvirt_validateLibvirtUri(t *testing.T) {
	t.Run("no error on good uri provided", func(t *testing.T) {
		l := Libvirt{
			LibvirtURI: defaultLibvirtURI,
			Log:        testutil.Logger{},
		}
		err := l.validateLibvirtURI()
		require.NoError(t, err)
	})

	t.Run("unmarshal error on bad uri provided", func(t *testing.T) {
		l := Libvirt{
			LibvirtURI: "this/is/invalid/uri",
			Log:        testutil.Logger{},
		}
		err := l.validateLibvirtURI()
		require.Error(t, err)
		require.Contains(t, err.Error(), "can't parse '"+l.LibvirtURI+"' as a libvirt uri")
	})

	t.Run("dialer error on bad ssh uri provided", func(t *testing.T) {
		l := Libvirt{
			LibvirtURI: "qemu+ssh://invalid@host:666/system",
			Log:        testutil.Logger{},
		}
		err := l.validateLibvirtURI()
		require.Error(t, err)
		require.Contains(t, err.Error(), "ssh transport requires keyfile parameter")
	})
}

func TestLibvirt_calculateMetricNumber(t *testing.T) {
	t.Run("error on duplicated metric name", func(t *testing.T) {
		l := Libvirt{
			StatisticsGroups: []string{"state", "state"},
			Log:              testutil.Logger{},
		}
		err := l.calculateMetricNumber()
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicated statistics group in config")
	})

	t.Run("error on unrecognized metric name", func(t *testing.T) {
		l := Libvirt{
			StatisticsGroups: []string{"invalidName"},
			Log:              testutil.Logger{},
		}
		err := l.calculateMetricNumber()
		require.Error(t, err)
		require.Contains(t, err.Error(), "unrecognized metrics name")
	})

	t.Run("correctly calculates metrics number provided", func(t *testing.T) {
		metrics := []string{"state", "cpu_total", "vcpu", "interface", "block", "balloon",
			"memory", "perf", "iothread", "dirtyrate"}

		l := Libvirt{
			StatisticsGroups: metrics,
			Log:              testutil.Logger{},
		}
		err := l.calculateMetricNumber()
		require.NoError(t, err)
		require.Equal(t, domainStatsAll, l.metricNumber)
	})
}

func TestLibvirt_filterDomains(t *testing.T) {
	t.Run("success filter domains", func(t *testing.T) {
		l := Libvirt{
			Domains: []string{"Droplet-844329", "Droplet-33436"},
			Log:     testutil.Logger{},
		}

		result := l.filterDomains(domains)
		require.NotEmpty(t, result)
	})
}

var (
	domains = []golibvirt.Domain{
		{Name: "Droplet-844329", UUID: golibvirt.UUID{}, ID: 0},
		{Name: "Droplet-33436", UUID: golibvirt.UUID{}, ID: 0},
	}

	domainStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "state.reason", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "state.state", Value: *golibvirt.NewTypedParamValueLlong(1)},
			},
		},
		{
			Dom: domains[1],
			Params: []golibvirt.TypedParam{
				{Field: "state.reason", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "state.state", Value: *golibvirt.NewTypedParamValueLlong(1)},
			},
		},
	}

	memoryStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "memory.bandwidth.monitor.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "memory.bandwidth.monitor.0.name", Value: *golibvirt.NewTypedParamValueString("any_name_vcpus_0-4")},
				{Field: "memory.bandwidth.monitor.0.vcpus", Value: *golibvirt.NewTypedParamValueString("0-4")},
				{Field: "memory.bandwidth.monitor.0.node.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "memory.bandwidth.monitor.1.name", Value: *golibvirt.NewTypedParamValueString("vcpus_7")},
				{Field: "memory.bandwidth.monitor.1.vcpus", Value: *golibvirt.NewTypedParamValueString("7")},
				{Field: "memory.bandwidth.monitor.1.node.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "memory.bandwidth.monitor.0.node.0.id", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "memory.bandwidth.monitor.0.node.0.bytes.total", Value: *golibvirt.NewTypedParamValueLlong(10208067584)},
				{Field: "memory.bandwidth.monitor.0.node.0.bytes.local", Value: *golibvirt.NewTypedParamValueLlong(4807114752)},
				{Field: "memory.bandwidth.monitor.0.node.1.id", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "memory.bandwidth.monitor.0.node.1.bytes.total", Value: *golibvirt.NewTypedParamValueLlong(8693735424)},
				{Field: "memory.bandwidth.monitor.0.node.1.bytes.local", Value: *golibvirt.NewTypedParamValueLlong(5850161152)},
				{Field: "memory.bandwidth.monitor.1.node.0.id", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "memory.bandwidth.monitor.1.node.0.bytes.total", Value: *golibvirt.NewTypedParamValueLlong(853811200)},
				{Field: "memory.bandwidth.monitor.1.node.0.bytes.local", Value: *golibvirt.NewTypedParamValueLlong(290701312)},
				{Field: "memory.bandwidth.monitor.1.node.1.id", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "memory.bandwidth.monitor.1.node.1.bytes.total", Value: *golibvirt.NewTypedParamValueLlong(406044672)},
				{Field: "memory.bandwidth.monitor.1.node.1.bytes.local", Value: *golibvirt.NewTypedParamValueLlong(229425152)},
			},
		},
	}

	cpuStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "cpu.time", Value: *golibvirt.NewTypedParamValueLlong(67419144867000)},
				{Field: "cpu.user", Value: *golibvirt.NewTypedParamValueLlong(63886161852000)},
				{Field: "cpu.system", Value: *golibvirt.NewTypedParamValueLlong(3532983015000)},
				{Field: "cpu.haltpoll.success.time", Value: *golibvirt.NewTypedParamValueLlong(516907915)},
				{Field: "cpu.haltpoll.fail.time", Value: *golibvirt.NewTypedParamValueLlong(2727253643)},
				{Field: "cpu.cache.monitor.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "cpu.cache.monitor.0.name", Value: *golibvirt.NewTypedParamValueString("any_name_vcpus_0-3")},
				{Field: "cpu.cache.monitor.0.vcpus", Value: *golibvirt.NewTypedParamValueString("0-3")},
				{Field: "cpu.cache.monitor.0.bank.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "cpu.cache.monitor.1.name", Value: *golibvirt.NewTypedParamValueString("vcpus_4-9")},
				{Field: "cpu.cache.monitor.1.vcpus", Value: *golibvirt.NewTypedParamValueString("4-9")},
				{Field: "cpu.cache.monitor.1.bank.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "cpu.cache.monitor.0.bank.0.id", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "cpu.cache.monitor.0.bank.0.bytes", Value: *golibvirt.NewTypedParamValueLlong(5406720)},
				{Field: "cpu.cache.monitor.0.bank.1.id", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "cpu.cache.monitor.0.bank.1.bytes", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "cpu.cache.monitor.1.bank.0.id", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "cpu.cache.monitor.1.bank.0.bytes", Value: *golibvirt.NewTypedParamValueLlong(720896)},
				{Field: "cpu.cache.monitor.1.bank.1.id", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "cpu.cache.monitor.1.bank.1.bytes", Value: *golibvirt.NewTypedParamValueLlong(8200192)},
			},
		},
	}

	balloonStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "balloon.current", Value: *golibvirt.NewTypedParamValueLlong(4194304)},
				{Field: "balloon.maximum", Value: *golibvirt.NewTypedParamValueLlong(4194304)},
				{Field: "balloon.swap_in", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "balloon.swap_out", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "balloon.major_fault", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "balloon.minor_fault", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "balloon.unused", Value: *golibvirt.NewTypedParamValueLlong(3928628)},
				{Field: "balloon.available", Value: *golibvirt.NewTypedParamValueLlong(4018480)},
				{Field: "balloon.rss", Value: *golibvirt.NewTypedParamValueLlong(1036012)},
				{Field: "balloon.usable", Value: *golibvirt.NewTypedParamValueLlong(3808724)},
				{Field: "balloon.last-update", Value: *golibvirt.NewTypedParamValueLlong(1654611373)},
				{Field: "balloon.disk_caches", Value: *golibvirt.NewTypedParamValueLlong(68820)},
				{Field: "balloon.hugetlb_pgalloc", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "balloon.hugetlb_pgfail", Value: *golibvirt.NewTypedParamValueLlong(0)},
			},
		},
	}

	perfStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "perf.cmt", Value: *golibvirt.NewTypedParamValueLlong(19087360)},
				{Field: "perf.mbmt", Value: *golibvirt.NewTypedParamValueLlong(77168640)},
				{Field: "perf.mbml", Value: *golibvirt.NewTypedParamValueLlong(67788800)},
				{Field: "perf.cpu_cycles", Value: *golibvirt.NewTypedParamValueLlong(29858995122)},
				{Field: "perf.instructions", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "perf.cache_references", Value: *golibvirt.NewTypedParamValueLlong(3053301695)},
				{Field: "perf.cache_misses", Value: *golibvirt.NewTypedParamValueLlong(609441024)},
				{Field: "perf.branch_instructions", Value: *golibvirt.NewTypedParamValueLlong(2623890194)},
				{Field: "perf.branch_misses", Value: *golibvirt.NewTypedParamValueLlong(103707961)},
				{Field: "perf.bus_cycles", Value: *golibvirt.NewTypedParamValueLlong(188105628)},
				{Field: "perf.stalled_cycles_frontend", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "perf.stalled_cycles_backend", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "perf.ref_cpu_cycles", Value: *golibvirt.NewTypedParamValueLlong(30766094039)},
				{Field: "perf.cpu_clock", Value: *golibvirt.NewTypedParamValueLlong(25166642695)},
				{Field: "perf.task_clock", Value: *golibvirt.NewTypedParamValueLlong(25263578917)},
				{Field: "perf.page_faults", Value: *golibvirt.NewTypedParamValueLlong(2670)},
				{Field: "perf.context_switches", Value: *golibvirt.NewTypedParamValueLlong(294284)},
				{Field: "perf.cpu_migrations", Value: *golibvirt.NewTypedParamValueLlong(17949)},
				{Field: "perf.page_faults_min", Value: *golibvirt.NewTypedParamValueLlong(2670)},
				{Field: "perf.page_faults_maj", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "perf.alignment_faults", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "perf.emulation_faults", Value: *golibvirt.NewTypedParamValueLlong(0)},
			},
		},
	}

	interfaceStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "net.count", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "net.0.name", Value: *golibvirt.NewTypedParamValueString("vnet0")},
				{Field: "net.0.rx.bytes", Value: *golibvirt.NewTypedParamValueLlong(110)},
				{Field: "net.0.rx.pkts", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "net.0.rx.errs", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "net.0.rx.drop", Value: *golibvirt.NewTypedParamValueLlong(31007)},
				{Field: "net.0.tx.bytes", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "net.0.tx.pkts", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "net.0.tx.errs", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "net.0.tx.drop", Value: *golibvirt.NewTypedParamValueLlong(0)},
			},
		},
	}

	blockStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "block.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "block.0.name", Value: *golibvirt.NewTypedParamValueString("vda")},
				{Field: "block.0.backingIndex", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "block.0.path", Value: *golibvirt.NewTypedParamValueString("/tmp/ubuntu_image.img")},
				{Field: "block.0.rd.reqs", Value: *golibvirt.NewTypedParamValueLlong(11354)},
				{Field: "block.0.rd.bytes", Value: *golibvirt.NewTypedParamValueLlong(330314752)},
				{Field: "block.0.rd.times", Value: *golibvirt.NewTypedParamValueLlong(6240559566)},
				{Field: "block.0.wr.reqs", Value: *golibvirt.NewTypedParamValueLlong(52440)},
				{Field: "block.0.wr.bytes", Value: *golibvirt.NewTypedParamValueLlong(1183828480)},
				{Field: "block.0.wr.times", Value: *golibvirt.NewTypedParamValueLlong(21887150375)},
				{Field: "block.0.fl.reqs", Value: *golibvirt.NewTypedParamValueLlong(32250)},
				{Field: "block.0.fl.times", Value: *golibvirt.NewTypedParamValueLlong(23158998353)},
				{Field: "block.0.errors", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "block.0.allocation", Value: *golibvirt.NewTypedParamValueLlong(770048000)},
				{Field: "block.0.capacity", Value: *golibvirt.NewTypedParamValueLlong(2361393152)},
				{Field: "block.0.physical", Value: *golibvirt.NewTypedParamValueLlong(770052096)},
				{Field: "block.0.threshold", Value: *golibvirt.NewTypedParamValueLlong(2147483648)},
				{Field: "block.1.name", Value: *golibvirt.NewTypedParamValueString("vda1")},
				{Field: "block.1.backingIndex", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "block.1.path", Value: *golibvirt.NewTypedParamValueString("/tmp/ubuntu_image1.img")},
				{Field: "block.1.rd.reqs", Value: *golibvirt.NewTypedParamValueLlong(11354)},
				{Field: "block.1.rd.bytes", Value: *golibvirt.NewTypedParamValueLlong(330314752)},
				{Field: "block.1.rd.times", Value: *golibvirt.NewTypedParamValueLlong(6240559566)},
				{Field: "block.1.wr.reqs", Value: *golibvirt.NewTypedParamValueLlong(52440)},
				{Field: "block.1.wr.bytes", Value: *golibvirt.NewTypedParamValueLlong(1183828480)},
				{Field: "block.1.wr.times", Value: *golibvirt.NewTypedParamValueLlong(21887150375)},
				{Field: "block.1.fl.reqs", Value: *golibvirt.NewTypedParamValueLlong(32250)},
				{Field: "block.1.fl.times", Value: *golibvirt.NewTypedParamValueLlong(23158998353)},
				{Field: "block.1.errors", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "block.1.allocation", Value: *golibvirt.NewTypedParamValueLlong(770048000)},
				{Field: "block.1.capacity", Value: *golibvirt.NewTypedParamValueLlong(2361393152)},
				{Field: "block.1.physical", Value: *golibvirt.NewTypedParamValueLlong(770052096)},
				{Field: "block.1.threshold", Value: *golibvirt.NewTypedParamValueLlong(2147483648)},
			},
		},
	}

	iothreadStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "iothread.count", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "iothread.0.poll-max-ns", Value: *golibvirt.NewTypedParamValueLlong(32768)},
				{Field: "iothread.0.poll-grow", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "iothread.0.poll-shrink", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "iothread.1.poll-max-ns", Value: *golibvirt.NewTypedParamValueLlong(32769)},
				{Field: "iothread.1.poll-grow", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "iothread.1.poll-shrink", Value: *golibvirt.NewTypedParamValueLlong(0)},
			},
		},
	}

	dirtyrateStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "dirtyrate.calc_status", Value: *golibvirt.NewTypedParamValueLlong(2)},
				{Field: "dirtyrate.calc_start_time", Value: *golibvirt.NewTypedParamValueLlong(348414)},
				{Field: "dirtyrate.calc_period", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "dirtyrate.megabytes_per_second", Value: *golibvirt.NewTypedParamValueLlong(4)},
				{Field: "dirtyrate.calc_mode", Value: *golibvirt.NewTypedParamValueString("dirty-ring")},
				{Field: "dirtyrate.vcpu.0.megabytes_per_second", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "dirtyrate.vcpu.1.megabytes_per_second", Value: *golibvirt.NewTypedParamValueLlong(2)},
			},
		},
	}

	vcpuStats = []golibvirt.DomainStatsRecord{
		{
			Dom: domains[0],
			Params: []golibvirt.TypedParam{
				{Field: "vcpu.current", Value: *golibvirt.NewTypedParamValueLlong(3)},
				{Field: "vcpu.maximum", Value: *golibvirt.NewTypedParamValueLlong(3)},
				{Field: "vcpu.0.state", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "vcpu.0.time", Value: *golibvirt.NewTypedParamValueLlong(17943740000000)},
				{Field: "vcpu.0.wait", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "vcpu.0.halted", Value: *golibvirt.NewTypedParamValueString("no")},
				{Field: "vcpu.0.delay", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "vcpu.1.state", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "vcpu.1.time", Value: *golibvirt.NewTypedParamValueLlong(17943740000000)},
				{Field: "vcpu.1.wait", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "vcpu.1.halted", Value: *golibvirt.NewTypedParamValueString("yes")},
				{Field: "vcpu.1.delay", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "vcpu.2.state", Value: *golibvirt.NewTypedParamValueLlong(1)},
				{Field: "vcpu.2.time", Value: *golibvirt.NewTypedParamValueLlong(17943740000000)},
				{Field: "vcpu.2.wait", Value: *golibvirt.NewTypedParamValueLlong(0)},
				{Field: "vcpu.2.delay", Value: *golibvirt.NewTypedParamValueLlong(0)},
			},
		},
	}

	vcpusMapping = []vcpuAffinity{
		{"0", "0,1,2,3", 0},
		{"1", "1,2,3,4", 1},
	}

	expectedMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_state",
			map[string]string{"domain_name": "Droplet-844329"},
			map[string]interface{}{
				"reason": 2,
				"state":  1,
			},
			time.Now()),
		testutil.MustMetric("libvirt_state",
			map[string]string{"domain_name": "Droplet-33436"},
			map[string]interface{}{
				"reason": 1,
				"state":  1,
			},
			time.Now()),
	}

	expectedMemoryMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_memory_bandwidth_monitor_total",
			map[string]string{"domain_name": "Droplet-844329"},
			map[string]interface{}{
				"count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_memory_bandwidth_monitor",
			map[string]string{"domain_name": "Droplet-844329", "memory_bandwidth_monitor_id": "0"},
			map[string]interface{}{
				"name":       "any_name_vcpus_0-4",
				"vcpus":      "0-4",
				"node_count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_memory_bandwidth_monitor",
			map[string]string{"domain_name": "Droplet-844329", "memory_bandwidth_monitor_id": "1"},
			map[string]interface{}{
				"name":       "vcpus_7",
				"vcpus":      "7",
				"node_count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_memory_bandwidth_monitor_node",
			map[string]string{"domain_name": "Droplet-844329", "memory_bandwidth_monitor_id": "0", "controller_index": "0"},
			map[string]interface{}{
				"id":          0,
				"bytes_total": int64(10208067584),
				"bytes_local": int64(4807114752),
			},
			time.Now()),
		testutil.MustMetric("libvirt_memory_bandwidth_monitor_node",
			map[string]string{"domain_name": "Droplet-844329", "memory_bandwidth_monitor_id": "0", "controller_index": "1"},
			map[string]interface{}{
				"id":          1,
				"bytes_total": int64(8693735424),
				"bytes_local": int64(5850161152),
			},
			time.Now()),
		testutil.MustMetric("libvirt_memory_bandwidth_monitor_node",
			map[string]string{"domain_name": "Droplet-844329", "memory_bandwidth_monitor_id": "1", "controller_index": "0"},
			map[string]interface{}{
				"id":          0,
				"bytes_total": 853811200,
				"bytes_local": 290701312,
			},
			time.Now()),
		testutil.MustMetric("libvirt_memory_bandwidth_monitor_node",
			map[string]string{"domain_name": "Droplet-844329", "memory_bandwidth_monitor_id": "1", "controller_index": "1"},
			map[string]interface{}{
				"id":          1,
				"bytes_total": 406044672,
				"bytes_local": 229425152,
			},
			time.Now()),
	}

	expectedCPUMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_cpu",
			map[string]string{"domain_name": "Droplet-844329"},
			map[string]interface{}{
				"time":                  int64(67419144867000),
				"user":                  int64(63886161852000),
				"system":                int64(3532983015000),
				"haltpoll_success_time": 516907915,
				"haltpoll_fail_time":    int64(2727253643),
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_cache_monitor_total",
			map[string]string{"domain_name": "Droplet-844329"},
			map[string]interface{}{
				"count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_cache_monitor",
			map[string]string{"domain_name": "Droplet-844329", "cache_monitor_id": "0"},
			map[string]interface{}{
				"name":       "any_name_vcpus_0-3",
				"vcpus":      "0-3",
				"bank_count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_cache_monitor",
			map[string]string{"domain_name": "Droplet-844329", "cache_monitor_id": "1"},
			map[string]interface{}{
				"name":       "vcpus_4-9",
				"vcpus":      "4-9",
				"bank_count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_cache_monitor_bank",
			map[string]string{"domain_name": "Droplet-844329", "cache_monitor_id": "0", "bank_index": "0"},
			map[string]interface{}{
				"id":    0,
				"bytes": 5406720,
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_cache_monitor_bank",
			map[string]string{"domain_name": "Droplet-844329", "cache_monitor_id": "0", "bank_index": "1"},
			map[string]interface{}{
				"id":    1,
				"bytes": 0,
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_cache_monitor_bank",
			map[string]string{"domain_name": "Droplet-844329", "cache_monitor_id": "1", "bank_index": "0"},
			map[string]interface{}{
				"id":    0,
				"bytes": 720896,
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_cache_monitor_bank",
			map[string]string{"domain_name": "Droplet-844329", "cache_monitor_id": "1", "bank_index": "1"},
			map[string]interface{}{
				"id":    1,
				"bytes": 8200192,
			},
			time.Now()),
	}

	expectedVcpuAffinityMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_cpu_affinity",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "0"},
			map[string]interface{}{
				"cpu_id": "0,1,2,3",
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_affinity",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "1"},
			map[string]interface{}{
				"cpu_id": "1,2,3,4",
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_affinity",
			map[string]string{
				"domain_name": "Droplet-33436",
				"vcpu_id":     "0"},
			map[string]interface{}{
				"cpu_id": "0,1,2,3",
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_affinity",
			map[string]string{
				"domain_name": "Droplet-33436",
				"vcpu_id":     "1"},
			map[string]interface{}{
				"cpu_id": "1,2,3,4",
			},
			time.Now()),
	}

	expectedBalloonMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_balloon",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"current":         4194304,
				"maximum":         4194304,
				"swap_in":         0,
				"swap_out":        0,
				"major_fault":     0,
				"minor_fault":     0,
				"unused":          3928628,
				"available":       4018480,
				"rss":             1036012,
				"usable":          3808724,
				"last_update":     1654611373,
				"disk_caches":     68820,
				"hugetlb_pgalloc": 0,
				"hugetlb_pgfail":  0,
			},
			time.Now()),
	}

	expectedPerfMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_perf",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"cmt":                     19087360,
				"mbmt":                    77168640,
				"mbml":                    67788800,
				"cpu_cycles":              int64(29858995122),
				"instructions":            0,
				"cache_references":        int64(3053301695),
				"cache_misses":            609441024,
				"branch_instructions":     int64(2623890194),
				"branch_misses":           103707961,
				"bus_cycles":              188105628,
				"stalled_cycles_frontend": 0,
				"stalled_cycles_backend":  0,
				"ref_cpu_cycles":          int64(30766094039),
				"cpu_clock":               int64(25166642695),
				"task_clock":              int64(25263578917),
				"page_faults":             2670,
				"context_switches":        294284,
				"cpu_migrations":          17949,
				"page_faults_min":         2670,
				"page_faults_maj":         0,
				"alignment_faults":        0,
				"emulation_faults":        0,
			},
			time.Now()),
	}

	expectedInterfaceMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_net_total",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"count": 1,
			},
			time.Now()),
		testutil.MustMetric("libvirt_net",
			map[string]string{
				"domain_name":  "Droplet-844329",
				"interface_id": "0",
			},
			map[string]interface{}{
				"name":     "vnet0",
				"rx_bytes": 110,
				"rx_pkts":  1,
				"rx_errs":  0,
				"rx_drop":  31007,
				"tx_bytes": 0,
				"tx_pkts":  0,
				"tx_errs":  0,
				"tx_drop":  0,
			},
			time.Now()),
	}

	expectedBlockMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_block_total",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_block",
			map[string]string{
				"domain_name": "Droplet-844329",
				"block_id":    "0",
			},
			map[string]interface{}{
				"name":         "vda",
				"backingIndex": 1,
				"path":         "/tmp/ubuntu_image.img",
				"rd_reqs":      11354,
				"rd_bytes":     330314752,
				"rd_times":     int64(6240559566),
				"wr_reqs":      52440,
				"wr_bytes":     1183828480,
				"wr_times":     int64(21887150375),
				"fl_reqs":      32250,
				"fl_times":     int64(23158998353),
				"errors":       0,
				"allocation":   770048000,
				"capacity":     int64(2361393152),
				"physical":     770052096,
				"threshold":    int64(2147483648),
			},
			time.Now()),
		testutil.MustMetric("libvirt_block",
			map[string]string{
				"domain_name": "Droplet-844329",
				"block_id":    "1",
			},
			map[string]interface{}{
				"name":         "vda1",
				"backingIndex": 1,
				"path":         "/tmp/ubuntu_image1.img",
				"rd_reqs":      11354,
				"rd_bytes":     330314752,
				"rd_times":     int64(6240559566),
				"wr_reqs":      52440,
				"wr_bytes":     1183828480,
				"wr_times":     int64(21887150375),
				"fl_reqs":      32250,
				"fl_times":     int64(23158998353),
				"errors":       0,
				"allocation":   770048000,
				"capacity":     int64(2361393152),
				"physical":     770052096,
				"threshold":    int64(2147483648),
			},
			time.Now()),
	}

	expectedIOThreadMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_iothread_total",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"count": 2,
			},
			time.Now()),
		testutil.MustMetric("libvirt_iothread",
			map[string]string{
				"domain_name": "Droplet-844329",
				"iothread_id": "0",
			},
			map[string]interface{}{
				"poll_max_ns": 32768,
				"poll_grow":   0,
				"poll_shrink": 0,
			},
			time.Now()),
		testutil.MustMetric("libvirt_iothread",
			map[string]string{
				"domain_name": "Droplet-844329",
				"iothread_id": "1",
			},
			map[string]interface{}{
				"poll_max_ns": 32769,
				"poll_grow":   0,
				"poll_shrink": 0,
			},
			time.Now()),
	}

	expectedDirtyrateMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_dirtyrate",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"calc_status":          2,
				"calc_start_time":      348414,
				"calc_period":          1,
				"megabytes_per_second": 4,
				"calc_mode":            "dirty-ring",
			},
			time.Now()),
		testutil.MustMetric("libvirt_dirtyrate_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "0",
			},
			map[string]interface{}{
				"megabytes_per_second": 1,
			},
			time.Now()),
		testutil.MustMetric("libvirt_dirtyrate_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "1",
			},
			map[string]interface{}{
				"megabytes_per_second": 2,
			},
			time.Now()),
	}

	expectedVCPUMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_vcpu_total",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"current": 3,
				"maximum": 3,
			},
			time.Now()),
		testutil.MustMetric("libvirt_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "0",
			},
			map[string]interface{}{
				"state":    1,
				"time":     int64(17943740000000),
				"wait":     0,
				"halted":   "no",
				"halted_i": 0,
				"delay":    0,
			},
			time.Now()),
		testutil.MustMetric("libvirt_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "1",
			},
			map[string]interface{}{
				"state":    1,
				"time":     int64(17943740000000),
				"wait":     0,
				"halted":   "yes",
				"halted_i": 1,
				"delay":    0,
			},
			time.Now()),
		testutil.MustMetric("libvirt_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "2",
			},
			map[string]interface{}{
				"state": 1,
				"time":  int64(17943740000000),
				"wait":  0,
				"delay": 0,
			},
			time.Now()),
	}

	expectedExtendedVCPUMetrics = []telegraf.Metric{
		testutil.MustMetric("libvirt_cpu_affinity",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "0"},
			map[string]interface{}{
				"cpu_id": "0,1,2,3",
			},
			time.Now()),
		testutil.MustMetric("libvirt_cpu_affinity",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "1"},
			map[string]interface{}{
				"cpu_id": "1,2,3,4",
			},
			time.Now()),
		testutil.MustMetric("libvirt_vcpu_total",
			map[string]string{
				"domain_name": "Droplet-844329",
			},
			map[string]interface{}{
				"current": 3,
				"maximum": 3,
			},
			time.Now()),
		testutil.MustMetric("libvirt_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "0",
			},
			map[string]interface{}{
				"state":    1,
				"time":     int64(17943740000000),
				"wait":     0,
				"halted":   "no",
				"halted_i": 0,
				"delay":    0,
				"cpu_id":   0,
			},
			time.Now()),
		testutil.MustMetric("libvirt_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "1",
			},
			map[string]interface{}{
				"state":    1,
				"time":     int64(17943740000000),
				"wait":     0,
				"halted":   "yes",
				"halted_i": 1,
				"delay":    0,
				"cpu_id":   1,
			},
			time.Now()),
		testutil.MustMetric("libvirt_vcpu",
			map[string]string{
				"domain_name": "Droplet-844329",
				"vcpu_id":     "2",
			},
			map[string]interface{}{
				"state": 1,
				"time":  int64(17943740000000),
				"wait":  0,
				"delay": 0,
			},
			time.Now()),
	}
)
