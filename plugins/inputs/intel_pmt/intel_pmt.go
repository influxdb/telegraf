//go:generate ../../../tools/readme_config_includer/generator
//go:build linux && amd64

package intel_pmt

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/PaesslerAG/gval"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var hexToDecRegex = regexp.MustCompile(`0x[0-9a-fA-F]+`)

const (
	defaultPmtBasePath = "/sys/class/intel_pmt"
	pluginName         = "intel_pmt"
)

type IntelPMT struct {
	PmtSpec        string          `toml:"spec"`
	DatatypeFilter []string        `toml:"datatypes_enabled"`
	SampleFilter   []string        `toml:"samples_enabled"`
	Log            telegraf.Logger `toml:"-"`

	pmtBasePath            string
	reader                 sourceReader
	pmtTelemetryFiles      map[string]pmtFileInfo
	pmtMetadata            *pmt
	pmtAggregator          map[string]aggregator
	pmtAggregatorInterface map[string]aggregatorInterface
	pmtTransformations     map[string]map[string]transformation
}

type pmtFileInfo []fileInfo

type fileInfo struct {
	path     string
	numaNode string
	pciBdf   string // PCI Bus:Device.Function (BDF)
}

func (*IntelPMT) SampleConfig() string {
	return sampleConfig
}

func (p *IntelPMT) Init() error {
	err := p.checkPmtSpec()
	if err != nil {
		return err
	}

	err = p.explorePmtInSysfs()
	if err != nil {
		return fmt.Errorf("error while exploring pmt sysfs: %w", err)
	}

	return p.parseXMLs()
}

func (p *IntelPMT) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	var hasError atomic.Bool
	for guid := range p.pmtTelemetryFiles {
		wg.Add(1)
		go func(guid string, fileInfo []fileInfo) {
			defer wg.Done()
			for _, info := range fileInfo {
				data, err := os.ReadFile(info.path)
				if err != nil {
					hasError.Store(true)
					acc.AddError(fmt.Errorf("gathering metrics failed: %w", err))
					return
				}

				err = p.aggregateSamples(acc, guid, data, info.numaNode, info.pciBdf)
				if err != nil {
					hasError.Store(true)
					acc.AddError(fmt.Errorf("gathering metrics failed: %w", err))
					return
				}
			}
		}(guid, p.pmtTelemetryFiles[guid])
	}
	wg.Wait()

	if hasError.Load() {
		return errors.New("error(s) occurred while gathering metrics")
	}
	return nil
}

// checkPmtSpec checks if provided PmtSpec is correct and readable.
//
// PmtSpec is expected to be an absolute filepath.
//
// Returns:
//
//	error - error if PmtSpec is invalid, not readable, or not absolute.
func (p *IntelPMT) checkPmtSpec() error {
	if p.PmtSpec == "" {
		return errors.New("pmt spec is empty")
	}

	if !isFileReadable(p.PmtSpec) {
		return fmt.Errorf("provided pmt spec is not readable %q", p.PmtSpec)
	}

	lastSlash := strings.LastIndex(p.PmtSpec, "/")
	// if PmtSpec contains no "/"
	if lastSlash == -1 {
		return errors.New("provided pmt spec is not an absolute path")
	}
	p.pmtBasePath = p.PmtSpec[:lastSlash]
	p.reader = fileReader{}

	return nil
}

// explorePmtInSysfs finds necessary paths in pmt sysfs.
//
// This method finds "telem" files, used to retrieve telemetry values
// and saves them under their corresponding GUID.
// It also finds which NUMA node and PCI BDF the samples belong to.
//
// Returns:
//
//	error - error if any of the operations failed.
func (p *IntelPMT) explorePmtInSysfs() error {
	pmtDirectories, err := os.ReadDir(defaultPmtBasePath)
	if err != nil {
		return fmt.Errorf("error reading pmt directory: %w", err)
	}
	p.pmtTelemetryFiles = make(map[string]pmtFileInfo)
	for _, dir := range pmtDirectories {
		if !strings.HasPrefix(dir.Name(), "telem") {
			continue
		}
		telemDirPath := filepath.Join(defaultPmtBasePath, dir.Name())
		symlinkInfo, err := os.Stat(telemDirPath)
		if err != nil {
			return fmt.Errorf("error resolving symlink for directory %q: %w", telemDirPath, err)
		}
		if !symlinkInfo.IsDir() {
			continue
		}

		pmtGUIDPath := filepath.Join(telemDirPath, "guid")
		rawGUID, err := os.ReadFile(pmtGUIDPath)
		if err != nil {
			return fmt.Errorf("cannot read GUID: %w", err)
		}
		// cut the newline char
		tID := strings.TrimSpace(string(rawGUID))

		telemPath := filepath.Join(telemDirPath, "telem")
		if !isFileReadable(telemPath) {
			p.Log.Warnf("telem file is not readable %q", telemPath)
			continue
		}

		telemDevicePath := filepath.Join(telemDirPath, "device")
		telemDeviceSymlink, err := filepath.EvalSymlinks(telemDevicePath)
		if err != nil {
			return fmt.Errorf("error while evaluating symlink %q: %w", telemDeviceSymlink, err)
		}

		telemDevicePciBdf := filepath.Base(filepath.Join(telemDeviceSymlink, ".."))

		numaNodePath := filepath.Join(telemDeviceSymlink, "..", "numa_node")

		numaNode, err := os.ReadFile(numaNodePath)
		if err != nil {
			return fmt.Errorf("error while reading numa_node file %q: %w", numaNodePath, err)
		}
		numaNodeString := strings.TrimSpace(string(numaNode))
		if numaNodeString == "" {
			return fmt.Errorf("numa_node file %q is empty", numaNodePath)
		}

		fi := fileInfo{
			path:     telemPath,
			numaNode: numaNodeString,
			pciBdf:   telemDevicePciBdf,
		}
		p.pmtTelemetryFiles[tID] = append(p.pmtTelemetryFiles[tID], fi)
	}
	if len(p.pmtTelemetryFiles) == 0 {
		return errors.New("no telemetry sources found - current platform doesn't support PMT or proper permissions needed to read them")
	}
	return nil
}

func isFileReadable(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}

	file, err := os.Open(path)
	if err != nil {
		return false
	}
	file.Close()

	return true
}

// getSampleValues reads all sample values for all sample groups.
//
// This method reads all telemetry samples for given GUID from given data
// and saves it in results map.
//
// Parameters:
//
//	guid - GUID saying which Aggregator XML will be read.
//	data - data read from "telem" file.
//
// Returns:
//
//	map[string]uint64 - results map with read data.
//	error - error if getting any of the values failed.
func (p *IntelPMT) getSampleValues(guid string, data []byte) (map[string]uint64, error) {
	results := make(map[string]uint64)
	for _, group := range p.pmtAggregator[guid].SampleGroup {
		// Determine starting position of the Sample Group.
		// Each Sample Group occupies 8 bytes.
		offset := 8 * group.SampleID
		for _, sample := range group.Sample {
			var err error
			results[sample.SampleID], err = getTelemSample(sample, data, offset)
			if err != nil {
				return nil, err
			}
		}
	}
	return results, nil
}

// getTelemSample extracts a telemetry sample from a given buffer.
//
// This function uses offset as a starting position.
// Then it uses LSB and MSB from sample to determine which bits
// to read from the given buffer.
//
// Parameters:
//
//	s - sample from Aggregator XML containing LSB and MSB info.
//	buf - the byte buffer containing the telemetry data.
//	offset - the starting position (in bytes) in the buffer.
//
// Returns:
//
//	uint64 - the extracted sample as a 64-bit unsigned integer.
//	error - error if offset+8 exceeds the size of the buffer.
func getTelemSample(s sample, buf []byte, offset uint64) (uint64, error) {
	if len(buf) < int(offset+8) {
		return 0, fmt.Errorf("error reading telemetry sample: insufficient bytes from offset %d in buffer of size %d", offset, len(buf))
	}
	data := binary.LittleEndian.Uint64(buf[offset : offset+8])

	// Apply mask and shift right
	value := (data & s.mask) >> s.Lsb
	return value, nil
}

// aggregateSamples outputs transformed metrics to Telegraf.
//
// This method transforms low level samples
// into high-level samples with appropriate transformation equation.
// Then it creates fields and tags and adds them to Telegraf Accumulator.
//
// Parameters:
//
// guid - GUID saying which Aggregator Interface will be read.
// data - contents of the "telem" file.
// numaNode - which NUMA node this sample belongs to.
// pciBdf - PCI Bus:Device.Function (BDF) this sample belongs to.
// acc - Telegraf Accumulator.
//
// Returns:
//
//	error - error if getting values has failed, if sample IDref is missing or if equation evaluation has failed.
func (p *IntelPMT) aggregateSamples(acc telegraf.Accumulator, guid string, data []byte, numaNode, pciBdf string) error {
	results, err := p.getSampleValues(guid, data)
	if err != nil {
		return err
	}
	for _, sample := range p.pmtAggregatorInterface[guid].AggregatorSamples.AggregatorSample {
		parameters := make(map[string]interface{})
		for _, input := range sample.TransformInputs.TransformInput {
			if _, ok := results[input.SampleIDREF]; !ok {
				return fmt.Errorf("sample with IDREF %q has not been found", input.SampleIDREF)
			}
			parameters[input.VarName] = results[input.SampleIDREF]
		}
		eq := transformEquation(p.pmtTransformations[guid][sample.TransformREF].Transform)
		res, err := eval(eq, parameters)
		if err != nil {
			return fmt.Errorf("error during eval of sample %q: %w", sample.SampleName, err)
		}
		fields := map[string]interface{}{
			"value": res,
		}
		tags := map[string]string{
			"guid":           guid,
			"numa_node":      numaNode,
			"pci_bdf":        pciBdf,
			"sample_name":    sample.SampleName,
			"sample_group":   sample.SampleGroup,
			"datatype_idref": sample.DatatypeIDRef,
		}
		if sample.core != "" {
			tags["core"] = sample.core
		}
		if sample.cha != "" {
			tags["cha"] = sample.cha
		}

		acc.AddFields(pluginName, fields, tags)
	}
	return nil
}

// transformEquation changes the equation string to be ready for eval.
//
// This function removes "$" signs, which prefixes every parameter in equations.
// Then escapes special characters from XML
// like "&lt;" into "<", "&amp;" into "&" and "&gt;" into ">"
// so they can be used in evaluation.
//
// Parameters:
//
//	eq - string which should be transformed.
//
// Returns:
//
//	string - transformed string.
func transformEquation(eq string) string {
	withoutDollar := strings.ReplaceAll(eq, "$", "")
	decoded := html.UnescapeString(withoutDollar)
	return decoded
}

// eval calculates the value of given equation for given parameters.
//
// This function evaluates arbitrary equations with parameters.
// It substitutes the parameters in the equation with their values
// and calculates its value.
// Example: equation "a + b", with params: a: 2, b: 3.
// a and b will be substituted with their values so the equation becomes "2 + 3".
// If any of the parameters are missing then the equation is invalid and returns an error.
// Parameters:
//
//	eq - equation which should be calculated.
//	params - parameters to substitute in the equation.
//
// Returns:
//
//	interface - the value of calculation.
//	error - error if the equation is empty, if hex to dec conversion failed or if the equation is invalid.
func eval(eq string, params map[string]interface{}) (interface{}, error) {
	if eq == "" {
		return nil, errors.New("no transformation equation found")
	}
	// gval doesn't support hexadecimals
	eq = hexToDecRegex.ReplaceAllStringFunc(eq, hexToDec)
	if eq == "" {
		return nil, errors.New("error during hex to decimal conversion")
	}
	result, err := gval.Evaluate(eq, params)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func hexToDec(hexStr string) string {
	dec, err := strconv.ParseInt(hexStr, 0, 64)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(dec, 10)
}

func init() {
	inputs.Add(pluginName, func() telegraf.Input {
		return &IntelPMT{}
	})
}
