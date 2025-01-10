package kube_inventory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestNode(t *testing.T) {
	cli := &client{}
	now := time.Now()

	tests := []struct {
		name     string
		handler  *mockHandler
		output   []telegraf.Metric
		hasError bool
	}{
		{
			name: "no nodes",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/nodes/": corev1.NodeList{},
				},
			},
			output: []telegraf.Metric{
				testutil.MustMetric(
					nodeMeasurement,
					map[string]string{},
					map[string]interface{}{
						"node_count": int64(0),
					},
					time.Unix(0, 0),
				),
			},
			hasError: false,
		},
		{
			name: "collect nodes",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/nodes/": corev1.NodeList{
						Items: []corev1.Node{
							{
								Status: corev1.NodeStatus{
									NodeInfo: corev1.NodeSystemInfo{
										KernelVersion:           "4.14.48-coreos-r2",
										OSImage:                 "Container Linux by CoreOS 1745.7.0 (Rhyolite)",
										ContainerRuntimeVersion: "docker://18.3.1",
										KubeletVersion:          "v1.10.3",
										KubeProxyVersion:        "v1.10.3",
									},
									Phase: "Running",
									Capacity: corev1.ResourceList{
										"cpu":                     resource.MustParse("16"),
										"ephemeral_storage_bytes": resource.MustParse("49536401408"),
										"hugepages_1Gi_bytes":     resource.MustParse("0"),
										"hugepages_2Mi_bytes":     resource.MustParse("0"),
										"memory":                  resource.MustParse("125817904Ki"),
										"pods":                    resource.MustParse("110"),
									},
									Allocatable: corev1.ResourceList{
										"cpu":                     resource.MustParse("1000m"),
										"ephemeral_storage_bytes": resource.MustParse("44582761194"),
										"hugepages_1Gi_bytes":     resource.MustParse("0"),
										"hugepages_2Mi_bytes":     resource.MustParse("0"),
										"memory":                  resource.MustParse("125715504Ki"),
										"pods":                    resource.MustParse("110"),
									},
									Conditions: []corev1.NodeCondition{
										{Type: "Ready", Status: "True", LastTransitionTime: metav1.Time{Time: now}},
									},
								},
								Spec: corev1.NodeSpec{
									ProviderID: "aws:///us-east-1c/i-0c00",
									Taints: []corev1.Taint{
										{
											Key:    "k1",
											Value:  "v1",
											Effect: "NoExecute",
										},
										{
											Key:    "k2",
											Value:  "v2",
											Effect: "NoSchedule",
										},
									},
								},
								ObjectMeta: metav1.ObjectMeta{
									Generation: 11232,
									Name:       "node1",
									Labels: map[string]string{
										"lab1": "v1",
										"lab2": "v2",
									},
									Namespace: "ns1",
									Annotations: map[string]string{
										"cluster.x-k8s.io/cluster-namespace": "ns1",
									},
									CreationTimestamp: metav1.Time{Time: now},
								},
							},
						},
					},
				},
			},
			output: []telegraf.Metric{
				testutil.MustMetric(
					nodeMeasurement,
					map[string]string{
						"node_name":         "node1",
						"cluster_namespace": "ns1",
						"condition":         "Ready",
						"status":            "True",
						"version":           "v1.10.3",
					},
					map[string]interface{}{
						"status_condition": int64(1),
						"ready":            int64(1),
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric(
					nodeMeasurement,
					map[string]string{
						"node_name":         "node1",
						"cluster_namespace": "ns1",
						"version":           "v1.10.3",
					},
					map[string]interface{}{
						"capacity_cpu_cores":         int64(16),
						"capacity_millicpu_cores":    int64(16000),
						"capacity_memory_bytes":      int64(1.28837533696e+11),
						"capacity_pods":              int64(110),
						"allocatable_cpu_cores":      int64(1),
						"allocatable_millicpu_cores": int64(1000),
						"allocatable_memory_bytes":   int64(1.28732676096e+11),
						"allocatable_pods":           int64(110),
						"spec_unschedulable":         int64(0),
					},
					time.Unix(0, 0),
				),
			},
			hasError: false,
		},
	}

	for _, v := range tests {
		ks := &KubernetesInventory{
			client: cli,
		}
		acc := new(testutil.Accumulator)
		items := ((v.handler.responseMap["/nodes/"]).(corev1.NodeList)).Items
		for i := range items {
			ks.gatherNode(&items[i], acc)
		}

		err := acc.FirstError()
		if v.hasError {
			require.Errorf(t, err, "%s failed, should have error", v.name)
			continue
		}

		// No error case
		require.NoErrorf(t, err, "%s failed, err: %v", v.name, err)

		if v.name == "no nodes" {
			nodeCount := len((v.handler.responseMap["/nodes/"]).(corev1.NodeList).Items)
			gatherNodeCount(nodeCount, acc)
		}
		require.Len(t, acc.Metrics, len(v.output))
		testutil.RequireMetricsEqual(t, acc.GetTelegrafMetrics(), v.output, testutil.IgnoreTime())
	}
}
