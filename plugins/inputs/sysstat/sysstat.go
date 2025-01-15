//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

package sysstat

import (
	"bufio"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	execCommand    = exec.Command // execCommand is used to mock commands in tests.
	dfltActivities = []string{"DISK"}
)

const (
	parseInterval = 1 // parseInterval is the interval (in seconds) where the parsing of the binary file takes place.
	cmd           = "sadf"
)

type Sysstat struct {
	// Sadc represents the path to the sadc collector utility.
	Sadc string `toml:"sadc_path"`

	// Force the execution time of sadc
	SadcInterval config.Duration `toml:"sadc_interval"`

	// Sadf represents the path to the sadf cmd.
	Sadf string `toml:"sadf_path"`

	// Activities is a list of activities that are passed as argument to the
	// collector utility (e.g: DISK, SNMP etc...)
	// The more activities that are added, the more data is collected.
	Activities []string `toml:"activities"`

	// Options is a map of options.
	//
	// The key represents the actual option that the Sadf command is called with and
	// the value represents the description for that option.
	//
	// For example, if you have the following options map:
	//    map[string]string{"-C": "cpu", "-d": "disk"}
	// The Sadf command is run with the options -C and -d to extract cpu and
	// disk metrics from the collected binary file.
	//
	// If Group is false (see below), each metric will be prefixed with the corresponding description
	// and represents itself a measurement.
	//
	// If Group is true, metrics are grouped to a single measurement with the corresponding description as name.
	Options map[string]string `toml:"options"`

	// Group determines if metrics are grouped or not.
	Group bool `toml:"group"`

	// DeviceTags adds the possibility to add additional tags for devices.
	DeviceTags map[string][]map[string]string `toml:"device_tags"`

	Log telegraf.Logger `toml:"-"`

	// Used to autodetect how long the sadc command should run for
	interval       int
	firstTimestamp time.Time
}

func (*Sysstat) SampleConfig() string {
	return sampleConfig
}

func (s *Sysstat) Init() error {
	// Set defaults
	if s.Sadf == "" {
		sadf, err := exec.LookPath(cmd)
		if err != nil {
			return fmt.Errorf("looking up %q failed: %w", cmd, err)
		}
		s.Sadf = sadf
	}

	if s.Sadf == "" {
		return fmt.Errorf("no path specified for %q", cmd)
	}

	return nil
}

func (s *Sysstat) Gather(acc telegraf.Accumulator) error {
	if time.Duration(s.SadcInterval) != 0 {
		// Collect interval is calculated as interval - parseInterval
		s.interval = int(time.Duration(s.SadcInterval).Seconds()) + parseInterval
	}

	if s.interval == 0 {
		if s.firstTimestamp.IsZero() {
			s.firstTimestamp = time.Now()
		} else {
			s.interval = int(time.Since(s.firstTimestamp).Seconds() + 0.5)
		}
	}

	tmpfile, err := os.CreateTemp("", "sysstat-*")
	if err != nil {
		return fmt.Errorf("failed to create tmp file: %w", err)
	}
	defer os.Remove(tmpfile.Name())
	defer tmpfile.Close()

	ts := time.Now().Add(time.Duration(s.interval) * time.Second)
	if err := s.collect(tmpfile.Name()); err != nil {
		return err
	}
	var wg sync.WaitGroup
	for option := range s.Options {
		wg.Add(1)
		go func(acc telegraf.Accumulator, option string) {
			defer wg.Done()
			acc.AddError(s.parse(acc, option, tmpfile.Name(), ts))
		}(acc, option)
	}
	wg.Wait()

	return nil
}

// collect collects sysstat data with the collector utility sadc.
// It runs the following command:
//
//	Sadc -S <Activity1> -S <Activity2> ... <collectInterval> 2 tmpFile
//
// The above command collects system metrics during <collectInterval> and
// saves it in binary form to tmpFile.
func (s *Sysstat) collect(tempfile string) error {
	options := make([]string, 0, len(s.Activities))
	for _, act := range s.Activities {
		options = append(options, "-S", act)
	}

	// collectInterval has to be smaller than the telegraf data collection interval
	collectInterval := s.interval - parseInterval

	// If true, interval is not defined yet and Gather is run for the first time.
	if collectInterval <= 0 {
		collectInterval = 1 // In that case we only collect for 1 second.
	}

	options = append(options, strconv.Itoa(collectInterval), "2", tempfile)
	cmd := execCommand(s.Sadc, options...)
	out, err := internal.CombinedOutputTimeout(cmd, time.Second*time.Duration(collectInterval+parseInterval))
	if err != nil {
		return fmt.Errorf("failed to run command %q: %w - %q", strings.Join(cmd.Args, " "), err, string(out))
	}
	return nil
}

func filterEnviron(env []string, prefix string) []string {
	newenv := env[:0]
	for _, envvar := range env {
		if !strings.HasPrefix(envvar, prefix) {
			newenv = append(newenv, envvar)
		}
	}
	return newenv
}

// Return the Cmd with its environment configured to use the C locale
func withCLocale(cmd *exec.Cmd) *exec.Cmd {
	var env []string
	if cmd.Env != nil {
		env = cmd.Env
	} else {
		env = os.Environ()
	}
	env = filterEnviron(env, "LANG")
	env = filterEnviron(env, "LC_")
	env = append(env, "LANG=C")
	cmd.Env = env
	return cmd
}

// parse runs Sadf on the previously saved tmpFile:
//
//	Sadf -p -- -p <option> tmpFile
//
// and parses the output to add it to the telegraf.Accumulator acc.
func (s *Sysstat) parse(acc telegraf.Accumulator, option, tmpfile string, ts time.Time) error {
	cmd := execCommand(s.Sadf, sadfOptions(option, tmpfile)...)
	cmd = withCLocale(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("running command %q failed: %w", strings.Join(cmd.Args, " "), err)
	}

	r := bufio.NewReader(stdout)
	csvReader := csv.NewReader(r)
	csvReader.Comma = '\t'
	csvReader.FieldsPerRecord = 6
	var measurement string
	// groupData to accumulate data when Group=true
	type groupData struct {
		tags   map[string]string
		fields map[string]interface{}
	}
	m := make(map[string]groupData)
	for {
		record, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		device := record[3]
		value, err := strconv.ParseFloat(record[5], 64)
		if err != nil {
			return err
		}

		tags := make(map[string]string)
		if device != "-" {
			tags["device"] = device
			if addTags, ok := s.DeviceTags[device]; ok {
				for _, tag := range addTags {
					for k, v := range tag {
						tags[k] = v
					}
				}
			}
		}

		if s.Group {
			measurement = s.Options[option]
			if _, ok := m[device]; !ok {
				m[device] = groupData{
					fields: make(map[string]interface{}),
					tags:   make(map[string]string),
				}
			}
			g := m[device]
			if len(g.tags) == 0 {
				for k, v := range tags {
					g.tags[k] = v
				}
			}
			g.fields[escape(record[4])] = value
		} else {
			measurement = s.Options[option] + "_" + escape(record[4])
			fields := map[string]interface{}{
				"value": value,
			}
			acc.AddFields(measurement, fields, tags, ts)
		}
	}
	if s.Group {
		for _, v := range m {
			acc.AddFields(measurement, v.fields, v.tags, ts)
		}
	}
	if err := internal.WaitTimeout(cmd, time.Second*5); err != nil {
		return fmt.Errorf("command %q failed with: %w", strings.Join(cmd.Args, " "), err)
	}
	return nil
}

// sadfOptions creates the correct options for the sadf utility.
func sadfOptions(activityOption, tmpfile string) []string {
	options := []string{
		"-p",
		"--",
		"-p",
	}

	opts := strings.Split(activityOption, " ")
	options = append(options, opts...)
	options = append(options, tmpfile)

	return options
}

// escape removes % and / chars in field names
func escape(dirty string) string {
	var fieldEscaper = strings.NewReplacer(
		`%%`, "pct_",
		`%`, "pct_",
		`/`, "_per_",
	)
	return fieldEscaper.Replace(dirty)
}

func init() {
	inputs.Add("sysstat", func() telegraf.Input {
		return &Sysstat{
			Group:      true,
			Activities: dfltActivities,
		}
	})
}
