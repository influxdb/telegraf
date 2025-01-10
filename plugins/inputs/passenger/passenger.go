//go:generate ../../../tools/readme_config_includer/generator
package passenger

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/net/html/charset"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Passenger struct {
	Command string `toml:"command"`
}

type info struct {
	PassengerVersion string `xml:"passenger_version"`
	ProcessCount     int    `xml:"process_count"`
	CapacityUsed     int    `xml:"capacity_used"`
	GetWaitListSize  int    `xml:"get_wait_list_size"`
	Max              int    `xml:"max"`
	Supergroups      struct {
		Supergroup []struct {
			Name            string `xml:"name"`
			GetWaitListSize int    `xml:"get_wait_list_size"`
			CapacityUsed    int    `xml:"capacity_used"`
			Group           []struct {
				Name                  string `xml:"name"`
				AppRoot               string `xml:"app_root"`
				AppType               string `xml:"app_type"`
				EnabledProcessCount   int    `xml:"enabled_process_count"`
				DisablingProcessCount int    `xml:"disabling_process_count"`
				DisabledProcessCount  int    `xml:"disabled_process_count"`
				CapacityUsed          int    `xml:"capacity_used"`
				GetWaitListSize       int    `xml:"get_wait_list_size"`
				ProcessesBeingSpawned int    `xml:"processes_being_spawned"`
				Processes             struct {
					Process []*process `xml:"process"`
				} `xml:"processes"`
			} `xml:"group"`
		} `xml:"supergroup"`
	} `xml:"supergroups"`
}

type process struct {
	Pid                 int    `xml:"pid"`
	Concurrency         int    `xml:"concurrency"`
	Sessions            int    `xml:"sessions"`
	Busyness            int    `xml:"busyness"`
	Processed           int    `xml:"processed"`
	SpawnerCreationTime int64  `xml:"spawner_creation_time"`
	SpawnStartTime      int64  `xml:"spawn_start_time"`
	SpawnEndTime        int64  `xml:"spawn_end_time"`
	LastUsed            int64  `xml:"last_used"`
	Uptime              string `xml:"uptime"`
	CodeRevision        string `xml:"code_revision"`
	LifeStatus          string `xml:"life_status"`
	Enabled             string `xml:"enabled"`
	HasMetrics          bool   `xml:"has_metrics"`
	CPU                 int64  `xml:"cpu"`
	Rss                 int64  `xml:"rss"`
	Pss                 int64  `xml:"pss"`
	PrivateDirty        int64  `xml:"private_dirty"`
	Swap                int64  `xml:"swap"`
	RealMemory          int64  `xml:"real_memory"`
	Vmsize              int64  `xml:"vmsize"`
	ProcessGroupID      string `xml:"process_group_id"`
}

func (*Passenger) SampleConfig() string {
	return sampleConfig
}

func (p *Passenger) Gather(acc telegraf.Accumulator) error {
	if p.Command == "" {
		p.Command = "passenger-status -v --show=xml"
	}

	cmd, args := p.parseCommand()
	out, err := exec.Command(cmd, args...).Output()

	if err != nil {
		return err
	}

	return importMetric(out, acc)
}

func (p *Passenger) parseCommand() (string, []string) {
	var arguments []string
	if !strings.Contains(p.Command, " ") {
		return p.Command, arguments
	}

	arguments = strings.Split(p.Command, " ")
	if len(arguments) == 1 {
		return arguments[0], arguments[1:]
	}

	return arguments[0], arguments[1:]
}

func (p *process) getUptime() int64 {
	if p.Uptime == "" {
		return 0
	}

	timeSlice := strings.Split(p.Uptime, " ")
	var uptime int64
	uptime = 0
	for _, v := range timeSlice {
		switch {
		case strings.HasSuffix(v, "d"):
			iValue := strings.TrimSuffix(v, "d")
			value, err := strconv.ParseInt(iValue, 10, 64)
			if err == nil {
				uptime += value * (24 * 60 * 60)
			}
		case strings.HasSuffix(v, "h"):
			iValue := strings.TrimSuffix(v, "h")
			value, err := strconv.ParseInt(iValue, 10, 64)
			if err == nil {
				uptime += value * (60 * 60)
			}
		case strings.HasSuffix(v, "m"):
			iValue := strings.TrimSuffix(v, "m")
			value, err := strconv.ParseInt(iValue, 10, 64)
			if err == nil {
				uptime += value * 60
			}
		case strings.HasSuffix(v, "s"):
			iValue := strings.TrimSuffix(v, "s")
			value, err := strconv.ParseInt(iValue, 10, 64)
			if err == nil {
				uptime += value
			}
		}
	}

	return uptime
}

func importMetric(stat []byte, acc telegraf.Accumulator) error {
	var p info

	decoder := xml.NewDecoder(bytes.NewReader(stat))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&p); err != nil {
		return fmt.Errorf("cannot parse input with error: %w", err)
	}

	tags := map[string]string{
		"passenger_version": p.PassengerVersion,
	}
	fields := map[string]interface{}{
		"process_count":      p.ProcessCount,
		"max":                p.Max,
		"capacity_used":      p.CapacityUsed,
		"get_wait_list_size": p.GetWaitListSize,
	}
	acc.AddFields("passenger", fields, tags)

	for _, sg := range p.Supergroups.Supergroup {
		tags := map[string]string{
			"name": sg.Name,
		}
		fields := map[string]interface{}{
			"get_wait_list_size": sg.GetWaitListSize,
			"capacity_used":      sg.CapacityUsed,
		}
		acc.AddFields("passenger_supergroup", fields, tags)

		for _, group := range sg.Group {
			tags := map[string]string{
				"name":     group.Name,
				"app_root": group.AppRoot,
				"app_type": group.AppType,
			}
			fields := map[string]interface{}{
				"get_wait_list_size":      group.GetWaitListSize,
				"capacity_used":           group.CapacityUsed,
				"processes_being_spawned": group.ProcessesBeingSpawned,
			}
			acc.AddFields("passenger_group", fields, tags)

			for _, process := range group.Processes.Process {
				tags := map[string]string{
					"group_name":       group.Name,
					"app_root":         group.AppRoot,
					"supergroup_name":  sg.Name,
					"pid":              strconv.Itoa(process.Pid),
					"code_revision":    process.CodeRevision,
					"life_status":      process.LifeStatus,
					"process_group_id": process.ProcessGroupID,
				}
				fields := map[string]interface{}{
					"concurrency":           process.Concurrency,
					"sessions":              process.Sessions,
					"busyness":              process.Busyness,
					"processed":             process.Processed,
					"spawner_creation_time": process.SpawnerCreationTime,
					"spawn_start_time":      process.SpawnStartTime,
					"spawn_end_time":        process.SpawnEndTime,
					"last_used":             process.LastUsed,
					"uptime":                process.getUptime(),
					"cpu":                   process.CPU,
					"rss":                   process.Rss,
					"pss":                   process.Pss,
					"private_dirty":         process.PrivateDirty,
					"swap":                  process.Swap,
					"real_memory":           process.RealMemory,
					"vmsize":                process.Vmsize,
				}
				acc.AddFields("passenger_process", fields, tags)
			}
		}
	}

	return nil
}

func init() {
	inputs.Add("passenger", func() telegraf.Input {
		return &Passenger{}
	})
}
