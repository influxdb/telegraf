package procstat

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf/internal"
)

// Implementation of PIDGatherer that execs pgrep to find processes
type pgrep struct {
	path string
}

func newPgrepFinder() (pidFinder, error) {
	path, err := exec.LookPath("pgrep")
	if err != nil {
		return nil, fmt.Errorf("could not find pgrep binary: %w", err)
	}
	return &pgrep{path}, nil
}

func (*pgrep) pidFile(path string) ([]pid, error) {
	var pids []pid
	pidString, err := os.ReadFile(path)
	if err != nil {
		return pids, fmt.Errorf("failed to read pidfile %q: %w",
			path, err)
	}
	processID, err := strconv.ParseInt(strings.TrimSpace(string(pidString)), 10, 32)
	if err != nil {
		return pids, err
	}
	pids = append(pids, pid(processID))
	return pids, nil
}

func (pg *pgrep) pattern(pattern string) ([]pid, error) {
	args := []string{pattern}
	return pg.find(args)
}

func (pg *pgrep) uid(user string) ([]pid, error) {
	args := []string{"-u", user}
	return pg.find(args)
}

func (pg *pgrep) fullPattern(pattern string) ([]pid, error) {
	args := []string{"-f", pattern}
	return pg.find(args)
}

func (pg *pgrep) children(pid pid) ([]pid, error) {
	args := []string{"-P", strconv.FormatInt(int64(pid), 10)}
	return pg.find(args)
}

func (pg *pgrep) find(args []string) ([]pid, error) {
	// Execute pgrep with the given arguments
	buf, err := exec.Command(pg.path, args...).Output()
	if err != nil {
		// Exit code 1 means "no processes found" so we should not return
		// an error in this case.
		if status, _ := internal.ExitStatus(err); status == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("error running %q: %w", pg.path, err)
	}
	out := string(buf)

	// Parse the command output to extract the PIDs
	fields := strings.Fields(out)
	pids := make([]pid, 0, len(fields))
	for _, field := range fields {
		processID, err := strconv.ParseInt(field, 10, 32)
		if err != nil {
			return nil, err
		}
		pids = append(pids, pid(processID))
	}
	return pids, nil
}
