//go:generate ../../../tools/readme_config_includer/generator
package linux_sysctl_fs

import (
	"bytes"
	_ "embed"
	"errors"
	"os"
	"path"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// https://www.kernel.org/doc/Documentation/sysctl/fs.txt
type SysctlFS struct {
	path string
}

func (*SysctlFS) SampleConfig() string {
	return sampleConfig
}

func (sfs *SysctlFS) Gather(acc telegraf.Accumulator) error {
	fields := make(map[string]interface{})

	for _, n := range []string{"aio-nr", "aio-max-nr", "dquot-nr", "dquot-max", "super-nr", "super-max"} {
		if err := sfs.gatherOne(n, fields); err != nil {
			return err
		}
	}

	err := sfs.gatherList("inode-state", fields, "inode-nr", "inode-free-nr", "inode-preshrink-nr")
	if err != nil {
		return err
	}
	err = sfs.gatherList("dentry-state", fields, "dentry-nr", "dentry-unused-nr", "dentry-age-limit", "dentry-want-pages")
	if err != nil {
		return err
	}
	err = sfs.gatherList("file-nr", fields, "file-nr", "", "file-max")
	if err != nil {
		return err
	}

	acc.AddFields("linux_sysctl_fs", fields, nil)
	return nil
}

func (sfs *SysctlFS) gatherList(file string, fields map[string]interface{}, fieldNames ...string) error {
	bs, err := os.ReadFile(sfs.path + "/" + file)
	if err != nil {
		// Ignore non-existing entries
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	bsplit := bytes.Split(bytes.TrimRight(bs, "\n"), []byte{'\t'})
	for i, name := range fieldNames {
		if i >= len(bsplit) {
			break
		}
		if name == "" {
			continue
		}

		v, err := strconv.ParseUint(string(bsplit[i]), 10, 64)
		if err != nil {
			return err
		}
		fields[name] = v
	}

	return nil
}

func (sfs *SysctlFS) gatherOne(name string, fields map[string]interface{}) error {
	bs, err := os.ReadFile(sfs.path + "/" + name)
	if err != nil {
		// Ignore non-existing entries
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	v, err := strconv.ParseUint(string(bytes.TrimRight(bs, "\n")), 10, 64)
	if err != nil {
		return err
	}

	fields[name] = v
	return nil
}

func init() {
	inputs.Add("linux_sysctl_fs", func() telegraf.Input {
		return &SysctlFS{
			path: path.Join(internal.GetProcPath(), "/sys/fs"),
		}
	})
}
