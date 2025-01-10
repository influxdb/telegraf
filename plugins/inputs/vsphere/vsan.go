package vsphere

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	vsanmethods "github.com/vmware/govmomi/vsan/methods"
	vsantypes "github.com/vmware/govmomi/vsan/types"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
)

const (
	vsanNamespace    = "vsan"
	vsanPath         = "/vsanHealth"
	hwMarksKeyPrefix = "vsan-perf-"
	perfPrefix       = "performance."
)

var (
	vsanPerfMetricsName    string
	vsanSummaryMetricsName string
	perfManagerRef         = types.ManagedObjectReference{
		Type:  "VsanPerformanceManager",
		Value: "vsan-performance-manager",
	}
	hyphenReplacer = strings.NewReplacer("-", "")
)

// collectVsan is the entry point for vsan metrics collection
func (e *Endpoint) collectVsan(ctx context.Context, acc telegraf.Accumulator) error {
	lower := versionLowerThan(e.apiVersion, 5, 5)
	if lower {
		return fmt.Errorf("a minimum API version of 5.5 is required for vSAN. Found: %s. Skipping vCenter: %s", e.apiVersion, e.URL.Host)
	}
	vsanPerfMetricsName = strings.Join([]string{"vsphere", "vsan", "performance"}, e.Parent.Separator)
	vsanSummaryMetricsName = strings.Join([]string{"vsphere", "vsan", "summary"}, e.Parent.Separator)
	res := e.resourceKinds["vsan"]
	client, err := e.clientFactory.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("fail to get client when collect vsan: %w", err)
	}
	// Create vSAN client
	vimClient := client.Client.Client
	vsanClient := vimClient.NewServiceClient(vsanPath, vsanNamespace)
	// vSAN Metrics to collect
	metrics := e.getVsanMetadata(ctx, vsanClient, res)
	// Iterate over all clusters, run a goroutine for each cluster
	te := NewThrottledExecutor(e.Parent.CollectConcurrency)
	for _, obj := range res.objects {
		te.Run(ctx, func() {
			e.collectVsanPerCluster(ctx, obj, vimClient, vsanClient, metrics, acc)
		})
	}
	te.Wait()
	return nil
}

// collectVsanPerCluster is called by goroutines in collectVsan function.
func (e *Endpoint) collectVsanPerCluster(ctx context.Context, clusterRef *objectRef, vimClient *vim25.Client, vsanClient *soap.Client,
	metrics map[string]string, acc telegraf.Accumulator) {
	// Construct a map for cmmds
	cluster := object.NewClusterComputeResource(vimClient, clusterRef.ref)
	if !vsanEnabled(ctx, cluster) {
		acc.AddError(fmt.Errorf("[vSAN] Fail to identify vSAN for cluster %s. Skipping", clusterRef.name))
		return
	}
	// Do collection
	if _, ok := metrics["summary.disk-usage"]; ok {
		if err := e.queryDiskUsage(ctx, vsanClient, clusterRef, acc); err != nil {
			acc.AddError(fmt.Errorf("error querying disk usage for cluster %s: %w", clusterRef.name, err))
		}
	}
	if _, ok := metrics["summary.health"]; ok {
		if err := e.queryHealthSummary(ctx, vsanClient, clusterRef, acc); err != nil {
			acc.AddError(fmt.Errorf("error querying vsan health summary for cluster %s: %w", clusterRef.name, err))
		}
	}
	if _, ok := metrics["summary.resync"]; ok {
		if err := e.queryResyncSummary(ctx, vsanClient, cluster, clusterRef, acc); err != nil {
			acc.AddError(fmt.Errorf("error querying vsan resync summary for cluster %s: %w", clusterRef.name, err))
		}
	}
	cmmds, err := getCmmdsMap(ctx, vimClient, cluster)
	if err != nil {
		e.Parent.Log.Errorf("[vSAN] Error while query cmmds data. Error: %s. Skipping", err)
		cmmds = make(map[string]CmmdsEntity)
	}
	if err := e.queryPerformance(ctx, vsanClient, clusterRef, metrics, cmmds, acc); err != nil {
		acc.AddError(fmt.Errorf("error querying performance metrics for cluster %s: %w", clusterRef.name, err))
	}
}

// vsanEnabled returns True if vSAN is enabled, otherwise False
func vsanEnabled(ctx context.Context, clusterObj *object.ClusterComputeResource) bool {
	config, err := clusterObj.Configuration(ctx)
	if err != nil {
		return false
	}
	enabled := config.VsanConfigInfo.Enabled
	return enabled != nil && *enabled
}

// getVsanMetadata returns a string list of the entity types that will be queried.
// e.g ["summary.health", "summary.disk-usage", "summary.resync", "performance.cluster-domclient", "performance.host-domclient"]
func (e *Endpoint) getVsanMetadata(ctx context.Context, vsanClient *soap.Client, res *resourceKind) map[string]string {
	metrics := make(map[string]string)
	if res.simple { // Skip getting supported Entity types from vCenter. Using user defined metrics without verifying.
		for _, entity := range res.include {
			if strings.Contains(entity, "*") {
				e.Parent.Log.Infof("[vSAN] Won't use wildcard match \"*\" when vsan_metric_skip_verify = true. Skipping")
				continue
			}
			metrics[entity] = ""
		}
		return metrics
	}
	// Use the include & exclude configuration to filter all summary metrics
	for _, entity := range []string{"summary.health", "summary.disk-usage", "summary.resync"} {
		if res.filters.Match(entity) {
			metrics[entity] = ""
		}
	}
	resp, err := vsanmethods.VsanPerfGetSupportedEntityTypes(ctx, vsanClient,
		&vsantypes.VsanPerfGetSupportedEntityTypes{
			This: perfManagerRef,
		})
	if err != nil {
		e.Parent.Log.Errorf("[vSAN] Fail to get supported entities: %v. Skipping vsan performance data.", err)
		return metrics
	}
	// Use the include & exclude configuration to filter all supported performance metrics
	for _, entity := range resp.Returnval {
		if res.filters.Match(perfPrefix + entity.Name) {
			metrics[perfPrefix+entity.Name] = ""
		}
	}
	return metrics
}

// getCmmdsMap returns a map which maps a uuid to a CmmdsEntity
func getCmmdsMap(ctx context.Context, client *vim25.Client, clusterObj *object.ClusterComputeResource) (map[string]CmmdsEntity, error) {
	hosts, err := clusterObj.Hosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("fail to get host: %w", err)
	}

	if len(hosts) == 0 {
		return make(map[string]CmmdsEntity), nil
	}

	queries := []types.HostVsanInternalSystemCmmdsQuery{
		{Type: "HOSTNAME"},
		{Type: "DISK"},
		{Type: "DISK_CAPACITY_TIER"},
	}

	// Some esx host can be down or in maintenance mode. Hence cmmds query might fail on such hosts.
	// We iterate until be get proper api response
	var resp *types.QueryCmmdsResponse
	for _, host := range hosts {
		vis, err := host.ConfigManager().VsanInternalSystem(ctx)
		if err != nil {
			continue
		}
		request := types.QueryCmmds{
			This:    vis.Reference(),
			Queries: queries,
		}
		resp, err = methods.QueryCmmds(ctx, client.RoundTripper, &request)
		if err == nil {
			break
		}
	}
	if resp == nil {
		return nil, errors.New("all hosts fail to query cmmds")
	}
	var clusterCmmds Cmmds
	if err := json.Unmarshal([]byte(resp.Returnval), &clusterCmmds); err != nil {
		return nil, fmt.Errorf("fail to convert cmmds to json: %w", err)
	}

	cmmdsMap := make(map[string]CmmdsEntity)
	for _, entity := range clusterCmmds.Res {
		cmmdsMap[entity.UUID] = entity
	}
	return cmmdsMap, nil
}

// queryPerformance adds performance metrics to telegraf accumulator
func (e *Endpoint) queryPerformance(ctx context.Context, vsanClient *soap.Client, clusterRef *objectRef, metrics map[string]string,
	cmmds map[string]CmmdsEntity, acc telegraf.Accumulator) error {
	end := time.Now().UTC()

	// We're using a fake metric key, since we only store one highwater mark per resource
	start, ok := e.hwMarks.Get(hwMarksKeyPrefix+clusterRef.ref.Value, "generic")
	if !ok {
		// Look back 3 sampling periods by default
		start = end.Add(time.Duration(e.Parent.MetricLookback) * time.Duration(-e.resourceKinds["vsan"].sampling) * time.Second)
	}
	e.Parent.Log.Debugf("[vSAN] Query vsan performance for time interval: %s ~ %s", start, end)
	latest := start

	var commonError error
	for entityRefID := range metrics {
		if !strings.HasPrefix(entityRefID, perfPrefix) {
			continue
		}
		entityRefID = strings.TrimPrefix(entityRefID, perfPrefix)
		var perfSpecs []vsantypes.VsanPerfQuerySpec

		perfSpec := vsantypes.VsanPerfQuerySpec{
			EntityRefId: entityRefID + ":*",
			StartTime:   &start,
			EndTime:     &end,
		}
		perfSpecs = append(perfSpecs, perfSpec)

		perfRequest := vsantypes.VsanPerfQueryPerf{
			This:       perfManagerRef,
			QuerySpecs: perfSpecs,
			Cluster:    &clusterRef.ref,
		}
		resp, err := vsanmethods.VsanPerfQueryPerf(ctx, vsanClient, &perfRequest)
		if err != nil {
			if err.Error() == "ServerFaultCode: NotFound" {
				e.Parent.Log.Errorf("[vSAN] Is vSAN performance service enabled for %s? Skipping ...", clusterRef.name)
				commonError = err
				break
			}
			e.Parent.Log.Errorf("[vSAN] Error querying performance data for %s: %s: %s.", clusterRef.name, entityRefID, err)
			continue
		}
		tags := populateClusterTags(make(map[string]string), clusterRef, e.URL.Host)

		count := 0
		for _, em := range resp.Returnval {
			vals := strings.Split(em.EntityRefId, ":")
			var entityName, uuid string
			if len(vals) == 1 {
				entityName, uuid = vals[0], ""
			} else {
				entityName, uuid = vals[0], vals[1]
			}

			buckets := make(map[string]metricEntry)
			tags := populateCMMDSTags(tags, entityName, uuid, cmmds)
			var timeStamps []time.Time
			// 1. Construct a timestamp list from sample info
			formattedEntityName := hyphenReplacer.Replace(entityName)
			for _, t := range strings.Split(em.SampleInfo, ",") {
				// Parse the input string to a time.Time object
				utcTimeStamp, err := time.Parse("2006-01-02 15:04:05", t)
				if err != nil {
					e.Parent.Log.Errorf("[vSAN] Failed to parse a timestamp: %s. Skipping", utcTimeStamp)
					timeStamps = append(timeStamps, time.Time{})
					continue
				}
				timeStamps = append(timeStamps, utcTimeStamp)
			}
			// 2. Iterate on each measurement
			for _, counter := range em.Value {
				metricLabel := internal.SnakeCase(counter.MetricId.Label)
				// 3. Iterate on each data point.
				for i, values := range strings.Split(counter.Values, ",") {
					ts := timeStamps[i]
					if ts.IsZero() {
						continue
					}
					// Organize the metrics into a bucket per measurement.
					bKey := em.EntityRefId + " " + strconv.FormatInt(ts.UnixNano(), 10)
					bucket, found := buckets[bKey]
					if !found {
						mn := vsanPerfMetricsName + e.Parent.Separator + formattedEntityName
						bucket = metricEntry{name: mn, ts: ts, fields: make(map[string]interface{}), tags: tags}
						buckets[bKey] = bucket
					}
					if v, err := strconv.ParseFloat(values, 32); err == nil {
						bucket.fields[metricLabel] = v
					}
				}
			}
			if len(timeStamps) > 0 {
				lastSample := timeStamps[len(timeStamps)-1]
				if !lastSample.IsZero() && lastSample.After(latest) {
					latest = lastSample
				}
			}
			// We've iterated through all the metrics and collected buckets for each measurement name. Now emit them!
			for _, bucket := range buckets {
				acc.AddFields(bucket.name, bucket.fields, bucket.tags, bucket.ts)
			}
			count += len(buckets)
		}
	}
	e.hwMarks.Put(hwMarksKeyPrefix+clusterRef.ref.Value, "generic", latest)
	return commonError
}

// queryDiskUsage adds 'FreeCapacityB' and 'TotalCapacityB' metrics to telegraf accumulator
func (e *Endpoint) queryDiskUsage(ctx context.Context, vsanClient *soap.Client, clusterRef *objectRef, acc telegraf.Accumulator) error {
	spaceManagerRef := types.ManagedObjectReference{
		Type:  "VsanSpaceReportSystem",
		Value: "vsan-cluster-space-report-system",
	}
	resp, err := vsanmethods.VsanQuerySpaceUsage(ctx, vsanClient,
		&vsantypes.VsanQuerySpaceUsage{
			This:    spaceManagerRef,
			Cluster: clusterRef.ref,
		})
	if err != nil {
		return err
	}
	fields := map[string]interface{}{
		"free_capacity_byte":  resp.Returnval.FreeCapacityB,
		"total_capacity_byte": resp.Returnval.TotalCapacityB,
	}
	tags := populateClusterTags(make(map[string]string), clusterRef, e.URL.Host)
	acc.AddFields(vsanSummaryMetricsName, fields, tags)
	return nil
}

// queryDiskUsage adds 'OverallHealth' metric to telegraf accumulator
func (e *Endpoint) queryHealthSummary(ctx context.Context, vsanClient *soap.Client, clusterRef *objectRef, acc telegraf.Accumulator) error {
	healthSystemRef := types.ManagedObjectReference{
		Type:  "VsanVcClusterHealthSystem",
		Value: "vsan-cluster-health-system",
	}
	fetchFromCache := true
	resp, err := vsanmethods.VsanQueryVcClusterHealthSummary(ctx, vsanClient,
		&vsantypes.VsanQueryVcClusterHealthSummary{
			This:           healthSystemRef,
			Cluster:        &clusterRef.ref,
			Fields:         []string{"overallHealth", "overallHealthDescription"},
			FetchFromCache: &fetchFromCache,
		})
	if err != nil {
		return err
	}
	healthStr := resp.Returnval.OverallHealth
	healthMap := map[string]int{"red": 2, "yellow": 1, "green": 0}
	fields := make(map[string]interface{})
	if val, ok := healthMap[healthStr]; ok {
		fields["overall_health"] = val
	}
	tags := populateClusterTags(make(map[string]string), clusterRef, e.URL.Host)
	acc.AddFields(vsanSummaryMetricsName, fields, tags)
	return nil
}

// queryResyncSummary adds resync information to accumulator
func (e *Endpoint) queryResyncSummary(ctx context.Context, vsanClient *soap.Client, clusterObj *object.ClusterComputeResource,
	clusterRef *objectRef, acc telegraf.Accumulator) error {
	if lower := versionLowerThan(e.apiVersion, 6, 7); lower {
		e.Parent.Log.Infof("I! [inputs.vsphere][vSAN] Minimum API Version 6.7 required for resync summary. Found: %s. Skipping VCenter: %s",
			e.apiVersion, e.URL.Host)
		return nil
	}
	hosts, err := clusterObj.Hosts(ctx)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return nil
	}
	hostRefValue := hosts[0].Reference().Value
	hostRefValueParts := strings.Split(hostRefValue, "-")
	if len(hostRefValueParts) != 2 {
		e.Parent.Log.Errorf("[vSAN] Host reference value does not match expected pattern:  host-<num>. Actual Value %s", hostRefValue)
		return err
	}
	vsanSystemEx := types.ManagedObjectReference{
		Type:  "VsanSystemEx",
		Value: "vsanSystemEx-" + strings.Split(hostRefValue, "-")[1],
	}

	includeSummary := true
	request := vsantypes.VsanQuerySyncingVsanObjects{
		This:           vsanSystemEx,
		Uuids:          make([]string, 0), // We only need summary information.
		Start:          0,
		IncludeSummary: &includeSummary,
	}

	resp, err := vsanmethods.VsanQuerySyncingVsanObjects(ctx, vsanClient, &request)
	if err != nil {
		return err
	}
	fields := make(map[string]interface{})
	fields["total_bytes_to_sync"] = resp.Returnval.TotalBytesToSync
	fields["total_objects_to_sync"] = resp.Returnval.TotalObjectsToSync
	fields["total_recovery_eta"] = resp.Returnval.TotalRecoveryETA
	tags := populateClusterTags(make(map[string]string), clusterRef, e.URL.Host)
	acc.AddFields(vsanSummaryMetricsName, fields, tags)
	return nil
}

// populateClusterTags takes in a tag map, makes a copy, populates cluster related tags and returns the copy.
func populateClusterTags(tags map[string]string, clusterRef *objectRef, vcenter string) map[string]string {
	newTags := make(map[string]string)
	// deep copy
	for k, v := range tags {
		newTags[k] = v
	}
	newTags["vcenter"] = vcenter
	newTags["dcname"] = clusterRef.dcname
	newTags["clustername"] = clusterRef.name
	newTags["moid"] = clusterRef.ref.Value
	newTags["source"] = clusterRef.name
	return newTags
}

// populateCMMDSTags takes in a tag map, makes a copy, adds more tags using a cmmds map and returns the copy.
func populateCMMDSTags(tags map[string]string, entityName, uuid string, cmmds map[string]CmmdsEntity) map[string]string {
	newTags := make(map[string]string)
	// deep copy
	for k, v := range tags {
		newTags[k] = v
	}
	// There are cases when the uuid is missing. (Usually happens when performance service is just enabled or disabled)
	// We need this check to avoid index-out-of-range error
	if uuid == "*" || uuid == "" {
		return newTags
	}
	// Add additional tags based on CMMDS data
	switch {
	case strings.Contains(entityName, "-disk") || strings.Contains(entityName, "disk-"):
		if e, ok := cmmds[uuid]; ok {
			if host, ok := cmmds[e.Owner]; ok {
				newTags["hostname"] = host.Content.Hostname
			}
			newTags["devicename"] = e.Content.DevName
			// Skip adding ssduuid tag for VSAN ESA disks as this property is not returned in the CMMDS data
			if !(strings.Contains(entityName, "-esa-")) {
				if int(e.Content.IsSsd) == 0 {
					newTags["ssduuid"] = e.Content.SsdUUID
				}
			}
		}
	case strings.Contains(entityName, "host-memory-"):
		memInfo := strings.Split(uuid, "|")
		if strings.Contains(entityName, "-slab") && len(memInfo) > 1 {
			newTags["slabname"] = memInfo[1]
		}
		if strings.Contains(entityName, "-heap") && len(memInfo) > 1 {
			newTags["heapname"] = memInfo[1]
		}
		if e, ok := cmmds[memInfo[0]]; ok {
			newTags["hostname"] = e.Content.Hostname
		}
	case strings.Contains(entityName, "host-") || strings.Contains(entityName, "system-mem"):
		if e, ok := cmmds[uuid]; ok {
			newTags["hostname"] = e.Content.Hostname
		}
	case strings.Contains(entityName, "vnic-net"):
		nicInfo := strings.Split(uuid, "|")
		if len(nicInfo) > 2 {
			newTags["stackname"] = nicInfo[1]
			newTags["vnic"] = nicInfo[2]
		}
		if e, ok := cmmds[nicInfo[0]]; ok {
			newTags["hostname"] = e.Content.Hostname
		}
	case strings.Contains(entityName, "pnic-net"):
		nicInfo := strings.Split(uuid, "|")
		if len(nicInfo) > 1 {
			newTags["pnic"] = nicInfo[1]
		}
		if e, ok := cmmds[nicInfo[0]]; ok {
			newTags["hostname"] = e.Content.Hostname
		}
	case strings.Contains(entityName, "world-cpu"):
		cpuInfo := strings.Split(uuid, "|")
		if len(cpuInfo) > 1 {
			newTags["worldname"] = cpuInfo[1]
		}
		if e, ok := cmmds[cpuInfo[0]]; ok {
			newTags["hostname"] = e.Content.Hostname
		}
	default:
		// If no tags are added in previous steps, we add uuid for it
		if len(newTags) == len(tags) {
			newTags["uuid"] = uuid
		}
	}
	return newTags
}

// versionLowerThan returns true is the current version < a base version
func versionLowerThan(current string, major, minor int) bool {
	version := strings.Split(current, ".")
	currentMajor, err := strconv.Atoi(version[0])
	if err != nil {
		return false
	}

	if currentMajor > major {
		return false
	}
	if currentMajor == major {
		if len(version) < 2 {
			return true
		}
		currentMinor, err := strconv.Atoi(version[1])
		if err != nil {
			return true
		}
		if currentMinor >= minor {
			return false
		}
	}

	return true
}

type CmmdsEntity struct {
	UUID    string       `json:"uuid"`
	Owner   string       `json:"owner"` // ESXi UUID
	Type    string       `json:"type"`
	Content CmmdsContent `json:"content"`
}

type Cmmds struct {
	Res []CmmdsEntity `json:"result"`
}

type CmmdsContent struct {
	Hostname string  `json:"hostname"`
	IsSsd    float64 `json:"isSsd"`
	SsdUUID  string  `json:"ssdUuid"`
	DevName  string  `json:"devName"`
}
