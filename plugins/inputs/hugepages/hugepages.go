//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

package hugepages

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	newlineByte = []byte("\n")
	colonByte   = []byte(":")

	hugepagesMetricsRoot = map[string]string{
		"free_hugepages":          "free",
		"nr_hugepages":            "total",
		"nr_hugepages_mempolicy":  "mempolicy",
		"nr_overcommit_hugepages": "overcommit",
		"resv_hugepages":          "reserved",
		"surplus_hugepages":       "surplus",
	}

	hugepagesMetricsPerNUMANode = map[string]string{
		"free_hugepages":    "free",
		"nr_hugepages":      "total",
		"surplus_hugepages": "surplus",
	}

	hugepagesMetricsFromMeminfo = map[string]string{
		"HugePages_Total": "total",
		"HugePages_Free":  "free",
		"HugePages_Rsvd":  "reserved",
		"HugePages_Surp":  "surplus",
		"Hugepagesize":    "size_kb",
		"Hugetlb":         "tlb_kb",
		"AnonHugePages":   "anonymous_kb",
		"ShmemHugePages":  "shared_kb",
		"FileHugePages":   "file_kb",
	}
)

const (
	// path to root huge page control directory
	rootHugepagePath = "/sys/kernel/mm/hugepages"
	// path where per NUMA node statistics are kept
	numaNodePath = "/sys/devices/system/node"
	// path to the meminfo file
	meminfoPath = "/proc/meminfo"

	rootHugepages    = "root"
	perNodeHugepages = "per_node"
	meminfoHugepages = "meminfo"
)

type Hugepages struct {
	Types []string `toml:"types"`

	gatherRoot    bool
	gatherPerNode bool
	gatherMeminfo bool

	rootHugepagePath string
	numaNodePath     string
	meminfoPath      string
}

func (*Hugepages) SampleConfig() string {
	return sampleConfig
}

func (h *Hugepages) Init() error {
	err := h.parseHugepagesConfig()
	if err != nil {
		return err
	}

	h.rootHugepagePath = rootHugepagePath
	h.numaNodePath = numaNodePath
	h.meminfoPath = meminfoPath

	return nil
}

func (h *Hugepages) Gather(acc telegraf.Accumulator) error {
	if h.gatherRoot {
		if err := h.gatherRootStats(acc); err != nil {
			return fmt.Errorf("gathering root stats failed: %w", err)
		}
	}

	if h.gatherPerNode {
		if err := h.gatherStatsPerNode(acc); err != nil {
			return fmt.Errorf("gathering per node stats failed: %w", err)
		}
	}

	if h.gatherMeminfo {
		if err := h.gatherStatsFromMeminfo(acc); err != nil {
			return fmt.Errorf("gathering meminfo stats failed: %w", err)
		}
	}

	return nil
}

// gatherStatsPerNode collects root hugepages statistics
func (h *Hugepages) gatherRootStats(acc telegraf.Accumulator) error {
	return gatherFromHugepagePath(acc, "hugepages_"+rootHugepages, h.rootHugepagePath, hugepagesMetricsRoot, nil)
}

// gatherStatsPerNode collects hugepages statistics per NUMA node
func (h *Hugepages) gatherStatsPerNode(acc telegraf.Accumulator) error {
	nodeDirs, err := os.ReadDir(h.numaNodePath)
	if err != nil {
		return err
	}

	// read metrics from: node*/hugepages/hugepages-*/*
	for _, nodeDir := range nodeDirs {
		if !nodeDir.IsDir() || !strings.HasPrefix(nodeDir.Name(), "node") {
			continue
		}

		nodeNumber := strings.TrimPrefix(nodeDir.Name(), "node")
		_, err := strconv.Atoi(nodeNumber)
		if err != nil {
			continue
		}

		perNodeTags := map[string]string{
			"node": nodeNumber,
		}
		hugepagesPath := filepath.Join(h.numaNodePath, nodeDir.Name(), "hugepages")
		err = gatherFromHugepagePath(acc, "hugepages_"+perNodeHugepages, hugepagesPath, hugepagesMetricsPerNUMANode, perNodeTags)
		if err != nil {
			return err
		}
	}
	return nil
}

func gatherFromHugepagePath(acc telegraf.Accumulator, measurement, path string, fileFilter, defaultTags map[string]string) error {
	// read metrics from: hugepages/hugepages-*/*
	hugepagesDirs, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("reading root dir failed: %w", err)
	}

	for _, hugepagesDir := range hugepagesDirs {
		if !hugepagesDir.IsDir() || !strings.HasPrefix(hugepagesDir.Name(), "hugepages-") {
			continue
		}

		hugepagesSize := strings.TrimPrefix(strings.TrimSuffix(hugepagesDir.Name(), "kB"), "hugepages-")
		_, err := strconv.Atoi(hugepagesSize)
		if err != nil {
			continue
		}

		metricsPath := filepath.Join(path, hugepagesDir.Name())
		metricFiles, err := os.ReadDir(metricsPath)
		if err != nil {
			return fmt.Errorf("reading metric dir failed: %w", err)
		}

		metrics := make(map[string]interface{})
		for _, metricFile := range metricFiles {
			metricName, ok := fileFilter[metricFile.Name()]
			if mode := metricFile.Type(); !mode.IsRegular() || !ok {
				continue
			}

			metricFullPath := filepath.Join(metricsPath, metricFile.Name())
			metricBytes, err := os.ReadFile(metricFullPath)
			if err != nil {
				return err
			}

			metricValue, err := strconv.Atoi(string(bytes.TrimSuffix(metricBytes, newlineByte)))
			if err != nil {
				return fmt.Errorf("failed to convert content of %q: %w", metricFullPath, err)
			}

			metrics[metricName] = metricValue
		}

		if len(metrics) == 0 {
			continue
		}

		tags := make(map[string]string)
		for key, value := range defaultTags {
			tags[key] = value
		}
		tags["size_kb"] = hugepagesSize

		acc.AddFields(measurement, metrics, tags)
	}
	return nil
}

// gatherStatsFromMeminfo collects hugepages statistics from meminfo file
func (h *Hugepages) gatherStatsFromMeminfo(acc telegraf.Accumulator) error {
	meminfo, err := os.ReadFile(h.meminfoPath)
	if err != nil {
		return err
	}

	metrics := make(map[string]interface{})
	lines := bytes.Split(meminfo, newlineByte)
	for _, line := range lines {
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			continue
		}
		fieldName := string(bytes.TrimSuffix(fields[0], colonByte))
		metricName, ok := hugepagesMetricsFromMeminfo[fieldName]
		if !ok {
			continue
		}

		fieldValue, err := strconv.Atoi(string(fields[1]))
		if err != nil {
			return fmt.Errorf("failed to convert content of %q: %w", fieldName, err)
		}

		metrics[metricName] = fieldValue
	}

	acc.AddFields("hugepages_"+meminfoHugepages, metrics, make(map[string]string))
	return nil
}

func (h *Hugepages) parseHugepagesConfig() error {
	// default
	if h.Types == nil {
		h.gatherRoot = true
		h.gatherMeminfo = true
		return nil
	}

	// empty array
	if len(h.Types) == 0 {
		return errors.New("plugin was configured with nothing to read")
	}

	for _, hugepagesType := range h.Types {
		switch hugepagesType {
		case rootHugepages:
			h.gatherRoot = true
		case perNodeHugepages:
			h.gatherPerNode = true
		case meminfoHugepages:
			h.gatherMeminfo = true
		default:
			return fmt.Errorf("provided hugepages type %q is not valid", hugepagesType)
		}
	}

	return nil
}

func init() {
	inputs.Add("hugepages", func() telegraf.Input {
		return &Hugepages{}
	})
}
