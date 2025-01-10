package kube_inventory

import (
	"context"
	"strings"

	"github.com/influxdata/telegraf"
	corev1 "k8s.io/api/core/v1"
)

func collectEndpoints(ctx context.Context, acc telegraf.Accumulator, ki *KubernetesInventory) {
	list, err := ki.client.getEndpoints(ctx)
	if err != nil {
		acc.AddError(err)
		return
	}
	for _, i := range list.Items {
		gatherEndpoint(i, acc)
	}
}

func gatherEndpoint(e corev1.Endpoints, acc telegraf.Accumulator) {
	creationTs := e.GetCreationTimestamp()
	if creationTs.IsZero() {
		return
	}

	fields := map[string]interface{}{
		"created":    e.GetCreationTimestamp().UnixNano(),
		"generation": e.Generation,
	}

	tags := map[string]string{
		"endpoint_name": e.Name,
		"namespace":     e.Namespace,
	}

	for _, endpoint := range e.Subsets {
		for _, readyAddr := range endpoint.Addresses {
			fields["ready"] = true

			tags["hostname"] = readyAddr.Hostname
			if readyAddr.NodeName != nil {
				tags["node_name"] = *readyAddr.NodeName
			}
			if readyAddr.TargetRef != nil {
				tags[strings.ToLower(readyAddr.TargetRef.Kind)] = readyAddr.TargetRef.Name
			}

			for _, port := range endpoint.Ports {
				fields["port"] = port.Port

				tags["port_name"] = port.Name
				tags["port_protocol"] = string(port.Protocol)

				acc.AddFields(endpointMeasurement, fields, tags)
			}
		}
		for _, notReadyAddr := range endpoint.NotReadyAddresses {
			fields["ready"] = false

			tags["hostname"] = notReadyAddr.Hostname
			if notReadyAddr.NodeName != nil {
				tags["node_name"] = *notReadyAddr.NodeName
			}
			if notReadyAddr.TargetRef != nil {
				tags[strings.ToLower(notReadyAddr.TargetRef.Kind)] = notReadyAddr.TargetRef.Name
			}

			for _, port := range endpoint.Ports {
				fields["port"] = port.Port

				tags["port_name"] = port.Name
				tags["port_protocol"] = string(port.Protocol)

				acc.AddFields(endpointMeasurement, fields, tags)
			}
		}
	}
}
