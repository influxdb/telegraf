package libvirt

import (
	"strconv"
	"strings"

	golibvirt "github.com/digitalocean/go-libvirt"
	libvirtutils "github.com/thomasklein94/packer-plugin-libvirt/libvirt-utils"
)

type utils interface {
	gatherAllDomains() (domains []golibvirt.Domain, err error)
	gatherStatsForDomains(domains []golibvirt.Domain, metricNumber uint32) ([]golibvirt.DomainStatsRecord, error)
	gatherNumberOfPCPUs() (int, error)
	gatherVcpuMapping(domain golibvirt.Domain, pCPUs int, shouldGetCurrentPCPU bool) ([]vcpuAffinity, error)
	ensureConnected(libvirtURI string) error
	disconnect() error
}

type utilsImpl struct {
	libvirt *golibvirt.Libvirt
}

type vcpuAffinity struct {
	vcpuID        string
	coresAffinity string
	currentPCPUID int32
}

// gatherAllDomains gathers all domains on system
func (l *utilsImpl) gatherAllDomains() (domains []golibvirt.Domain, err error) {
	allDomainStatesFlag := golibvirt.ConnectListDomainsRunning + golibvirt.ConnectListDomainsPaused +
		golibvirt.ConnectListDomainsShutoff + golibvirt.ConnectListDomainsOther

	domains, _, err = l.libvirt.ConnectListAllDomains(1, allDomainStatesFlag)
	return domains, err
}

// gatherStatsForDomains gathers stats for given domains based on number that was previously calculated
func (l *utilsImpl) gatherStatsForDomains(domains []golibvirt.Domain, metricNumber uint32) ([]golibvirt.DomainStatsRecord, error) {
	if metricNumber == 0 {
		// do not need to do expensive call if no stats were set to gather
		return nil, nil
	}

	allDomainStatesFlag := golibvirt.ConnectGetAllDomainsStatsRunning + golibvirt.ConnectGetAllDomainsStatsPaused +
		golibvirt.ConnectGetAllDomainsStatsShutoff + golibvirt.ConnectGetAllDomainsStatsOther

	return l.libvirt.ConnectGetAllDomainStats(domains, metricNumber, uint32(allDomainStatesFlag))
}

func (l *utilsImpl) gatherNumberOfPCPUs() (int, error) {
	//nolint:dogsled //Using only needed values from library function
	_, _, _, _, nodes, sockets, cores, threads, err := l.libvirt.NodeGetInfo()
	if err != nil {
		return 0, err
	}

	return int(nodes * sockets * cores * threads), nil
}

// gatherVcpuMapping is based on official go-libvirt library:
// https://github.com/libvirt/libvirt-go-module/blob/268a5d02e00cc9b3d5d7fa6c08d753071e7d14b8/domain.go#L4516
// (this library cannot be used here because of C bindings)
func (l *utilsImpl) gatherVcpuMapping(domain golibvirt.Domain, pCPUs int, shouldGetCurrentPCPU bool) ([]vcpuAffinity, error) {
	//nolint:dogsled //Using only needed values from library function
	_, _, _, vCPUs, _, err := l.libvirt.DomainGetInfo(domain)
	if err != nil {
		return nil, err
	}

	bytesToHoldPCPUs := (pCPUs + 7) / 8

	cpuInfo, vcpuPinInfo, err := l.libvirt.DomainGetVcpus(domain, int32(vCPUs), int32(bytesToHoldPCPUs))
	if err != nil {
		// DomainGetVcpus gets not only affinity (1:N mapping from VCPU to PCPU)
		// but also realtime 1:1 mapping from VCPU to PCPU
		// Unfortunately it will return nothing (only error) for inactive domains -> for that case use
		// DomainGetVcpuPinInfo (which only gets affinity but even for inactive domains)

		vcpuPinInfo, _, err = l.libvirt.DomainGetVcpuPinInfo(domain, int32(vCPUs), int32(bytesToHoldPCPUs), uint32(golibvirt.DomainAffectCurrent))
		if err != nil {
			return nil, err
		}
	}

	var vcpuAffinities []vcpuAffinity
	for i := 0; i < int(vCPUs); i++ {
		var coresAffinity []string
		for j := 0; j < pCPUs; j++ {
			aByte := (i * bytesToHoldPCPUs) + (j / 8)
			aBit := j % 8

			if (vcpuPinInfo[aByte] & (1 << uint(aBit))) != 0 {
				coresAffinity = append(coresAffinity, strconv.Itoa(j))
			}
		}

		vcpu := vcpuAffinity{
			vcpuID:        strconv.FormatInt(int64(i), 10),
			coresAffinity: strings.Join(coresAffinity, ","),
			currentPCPUID: -1,
		}

		if shouldGetCurrentPCPU && i < len(cpuInfo) {
			vcpu.currentPCPUID = cpuInfo[i].CPU
		}

		if len(coresAffinity) > 0 {
			vcpuAffinities = append(vcpuAffinities, vcpu)
		}
	}

	return vcpuAffinities, nil
}

func (l *utilsImpl) ensureConnected(libvirtURI string) error {
	if isConnected(l.libvirt) {
		return nil
	}

	driver, err := libvirtutils.ConnectByUriString(libvirtURI)
	if err != nil {
		return err
	}
	l.libvirt = driver
	return nil
}

func (l *utilsImpl) disconnect() error {
	l.libvirt = nil
	return nil
}

func isConnected(driver *golibvirt.Libvirt) bool {
	if driver == nil {
		return false
	}

	select {
	case <-driver.Disconnected():
		return false
	default:
	}
	return true
}
