package libvirt

import (
	"regexp"
	"strings"

	golibvirt "github.com/digitalocean/go-libvirt"

	"github.com/influxdata/telegraf"
)

var (
	cpuCacheMonitorRegexp            = regexp.MustCompile(`^cache\.monitor\..+?\.(name|vcpus|bank_count)$`)
	cpuCacheMonitorBankRegexp        = regexp.MustCompile(`^cache\.monitor\..+?\.bank\..+?\.(id|bytes)$`)
	memoryBandwidthMonitorRegexp     = regexp.MustCompile(`^bandwidth\.monitor\..+?\.(name|vcpus|node_count)$`)
	memoryBandwidthMonitorNodeRegexp = regexp.MustCompile(`^bandwidth\.monitor\..+?\.node\..+?\.(id|bytes_local|bytes_total)$`)
)

func (l *Libvirt) addMetrics(stats []golibvirt.DomainStatsRecord, vcpuInfos map[string][]vcpuAffinity, acc telegraf.Accumulator) {
	domainsMetrics := translateMetrics(stats)

	for domainName, metrics := range domainsMetrics {
		for metricType, values := range metrics {
			switch metricType {
			case "state":
				addStateMetrics(values, domainName, acc)
			case "cpu":
				addCPUMetrics(values, domainName, acc)
			case "balloon":
				addBalloonMetrics(values, domainName, acc)
			case "vcpu":
				l.addVcpuMetrics(values, domainName, vcpuInfos[domainName], acc)
			case "net":
				addInterfaceMetrics(values, domainName, acc)
			case "perf":
				addPerfMetrics(values, domainName, acc)
			case "block":
				addBlockMetrics(values, domainName, acc)
			case "iothread":
				addIothreadMetrics(values, domainName, acc)
			case "memory":
				addMemoryMetrics(values, domainName, acc)
			case "dirtyrate":
				addDirtyrateMetrics(values, domainName, acc)
			}
		}
	}

	if l.vcpuMappingEnabled {
		for domainName, vcpuInfo := range vcpuInfos {
			var tags = make(map[string]string)
			var fields = make(map[string]interface{})

			for _, vcpu := range vcpuInfo {
				tags["domain_name"] = domainName
				tags["vcpu_id"] = vcpu.vcpuID
				fields["cpu_id"] = vcpu.coresAffinity
				acc.AddFields("libvirt_cpu_affinity", fields, tags)
			}
		}
	}
}

func translateMetrics(stats []golibvirt.DomainStatsRecord) map[string]map[string]map[string]golibvirt.TypedParamValue {
	metrics := make(map[string]map[string]map[string]golibvirt.TypedParamValue)
	for _, stat := range stats {
		if stat.Params != nil {
			if metrics[stat.Dom.Name] == nil {
				metrics[stat.Dom.Name] = make(map[string]map[string]golibvirt.TypedParamValue)
			}

			for _, params := range stat.Params {
				statGroup := strings.Split(params.Field, ".")[0]
				if metrics[stat.Dom.Name][statGroup] == nil {
					metrics[stat.Dom.Name][statGroup] = make(map[string]golibvirt.TypedParamValue)
				}

				metrics[stat.Dom.Name][statGroup][strings.TrimPrefix(params.Field, statGroup+".")] = params.Value
			}
		}
	}

	return metrics
}

func addStateMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var stateFields = make(map[string]interface{})
	var stateTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		switch key {
		case "state", "reason":
			stateFields[key] = metric.I
		}
	}

	if len(stateFields) > 0 {
		acc.AddFields("libvirt_state", stateFields, stateTags)
	}
}

func addCPUMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var cpuFields = make(map[string]interface{})
	var cpuCacheMonitorTotalFields = make(map[string]interface{})

	var cpuCacheMonitorData = make(map[string]map[string]interface{})
	var cpuCacheMonitorBankData = make(map[string]map[string]map[string]interface{})

	var cpuTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		switch key {
		case "time", "user", "system":
			cpuFields[key] = metric.I
		case "haltpoll.success.time", "haltpoll.fail.time":
			cpuFields[strings.ReplaceAll(key, ".", "_")] = metric.I
		case "cache.monitor.count":
			cpuCacheMonitorTotalFields["count"] = metric.I
		default:
			if strings.Contains(key, "bank.count") {
				key = strings.ReplaceAll(key, "bank.count", "bank_count")
			}

			cpuStat := strings.Split(key, ".")
			if len(cpuStat) == 4 && cpuCacheMonitorRegexp.MatchString(key) {
				cacheMonitorID := cpuStat[2]
				cpuCacheMonitorFields, ok := cpuCacheMonitorData[cacheMonitorID]
				if !ok {
					cpuCacheMonitorFields = make(map[string]interface{})
					cpuCacheMonitorData[cacheMonitorID] = cpuCacheMonitorFields
				}

				cpuCacheMonitorFields[cpuStat[3]] = metric.I
			} else if len(cpuStat) == 6 && cpuCacheMonitorBankRegexp.MatchString(key) {
				cacheMonitorID := cpuStat[2]
				bankIndex := cpuStat[4]

				bankData, ok := cpuCacheMonitorBankData[cacheMonitorID]
				if !ok {
					bankData = make(map[string]map[string]interface{})
					cpuCacheMonitorBankData[cacheMonitorID] = bankData
				}

				bankFields, ok := cpuCacheMonitorBankData[cacheMonitorID][bankIndex]
				if !ok {
					bankFields = make(map[string]interface{})
					bankData[bankIndex] = bankFields
				}

				bankFields[cpuStat[5]] = metric.I
			}
		}
	}

	if len(cpuFields) > 0 {
		acc.AddFields("libvirt_cpu", cpuFields, cpuTags)
	}

	if len(cpuCacheMonitorTotalFields) > 0 {
		acc.AddFields("libvirt_cpu_cache_monitor_total", cpuCacheMonitorTotalFields, cpuTags)
	}

	for cpuID, cpuCacheMonitorFields := range cpuCacheMonitorData {
		if len(cpuCacheMonitorFields) > 0 {
			cpuCacheMonitorTags := map[string]string{
				"domain_name":      domainName,
				"cache_monitor_id": cpuID,
			}
			acc.AddFields("libvirt_cpu_cache_monitor", cpuCacheMonitorFields, cpuCacheMonitorTags)
		}
	}

	for cacheMonitorID, bankData := range cpuCacheMonitorBankData {
		for bankIndex, bankFields := range bankData {
			if len(bankFields) > 0 {
				bankTags := map[string]string{
					"domain_name":      domainName,
					"cache_monitor_id": cacheMonitorID,
					"bank_index":       bankIndex,
				}
				acc.AddFields("libvirt_cpu_cache_monitor_bank", bankFields, bankTags)
			}
		}
	}
}

func addBalloonMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var balloonFields = make(map[string]interface{})
	var balloonTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		switch key {
		case "current", "maximum", "swap_in", "swap_out", "major_fault", "minor_fault", "unused", "available",
			"rss", "usable", "disk_caches", "hugetlb_pgalloc", "hugetlb_pgfail":
			balloonFields[key] = metric.I
		case "last-update":
			balloonFields["last_update"] = metric.I
		}
	}

	if len(balloonFields) > 0 {
		acc.AddFields("libvirt_balloon", balloonFields, balloonTags)
	}
}

func (l *Libvirt) addVcpuMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, vcpuInfos []vcpuAffinity, acc telegraf.Accumulator) {
	var vcpuTotalFields = make(map[string]interface{})
	var vcpuData = make(map[string]map[string]interface{})

	var vcpuTotalTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		switch key {
		case "current", "maximum":
			vcpuTotalFields[key] = metric.I
		default:
			vcpuStat := strings.Split(key, ".")
			if len(vcpuStat) != 2 {
				continue
			}
			vcpuID := vcpuStat[0]
			fieldName := vcpuStat[1]
			vcpuFields, ok := vcpuData[vcpuID]
			if !ok {
				vcpuFields = make(map[string]interface{})
				vcpuData[vcpuID] = vcpuFields
			}

			switch fieldName {
			case "halted":
				haltedIntegerValue := 0
				if metric.I == "yes" {
					haltedIntegerValue = 1
				}

				vcpuFields["halted_i"] = haltedIntegerValue
				fallthrough
			case "state", "time", "wait", "delay":
				vcpuFields[fieldName] = metric.I
			}
		}
	}

	if len(vcpuTotalFields) > 0 {
		acc.AddFields("libvirt_vcpu_total", vcpuTotalFields, vcpuTotalTags)
	}

	for vcpuID, vcpuFields := range vcpuData {
		if len(vcpuFields) > 0 {
			vcpuTags := map[string]string{
				"domain_name": domainName,
				"vcpu_id":     vcpuID,
			}

			if pCPUID := l.getCurrentPCPUForVCPU(vcpuID, vcpuInfos); pCPUID >= 0 {
				vcpuFields["cpu_id"] = pCPUID
			}

			acc.AddFields("libvirt_vcpu", vcpuFields, vcpuTags)
		}
	}
}

func (l *Libvirt) getCurrentPCPUForVCPU(vcpuID string, vcpuInfos []vcpuAffinity) int32 {
	if !l.shouldGetCurrentPCPU() {
		return -1
	}

	for _, vcpuInfo := range vcpuInfos {
		if vcpuInfo.vcpuID == vcpuID {
			return vcpuInfo.currentPCPUID
		}
	}

	return -1
}

func addInterfaceMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var netTotalFields = make(map[string]interface{})
	var netData = make(map[string]map[string]interface{})

	var netTotalTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		if key == "count" {
			netTotalFields[key] = metric.I
		} else {
			netStat := strings.SplitN(key, ".", 2)
			if len(netStat) < 2 {
				continue
			}

			netID := netStat[0]
			netFields, ok := netData[netID]
			if !ok {
				netFields = make(map[string]interface{})
				netData[netID] = netFields
			}

			fieldName := strings.ReplaceAll(netStat[1], ".", "_")
			switch fieldName {
			case "name", "rx_bytes", "rx_pkts", "rx_errs", "rx_drop", "tx_bytes", "tx_pkts", "tx_errs", "tx_drop":
				netFields[fieldName] = metric.I
			}
		}
	}

	if len(netTotalFields) > 0 {
		acc.AddFields("libvirt_net_total", netTotalFields, netTotalTags)
	}

	for netID, netFields := range netData {
		if len(netFields) > 0 {
			netTags := map[string]string{
				"domain_name":  domainName,
				"interface_id": netID,
			}
			acc.AddFields("libvirt_net", netFields, netTags)
		}
	}
}

func addPerfMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var perfFields = make(map[string]interface{})
	var perfTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		switch key {
		case "cmt", "mbmt", "mbml", "cpu_cycles", "instructions", "cache_references", "cache_misses",
			"branch_instructions", "branch_misses", "bus_cycles", "stalled_cycles_frontend", "stalled_cycles_backend",
			"ref_cpu_cycles", "cpu_clock", "task_clock", "page_faults", "context_switches",
			"cpu_migrations", "page_faults_min", "page_faults_maj", "alignment_faults", "emulation_faults":
			perfFields[key] = metric.I
		}
	}

	if len(perfFields) > 0 {
		acc.AddFields("libvirt_perf", perfFields, perfTags)
	}
}

func addBlockMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var blockTotalFields = make(map[string]interface{})
	var blockData = make(map[string]map[string]interface{})

	var blockTotalTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		if key == "count" {
			blockTotalFields["count"] = metric.I
		} else {
			blockStat := strings.SplitN(key, ".", 2)
			if len(blockStat) < 2 {
				continue
			}

			blockID := blockStat[0]
			blockFields, ok := blockData[blockID]
			if !ok {
				blockFields = make(map[string]interface{})
				blockData[blockID] = blockFields
			}

			fieldName := strings.ReplaceAll(blockStat[1], ".", "_")
			switch fieldName {
			case "name", "backingIndex", "path", "rd_reqs", "rd_bytes", "rd_times", "wr_reqs", "wr_bytes", "wr_times",
				"fl_reqs", "fl_times", "errors", "allocation", "capacity", "physical", "threshold":
				blockFields[fieldName] = metric.I
			}
		}
	}

	if len(blockTotalFields) > 0 {
		acc.AddFields("libvirt_block_total", blockTotalFields, blockTotalTags)
	}

	for blockID, blockFields := range blockData {
		if len(blockFields) > 0 {
			blockTags := map[string]string{
				"domain_name": domainName,
				"block_id":    blockID,
			}
			acc.AddFields("libvirt_block", blockFields, blockTags)
		}
	}
}

func addIothreadMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var iothreadTotalFields = make(map[string]interface{})
	var iothreadData = make(map[string]map[string]interface{})

	var iothreadTotalTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		if key == "count" {
			iothreadTotalFields["count"] = metric.I
		} else {
			iothreadStat := strings.Split(key, ".")
			if len(iothreadStat) != 2 {
				continue
			}

			iothreadID := iothreadStat[0]
			iothreadFields, ok := iothreadData[iothreadID]
			if !ok {
				iothreadFields = make(map[string]interface{})
				iothreadData[iothreadID] = iothreadFields
			}

			fieldName := strings.ReplaceAll(iothreadStat[1], "-", "_")
			switch fieldName {
			case "poll_max_ns", "poll_grow", "poll_shrink":
				iothreadFields[fieldName] = metric.I
			}
		}
	}

	if len(iothreadTotalFields) > 0 {
		acc.AddFields("libvirt_iothread_total", iothreadTotalFields, iothreadTotalTags)
	}

	for iothreadID, iothreadFields := range iothreadData {
		if len(iothreadFields) > 0 {
			iothreadTags := map[string]string{
				"domain_name": domainName,
				"iothread_id": iothreadID,
			}
			acc.AddFields("libvirt_iothread", iothreadFields, iothreadTags)
		}
	}
}

func addMemoryMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var memoryBandwidthMonitorTotalFields = make(map[string]interface{})

	var memoryBandwidthMonitorData = make(map[string]map[string]interface{})
	var memoryBandwidthMonitorNodeData = make(map[string]map[string]map[string]interface{})

	var memoryBandwidthMonitorTotalTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		switch key {
		case "bandwidth.monitor.count":
			memoryBandwidthMonitorTotalFields["count"] = metric.I
		default:
			if strings.Contains(key, "node.count") {
				key = strings.ReplaceAll(key, "node.count", "node_count")
			} else if strings.Contains(key, "bytes.local") {
				key = strings.ReplaceAll(key, "bytes.local", "bytes_local")
			} else if strings.Contains(key, "bytes.total") {
				key = strings.ReplaceAll(key, "bytes.total", "bytes_total")
			}

			memoryStat := strings.Split(key, ".")
			if len(memoryStat) == 4 && memoryBandwidthMonitorRegexp.MatchString(key) {
				memoryBandwidthMonitorID := memoryStat[2]
				memoryBandwidthMonitorFields, ok := memoryBandwidthMonitorData[memoryBandwidthMonitorID]
				if !ok {
					memoryBandwidthMonitorFields = make(map[string]interface{})
					memoryBandwidthMonitorData[memoryBandwidthMonitorID] = memoryBandwidthMonitorFields
				}

				memoryBandwidthMonitorFields[memoryStat[3]] = metric.I
			} else if len(memoryStat) == 6 && memoryBandwidthMonitorNodeRegexp.MatchString(key) {
				memoryBandwidthMonitorID := memoryStat[2]
				controllerIndex := memoryStat[4]

				nodeData, ok := memoryBandwidthMonitorNodeData[memoryBandwidthMonitorID]
				if !ok {
					nodeData = make(map[string]map[string]interface{})
					memoryBandwidthMonitorNodeData[memoryBandwidthMonitorID] = nodeData
				}

				nodeFields, ok := memoryBandwidthMonitorNodeData[memoryBandwidthMonitorID][controllerIndex]
				if !ok {
					nodeFields = make(map[string]interface{})
					nodeData[controllerIndex] = nodeFields
				}

				nodeFields[memoryStat[5]] = metric.I
			}
		}
	}

	if len(memoryBandwidthMonitorTotalFields) > 0 {
		acc.AddFields("libvirt_memory_bandwidth_monitor_total", memoryBandwidthMonitorTotalFields, memoryBandwidthMonitorTotalTags)
	}

	for memoryBandwidthMonitorID, memoryFields := range memoryBandwidthMonitorData {
		if len(memoryFields) > 0 {
			tags := map[string]string{
				"domain_name":                 domainName,
				"memory_bandwidth_monitor_id": memoryBandwidthMonitorID,
			}
			acc.AddFields("libvirt_memory_bandwidth_monitor", memoryFields, tags)
		}
	}

	for memoryBandwidthMonitorID, nodeData := range memoryBandwidthMonitorNodeData {
		for controllerIndex, nodeFields := range nodeData {
			if len(nodeFields) > 0 {
				tags := map[string]string{
					"domain_name":                 domainName,
					"memory_bandwidth_monitor_id": memoryBandwidthMonitorID,
					"controller_index":            controllerIndex,
				}
				acc.AddFields("libvirt_memory_bandwidth_monitor_node", nodeFields, tags)
			}
		}
	}
}

func addDirtyrateMetrics(metrics map[string]golibvirt.TypedParamValue, domainName string, acc telegraf.Accumulator) {
	var dirtyrateFields = make(map[string]interface{})
	var dirtyrateVcpuData = make(map[string]map[string]interface{})

	var dirtyrateTags = map[string]string{
		"domain_name": domainName,
	}

	for key, metric := range metrics {
		switch key {
		case "calc_status", "calc_start_time", "calc_period",
			"megabytes_per_second", "calc_mode":
			dirtyrateFields[key] = metric.I
		default:
			dirtyrateStat := strings.Split(key, ".")
			if len(dirtyrateStat) == 3 && dirtyrateStat[0] == "vcpu" && dirtyrateStat[2] == "megabytes_per_second" {
				vcpuID := dirtyrateStat[1]
				dirtyRateFields, ok := dirtyrateVcpuData[vcpuID]
				if !ok {
					dirtyRateFields = make(map[string]interface{})
					dirtyrateVcpuData[vcpuID] = dirtyRateFields
				}
				dirtyRateFields[dirtyrateStat[2]] = metric.I
			}
		}
	}

	if len(dirtyrateFields) > 0 {
		acc.AddFields("libvirt_dirtyrate", dirtyrateFields, dirtyrateTags)
	}

	for vcpuID, dirtyRateFields := range dirtyrateVcpuData {
		if len(dirtyRateFields) > 0 {
			dirtyRateTags := map[string]string{
				"domain_name": domainName,
				"vcpu_id":     vcpuID,
			}
			acc.AddFields("libvirt_dirtyrate_vcpu", dirtyRateFields, dirtyRateTags)
		}
	}
}
