//go:generate ../../../tools/readme_config_includer/generator
package supervisor

import (
	_ "embed"
	"fmt"
	"net/url"
	"strings"

	"github.com/kolo/xmlrpc"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Supervisor struct {
	Server     string          `toml:"url"`
	MetricsInc []string        `toml:"metrics_include"`
	MetricsExc []string        `toml:"metrics_exclude"`
	Log        telegraf.Logger `toml:"-"`

	rpcClient   *xmlrpc.Client
	fieldFilter filter.Filter
}

type processInfo struct {
	Name          string `xmlrpc:"name"`
	Group         string `xmlrpc:"group"`
	Description   string `xmlrpc:"description"`
	Start         int32  `xmlrpc:"start"`
	Stop          int32  `xmlrpc:"stop"`
	Now           int32  `xmlrpc:"now"`
	State         int16  `xmlrpc:"state"`
	Statename     string `xmlrpc:"statename"`
	StdoutLogfile string `xmlrpc:"stdout_logfile"`
	StderrLogfile string `xmlrpc:"stderr_logfile"`
	SpawnErr      string `xmlrpc:"spawnerr"`
	ExitStatus    int8   `xmlrpc:"exitstatus"`
	Pid           int32  `xmlrpc:"pid"`
}

type supervisorInfo struct {
	StateCode int8   `xmlrpc:"statecode"`
	StateName string `xmlrpc:"statename"`
	Ident     string
}

func (*Supervisor) SampleConfig() string {
	return sampleConfig
}

func (s *Supervisor) Init() error {
	// Using default server URL if none was specified in config
	if s.Server == "" {
		s.Server = "http://localhost:9001/RPC2"
	}
	var err error
	// Initializing XML-RPC client
	s.rpcClient, err = xmlrpc.NewClient(s.Server, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize XML-RPC client: %w", err)
	}
	// Setting filter for additional metrics
	s.fieldFilter, err = filter.NewIncludeExcludeFilter(s.MetricsInc, s.MetricsExc)
	if err != nil {
		return fmt.Errorf("metrics filter setup failed: %w", err)
	}
	return nil
}

func (s *Supervisor) Gather(acc telegraf.Accumulator) error {
	// API call to get information about all running processes
	var rawProcessData []processInfo
	err := s.rpcClient.Call("supervisor.getAllProcessInfo", nil, &rawProcessData)
	if err != nil {
		return fmt.Errorf("failed to get processes info: %w", err)
	}

	// API call to get information about instance status
	var status supervisorInfo
	err = s.rpcClient.Call("supervisor.getState", nil, &status)
	if err != nil {
		return fmt.Errorf("failed to get processes info: %w", err)
	}

	// API call to get identification string
	err = s.rpcClient.Call("supervisor.getIdentification", nil, &status.Ident)
	if err != nil {
		return fmt.Errorf("failed to get instance identification: %w", err)
	}

	// Iterating through array of structs with processes info and adding fields to accumulator
	for _, process := range rawProcessData {
		processTags, processFields, err := s.parseProcessData(process, status)
		if err != nil {
			acc.AddError(err)
			continue
		}
		acc.AddFields("supervisor_processes", processFields, processTags)
	}
	// Adding instance info fields to accumulator
	instanceTags, instanceFields, err := s.parseInstanceData(status)
	if err != nil {
		return fmt.Errorf("failed to parse instance data: %w", err)
	}
	acc.AddFields("supervisor_instance", instanceFields, instanceTags)
	return nil
}

func (s *Supervisor) parseProcessData(pInfo processInfo, status supervisorInfo) (map[string]string, map[string]interface{}, error) {
	tags := map[string]string{
		"process": pInfo.Name,
		"group":   pInfo.Group,
	}
	fields := map[string]interface{}{
		"uptime": pInfo.Now - pInfo.Start,
		"state":  pInfo.State,
	}
	if s.fieldFilter.Match("pid") {
		fields["pid"] = pInfo.Pid
	}
	if s.fieldFilter.Match("rc") {
		fields["exitCode"] = pInfo.ExitStatus
	}
	splittedURL, err := beautifyServerString(s.Server)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse server string: %w", err)
	}
	tags["id"] = status.Ident
	tags["source"] = splittedURL[0]
	tags["port"] = splittedURL[1]
	return tags, fields, nil
}

// Parsing of supervisor instance data
func (s *Supervisor) parseInstanceData(status supervisorInfo) (map[string]string, map[string]interface{}, error) {
	splittedURL, err := beautifyServerString(s.Server)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse server string: %w", err)
	}
	tags := map[string]string{
		"id":     status.Ident,
		"source": splittedURL[0],
		"port":   splittedURL[1],
	}
	fields := map[string]interface{}{
		"state": status.StateCode,
	}
	return tags, fields, nil
}

// Function to get only address and port from URL
func beautifyServerString(rawurl string) ([]string, error) {
	parsedURL, err := url.Parse(rawurl)
	splittedURL := strings.Split(parsedURL.Host, ":")
	if err != nil {
		return nil, err
	}
	if len(splittedURL) < 2 {
		if parsedURL.Scheme == "https" {
			splittedURL[1] = "443"
		} else {
			splittedURL[1] = "80"
		}
	}
	return splittedURL, nil
}

func init() {
	inputs.Add("supervisor", func() telegraf.Input {
		return &Supervisor{
			MetricsExc: []string{"pid", "rc"},
		}
	})
}
