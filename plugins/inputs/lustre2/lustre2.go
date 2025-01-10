//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

// Package lustre2 (doesn't aim for Windows)
// Lustre 2.x Telegraf plugin
// Lustre (http://lustre.org/) is an open-source, parallel file system
// for HPC environments. It stores statistics about its activity in /proc
package lustre2

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Lustre2 struct {
	// Lustre proc files can change between versions, so we want to future-proof by letting people choose what to look at.
	MgsProcfiles []string        `toml:"mgs_procfiles"`
	OstProcfiles []string        `toml:"ost_procfiles"`
	MdsProcfiles []string        `toml:"mds_procfiles"`
	Log          telegraf.Logger `toml:"-"`

	// used by the testsuite to generate mock sysfs and procfs files
	rootdir string

	// allFields maps an OST name to the metric fields associated with that OST
	allFields map[tags]map[string]interface{}
}

type tags struct {
	name, brwSection, bucket, job, client string
}

func (*Lustre2) SampleConfig() string {
	return sampleConfig
}

func (l *Lustre2) Gather(acc telegraf.Accumulator) error {
	l.allFields = make(map[tags]map[string]interface{})

	err := l.getLustreHealth()
	if err != nil {
		return err
	}

	if len(l.MgsProcfiles) == 0 {
		l.MgsProcfiles = []string{
			// eviction count
			"/sys/fs/lustre/mgs/*/eviction_count",
		}
	}

	if len(l.OstProcfiles) == 0 {
		l.OstProcfiles = []string{
			// read/write bytes are in obdfilter/<ost_name>/stats
			"/proc/fs/lustre/obdfilter/*/stats",
			// cache counters are in osd-ldiskfs/<ost_name>/stats
			"/proc/fs/lustre/osd-ldiskfs/*/stats",
			// per job statistics are in obdfilter/<ost_name>/job_stats
			"/proc/fs/lustre/obdfilter/*/job_stats",
			// bulk read/write statistics for ldiskfs
			"/proc/fs/lustre/osd-ldiskfs/*/brw_stats",
			// bulk read/write statistics for zfs
			"/proc/fs/lustre/osd-zfs/*/brw_stats",
			// eviction count
			"/sys/fs/lustre/obdfilter/*/eviction_count",
		}
	}

	if len(l.MdsProcfiles) == 0 {
		l.MdsProcfiles = []string{
			// Metadata server stats
			"/proc/fs/lustre/mdt/*/md_stats",
			// Metadata target job stats
			"/proc/fs/lustre/mdt/*/job_stats",
			// eviction count
			"/sys/fs/lustre/mdt/*/eviction_count",
		}
	}

	for _, procfile := range l.MgsProcfiles {
		if !strings.HasSuffix(procfile, "eviction_count") {
			return fmt.Errorf("no handler found for mgs procfile pattern \"%s\"", procfile)
		}
		err := l.getLustreEvictionCount(procfile)
		if err != nil {
			return err
		}
	}
	for _, procfile := range l.OstProcfiles {
		if strings.HasSuffix(procfile, "brw_stats") {
			err := l.getLustreProcBrwStats(procfile, wantedBrwstatsFields)
			if err != nil {
				return err
			}
		} else if strings.HasSuffix(procfile, "job_stats") {
			err := l.getLustreProcStats(procfile, wantedOstJobstatsFields)
			if err != nil {
				return err
			}
		} else if strings.HasSuffix(procfile, "eviction_count") {
			err := l.getLustreEvictionCount(procfile)
			if err != nil {
				return err
			}
		} else {
			err := l.getLustreProcStats(procfile, wantedOstFields)
			if err != nil {
				return err
			}
		}
	}
	for _, procfile := range l.MdsProcfiles {
		if strings.HasSuffix(procfile, "brw_stats") {
			err := l.getLustreProcBrwStats(procfile, wantedBrwstatsFields)
			if err != nil {
				return err
			}
		} else if strings.HasSuffix(procfile, "job_stats") {
			err := l.getLustreProcStats(procfile, wantedMdtJobstatsFields)
			if err != nil {
				return err
			}
		} else if strings.HasSuffix(procfile, "eviction_count") {
			err := l.getLustreEvictionCount(procfile)
			if err != nil {
				return err
			}
		} else {
			err := l.getLustreProcStats(procfile, wantedMdsFields)
			if err != nil {
				return err
			}
		}
	}

	for tgs, fields := range l.allFields {
		tags := make(map[string]string, 5)
		if len(tgs.name) > 0 {
			tags["name"] = tgs.name
		}
		if len(tgs.brwSection) > 0 {
			tags["brw_section"] = tgs.brwSection
		}
		if len(tgs.bucket) > 0 {
			tags["bucket"] = tgs.bucket
		}
		if len(tgs.job) > 0 {
			tags["jobid"] = tgs.job
		}
		if len(tgs.client) > 0 {
			tags["client"] = tgs.client
		}
		acc.AddFields("lustre2", fields, tags)
	}

	return nil
}

func (l *Lustre2) getLustreHealth() error {
	// the linter complains about using an element containing '/' in filepath.Join()
	// so we explicitly set the rootdir default to '/' in this function rather than
	// starting the second element with a '/'.
	rootdir := l.rootdir
	if rootdir == "" {
		rootdir = "/"
	}

	filename := filepath.Join(rootdir, "sys", "fs", "lustre", "health_check")
	if _, err := os.Stat(filename); err != nil {
		// try falling back to the old procfs location
		// it was moved in https://github.com/lustre/lustre-release/commit/5d368bd0b2
		filename = filepath.Join(rootdir, "proc", "fs", "lustre", "health_check")
		if _, err = os.Stat(filename); err != nil {
			return nil //nolint:nilerr // we don't want to return an error if the file doesn't exist
		}
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	value := strings.TrimSpace(string(contents))
	var health uint64
	if value == "healthy" {
		health = 1
	}

	t := tags{}
	var fields map[string]interface{}
	fields, ok := l.allFields[t]
	if !ok {
		fields = make(map[string]interface{})
		l.allFields[t] = fields
	}

	fields["health"] = health
	return nil
}

func (l *Lustre2) getLustreProcStats(fileglob string, wantedFields []*mapping) error {
	files, err := filepath.Glob(filepath.Join(l.rootdir, fileglob))
	if err != nil {
		return err
	}

	fieldSplitter := regexp.MustCompile(`[ :]+`)

	for _, file := range files {
		/* From /proc/fs/lustre/obdfilter/<ost_name>/stats and similar
		 * extract the object store target name,
		 * and for per-client files under
		 * /proc/fs/lustre/obdfilter/<ost_name>/exports/<client_nid>/stats
		 * and similar the client NID
		 * Assumption: the target name is fourth to last
		 * for per-client files and second to last otherwise
		 * and the client NID is always second to last,
		 * which is true in Lustre 2.1->2.14
		 */
		path := strings.Split(file, "/")
		var name, client string
		if strings.Contains(file, "/exports/") {
			name = path[len(path)-4]
			client = path[len(path)-2]
		} else {
			name = path[len(path)-2]
			client = ""
		}

		wholeFile, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		jobs := strings.Split(string(wholeFile), "- ")
		for _, job := range jobs {
			lines := strings.Split(job, "\n")
			jobid := ""

			// figure out if the data should be tagged with job_id here
			parts := strings.Fields(lines[0])
			if strings.TrimSuffix(parts[0], ":") == "job_id" {
				jobid = parts[1]
			}

			for _, line := range lines {
				// skip any empty lines
				if len(line) < 1 {
					continue
				}

				parts := fieldSplitter.Split(line, -1)
				if len(parts[0]) == 0 {
					parts = parts[1:]
				}

				var fields map[string]interface{}
				fields, ok := l.allFields[tags{name, "", "", jobid, client}]
				if !ok {
					fields = make(map[string]interface{})
					l.allFields[tags{name, "", "", jobid, client}] = fields
				}

				for _, wanted := range wantedFields {
					var data uint64
					if parts[0] == wanted.inProc {
						wantedField := wanted.field
						// if not set, assume field[1]. Shouldn't be field[0], as
						// that's a string
						if wantedField == 0 {
							wantedField = 1
						}
						data, err = strconv.ParseUint(strings.TrimSuffix(parts[wantedField], ","), 10, 64)
						if err != nil {
							return err
						}
						reportName := wanted.inProc
						if wanted.reportAs != "" {
							reportName = wanted.reportAs
						}
						fields[reportName] = data
					}
				}
			}
		}
	}
	return nil
}

func (l *Lustre2) getLustreProcBrwStats(fileglob string, wantedFields []*mapping) error {
	files, err := filepath.Glob(filepath.Join(l.rootdir, fileglob))
	if err != nil {
		return fmt.Errorf("failed to find files matching glob %s: %w", fileglob, err)
	}

	for _, file := range files {
		// Turn /proc/fs/lustre/obdfilter/<ost_name>/stats and similar into just the object store target name
		// This assumes that the target name is always second to last, which is true in Lustre 2.1->2.12
		path := strings.Split(file, "/")
		if len(path) < 2 {
			continue
		}
		name := path[len(path)-2]

		wholeFile, err := os.ReadFile(file)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				l.Log.Debugf("%s", err)
				continue
			}
			return fmt.Errorf("failed to read file %s: %w", file, err)
		}
		lines := strings.Split(string(wholeFile), "\n")

		var headerName string
		for _, line := range lines {
			// There are four types of lines in a brw_stats file:
			// 1. Header lines - contain the category of metric (e.g. disk I/Os in flight, disk I/O time)
			// 2. Bucket lines - follow headers, contain the bucket value (e.g. 4K, 1M) and metric values
			// 3. Empty lines - these will simply be filtered out
			// 4. snapshot_time line - this will be filtered out, as it "looks" like a bucket line
			if len(line) < 1 {
				continue
			}
			parts := strings.Fields(line)

			// This is a header line
			// Set report name for use by the buckets that follow
			if !strings.Contains(parts[0], ":") {
				nameParts := strings.Split(line, "  ")
				headerName = nameParts[0]
				continue
			}

			// snapshot_time should be discarded
			if strings.Contains(parts[0], "snapshot_time") {
				continue
			}

			// This is a bucket for a given header
			for _, wanted := range wantedFields {
				if headerName != wanted.inProc {
					continue
				}
				bucket := strings.TrimSuffix(parts[0], ":")

				// brw_stats columns are static and don't need configurable fields
				readIos, err := strconv.ParseUint(parts[1], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse read_ios: %w", err)
				}
				readPercent, err := strconv.ParseUint(parts[2], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse read_percent: %w", err)
				}
				writeIos, err := strconv.ParseUint(parts[5], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse write_ios: %w", err)
				}
				writePercent, err := strconv.ParseUint(parts[6], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse write_percent: %w", err)
				}
				reportName := headerName
				if wanted.reportAs != "" {
					reportName = wanted.reportAs
				}

				tag := tags{name, reportName, bucket, "", ""}
				fields, ok := l.allFields[tag]
				if !ok {
					fields = make(map[string]interface{})
					l.allFields[tag] = fields
				}

				fields["read_ios"] = readIos
				fields["read_percent"] = readPercent
				fields["write_ios"] = writeIos
				fields["write_percent"] = writePercent
			}
		}
	}
	return nil
}

func (l *Lustre2) getLustreEvictionCount(fileglob string) error {
	files, err := filepath.Glob(filepath.Join(l.rootdir, fileglob))
	if err != nil {
		return fmt.Errorf("failed to find files matching glob %s: %w", fileglob, err)
	}

	for _, file := range files {
		// Turn /sys/fs/lustre/*/<mgt/mdt/ost_name>/eviction_count into just the object store target name
		// This assumes that the target name is always second to last, which is true in Lustre 2.1->2.12
		path := strings.Split(file, "/")
		if len(path) < 2 {
			continue
		}
		name := path[len(path)-2]

		contents, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", file, err)
		}

		value, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse file %s: %w", file, err)
		}

		tag := tags{name, "", "", "", ""}
		fields, ok := l.allFields[tag]
		if !ok {
			fields = make(map[string]interface{})
			l.allFields[tag] = fields
		}

		fields["evictions"] = value
	}
	return nil
}

// The wanted fields would be a []string, if not for the lines that start with read_bytes/write_bytes
// and contain both the byte count and the function call count
type mapping struct {
	inProc   string // What to look for at the start of a line in /proc/fs/lustre/*
	field    uint32 // which field to extract from that line
	reportAs string // What measurement name to use
}

var wantedBrwstatsFields = []*mapping{
	{
		inProc:   "pages per bulk r/w",
		reportAs: "pages_per_bulk_rw",
	},
	{
		inProc:   "discontiguous pages",
		reportAs: "discontiguous_pages",
	},
	{
		inProc:   "disk I/Os in flight",
		reportAs: "disk_ios_in_flight",
	},
	{
		inProc:   "I/O time (1/1000s)",
		reportAs: "io_time",
	},
	{
		inProc:   "disk I/O size",
		reportAs: "disk_io_size",
	},
}

var wantedOstFields = []*mapping{
	{
		inProc:   "write_bytes",
		field:    6,
		reportAs: "write_bytes",
	},
	{ // line starts with 'write_bytes', but value write_calls is in second column
		inProc:   "write_bytes",
		field:    1,
		reportAs: "write_calls",
	},
	{
		inProc:   "read_bytes",
		field:    6,
		reportAs: "read_bytes",
	},
	{ // line starts with 'read_bytes', but value read_calls is in second column
		inProc:   "read_bytes",
		field:    1,
		reportAs: "read_calls",
	},
	{
		inProc: "cache_hit",
	},
	{
		inProc: "cache_miss",
	},
	{
		inProc: "cache_access",
	},
}

var wantedOstJobstatsFields = []*mapping{
	{ // The read line has several fields, so we need to differentiate what they are
		inProc:   "read",
		field:    3,
		reportAs: "jobstats_read_calls",
	},
	{
		inProc:   "read",
		field:    7,
		reportAs: "jobstats_read_min_size",
	},
	{
		inProc:   "read",
		field:    9,
		reportAs: "jobstats_read_max_size",
	},
	{
		inProc:   "read",
		field:    11,
		reportAs: "jobstats_read_bytes",
	},
	{ // Different inProc for newer versions
		inProc:   "read_bytes",
		field:    3,
		reportAs: "jobstats_read_calls",
	},
	{
		inProc:   "read_bytes",
		field:    7,
		reportAs: "jobstats_read_min_size",
	},
	{
		inProc:   "read_bytes",
		field:    9,
		reportAs: "jobstats_read_max_size",
	},
	{
		inProc:   "read_bytes",
		field:    11,
		reportAs: "jobstats_read_bytes",
	},
	{ // We need to do the same for the write fields
		inProc:   "write",
		field:    3,
		reportAs: "jobstats_write_calls",
	},
	{
		inProc:   "write",
		field:    7,
		reportAs: "jobstats_write_min_size",
	},
	{
		inProc:   "write",
		field:    9,
		reportAs: "jobstats_write_max_size",
	},
	{
		inProc:   "write",
		field:    11,
		reportAs: "jobstats_write_bytes",
	},
	{ // Different inProc for newer versions
		inProc:   "write_bytes",
		field:    3,
		reportAs: "jobstats_write_calls",
	},
	{
		inProc:   "write_bytes",
		field:    7,
		reportAs: "jobstats_write_min_size",
	},
	{
		inProc:   "write_bytes",
		field:    9,
		reportAs: "jobstats_write_max_size",
	},
	{
		inProc:   "write_bytes",
		field:    11,
		reportAs: "jobstats_write_bytes",
	},
	{
		inProc:   "getattr",
		field:    3,
		reportAs: "jobstats_ost_getattr",
	},
	{
		inProc:   "setattr",
		field:    3,
		reportAs: "jobstats_ost_setattr",
	},
	{
		inProc:   "punch",
		field:    3,
		reportAs: "jobstats_punch",
	},
	{
		inProc:   "sync",
		field:    3,
		reportAs: "jobstats_ost_sync",
	},
	{
		inProc:   "destroy",
		field:    3,
		reportAs: "jobstats_destroy",
	},
	{
		inProc:   "create",
		field:    3,
		reportAs: "jobstats_create",
	},
	{
		inProc:   "statfs",
		field:    3,
		reportAs: "jobstats_ost_statfs",
	},
	{
		inProc:   "get_info",
		field:    3,
		reportAs: "jobstats_get_info",
	},
	{
		inProc:   "set_info",
		field:    3,
		reportAs: "jobstats_set_info",
	},
	{
		inProc:   "quotactl",
		field:    3,
		reportAs: "jobstats_quotactl",
	},
}

var wantedMdsFields = []*mapping{
	{
		inProc: "open",
	},
	{
		inProc: "close",
	},
	{
		inProc: "mknod",
	},
	{
		inProc: "link",
	},
	{
		inProc: "unlink",
	},
	{
		inProc: "mkdir",
	},
	{
		inProc: "rmdir",
	},
	{
		inProc: "rename",
	},
	{
		inProc: "getattr",
	},
	{
		inProc: "setattr",
	},
	{
		inProc: "getxattr",
	},
	{
		inProc: "setxattr",
	},
	{
		inProc: "statfs",
	},
	{
		inProc: "sync",
	},
	{
		inProc: "samedir_rename",
	},
	{
		inProc: "crossdir_rename",
	},
}

var wantedMdtJobstatsFields = []*mapping{
	{
		inProc:   "open",
		field:    3,
		reportAs: "jobstats_open",
	},
	{
		inProc:   "close",
		field:    3,
		reportAs: "jobstats_close",
	},
	{
		inProc:   "mknod",
		field:    3,
		reportAs: "jobstats_mknod",
	},
	{
		inProc:   "link",
		field:    3,
		reportAs: "jobstats_link",
	},
	{
		inProc:   "unlink",
		field:    3,
		reportAs: "jobstats_unlink",
	},
	{
		inProc:   "mkdir",
		field:    3,
		reportAs: "jobstats_mkdir",
	},
	{
		inProc:   "rmdir",
		field:    3,
		reportAs: "jobstats_rmdir",
	},
	{
		inProc:   "rename",
		field:    3,
		reportAs: "jobstats_rename",
	},
	{
		inProc:   "getattr",
		field:    3,
		reportAs: "jobstats_getattr",
	},
	{
		inProc:   "setattr",
		field:    3,
		reportAs: "jobstats_setattr",
	},
	{
		inProc:   "getxattr",
		field:    3,
		reportAs: "jobstats_getxattr",
	},
	{
		inProc:   "setxattr",
		field:    3,
		reportAs: "jobstats_setxattr",
	},
	{
		inProc:   "statfs",
		field:    3,
		reportAs: "jobstats_statfs",
	},
	{
		inProc:   "sync",
		field:    3,
		reportAs: "jobstats_sync",
	},
	{
		inProc:   "samedir_rename",
		field:    3,
		reportAs: "jobstats_samedir_rename",
	},
	{
		inProc:   "crossdir_rename",
		field:    3,
		reportAs: "jobstats_crossdir_rename",
	},
}

func init() {
	inputs.Add("lustre2", func() telegraf.Input {
		return &Lustre2{}
	})
}
