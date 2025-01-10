package procstat

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gopsprocess "github.com/shirou/gopsutil/v4/process"

	"github.com/influxdata/telegraf"
)

type processFinder struct {
	errPidFiles map[string]bool
	log         telegraf.Logger
}

func newProcessFinder(log telegraf.Logger) *processFinder {
	return &processFinder{
		errPidFiles: make(map[string]bool),
		log:         log,
	}
}

func (f *processFinder) findByPidFiles(paths []string) ([]processGroup, error) {
	groups := make([]processGroup, 0, len(paths))
	for _, path := range paths {
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read pidfile %q: %w", path, err)
		}
		pid, err := strconv.ParseInt(strings.TrimSpace(string(buf)), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PID in file %q: %w", path, err)
		}

		p, err := gopsprocess.NewProcess(int32(pid))
		if err != nil && !f.errPidFiles[path] {
			f.log.Errorf("failed to find process for PID %d of file %q: %v", pid, path, err)
			f.errPidFiles[path] = true
		}
		groups = append(groups, processGroup{
			processes: []*gopsprocess.Process{p},
			tags:      map[string]string{"pidfile": path},
		})
	}

	return groups, nil
}

func findByCgroups(cgroups []string) ([]processGroup, error) {
	groups := make([]processGroup, 0, len(cgroups))
	for _, cgroup := range cgroups {
		path := cgroup
		if !filepath.IsAbs(cgroup) {
			path = filepath.Join("sys", "fs", "cgroup"+cgroup)
		}

		files, err := filepath.Glob(path)
		if err != nil {
			return nil, fmt.Errorf("failed to determine files for cgroup %q: %w", cgroup, err)
		}

		for _, fpath := range files {
			if f, err := os.Stat(fpath); err != nil {
				return nil, fmt.Errorf("accessing %q failed: %w", fpath, err)
			} else if !f.IsDir() {
				return nil, fmt.Errorf("%q is not a directory", fpath)
			}

			fn := filepath.Join(fpath, "cgroup.procs")
			buf, err := os.ReadFile(fn)
			if err != nil {
				return nil, err
			}
			lines := bytes.Split(buf, []byte{'\n'})
			procs := make([]*gopsprocess.Process, 0, len(lines))
			for _, l := range lines {
				l := strings.TrimSpace(string(l))
				if len(l) == 0 {
					continue
				}
				pid, err := strconv.ParseInt(l, 10, 32)
				if err != nil {
					return nil, fmt.Errorf("failed to parse PID %q in file %q", l, fpath)
				}
				p, err := gopsprocess.NewProcess(int32(pid))
				if err != nil {
					return nil, fmt.Errorf("failed to find process for PID %d of %q: %w", pid, fpath, err)
				}
				procs = append(procs, p)
			}

			groups = append(groups, processGroup{
				processes: procs,
				tags:      map[string]string{"cgroup": cgroup, "cgroup_full": fpath}})
		}
	}

	return groups, nil
}

func findBySupervisorUnits(units string) ([]processGroup, error) {
	buf, err := execCommand("supervisorctl", "status", units, " ").Output()
	if err != nil && !strings.Contains(err.Error(), "exit status 3") {
		// Exit 3 means at least on process is in one of the "STOPPED" states
		return nil, fmt.Errorf("failed to execute 'supervisorctl': %w", err)
	}
	lines := strings.Split(string(buf), "\n")

	// Get the PID, running status, running time and boot time of the main process:
	// pid 11779, uptime 17:41:16
	// Exited too quickly (process log may have details)
	groups := make([]processGroup, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}

		kv := strings.Fields(line)
		if len(kv) < 2 {
			// Not a key-value pair
			continue
		}
		name, status := kv[0], kv[1]
		tags := map[string]string{
			"supervisor_unit": name,
			"status":          status,
		}

		var procs []*gopsprocess.Process
		switch status {
		case "FATAL", "EXITED", "BACKOFF", "STOPPING":
			tags["error"] = strings.Join(kv[2:], " ")
		case "RUNNING":
			tags["uptimes"] = kv[5]
			rawpid := strings.ReplaceAll(kv[3], ",", "")
			grouppid, err := strconv.ParseInt(rawpid, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse group PID %q: %w", rawpid, err)
			}
			p, err := gopsprocess.NewProcess(int32(grouppid))
			if err != nil {
				return nil, fmt.Errorf("failed to find process for PID %d of unit %q: %w", grouppid, name, err)
			}
			// Get all children of the supervisor unit
			procs, err = p.Children()
			if err != nil {
				return nil, fmt.Errorf("failed to get children for PID %d of unit %q: %w", grouppid, name, err)
			}
			tags["parent_pid"] = rawpid
		case "STOPPED", "UNKNOWN", "STARTING":
			// No additional info
		}

		groups = append(groups, processGroup{
			processes: procs,
			tags:      tags,
		})
	}

	return groups, nil
}
