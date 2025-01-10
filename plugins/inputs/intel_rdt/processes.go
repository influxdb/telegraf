//go:build !windows

package intel_rdt

import "github.com/prometheus/procfs"

type processesHandler interface {
	getAllProcesses() ([]process, error)
}

type process struct {
	Name string
	PID  int
}

type processManager struct{}

func newProcessor() processesHandler {
	return &processManager{}
}

func (*processManager) getAllProcesses() ([]process, error) {
	allProcesses, err := procfs.AllProcs()
	if err != nil {
		return nil, err
	}

	processes := make([]process, 0, len(allProcesses))
	for _, proc := range allProcesses {
		procComm, err := proc.Comm()
		if err != nil {
			continue
		}
		newProcess := process{
			PID:  proc.PID,
			Name: procComm,
		}
		processes = append(processes, newProcess)
	}
	return processes, nil
}
