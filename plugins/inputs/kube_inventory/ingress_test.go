package kube_inventory

import (
	"testing"
	"time"

	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestIngress(t *testing.T) {
	now := time.Now()
	now = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 1, 36, 0, now.Location())

	tests := []struct {
		name     string
		handler  *mockHandler
		output   []telegraf.Metric
		hasError bool
	}{
		{
			name: "no ingress",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/ingress/": netv1.IngressList{},
				},
			},
			hasError: false,
		},
		{
			name: "collect ingress",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/ingress/": netv1.IngressList{
						Items: []netv1.Ingress{
							{
								Status: netv1.IngressStatus{
									LoadBalancer: netv1.IngressLoadBalancerStatus{
										Ingress: []netv1.IngressLoadBalancerIngress{
											{
												Hostname: "chron-1",
												IP:       "1.0.0.127",
											},
										},
									},
								},
								Spec: netv1.IngressSpec{
									Rules: []netv1.IngressRule{
										{
											Host: "ui.internal",
											IngressRuleValue: netv1.IngressRuleValue{
												HTTP: &netv1.HTTPIngressRuleValue{
													Paths: []netv1.HTTPIngressPath{
														{
															Path: "/",
															Backend: netv1.IngressBackend{
																Service: &netv1.IngressServiceBackend{
																	Name: "chronografd",
																	Port: netv1.ServiceBackendPort{
																		Number: 8080,
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
								ObjectMeta: metav1.ObjectMeta{
									Generation:        12,
									Namespace:         "ns1",
									Name:              "ui-lb",
									CreationTimestamp: metav1.Time{Time: now},
								},
							},
						},
					},
				},
			},
			output: []telegraf.Metric{
				testutil.MustMetric(
					"kubernetes_ingress",
					map[string]string{
						"ingress_name":         "ui-lb",
						"namespace":            "ns1",
						"ip":                   "1.0.0.127",
						"hostname":             "chron-1",
						"backend_service_name": "chronografd",
						"host":                 "ui.internal",
						"path":                 "/",
					},
					map[string]interface{}{
						"tls":                  false,
						"backend_service_port": int32(8080),
						"generation":           int64(12),
						"created":              now.UnixNano(),
					},
					time.Unix(0, 0),
				),
			},
			hasError: false,
		},
		{
			name: "no HTTPIngressRuleValue",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/ingress/": netv1.IngressList{
						Items: []netv1.Ingress{
							{
								Status: netv1.IngressStatus{
									LoadBalancer: netv1.IngressLoadBalancerStatus{
										Ingress: []netv1.IngressLoadBalancerIngress{
											{
												Hostname: "chron-1",
												IP:       "1.0.0.127",
											},
										},
									},
								},
								Spec: netv1.IngressSpec{
									Rules: []netv1.IngressRule{
										{
											Host: "ui.internal",
											IngressRuleValue: netv1.IngressRuleValue{
												HTTP: nil,
											},
										},
									},
								},
								ObjectMeta: metav1.ObjectMeta{
									Generation:        12,
									Namespace:         "ns1",
									Name:              "ui-lb",
									CreationTimestamp: metav1.Time{Time: now},
								},
							},
						},
					},
				},
			},
			hasError: false,
		},
		{
			name: "no IngressServiceBackend",
			handler: &mockHandler{
				responseMap: map[string]interface{}{
					"/ingress/": netv1.IngressList{
						Items: []netv1.Ingress{
							{
								Status: netv1.IngressStatus{
									LoadBalancer: netv1.IngressLoadBalancerStatus{
										Ingress: []netv1.IngressLoadBalancerIngress{
											{
												Hostname: "chron-1",
												IP:       "1.0.0.127",
											},
										},
									},
								},
								Spec: netv1.IngressSpec{
									Rules: []netv1.IngressRule{
										{
											Host: "ui.internal",
											IngressRuleValue: netv1.IngressRuleValue{
												HTTP: &netv1.HTTPIngressRuleValue{
													Paths: []netv1.HTTPIngressPath{
														{
															Path: "/",
															Backend: netv1.IngressBackend{
																Service: nil,
															},
														},
													},
												},
											},
										},
									},
								},
								ObjectMeta: metav1.ObjectMeta{
									Generation:        12,
									Namespace:         "ns1",
									Name:              "ui-lb",
									CreationTimestamp: metav1.Time{Time: now},
								},
							},
						},
					},
				},
			},
			output: []telegraf.Metric{
				testutil.MustMetric(
					"kubernetes_ingress",
					map[string]string{
						"ingress_name": "ui-lb",
						"namespace":    "ns1",
						"ip":           "1.0.0.127",
						"hostname":     "chron-1",
						"host":         "ui.internal",
						"path":         "/",
					},
					map[string]interface{}{
						"tls":        false,
						"generation": int64(12),
						"created":    now.UnixNano(),
					},
					time.Unix(0, 0),
				),
			},
			hasError: false,
		},
	}

	for _, v := range tests {
		acc := new(testutil.Accumulator)
		for _, ingress := range ((v.handler.responseMap["/ingress/"]).(netv1.IngressList)).Items {
			gatherIngress(ingress, acc)
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
