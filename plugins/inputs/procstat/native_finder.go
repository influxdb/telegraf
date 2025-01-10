package procstat

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
)

// NativeFinder uses gopsutil to find processes
type NativeFinder struct{}

// Uid will return all pids for the given user
func (*NativeFinder) uid(user string) ([]pid, error) {
	var dst []pid
	procs, err := gopsprocess.Processes()
	if err != nil {
		return dst, err
	}
	for _, p := range procs {
		username, err := p.Username()
		if err != nil {
			// skip, this can be caused by the pid no longer exists, or you don't have permissions to access it
			continue
		}
		if username == user {
			dst = append(dst, pid(p.Pid))
		}
	}
	return dst, nil
}

// PidFile returns the pid from the pid file given.
func (*NativeFinder) pidFile(path string) ([]pid, error) {
	var pids []pid
	pidString, err := os.ReadFile(path)
	if err != nil {
		return pids, fmt.Errorf("failed to read pidfile %q: %w", path, err)
	}
	processID, err := strconv.ParseInt(strings.TrimSpace(string(pidString)), 10, 32)
	if err != nil {
		return pids, err
	}
	pids = append(pids, pid(processID))
	return pids, nil
}

// FullPattern matches on the command line when the process was executed
func (*NativeFinder) fullPattern(pattern string) ([]pid, error) {
	var pids []pid
	regxPattern, err := regexp.Compile(pattern)
	if err != nil {
		return pids, err
	}
	procs, err := fastProcessList()
	if err != nil {
		return pids, err
	}
	for _, p := range procs {
		cmd, err := p.Cmdline()
		if err != nil {
			// skip, this can be caused by the pid no longer exists, or you don't have permissions to access it
			continue
		}
		if regxPattern.MatchString(cmd) {
			pids = append(pids, pid(p.Pid))
		}
	}
	return pids, err
}

// Children matches children pids on the command line when the process was executed
func (*NativeFinder) children(processID pid) ([]pid, error) {
	// Get all running processes
	p, err := gopsprocess.NewProcess(int32(processID))
	if err != nil {
		return nil, fmt.Errorf("getting process %d failed: %w", processID, err)
	}

	// Get all children of the current process
	children, err := p.Children()
	if err != nil {
		return nil, fmt.Errorf("unable to get children of process %d: %w", p.Pid, err)
	}
	pids := make([]pid, 0, len(children))
	for _, child := range children {
		pids = append(pids, pid(child.Pid))
	}

	return pids, err
}

func fastProcessList() ([]*gopsprocess.Process, error) {
	pids, err := gopsprocess.Pids()
	if err != nil {
		return nil, err
	}

	result := make([]*gopsprocess.Process, 0, len(pids))
	for _, pid := range pids {
		result = append(result, &gopsprocess.Process{Pid: pid})
	}
	return result, nil
}

// Pattern matches on the process name
func (*NativeFinder) pattern(pattern string) ([]pid, error) {
	var pids []pid
	regxPattern, err := regexp.Compile(pattern)
	if err != nil {
		return pids, err
	}
	procs, err := fastProcessList()
	if err != nil {
		return pids, err
	}
	for _, p := range procs {
		name, err := processName(p)
		if err != nil {
			// skip, this can be caused by the pid no longer exists, or you don't have permissions to access it
			continue
		}
		if regxPattern.MatchString(name) {
			pids = append(pids, pid(p.Pid))
		}
	}
	return pids, err
}
