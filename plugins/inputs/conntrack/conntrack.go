//go:generate go run ../../../tools/generate_plugindata/main.go
//go:generate go run ../../../tools/generate_plugindata/main.go --clean
//go:build linux
// +build linux

package conntrack

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"path/filepath"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

type Conntrack struct {
	Path  string
	Dirs  []string
	Files []string
}

const (
	inputName = "conntrack"
)

var dfltDirs = []string{
	"/proc/sys/net/ipv4/netfilter",
	"/proc/sys/net/netfilter",
}

var dfltFiles = []string{
	"ip_conntrack_count",
	"ip_conntrack_max",
	"nf_conntrack_count",
	"nf_conntrack_max",
}

func (c *Conntrack) setDefaults() {
	if len(c.Dirs) == 0 {
		c.Dirs = dfltDirs
	}

	if len(c.Files) == 0 {
		c.Files = dfltFiles
	}
}

func (c *Conntrack) Gather(acc telegraf.Accumulator) error {
	c.setDefaults()

	var metricKey string
	fields := make(map[string]interface{})

	for _, dir := range c.Dirs {
		for _, file := range c.Files {
			// NOTE: no system will have both nf_ and ip_ prefixes,
			// so we're safe to branch on suffix only.
			parts := strings.SplitN(file, "_", 2)
			if len(parts) < 2 {
				continue
			}
			metricKey = "ip_" + parts[1]

			fName := filepath.Join(dir, file)
			if _, err := os.Stat(fName); err != nil {
				continue
			}

			contents, err := os.ReadFile(fName)
			if err != nil {
				acc.AddError(fmt.Errorf("E! failed to read file '%s': %v", fName, err))
				continue
			}

			v := strings.TrimSpace(string(contents))
			fields[metricKey], err = strconv.ParseFloat(v, 64)
			if err != nil {
				acc.AddError(fmt.Errorf("E! failed to parse metric, expected number but "+
					" found '%s': %v", v, err))
			}
		}
	}

	if len(fields) == 0 {
		return fmt.Errorf("Conntrack input failed to collect metrics. " +
			"Is the conntrack kernel module loaded?")
	}

	acc.AddFields(inputName, fields, nil)
	return nil
}

func init() {
	inputs.Add(inputName, func() telegraf.Input { return &Conntrack{} })
}
