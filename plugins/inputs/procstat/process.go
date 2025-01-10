package procstat

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"time"

	gopsnet "github.com/shirou/gopsutil/v4/net"
	gopsprocess "github.com/shirou/gopsutil/v4/process"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

type process interface {
	Name() (string, error)
	MemoryMaps(bool) (*[]gopsprocess.MemoryMapsStat, error)
	pid() pid
	setTag(string, string)
	metrics(string, *collectionConfig, time.Time) ([]telegraf.Metric, error)
}

type pidFinder interface {
	pidFile(path string) ([]pid, error)
	pattern(pattern string) ([]pid, error)
	uid(user string) ([]pid, error)
	fullPattern(path string) ([]pid, error)
	children(pid pid) ([]pid, error)
}

type proc struct {
	hasCPUTimes bool
	tags        map[string]string
	*gopsprocess.Process
}

func newProc(pid pid) (process, error) {
	p, err := gopsprocess.NewProcess(int32(pid))
	if err != nil {
		return nil, err
	}

	proc := &proc{
		Process:     p,
		hasCPUTimes: false,
		tags:        make(map[string]string),
	}
	return proc, nil
}

func (p *proc) pid() pid {
	return pid(p.Process.Pid)
}

func (p *proc) setTag(k, v string) {
	p.tags[k] = v
}

func (p *proc) percent(_ time.Duration) (float64, error) {
	cpuPerc, err := p.Process.Percent(time.Duration(0))
	if !p.hasCPUTimes && err == nil {
		p.hasCPUTimes = true
		return 0, errors.New("must call Percent twice to compute percent cpu")
	}
	return cpuPerc, err
}

// Add metrics a single process
func (p *proc) metrics(prefix string, cfg *collectionConfig, t time.Time) ([]telegraf.Metric, error) {
	if prefix != "" {
		prefix += "_"
	}

	fields := make(map[string]interface{})
	numThreads, err := p.NumThreads()
	if err == nil {
		fields[prefix+"num_threads"] = numThreads
	}

	fds, err := p.NumFDs()
	if err == nil {
		fields[prefix+"num_fds"] = fds
	}

	ctx, err := p.NumCtxSwitches()
	if err == nil {
		fields[prefix+"voluntary_context_switches"] = ctx.Voluntary
		fields[prefix+"involuntary_context_switches"] = ctx.Involuntary
	}

	faults, err := p.PageFaults()
	if err == nil {
		fields[prefix+"minor_faults"] = faults.MinorFaults
		fields[prefix+"major_faults"] = faults.MajorFaults
		fields[prefix+"child_minor_faults"] = faults.ChildMinorFaults
		fields[prefix+"child_major_faults"] = faults.ChildMajorFaults
	}

	io, err := p.IOCounters()
	if err == nil {
		fields[prefix+"read_count"] = io.ReadCount
		fields[prefix+"write_count"] = io.WriteCount
		fields[prefix+"read_bytes"] = io.ReadBytes
		fields[prefix+"write_bytes"] = io.WriteBytes
	}

	// Linux fixup for gopsutils exposing the disk-only-IO instead of the total
	// I/O as for example on Windows
	if rc, wc, err := collectTotalReadWrite(p); err == nil {
		fields[prefix+"read_bytes"] = rc
		fields[prefix+"write_bytes"] = wc
		fields[prefix+"disk_read_bytes"] = io.ReadBytes
		fields[prefix+"disk_write_bytes"] = io.WriteBytes
	}

	createdAt, err := p.CreateTime() // returns epoch in ms
	if err == nil {
		fields[prefix+"created_at"] = createdAt * 1000000 // ms to ns
	}

	if cfg.features["cpu"] {
		cpuTime, err := p.Times()
		if err == nil {
			fields[prefix+"cpu_time_user"] = cpuTime.User
			fields[prefix+"cpu_time_system"] = cpuTime.System
			fields[prefix+"cpu_time_iowait"] = cpuTime.Iowait // only reported on Linux
		}

		cpuPerc, err := p.percent(time.Duration(0))
		if err == nil {
			if cfg.solarisMode {
				fields[prefix+"cpu_usage"] = cpuPerc / float64(runtime.NumCPU())
			} else {
				fields[prefix+"cpu_usage"] = cpuPerc
			}
		}
	}

	// This only returns values for RSS and VMS
	if cfg.features["memory"] {
		mem, err := p.MemoryInfo()
		if err == nil {
			fields[prefix+"memory_rss"] = mem.RSS
			fields[prefix+"memory_vms"] = mem.VMS
		}

		memPerc, err := p.MemoryPercent()
		if err == nil {
			fields[prefix+"memory_usage"] = memPerc
		}
	}

	if cfg.features["mmap"] {
		collectMemmap(p, prefix, fields)
	}

	if cfg.features["limits"] {
		rlims, err := p.RlimitUsage(true)
		if err == nil {
			for _, rlim := range rlims {
				var name string
				switch rlim.Resource {
				case gopsprocess.RLIMIT_CPU:
					name = "cpu_time"
				case gopsprocess.RLIMIT_DATA:
					name = "memory_data"
				case gopsprocess.RLIMIT_STACK:
					name = "memory_stack"
				case gopsprocess.RLIMIT_RSS:
					name = "memory_rss"
				case gopsprocess.RLIMIT_NOFILE:
					name = "num_fds"
				case gopsprocess.RLIMIT_MEMLOCK:
					name = "memory_locked"
				case gopsprocess.RLIMIT_AS:
					name = "memory_vms"
				case gopsprocess.RLIMIT_LOCKS:
					name = "file_locks"
				case gopsprocess.RLIMIT_SIGPENDING:
					name = "signals_pending"
				case gopsprocess.RLIMIT_NICE:
					name = "nice_priority"
				case gopsprocess.RLIMIT_RTPRIO:
					name = "realtime_priority"
				default:
					continue
				}

				fields[prefix+"rlimit_"+name+"_soft"] = rlim.Soft
				fields[prefix+"rlimit_"+name+"_hard"] = rlim.Hard
				if name != "file_locks" { // gopsutil doesn't currently track the used file locks count
					fields[prefix+name] = rlim.Used
				}
			}
		}
	}

	// Add the tags as requested by the user
	cmdline, err := p.Cmdline()
	if err == nil {
		if cfg.tagging["cmdline"] {
			p.tags["cmdline"] = cmdline
		} else {
			fields[prefix+"cmdline"] = cmdline
		}
	}

	if cfg.tagging["pid"] {
		p.tags["pid"] = strconv.Itoa(int(p.Pid))
	} else {
		fields["pid"] = p.Pid
	}

	ppid, err := p.Ppid()
	if err == nil {
		if cfg.tagging["ppid"] {
			p.tags["ppid"] = strconv.Itoa(int(ppid))
		} else {
			fields[prefix+"ppid"] = ppid
		}
	}

	status, err := p.Status()
	if err == nil {
		if cfg.tagging["status"] {
			p.tags["status"] = status[0]
		} else {
			fields[prefix+"status"] = status[0]
		}
	}

	user, err := p.Username()
	if err == nil {
		if cfg.tagging["user"] {
			p.tags["user"] = user
		} else {
			fields[prefix+"user"] = user
		}
	}

	if _, exists := p.tags["process_name"]; !exists {
		name, err := p.Name()
		if err == nil {
			p.tags["process_name"] = name
		}
	}

	metrics := []telegraf.Metric{metric.New("procstat", p.tags, fields, t)}

	// Collect the socket statistics if requested
	if cfg.features["sockets"] {
		for _, protocol := range cfg.socketProtos {
			// Get the requested connections for the PID
			var fieldlist []map[string]interface{}
			switch protocol {
			case "all":
				conns, err := gopsnet.ConnectionsPid(protocol, p.Pid)
				if err != nil {
					return metrics, fmt.Errorf("cannot get connections for %q of PID %d", protocol, p.Pid)
				}
				var connsTCPv4, connsTCPv6, connsUDPv4, connsUDPv6, connsUnix []gopsnet.ConnectionStat
				for _, c := range conns {
					switch {
					case c.Family == syscall.AF_INET && c.Type == syscall.SOCK_STREAM:
						connsTCPv4 = append(connsTCPv4, c)
					case c.Family == syscall.AF_INET6 && c.Type == syscall.SOCK_STREAM:
						connsTCPv6 = append(connsTCPv6, c)
					case c.Family == syscall.AF_INET && c.Type == syscall.SOCK_DGRAM:
						connsUDPv4 = append(connsUDPv4, c)
					case c.Family == syscall.AF_INET6 && c.Type == syscall.SOCK_DGRAM:
						connsUDPv6 = append(connsUDPv6, c)
					case c.Family == syscall.AF_UNIX:
						connsUnix = append(connsUnix, c)
					}
				}
				fl, err := statsTCP(connsTCPv4, syscall.AF_INET)
				if err != nil {
					return metrics, fmt.Errorf("cannot get statistics for \"tcp4\" of PID %d", p.Pid)
				}
				fieldlist = append(fieldlist, fl...)

				fl, err = statsTCP(connsTCPv6, syscall.AF_INET6)
				if err != nil {
					return metrics, fmt.Errorf("cannot get statistics for \"tcp6\" of PID %d", p.Pid)
				}
				fieldlist = append(fieldlist, fl...)

				fl, err = statsUDP(connsUDPv4, syscall.AF_INET)
				if err != nil {
					return metrics, fmt.Errorf("cannot get statistics for \"udp4\" of PID %d", p.Pid)
				}
				fieldlist = append(fieldlist, fl...)

				fl, err = statsUDP(connsUDPv6, syscall.AF_INET6)
				if err != nil {
					return metrics, fmt.Errorf("cannot get statistics for \"udp6\" of PID %d", p.Pid)
				}
				fieldlist = append(fieldlist, fl...)

				fl, err = statsUnix(connsUnix)
				if err != nil {
					return metrics, fmt.Errorf("cannot get statistics for \"unix\" of PID %d", p.Pid)
				}
				fieldlist = append(fieldlist, fl...)
			case "tcp4", "tcp6":
				family := uint8(syscall.AF_INET)
				if protocol == "tcp6" {
					family = syscall.AF_INET6
				}
				conns, err := gopsnet.ConnectionsPid(protocol, p.Pid)
				if err != nil {
					return metrics, fmt.Errorf("cannot get connections for %q of PID %d", protocol, p.Pid)
				}
				if fieldlist, err = statsTCP(conns, family); err != nil {
					return metrics, fmt.Errorf("cannot get statistics for %q of PID %d", protocol, p.Pid)
				}
			case "udp4", "udp6":
				family := uint8(syscall.AF_INET)
				if protocol == "udp6" {
					family = syscall.AF_INET6
				}
				conns, err := gopsnet.ConnectionsPid(protocol, p.Pid)
				if err != nil {
					return metrics, fmt.Errorf("cannot get connections for %q of PID %d", protocol, p.Pid)
				}
				if fieldlist, err = statsUDP(conns, family); err != nil {
					return metrics, fmt.Errorf("cannot get statistics for %q of PID %d", protocol, p.Pid)
				}
			case "unix":
				conns, err := gopsnet.ConnectionsPid(protocol, p.Pid)
				if err != nil {
					return metrics, fmt.Errorf("cannot get connections for %q of PID %d", protocol, p.Pid)
				}
				if fieldlist, err = statsUnix(conns); err != nil {
					return metrics, fmt.Errorf("cannot get statistics for %q of PID %d", protocol, p.Pid)
				}
			}

			for _, fields := range fieldlist {
				if cfg.tagging["protocol"] {
					p.tags["protocol"] = fields["protocol"].(string)
					delete(fields, "protocol")
				}
				if cfg.tagging["state"] {
					p.tags["state"] = fields["state"].(string)
					delete(fields, "state")
				}
				if cfg.tagging["src"] && fields["src"] != nil {
					p.tags["src"] = fields["src"].(string)
					delete(fields, "src")
				}
				if cfg.tagging["src_port"] && fields["src_port"] != nil {
					port := uint64(fields["src_port"].(uint16))
					p.tags["src_port"] = strconv.FormatUint(port, 10)
					delete(fields, "src_port")
				}
				if cfg.tagging["dest"] && fields["dest"] != nil {
					p.tags["dest"] = fields["dest"].(string)
					delete(fields, "dest")
				}
				if cfg.tagging["dest_port"] && fields["dest_port"] != nil {
					port := uint64(fields["dest_port"].(uint16))
					p.tags["dest_port"] = strconv.FormatUint(port, 10)
					delete(fields, "dest_port")
				}
				if cfg.tagging["name"] && fields["name"] != nil {
					p.tags["name"] = fields["name"].(string)
					delete(fields, "name")
				}

				metrics = append(metrics, metric.New("procstat_socket", p.tags, fields, t))
			}
		}
	}

	return metrics, nil
}
