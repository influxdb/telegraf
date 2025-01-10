//go:generate ../../../tools/readme_config_includer/generator
package pf

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	errParseHeader     = fmt.Errorf("cannot find header in %s output", pfctlCommand)
	anyTableHeaderRE   = regexp.MustCompile("^[A-Z]")
	stateTableRE       = regexp.MustCompile(`^  (.*?)\s+(\d+)`)
	counterTableRE     = regexp.MustCompile(`^  (.*?)\s+(\d+)`)
	execLookPath       = exec.LookPath
	execCommand        = exec.Command
	pfctlOutputStanzas = []*pfctlOutputStanza{
		{
			headerRE:  regexp.MustCompile("^State Table"),
			parseFunc: parseStateTable,
		},
		{
			headerRE:  regexp.MustCompile("^Counters"),
			parseFunc: parseCounterTable,
		},
	}
	stateTable = []*entry{
		{"entries", "current entries", -1},
		{"searches", "searches", -1},
		{"inserts", "inserts", -1},
		{"removals", "removals", -1},
	}
	counterTable = []*entry{
		{"match", "match", -1},
		{"bad-offset", "bad-offset", -1},
		{"fragment", "fragment", -1},
		{"short", "short", -1},
		{"normalize", "normalize", -1},
		{"memory", "memory", -1},
		{"bad-timestamp", "bad-timestamp", -1},
		{"congestion", "congestion", -1},
		{"ip-option", "ip-option", -1},
		{"proto-cksum", "proto-cksum", -1},
		{"state-mismatch", "state-mismatch", -1},
		{"state-insert", "state-insert", -1},
		{"state-limit", "state-limit", -1},
		{"src-limit", "src-limit", -1},
		{"synproxy", "synproxy", -1},
	}
)

const (
	measurement  = "pf"
	pfctlCommand = "pfctl"
)

type PF struct {
	UseSudo bool `toml:"use_sudo"`

	pfctlCommand string
	pfctlArgs    []string
	infoFunc     func() (string, error)
}

type pfctlOutputStanza struct {
	headerRE  *regexp.Regexp
	parseFunc func([]string, map[string]interface{}) error
	found     bool
}

type entry struct {
	field      string
	pfctlTitle string
	value      int64
}

func (*PF) SampleConfig() string {
	return sampleConfig
}

func (pf *PF) Gather(acc telegraf.Accumulator) error {
	if pf.pfctlCommand == "" {
		var err error
		if pf.pfctlCommand, pf.pfctlArgs, err = pf.buildPfctlCmd(); err != nil {
			acc.AddError(fmt.Errorf("can't construct pfctl commandline: %w", err))
			return nil
		}
	}

	o, err := pf.infoFunc()
	if err != nil {
		acc.AddError(err)
		return nil
	}

	if perr := parsePfctlOutput(o, acc); perr != nil {
		acc.AddError(perr)
	}
	return nil
}

func errMissingData(tag string) error {
	return fmt.Errorf("struct data for tag %q not found in %s output", tag, pfctlCommand)
}

func parsePfctlOutput(pfoutput string, acc telegraf.Accumulator) error {
	fields := make(map[string]interface{})
	scanner := bufio.NewScanner(strings.NewReader(pfoutput))
	for scanner.Scan() {
		line := scanner.Text()
		for _, s := range pfctlOutputStanzas {
			if s.headerRE.MatchString(line) {
				var stanzaLines []string
				scanner.Scan()
				line = scanner.Text()
				for !anyTableHeaderRE.MatchString(line) {
					stanzaLines = append(stanzaLines, line)
					more := scanner.Scan()
					if !more {
						break
					}
					line = scanner.Text()
				}
				if perr := s.parseFunc(stanzaLines, fields); perr != nil {
					return perr
				}
				s.found = true
			}
		}
	}
	for _, s := range pfctlOutputStanzas {
		if !s.found {
			return errParseHeader
		}
	}

	acc.AddFields(measurement, fields, make(map[string]string))
	return nil
}

func parseStateTable(lines []string, fields map[string]interface{}) error {
	return storeFieldValues(lines, stateTableRE, fields, stateTable)
}

func parseCounterTable(lines []string, fields map[string]interface{}) error {
	return storeFieldValues(lines, counterTableRE, fields, counterTable)
}

func storeFieldValues(lines []string, regex *regexp.Regexp, fields map[string]interface{}, entryTable []*entry) error {
	for _, v := range lines {
		entries := regex.FindStringSubmatch(v)
		if entries != nil {
			for _, f := range entryTable {
				if f.pfctlTitle == entries[1] {
					var err error
					if f.value, err = strconv.ParseInt(entries[2], 10, 64); err != nil {
						return err
					}
				}
			}
		}
	}

	for _, v := range entryTable {
		if v.value == -1 {
			return errMissingData(v.pfctlTitle)
		}
		fields[v.field] = v.value
	}

	return nil
}

func (pf *PF) callPfctl() (string, error) {
	cmd := execCommand(pf.pfctlCommand, pf.pfctlArgs...)
	out, oerr := cmd.Output()
	if oerr != nil {
		var ee *exec.ExitError
		if !errors.As(oerr, &ee) {
			return string(out), fmt.Errorf("error running %q: %w: (unable to get stderr)", pfctlCommand, oerr)
		}
		return string(out), fmt.Errorf("error running %q: %w - %s", pfctlCommand, oerr, ee.Stderr)
	}
	return string(out), oerr
}

func (pf *PF) buildPfctlCmd() (string, []string, error) {
	cmd, err := execLookPath(pfctlCommand)
	if err != nil {
		return "", nil, fmt.Errorf("can't locate %q: %w", pfctlCommand, err)
	}
	args := []string{"-s", "info"}
	if pf.UseSudo {
		args = append([]string{cmd}, args...)
		cmd, err = execLookPath("sudo")
		if err != nil {
			return "", nil, fmt.Errorf("can't locate sudo: %w", err)
		}
	}
	return cmd, args, nil
}

func init() {
	inputs.Add("pf", func() telegraf.Input {
		pf := &PF{}
		pf.infoFunc = pf.callPfctl
		return pf
	})
}
