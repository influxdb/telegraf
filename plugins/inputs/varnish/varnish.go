//go:build !windows
// +build !windows

package varnish

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

var (
	measurementNamespace = "varnish"
	defaultStats         = []string{"MAIN.cache_hit", "MAIN.cache_miss", "MAIN.uptime"}
	defaultStatBinary    = "/usr/bin/varnishstat"
	defaultAdmBinary     = "/usr/bin/varnishadm"
	defaultTimeout       = config.Duration(time.Second)

	//vcl name and backend restriction regexp [A-Za-z][A-Za-z0-9_-]*
	defaultRegexps = []*regexp.Regexp{
		//dynamic backends
		//VBE.VCL_xxxx_xxx_VOD_SHIELD_Vxxxxxxxxxxxxx_xxxxxxxxxxxxx.goto.000007c8.(xx.xx.xxx.xx).(http://xxxxxxx-xxxxx-xxxxx-xxxxxx-xx-xxxx-x-xxxx.xx-xx-xxxx-x.amazonaws.com:80).(ttl:5.000000).fail_eaddrnotavail
		regexp.MustCompile(`^VBE\.(?P<_vcl>[\w\-]*)\.goto\.[[:alnum:]]+\.\((?P<backend>\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\)\.\((?P<server>.*)\)\.\(ttl:\d*\.\d*.*\)`),

		//VBE.reload_20210622_153544_23757.default.unhealthy
		regexp.MustCompile(`^VBE\.(?P<_vcl>[\w\-]*)\.(?P<backend>[\w\-]*)\.([\w\-]*)`),

		//KVSTORE values
		regexp.MustCompile(`^KVSTORE\.(?P<id>[\w\-]*)\.(?P<_vcl>[\w\-]*)\.([\w\-]*)`),

		//XCNT.abc1234.XXX+_YYYY.cr.pass.val
		regexp.MustCompile(`^XCNT\.(?P<_vcl>[\w\-]*)(\.)*(?P<group>[\w\-.+]*)\.(?P<_field>[\w\-.+]*)\.val`),

		//generic metric like MSE_STORE.store-1-1.g_aio_running_bytes_write
		regexp.MustCompile(`([\w\-]*)\.(?P<id>[\w\-.]*)\.([\w\-]*)`),
	}
)

type runner func(cmdName string, useSudo bool, args []string, timeout config.Duration) (*bytes.Buffer, error)

// Varnish is used to store configuration values
type Varnish struct {
	Stats         []string
	Binary        string
	BinaryArgs    []string
	AdmBinary     string
	AdmBinaryArgs []string
	UseSudo       bool
	InstanceName  string
	Timeout       config.Duration
	Regexps       []string
	MetricVersion int

	filter          filter.Filter
	run             runner
	admRun          runner
	regexpsCompiled []*regexp.Regexp
}

var sampleConfig = `
  ## If running as a restricted user you can prepend sudo for additional access:
  #use_sudo = false

  ## The default location of the varnishstat binary can be overridden with:
  binary = "/usr/bin/varnishstat"

  ## Additional custom arguments for the varnishstat command
  # binary_args = ["-f", "MAIN.*"]
  
  ## The default location of the varnishadm binary can be overriden with:
  adm_binary = "/usr/bin/varnishadm"

  ## Custom arguments for the varnishadm command 
  # adm_binary_args = [""] 

  ## Metric version
  metric_version = 2

  ## Additional regexps to override builtin conversion varnish metric into telegraf metrics. 
  ## Regexp group "_vcl" is used for extracting VCL name. Metrics that contains not active VCL are skipped.  
  ## Regexp group "_field" overides field name. Other named regexp groups are used as tags.
  # regexps = ['XCNT\.(?P<_vcl>[\w\-]*)\.(?P<group>[\w\-.+]*)\.(?P<_field>[\w\-.+]*)\.val']

  ## By default, telegraf gather stats for 3 metric points.
  ## Setting stats will override the defaults shown below.
  ## Glob matching can be used, ie, stats = ["MAIN.*"]
  ## stats may also be set to ["*"], which will collect all stats
  stats = ["MAIN.cache_hit", "MAIN.cache_miss", "MAIN.uptime"]

  ## Optional name for the varnish instance (or working directory) to query
  ## Usually append after -n in varnish cli
  # instance_name = instanceName

  ## Timeout for varnishstat command
  # timeout = "1s"
`

func (s *Varnish) Description() string {
	return "A plugin to collect stats from Varnish HTTP Cache"
}

// SampleConfig displays configuration instructions
func (s *Varnish) SampleConfig() string {
	return sampleConfig
}

// Shell out to varnish cli and return the output
func varnishRunner(cmdName string, useSudo bool, cmdArgs []string, timeout config.Duration) (*bytes.Buffer, error) {
	cmd := exec.Command(cmdName, cmdArgs...)

	if useSudo {
		cmdArgs = append([]string{cmdName}, cmdArgs...)
		cmdArgs = append([]string{"-n"}, cmdArgs...)
		cmd = exec.Command("sudo", cmdArgs...)
	}

	var out bytes.Buffer
	cmd.Stdout = &out

	err := internal.RunTimeout(cmd, time.Duration(timeout))
	if err != nil {
		return &out, fmt.Errorf("error running %s %v - %s", cmdName, cmdArgs, err)
	}

	return &out, nil
}

// Gather collects the configured stats from varnish_stat and adds them to the
// Accumulator
//
// The prefix of each stat (eg MAIN, MEMPOOL, LCK, etc) will be used as a
// 'section' tag and all stats that share that prefix will be reported as fields
// with that tag
func (s *Varnish) Gather(acc telegraf.Accumulator) error {
	if s.filter == nil {
		var err error
		if len(s.Stats) == 0 {
			s.filter, err = filter.Compile(defaultStats)
		} else {
			// legacy support, change "all" -> "*":
			if s.Stats[0] == "all" {
				s.Stats[0] = "*"
			}
			s.filter, err = filter.Compile(s.Stats)
		}
		if err != nil {
			return err
		}
	}
	//Add custom regexpsCompiled
	var customRegexps []*regexp.Regexp
	for _, re := range s.Regexps {
		compiled, err := regexp.Compile(re)
		if err != nil {
			return fmt.Errorf("error parsing regexp: %s", err)
		}
		customRegexps = append(customRegexps, compiled)
	}
	s.regexpsCompiled = append(customRegexps, s.regexpsCompiled...)

	admArgs, statsArgs := s.prepareCmdArgs()
	var err error

	//run varnishadm to get active vcl
	var activeVcl = "boot"
	if s.admRun != nil {
		admOut, err := s.admRun(s.AdmBinary, s.UseSudo, admArgs, s.Timeout)
		if err != nil {
			return fmt.Errorf("error gathering metrics: %s", err)
		}
		activeVcl = getActiveVCL(admOut)
	}

	statOut, err := s.run(s.Binary, s.UseSudo, statsArgs, s.Timeout)
	if err != nil {
		return fmt.Errorf("error gathering metrics: %s", err)
	}

	if s.MetricVersion == 0 || s.MetricVersion == 1 {
		return s.processMetricsV1(activeVcl, acc, statOut)
	} else if s.MetricVersion == 2 {
		return s.processMetricsV2(activeVcl, acc, statOut)
	} else {
		return fmt.Errorf("unsupported metrics_version: %v", s.MetricVersion)
	}
}

// Prepare varnish cli tools arguments
func (s *Varnish) prepareCmdArgs() ([]string, []string) {
	//default varnishadm arguments
	admArgs := []string{"vcl.list"}

	//default varnish stats arguments
	statsArgs := []string{"-j"}
	if s.MetricVersion == 1 {
		statsArgs = []string{"-1"}
	}

	//add optional instance name
	if s.InstanceName != "" {
		statsArgs = append(statsArgs, []string{"-n", s.InstanceName}...)
		admArgs = append([]string{"-n", s.InstanceName}, admArgs...)
	}

	//override custom arguments
	if len(s.AdmBinaryArgs) > 0 {
		admArgs = s.AdmBinaryArgs
	}
	//override custom arguments
	if len(s.BinaryArgs) > 0 {
		statsArgs = s.BinaryArgs
	}
	return admArgs, statsArgs
}

func (s *Varnish) processMetricsV1(activeVcl string, acc telegraf.Accumulator, out *bytes.Buffer) error {
	sectionMap := make(map[string]map[string]interface{})
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		cols := strings.Fields(scanner.Text())
		if len(cols) < 2 {
			continue
		}
		if !strings.Contains(cols[0], ".") {
			continue
		}

		stat := cols[0]
		value := cols[1]

		if s.filter != nil && !s.filter.Match(stat) {
			continue
		}

		//skip not active vcls
		vMetric := parseMetricV2(stat)
		if vMetric.vclName != "" && activeVcl != "" && vMetric.vclName != activeVcl {
			continue
		}
		// strip vclName from metrics name
		if vMetric.vclName != "" {
			stat = strings.ReplaceAll(stat, vMetric.vclName+".", "")
		}

		parts := strings.SplitN(stat, ".", 2)
		section := parts[0]
		field := parts[1]

		// Init the section if necessary
		if _, ok := sectionMap[section]; !ok {
			sectionMap[section] = make(map[string]interface{})
		}

		var err error
		sectionMap[section][field], err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			acc.AddError(fmt.Errorf("expected a numeric value for %s = %v", stat, value))
		}
	}

	for section, fields := range sectionMap {
		tags := map[string]string{
			"section": section,
		}
		if len(fields) == 0 {
			continue
		}

		acc.AddFields("varnish", fields, tags)
	}
	return nil
}

// metrics version 2 - parsing json
func (s *Varnish) processMetricsV2(activeVcl string, acc telegraf.Accumulator, out *bytes.Buffer) error {
	rootJSON := make(map[string]interface{})
	dec := json.NewDecoder(out)
	dec.UseNumber()
	if err := dec.Decode(&rootJSON); err != nil {
		return err
	}
	countersJSON := getCountersJSON(rootJSON)
	timestamp := time.Now()
	for vFieldName, raw := range countersJSON {
		if vFieldName == "timestamp" {
			continue
		}

		if s.filter != nil && !s.filter.Match(vFieldName) {
			continue
		}

		data, ok := raw.(map[string]interface{})

		if !ok {
			acc.AddError(fmt.Errorf("unexpected data from json: %s: %#v", vFieldName, raw))
			continue
		}
		var (
			vValue interface{}
			vErr   error
		)

		flag := data["flag"]

		if value, ok := data["value"]; ok {
			if number, ok := value.(json.Number); ok {
				if flag == "b" {
					if vValue, vErr = strconv.ParseUint(number.String(), 10, 64); vErr != nil {
						vErr = fmt.Errorf("%s value uint64 error: %s", vFieldName, vErr)
					}
				} else if vValue, vErr = number.Int64(); vErr != nil {
					//try parse float
					if vValue, vErr = number.Float64(); vErr != nil {
						vErr = fmt.Errorf("stat %s value %v is not valid number: %s", vFieldName, value, vErr)
					}
				}
			} else {
				vValue = value
			}
		}

		if vErr != nil {
			acc.AddError(vErr)
			continue
		}

		vMetric := parseMetricV2(vFieldName)

		if vMetric.vclName != "" && activeVcl != "" && vMetric.vclName != activeVcl {
			//skip not active vcl
			continue
		}

		fields := make(map[string]interface{})
		fields[vMetric.fieldName] = vValue
		switch flag {
		case "c", "a":
			acc.AddCounter(vMetric.measurement, fields, vMetric.tags, timestamp)
		case "g":
			acc.AddGauge(vMetric.measurement, fields, vMetric.tags, timestamp)
		default:
			acc.AddGauge(vMetric.measurement, fields, vMetric.tags, timestamp)
		}
	}
	return nil
}

// Parse the output of "varnishadm vcl.list" and find active vcls
func getActiveVCL(reader io.Reader) string {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		words := strings.Fields(line)
		// filter "active" row vcl
		if len(words) >= 5 && "active" == words[0] {
			return words[4]
		}
	}
	return ""
}

// Gets the "counters" section from varnishstat json (there is change in schema structure in varnish 6.5+)
func getCountersJSON(rootJSON map[string]interface{}) map[string]interface{} {
	//version 1 contains "counters" wrapper
	if counters, exists := rootJSON["counters"]; exists {
		return counters.(map[string]interface{})
	}
	return rootJSON
}

// converts varnish metrics name into field and list of tags
func parseMetricV2(vName string) (vMetric vMetric) {
	vMetric.measurement = measurementNamespace + "_" + strings.Split(strings.ToLower(vName), ".")[0]
	vMetric.vclName = ""
	if strings.Count(vName, ".") == 0 {
		return vMetric
	}
	vMetric.fieldName = vName[strings.LastIndex(vName, ".")+1:]
	vMetric.tags = make(map[string]string)

	//parse vName using regexpsCompiled
	for _, re := range defaultRegexps {
		submatch := re.FindStringSubmatch(vName)
		if len(submatch) < 1 {
			continue
		}
		for _, sub := range re.SubexpNames() {
			if sub == "" {
				continue
			}
			val := submatch[re.SubexpIndex(sub)]
			if sub == "_vcl" {
				vMetric.vclName = val
			} else if sub == "_field" {
				vMetric.fieldName = val
			} else if val != "" {
				vMetric.tags[sub] = val
			}
		}
		break
	}
	return vMetric
}

type vMetric struct {
	measurement string
	fieldName   string
	tags        map[string]string
	vclName     string
}

func init() {
	inputs.Add("varnish", func() telegraf.Input {
		return &Varnish{
			run:             varnishRunner,
			admRun:          varnishRunner,
			regexpsCompiled: defaultRegexps,
			Stats:           defaultStats,
			Binary:          defaultStatBinary,
			AdmBinary:       defaultAdmBinary,
			MetricVersion:   1,
			UseSudo:         false,
			InstanceName:    "",
			Timeout:         defaultTimeout,
			Regexps:         []string{},
		}
	})
}
