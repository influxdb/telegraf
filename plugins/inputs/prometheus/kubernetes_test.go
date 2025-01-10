package prometheus

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"github.com/influxdata/telegraf/testutil"
)

func initPrometheus() *Prometheus {
	prom := &Prometheus{Log: testutil.Logger{}}
	prom.MonitorKubernetesPodsScheme = "http"
	prom.MonitorKubernetesPodsPort = 9102
	prom.MonitorKubernetesPodsPath = "/metrics"
	prom.MonitorKubernetesPodsMethod = monitorMethodAnnotations
	prom.kubernetesPods = map[podID]urlAndAddress{}
	return prom
}

func TestScrapeURLNoAnnotations(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{}}
	p.Annotations = map[string]string{}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Nil(t, url)
}

func TestScrapeURLNoAnnotationsScrapeConfig(t *testing.T) {
	prom := initPrometheus()
	prom.MonitorKubernetesPodsMethod = monitorMethodSettingsAndAnnotations

	p := pod()
	p.Annotations = map[string]string{}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9102/metrics", url.String())
}

func TestScrapeURLScrapeConfigCustom(t *testing.T) {
	prom := initPrometheus()
	prom.MonitorKubernetesPodsMethod = monitorMethodSettingsAndAnnotations

	prom.MonitorKubernetesPodsScheme = "https"
	prom.MonitorKubernetesPodsPort = 9999
	prom.MonitorKubernetesPodsPath = "/svc/metrics"
	p := pod()
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "https://127.0.0.1:9999/svc/metrics", url.String())
}

func TestScrapeURLAnnotations(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}}
	p := pod()
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9102/metrics", url.String())
}

func TestScrapeURLAnnotationsScrapeConfig(t *testing.T) {
	prom := initPrometheus()
	prom.MonitorKubernetesPodsMethod = monitorMethodSettingsAndAnnotations
	p := pod()
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9102/metrics", url.String())
}

func TestScrapeURLAnnotationsCustomPort(t *testing.T) {
	prom := initPrometheus()
	p := pod()
	p.Annotations = map[string]string{"prometheus.io/port": "9000"}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9000/metrics", url.String())
}

func TestScrapeURLAnnotationsCustomPortScrapeConfig(t *testing.T) {
	prom := initPrometheus()
	prom.MonitorKubernetesPodsMethod = monitorMethodSettingsAndAnnotations
	p := pod()
	p.Annotations = map[string]string{"prometheus.io/port": "9000"}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9000/metrics", url.String())
}

func TestScrapeURLAnnotationsCustomPath(t *testing.T) {
	prom := initPrometheus()
	p := pod()
	p.Annotations = map[string]string{"prometheus.io/path": "mymetrics"}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9102/mymetrics", url.String())
}

func TestScrapeURLAnnotationsCustomPathWithSep(t *testing.T) {
	prom := initPrometheus()
	p := pod()
	p.Annotations = map[string]string{"prometheus.io/path": "/mymetrics"}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9102/mymetrics", url.String())
}

func TestScrapeURLAnnotationsCustomPathWithQueryParameters(t *testing.T) {
	prom := initPrometheus()
	p := pod()
	p.Annotations = map[string]string{"prometheus.io/path": "/v1/agent/metrics?format=prometheus"}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9102/v1/agent/metrics?format=prometheus", url.String())
}

func TestScrapeURLAnnotationsCustomPathWithFragment(t *testing.T) {
	prom := initPrometheus()
	p := pod()
	p.Annotations = map[string]string{"prometheus.io/path": "/v1/agent/metrics#prometheus"}
	url, err := getScrapeURL(p, prom)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9102/v1/agent/metrics#prometheus", url.String())
}

func TestAddPod(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}, kubernetesPods: map[podID]urlAndAddress{}}

	p := pod()
	p.Annotations = map[string]string{"prometheus.io/scrape": "true"}
	registerPod(p, prom)
	require.Len(t, prom.kubernetesPods, 1)
}

func TestAddPodScrapeConfig(t *testing.T) {
	prom := initPrometheus()
	prom.MonitorKubernetesPodsMethod = monitorMethodSettingsAndAnnotations

	p := pod()
	p.Annotations = map[string]string{}
	registerPod(p, prom)
	require.Len(t, prom.kubernetesPods, 1)
}

func TestAddMultipleDuplicatePods(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}, kubernetesPods: map[podID]urlAndAddress{}}

	p := pod()
	p.Annotations = map[string]string{"prometheus.io/scrape": "true"}
	registerPod(p, prom)
	p.Name = "Pod2"
	registerPod(p, prom)

	urls, err := prom.getAllURLs()
	require.NoError(t, err)
	require.Len(t, urls, 1)
}

func TestAddMultiplePods(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}, kubernetesPods: map[podID]urlAndAddress{}}

	p := pod()
	p.Annotations = map[string]string{"prometheus.io/scrape": "true"}
	registerPod(p, prom)
	p.Name = "Pod2"
	p.Status.PodIP = "127.0.0.2"
	registerPod(p, prom)
	require.Len(t, prom.kubernetesPods, 2)
}

func TestDeletePods(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}, kubernetesPods: map[podID]urlAndAddress{}}

	p := pod()
	p.Annotations = map[string]string{"prometheus.io/scrape": "true"}
	registerPod(p, prom)

	id, err := cache.MetaNamespaceKeyFunc(p)
	require.NoError(t, err)
	unregisterPod(podID(id), prom)
	require.Empty(t, prom.kubernetesPods)
}

func TestKeepDefaultNamespaceLabelName(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}, kubernetesPods: map[podID]urlAndAddress{}}

	p := pod()
	p.Annotations = map[string]string{"prometheus.io/scrape": "true"}
	registerPod(p, prom)

	id, err := cache.MetaNamespaceKeyFunc(p)
	require.NoError(t, err)
	tags := prom.kubernetesPods[podID(id)].tags
	require.Equal(t, "default", tags["namespace"])
}

func TestChangeNamespaceLabelName(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}, PodNamespaceLabelName: "pod_namespace", kubernetesPods: map[podID]urlAndAddress{}}

	p := pod()
	p.Annotations = map[string]string{"prometheus.io/scrape": "true"}
	registerPod(p, prom)

	id, err := cache.MetaNamespaceKeyFunc(p)
	require.NoError(t, err)
	tags := prom.kubernetesPods[podID(id)].tags
	require.Equal(t, "default", tags["pod_namespace"])
	require.Equal(t, "", tags["namespace"])
}

func TestPodHasMatchingNamespace(t *testing.T) {
	prom := &Prometheus{Log: testutil.Logger{}, PodNamespace: "default"}

	pod := pod()
	pod.Name = "Pod1"
	pod.Namespace = "default"
	shouldMatch := podHasMatchingNamespace(pod, prom)
	require.True(t, shouldMatch)

	pod.Name = "Pod2"
	pod.Namespace = "namespace"
	shouldNotMatch := podHasMatchingNamespace(pod, prom)
	require.False(t, shouldNotMatch)
}

func TestPodHasMatchingLabelSelector(t *testing.T) {
	labelSelectorString := "label0==label0,label1=label1,label2!=label,label3 in (label1,label2, label3),label4 notin (label1, label2,label3),label5,!label6"
	prom := &Prometheus{Log: testutil.Logger{}, KubernetesLabelSelector: labelSelectorString}

	pod := pod()
	pod.Labels = make(map[string]string)
	pod.Labels["label0"] = "label0"
	pod.Labels["label1"] = "label1"
	pod.Labels["label2"] = "label2"
	pod.Labels["label3"] = "label3"
	pod.Labels["label4"] = "label4"
	pod.Labels["label5"] = "label5"

	labelSelector, err := labels.Parse(prom.KubernetesLabelSelector)
	require.NoError(t, err)
	require.True(t, podHasMatchingLabelSelector(pod, labelSelector))
}

func TestPodHasMatchingFieldSelector(t *testing.T) {
	fieldSelectorString := "status.podIP=127.0.0.1,spec.restartPolicy=Always,spec.NodeName!=nodeName"
	prom := &Prometheus{Log: testutil.Logger{}, KubernetesFieldSelector: fieldSelectorString}
	pod := pod()
	pod.Spec.RestartPolicy = "Always"
	pod.Spec.NodeName = "node1000"

	fieldSelector, err := fields.ParseSelector(prom.KubernetesFieldSelector)
	require.NoError(t, err)
	require.True(t, podHasMatchingFieldSelector(pod, fieldSelector))
}

func TestInvalidFieldSelector(t *testing.T) {
	fieldSelectorString := "status.podIP=127.0.0.1,spec.restartPolicy=Always,spec.NodeName!=nodeName,spec.nodeName"
	prom := &Prometheus{Log: testutil.Logger{}, KubernetesFieldSelector: fieldSelectorString}
	pod := pod()
	pod.Spec.RestartPolicy = "Always"
	pod.Spec.NodeName = "node1000"

	_, err := fields.ParseSelector(prom.KubernetesFieldSelector)
	require.Error(t, err)
}

func TestAnnotationFilters(t *testing.T) {
	p := pod()
	p.Annotations = map[string]string{
		"prometheus.io/scrape": "true",
		"includeme":            "true",
		"excludeme":            "true",
		"neutral":              "true",
	}

	cases := []struct {
		desc         string
		include      []string
		exclude      []string
		expectedTags []string
	}{
		{"Just include",
			[]string{"includeme"},
			nil,
			[]string{"includeme"}},
		{"Just exclude",
			nil,
			[]string{"excludeme"},
			[]string{"includeme", "neutral"}},
		{"Include & exclude",
			[]string{"includeme"},
			[]string{"exludeme"},
			[]string{"includeme"}},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			prom := &Prometheus{Log: testutil.Logger{}, kubernetesPods: map[podID]urlAndAddress{}}
			prom.PodAnnotationInclude = tc.include
			prom.PodAnnotationExclude = tc.exclude
			require.NoError(t, prom.initFilters())
			registerPod(p, prom)
			for _, pd := range prom.kubernetesPods {
				for _, tagKey := range tc.expectedTags {
					require.Contains(t, pd.tags, tagKey)
				}
			}
		})
	}
}

func TestLabelFilters(t *testing.T) {
	p := pod()
	p.Annotations = map[string]string{"prometheus.io/scrape": "true"}
	p.Labels = map[string]string{
		"includeme": "true",
		"excludeme": "true",
		"neutral":   "true",
	}

	cases := []struct {
		desc         string
		include      []string
		exclude      []string
		expectedTags []string
	}{
		{"Just include",
			[]string{"includeme"},
			nil,
			[]string{"includeme"}},
		{"Just exclude",
			nil,
			[]string{"excludeme"},
			[]string{"includeme", "neutral"}},
		{"Include & exclude",
			[]string{"includeme"},
			[]string{"exludeme"},
			[]string{"includeme"}},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			prom := &Prometheus{Log: testutil.Logger{}, kubernetesPods: map[podID]urlAndAddress{}}
			prom.PodLabelInclude = tc.include
			prom.PodLabelExclude = tc.exclude
			require.NoError(t, prom.initFilters())
			registerPod(p, prom)
			for _, pd := range prom.kubernetesPods {
				for _, tagKey := range tc.expectedTags {
					require.Contains(t, pd.tags, tagKey)
				}
			}
		})
	}
}

func pod() *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{}, Status: corev1.PodStatus{}, Spec: corev1.PodSpec{}}
	p.Status.PodIP = "127.0.0.1"
	p.Name = "myPod"
	p.Namespace = "default"
	return p
}
