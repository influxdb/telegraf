package kube_inventory

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/influxdata/telegraf"
)

func collectPersistentVolumes(ctx context.Context, acc telegraf.Accumulator, ki *KubernetesInventory) {
	list, err := ki.client.getPersistentVolumes(ctx)
	if err != nil {
		acc.AddError(err)
		return
	}
	for i := range list.Items {
		gatherPersistentVolume(&list.Items[i], acc)
	}
}

func gatherPersistentVolume(pv *corev1.PersistentVolume, acc telegraf.Accumulator) {
	phaseType := 5
	switch strings.ToLower(string(pv.Status.Phase)) {
	case "bound":
		phaseType = 0
	case "failed":
		phaseType = 1
	case "pending":
		phaseType = 2
	case "released":
		phaseType = 3
	case "available":
		phaseType = 4
	}
	fields := map[string]interface{}{
		"phase_type": phaseType,
	}
	tags := map[string]string{
		"pv_name":      pv.Name,
		"phase":        string(pv.Status.Phase),
		"storageclass": pv.Spec.StorageClassName,
	}

	acc.AddFields(persistentVolumeMeasurement, fields, tags)
}
