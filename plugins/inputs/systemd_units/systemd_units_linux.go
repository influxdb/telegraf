//go:build linux

package systemd_units

import (
	"context"
	"fmt"
	"math"
	"os/user"
	"path"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
)

// Below are mappings of systemd state tables as defined in
// https://github.com/systemd/systemd/blob/c87700a1335f489be31cd3549927da68b5638819/src/basic/unit-def.c
// Duplicate strings are removed from this list.
// This map is used by `subcommand_show` and `subcommand_list`. Changes must be
// compatible with both subcommands.
var loadMap = map[string]int{
	"loaded":      0,
	"stub":        1,
	"not-found":   2,
	"bad-setting": 3,
	"error":       4,
	"merged":      5,
	"masked":      6,
}

var activeMap = map[string]int{
	"active":       0,
	"reloading":    1,
	"inactive":     2,
	"failed":       3,
	"activating":   4,
	"deactivating": 5,
}

var subMap = map[string]int{
	// service_state_table, offset 0x0000
	"running":       0x0000,
	"dead":          0x0001,
	"start-pre":     0x0002,
	"start":         0x0003,
	"exited":        0x0004,
	"reload":        0x0005,
	"stop":          0x0006,
	"stop-watchdog": 0x0007,
	"stop-sigterm":  0x0008,
	"stop-sigkill":  0x0009,
	"stop-post":     0x000a,
	"final-sigterm": 0x000b,
	"failed":        0x000c,
	"auto-restart":  0x000d,
	"condition":     0x000e,
	"cleaning":      0x000f,

	// automount_state_table, offset 0x0010
	// continuation of service_state_table
	"waiting":                    0x0010,
	"reload-signal":              0x0011,
	"reload-notify":              0x0012,
	"final-watchdog":             0x0013,
	"dead-before-auto-restart":   0x0014,
	"failed-before-auto-restart": 0x0015,
	"dead-resources-pinned":      0x0016,
	"auto-restart-queued":        0x0017,

	// device_state_table, offset 0x0020
	"tentative": 0x0020,
	"plugged":   0x0021,

	// mount_state_table, offset 0x0030
	"mounting":           0x0030,
	"mounting-done":      0x0031,
	"mounted":            0x0032,
	"remounting":         0x0033,
	"unmounting":         0x0034,
	"remounting-sigterm": 0x0035,
	"remounting-sigkill": 0x0036,
	"unmounting-sigterm": 0x0037,
	"unmounting-sigkill": 0x0038,

	// path_state_table, offset 0x0040

	// scope_state_table, offset 0x0050
	"abandoned": 0x0050,

	// slice_state_table, offset 0x0060
	"active": 0x0060,

	// socket_state_table, offset 0x0070
	"start-chown":      0x0070,
	"start-post":       0x0071,
	"listening":        0x0072,
	"stop-pre":         0x0073,
	"stop-pre-sigterm": 0x0074,
	"stop-pre-sigkill": 0x0075,
	"final-sigkill":    0x0076,

	// swap_state_table, offset 0x0080
	"activating":           0x0080,
	"activating-done":      0x0081,
	"deactivating":         0x0082,
	"deactivating-sigterm": 0x0083,
	"deactivating-sigkill": 0x0084,

	// target_state_table, offset 0x0090

	// timer_state_table, offset 0x00a0
	"elapsed": 0x00a0,
}

type client interface {
	Connected() bool
	Close()

	ListUnitFilesByPatternsContext(ctx context.Context, states, pattern []string) ([]dbus.UnitFile, error)
	ListUnitsByNamesContext(ctx context.Context, units []string) ([]dbus.UnitStatus, error)
	GetUnitTypePropertiesContext(ctx context.Context, unit, unitType string) (map[string]interface{}, error)
	GetUnitPropertiesContext(ctx context.Context, unit string) (map[string]interface{}, error)
	ListUnitsContext(ctx context.Context) ([]dbus.UnitStatus, error)
}

type archParams struct {
	client        client
	pattern       []string
	filter        filter.Filter
	unitTypeDBus  string
	scope         string
	user          string
	warnUnitProps map[string]bool
}

func (s *SystemdUnits) Init() error {
	// Set default pattern
	if s.Pattern == "" {
		s.Pattern = "*"
	}

	// Check unit-type and convert the first letter to uppercase as this is
	// what dbus expects.
	switch s.UnitType {
	case "":
		s.UnitType = "service"
	case "service", "socket", "target", "device", "mount", "automount", "swap",
		"timer", "path", "slice", "scope":
	default:
		return fmt.Errorf("invalid 'unittype' %q", s.UnitType)
	}
	s.unitTypeDBus = strings.ToUpper(s.UnitType[0:1]) + strings.ToLower(s.UnitType[1:])

	s.pattern = strings.Split(s.Pattern, " ")
	f, err := filter.Compile(s.pattern)
	if err != nil {
		return fmt.Errorf("compiling filter failed: %w", err)
	}
	s.filter = f

	switch s.Scope {
	case "", "system":
		s.scope = "system"
	case "user":
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("unable to determine user: %w", err)
		}

		s.scope = "user"
		s.user = u.Username
	default:
		return fmt.Errorf("invalid 'scope' %q", s.Scope)
	}

	s.warnUnitProps = make(map[string]bool)

	return nil
}

func (s *SystemdUnits) Start(telegraf.Accumulator) error {
	ctx := context.Background()

	var client *dbus.Conn
	var err error
	if s.scope == "user" {
		client, err = dbus.NewUserConnectionContext(ctx)
	} else {
		client, err = dbus.NewSystemConnectionContext(ctx)
	}
	if err != nil {
		return err
	}

	s.client = client

	return nil
}

func (s *SystemdUnits) Stop() {
	if s.client != nil && s.client.Connected() {
		s.client.Close()
	}
	s.client = nil
}

func (s *SystemdUnits) Gather(acc telegraf.Accumulator) error {
	// Reconnect in case the connection was lost
	if !s.client.Connected() {
		s.Log.Debug("Connection to systemd daemon lost, trying to reconnect...")
		s.Stop()
		if err := s.Start(acc); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.Timeout))
	defer cancel()

	// List all loaded units to handle multi-instance units correctly
	loaded, err := s.client.ListUnitsContext(ctx)
	if err != nil {
		return fmt.Errorf("listing loaded units failed: %w", err)
	}

	var files []dbus.UnitFile
	if s.CollectDisabled {
		// List all unit files matching the pattern to also get disabled units
		list := []string{"enabled", "disabled", "static"}
		files, err = s.client.ListUnitFilesByPatternsContext(ctx, list, s.pattern)
		if err != nil {
			return fmt.Errorf("listing unit files failed: %w", err)
		}
	}

	// Collect all matching units, the loaded ones and the disabled ones
	states := make([]dbus.UnitStatus, 0, len(loaded))

	// Match all loaded units first
	seen := make(map[string]bool)
	for _, u := range loaded {
		if !s.filter.Match(u.Name) {
			continue
		}
		states = append(states, u)

		// Remember multi-instance units to remove duplicates from files
		instance := u.Name
		if strings.Contains(u.Name, "@") {
			prefix, _, _ := strings.Cut(u.Name, "@")
			suffix := path.Ext(u.Name)
			instance = prefix + "@" + suffix
		}
		seen[instance] = true
	}

	// Now split the unit-files into disabled ones and static ones, ignore
	// enabled units as those are already contained in the "loaded" list.
	if len(files) > 0 {
		disabled := make([]string, 0, len(files))
		static := make([]string, 0, len(files))
		for _, f := range files {
			name := path.Base(f.Path)

			switch f.Type {
			case "disabled":
				if seen[name] {
					continue
				}
				seen[name] = true

				// Detect disabled multi-instance units and declare them as static
				_, suffix, found := strings.Cut(name, "@")
				instance, _, _ := strings.Cut(suffix, ".")
				if found && instance == "" {
					static = append(static, name)
					continue
				}
				disabled = append(disabled, name)
			case "static":
				// Make sure we filter already loaded static multi-instance units
				instance := name
				if strings.Contains(name, "@") {
					prefix, _, _ := strings.Cut(name, "@")
					suffix := path.Ext(name)
					instance = prefix + "@" + suffix
				}
				if seen[instance] || seen[name] {
					continue
				}
				seen[instance] = true
				static = append(static, name)
			}
		}

		// Resolve the disabled and remaining static units
		disabledStates, err := s.client.ListUnitsByNamesContext(ctx, disabled)
		if err != nil {
			return fmt.Errorf("listing unit states failed: %w", err)
		}
		states = append(states, disabledStates...)

		// Add special information about unused static units
		for _, name := range static {
			if !strings.EqualFold(strings.TrimPrefix(path.Ext(name), "."), s.UnitType) {
				continue
			}

			states = append(states, dbus.UnitStatus{
				Name:        name,
				LoadState:   "stub",
				ActiveState: "inactive",
				SubState:    "dead",
			})
		}
	}

	// Merge the unit information into one struct
	for _, state := range states {
		// Filter units of the wrong type
		if idx := strings.LastIndex(state.Name, "."); idx < 0 || state.Name[idx+1:] != s.UnitType {
			continue
		}

		// Map the state names to numerical values
		load, ok := loadMap[state.LoadState]
		if !ok {
			acc.AddError(fmt.Errorf("parsing field 'load' failed, value not in map: %s", state.LoadState))
			continue
		}
		active, ok := activeMap[state.ActiveState]
		if !ok {
			acc.AddError(fmt.Errorf("parsing field 'active' failed, value not in map: %s", state.ActiveState))
			continue
		}
		subState, ok := subMap[state.SubState]
		if !ok {
			acc.AddError(fmt.Errorf("parsing field 'sub' failed, value not in map: %s", state.SubState))
			continue
		}

		// Create the metric
		tags := map[string]string{
			"name":   state.Name,
			"load":   state.LoadState,
			"active": state.ActiveState,
			"sub":    state.SubState,
		}
		if s.scope == "user" {
			tags["user"] = s.user
		}

		fields := map[string]interface{}{
			"load_code":   load,
			"active_code": active,
			"sub_code":    subState,
		}

		if s.Details {
			properties, err := s.client.GetUnitTypePropertiesContext(ctx, state.Name, s.unitTypeDBus)
			if err != nil {
				// Skip units returning "Unknown interface" errors as those indicate
				// that the unit is of the wrong type.
				if strings.Contains(err.Error(), "Unknown interface") {
					continue
				}
				// For other units we make up properties, usually those are
				// disabled multi-instance units
				properties = map[string]interface{}{
					"StatusErrno": int64(-1),
					"NRestarts":   uint64(0),
				}
			}

			// Get required unit file properties
			unitProperties, err := s.client.GetUnitPropertiesContext(ctx, state.Name)
			if err != nil && !s.warnUnitProps[state.Name] {
				s.Log.Warnf("Cannot read unit properties for %q: %v", state.Name, err)
				s.warnUnitProps[state.Name] = true
			}

			// Set tags
			if v, found := unitProperties["UnitFileState"]; found {
				tags["state"] = v.(string)
			}
			if v, found := unitProperties["UnitFilePreset"]; found {
				tags["preset"] = v.(string)
			}

			// Set fields
			if v, found := unitProperties["ActiveEnterTimestamp"]; found {
				fields["active_enter_timestamp_us"] = v
			}

			fields["status_errno"] = properties["StatusErrno"]
			fields["restarts"] = properties["NRestarts"]
			fields["pid"] = properties["MainPID"]

			fields["mem_current"] = properties["MemoryCurrent"]
			fields["mem_peak"] = properties["MemoryPeak"]
			fields["mem_avail"] = properties["MemoryAvailable"]

			fields["swap_current"] = properties["MemorySwapCurrent"]
			fields["swap_peak"] = properties["MemorySwapPeak"]

			// Sanitize unset memory fields
			for k, value := range fields {
				switch {
				case strings.HasPrefix(k, "mem_"), strings.HasPrefix(k, "swap_"):
					v, ok := value.(uint64)
					if ok && v == math.MaxUint64 || value == nil {
						fields[k] = uint64(0)
					}
				}
			}
		}
		acc.AddFields("systemd_units", fields, tags)
	}

	return nil
}
