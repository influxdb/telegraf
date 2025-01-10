package docker

import (
	"context"
	"crypto/tls"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/testutil"
)

type mockClient struct {
	InfoF             func() (system.Info, error)
	ContainerListF    func(options container.ListOptions) ([]types.Container, error)
	ContainerStatsF   func(containerID string) (container.StatsResponseReader, error)
	ContainerInspectF func() (types.ContainerJSON, error)
	ServiceListF      func() ([]swarm.Service, error)
	TaskListF         func() ([]swarm.Task, error)
	NodeListF         func() ([]swarm.Node, error)
	DiskUsageF        func() (types.DiskUsage, error)
	ClientVersionF    func() string
	CloseF            func() error
}

func (c *mockClient) Info(context.Context) (system.Info, error) {
	return c.InfoF()
}

func (c *mockClient) ContainerList(_ context.Context, options container.ListOptions) ([]types.Container, error) {
	return c.ContainerListF(options)
}

func (c *mockClient) ContainerStats(_ context.Context, containerID string, _ bool) (container.StatsResponseReader, error) {
	return c.ContainerStatsF(containerID)
}

func (c *mockClient) ContainerInspect(context.Context, string) (types.ContainerJSON, error) {
	return c.ContainerInspectF()
}

func (c *mockClient) ServiceList(context.Context, types.ServiceListOptions) ([]swarm.Service, error) {
	return c.ServiceListF()
}

func (c *mockClient) TaskList(context.Context, types.TaskListOptions) ([]swarm.Task, error) {
	return c.TaskListF()
}

func (c *mockClient) NodeList(context.Context, types.NodeListOptions) ([]swarm.Node, error) {
	return c.NodeListF()
}

func (c *mockClient) DiskUsage(context.Context, types.DiskUsageOptions) (types.DiskUsage, error) {
	return c.DiskUsageF()
}

func (c *mockClient) ClientVersion() string {
	return c.ClientVersionF()
}

func (c *mockClient) Close() error {
	return c.CloseF()
}

var baseClient = mockClient{
	InfoF: func() (system.Info, error) {
		return info, nil
	},
	ContainerListF: func(container.ListOptions) ([]types.Container, error) {
		return containerList, nil
	},
	ContainerStatsF: func(s string) (container.StatsResponseReader, error) {
		return containerStats(s), nil
	},
	ContainerInspectF: func() (types.ContainerJSON, error) {
		return containerInspect(), nil
	},
	ServiceListF: func() ([]swarm.Service, error) {
		return serviceList, nil
	},
	TaskListF: func() ([]swarm.Task, error) {
		return taskList, nil
	},
	NodeListF: func() ([]swarm.Node, error) {
		return nodeList, nil
	},
	DiskUsageF: func() (types.DiskUsage, error) {
		return diskUsage, nil
	},
	ClientVersionF: func() string {
		return version
	},
	CloseF: func() error {
		return nil
	},
}

func TestDockerGatherContainerStats(t *testing.T) {
	var acc testutil.Accumulator
	stats := testStats()

	tags := map[string]string{
		"container_name":  "redis",
		"container_image": "redis/image",
	}

	d := &Docker{
		Log:              testutil.Logger{},
		PerDeviceInclude: containerMetricClasses,
		TotalInclude:     containerMetricClasses,
	}
	d.parseContainerStats(stats, &acc, tags, "123456789", "linux")

	// test docker_container_net measurement
	netfields := map[string]interface{}{
		"rx_dropped":   uint64(1),
		"rx_bytes":     uint64(2),
		"rx_errors":    uint64(3),
		"tx_packets":   uint64(4),
		"tx_dropped":   uint64(1),
		"rx_packets":   uint64(2),
		"tx_errors":    uint64(3),
		"tx_bytes":     uint64(4),
		"container_id": "123456789",
	}
	nettags := copyTags(tags)
	nettags["network"] = "eth0"
	acc.AssertContainsTaggedFields(t, "docker_container_net", netfields, nettags)

	netfields = map[string]interface{}{
		"rx_dropped":   uint64(6),
		"rx_bytes":     uint64(8),
		"rx_errors":    uint64(10),
		"tx_packets":   uint64(12),
		"tx_dropped":   uint64(6),
		"rx_packets":   uint64(8),
		"tx_errors":    uint64(10),
		"tx_bytes":     uint64(12),
		"container_id": "123456789",
	}
	nettags = copyTags(tags)
	nettags["network"] = "total"
	acc.AssertContainsTaggedFields(t, "docker_container_net", netfields, nettags)

	// test docker_blkio measurement
	blkiotags := copyTags(tags)
	blkiotags["device"] = "6:0"
	blkiofields := map[string]interface{}{
		"io_service_bytes_recursive_read": uint64(100),
		"io_serviced_recursive_write":     uint64(101),
		"container_id":                    "123456789",
	}
	acc.AssertContainsTaggedFields(t, "docker_container_blkio", blkiofields, blkiotags)

	blkiotags = copyTags(tags)
	blkiotags["device"] = "total"
	blkiofields = map[string]interface{}{
		"io_service_bytes_recursive_read": uint64(100),
		"io_serviced_recursive_write":     uint64(302),
		"container_id":                    "123456789",
	}
	acc.AssertContainsTaggedFields(t, "docker_container_blkio", blkiofields, blkiotags)

	// test docker_container_mem measurement
	memfields := map[string]interface{}{
		"active_anon":               uint64(0),
		"active_file":               uint64(1),
		"cache":                     uint64(0),
		"container_id":              "123456789",
		"fail_count":                uint64(1),
		"hierarchical_memory_limit": uint64(0),
		"inactive_anon":             uint64(0),
		"inactive_file":             uint64(3),
		"limit":                     uint64(2000),
		"mapped_file":               uint64(0),
		"max_usage":                 uint64(1001),
		"pgfault":                   uint64(2),
		"pgmajfault":                uint64(0),
		"pgpgin":                    uint64(0),
		"pgpgout":                   uint64(0),
		"rss_huge":                  uint64(0),
		"rss":                       uint64(0),
		"total_active_anon":         uint64(0),
		"total_active_file":         uint64(0),
		"total_cache":               uint64(0),
		"total_inactive_anon":       uint64(0),
		"total_inactive_file":       uint64(0),
		"total_mapped_file":         uint64(0),
		"total_pgfault":             uint64(0),
		"total_pgmajfault":          uint64(0),
		"total_pgpgin":              uint64(4),
		"total_pgpgout":             uint64(0),
		"total_rss_huge":            uint64(444),
		"total_rss":                 uint64(44),
		"total_unevictable":         uint64(0),
		"total_writeback":           uint64(55),
		"unevictable":               uint64(0),
		"usage_percent":             float64(55.55),
		"usage":                     uint64(1111),
		"writeback":                 uint64(0),
	}

	acc.AssertContainsTaggedFields(t, "docker_container_mem", memfields, tags)

	// test docker_container_cpu measurement
	cputags := copyTags(tags)
	cputags["cpu"] = "cpu-total"
	cpufields := map[string]interface{}{
		"usage_total":                  uint64(500),
		"usage_in_usermode":            uint64(100),
		"usage_in_kernelmode":          uint64(200),
		"usage_system":                 uint64(100),
		"throttling_periods":           uint64(1),
		"throttling_throttled_periods": uint64(0),
		"throttling_throttled_time":    uint64(0),
		"usage_percent":                float64(400.0),
		"container_id":                 "123456789",
	}
	acc.AssertContainsTaggedFields(t, "docker_container_cpu", cpufields, cputags)

	cputags["cpu"] = "cpu0"
	cpu0fields := map[string]interface{}{
		"usage_total":  uint64(1),
		"container_id": "123456789",
	}
	acc.AssertContainsTaggedFields(t, "docker_container_cpu", cpu0fields, cputags)

	cputags["cpu"] = "cpu1"
	cpu1fields := map[string]interface{}{
		"usage_total":  uint64(1002),
		"container_id": "123456789",
	}
	acc.AssertContainsTaggedFields(t, "docker_container_cpu", cpu1fields, cputags)

	// Those tagged filed should not be present because of offline CPUs
	cputags["cpu"] = "cpu2"
	cpu2fields := map[string]interface{}{
		"usage_total":  uint64(0),
		"container_id": "123456789",
	}
	acc.AssertDoesNotContainsTaggedFields(t, "docker_container_cpu", cpu2fields, cputags)

	cputags["cpu"] = "cpu3"
	cpu3fields := map[string]interface{}{
		"usage_total":  uint64(0),
		"container_id": "123456789",
	}
	acc.AssertDoesNotContainsTaggedFields(t, "docker_container_cpu", cpu3fields, cputags)
}

func TestDockerMemoryExcludesCache(t *testing.T) {
	var acc testutil.Accumulator
	stats := testStats()

	tags := map[string]string{
		"container_name":  "redis",
		"container_image": "redis/image",
	}

	d := &Docker{
		Log: testutil.Logger{},
	}

	delete(stats.MemoryStats.Stats, "cache")
	delete(stats.MemoryStats.Stats, "inactive_file")
	delete(stats.MemoryStats.Stats, "total_inactive_file")

	// set cgroup v2 cache value
	stats.MemoryStats.Stats["inactive_file"] = 9

	d.parseContainerStats(stats, &acc, tags, "123456789", "linux")

	// test docker_container_mem measurement
	memfields := map[string]interface{}{
		"active_anon":               uint64(0),
		"active_file":               uint64(1),
		"container_id":              "123456789",
		"fail_count":                uint64(1),
		"hierarchical_memory_limit": uint64(0),
		"inactive_anon":             uint64(0),
		"inactive_file":             uint64(9),
		"limit":                     uint64(2000),
		"mapped_file":               uint64(0),
		"max_usage":                 uint64(1001),
		"pgfault":                   uint64(2),
		"pgmajfault":                uint64(0),
		"pgpgin":                    uint64(0),
		"pgpgout":                   uint64(0),
		"rss_huge":                  uint64(0),
		"rss":                       uint64(0),
		"total_active_anon":         uint64(0),
		"total_active_file":         uint64(0),
		"total_cache":               uint64(0),
		"total_inactive_anon":       uint64(0),
		"total_mapped_file":         uint64(0),
		"total_pgfault":             uint64(0),
		"total_pgmajfault":          uint64(0),
		"total_pgpgin":              uint64(4),
		"total_pgpgout":             uint64(0),
		"total_rss_huge":            uint64(444),
		"total_rss":                 uint64(44),
		"total_unevictable":         uint64(0),
		"total_writeback":           uint64(55),
		"unevictable":               uint64(0),
		"usage_percent":             float64(55.1), // 1102 / 2000
		"usage":                     uint64(1102),
		"writeback":                 uint64(0),
	}

	acc.AssertContainsTaggedFields(t, "docker_container_mem", memfields, tags)
	acc.ClearMetrics()

	// set cgroup v1 cache value (has priority over cgroups v2)
	stats.MemoryStats.Stats["total_inactive_file"] = 7

	d.parseContainerStats(stats, &acc, tags, "123456789", "linux")

	// test docker_container_mem measurement
	memfields = map[string]interface{}{
		"active_anon": uint64(0),
		"active_file": uint64(1),
		// "cache":                     uint64(0),
		"container_id":              "123456789",
		"fail_count":                uint64(1),
		"hierarchical_memory_limit": uint64(0),
		"inactive_anon":             uint64(0),
		"inactive_file":             uint64(9),
		"limit":                     uint64(2000),
		"mapped_file":               uint64(0),
		"max_usage":                 uint64(1001),
		"pgfault":                   uint64(2),
		"pgmajfault":                uint64(0),
		"pgpgin":                    uint64(0),
		"pgpgout":                   uint64(0),
		"rss_huge":                  uint64(0),
		"rss":                       uint64(0),
		"total_active_anon":         uint64(0),
		"total_active_file":         uint64(0),
		"total_cache":               uint64(0),
		"total_inactive_anon":       uint64(0),
		"total_inactive_file":       uint64(7),
		"total_mapped_file":         uint64(0),
		"total_pgfault":             uint64(0),
		"total_pgmajfault":          uint64(0),
		"total_pgpgin":              uint64(4),
		"total_pgpgout":             uint64(0),
		"total_rss_huge":            uint64(444),
		"total_rss":                 uint64(44),
		"total_unevictable":         uint64(0),
		"total_writeback":           uint64(55),
		"unevictable":               uint64(0),
		"usage_percent":             float64(55.2), // 1104 / 2000
		"usage":                     uint64(1104),
		"writeback":                 uint64(0),
	}

	acc.AssertContainsTaggedFields(t, "docker_container_mem", memfields, tags)
	acc.ClearMetrics()

	// set Docker 19.03 and older cache value (has priority over cgroups v1 and v2)
	stats.MemoryStats.Stats["cache"] = 16

	d.parseContainerStats(stats, &acc, tags, "123456789", "linux")

	// test docker_container_mem measurement
	memfields = map[string]interface{}{
		"active_anon":               uint64(0),
		"active_file":               uint64(1),
		"cache":                     uint64(16),
		"container_id":              "123456789",
		"fail_count":                uint64(1),
		"hierarchical_memory_limit": uint64(0),
		"inactive_anon":             uint64(0),
		"inactive_file":             uint64(9),
		"limit":                     uint64(2000),
		"mapped_file":               uint64(0),
		"max_usage":                 uint64(1001),
		"pgfault":                   uint64(2),
		"pgmajfault":                uint64(0),
		"pgpgin":                    uint64(0),
		"pgpgout":                   uint64(0),
		"rss_huge":                  uint64(0),
		"rss":                       uint64(0),
		"total_active_anon":         uint64(0),
		"total_active_file":         uint64(0),
		"total_cache":               uint64(0),
		"total_inactive_anon":       uint64(0),
		"total_inactive_file":       uint64(7),
		"total_mapped_file":         uint64(0),
		"total_pgfault":             uint64(0),
		"total_pgmajfault":          uint64(0),
		"total_pgpgin":              uint64(4),
		"total_pgpgout":             uint64(0),
		"total_rss_huge":            uint64(444),
		"total_rss":                 uint64(44),
		"total_unevictable":         uint64(0),
		"total_writeback":           uint64(55),
		"unevictable":               uint64(0),
		"usage_percent":             float64(54.75), // 1095 / 2000
		"usage":                     uint64(1095),
		"writeback":                 uint64(0),
	}

	acc.AssertContainsTaggedFields(t, "docker_container_mem", memfields, tags)
}

func TestDocker_WindowsMemoryContainerStats(t *testing.T) {
	var acc testutil.Accumulator

	d := Docker{
		Log: testutil.Logger{},
		newClient: func(string, *tls.Config) (dockerClient, error) {
			return &mockClient{
				InfoF: func() (system.Info, error) {
					return info, nil
				},
				ContainerListF: func(container.ListOptions) ([]types.Container, error) {
					return containerList, nil
				},
				ContainerStatsF: func(string) (container.StatsResponseReader, error) {
					return containerStatsWindows(), nil
				},
				ContainerInspectF: func() (types.ContainerJSON, error) {
					return containerInspect(), nil
				},
				ServiceListF: func() ([]swarm.Service, error) {
					return serviceList, nil
				},
				TaskListF: func() ([]swarm.Task, error) {
					return taskList, nil
				},
				NodeListF: func() ([]swarm.Node, error) {
					return nodeList, nil
				},
				DiskUsageF: func() (types.DiskUsage, error) {
					return diskUsage, nil
				},
				ClientVersionF: func() string {
					return version
				},
				CloseF: func() error {
					return nil
				},
			}, nil
		},
	}
	err := d.Gather(&acc)
	require.NoError(t, err)
}

func TestContainerLabels(t *testing.T) {
	var tests = []struct {
		name      string
		container types.Container
		include   []string
		exclude   []string
		expected  map[string]string
	}{
		{
			name: "Nil filters matches all",
			container: genContainerLabeled(map[string]string{
				"a": "x",
			}),
			include: nil,
			exclude: nil,
			expected: map[string]string{
				"a": "x",
			},
		},
		{
			name: "Empty filters matches all",
			container: genContainerLabeled(map[string]string{
				"a": "x",
			}),
			expected: map[string]string{
				"a": "x",
			},
		},
		{
			name: "Must match include",
			container: genContainerLabeled(map[string]string{
				"a": "x",
				"b": "y",
			}),
			include: []string{"a"},
			expected: map[string]string{
				"a": "x",
			},
		},
		{
			name: "Must not match exclude",
			container: genContainerLabeled(map[string]string{
				"a": "x",
				"b": "y",
			}),
			exclude: []string{"b"},
			expected: map[string]string{
				"a": "x",
			},
		},
		{
			name: "Include Glob",
			container: genContainerLabeled(map[string]string{
				"aa": "x",
				"ab": "y",
				"bb": "z",
			}),
			include: []string{"a*"},
			expected: map[string]string{
				"aa": "x",
				"ab": "y",
			},
		},
		{
			name: "Exclude Glob",
			container: genContainerLabeled(map[string]string{
				"aa": "x",
				"ab": "y",
				"bb": "z",
			}),
			exclude: []string{"a*"},
			expected: map[string]string{
				"bb": "z",
			},
		},
		{
			name: "Excluded Includes",
			container: genContainerLabeled(map[string]string{
				"aa": "x",
				"ab": "y",
				"bb": "z",
			}),
			include: []string{"a*"},
			exclude: []string{"*b"},
			expected: map[string]string{
				"aa": "x",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator

			newClientFunc := func(string, *tls.Config) (dockerClient, error) {
				client := baseClient
				client.ContainerListF = func(container.ListOptions) ([]types.Container, error) {
					return []types.Container{tt.container}, nil
				}
				return &client, nil
			}

			d := Docker{
				Log:          testutil.Logger{},
				newClient:    newClientFunc,
				LabelInclude: tt.include,
				LabelExclude: tt.exclude,
				Total:        true,
				TotalInclude: []string{"cpu"},
			}

			err := d.Gather(&acc)
			require.NoError(t, err)

			// Grab tags from a container metric
			var actual map[string]string
			for _, metric := range acc.Metrics {
				if metric.Measurement == "docker_container_cpu" {
					actual = metric.Tags
				}
			}

			for k, v := range tt.expected {
				require.Equal(t, v, actual[k])
			}
		})
	}
}

func genContainerLabeled(labels map[string]string) types.Container {
	c := containerList[0]
	c.Labels = labels
	return c
}

func TestContainerNames(t *testing.T) {
	var tests = []struct {
		name       string
		containers [][]string
		include    []string
		exclude    []string
		expected   []string
	}{
		{
			name:     "Nil filters matches all",
			include:  nil,
			exclude:  nil,
			expected: []string{"etcd", "etcd2", "acme", "acme-test", "foo"},
		},
		{
			name:     "Empty filters matches all",
			expected: []string{"etcd", "etcd2", "acme", "acme-test", "foo"},
		},
		{
			name:     "Match all containers",
			include:  []string{"*"},
			expected: []string{"etcd", "etcd2", "acme", "acme-test", "foo"},
		},
		{
			name:     "Include prefix match",
			include:  []string{"etc*"},
			expected: []string{"etcd", "etcd2"},
		},
		{
			name:     "Exact match",
			include:  []string{"etcd"},
			expected: []string{"etcd"},
		},
		{
			name:     "Star matches zero length",
			include:  []string{"etcd2*"},
			expected: []string{"etcd2"},
		},
		{
			name:     "Exclude matches all",
			exclude:  []string{"etc*"},
			expected: []string{"acme", "acme-test", "foo"},
		},
		{
			name:     "Exclude single",
			exclude:  []string{"etcd"},
			expected: []string{"etcd2", "acme", "acme-test", "foo"},
		},
		{
			name:    "Exclude all",
			include: []string{"*"},
			exclude: []string{"*"},
		},
		{
			name:     "Exclude item matching include",
			include:  []string{"acme*"},
			exclude:  []string{"*test*"},
			expected: []string{"acme"},
		},
		{
			name:     "Exclude item no wildcards",
			include:  []string{"acme*"},
			exclude:  []string{"test"},
			expected: []string{"acme", "acme-test"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator

			newClientFunc := func(string, *tls.Config) (dockerClient, error) {
				client := baseClient
				client.ContainerListF = func(container.ListOptions) ([]types.Container, error) {
					return containerList, nil
				}
				client.ContainerStatsF = func(s string) (container.StatsResponseReader, error) {
					return containerStats(s), nil
				}

				return &client, nil
			}

			d := Docker{
				Log:              testutil.Logger{},
				newClient:        newClientFunc,
				ContainerInclude: tt.include,
				ContainerExclude: tt.exclude,
			}

			err := d.Gather(&acc)
			require.NoError(t, err)

			// Set of expected names
			var expected = make(map[string]bool)
			for _, v := range tt.expected {
				expected[v] = true
			}

			// Set of actual names
			var actual = make(map[string]bool)
			for _, metric := range acc.Metrics {
				if name, ok := metric.Tags["container_name"]; ok {
					actual[name] = true
				}
			}

			require.Equal(t, expected, actual)
		})
	}
}

func filterMetrics(metrics []telegraf.Metric, f func(telegraf.Metric) bool) []telegraf.Metric {
	results := make([]telegraf.Metric, 0, len(metrics))
	for _, m := range metrics {
		if f(m) {
			results = append(results, m)
		}
	}
	return results
}

func TestContainerStatus(t *testing.T) {
	var tests = []struct {
		name     string
		now      func() time.Time
		inspect  types.ContainerJSON
		expected []telegraf.Metric
	}{
		{
			name: "finished_at is zero value",
			now: func() time.Time {
				return time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC)
			},
			inspect: containerInspect(),
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"docker_container_status",
					map[string]string{
						"container_name":    "etcd",
						"container_image":   "quay.io/coreos/etcd",
						"container_version": "v3.3.25",
						"engine_host":       "absol",
						"label1":            "test_value_1",
						"label2":            "test_value_2",
						"server_version":    "17.09.0-ce",
						"container_status":  "running",
						"source":            "e2173b9478a6",
					},
					map[string]interface{}{
						"oomkilled":     false,
						"pid":           1234,
						"restart_count": 0,
						"exitcode":      0,
						"container_id":  "e2173b9478a6ae55e237d4d74f8bbb753f0817192b5081334dc78476296b7dfb",
						"started_at":    time.Date(2018, 6, 14, 5, 48, 53, 266176036, time.UTC).UnixNano(),
						"uptime_ns":     int64(3 * time.Minute),
					},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			name: "finished_at is non-zero value",
			now: func() time.Time {
				return time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC)
			},
			inspect: func() types.ContainerJSON {
				i := containerInspect()
				i.ContainerJSONBase.State.FinishedAt = "2018-06-14T05:53:53.266176036Z"
				return i
			}(),
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"docker_container_status",
					map[string]string{
						"container_name":    "etcd",
						"container_image":   "quay.io/coreos/etcd",
						"container_version": "v3.3.25",
						"engine_host":       "absol",
						"label1":            "test_value_1",
						"label2":            "test_value_2",
						"server_version":    "17.09.0-ce",
						"container_status":  "running",
						"source":            "e2173b9478a6",
					},
					map[string]interface{}{
						"oomkilled":     false,
						"pid":           1234,
						"exitcode":      0,
						"restart_count": 0,
						"container_id":  "e2173b9478a6ae55e237d4d74f8bbb753f0817192b5081334dc78476296b7dfb",
						"started_at":    time.Date(2018, 6, 14, 5, 48, 53, 266176036, time.UTC).UnixNano(),
						"finished_at":   time.Date(2018, 6, 14, 5, 53, 53, 266176036, time.UTC).UnixNano(),
						"uptime_ns":     int64(5 * time.Minute),
					},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			name: "started_at is zero value",
			now: func() time.Time {
				return time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC)
			},
			inspect: func() types.ContainerJSON {
				i := containerInspect()
				i.ContainerJSONBase.State.StartedAt = ""
				i.ContainerJSONBase.State.FinishedAt = "2018-06-14T05:53:53.266176036Z"
				return i
			}(),
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"docker_container_status",
					map[string]string{
						"container_name":    "etcd",
						"container_image":   "quay.io/coreos/etcd",
						"container_version": "v3.3.25",
						"engine_host":       "absol",
						"label1":            "test_value_1",
						"label2":            "test_value_2",
						"server_version":    "17.09.0-ce",
						"container_status":  "running",
						"source":            "e2173b9478a6",
					},
					map[string]interface{}{
						"oomkilled":     false,
						"pid":           1234,
						"exitcode":      0,
						"restart_count": 0,
						"container_id":  "e2173b9478a6ae55e237d4d74f8bbb753f0817192b5081334dc78476296b7dfb",
						"finished_at":   time.Date(2018, 6, 14, 5, 53, 53, 266176036, time.UTC).UnixNano(),
					},
					time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC),
				),
			},
		},
		{
			name: "container has been restarted",
			now: func() time.Time {
				return time.Date(2019, 1, 1, 0, 0, 3, 0, time.UTC)
			},
			inspect: func() types.ContainerJSON {
				i := containerInspect()
				i.ContainerJSONBase.State.StartedAt = "2019-01-01T00:00:02Z"
				i.ContainerJSONBase.State.FinishedAt = "2019-01-01T00:00:01Z"
				return i
			}(),
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"docker_container_status",
					map[string]string{
						"container_name":    "etcd",
						"container_image":   "quay.io/coreos/etcd",
						"container_version": "v3.3.25",
						"engine_host":       "absol",
						"label1":            "test_value_1",
						"label2":            "test_value_2",
						"server_version":    "17.09.0-ce",
						"container_status":  "running",
						"source":            "e2173b9478a6",
					},
					map[string]interface{}{
						"oomkilled":     false,
						"pid":           1234,
						"exitcode":      0,
						"restart_count": 0,
						"container_id":  "e2173b9478a6ae55e237d4d74f8bbb753f0817192b5081334dc78476296b7dfb",
						"started_at":    time.Date(2019, 1, 1, 0, 0, 2, 0, time.UTC).UnixNano(),
						"finished_at":   time.Date(2019, 1, 1, 0, 0, 1, 0, time.UTC).UnixNano(),
						"uptime_ns":     int64(1 * time.Second),
					},
					time.Date(2019, 1, 1, 0, 0, 3, 0, time.UTC),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				acc           testutil.Accumulator
				newClientFunc = func(string, *tls.Config) (dockerClient, error) {
					client := baseClient
					client.ContainerListF = func(container.ListOptions) ([]types.Container, error) {
						return containerList[:1], nil
					}
					client.ContainerInspectF = func() (types.ContainerJSON, error) {
						return tt.inspect, nil
					}

					return &client, nil
				}
				d = Docker{
					Log:              testutil.Logger{},
					newClient:        newClientFunc,
					IncludeSourceTag: true,
				}
			)

			// mock time
			if tt.now != nil {
				now = tt.now
			}
			defer func() {
				now = time.Now
			}()

			err := d.Gather(&acc)
			require.NoError(t, err)

			actual := filterMetrics(acc.GetTelegrafMetrics(), func(m telegraf.Metric) bool {
				return m.Name() == "docker_container_status"
			})
			testutil.RequireMetricsEqual(t, tt.expected, actual)
		})
	}
}

func TestDockerGatherInfo(t *testing.T) {
	var acc testutil.Accumulator
	d := Docker{
		Log:       testutil.Logger{},
		newClient: func(string, *tls.Config) (dockerClient, error) { return &baseClient, nil },
		TagEnvironment: []string{"ENVVAR1", "ENVVAR2", "ENVVAR3", "ENVVAR5",
			"ENVVAR6", "ENVVAR7", "ENVVAR8", "ENVVAR9"},
		PerDeviceInclude: []string{"cpu", "network", "blkio"},
		Total:            true,
		TotalInclude:     []string{""},
	}

	err := acc.GatherError(d.Gather)
	require.NoError(t, err)

	acc.AssertContainsTaggedFields(t,
		"docker",
		map[string]interface{}{
			"n_listener_events":       int(0),
			"n_cpus":                  int(4),
			"n_used_file_descriptors": int(19),
			"n_containers":            int(108),
			"n_containers_running":    int(98),
			"n_containers_stopped":    int(6),
			"n_containers_paused":     int(3),
			"n_images":                int(199),
			"n_goroutines":            int(39),
		},
		map[string]string{
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker",
		map[string]interface{}{
			"memory_total": int64(3840757760),
		},
		map[string]string{
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker",
		map[string]interface{}{
			"pool_blocksize": int64(65540),
		},
		map[string]string{
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
			"unit":           "bytes",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_data",
		map[string]interface{}{
			"used":      int64(17300000000),
			"total":     int64(107400000000),
			"available": int64(36530000000),
		},
		map[string]string{
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
			"unit":           "bytes",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_metadata",
		map[string]interface{}{
			"used":      int64(20970000),
			"total":     int64(2146999999),
			"available": int64(2126999999),
		},
		map[string]string{
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
			"unit":           "bytes",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_devicemapper",
		map[string]interface{}{
			"base_device_size_bytes":             int64(10740000000),
			"pool_blocksize_bytes":               int64(65540),
			"data_space_used_bytes":              int64(17300000000),
			"data_space_total_bytes":             int64(107400000000),
			"data_space_available_bytes":         int64(36530000000),
			"metadata_space_used_bytes":          int64(20970000),
			"metadata_space_total_bytes":         int64(2146999999),
			"metadata_space_available_bytes":     int64(2126999999),
			"thin_pool_minimum_free_space_bytes": int64(10740000000),
		},
		map[string]string{
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
			"pool_name":      "docker-8:1-1182287-pool",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_container_cpu",
		map[string]interface{}{
			"usage_total":  uint64(1231652),
			"container_id": "b7dfbb9478a6ae55e237d4d74f8bbb753f0817192b5081334dc78476296e2173",
		},
		map[string]string{
			"container_name":    "etcd2",
			"container_image":   "quay.io:4443/coreos/etcd",
			"cpu":               "cpu3",
			"container_version": "v3.3.25",
			"engine_host":       "absol",
			"ENVVAR1":           "loremipsum",
			"ENVVAR2":           "dolorsitamet",
			"ENVVAR3":           "=ubuntu:10.04",
			"ENVVAR7":           "ENVVAR8=ENVVAR9",
			"label1":            "test_value_1",
			"label2":            "test_value_2",
			"server_version":    "17.09.0-ce",
			"container_status":  "running",
		},
	)
	acc.AssertContainsTaggedFields(t,
		"docker_container_mem",
		map[string]interface{}{
			"container_id":  "b7dfbb9478a6ae55e237d4d74f8bbb753f0817192b5081334dc78476296e2173",
			"limit":         uint64(18935443456),
			"max_usage":     uint64(0),
			"usage":         uint64(0),
			"usage_percent": float64(0),
		},
		map[string]string{
			"engine_host":       "absol",
			"container_name":    "etcd2",
			"container_image":   "quay.io:4443/coreos/etcd",
			"container_version": "v3.3.25",
			"ENVVAR1":           "loremipsum",
			"ENVVAR2":           "dolorsitamet",
			"ENVVAR3":           "=ubuntu:10.04",
			"ENVVAR7":           "ENVVAR8=ENVVAR9",
			"label1":            "test_value_1",
			"label2":            "test_value_2",
			"server_version":    "17.09.0-ce",
			"container_status":  "running",
		},
	)
}

func TestDockerGatherSwarmInfo(t *testing.T) {
	var acc testutil.Accumulator
	d := Docker{
		Log:       testutil.Logger{},
		newClient: func(string, *tls.Config) (dockerClient, error) { return &baseClient, nil },
	}

	err := acc.GatherError(d.Gather)
	require.NoError(t, err)

	require.NoError(t, d.gatherSwarmInfo(&acc))

	// test docker_container_net measurement
	acc.AssertContainsTaggedFields(t,
		"docker_swarm",
		map[string]interface{}{
			"tasks_running": int(2),
			"tasks_desired": uint64(2),
		},
		map[string]string{
			"service_id":   "qolkls9g5iasdiuihcyz9rnx2",
			"service_name": "test1",
			"service_mode": "replicated",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_swarm",
		map[string]interface{}{
			"tasks_running": int(1),
			"tasks_desired": uint64(1),
		},
		map[string]string{
			"service_id":   "qolkls9g5iasdiuihcyz9rn3",
			"service_name": "test2",
			"service_mode": "global",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_swarm",
		map[string]interface{}{
			"tasks_running":     int(0),
			"max_concurrent":    uint64(2),
			"total_completions": uint64(2),
		},
		map[string]string{
			"service_id":   "rfmqydhe8cluzl9hayyrhw5ga",
			"service_name": "test3",
			"service_mode": "replicated_job",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_swarm",
		map[string]interface{}{
			"tasks_running": int(0),
		},
		map[string]string{
			"service_id":   "mp50lo68vqgkory4e26ts8f9d",
			"service_name": "test4",
			"service_mode": "global_job",
		},
	)
}

func TestContainerStateFilter(t *testing.T) {
	var tests = []struct {
		name     string
		include  []string
		exclude  []string
		expected map[string][]string
	}{
		{
			name: "default",
			expected: map[string][]string{
				"status": {"running"},
			},
		},
		{
			name:    "include running",
			include: []string{"running"},
			expected: map[string][]string{
				"status": {"running"},
			},
		},
		{
			name:    "include glob",
			include: []string{"r*"},
			expected: map[string][]string{
				"status": {"restarting", "running", "removing"},
			},
		},
		{
			name:    "include all",
			include: []string{"*"},
			expected: map[string][]string{
				"status": {"created", "restarting", "running", "removing", "paused", "exited", "dead"},
			},
		},
		{
			name:    "exclude all",
			exclude: []string{"*"},
			expected: map[string][]string{
				"status": {},
			},
		},
		{
			name:    "exclude all",
			include: []string{"*"},
			exclude: []string{"exited"},
			expected: map[string][]string{
				"status": {"created", "restarting", "running", "removing", "paused", "dead"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator

			newClientFunc := func(string, *tls.Config) (dockerClient, error) {
				client := baseClient
				client.ContainerListF = func(options container.ListOptions) ([]types.Container, error) {
					for k, v := range tt.expected {
						actual := options.Filters.Get(k)
						sort.Strings(actual)
						sort.Strings(v)
						require.Equal(t, v, actual)
					}

					return nil, nil
				}
				return &client, nil
			}

			d := Docker{
				Log:                   testutil.Logger{},
				newClient:             newClientFunc,
				ContainerStateInclude: tt.include,
				ContainerStateExclude: tt.exclude,
			}

			err := d.Gather(&acc)
			require.NoError(t, err)
		})
	}
}

func TestContainerName(t *testing.T) {
	tests := []struct {
		name       string
		clientFunc func(host string, tlsConfig *tls.Config) (dockerClient, error)
		expected   string
	}{
		{
			name: "container stats name is preferred",
			clientFunc: func(string, *tls.Config) (dockerClient, error) {
				client := baseClient
				client.ContainerListF = func(container.ListOptions) ([]types.Container, error) {
					var containers []types.Container
					containers = append(containers, types.Container{
						Names: []string{"/logspout/foo"},
					})
					return containers, nil
				}
				client.ContainerStatsF = func(string) (container.StatsResponseReader, error) {
					return container.StatsResponseReader{
						Body: io.NopCloser(strings.NewReader(`{"name": "logspout"}`)),
					}, nil
				}
				return &client, nil
			},
			expected: "logspout",
		},
		{
			name: "container stats without name uses container list name",
			clientFunc: func(string, *tls.Config) (dockerClient, error) {
				client := baseClient
				client.ContainerListF = func(container.ListOptions) ([]types.Container, error) {
					var containers []types.Container
					containers = append(containers, types.Container{
						Names: []string{"/logspout"},
					})
					return containers, nil
				}
				client.ContainerStatsF = func(string) (container.StatsResponseReader, error) {
					return container.StatsResponseReader{
						Body: io.NopCloser(strings.NewReader(`{}`)),
					}, nil
				}
				return &client, nil
			},
			expected: "logspout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Docker{
				Log:       testutil.Logger{},
				newClient: tt.clientFunc,
			}
			var acc testutil.Accumulator
			err := d.Gather(&acc)
			require.NoError(t, err)

			for _, metric := range acc.Metrics {
				// This tag is set on all container measurements
				if metric.Measurement == "docker_container_mem" {
					require.Equal(t, tt.expected, metric.Tags["container_name"])
				}
			}
		})
	}
}

func TestHostnameFromID(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		expect string
	}{
		{
			name:   "Real ID",
			id:     "565e3a55f5843cfdd4aa5659a1a75e4e78d47f73c3c483f782fe4a26fc8caa07",
			expect: "565e3a55f584",
		},
		{
			name:   "Short ID",
			id:     "shortid123",
			expect: "shortid123",
		},
		{
			name:   "No ID",
			id:     "",
			expect: "shortid123",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := hostnameFromID(test.id)
			if test.expect != output {
				t.Logf("Container ID for hostname is wrong. Want: %s, Got: %s", output, test.expect)
			}
		})
	}
}

func Test_parseContainerStatsPerDeviceAndTotal(t *testing.T) {
	type args struct {
		stat             *container.StatsResponse
		tags             map[string]string
		id               string
		perDeviceInclude []string
		totalInclude     []string
		daemonOSType     string
	}

	var (
		testDate       = time.Date(2018, 6, 14, 5, 51, 53, 266176036, time.UTC)
		metricCPUTotal = testutil.MustMetric(
			"docker_container_cpu",
			map[string]string{
				"cpu": "cpu-total",
			},
			map[string]interface{}{},
			testDate)

		metricCPU0 = testutil.MustMetric(
			"docker_container_cpu",
			map[string]string{
				"cpu": "cpu0",
			},
			map[string]interface{}{},
			testDate)
		metricCPU1 = testutil.MustMetric(
			"docker_container_cpu",
			map[string]string{
				"cpu": "cpu1",
			},
			map[string]interface{}{},
			testDate)

		metricNetworkTotal = testutil.MustMetric(
			"docker_container_net",
			map[string]string{
				"network": "total",
			},
			map[string]interface{}{},
			testDate)

		metricNetworkEth0 = testutil.MustMetric(
			"docker_container_net",
			map[string]string{
				"network": "eth0",
			},
			map[string]interface{}{},
			testDate)

		metricNetworkEth1 = testutil.MustMetric(
			"docker_container_net",
			map[string]string{
				"network": "eth0",
			},
			map[string]interface{}{},
			testDate)
		metricBlkioTotal = testutil.MustMetric(
			"docker_container_blkio",
			map[string]string{
				"device": "total",
			},
			map[string]interface{}{},
			testDate)
		metricBlkio6_0 = testutil.MustMetric(
			"docker_container_blkio",
			map[string]string{
				"device": "6:0",
			},
			map[string]interface{}{},
			testDate)
		metricBlkio6_1 = testutil.MustMetric(
			"docker_container_blkio",
			map[string]string{
				"device": "6:1",
			},
			map[string]interface{}{},
			testDate)
	)
	stats := testStats()
	tests := []struct {
		name     string
		args     args
		expected []telegraf.Metric
	}{
		{
			name: "Per device and total metrics enabled",
			args: args{
				stat:             stats,
				perDeviceInclude: containerMetricClasses,
				totalInclude:     containerMetricClasses,
			},
			expected: []telegraf.Metric{
				metricCPUTotal, metricCPU0, metricCPU1,
				metricNetworkTotal, metricNetworkEth0, metricNetworkEth1,
				metricBlkioTotal, metricBlkio6_0, metricBlkio6_1,
			},
		},
		{
			name: "Per device metrics enabled",
			args: args{
				stat:             stats,
				perDeviceInclude: containerMetricClasses,
			},
			expected: []telegraf.Metric{
				metricCPU0, metricCPU1,
				metricNetworkEth0, metricNetworkEth1,
				metricBlkio6_0, metricBlkio6_1,
			},
		},
		{
			name: "Total metrics enabled",
			args: args{
				stat:         stats,
				totalInclude: containerMetricClasses,
			},
			expected: []telegraf.Metric{metricCPUTotal, metricNetworkTotal, metricBlkioTotal},
		},
		{
			name: "Per device and total metrics disabled",
			args: args{
				stat: stats,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			d := &Docker{
				Log:              testutil.Logger{},
				PerDeviceInclude: tt.args.perDeviceInclude,
				TotalInclude:     tt.args.totalInclude,
			}
			d.parseContainerStats(tt.args.stat, &acc, tt.args.tags, tt.args.id, tt.args.daemonOSType)

			actual := filterMetrics(acc.GetTelegrafMetrics(), func(m telegraf.Metric) bool {
				return choice.Contains(m.Name(),
					[]string{"docker_container_cpu", "docker_container_net", "docker_container_blkio"})
			})
			testutil.RequireMetricsEqual(t, tt.expected, actual, testutil.OnlyTags(), testutil.SortMetrics())
		})
	}
}

func TestDocker_Init(t *testing.T) {
	type fields struct {
		PerDevice        bool
		PerDeviceInclude []string
		Total            bool
		TotalInclude     []string
	}
	tests := []struct {
		name                 string
		fields               fields
		wantErr              bool
		wantPerDeviceInclude []string
		wantTotalInclude     []string
	}{
		{
			name: "Unsupported perdevice_include setting",
			fields: fields{
				PerDevice:        false,
				PerDeviceInclude: []string{"nonExistentClass"},
				Total:            false,
				TotalInclude:     []string{"cpu"},
			},
			wantErr: true,
		},
		{
			name: "Unsupported total_include setting",
			fields: fields{
				PerDevice:        false,
				PerDeviceInclude: []string{"cpu"},
				Total:            false,
				TotalInclude:     []string{"nonExistentClass"},
			},
			wantErr: true,
		},
		{
			name: "PerDevice true adds network and blkio",
			fields: fields{
				PerDevice:        true,
				PerDeviceInclude: []string{"cpu"},
				Total:            true,
				TotalInclude:     []string{"cpu"},
			},
			wantPerDeviceInclude: []string{"cpu", "network", "blkio"},
			wantTotalInclude:     []string{"cpu"},
		},
		{
			name: "Total false removes network and blkio",
			fields: fields{
				PerDevice:        false,
				PerDeviceInclude: []string{"cpu"},
				Total:            false,
				TotalInclude:     []string{"cpu", "network", "blkio"},
			},
			wantPerDeviceInclude: []string{"cpu"},
			wantTotalInclude:     []string{"cpu"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Docker{
				Log:              testutil.Logger{},
				PerDevice:        tt.fields.PerDevice,
				PerDeviceInclude: tt.fields.PerDeviceInclude,
				Total:            tt.fields.Total,
				TotalInclude:     tt.fields.TotalInclude,
			}
			err := d.Init()
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil {
				if !reflect.DeepEqual(d.PerDeviceInclude, tt.wantPerDeviceInclude) {
					t.Errorf("Perdevice include: got  '%v', want '%v'", d.PerDeviceInclude, tt.wantPerDeviceInclude)
				}

				if !reflect.DeepEqual(d.TotalInclude, tt.wantTotalInclude) {
					t.Errorf("Total include: got  '%v', want '%v'", d.TotalInclude, tt.wantTotalInclude)
				}
			}
		})
	}
}

func TestDockerGatherDiskUsage(t *testing.T) {
	var acc testutil.Accumulator
	d := Docker{
		Log:       testutil.Logger{},
		newClient: func(string, *tls.Config) (dockerClient, error) { return &baseClient, nil },
	}

	require.NoError(t, acc.GatherError(d.Gather))

	d.gatherDiskUsage(&acc, types.DiskUsageOptions{})

	acc.AssertContainsTaggedFields(t,
		"docker_disk_usage",
		map[string]interface{}{
			"layers_size": int64(1e10),
		},
		map[string]string{
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_disk_usage",
		map[string]interface{}{
			"size_root_fs": int64(123456789),
			"size_rw":      int64(0)},
		map[string]string{
			"container_image":   "some_image",
			"container_version": "1.0.0-alpine",
			"engine_host":       "absol",
			"server_version":    "17.09.0-ce",
			"container_name":    "some_container",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_disk_usage",
		map[string]interface{}{
			"size":        int64(123456789),
			"shared_size": int64(0)},
		map[string]string{
			"image_id":       "some_imageid",
			"image_name":     "some_image_tag",
			"image_version":  "1.0.0-alpine",
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_disk_usage",
		map[string]interface{}{
			"size":        int64(425484494),
			"shared_size": int64(0)},
		map[string]string{
			"image_id":       "7f4a1cc74046",
			"image_name":     "telegraf",
			"image_version":  "latest",
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
		},
	)

	acc.AssertContainsTaggedFields(t,
		"docker_disk_usage",
		map[string]interface{}{
			"size": int64(123456789),
		},
		map[string]string{
			"volume_name":    "some_volume",
			"engine_host":    "absol",
			"server_version": "17.09.0-ce",
		},
	)
}
