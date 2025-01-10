//go:build linux

package procstat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/prometheus/procfs"
	gopsnet "github.com/shirou/gopsutil/v4/net"
	gopsprocess "github.com/shirou/gopsutil/v4/process"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/influxdata/telegraf/internal"
)

func processName(p *gopsprocess.Process) (string, error) {
	return p.Exe()
}

func queryPidWithWinServiceName(_ string) (uint32, error) {
	return 0, errors.New("os not supporting win_service option")
}

func collectMemmap(proc process, prefix string, fields map[string]any) {
	memMapStats, err := proc.MemoryMaps(true)
	if err == nil && len(*memMapStats) == 1 {
		memMap := (*memMapStats)[0]
		fields[prefix+"memory_size"] = memMap.Size
		fields[prefix+"memory_pss"] = memMap.Pss
		fields[prefix+"memory_shared_clean"] = memMap.SharedClean
		fields[prefix+"memory_shared_dirty"] = memMap.SharedDirty
		fields[prefix+"memory_private_clean"] = memMap.PrivateClean
		fields[prefix+"memory_private_dirty"] = memMap.PrivateDirty
		fields[prefix+"memory_referenced"] = memMap.Referenced
		fields[prefix+"memory_anonymous"] = memMap.Anonymous
		fields[prefix+"memory_swap"] = memMap.Swap
	}
}

func findBySystemdUnits(units []string) ([]processGroup, error) {
	ctx := context.Background()
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to systemd: %w", err)
	}
	defer conn.Close()

	sdunits, err := conn.ListUnitsByPatternsContext(ctx, []string{"enabled", "disabled", "static"}, units)
	if err != nil {
		return nil, fmt.Errorf("failed to list units: %w", err)
	}

	groups := make([]processGroup, 0, len(sdunits))
	for _, u := range sdunits {
		prop, err := conn.GetUnitTypePropertyContext(ctx, u.Name, "Service", "MainPID")
		if err != nil {
			// This unit might not be a service or similar
			continue
		}
		raw := prop.Value.Value()
		pid, ok := raw.(uint32)
		if !ok {
			return nil, fmt.Errorf("failed to parse PID %v of unit %q: invalid type %T", raw, u, raw)
		}
		p, err := gopsprocess.NewProcess(int32(pid))
		if err != nil {
			return nil, fmt.Errorf("failed to find process for PID %d of unit %q: %w", pid, u, err)
		}
		groups = append(groups, processGroup{
			processes: []*gopsprocess.Process{p},
			tags:      map[string]string{"systemd_unit": u.Name},
		})
	}

	return groups, nil
}

func findByWindowsServices(_ []string) ([]processGroup, error) {
	return nil, nil
}

func collectTotalReadWrite(proc process) (r, w uint64, err error) {
	path := internal.GetProcPath()
	fs, err := procfs.NewFS(path)
	if err != nil {
		return 0, 0, err
	}

	p, err := fs.Proc(int(proc.pid()))
	if err != nil {
		return 0, 0, err
	}

	stat, err := p.IO()
	if err != nil {
		return 0, 0, err
	}

	return stat.RChar, stat.WChar, nil
}

/* Socket statistics functions */
func socketStateName(s uint8) string {
	switch s {
	case unix.BPF_TCP_ESTABLISHED:
		return "established"
	case unix.BPF_TCP_SYN_SENT:
		return "syn-sent"
	case unix.BPF_TCP_SYN_RECV:
		return "syn-recv"
	case unix.BPF_TCP_FIN_WAIT1:
		return "fin-wait1"
	case unix.BPF_TCP_FIN_WAIT2:
		return "fin-wait2"
	case unix.BPF_TCP_TIME_WAIT:
		return "time-wait"
	case unix.BPF_TCP_CLOSE:
		return "closed"
	case unix.BPF_TCP_CLOSE_WAIT:
		return "close-wait"
	case unix.BPF_TCP_LAST_ACK:
		return "last-ack"
	case unix.BPF_TCP_LISTEN:
		return "listen"
	case unix.BPF_TCP_CLOSING:
		return "closing"
	case unix.BPF_TCP_NEW_SYN_RECV:
		return "sync-recv"
	}

	return "unknown"
}

func socketTypeName(t uint8) string {
	switch t {
	case syscall.SOCK_STREAM:
		return "stream"
	case syscall.SOCK_DGRAM:
		return "dgram"
	case syscall.SOCK_RAW:
		return "raw"
	case syscall.SOCK_RDM:
		return "rdm"
	case syscall.SOCK_SEQPACKET:
		return "seqpacket"
	case syscall.SOCK_DCCP:
		return "dccp"
	case syscall.SOCK_PACKET:
		return "packet"
	}

	return "unknown"
}

func mapFdToInode(pid int32, fd uint32) (uint32, error) {
	root := internal.GetProcPath()
	fn := fmt.Sprintf("%s/%d/fd/%d", root, pid, fd)
	link, err := os.Readlink(fn)
	if err != nil {
		return 0, fmt.Errorf("reading link failed: %w", err)
	}
	target := strings.TrimPrefix(link, "socket:[")
	target = strings.TrimSuffix(target, "]")
	inode, err := strconv.ParseUint(target, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parsing link %q: %w", link, err)
	}

	return uint32(inode), nil
}

func statsTCP(conns []gopsnet.ConnectionStat, family uint8) ([]map[string]interface{}, error) {
	if len(conns) == 0 {
		return nil, nil
	}

	// For TCP we need the inode for each connection to relate the connection
	// statistics to the actual process socket. Therefore, map the
	// file-descriptors to inodes using the /proc/<pid>/fd entries.
	inodes := make(map[uint32]gopsnet.ConnectionStat, len(conns))
	for _, c := range conns {
		inode, err := mapFdToInode(c.Pid, c.Fd)
		if err != nil {
			return nil, fmt.Errorf("mapping fd %d of pid %d failed: %w", c.Fd, c.Pid, err)
		}
		inodes[inode] = c
	}

	// Get the TCP socket statistics from the netlink socket.
	responses, err := netlink.SocketDiagTCPInfo(family)
	if err != nil {
		return nil, fmt.Errorf("connecting to diag socket failed: %w", err)
	}

	// Filter the responses via the inodes belonging to the process
	fieldslist := make([]map[string]interface{}, 0)
	for _, r := range responses {
		c, found := inodes[r.InetDiagMsg.INode]
		if !found {
			// The inode does not belong to the process.
			continue
		}

		var proto string
		switch r.InetDiagMsg.Family {
		case syscall.AF_INET:
			proto = "tcp4"
		case syscall.AF_INET6:
			proto = "tcp6"
		default:
			continue
		}

		fields := map[string]interface{}{
			"protocol":       proto,
			"state":          socketStateName(r.InetDiagMsg.State),
			"pid":            c.Pid,
			"src":            r.InetDiagMsg.ID.Source.String(),
			"src_port":       r.InetDiagMsg.ID.SourcePort,
			"dest":           r.InetDiagMsg.ID.Destination.String(),
			"dest_port":      r.InetDiagMsg.ID.DestinationPort,
			"bytes_received": r.TCPInfo.Bytes_received,
			"bytes_sent":     r.TCPInfo.Bytes_sent,
			"lost":           r.TCPInfo.Lost,
			"retransmits":    r.TCPInfo.Retransmits,
			"rx_queue":       r.InetDiagMsg.RQueue,
			"tx_queue":       r.InetDiagMsg.WQueue,
		}
		fieldslist = append(fieldslist, fields)
	}

	return fieldslist, nil
}

func statsUDP(conns []gopsnet.ConnectionStat, family uint8) ([]map[string]interface{}, error) {
	if len(conns) == 0 {
		return nil, nil
	}

	// For UDP we need the inode for each connection to relate the connection
	// statistics to the actual process socket. Therefore, map the
	// file-descriptors to inodes using the /proc/<pid>/fd entries.
	inodes := make(map[uint32]gopsnet.ConnectionStat, len(conns))
	for _, c := range conns {
		inode, err := mapFdToInode(c.Pid, c.Fd)
		if err != nil {
			return nil, fmt.Errorf("mapping fd %d of pid %d failed: %w", c.Fd, c.Pid, err)
		}
		inodes[inode] = c
	}

	// Get the UDP socket statistics from the netlink socket.
	responses, err := netlink.SocketDiagUDPInfo(family)
	if err != nil {
		return nil, fmt.Errorf("connecting to diag socket failed: %w", err)
	}

	// Filter the responses via the inodes belonging to the process
	fieldslist := make([]map[string]interface{}, 0)
	for _, r := range responses {
		c, found := inodes[r.InetDiagMsg.INode]
		if !found {
			// The inode does not belong to the process.
			continue
		}

		var proto string
		switch r.InetDiagMsg.Family {
		case syscall.AF_INET:
			proto = "udp4"
		case syscall.AF_INET6:
			proto = "udp6"
		default:
			continue
		}

		fields := map[string]interface{}{
			"protocol":  proto,
			"state":     socketStateName(r.InetDiagMsg.State),
			"pid":       c.Pid,
			"src":       r.InetDiagMsg.ID.Source.String(),
			"src_port":  r.InetDiagMsg.ID.SourcePort,
			"dest":      r.InetDiagMsg.ID.Destination.String(),
			"dest_port": r.InetDiagMsg.ID.DestinationPort,
			"rx_queue":  r.InetDiagMsg.RQueue,
			"tx_queue":  r.InetDiagMsg.WQueue,
		}
		fieldslist = append(fieldslist, fields)
	}

	return fieldslist, nil
}

func statsUnix(conns []gopsnet.ConnectionStat) ([]map[string]interface{}, error) {
	if len(conns) == 0 {
		return nil, nil
	}

	// We need to read the inode for each connection to relate the connection
	// statistics to the actual process socket. Therefore, map the
	// file-descriptors to inodes using the /proc/<pid>/fd entries.
	inodes := make(map[uint32]gopsnet.ConnectionStat, len(conns))
	for _, c := range conns {
		inode, err := mapFdToInode(c.Pid, c.Fd)
		if err != nil {
			return nil, fmt.Errorf("mapping fd %d of pid %d failed: %w", c.Fd, c.Pid, err)
		}
		inodes[inode] = c
	}

	// Get the UDP socket statistics from the netlink socket.
	responses, err := netlink.UnixSocketDiagInfo()
	if err != nil {
		return nil, fmt.Errorf("connecting to diag socket failed: %w", err)
	}

	// Filter the responses via the inodes belonging to the process
	fieldslist := make([]map[string]interface{}, 0)
	for _, r := range responses {
		// Check if the inode belongs to the process and skip otherwise
		c, found := inodes[r.DiagMsg.INode]
		if !found {
			continue
		}

		name := c.Laddr.IP
		if name == "" {
			name = fmt.Sprintf("inode-%d", r.DiagMsg.INode)
		}

		fields := map[string]interface{}{
			"protocol": "unix",
			"type":     "stream",
			"state":    socketStateName(r.DiagMsg.State),
			"pid":      c.Pid,
			"name":     name,
			"rx_queue": r.Queue.RQueue,
			"tx_queue": r.Queue.WQueue,
			"inode":    r.DiagMsg.INode,
		}
		if r.Peer != nil {
			fields["peer"] = *r.Peer
		}
		fieldslist = append(fieldslist, fields)
	}

	// Diagnosis only works for stream sockets, so add all non-stream sockets
	// of the process without further data
	for inode, c := range inodes {
		if c.Type == syscall.SOCK_STREAM {
			continue
		}

		name := c.Laddr.IP
		if name == "" {
			name = fmt.Sprintf("inode-%d", inode)
		}

		fields := map[string]interface{}{
			"protocol": "unix",
			"type":     socketTypeName(uint8(c.Type)),
			"state":    "close",
			"pid":      c.Pid,
			"name":     name,
			"rx_queue": uint32(0),
			"tx_queue": uint32(0),
			"inode":    inode,
		}
		fieldslist = append(fieldslist, fields)
	}

	return fieldslist, nil
}
