//go:generate ../../../tools/readme_config_includer/generator
package redfish

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	// tag sets used for including redfish OData link parent data
	tagSetChassisLocation = "chassis.location"
	tagSetChassis         = "chassis"
)

type Redfish struct {
	Address          string          `toml:"address"`
	Username         config.Secret   `toml:"username"`
	Password         config.Secret   `toml:"password"`
	ComputerSystemID string          `toml:"computer_system_id"`
	IncludeMetrics   []string        `toml:"include_metrics"`
	IncludeTagSets   []string        `toml:"include_tag_sets"`
	Workarounds      []string        `toml:"workarounds"`
	Timeout          config.Duration `toml:"timeout"`

	tagSet map[string]bool
	client http.Client
	tls.ClientConfig
	baseURL *url.URL
}

type system struct {
	Hostname string `json:"hostname"`
	Links    struct {
		Chassis []struct {
			Ref string `json:"@odata.id"`
		}
	}
}

type chassis struct {
	ChassisType  string
	Location     *location
	Manufacturer string
	Model        string
	PartNumber   string
	Power        struct {
		Ref string `json:"@odata.id"`
	}
	PowerState   string
	SKU          string
	SerialNumber string
	Status       status
	Thermal      struct {
		Ref string `json:"@odata.id"`
	}
}

type power struct {
	PowerControl []struct {
		Name                string
		MemberID            string
		PowerAllocatedWatts *float64
		PowerAvailableWatts *float64
		PowerCapacityWatts  *float64
		PowerConsumedWatts  *float64
		PowerRequestedWatts *float64
		PowerMetrics        struct {
			AverageConsumedWatts *float64
			IntervalInMin        int
			MaxConsumedWatts     *float64
			MinConsumedWatts     *float64
		}
	}
	PowerSupplies []struct {
		Name                 string
		MemberID             string
		PowerInputWatts      *float64
		PowerCapacityWatts   *float64
		PowerOutputWatts     *float64
		LastPowerOutputWatts *float64
		Status               status
		LineInputVoltage     *float64
	}
	Voltages []struct {
		Name                   string
		MemberID               string
		ReadingVolts           *float64
		UpperThresholdCritical *float64
		UpperThresholdFatal    *float64
		LowerThresholdCritical *float64
		LowerThresholdFatal    *float64
		Status                 status
	}
}

type thermal struct {
	Fans []struct {
		Name                   string
		MemberID               string
		FanName                string
		CurrentReading         *int64
		Reading                *int64
		ReadingUnits           *string
		UpperThresholdCritical *int64
		UpperThresholdFatal    *int64
		LowerThresholdCritical *int64
		LowerThresholdFatal    *int64
		Status                 status
	}
	Temperatures []struct {
		Name                   string
		MemberID               string
		ReadingCelsius         *float64
		UpperThresholdCritical *float64
		UpperThresholdFatal    *float64
		LowerThresholdCritical *float64
		LowerThresholdFatal    *float64
		Status                 status
	}
}

type location struct {
	PostalAddress struct {
		DataCenter string
		Room       string
	}
	Placement struct {
		Rack string
		Row  string
	}
}

type status struct {
	State  string
	Health string
}

func (*Redfish) SampleConfig() string {
	return sampleConfig
}

func (r *Redfish) Init() error {
	if r.Address == "" {
		return errors.New("did not provide IP")
	}

	if r.Username.Empty() && r.Password.Empty() {
		return errors.New("did not provide username and password")
	}

	if r.ComputerSystemID == "" {
		return errors.New("did not provide the computer system ID of the resource")
	}

	if len(r.IncludeMetrics) == 0 {
		return errors.New("no metrics specified to collect")
	}
	for _, metric := range r.IncludeMetrics {
		switch metric {
		case "thermal", "power":
		default:
			return fmt.Errorf("unknown metric requested: %s", metric)
		}
	}

	for _, workaround := range r.Workarounds {
		switch workaround {
		case "ilo4-thermal":
		default:
			return fmt.Errorf("unknown workaround requested: %s", workaround)
		}
	}
	r.tagSet = make(map[string]bool, len(r.IncludeTagSets))
	for _, setLabel := range r.IncludeTagSets {
		r.tagSet[setLabel] = true
	}

	var err error
	r.baseURL, err = url.Parse(r.Address)
	if err != nil {
		return err
	}

	tlsCfg, err := r.ClientConfig.TLSConfig()
	if err != nil {
		return err
	}

	r.client = http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
			Proxy:           http.ProxyFromEnvironment,
		},
		Timeout: time.Duration(r.Timeout),
	}

	return nil
}

func (r *Redfish) Gather(acc telegraf.Accumulator) error {
	address, _, err := net.SplitHostPort(r.baseURL.Host)
	if err != nil {
		address = r.baseURL.Host
	}

	system, err := r.getComputerSystem(r.ComputerSystemID)
	if err != nil {
		return err
	}

	for _, link := range system.Links.Chassis {
		chassis, err := r.getChassis(link.Ref)
		if err != nil {
			return err
		}

		for _, metric := range r.IncludeMetrics {
			var err error
			switch metric {
			case "thermal":
				err = r.gatherThermal(acc, address, system, chassis)
			case "power":
				err = r.gatherPower(acc, address, system, chassis)
			default:
				return fmt.Errorf("unknown metric requested: %s", metric)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Redfish) getData(address string, payload interface{}) error {
	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return err
	}

	username, err := r.Username.Get()
	if err != nil {
		return fmt.Errorf("getting username failed: %w", err)
	}
	user := username.String()
	username.Destroy()

	password, err := r.Password.Get()
	if err != nil {
		return fmt.Errorf("getting password failed: %w", err)
	}
	pass := password.String()
	password.Destroy()

	req.SetBasicAuth(user, pass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OData-Version", "4.0")

	// workaround for iLO4 thermal data
	if slices.Contains(r.Workarounds, "ilo4-thermal") && strings.Contains(address, "/Thermal") {
		req.Header.Del("OData-Version")
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("received status code %d (%s) for address %s, expected 200",
			resp.StatusCode,
			http.StatusText(resp.StatusCode),
			r.Address)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(body, &payload)
	if err != nil {
		return fmt.Errorf("error parsing input: %w", err)
	}

	return nil
}

func (r *Redfish) getComputerSystem(id string) (*system, error) {
	loc := r.baseURL.ResolveReference(&url.URL{Path: path.Join("/redfish/v1/Systems/", id)})
	system := &system{}
	err := r.getData(loc.String(), system)
	if err != nil {
		return nil, err
	}
	return system, nil
}

func (r *Redfish) getChassis(ref string) (*chassis, error) {
	loc := r.baseURL.ResolveReference(&url.URL{Path: ref})
	chassis := &chassis{}
	err := r.getData(loc.String(), chassis)
	if err != nil {
		return nil, err
	}
	return chassis, nil
}

func (r *Redfish) getPower(ref string) (*power, error) {
	loc := r.baseURL.ResolveReference(&url.URL{Path: ref})
	power := &power{}
	err := r.getData(loc.String(), power)
	if err != nil {
		return nil, err
	}
	return power, nil
}

func (r *Redfish) getThermal(ref string) (*thermal, error) {
	loc := r.baseURL.ResolveReference(&url.URL{Path: ref})
	thermal := &thermal{}
	err := r.getData(loc.String(), thermal)
	if err != nil {
		return nil, err
	}
	return thermal, nil
}

func setChassisTags(chassis *chassis, tags map[string]string) {
	tags["chassis_chassistype"] = chassis.ChassisType
	tags["chassis_manufacturer"] = chassis.Manufacturer
	tags["chassis_model"] = chassis.Model
	tags["chassis_partnumber"] = chassis.PartNumber
	tags["chassis_powerstate"] = chassis.PowerState
	tags["chassis_sku"] = chassis.SKU
	tags["chassis_serialnumber"] = chassis.SerialNumber
	tags["chassis_state"] = chassis.Status.State
	tags["chassis_health"] = chassis.Status.Health
}

func (r *Redfish) gatherThermal(acc telegraf.Accumulator, address string, system *system, chassis *chassis) error {
	thermal, err := r.getThermal(chassis.Thermal.Ref)
	if err != nil {
		return err
	}

	for _, j := range thermal.Temperatures {
		tags := make(map[string]string, 19)
		tags["member_id"] = j.MemberID
		tags["address"] = address
		tags["name"] = j.Name
		tags["source"] = system.Hostname
		tags["state"] = j.Status.State
		tags["health"] = j.Status.Health
		if _, ok := r.tagSet[tagSetChassisLocation]; ok && chassis.Location != nil {
			tags["datacenter"] = chassis.Location.PostalAddress.DataCenter
			tags["room"] = chassis.Location.PostalAddress.Room
			tags["rack"] = chassis.Location.Placement.Rack
			tags["row"] = chassis.Location.Placement.Row
		}
		if _, ok := r.tagSet[tagSetChassis]; ok {
			setChassisTags(chassis, tags)
		}

		fields := make(map[string]interface{})
		fields["reading_celsius"] = j.ReadingCelsius
		fields["upper_threshold_critical"] = j.UpperThresholdCritical
		fields["upper_threshold_fatal"] = j.UpperThresholdFatal
		fields["lower_threshold_critical"] = j.LowerThresholdCritical
		fields["lower_threshold_fatal"] = j.LowerThresholdFatal
		acc.AddFields("redfish_thermal_temperatures", fields, tags)
	}

	for _, j := range thermal.Fans {
		tags := make(map[string]string, 20)
		fields := make(map[string]interface{}, 5)
		tags["member_id"] = j.MemberID
		tags["address"] = address
		tags["name"] = j.Name
		if j.FanName != "" {
			tags["name"] = j.FanName
		}
		tags["source"] = system.Hostname
		tags["state"] = j.Status.State
		tags["health"] = j.Status.Health
		if _, ok := r.tagSet[tagSetChassisLocation]; ok && chassis.Location != nil {
			tags["datacenter"] = chassis.Location.PostalAddress.DataCenter
			tags["room"] = chassis.Location.PostalAddress.Room
			tags["rack"] = chassis.Location.Placement.Rack
			tags["row"] = chassis.Location.Placement.Row
		}
		if _, ok := r.tagSet[tagSetChassis]; ok {
			setChassisTags(chassis, tags)
		}

		if j.ReadingUnits != nil && *j.ReadingUnits == "RPM" {
			fields["upper_threshold_critical"] = j.UpperThresholdCritical
			fields["upper_threshold_fatal"] = j.UpperThresholdFatal
			fields["lower_threshold_critical"] = j.LowerThresholdCritical
			fields["lower_threshold_fatal"] = j.LowerThresholdFatal
			fields["reading_rpm"] = j.Reading
		} else if j.CurrentReading != nil {
			fields["reading_percent"] = j.CurrentReading
		} else {
			fields["reading_percent"] = j.Reading
		}
		acc.AddFields("redfish_thermal_fans", fields, tags)
	}

	return nil
}

func (r *Redfish) gatherPower(acc telegraf.Accumulator, address string, system *system, chassis *chassis) error {
	power, err := r.getPower(chassis.Power.Ref)
	if err != nil {
		return err
	}

	for _, j := range power.PowerControl {
		tags := map[string]string{
			"member_id": j.MemberID,
			"address":   address,
			"name":      j.Name,
			"source":    system.Hostname,
		}
		if _, ok := r.tagSet[tagSetChassisLocation]; ok && chassis.Location != nil {
			tags["datacenter"] = chassis.Location.PostalAddress.DataCenter
			tags["room"] = chassis.Location.PostalAddress.Room
			tags["rack"] = chassis.Location.Placement.Rack
			tags["row"] = chassis.Location.Placement.Row
		}
		if _, ok := r.tagSet[tagSetChassis]; ok {
			setChassisTags(chassis, tags)
		}

		fields := map[string]interface{}{
			"power_allocated_watts":  j.PowerAllocatedWatts,
			"power_available_watts":  j.PowerAvailableWatts,
			"power_capacity_watts":   j.PowerCapacityWatts,
			"power_consumed_watts":   j.PowerConsumedWatts,
			"power_requested_watts":  j.PowerRequestedWatts,
			"average_consumed_watts": j.PowerMetrics.AverageConsumedWatts,
			"interval_in_min":        j.PowerMetrics.IntervalInMin,
			"max_consumed_watts":     j.PowerMetrics.MaxConsumedWatts,
			"min_consumed_watts":     j.PowerMetrics.MinConsumedWatts,
		}

		acc.AddFields("redfish_power_powercontrol", fields, tags)
	}

	for _, j := range power.PowerSupplies {
		tags := make(map[string]string, 19)
		tags["member_id"] = j.MemberID
		tags["address"] = address
		tags["name"] = j.Name
		tags["source"] = system.Hostname
		tags["state"] = j.Status.State
		tags["health"] = j.Status.Health
		if _, ok := r.tagSet[tagSetChassisLocation]; ok && chassis.Location != nil {
			tags["datacenter"] = chassis.Location.PostalAddress.DataCenter
			tags["room"] = chassis.Location.PostalAddress.Room
			tags["rack"] = chassis.Location.Placement.Rack
			tags["row"] = chassis.Location.Placement.Row
		}
		if _, ok := r.tagSet[tagSetChassis]; ok {
			setChassisTags(chassis, tags)
		}

		fields := make(map[string]interface{})
		fields["power_input_watts"] = j.PowerInputWatts
		fields["power_output_watts"] = j.PowerOutputWatts
		fields["line_input_voltage"] = j.LineInputVoltage
		fields["last_power_output_watts"] = j.LastPowerOutputWatts
		fields["power_capacity_watts"] = j.PowerCapacityWatts
		acc.AddFields("redfish_power_powersupplies", fields, tags)
	}

	for _, j := range power.Voltages {
		tags := make(map[string]string, 19)
		tags["member_id"] = j.MemberID
		tags["address"] = address
		tags["name"] = j.Name
		tags["source"] = system.Hostname
		tags["state"] = j.Status.State
		tags["health"] = j.Status.Health
		if _, ok := r.tagSet[tagSetChassisLocation]; ok && chassis.Location != nil {
			tags["datacenter"] = chassis.Location.PostalAddress.DataCenter
			tags["room"] = chassis.Location.PostalAddress.Room
			tags["rack"] = chassis.Location.Placement.Rack
			tags["row"] = chassis.Location.Placement.Row
		}
		if _, ok := r.tagSet[tagSetChassis]; ok {
			setChassisTags(chassis, tags)
		}

		fields := make(map[string]interface{})
		fields["reading_volts"] = j.ReadingVolts
		fields["upper_threshold_critical"] = j.UpperThresholdCritical
		fields["upper_threshold_fatal"] = j.UpperThresholdFatal
		fields["lower_threshold_critical"] = j.LowerThresholdCritical
		fields["lower_threshold_fatal"] = j.LowerThresholdFatal
		acc.AddFields("redfish_power_voltages", fields, tags)
	}

	return nil
}

func init() {
	inputs.Add("redfish", func() telegraf.Input {
		return &Redfish{
			// default tag set of chassis.location required for backwards compatibility
			IncludeTagSets: []string{tagSetChassisLocation},
			IncludeMetrics: []string{"power", "thermal"},
		}
	})
}
