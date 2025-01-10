package procstat

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	gopsprocess "github.com/shirou/gopsutil/v4/process"

	"github.com/influxdata/telegraf"
	telegraf_filter "github.com/influxdata/telegraf/filter"
)

type filter struct {
	Name            string          `toml:"name"`
	PidFiles        []string        `toml:"pid_files"`
	SystemdUnits    []string        `toml:"systemd_units"`
	SupervisorUnits []string        `toml:"supervisor_units"`
	WinService      []string        `toml:"win_services"`
	CGroups         []string        `toml:"cgroups"`
	Patterns        []string        `toml:"patterns"`
	Users           []string        `toml:"users"`
	Executables     []string        `toml:"executables"`
	ProcessNames    []string        `toml:"process_names"`
	RecursionDepth  int             `toml:"recursion_depth"`
	Log             telegraf.Logger `toml:"-"`

	filterSupervisorUnit string
	filterCmds           []*regexp.Regexp
	filterUser           telegraf_filter.Filter
	filterExecutable     telegraf_filter.Filter
	filterProcessName    telegraf_filter.Filter
	finder               *processFinder
}

func (f *filter) init() error {
	if f.Name == "" {
		return errors.New("filter must be named")
	}

	// Check for only one service selector being active
	var active []string
	if len(f.PidFiles) > 0 {
		active = append(active, "pid_files")
	}
	if len(f.CGroups) > 0 {
		active = append(active, "cgroups")
	}
	if len(f.SystemdUnits) > 0 {
		active = append(active, "systemd_units")
	}
	if len(f.SupervisorUnits) > 0 {
		active = append(active, "supervisor_units")
	}
	if len(f.WinService) > 0 {
		active = append(active, "win_services")
	}
	if len(active) > 1 {
		return fmt.Errorf("cannot select multiple services %q", strings.Join(active, ", "))
	}

	// Prepare the filters
	f.filterCmds = make([]*regexp.Regexp, 0, len(f.Patterns))
	for _, p := range f.Patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("compiling pattern %q of filter %q failed: %w", p, f.Name, err)
		}
		f.filterCmds = append(f.filterCmds, re)
	}

	f.filterSupervisorUnit = strings.TrimSpace(strings.Join(f.SupervisorUnits, " "))

	var err error
	if f.filterUser, err = telegraf_filter.Compile(f.Users); err != nil {
		return fmt.Errorf("compiling users filter for %q failed: %w", f.Name, err)
	}
	if f.filterExecutable, err = telegraf_filter.Compile(f.Executables); err != nil {
		return fmt.Errorf("compiling executables filter for %q failed: %w", f.Name, err)
	}
	if f.filterProcessName, err = telegraf_filter.Compile(f.ProcessNames); err != nil {
		return fmt.Errorf("compiling process-names filter for %q failed: %w", f.Name, err)
	}

	// Setup the process finder
	f.finder = newProcessFinder(f.Log)
	return nil
}

func (f *filter) applyFilter() ([]processGroup, error) {
	// Determine processes on service level. if there is no constraint on the
	// services, use all processes for matching.
	var groups []processGroup
	switch {
	case len(f.PidFiles) > 0:
		g, err := f.finder.findByPidFiles(f.PidFiles)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g...)
	case len(f.CGroups) > 0:
		g, err := findByCgroups(f.CGroups)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g...)
	case len(f.SystemdUnits) > 0:
		g, err := findBySystemdUnits(f.SystemdUnits)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g...)
	case f.filterSupervisorUnit != "":
		g, err := findBySupervisorUnits(f.filterSupervisorUnit)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g...)
	case len(f.WinService) > 0:
		g, err := findByWindowsServices(f.WinService)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g...)
	default:
		procs, err := gopsprocess.Processes()
		if err != nil {
			return nil, err
		}
		groups = append(groups, processGroup{processes: procs, tags: make(map[string]string)})
	}

	// Filter by additional properties such as users, patterns etc
	result := make([]processGroup, 0, len(groups))
	for _, g := range groups {
		var matched []*gopsprocess.Process
		for _, p := range g.processes {
			// Users
			if f.filterUser != nil {
				if username, err := p.Username(); err != nil || !f.filterUser.Match(username) {
					// Errors can happen if we don't have permissions or the process no longer exists
					continue
				}
			}

			// Executables
			if f.filterExecutable != nil {
				if exe, err := p.Exe(); err != nil || !f.filterExecutable.Match(exe) {
					continue
				}
			}

			// Process names
			if f.filterProcessName != nil {
				if name, err := p.Name(); err != nil || !f.filterProcessName.Match(name) {
					continue
				}
			}

			// Patterns
			if len(f.filterCmds) > 0 {
				cmd, err := p.Cmdline()
				if err != nil {
					// This can happen if we don't have permissions or the process no longer exists
					continue
				}
				var found bool
				for _, re := range f.filterCmds {
					if re.MatchString(cmd) {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			matched = append(matched, p)
		}
		result = append(result, processGroup{processes: matched, tags: g.tags})
	}

	// Resolve children down to the requested depth
	previous := result
	for depth := 0; depth < f.RecursionDepth || f.RecursionDepth < 0; depth++ {
		children := make([]processGroup, 0, len(previous))
		for _, group := range previous {
			for _, p := range group.processes {
				c, err := getChildren(p)
				if err != nil {
					return nil, fmt.Errorf("unable to get children of process %d: %w", p.Pid, err)
				}
				if len(c) == 0 {
					continue
				}

				tags := make(map[string]string, len(group.tags)+1)
				for k, v := range group.tags {
					tags[k] = v
				}
				tags["parent_pid"] = strconv.FormatInt(int64(p.Pid), 10)
				children = append(children, processGroup{
					processes: c,
					tags:      tags,
				})
			}
		}
		if len(children) == 0 {
			break
		}
		result = append(result, children...)
		previous = children
	}

	return result, nil
}

func getChildren(p *gopsprocess.Process) ([]*gopsprocess.Process, error) {
	children, err := p.Children()
	// Check for cases that do not really mean error but rather means that there
	// is no match.
	switch {
	case err == nil,
		errors.Is(err, gopsprocess.ErrorNoChildren),
		strings.Contains(err.Error(), "exit status 1"):
		return children, nil
	}
	return nil, fmt.Errorf("unable to get children of process %d: %w", p.Pid, err)
}
