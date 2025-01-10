//go:generate ../../../tools/readme_config_includer/generator
package upsd

import (
	_ "embed"
	"fmt"
	"strings"

	nut "github.com/robbiet480/go.nut"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// see: https://networkupstools.org/docs/developer-guide.chunked/index.html
const defaultAddress = "127.0.0.1"
const defaultPort = 3493

// Define the set of variables _always_ included in a metric
var mandatoryVariableSet = map[string]bool{
	"battery.date":     true,
	"battery.mfr.date": true,
	"battery.runtime":  true,
	"device.model":     true,
	"device.serial":    true,
	"ups.firmware":     true,
	"ups.status":       true,
}

// Define the default field set to add if existing
var defaultFieldSet = map[string]string{
	"battery.charge":          "battery_charge_percent",
	"battery.runtime.low":     "battery_runtime_low",
	"battery.voltage":         "battery_voltage",
	"input.frequency":         "input_frequency",
	"input.transfer.high":     "input_transfer_high",
	"input.transfer.low":      "input_transfer_low",
	"input.voltage":           "input_voltage",
	"ups.temperature":         "internal_temp",
	"ups.load":                "load_percent",
	"battery.voltage.nominal": "nominal_battery_voltage",
	"input.voltage.nominal":   "nominal_input_voltage",
	"ups.realpower.nominal":   "nominal_power",
	"output.voltage":          "output_voltage",
	"ups.realpower":           "real_power",
	"ups.delay.shutdown":      "ups_delay_shutdown",
	"ups.delay.start":         "ups_delay_start",
}

type Upsd struct {
	Server     string          `toml:"server"`
	Port       int             `toml:"port"`
	Username   string          `toml:"username"`
	Password   string          `toml:"password"`
	ForceFloat bool            `toml:"force_float"`
	Additional []string        `toml:"additional_fields"`
	DumpRaw    bool            `toml:"dump_raw_variables" deprecated:"1.35.0;use 'log_level' 'trace' instead"`
	Log        telegraf.Logger `toml:"-"`

	filter filter.Filter
	dumped map[string]bool
}

func (*Upsd) SampleConfig() string {
	return sampleConfig
}

func (u *Upsd) Init() error {
	// Compile the additional fields filter
	f, err := filter.Compile(u.Additional)
	if err != nil {
		return fmt.Errorf("compiling additional_fields filter failed: %w", err)
	}
	u.filter = f

	u.dumped = make(map[string]bool)

	return nil
}

func (u *Upsd) Gather(acc telegraf.Accumulator) error {
	upsList, err := u.fetchVariables(u.Server, u.Port)
	if err != nil {
		return err
	}
	if u.Log.Level().Includes(telegraf.Trace) || u.DumpRaw { // for backward compatibility
		for name, variables := range upsList {
			// Only dump the information once per UPS
			if u.dumped[name] {
				continue
			}
			values := make([]string, 0, len(variables))
			types := make([]string, 0, len(variables))
			for _, v := range variables {
				values = append(values, fmt.Sprintf("%s: %v", v.Name, v.Value))
				types = append(types, fmt.Sprintf("%s: %v", v.Name, v.OriginalType))
			}
			u.Log.Tracef("Variables dump for UPS %q:\n%s\n-----\n%s", name, strings.Join(values, "\n"), strings.Join(types, "\n"))
		}
	}
	for name, variables := range upsList {
		u.gatherUps(acc, name, variables)
	}
	return nil
}

func (u *Upsd) gatherUps(acc telegraf.Accumulator, upsname string, variables []nut.Variable) {
	metrics := make(map[string]interface{})
	for _, variable := range variables {
		name := variable.Name
		value := variable.Value
		metrics[name] = value
	}

	tags := map[string]string{
		"serial":   fmt.Sprintf("%v", metrics["device.serial"]),
		"ups_name": upsname,
		// "variables": variables.Status not sure if it's a good idea to provide this
		"model": fmt.Sprintf("%v", metrics["device.model"]),
	}

	// For compatibility with the apcupsd plugin's output we map the status string status into a bit-format
	status := mapStatus(metrics, tags)

	timeLeftS, err := internal.ToFloat64(metrics["battery.runtime"])
	if err != nil {
		u.Log.Warnf("Type for 'battery.runtime' is not supported: %v", err)
	}

	timeLeftNS, err := internal.ToInt64(timeLeftS * 1_000_000_000)
	if err != nil {
		u.Log.Warnf("Converting 'battery.runtime' to 'time_left_ns' failed: %v", err)
	}

	// Add the mandatory information
	fields := map[string]interface{}{
		"battery_date":     metrics["battery.date"],
		"battery_mfr_date": metrics["battery.mfr.date"],
		"status_flags":     status,
		"ups_status":       metrics["ups.status"],

		// for compatibility with apcupsd metrics format
		"time_left_ns": timeLeftNS,
	}

	// Define the set of mandatory string fields
	val, err := internal.ToString(metrics["ups.firmware"])
	if err != nil {
		acc.AddError(fmt.Errorf("converting ups.firmware=%q failed: %w", metrics["ups.firmware"], err))
	} else {
		fields["firmware"] = val
	}

	// Try to gather all default fields and optional field
	for varname, v := range metrics {
		// Skip all empty fields and all fields contained in the mandatory set
		// of fields added above.
		if v == nil || mandatoryVariableSet[varname] {
			continue
		}

		// Use the name of the default field-set if present and otherwise check
		// the additional field-set. If none of them contains the variable, we
		// skip over it
		var key string
		if k, found := defaultFieldSet[varname]; found {
			key = k
		} else if u.filter != nil && u.filter.Match(varname) {
			key = strings.ReplaceAll(varname, ".", "_")
		} else {
			continue
		}

		// Force expected float values to actually being float (e.g. if delivered as int)
		if u.ForceFloat {
			float, err := internal.ToFloat64(v)
			if err == nil {
				v = float
			}
		}
		fields[key] = v
	}

	acc.AddFields("upsd", fields, tags)
}

func mapStatus(metrics map[string]interface{}, tags map[string]string) uint64 {
	status := uint64(0)
	statusString := fmt.Sprintf("%v", metrics["ups.status"])
	statuses := strings.Fields(statusString)
	// Source: 1.3.2 at http://rogerprice.org/NUT/ConfigExamples.A5.pdf
	// apcupsd bits:
	// 0	Runtime calibration occurring (Not reported by Smart UPS v/s and BackUPS Pro)
	// 1	SmartTrim (Not reported by 1st and 2nd generation SmartUPS models)
	// 2	SmartBoost
	// 3	On line (this is the normal condition)
	// 4	On battery
	// 5	Overloaded output
	// 6	Battery low
	// 7	Replace battery
	if choice.Contains("CAL", statuses) {
		status |= 1 << 0
		tags["status_CAL"] = "true"
	}
	if choice.Contains("TRIM", statuses) {
		status |= 1 << 1
		tags["status_TRIM"] = "true"
	}
	if choice.Contains("BOOST", statuses) {
		status |= 1 << 2
		tags["status_BOOST"] = "true"
	}
	if choice.Contains("OL", statuses) {
		status |= 1 << 3
		tags["status_OL"] = "true"
	}
	if choice.Contains("OB", statuses) {
		status |= 1 << 4
		tags["status_OB"] = "true"
	}
	if choice.Contains("OVER", statuses) {
		status |= 1 << 5
		tags["status_OVER"] = "true"
	}
	if choice.Contains("LB", statuses) {
		status |= 1 << 6
		tags["status_LB"] = "true"
	}
	if choice.Contains("RB", statuses) {
		status |= 1 << 7
		tags["status_RB"] = "true"
	}
	return status
}

func (u *Upsd) fetchVariables(server string, port int) (map[string][]nut.Variable, error) {
	client, err := nut.Connect(server, port)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	if u.Username != "" && u.Password != "" {
		_, err = client.Authenticate(u.Username, u.Password)
		if err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
	}

	upsList, err := client.GetUPSList()
	if err != nil {
		return nil, fmt.Errorf("getupslist: %w", err)
	}

	defer func() {
		_, disconnectErr := client.Disconnect()
		if disconnectErr != nil {
			err = fmt.Errorf("disconnect: %w", disconnectErr)
		}
	}()

	result := make(map[string][]nut.Variable)
	for _, ups := range upsList {
		result[ups.Name] = ups.Variables
	}

	return result, err
}

func init() {
	inputs.Add("upsd", func() telegraf.Input {
		return &Upsd{
			Server: defaultAddress,
			Port:   defaultPort,
		}
	})
}
