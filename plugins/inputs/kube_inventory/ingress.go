package kube_inventory

import (
	"context"

	netv1 "k8s.io/api/networking/v1"

	"github.com/influxdata/telegraf"
)

func collectIngress(ctx context.Context, acc telegraf.Accumulator, ki *KubernetesInventory) {
	list, err := ki.client.getIngress(ctx)
	if err != nil {
		acc.AddError(err)
		return
	}
	for _, i := range list.Items {
		gatherIngress(i, acc)
	}
}

func gatherIngress(i netv1.Ingress, acc telegraf.Accumulator) {
	creationTs := i.GetCreationTimestamp()
	if creationTs.IsZero() {
		return
	}

	fields := map[string]interface{}{
		"created":    i.GetCreationTimestamp().UnixNano(),
		"generation": i.Generation,
	}

	tags := map[string]string{
		"ingress_name": i.Name,
		"namespace":    i.Namespace,
	}

	for _, ingress := range i.Status.LoadBalancer.Ingress {
		tags["hostname"] = ingress.Hostname
		tags["ip"] = ingress.IP

		for _, rule := range i.Spec.Rules {
			if rule.IngressRuleValue.HTTP == nil {
				continue
			}
			for _, path := range rule.IngressRuleValue.HTTP.Paths {
				if path.Backend.Service != nil {
					tags["backend_service_name"] = path.Backend.Service.Name
					fields["backend_service_port"] = path.Backend.Service.Port.Number
				}

				fields["tls"] = i.Spec.TLS != nil

				tags["path"] = path.Path
				tags["host"] = rule.Host

				acc.AddFields(ingressMeasurement, fields, tags)
			}
		}
	}
}
