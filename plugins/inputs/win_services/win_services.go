//go:generate ../../../tools/readme_config_includer/generator
//go:build windows

package win_services

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type serviceError struct {
	Message string
	Service string
	Err     error
}

func (e *serviceError) Error() string {
	return fmt.Sprintf("%s: %q: %v", e.Message, e.Service, e.Err)
}

func IsPermission(err error) bool {
	var serviceErr *serviceError
	if errors.As(err, &serviceErr) {
		return errors.Is(serviceErr, fs.ErrPermission)
	}
	return false
}

// WinService provides interface for svc.Service
type WinService interface {
	Close() error
	Config() (mgr.Config, error)
	Query() (svc.Status, error)
}

// ManagerProvider sets interface for acquiring manager instance, like mgr.Mgr
type ManagerProvider interface {
	Connect() (WinServiceManager, error)
}

// WinServiceManager provides interface for mgr.Mgr
type WinServiceManager interface {
	Disconnect() error
	OpenService(name string) (WinService, error)
	ListServices() ([]string, error)
}

// winSvcMgr is wrapper for mgr.Mgr implementing WinServiceManager interface
type winSvcMgr struct {
	realMgr *mgr.Mgr
}

func (m *winSvcMgr) Disconnect() error {
	return m.realMgr.Disconnect()
}

func (m *winSvcMgr) OpenService(name string) (WinService, error) {
	serviceName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("cannot convert service name %q: %w", name, err)
	}
	h, err := windows.OpenService(m.realMgr.Handle, serviceName, windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	return &mgr.Service{Name: name, Handle: h}, nil
}

func (m *winSvcMgr) ListServices() ([]string, error) {
	return m.realMgr.ListServices()
}

// mgProvider is an implementation of WinServiceManagerProvider interface returning winSvcMgr
type mgProvider struct {
}

func (*mgProvider) Connect() (WinServiceManager, error) {
	h, err := windows.OpenSCManager(nil, nil, windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	scmgr := &mgr.Mgr{Handle: h}
	return &winSvcMgr{scmgr}, nil
}

// WinServices is an implementation if telegraf.Input interface, providing info about Windows Services
type WinServices struct {
	Log telegraf.Logger

	ServiceNames         []string `toml:"service_names"`
	ServiceNamesExcluded []string `toml:"excluded_service_names"`
	mgrProvider          ManagerProvider

	servicesFilter filter.Filter
}

type serviceInfo struct {
	ServiceName string
	DisplayName string
	State       int
	StartUpMode int
}

func (*WinServices) SampleConfig() string {
	return sampleConfig
}

func (m *WinServices) Init() error {
	// For case insensitive comparison (see issue #8796) we need to transform the services
	// to lowercase
	servicesInclude := make([]string, 0, len(m.ServiceNames))
	for _, s := range m.ServiceNames {
		servicesInclude = append(servicesInclude, strings.ToLower(s))
	}
	servicesExclude := make([]string, 0, len(m.ServiceNamesExcluded))
	for _, s := range m.ServiceNamesExcluded {
		servicesExclude = append(servicesExclude, strings.ToLower(s))
	}

	f, err := filter.NewIncludeExcludeFilter(servicesInclude, servicesExclude)
	if err != nil {
		return err
	}
	m.servicesFilter = f

	return nil
}

func (m *WinServices) Gather(acc telegraf.Accumulator) error {
	scmgr, err := m.mgrProvider.Connect()
	if err != nil {
		return fmt.Errorf("could not open service manager: %w", err)
	}
	defer scmgr.Disconnect()

	serviceNames, err := m.listServices(scmgr)
	if err != nil {
		return err
	}

	for _, srvName := range serviceNames {
		service, err := collectServiceInfo(scmgr, srvName)
		if err != nil {
			if IsPermission(err) {
				m.Log.Debug(err.Error())
			} else {
				m.Log.Error(err.Error())
			}
			continue
		}

		tags := map[string]string{
			"service_name": service.ServiceName,
		}
		// display name could be empty, but still valid service
		if len(service.DisplayName) > 0 {
			tags["display_name"] = service.DisplayName
		}

		fields := map[string]interface{}{
			"state":        service.State,
			"startup_mode": service.StartUpMode,
		}
		acc.AddFields("win_services", fields, tags)
	}

	return nil
}

// listServices returns a list of services to gather.
func (m *WinServices) listServices(scmgr WinServiceManager) ([]string, error) {
	names, err := scmgr.ListServices()
	if err != nil {
		return nil, fmt.Errorf("could not list services: %w", err)
	}

	var services []string
	for _, name := range names {
		// Compare case-insensitive. Use lowercase as we already converted the filter to use it.
		n := strings.ToLower(name)
		if m.servicesFilter.Match(n) {
			services = append(services, name)
		}
	}

	return services, nil
}

// collectServiceInfo gathers info about a service.
func collectServiceInfo(scmgr WinServiceManager, serviceName string) (*serviceInfo, error) {
	srv, err := scmgr.OpenService(serviceName)
	if err != nil {
		return nil, &serviceError{
			Message: "could not open service",
			Service: serviceName,
			Err:     err,
		}
	}
	defer srv.Close()

	srvStatus, err := srv.Query()
	if err != nil {
		return nil, &serviceError{
			Message: "could not query service",
			Service: serviceName,
			Err:     err,
		}
	}

	srvCfg, err := srv.Config()
	if err != nil {
		return nil, &serviceError{
			Message: "could not get config of service",
			Service: serviceName,
			Err:     err,
		}
	}

	serviceInfo := &serviceInfo{
		ServiceName: serviceName,
		DisplayName: srvCfg.DisplayName,
		StartUpMode: int(srvCfg.StartType),
		State:       int(srvStatus.State),
	}
	return serviceInfo, nil
}

func init() {
	inputs.Add("win_services", func() telegraf.Input {
		return &WinServices{
			mgrProvider: &mgProvider{},
		}
	})
}
