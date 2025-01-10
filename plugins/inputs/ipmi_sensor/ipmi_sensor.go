//go:generate ../../../tools/readme_config_includer/generator
package ipmi_sensor

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	execCommand          = exec.Command // execCommand is used to mock commands in tests.
	reV1ParseLine        = regexp.MustCompile(`^(?P<name>[^|]*)\|(?P<description>[^|]*)\|(?P<status_code>.*)`)
	reV2ParseLine        = regexp.MustCompile(`^(?P<name>[^|]*)\|[^|]+\|(?P<status_code>[^|]*)\|(?P<entity_id>[^|]*)\|(?:(?P<description>[^|]+))?`)
	reV2ParseDescription = regexp.MustCompile(`^(?P<analogValue>-?[0-9.]+)\s(?P<analogUnit>.*)|(?P<status>.+)|^$`)
	reV2ParseUnit        = regexp.MustCompile(`^(?P<realAnalogUnit>[^,]+)(?:,\s*(?P<statusDesc>.*))?`)
	dcmiPowerReading     = regexp.MustCompile(`^(?P<name>[^|]*)\:(?P<value>.* Watts)?`)
)

const cmd = "ipmitool"

type Ipmi struct {
	Path          string          `toml:"path"`
	Privilege     string          `toml:"privilege"`
	HexKey        string          `toml:"hex_key"`
	Servers       []string        `toml:"servers"`
	Sensors       []string        `toml:"sensors"`
	Timeout       config.Duration `toml:"timeout"`
	MetricVersion int             `toml:"metric_version"`
	UseSudo       bool            `toml:"use_sudo"`
	UseCache      bool            `toml:"use_cache"`
	CachePath     string          `toml:"cache_path"`
	Log           telegraf.Logger `toml:"-"`
}

func (*Ipmi) SampleConfig() string {
	return sampleConfig
}

func (m *Ipmi) Init() error {
	// Set defaults
	if m.Path == "" {
		path, err := exec.LookPath(cmd)
		if err != nil {
			return fmt.Errorf("looking up %q failed: %w", cmd, err)
		}
		m.Path = path
	}
	if m.CachePath == "" {
		m.CachePath = os.TempDir()
	}
	if len(m.Sensors) == 0 {
		m.Sensors = []string{"sdr"}
	}
	if err := choice.CheckSlice(m.Sensors, []string{"sdr", "chassis_power_status", "dcmi_power_reading"}); err != nil {
		return err
	}

	// Check parameters
	if m.Path == "" {
		return fmt.Errorf("no path for %q specified", cmd)
	}

	return nil
}

func (m *Ipmi) Gather(acc telegraf.Accumulator) error {
	if len(m.Path) == 0 {
		return errors.New("ipmitool not found: verify that ipmitool is installed and that ipmitool is in your PATH")
	}

	if len(m.Servers) > 0 {
		wg := sync.WaitGroup{}
		for _, server := range m.Servers {
			wg.Add(1)
			go func(a telegraf.Accumulator, s string) {
				defer wg.Done()
				for _, sensor := range m.Sensors {
					a.AddError(m.parse(a, s, sensor))
				}
			}(acc, server)
		}
		wg.Wait()
	} else {
		for _, sensor := range m.Sensors {
			err := m.parse(acc, "", sensor)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *Ipmi) parse(acc telegraf.Accumulator, server, sensor string) error {
	var command []string
	switch sensor {
	case "sdr":
		command = append(command, "sdr")
	case "chassis_power_status":
		command = append(command, "chassis", "power", "status")
	case "dcmi_power_reading":
		command = append(command, "dcmi", "power", "reading")
	default:
		return fmt.Errorf("unknown sensor type %q", sensor)
	}

	opts := make([]string, 0)
	hostname := ""
	if server != "" {
		conn := newConnection(server, m.Privilege, m.HexKey)
		hostname = conn.hostname
		opts = conn.options()
	}

	opts = append(opts, command...)

	if m.UseCache {
		cacheFile := filepath.Join(m.CachePath, server+"_ipmi_cache")
		_, err := os.Stat(cacheFile)
		if os.IsNotExist(err) {
			dumpOpts := opts
			// init cache file
			dumpOpts = append(dumpOpts, "dump", cacheFile)
			name := m.Path
			if m.UseSudo {
				// -n - avoid prompting the user for input of any kind
				dumpOpts = append([]string{"-n", name}, dumpOpts...)
				name = "sudo"
			}
			cmd := execCommand(name, dumpOpts...)
			out, err := internal.CombinedOutputTimeout(cmd, time.Duration(m.Timeout))
			if err != nil {
				return fmt.Errorf("failed to run command %q: %w - %s", strings.Join(sanitizeIPMICmd(cmd.Args), " "), err, string(out))
			}
		}
		opts = append(opts, "-S", cacheFile)
	}
	if m.MetricVersion == 2 && sensor == "sdr" {
		opts = append(opts, "elist")
	}
	name := m.Path
	if m.UseSudo {
		// -n - avoid prompting the user for input of any kind
		opts = append([]string{"-n", name}, opts...)
		name = "sudo"
	}
	cmd := execCommand(name, opts...)
	out, err := internal.CombinedOutputTimeout(cmd, time.Duration(m.Timeout))
	timestamp := time.Now()
	if err != nil {
		return fmt.Errorf("failed to run command %q: %w - %s", strings.Join(sanitizeIPMICmd(cmd.Args), " "), err, string(out))
	}

	switch sensor {
	case "sdr":
		if m.MetricVersion == 2 {
			return m.parseV2(acc, hostname, out, timestamp)
		} else {
			return m.parseV1(acc, hostname, out, timestamp)
		}
	case "chassis_power_status":
		return parseChassisPowerStatus(acc, hostname, out, timestamp)
	case "dcmi_power_reading":
		return m.parseDCMIPowerReading(acc, hostname, out, timestamp)
	}

	return fmt.Errorf("unknown sensor type %q", sensor)
}

func parseChassisPowerStatus(acc telegraf.Accumulator, hostname string, cmdOut []byte, measuredAt time.Time) error {
	// each line will look something like
	// Chassis Power is on
	// Chassis Power is off
	scanner := bufio.NewScanner(bytes.NewReader(cmdOut))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Chassis Power is on") {
			acc.AddFields("ipmi_sensor", map[string]interface{}{"value": 1}, map[string]string{"name": "chassis_power_status", "server": hostname}, measuredAt)
		} else if strings.Contains(line, "Chassis Power is off") {
			acc.AddFields("ipmi_sensor", map[string]interface{}{"value": 0}, map[string]string{"name": "chassis_power_status", "server": hostname}, measuredAt)
		}
	}

	return scanner.Err()
}

func (m *Ipmi) parseDCMIPowerReading(acc telegraf.Accumulator, hostname string, cmdOut []byte, measuredAt time.Time) error {
	// each line will look something like
	// Current Power Reading : 0.000
	scanner := bufio.NewScanner(bytes.NewReader(cmdOut))
	for scanner.Scan() {
		ipmiFields := m.extractFieldsFromRegex(dcmiPowerReading, scanner.Text())
		if len(ipmiFields) != 2 {
			continue
		}

		tags := map[string]string{
			"name": transform(ipmiFields["name"]),
		}

		// tag the server is we have one
		if hostname != "" {
			tags["server"] = hostname
		}

		fields := make(map[string]interface{})
		valunit := strings.Split(ipmiFields["value"], " ")
		if len(valunit) != 2 {
			continue
		}

		var err error
		fields["value"], err = aToFloat(valunit[0])
		if err != nil {
			continue
		}
		if len(valunit) > 1 {
			tags["unit"] = transform(valunit[1])
		}

		acc.AddFields("ipmi_sensor", fields, tags, measuredAt)
	}

	return scanner.Err()
}

func (m *Ipmi) parseV1(acc telegraf.Accumulator, hostname string, cmdOut []byte, measuredAt time.Time) error {
	// each line will look something like
	// Planar VBAT      | 3.05 Volts        | ok
	scanner := bufio.NewScanner(bytes.NewReader(cmdOut))
	for scanner.Scan() {
		ipmiFields := m.extractFieldsFromRegex(reV1ParseLine, scanner.Text())
		if len(ipmiFields) != 3 {
			continue
		}

		tags := map[string]string{
			"name": transform(ipmiFields["name"]),
		}

		// tag the server is we have one
		if hostname != "" {
			tags["server"] = hostname
		}

		fields := make(map[string]interface{})
		if strings.EqualFold("ok", trim(ipmiFields["status_code"])) {
			fields["status"] = 1
		} else {
			fields["status"] = 0
		}

		description := ipmiFields["description"]

		// handle hex description field
		if strings.HasPrefix(description, "0x") {
			descriptionInt, err := strconv.ParseInt(description, 0, 64)
			if err != nil {
				continue
			}

			fields["value"] = float64(descriptionInt)
		} else if strings.Index(description, " ") > 0 {
			// split middle column into value and unit
			valunit := strings.SplitN(description, " ", 2)
			var err error
			fields["value"], err = aToFloat(valunit[0])
			if err != nil {
				continue
			}
			if len(valunit) > 1 {
				tags["unit"] = transform(valunit[1])
			}
		} else {
			fields["value"] = 0.0
		}

		acc.AddFields("ipmi_sensor", fields, tags, measuredAt)
	}

	return scanner.Err()
}

func (m *Ipmi) parseV2(acc telegraf.Accumulator, hostname string, cmdOut []byte, measuredAt time.Time) error {
	// each line will look something like
	// CMOS Battery     | 65h | ok  |  7.1 |
	// Temp             | 0Eh | ok  |  3.1 | 55 degrees C
	// Drive 0          | A0h | ok  |  7.1 | Drive Present
	scanner := bufio.NewScanner(bytes.NewReader(cmdOut))
	for scanner.Scan() {
		ipmiFields := m.extractFieldsFromRegex(reV2ParseLine, scanner.Text())
		if len(ipmiFields) < 3 || len(ipmiFields) > 4 {
			continue
		}

		tags := map[string]string{
			"name": transform(ipmiFields["name"]),
		}

		// tag the server is we have one
		if hostname != "" {
			tags["server"] = hostname
		}
		tags["entity_id"] = transform(ipmiFields["entity_id"])
		tags["status_code"] = trim(ipmiFields["status_code"])
		fields := make(map[string]interface{})
		descriptionResults := m.extractFieldsFromRegex(reV2ParseDescription, trim(ipmiFields["description"]))
		// This is an analog value with a unit
		if descriptionResults["analogValue"] != "" && len(descriptionResults["analogUnit"]) >= 1 {
			var err error
			fields["value"], err = aToFloat(descriptionResults["analogValue"])
			if err != nil {
				continue
			}
			// Some implementations add an extra status to their analog units
			unitResults := m.extractFieldsFromRegex(reV2ParseUnit, descriptionResults["analogUnit"])
			tags["unit"] = transform(unitResults["realAnalogUnit"])
			if unitResults["statusDesc"] != "" {
				tags["status_desc"] = transform(unitResults["statusDesc"])
			}
		} else {
			// This is a status value
			fields["value"] = 0.0
			// Extended status descriptions aren't required, in which case for consistency re-use the status code
			if descriptionResults["status"] != "" {
				tags["status_desc"] = transform(descriptionResults["status"])
			} else {
				tags["status_desc"] = transform(ipmiFields["status_code"])
			}
		}

		acc.AddFields("ipmi_sensor", fields, tags, measuredAt)
	}

	return scanner.Err()
}

// extractFieldsFromRegex consumes a regex with named capture groups and returns a kvp map of strings with the results
func (m *Ipmi) extractFieldsFromRegex(re *regexp.Regexp, input string) map[string]string {
	submatches := re.FindStringSubmatch(input)
	results := make(map[string]string)
	subexpNames := re.SubexpNames()
	if len(subexpNames) > len(submatches) {
		m.Log.Debugf("No matches found in %q", input)
		return results
	}
	for i, name := range subexpNames {
		if name != input && name != "" && input != "" {
			results[name] = trim(submatches[i])
		}
	}
	return results
}

// aToFloat converts string representations of numbers to float64 values
func aToFloat(val string) (float64, error) {
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0.0, err
	}
	return f, nil
}

func sanitizeIPMICmd(args []string) []string {
	for i, v := range args {
		if v == "-P" {
			args[i+1] = "REDACTED"
		}
	}

	return args
}

func trim(s string) string {
	return strings.TrimSpace(s)
}

func transform(s string) string {
	s = trim(s)
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, " ", "_")
}

func init() {
	inputs.Add("ipmi_sensor", func() telegraf.Input {
		return &Ipmi{Timeout: config.Duration(20 * time.Second)}
	})
}
