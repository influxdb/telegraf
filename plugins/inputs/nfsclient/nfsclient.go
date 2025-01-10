//go:generate ../../../tools/readme_config_includer/generator
package nfsclient

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type NFSClient struct {
	Fullstat          bool            `toml:"fullstat"`
	IncludeMounts     []string        `toml:"include_mounts"`
	ExcludeMounts     []string        `toml:"exclude_mounts"`
	IncludeOperations []string        `toml:"include_operations"`
	ExcludeOperations []string        `toml:"exclude_operations"`
	Log               telegraf.Logger `toml:"-"`
	nfs3Ops           map[string]bool
	nfs4Ops           map[string]bool
	mountstatsPath    string
}

func (*NFSClient) SampleConfig() string {
	return sampleConfig
}

func (n *NFSClient) Init() error {
	var nfs3Fields = []string{
		"NULL",
		"GETATTR",
		"SETATTR",
		"LOOKUP",
		"ACCESS",
		"READLINK",
		"READ",
		"WRITE",
		"CREATE",
		"MKDIR",
		"SYMLINK",
		"MKNOD",
		"REMOVE",
		"RMDIR",
		"RENAME",
		"LINK",
		"READDIR",
		"READDIRPLUS",
		"FSSTAT",
		"FSINFO",
		"PATHCONF",
		"COMMIT",
	}

	var nfs4Fields = []string{
		"NULL",
		"READ",
		"WRITE",
		"COMMIT",
		"OPEN",
		"OPEN_CONFIRM",
		"OPEN_NOATTR",
		"OPEN_DOWNGRADE",
		"CLOSE",
		"SETATTR",
		"FSINFO",
		"RENEW",
		"SETCLIENTID",
		"SETCLIENTID_CONFIRM",
		"LOCK",
		"LOCKT",
		"LOCKU",
		"ACCESS",
		"GETATTR",
		"LOOKUP",
		"LOOKUP_ROOT",
		"REMOVE",
		"RENAME",
		"LINK",
		"SYMLINK",
		"CREATE",
		"PATHCONF",
		"STATFS",
		"READLINK",
		"READDIR",
		"SERVER_CAPS",
		"DELEGRETURN",
		"GETACL",
		"SETACL",
		"FS_LOCATIONS",
		"RELEASE_LOCKOWNER",
		"SECINFO",
		"FSID_PRESENT",
		"EXCHANGE_ID",
		"CREATE_SESSION",
		"DESTROY_SESSION",
		"SEQUENCE",
		"GET_LEASE_TIME",
		"RECLAIM_COMPLETE",
		"LAYOUTGET",
		"GETDEVICEINFO",
		"LAYOUTCOMMIT",
		"LAYOUTRETURN",
		"SECINFO_NO_NAME",
		"TEST_STATEID",
		"FREE_STATEID",
		"GETDEVICELIST",
		"BIND_CONN_TO_SESSION",
		"DESTROY_CLIENTID",
		"SEEK",
		"ALLOCATE",
		"DEALLOCATE",
		"LAYOUTSTATS",
		"CLONE",
		"COPY",
		"OFFLOAD_CANCEL",
		"LOOKUPP",
		"LAYOUTERROR",
		"COPY_NOTIFY",
		"GETXATTR",
		"SETXATTR",
		"LISTXATTRS",
		"REMOVEXATTR",
	}

	nfs3Ops := make(map[string]bool)
	nfs4Ops := make(map[string]bool)

	n.mountstatsPath = n.getMountStatsPath()

	if len(n.IncludeOperations) == 0 {
		for _, Op := range nfs3Fields {
			nfs3Ops[Op] = true
		}
		for _, Op := range nfs4Fields {
			nfs4Ops[Op] = true
		}
	} else {
		for _, Op := range n.IncludeOperations {
			nfs3Ops[Op] = true
		}
		for _, Op := range n.IncludeOperations {
			nfs4Ops[Op] = true
		}
	}

	if len(n.ExcludeOperations) > 0 {
		for _, Op := range n.ExcludeOperations {
			if nfs3Ops[Op] {
				delete(nfs3Ops, Op)
			}
			if nfs4Ops[Op] {
				delete(nfs4Ops, Op)
			}
		}
	}

	n.nfs3Ops = nfs3Ops
	n.nfs4Ops = nfs4Ops

	if len(n.IncludeMounts) > 0 {
		n.Log.Debugf("Including these mount patterns: %v", n.IncludeMounts)
	} else {
		n.Log.Debugf("Including all mounts.")
	}

	if len(n.ExcludeMounts) > 0 {
		n.Log.Debugf("Excluding these mount patterns: %v", n.ExcludeMounts)
	} else {
		n.Log.Debugf("Not excluding any mounts.")
	}

	if len(n.IncludeOperations) > 0 {
		n.Log.Debugf("Including these operations: %v", n.IncludeOperations)
	} else {
		n.Log.Debugf("Including all operations.")
	}

	if len(n.ExcludeOperations) > 0 {
		n.Log.Debugf("Excluding these mount patterns: %v", n.ExcludeOperations)
	} else {
		n.Log.Debugf("Not excluding any operations.")
	}

	return nil
}

func (n *NFSClient) Gather(acc telegraf.Accumulator) error {
	if _, err := os.Stat(n.mountstatsPath); os.IsNotExist(err) {
		return err
	}

	// Attempt to read the file to see if we have permissions before opening
	// which can lead to a panic
	if _, err := os.ReadFile(n.mountstatsPath); err != nil {
		return err
	}

	file, err := os.Open(n.mountstatsPath)
	if err != nil {
		n.Log.Errorf("Failed opening the %q file: %v ", file.Name(), err)
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if err := n.processText(scanner, acc); err != nil {
		return err
	}

	return scanner.Err()
}

func (n *NFSClient) parseStat(mountpoint, export, version string, line []string, acc telegraf.Accumulator) error {
	tags := map[string]string{"mountpoint": mountpoint, "serverexport": export}
	nline, err := convertToUint64(line)
	if err != nil {
		return err
	}

	if len(nline) == 0 {
		n.Log.Warnf("Parsing Stat line with one field: %s\n", line)
		return nil
	}

	first := strings.Replace(line[0], ":", "", 1)

	var eventsFields = []string{
		"inoderevalidates",
		"dentryrevalidates",
		"datainvalidates",
		"attrinvalidates",
		"vfsopen",
		"vfslookup",
		"vfsaccess",
		"vfsupdatepage",
		"vfsreadpage",
		"vfsreadpages",
		"vfswritepage",
		"vfswritepages",
		"vfsgetdents",
		"vfssetattr",
		"vfsflush",
		"vfsfsync",
		"vfslock",
		"vfsrelease",
		"congestionwait",
		"setattrtrunc",
		"extendwrite",
		"sillyrenames",
		"shortreads",
		"shortwrites",
		"delay",
		"pnfsreads",
		"pnfswrites",
	}

	var bytesFields = []string{
		"normalreadbytes",
		"normalwritebytes",
		"directreadbytes",
		"directwritebytes",
		"serverreadbytes",
		"serverwritebytes",
		"readpages",
		"writepages",
	}

	var xprtudpFields = []string{
		"bind_count",
		"rpcsends",
		"rpcreceives",
		"badxids",
		"inflightsends",
		"backlogutil",
	}

	var xprttcpFields = []string{
		"bind_count",
		"connect_count",
		"connect_time",
		"idle_time",
		"rpcsends",
		"rpcreceives",
		"badxids",
		"inflightsends",
		"backlogutil",
	}

	var nfsopFields = []string{
		"ops",
		"trans",
		"timeouts",
		"bytes_sent",
		"bytes_recv",
		"queue_time",
		"response_time",
		"total_time",
		"errors",
	}

	var fields = make(map[string]interface{})

	switch first {
	case "READ", "WRITE":
		fields["ops"] = nline[0]
		fields["retrans"] = nline[1] - nline[0]
		fields["bytes"] = nline[3] + nline[4]
		fields["rtt"] = nline[6]
		fields["exe"] = nline[7]
		fields["rtt_per_op"] = 0.0
		if nline[0] > 0 {
			fields["rtt_per_op"] = float64(nline[6]) / float64(nline[0])
		}
		tags["operation"] = first
		acc.AddFields("nfsstat", fields, tags)
	}

	if n.Fullstat {
		switch first {
		case "events":
			if len(nline) >= len(eventsFields) {
				for i, t := range eventsFields {
					fields[t] = nline[i]
				}
				acc.AddFields("nfs_events", fields, tags)
			}

		case "bytes":
			if len(nline) >= len(bytesFields) {
				for i, t := range bytesFields {
					fields[t] = nline[i]
				}
				acc.AddFields("nfs_bytes", fields, tags)
			}

		case "xprt":
			if len(line) > 1 {
				switch line[1] {
				case "tcp":
					if len(nline)+2 >= len(xprttcpFields) {
						for i, t := range xprttcpFields {
							fields[t] = nline[i+2]
						}
						acc.AddFields("nfs_xprt_tcp", fields, tags)
					}
				case "udp":
					if len(nline)+2 >= len(xprtudpFields) {
						for i, t := range xprtudpFields {
							fields[t] = nline[i+2]
						}
						acc.AddFields("nfs_xprt_udp", fields, tags)
					}
				}
			}
		}

		if (version == "3" && n.nfs3Ops[first]) || (version == "4" && n.nfs4Ops[first]) {
			tags["operation"] = first
			if len(nline) <= len(nfsopFields) {
				for i, t := range nline {
					fields[nfsopFields[i]] = t
				}
				acc.AddFields("nfs_ops", fields, tags)
			}
		}
	}

	return nil
}

func (n *NFSClient) processText(scanner *bufio.Scanner, acc telegraf.Accumulator) error {
	var mount string
	var version string
	var export string
	var skip bool

	for scanner.Scan() {
		line := strings.Fields(scanner.Text())
		lineLength := len(line)

		if lineLength == 0 {
			continue
		}

		skip = false

		// This denotes a new mount has been found, so set
		// mount and export, and stop skipping (for now)
		if lineLength > 4 && choice.Contains("fstype", line) && (choice.Contains("nfs", line) || choice.Contains("nfs4", line)) {
			mount = line[4]
			export = line[1]
		} else if lineLength > 5 && (choice.Contains("(nfs)", line) || choice.Contains("(nfs4)", line)) {
			version = strings.Split(line[5], "/")[1]
		}

		if mount == "" {
			continue
		}

		if len(n.IncludeMounts) > 0 {
			skip = true
			for _, RE := range n.IncludeMounts {
				matched, err := regexp.MatchString(RE, mount)
				if matched && err != nil {
					skip = false
					break
				}
			}
		}

		if !skip && len(n.ExcludeMounts) > 0 {
			for _, RE := range n.ExcludeMounts {
				matched, err := regexp.MatchString(RE, mount)
				if matched && err != nil {
					skip = true
					break
				}
			}
		}

		if !skip {
			err := n.parseStat(mount, export, version, line, acc)
			if err != nil {
				return fmt.Errorf("could not parseStat: %w", err)
			}
		}
	}

	return nil
}

func (n *NFSClient) getMountStatsPath() string {
	path := "/proc/self/mountstats"
	if os.Getenv("MOUNT_PROC") != "" {
		path = os.Getenv("MOUNT_PROC")
	}
	n.Log.Debugf("using [%s] for mountstats", path)
	return path
}

func convertToUint64(line []string) ([]uint64, error) {
	/* A "line" of input data (a pre-split array of strings) is
	   processed one field at a time.  Each field is converted to
	   an uint64 value, and appended to an array of return values.
	   On an error, check for ErrRange, and returns an error
	   if found.  This situation indicates a pretty major issue in
	   the /proc/self/mountstats file, and returning faulty data
	   is worse than no data.  Other errors are ignored, and append
	   whatever we got in the first place (probably 0).
	   Yes, this is ugly. */

	if len(line) < 2 {
		return nil, nil
	}

	nline := make([]uint64, 0, len(line[1:]))
	// Skip the first field; it's handled specially as the "first" variable
	for _, l := range line[1:] {
		val, err := strconv.ParseUint(l, 10, 64)
		if err != nil {
			var numError *strconv.NumError
			if errors.As(err, &numError) {
				if errors.Is(numError.Err, strconv.ErrRange) {
					return nil, fmt.Errorf("errrange: line:[%v] raw:[%v] -> parsed:[%v]", line, l, val)
				}
			}
		}
		nline = append(nline, val)
	}
	return nline, nil
}

func init() {
	inputs.Add("nfsclient", func() telegraf.Input {
		return &NFSClient{}
	})
}
