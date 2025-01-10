package kube_inventory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestPersistentVolume(t *testing.T) {
	now := time.Now()
	now = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 1, 36, 0, now.Location())

	tests := []struct {
		name     string
		handler  *mockHandler
		output   []telegraf.Metric
		hasError bool
	}{
		{
			name: "no pv",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/persistentvolumes/": &corev1.PersistentVolumeList{},
				},
			},
			hasError: false,
		},
		{
			name: "collect pvs",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/persistentvolumes/": &corev1.PersistentVolumeList{
						Items: []corev1.PersistentVolume{
							{
								Status: corev1.PersistentVolumeStatus{
									Phase: "pending",
								},
								Spec: corev1.PersistentVolumeSpec{
									StorageClassName: "ebs-1",
								},
								ObjectMeta: metav1.ObjectMeta{
									Name: "pv1",
									Labels: map[string]string{
										"lab1": "v1",
										"lab2": "v2",
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
					"kubernetes_persistentvolume",
					map[string]string{
						"pv_name":      "pv1",
						"storageclass": "ebs-1",
						"phase":        "pending",
					},
					map[string]interface{}{
						"phase_type": 2,
					},
					time.Unix(0, 0),
				),
			},
			hasError: false,
		},
	}

	for _, v := range tests {
		acc := new(testutil.Accumulator)
		items := ((v.handler.responseMap["/persistentvolumes/"]).(*corev1.PersistentVolumeList)).Items
		for i := range items {
			gatherPersistentVolume(&items[i], acc)
		}

		err := acc.FirstError()
		if v.hasError {
			require.Errorf(t, err, "%s failed, should have error", v.name)
			continue
		}

		// No error case
		require.NoErrorf(t, err, "%s failed, err: %v", v.name, err)

		require.Len(t, acc.Metrics, len(v.output))
		testutil.RequireMetricsEqual(t, acc.GetTelegrafMetrics(), v.output, testutil.IgnoreTime())
	}
}
