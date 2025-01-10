package dcos

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

type mockClient struct {
	SetTokenF            func()
	LoginF               func(ctx context.Context, sa *serviceAccount) (*authToken, error)
	GetSummaryF          func() (*summary, error)
	GetContainersF       func() ([]container, error)
	GetNodeMetricsF      func() (*metrics, error)
	GetContainerMetricsF func(ctx context.Context, node, container string) (*metrics, error)
	GetAppMetricsF       func(ctx context.Context, node, container string) (*metrics, error)
}

func (c *mockClient) setToken(string) {
	c.SetTokenF()
}

func (c *mockClient) login(ctx context.Context, sa *serviceAccount) (*authToken, error) {
	return c.LoginF(ctx, sa)
}

func (c *mockClient) getSummary(context.Context) (*summary, error) {
	return c.GetSummaryF()
}

func (c *mockClient) getContainers(context.Context, string) ([]container, error) {
	return c.GetContainersF()
}

func (c *mockClient) getNodeMetrics(context.Context, string) (*metrics, error) {
	return c.GetNodeMetricsF()
}

func (c *mockClient) getContainerMetrics(ctx context.Context, node, container string) (*metrics, error) {
	return c.GetContainerMetricsF(ctx, node, container)
}

func (c *mockClient) getAppMetrics(ctx context.Context, node, container string) (*metrics, error) {
	return c.GetAppMetricsF(ctx, node, container)
}

func TestAddNodeMetrics(t *testing.T) {
	var tests = []struct {
		name    string
		metrics *metrics
		check   func(*testutil.Accumulator) []bool
	}{
		{
			name: "basic datapoint conversion",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name:  "process.count",
						Unit:  "count",
						Value: 42.0,
					},
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{acc.HasPoint(
					"dcos_node",
					map[string]string{
						"cluster": "a",
					},
					"process_count", 42.0,
				)}
			},
		},
		{
			name: "path added as tag",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name: "filesystem.inode.free",
						Tags: map[string]string{
							"path": "/var/lib",
						},
						Unit:  "count",
						Value: 42.0,
					},
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{acc.HasPoint(
					"dcos_node",
					map[string]string{
						"cluster": "a",
						"path":    "/var/lib",
					},
					"filesystem_inode_free", 42.0,
				)}
			},
		},
		{
			name: "interface added as tag",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name: "network.out.dropped",
						Tags: map[string]string{
							"interface": "eth0",
						},
						Unit:  "count",
						Value: 42.0,
					},
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{acc.HasPoint(
					"dcos_node",
					map[string]string{
						"cluster":   "a",
						"interface": "eth0",
					},
					"network_out_dropped", 42.0,
				)}
			},
		},
		{
			name: "bytes unit appended to fieldkey",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name: "network.in",
						Tags: map[string]string{
							"interface": "eth0",
						},
						Unit:  "bytes",
						Value: 42.0,
					},
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{acc.HasPoint(
					"dcos_node",
					map[string]string{
						"cluster":   "a",
						"interface": "eth0",
					},
					"network_in_bytes", int64(42),
				)}
			},
		},
		{
			name: "dimensions added as tags",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name:  "process.count",
						Tags:  map[string]string{},
						Unit:  "count",
						Value: 42.0,
					},
					{
						Name:  "memory.total",
						Tags:  map[string]string{},
						Unit:  "bytes",
						Value: 42,
					},
				},
				Dimensions: map[string]interface{}{
					"cluster_id": "c0760bbd-9e9d-434b-bd4a-39c7cdef8a63",
					"hostname":   "192.168.122.18",
					"mesos_id":   "2dfbbd28-29d2-411d-92c4-e2f84c38688e-S1",
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{
					acc.HasPoint(
						"dcos_node",
						map[string]string{
							"cluster":  "a",
							"hostname": "192.168.122.18",
						},
						"process_count", 42.0),
					acc.HasPoint(
						"dcos_node",
						map[string]string{
							"cluster":  "a",
							"hostname": "192.168.122.18",
						},
						"memory_total_bytes", int64(42)),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			addNodeMetrics(&acc, "a", tt.metrics)
			for i, ok := range tt.check(&acc) {
				require.Truef(t, ok, "Index was not true: %d", i)
			}
		})
	}
}

func TestAddContainerMetrics(t *testing.T) {
	var tests = []struct {
		name    string
		metrics *metrics
		check   func(*testutil.Accumulator) []bool
	}{
		{
			name: "container",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name: "net.rx.errors",
						Tags: map[string]string{
							"container_id":  "f25c457b-fceb-44f0-8f5b-38be34cbb6fb",
							"executor_id":   "telegraf.192fb45f-cc0c-11e7-af48-ea183c0b541a",
							"executor_name": "Command Executor (Task: telegraf.192fb45f-cc0c-11e7-af48-ea183c0b541a) (Command: NO EXECUTABLE)",
							"framework_id":  "ab2f3a8b-06db-4e8c-95b6-fb1940874a30-0001",
							"source":        "telegraf.192fb45f-cc0c-11e7-af48-ea183c0b541a",
						},
						Unit:  "count",
						Value: 42.0,
					},
				},
				Dimensions: map[string]interface{}{
					"cluster_id":          "c0760bbd-9e9d-434b-bd4a-39c7cdef8a63",
					"container_id":        "f25c457b-fceb-44f0-8f5b-38be34cbb6fb",
					"executor_id":         "telegraf.192fb45f-cc0c-11e7-af48-ea183c0b541a",
					"framework_id":        "ab2f3a8b-06db-4e8c-95b6-fb1940874a30-0001",
					"framework_name":      "marathon",
					"framework_principal": "dcos_marathon",
					"framework_role":      "slave_public",
					"hostname":            "192.168.122.18",
					"labels": map[string]string{
						"DCOS_SPACE": "/telegraf",
					},
					"mesos_id":  "2dfbbd28-29d2-411d-92c4-e2f84c38688e-S1",
					"task_id":   "telegraf.192fb45f-cc0c-11e7-af48-ea183c0b541a",
					"task_name": "telegraf",
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{
					acc.HasPoint(
						"dcos_container",
						map[string]string{
							"cluster":      "a",
							"container_id": "f25c457b-fceb-44f0-8f5b-38be34cbb6fb",
							"hostname":     "192.168.122.18",
							"task_name":    "telegraf",
							"DCOS_SPACE":   "/telegraf",
						},
						"net_rx_errors",
						42.0,
					),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			addContainerMetrics(&acc, "a", tt.metrics)
			for i, ok := range tt.check(&acc) {
				require.Truef(t, ok, "Index was not true: %d", i)
			}
		})
	}
}

func TestAddAppMetrics(t *testing.T) {
	var tests = []struct {
		name    string
		metrics *metrics
		check   func(*testutil.Accumulator) []bool
	}{
		{
			name: "tags are optional",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name:  "dcos.metrics.module.container_throttled_bytes_per_sec",
						Unit:  "",
						Value: 42.0,
					},
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{
					acc.HasPoint(
						"dcos_app",
						map[string]string{
							"cluster": "a",
						},
						"container_throttled_bytes_per_sec", 42.0,
					),
				}
			},
		},
		{
			name: "dimensions are tagged",
			metrics: &metrics{
				Datapoints: []dataPoint{
					{
						Name:  "dcos.metrics.module.container_throttled_bytes_per_sec",
						Unit:  "",
						Value: 42.0,
					},
				},
				Dimensions: map[string]interface{}{
					"cluster_id":   "c0760bbd-9e9d-434b-bd4a-39c7cdef8a63",
					"container_id": "02d31175-1c01-4459-8520-ef8b1339bc52",
					"hostname":     "192.168.122.18",
					"mesos_id":     "2dfbbd28-29d2-411d-92c4-e2f84c38688e-S1",
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{
					acc.HasPoint(
						"dcos_app",
						map[string]string{
							"cluster":      "a",
							"container_id": "02d31175-1c01-4459-8520-ef8b1339bc52",
							"hostname":     "192.168.122.18",
						},
						"container_throttled_bytes_per_sec", 42.0,
					),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			addAppMetrics(&acc, "a", tt.metrics)
			for i, ok := range tt.check(&acc) {
				require.Truef(t, ok, "Index was not true: %d", i)
			}
		})
	}
}

func TestGatherFilterNode(t *testing.T) {
	var tests = []struct {
		name        string
		nodeInclude []string
		nodeExclude []string
		client      client
		check       func(*testutil.Accumulator) []bool
	}{
		{
			name: "cluster without nodes has no metrics",
			client: &mockClient{
				SetTokenF: func() {},
				GetSummaryF: func() (*summary, error) {
					return &summary{
						Cluster: "a",
					}, nil
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{
					acc.NMetrics() == 0,
				}
			},
		},
		{
			name:        "node include",
			nodeInclude: []string{"x"},
			client: &mockClient{
				SetTokenF: func() {},
				GetSummaryF: func() (*summary, error) {
					return &summary{
						Cluster: "a",
						Slaves: []slave{
							{ID: "x"},
							{ID: "y"},
						},
					}, nil
				},
				GetContainersF: func() ([]container, error) {
					return nil, nil
				},
				GetNodeMetricsF: func() (*metrics, error) {
					return &metrics{
						Datapoints: []dataPoint{
							{
								Name:  "value",
								Value: 42.0,
							},
						},
						Dimensions: map[string]interface{}{
							"hostname": "x",
						},
					}, nil
				},
			},
			check: func(acc *testutil.Accumulator) []bool {
				return []bool{
					acc.HasPoint(
						"dcos_node",
						map[string]string{
							"cluster":  "a",
							"hostname": "x",
						},
						"value", 42.0,
					),
					acc.NMetrics() == 1,
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			dcos := &DCOS{
				NodeInclude: tt.nodeInclude,
				NodeExclude: tt.nodeExclude,
				client:      tt.client,
			}
			err := dcos.Gather(&acc)
			require.NoError(t, err)
			for i, ok := range tt.check(&acc) {
				require.Truef(t, ok, "Index was not true: %d", i)
			}
		})
	}
}
