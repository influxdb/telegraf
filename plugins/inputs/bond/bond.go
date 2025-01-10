//go:generate ../../../tools/readme_config_includer/generator
package bond

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Bond struct {
	HostProc       string   `toml:"host_proc"`
	HostSys        string   `toml:"host_sys"`
	SysDetails     bool     `toml:"collect_sys_details"`
	BondInterfaces []string `toml:"bond_interfaces"`
	BondType       string
}

type sysFiles struct {
	ModeFile    string
	SlaveFile   string
	ADPortsFile string
}

func (*Bond) SampleConfig() string {
	return sampleConfig
}

func (bond *Bond) Gather(acc telegraf.Accumulator) error {
	// load proc path, get default value if config value and env variable are empty
	bond.loadPaths()
	// list bond interfaces from bonding directory or gather all interfaces.
	bondNames, err := bond.listInterfaces()
	if err != nil {
		return err
	}
	for _, bondName := range bondNames {
		bondAbsPath := bond.HostProc + "/net/bonding/" + bondName
		file, err := os.ReadFile(bondAbsPath)
		if err != nil {
			acc.AddError(fmt.Errorf("error inspecting %q interface: %w", bondAbsPath, err))
			continue
		}
		rawProcFile := strings.TrimSpace(string(file))
		err = bond.gatherBondInterface(bondName, rawProcFile, acc)
		if err != nil {
			acc.AddError(fmt.Errorf("error inspecting %q interface: %w", bondName, err))
		}

		/*
			Some details about bonds only exist in /sys/class/net/
			In particular, LACP bonds track upstream port state here
		*/
		if bond.SysDetails {
			files, err := bond.readSysFiles(bond.HostSys + "/class/net/" + bondName)
			if err != nil {
				acc.AddError(err)
			}
			gatherSysDetails(bondName, files, acc)
		}
	}
	return nil
}

func (bond *Bond) gatherBondInterface(bondName, rawFile string, acc telegraf.Accumulator) error {
	splitIndex := strings.Index(rawFile, "Slave Interface:")
	if splitIndex == -1 {
		splitIndex = len(rawFile)
	}
	bondPart := rawFile[:splitIndex]
	slavePart := rawFile[splitIndex:]

	err := bond.gatherBondPart(bondName, bondPart, acc)
	if err != nil {
		return err
	}
	err = bond.gatherSlavePart(bondName, slavePart, acc)
	if err != nil {
		return err
	}
	return nil
}

func (bond *Bond) gatherBondPart(bondName, rawFile string, acc telegraf.Accumulator) error {
	fields := make(map[string]interface{})
	tags := map[string]string{
		"bond": bondName,
	}
	scanner := bufio.NewScanner(strings.NewReader(rawFile))
	/*
		/proc/bond/... files are formatted in a way that is difficult
		to use regexes to parse. Because of that, we scan through
		the file one line at a time and rely on specific lines to
		mark "ends" of blocks. It's a hack that should be resolved,
		but for now, it works.
	*/
	for scanner.Scan() {
		line := scanner.Text()
		stats := strings.Split(line, ":")
		if len(stats) < 2 {
			continue
		}
		name := strings.TrimSpace(stats[0])
		value := strings.TrimSpace(stats[1])
		if name == "Bonding Mode" {
			bond.BondType = value
		}
		if strings.Contains(name, "Currently Active Slave") {
			fields["active_slave"] = value
		}
		if strings.Contains(name, "MII Status") {
			fields["status"] = 0
			if value == "up" {
				fields["status"] = 1
			}
			acc.AddFields("bond", fields, tags)
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("couldn't find status info for %q", bondName)
}

func (bond *Bond) readSysFiles(bondDir string) (sysFiles, error) {
	/*
		Files we may need
		bonding/mode
		bonding/slaves
		bonding/ad_num_ports

		We load files here first to allow for easier testing
	*/
	var output sysFiles

	file, err := os.ReadFile(bondDir + "/bonding/mode")
	if err != nil {
		return sysFiles{}, fmt.Errorf("error inspecting %q interface: %w", bondDir+"/bonding/mode", err)
	}
	output.ModeFile = strings.TrimSpace(string(file))
	file, err = os.ReadFile(bondDir + "/bonding/slaves")
	if err != nil {
		return sysFiles{}, fmt.Errorf("error inspecting %q interface: %w", bondDir+"/bonding/slaves", err)
	}
	output.SlaveFile = strings.TrimSpace(string(file))
	if bond.BondType == "IEEE 802.3ad Dynamic link aggregation" {
		file, err = os.ReadFile(bondDir + "/bonding/ad_num_ports")
		if err != nil {
			return sysFiles{}, fmt.Errorf("error inspecting %q interface: %w", bondDir+"/bonding/ad_num_ports", err)
		}
		output.ADPortsFile = strings.TrimSpace(string(file))
	}
	return output, nil
}

func gatherSysDetails(bondName string, files sysFiles, acc telegraf.Accumulator) {
	var slaves []string
	var adPortCount int

	// To start with, we get the bond operating mode
	mode := strings.TrimSpace(strings.Split(files.ModeFile, " ")[0])

	tags := map[string]string{
		"bond": bondName,
		"mode": mode,
	}

	// Next we collect the number of bond slaves the system expects
	slavesTmp := strings.Split(files.SlaveFile, " ")
	for _, slave := range slavesTmp {
		if slave != "" {
			slaves = append(slaves, slave)
		}
	}
	if mode == "802.3ad" {
		/*
			If we're in LACP mode, we should check on how the bond ports are
			interacting with the upstream switch ports
			a failed conversion can be treated as 0 ports
		*/
		if pc, err := strconv.Atoi(strings.TrimSpace(files.ADPortsFile)); err == nil {
			adPortCount = pc
		}
	} else {
		adPortCount = len(slaves)
	}

	fields := map[string]interface{}{
		"slave_count":   len(slaves),
		"ad_port_count": adPortCount,
	}
	acc.AddFields("bond_sys", fields, tags)
}

func (bond *Bond) gatherSlavePart(bondName, rawFile string, acc telegraf.Accumulator) error {
	var slaveCount int
	tags := map[string]string{
		"bond": bondName,
	}
	fields := map[string]interface{}{
		"status": 0,
	}
	var scanPast bool
	if bond.BondType == "IEEE 802.3ad Dynamic link aggregation" {
		scanPast = true
	}

	scanner := bufio.NewScanner(strings.NewReader(rawFile))
	for scanner.Scan() {
		line := scanner.Text()
		stats := strings.Split(line, ":")
		if len(stats) < 2 {
			continue
		}
		name := strings.TrimSpace(stats[0])
		value := strings.TrimSpace(stats[1])
		if strings.Contains(name, "Slave Interface") {
			tags["interface"] = value
			slaveCount++
		}
		if strings.Contains(name, "MII Status") && value == "up" {
			fields["status"] = 1
		}
		if strings.Contains(name, "Link Failure Count") {
			count, err := strconv.Atoi(value)
			if err != nil {
				return err
			}
			fields["failures"] = count
			if !scanPast {
				acc.AddFields("bond_slave", fields, tags)
				fields = map[string]interface{}{
					"status": 0,
				}
			}
		}
		if strings.Contains(name, "Actor Churned Count") {
			count, err := strconv.Atoi(value)
			if err != nil {
				return err
			}
			fields["actor_churned"] = count
		}
		if strings.Contains(name, "Partner Churned Count") {
			count, err := strconv.Atoi(value)
			if err != nil {
				return err
			}
			fields["partner_churned"] = count
			fields["total_churned"] = fields["actor_churned"].(int) + fields["partner_churned"].(int)
			acc.AddFields("bond_slave", fields, tags)
			fields = map[string]interface{}{
				"status": 0,
			}
		}
	}
	tags = map[string]string{
		"bond": bondName,
	}
	fields = map[string]interface{}{
		"count": slaveCount,
	}
	acc.AddFields("bond_slave", fields, tags)

	return scanner.Err()
}

// loadPaths can be used to read path firstly from config
// if it is empty then try read from env variable
func (bond *Bond) loadPaths() {
	if bond.HostProc == "" {
		bond.HostProc = internal.GetProcPath()
	}
	if bond.HostSys == "" {
		bond.HostSys = internal.GetSysPath()
	}
}

func (bond *Bond) listInterfaces() ([]string, error) {
	var interfaces []string
	if len(bond.BondInterfaces) > 0 {
		interfaces = bond.BondInterfaces
	} else {
		paths, err := filepath.Glob(bond.HostProc + "/net/bonding/*")
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			interfaces = append(interfaces, filepath.Base(p))
		}
	}
	return interfaces, nil
}

func init() {
	inputs.Add("bond", func() telegraf.Input {
		return &Bond{}
	})
}
