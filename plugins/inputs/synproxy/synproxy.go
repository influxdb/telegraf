//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

package synproxy

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Synproxy struct {
	Log telegraf.Logger `toml:"-"`

	// Synproxy stats filename (proc filesystem)
	statFile string
}

func (*Synproxy) SampleConfig() string {
	return sampleConfig
}

func (s *Synproxy) Gather(acc telegraf.Accumulator) error {
	data, err := s.getSynproxyStat()
	if err != nil {
		return err
	}

	acc.AddCounter("synproxy", data, make(map[string]string))
	return nil
}

func (s *Synproxy) getSynproxyStat() (map[string]interface{}, error) {
	var hname []string
	counters := []string{"entries", "syn_received", "cookie_invalid", "cookie_valid", "cookie_retrans", "conn_reopened"}
	fields := make(map[string]interface{})

	// Open synproxy file in proc filesystem
	file, err := os.Open(s.statFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Initialise expected fields
	for _, val := range counters {
		fields[val] = uint32(0)
	}

	scanner := bufio.NewScanner(file)
	// Read header row
	if scanner.Scan() {
		line := scanner.Text()
		// Parse fields separated by whitespace
		dataFields := strings.Fields(line)
		for _, val := range dataFields {
			if !inSlice(counters, val) {
				val = ""
			}
			hname = append(hname, val)
		}
	}
	if len(hname) == 0 {
		return nil, errors.New("invalid data")
	}
	// Read data rows
	for scanner.Scan() {
		line := scanner.Text()
		// Parse fields separated by whitespace
		dataFields := strings.Fields(line)
		// If number of data fields do not match number of header fields
		if len(dataFields) != len(hname) {
			return nil, fmt.Errorf("invalid number of columns in data, expected %d found %d", len(hname),
				len(dataFields))
		}
		for i, val := range dataFields {
			// Convert from hexstring to int32
			x, err := strconv.ParseUint(val, 16, 32)
			// If field is not a valid hexstring
			if err != nil {
				return nil, fmt.Errorf("invalid value %q found", val)
			}
			if hname[i] != "" {
				fields[hname[i]] = fields[hname[i]].(uint32) + uint32(x)
			}
		}
	}
	return fields, nil
}

func inSlice(haystack []string, needle string) bool {
	for _, val := range haystack {
		if needle == val {
			return true
		}
	}
	return false
}

func init() {
	inputs.Add("synproxy", func() telegraf.Input {
		return &Synproxy{
			statFile: path.Join(internal.GetProcPath(), "/net/stat/synproxy"),
		}
	})
}
