//go:build windows

package procstat

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	gopsnet "github.com/shirou/gopsutil/v4/net"
	gopsprocess "github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

func processName(p *gopsprocess.Process) (string, error) {
	return p.Name()
}

func getService(name string) (*mgr.Service, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect()

	srv, err := m.OpenService(name)
	if err != nil {
		return nil, err
	}

	return srv, nil
}

func queryPidWithWinServiceName(winServiceName string) (uint32, error) {
	srv, err := getService(winServiceName)
	if err != nil {
		return 0, err
	}

	var p *windows.SERVICE_STATUS_PROCESS
	var bytesNeeded uint32
	var buf []byte

	err = windows.QueryServiceStatusEx(srv.Handle, windows.SC_STATUS_PROCESS_INFO, nil, 0, &bytesNeeded)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return 0, err
	}

	buf = make([]byte, bytesNeeded)
	p = (*windows.SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buf[0])) //nolint:gosec // G103: Valid use of unsafe call to create SERVICE_STATUS_PROCESS
	if err := windows.QueryServiceStatusEx(srv.Handle, windows.SC_STATUS_PROCESS_INFO, &buf[0], uint32(len(buf)), &bytesNeeded); err != nil {
		return 0, err
	}

	return p.ProcessId, nil
}

func collectMemmap(process, string, map[string]any) {}

func findBySystemdUnits([]string) ([]processGroup, error) {
	return nil, nil
}

func findByWindowsServices(services []string) ([]processGroup, error) {
	groups := make([]processGroup, 0, len(services))
	for _, service := range services {
		pid, err := queryPidWithWinServiceName(service)
		if err != nil {
			return nil, fmt.Errorf("failed to query PID of service %q: %w", service, err)
		}

		p, err := gopsprocess.NewProcess(int32(pid))
		if err != nil {
			return nil, fmt.Errorf("failed to find process for PID %d of service %q: %w", pid, service, err)
		}

		groups = append(groups, processGroup{
			processes: []*gopsprocess.Process{p},
			tags:      map[string]string{"win_service": service},
		})
	}

	return groups, nil
}

func collectTotalReadWrite(process) (r, w uint64, err error) {
	return 0, 0, errors.ErrUnsupported
}

func statsTCP(conns []gopsnet.ConnectionStat, _ uint8) ([]map[string]interface{}, error) {
	if len(conns) == 0 {
		return nil, nil
	}

	// Filter the responses via the inodes belonging to the process
	fieldslist := make([]map[string]interface{}, 0, len(conns))
	for _, c := range conns {
		var proto string
		switch c.Family {
		case syscall.AF_INET:
			proto = "tcp4"
		case syscall.AF_INET6:
			proto = "tcp6"
		default:
			continue
		}

		fields := map[string]interface{}{
			"protocol":  proto,
			"state":     c.Status,
			"pid":       c.Pid,
			"src":       c.Laddr.IP,
			"src_port":  c.Laddr.Port,
			"dest":      c.Raddr.IP,
			"dest_port": c.Raddr.Port,
		}
		fieldslist = append(fieldslist, fields)
	}

	return fieldslist, nil
}

func statsUDP(conns []gopsnet.ConnectionStat, _ uint8) ([]map[string]interface{}, error) {
	if len(conns) == 0 {
		return nil, nil
	}

	// Filter the responses via the inodes belonging to the process
	fieldslist := make([]map[string]interface{}, 0, len(conns))
	for _, c := range conns {
		var proto string
		switch c.Family {
		case syscall.AF_INET:
			proto = "udp4"
		case syscall.AF_INET6:
			proto = "udp6"
		default:
			continue
		}

		fields := map[string]interface{}{
			"protocol":  proto,
			"state":     c.Status,
			"pid":       c.Pid,
			"src":       c.Laddr.IP,
			"src_port":  c.Laddr.Port,
			"dest":      c.Raddr.IP,
			"dest_port": c.Raddr.Port,
		}
		fieldslist = append(fieldslist, fields)
	}

	return fieldslist, nil
}

func statsUnix([]gopsnet.ConnectionStat) ([]map[string]interface{}, error) {
	return nil, nil
}
