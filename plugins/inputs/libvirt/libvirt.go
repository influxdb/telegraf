//go:generate ../../../tools/readme_config_includer/generator
package libvirt

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"

	golibvirt "github.com/digitalocean/go-libvirt"
	libvirtutils "github.com/thomasklein94/packer-plugin-libvirt/libvirt-utils"
	"golang.org/x/sync/errgroup"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	domainStatsState     uint32 = 1
	domainStatsCPUTotal  uint32 = 2
	domainStatsBalloon   uint32 = 4
	domainStatsVCPU      uint32 = 8
	domainStatsInterface uint32 = 16
	domainStatsBlock     uint32 = 32
	domainStatsPerf      uint32 = 64
	domainStatsIothread  uint32 = 128
	domainStatsMemory    uint32 = 256
	domainStatsDirtyrate uint32 = 512
	domainStatsAll       uint32 = 1023
	defaultLibvirtURI           = "qemu:///system"
	pluginName                  = "libvirt"
)

type Libvirt struct {
	LibvirtURI           string          `toml:"libvirt_uri"`
	Domains              []string        `toml:"domains"`
	StatisticsGroups     []string        `toml:"statistics_groups"`
	AdditionalStatistics []string        `toml:"additional_statistics"`
	Log                  telegraf.Logger `toml:"-"`

	utils              utils
	metricNumber       uint32
	vcpuMappingEnabled bool
	domainsMap         map[string]struct{}
}

func (*Libvirt) SampleConfig() string {
	return sampleConfig
}

func (l *Libvirt) Init() error {
	if len(l.Domains) == 0 {
		l.Log.Debugf("No domains given. Collecting metrics from all available domains.")
	}
	l.domainsMap = make(map[string]struct{}, len(l.Domains))
	for _, domain := range l.Domains {
		l.domainsMap[domain] = struct{}{}
	}

	if l.LibvirtURI == "" {
		l.Log.Debugf("Using default libvirt url - %q", defaultLibvirtURI)
		l.LibvirtURI = defaultLibvirtURI
	}

	if err := l.validateLibvirtURI(); err != nil {
		return err
	}

	// setting to defaults only when statistics_groups is missing in config
	if l.StatisticsGroups == nil {
		l.Log.Debugf("Setting libvirt to gather all metrics.")
		l.metricNumber = domainStatsAll
	} else {
		if err := l.calculateMetricNumber(); err != nil {
			return err
		}
	}

	if err := l.validateAdditionalStatistics(); err != nil {
		return err
	}

	if !l.isThereAnythingToGather() {
		return errors.New("all configuration options are empty or invalid. Did not find anything to gather")
	}

	return nil
}

func (l *Libvirt) Gather(acc telegraf.Accumulator) error {
	var err error
	if err := l.utils.ensureConnected(l.LibvirtURI); err != nil {
		return err
	}

	// Get all available domains
	gatheredDomains, err := l.utils.gatherAllDomains()
	if handledErr := handleError(err, "error occurred while gathering all domains", l.utils); handledErr != nil {
		return handledErr
	} else if len(gatheredDomains) == 0 {
		l.Log.Debug("Couldn't find any domains on system")
		return nil
	}

	// Exclude domain.
	domains := l.filterDomains(gatheredDomains)
	if len(domains) == 0 {
		l.Log.Debug("Configured domains are not available on system")
		return nil
	}

	var vcpuInfos map[string][]vcpuAffinity
	if l.vcpuMappingEnabled {
		vcpuInfos, err = l.getVcpuMapping(domains)
		if handledErr := handleError(err, "error occurred while gathering vcpu mapping", l.utils); handledErr != nil {
			return handledErr
		}
	}

	err = l.gatherMetrics(domains, vcpuInfos, acc)
	return handleError(err, "error occurred while gathering metrics", l.utils)
}

func (l *Libvirt) validateLibvirtURI() error {
	uri := libvirtutils.LibvirtUri{}
	err := uri.Unmarshal(l.LibvirtURI)
	if err != nil {
		return err
	}

	// dialer not needed, calling this just for validating libvirt URI as soon as possible:
	_, err = libvirtutils.NewDialerFromLibvirtUri(uri)
	return err
}

func (l *Libvirt) calculateMetricNumber() error {
	var libvirtMetricNumber = map[string]uint32{
		"state":     domainStatsState,
		"cpu_total": domainStatsCPUTotal,
		"balloon":   domainStatsBalloon,
		"vcpu":      domainStatsVCPU,
		"interface": domainStatsInterface,
		"block":     domainStatsBlock,
		"perf":      domainStatsPerf,
		"iothread":  domainStatsIothread,
		"memory":    domainStatsMemory,
		"dirtyrate": domainStatsDirtyrate}

	metricIsSet := make(map[string]bool)
	for _, metricName := range l.StatisticsGroups {
		metricNumber, exists := libvirtMetricNumber[metricName]
		if !exists {
			return fmt.Errorf("unrecognized metrics name %q", metricName)
		}
		if _, ok := metricIsSet[metricName]; ok {
			return fmt.Errorf("duplicated statistics group in config: %q", metricName)
		}
		l.metricNumber += metricNumber
		metricIsSet[metricName] = true
	}

	return nil
}

func (l *Libvirt) validateAdditionalStatistics() error {
	for _, stat := range l.AdditionalStatistics {
		switch stat {
		case "vcpu_mapping":
			if l.vcpuMappingEnabled {
				return fmt.Errorf("duplicated additional statistic in config: %q", stat)
			}
			l.vcpuMappingEnabled = true
		default:
			return fmt.Errorf("additional statistics: %v is not supported by this plugin", stat)
		}
	}
	return nil
}

func (l *Libvirt) isThereAnythingToGather() bool {
	return l.metricNumber > 0 || len(l.AdditionalStatistics) > 0
}

func handleError(err error, errMessage string, utils utils) error {
	if err != nil {
		if chanErr := utils.disconnect(); chanErr != nil {
			return fmt.Errorf("%s: %w; error occurred when disconnecting: %w", errMessage, err, chanErr)
		}
		return fmt.Errorf("%s: %w", errMessage, err)
	}
	return nil
}

func (l *Libvirt) filterDomains(availableDomains []golibvirt.Domain) []golibvirt.Domain {
	if len(l.domainsMap) == 0 {
		return availableDomains
	}

	var filteredDomains []golibvirt.Domain
	for _, domain := range availableDomains {
		if _, ok := l.domainsMap[domain.Name]; ok {
			filteredDomains = append(filteredDomains, domain)
		}
	}

	return filteredDomains
}

func (l *Libvirt) gatherMetrics(domains []golibvirt.Domain, vcpuInfos map[string][]vcpuAffinity, acc telegraf.Accumulator) error {
	stats, err := l.utils.gatherStatsForDomains(domains, l.metricNumber)
	if err != nil {
		return err
	}

	l.addMetrics(stats, vcpuInfos, acc)
	return nil
}

func (l *Libvirt) getVcpuMapping(domains []golibvirt.Domain) (map[string][]vcpuAffinity, error) {
	pCPUs, err := l.utils.gatherNumberOfPCPUs()
	if err != nil {
		return nil, err
	}

	var vcpuInfos = make(map[string][]vcpuAffinity)
	group := errgroup.Group{}
	mutex := &sync.RWMutex{}
	for i := range domains {
		domain := domains[i]

		// Executing gatherVcpuMapping can take some time, it is worth to call it in parallel
		group.Go(func() error {
			vcpuInfo, err := l.utils.gatherVcpuMapping(domain, pCPUs, l.shouldGetCurrentPCPU())
			if err != nil {
				return err
			}

			mutex.Lock()
			vcpuInfos[domain.Name] = vcpuInfo
			mutex.Unlock()
			return nil
		})
	}

	err = group.Wait()
	if err != nil {
		return nil, err
	}

	return vcpuInfos, nil
}

func (l *Libvirt) shouldGetCurrentPCPU() bool {
	return l.vcpuMappingEnabled && (l.metricNumber&domainStatsVCPU) != 0
}

func init() {
	inputs.Add(pluginName, func() telegraf.Input {
		return &Libvirt{
			utils: &utilsImpl{},
		}
	})
}
