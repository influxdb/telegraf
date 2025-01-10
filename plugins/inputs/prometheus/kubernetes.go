package prometheus

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/models"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type podMetadata struct {
	ResourceVersion string `json:"resourceVersion"`
	SelfLink        string `json:"selfLink"`
}

type podResponse struct {
	Kind       string        `json:"kind"`
	APIVersion string        `json:"apiVersion"`
	Metadata   podMetadata   `json:"metadata"`
	Items      []*corev1.Pod `json:"items,omitempty"`
}

const cAdvisorPodListDefaultInterval = 60

// loadConfig parses a kubeconfig from a file and returns a Kubernetes rest.Config
func loadConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		return rest.InClusterConfig()
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}

func (p *Prometheus) startK8s(ctx context.Context) error {
	config, err := loadConfig(p.KubeConfig)
	if err != nil {
		return fmt.Errorf("failed to get rest.Config from %q: %w", p.KubeConfig, err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to get current user: %w", err)
		}

		kubeconfig := filepath.Join(u.HomeDir, ".kube", "config")

		config, err = loadConfig(kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to get rest.Config from %q: %w", kubeconfig, err)
		}

		client, err = kubernetes.NewForConfig(config)
		if err != nil {
			return fmt.Errorf("failed to get kubernetes client: %w", err)
		}
	}

	if !p.isNodeScrapeScope {
		err = p.watchPod(ctx, client)
		if err != nil {
			p.Log.Warnf("Error while attempting to watch pod: %s", err.Error())
		}
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				if p.isNodeScrapeScope {
					bearerToken := config.BearerToken
					if config.BearerTokenFile != "" {
						bearerTokenBytes, err := os.ReadFile(config.BearerTokenFile)
						if err != nil {
							p.Log.Errorf("Error reading bearer token file hence falling back to BearerToken: %s", err.Error())
						} else {
							bearerToken = string(bearerTokenBytes)
						}
					}
					err = p.cAdvisor(ctx, bearerToken)
					if err != nil {
						p.Log.Errorf("Unable to monitor pods with node scrape scope: %s", err.Error())
					}
				} else {
					<-ctx.Done()
				}
			}
		}
	}()

	return nil
}

func shouldScrapePod(pod *corev1.Pod, p *Prometheus) bool {
	isCandidate := podReady(pod) &&
		podHasMatchingNamespace(pod, p) &&
		podHasMatchingLabelSelector(pod, p.podLabelSelector) &&
		podHasMatchingFieldSelector(pod, p.podFieldSelector)

	var shouldScrape bool
	switch p.MonitorKubernetesPodsMethod {
	case monitorMethodAnnotations: // must have 'true' annotation to be scraped
		shouldScrape = pod.Annotations != nil && pod.Annotations["prometheus.io/scrape"] == "true"
	case monitorMethodSettings: // will be scraped regardless of annotation
		shouldScrape = true
	case monitorMethodSettingsAndAnnotations: // will be scraped unless opts out with 'false' annotation
		shouldScrape = pod.Annotations == nil || pod.Annotations["prometheus.io/scrape"] != "false"
	}

	return isCandidate && shouldScrape
}

// Share informer per namespace across all instances of this plugin
var informerfactory map[string]informers.SharedInformerFactory

// An edge case exists if a pod goes offline at the same time a new pod is created
// (without the scrape annotations). K8s may re-assign the old pod ip to the non-scrape
// pod, causing errors in the logs. This is only true if the pod going offline is not
// directed to do so by K8s.
func (p *Prometheus) watchPod(ctx context.Context, clientset *kubernetes.Clientset) error {
	var resyncinterval time.Duration

	if p.CacheRefreshInterval != 0 {
		resyncinterval = time.Duration(p.CacheRefreshInterval) * time.Minute
	} else {
		resyncinterval = 60 * time.Minute
	}

	if informerfactory == nil {
		informerfactory = make(map[string]informers.SharedInformerFactory)
	}

	var f informers.SharedInformerFactory
	var ok bool
	if f, ok = informerfactory[p.PodNamespace]; !ok {
		var informerOptions []informers.SharedInformerOption
		if p.PodNamespace != "" {
			informerOptions = append(informerOptions, informers.WithNamespace(p.PodNamespace))
		}
		f = informers.NewSharedInformerFactoryWithOptions(clientset, resyncinterval, informerOptions...)
		informerfactory[p.PodNamespace] = f
	}

	if p.nsAnnotationPass != nil || p.nsAnnotationDrop != nil {
		p.nsStore = f.Core().V1().Namespaces().Informer().GetStore()
	}

	podinformer := f.Core().V1().Pods()
	_, err := podinformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			newPod, ok := newObj.(*corev1.Pod)
			if !ok {
				p.Log.Errorf("[BUG] received unexpected object: %v", newObj)
				return
			}
			if shouldScrapePod(newPod, p) {
				registerPod(newPod, p)
			}
		},
		// On Pod status updates and regular reList by Informer
		UpdateFunc: func(_, newObj interface{}) {
			newPod, ok := newObj.(*corev1.Pod)
			if !ok {
				p.Log.Errorf("[BUG] received unexpected object: %v", newObj)
				return
			}

			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newObj)
			if err != nil {
				p.Log.Errorf("getting key from cache %s", err.Error())
			}
			podID := podID(key)
			if shouldScrapePod(newPod, p) {
				// When Informers re-Lists, pod might already be registered,
				// do nothing if it is, register otherwise
				if _, ok = p.kubernetesPods[podID]; !ok {
					registerPod(newPod, p)
				}
			} else {
				// Pods are largely immutable, but it's readiness status can change, unregister then
				unregisterPod(podID, p)
			}
		},
		DeleteFunc: func(oldObj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(oldObj)
			if err == nil {
				unregisterPod(podID(key), p)
			}
		},
	})

	f.Start(ctx.Done())
	f.WaitForCacheSync(wait.NeverStop)
	return err
}

func (p *Prometheus) cAdvisor(ctx context.Context, bearerToken string) error {
	// The request will be the same each time
	podsURL := fmt.Sprintf("https://%s:10250/pods", p.NodeIP)
	req, err := http.NewRequest("GET", podsURL, nil)
	if err != nil {
		return fmt.Errorf("error when creating request to %s to get pod list: %w", podsURL, err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Add("Accept", "application/json")

	// Update right away so code is not waiting the length of the specified scrape interval initially
	err = updateCadvisorPodList(p, req)
	if err != nil {
		return fmt.Errorf("error initially updating pod list: %w", err)
	}

	scrapeInterval := cAdvisorPodListDefaultInterval
	if p.PodScrapeInterval != 0 {
		scrapeInterval = p.PodScrapeInterval
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Duration(scrapeInterval) * time.Second):
			err := updateCadvisorPodList(p, req)
			if err != nil {
				return fmt.Errorf("error updating pod list: %w", err)
			}
		}
	}
}

func updateCadvisorPodList(p *Prometheus, req *http.Request) error {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	httpClient := http.Client{}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error when making request for pod list: %w", err)
	}

	// If err is nil, still check response code
	if resp.StatusCode != 200 {
		return fmt.Errorf("error when making request for pod list with status %s", resp.Status)
	}

	defer resp.Body.Close()

	cadvisorPodsResponse := podResponse{}

	// Will have expected type errors for some parts of corev1.Pod struct for some unused fields
	// Instead have nil checks for every used field in case of incorrect decoding
	if err := json.NewDecoder(resp.Body).Decode(&cadvisorPodsResponse); err != nil {
		return fmt.Errorf("decoding response failed: %w", err)
	}
	pods := cadvisorPodsResponse.Items

	// Updating pod list to be latest cadvisor response
	p.lock.Lock()
	p.kubernetesPods = make(map[podID]urlAndAddress)

	// Register pod only if it has an annotation to scrape, if it is ready,
	// and if namespace and selectors are specified and match
	for _, pod := range pods {
		if necessaryPodFieldsArePresent(pod) && shouldScrapePod(pod, p) {
			registerPod(pod, p)
		}
	}
	p.lock.Unlock()

	// No errors
	return nil
}

func necessaryPodFieldsArePresent(pod *corev1.Pod) bool {
	return pod.Annotations != nil &&
		pod.Labels != nil &&
		pod.Status.ContainerStatuses != nil
}

/* See the docs on kubernetes label selectors:
 * https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
 */
func podHasMatchingLabelSelector(pod *corev1.Pod, labelSelector labels.Selector) bool {
	if labelSelector == nil {
		return true
	}

	var labelsSet labels.Set = pod.Labels
	return labelSelector.Matches(labelsSet)
}

/* See ToSelectableFields() for list of fields that are selectable:
 * https://github.com/kubernetes/kubernetes/release-1.20/pkg/registry/core/pod/strategy.go
 * See docs on kubernetes field selectors:
 * https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/
 */
func podHasMatchingFieldSelector(pod *corev1.Pod, fieldSelector fields.Selector) bool {
	if fieldSelector == nil {
		return true
	}

	fieldsSet := make(fields.Set)
	fieldsSet["spec.nodeName"] = pod.Spec.NodeName
	fieldsSet["spec.restartPolicy"] = string(pod.Spec.RestartPolicy)
	fieldsSet["spec.schedulerName"] = pod.Spec.SchedulerName
	fieldsSet["spec.serviceAccountName"] = pod.Spec.ServiceAccountName
	fieldsSet["status.phase"] = string(pod.Status.Phase)
	fieldsSet["status.podIP"] = pod.Status.PodIP
	fieldsSet["status.nominatedNodeName"] = pod.Status.NominatedNodeName

	return fieldSelector.Matches(fieldsSet)
}

// Get corev1.Namespace object by name
func getNamespaceObject(name string, p *Prometheus) *corev1.Namespace {
	nsObj, exists, err := p.nsStore.GetByKey(name)
	if err != nil {
		p.Log.Errorf("Err fetching namespace '%s': %v", name, err)
		return nil
	} else if !exists {
		return nil // can't happen
	}
	ns, ok := nsObj.(*corev1.Namespace)
	if !ok {
		p.Log.Errorf("[BUG] received unexpected object: %v", nsObj)
		return nil
	}
	return ns
}

func namespaceAnnotationMatch(nsName string, p *Prometheus) bool {
	// In case of no filtering or any issues with acquiring namespace information
	// just let it pass trough...
	if (p.nsAnnotationPass == nil && p.nsAnnotationDrop == nil) || p.nsStore == nil {
		return true
	}
	ns := getNamespaceObject(nsName, p)
	if ns == nil {
		return true
	}

	tags := make([]*telegraf.Tag, 0, len(ns.Annotations))
	for k, v := range ns.Annotations {
		tags = append(tags, &telegraf.Tag{Key: k, Value: v})
	}
	return models.ShouldTagsPass(p.nsAnnotationPass, p.nsAnnotationDrop, tags)
}

/*
 * If a namespace is specified and the pod doesn't have that namespace, return false
 * Else return true
 */
func podHasMatchingNamespace(pod *corev1.Pod, p *Prometheus) bool {
	return p.PodNamespace == "" || pod.Namespace == p.PodNamespace
}

func podReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return pod.Status.Phase == corev1.PodRunning
		}
	}
	return false
}

func registerPod(pod *corev1.Pod, p *Prometheus) {
	targetURL, err := getScrapeURL(pod, p)
	if err != nil {
		p.Log.Errorf("could not parse URL: %s", err)
		return
	} else if targetURL == nil {
		return
	}

	p.Log.Debugf("will scrape metrics from %q", targetURL.String())
	tags := make(map[string]string, len(pod.Annotations)+len(pod.Labels)+2)

	// add annotation as metrics tags, subject to include/exclude filters
	for k, v := range pod.Annotations {
		if models.ShouldPassFilters(p.podAnnotationIncludeFilter, p.podAnnotationExcludeFilter, k) {
			tags[k] = v
		}
	}

	tags["pod_name"] = pod.Name
	podNamespace := "namespace"
	if p.PodNamespaceLabelName != "" {
		podNamespace = p.PodNamespaceLabelName
	}
	tags[podNamespace] = pod.Namespace

	// add labels as metrics tags, subject to include/exclude filters
	for k, v := range pod.Labels {
		if models.ShouldPassFilters(p.podLabelIncludeFilter, p.podLabelExcludeFilter, k) {
			tags[k] = v
		}
	}
	podURL := addressToURL(targetURL, targetURL.Hostname())

	// Locks earlier if using cAdvisor calls - makes a new list each time
	// rather than updating and removing from the same list
	if !p.isNodeScrapeScope {
		p.lock.Lock()
		defer p.lock.Unlock()
	}
	p.kubernetesPods[podID(pod.GetNamespace()+"/"+pod.GetName())] = urlAndAddress{
		url:         podURL,
		address:     targetURL.Hostname(),
		originalURL: targetURL,
		tags:        tags,
		namespace:   pod.GetNamespace(),
	}
}

func getScrapeURL(pod *corev1.Pod, p *Prometheus) (*url.URL, error) {
	ip := pod.Status.PodIP
	if ip == "" {
		// return as if scrape was disabled, we will be notified again once the pod
		// has an IP
		return nil, nil
	}

	var scheme, pathAndQuery, port string

	if p.MonitorKubernetesPodsMethod == monitorMethodSettings ||
		p.MonitorKubernetesPodsMethod == monitorMethodSettingsAndAnnotations {
		scheme = p.MonitorKubernetesPodsScheme
		pathAndQuery = p.MonitorKubernetesPodsPath
		port = strconv.Itoa(p.MonitorKubernetesPodsPort)
	}

	if p.MonitorKubernetesPodsMethod == monitorMethodAnnotations ||
		p.MonitorKubernetesPodsMethod == monitorMethodSettingsAndAnnotations {
		if ann := pod.Annotations["prometheus.io/scheme"]; ann != "" {
			scheme = ann
		}
		if ann := pod.Annotations["prometheus.io/path"]; ann != "" {
			pathAndQuery = ann
		}
		if ann := pod.Annotations["prometheus.io/port"]; ann != "" {
			port = ann
		}
	}

	if scheme == "" {
		scheme = "http"
	}

	if port == "" || port == "0" {
		port = "9102"
	}

	if pathAndQuery == "" {
		pathAndQuery = "/metrics"
	}

	base, err := url.Parse(pathAndQuery)
	if err != nil {
		return nil, err
	}

	base.Scheme = scheme
	base.Host = net.JoinHostPort(ip, port)

	return base, nil
}

func unregisterPod(podID podID, p *Prometheus) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if v, ok := p.kubernetesPods[podID]; ok {
		p.Log.Debugf("registered a delete request for %s", podID)
		delete(p.kubernetesPods, podID)
		p.Log.Debugf("will stop scraping for %q", v.url.String())
	}
}
