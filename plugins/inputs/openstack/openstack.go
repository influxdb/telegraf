// Package openstack implements an OpenStack input plugin for Telegraf
//
// The OpenStack input plug is a simple two phase metric collector.  In the first
// pass a set of gatherers are run against the API to cache collections of resources.
// In the second phase the gathered resources are combined and emitted as metrics.
//
// No aggregation is performed by the input plugin, instead queries to InfluxDB should
// be used to gather global totals of things such as tag frequency.
//
//go:generate ../../../tools/readme_config_includer/generator
package openstack

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/schedulerstats"
	cinder_services "github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/services"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/aggregates"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/diagnostics"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/hypervisors"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	nova_services "github.com/gophercloud/gophercloud/v2/openstack/compute/v2/services"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/projects"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/services"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/agents"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/gophercloud/gophercloud/v2/openstack/orchestration/v1/stacks"

	"github.com/influxdata/telegraf"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	typePort    = regexp.MustCompile(`_rx$|_rx_drop$|_rx_errors$|_rx_packets$|_tx$|_tx_drop$|_tx_errors$|_tx_packets$`)
	typeCPU     = regexp.MustCompile(`cpu[0-9]{1,2}_time$`)
	typeStorage = regexp.MustCompile(`_errors$|_read$|_read_req$|_write$|_write_req$`)
)

type OpenStack struct {
	// Configuration variables
	IdentityEndpoint string          `toml:"authentication_endpoint"`
	Domain           string          `toml:"domain"`
	Project          string          `toml:"project"`
	Username         string          `toml:"username"`
	Password         string          `toml:"password"`
	EnabledServices  []string        `toml:"enabled_services"`
	ServerDiagnotics bool            `toml:"server_diagnotics" deprecated:"1.32.0;1.40.0;add 'serverdiagnostics' to 'enabled_services' instead"`
	OutputSecrets    bool            `toml:"output_secrets"`
	TagPrefix        string          `toml:"tag_prefix"`
	TagValue         string          `toml:"tag_value"`
	HumanReadableTS  bool            `toml:"human_readable_timestamps"`
	MeasureRequest   bool            `toml:"measure_openstack_requests"`
	AllTenants       bool            `toml:"query_all_tenants"`
	Log              telegraf.Logger `toml:"-"`
	common_http.HTTPClientConfig

	client *http.Client

	// Locally cached clients
	identity *gophercloud.ServiceClient
	compute  *gophercloud.ServiceClient
	volume   *gophercloud.ServiceClient
	network  *gophercloud.ServiceClient
	stack    *gophercloud.ServiceClient

	// Locally cached resources
	openstackFlavors  map[string]flavors.Flavor
	openstackProjects map[string]projects.Project
	openstackServices map[string]services.Service

	services map[string]bool
}

func (*OpenStack) SampleConfig() string {
	return sampleConfig
}

func (o *OpenStack) Init() error {
	if len(o.EnabledServices) == 0 {
		o.EnabledServices = []string{"services", "projects", "hypervisors", "flavors", "networks", "volumes"}
	}
	sort.Strings(o.EnabledServices)
	if o.Username == "" || o.Password == "" {
		return errors.New("username or password can not be empty string")
	}
	if o.TagValue == "" {
		return errors.New("tag_value option can not be empty string")
	}

	// For backward compatibility
	if o.ServerDiagnotics && !slices.Contains(o.EnabledServices, "serverdiagnostics") {
		o.EnabledServices = append(o.EnabledServices, "serverdiagnostics")
	}

	// Check the enabled services
	o.services = make(map[string]bool, len(o.EnabledServices))
	for _, service := range o.EnabledServices {
		switch service {
		case "agents", "aggregates", "cinder_services", "flavors", "hypervisors",
			"networks", "nova_services", "ports", "projects", "servers",
			"serverdiagnostics", "services", "stacks", "storage_pools",
			"subnets", "volumes":
			o.services[service] = true
		default:
			return fmt.Errorf("invalid service %q", service)
		}
	}

	return nil
}

func (o *OpenStack) Start(telegraf.Accumulator) error {
	// Authenticate against Keystone and get a token provider
	provider, err := openstack.NewClient(o.IdentityEndpoint)
	if err != nil {
		return fmt.Errorf("unable to create client for OpenStack endpoint: %w", err)
	}

	ctx := context.Background()
	client, err := o.HTTPClientConfig.CreateClient(ctx, o.Log)
	if err != nil {
		return err
	}

	o.client = client
	provider.HTTPClient = *o.client

	// Authenticate to the endpoint
	authOption := gophercloud.AuthOptions{
		IdentityEndpoint: o.IdentityEndpoint,
		DomainName:       o.Domain,
		TenantName:       o.Project,
		Username:         o.Username,
		Password:         o.Password,
		AllowReauth:      true,
	}
	if err := openstack.Authenticate(ctx, provider, authOption); err != nil {
		return fmt.Errorf("unable to authenticate OpenStack user: %w", err)
	}

	// Create required clients and attach to the OpenStack struct
	o.identity, err = openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return fmt.Errorf("unable to create V3 identity client: %w", err)
	}
	o.compute, err = openstack.NewComputeV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return fmt.Errorf("unable to create V2 compute client: %w", err)
	}
	o.network, err = openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return fmt.Errorf("unable to create V2 network client: %w", err)
	}

	// Check if we got a v3 authentication as we can skip the service listing
	// in this case and extract the services from the authentication response.
	// Otherwise we are falling back to the "services" API.
	if success, err := o.availableServicesFromAuth(provider); !success || err != nil {
		if err != nil {
			o.Log.Warnf("failed to get services from v3 authentication: %v; falling back to services API", err)
		}
		// Determine the services available at the endpoint
		if err := o.availableServices(ctx); err != nil {
			return fmt.Errorf("failed to get resource openstack services: %w", err)
		}
	}

	// Setup the optional services
	var hasOrchestration bool
	var hasBlockStorage bool
	for _, available := range o.openstackServices {
		switch available.Type {
		case "orchestration":
			o.stack, err = openstack.NewOrchestrationV1(provider, gophercloud.EndpointOpts{})
			if err != nil {
				return fmt.Errorf("unable to create V1 stack client: %w", err)
			}
			hasOrchestration = true
		case "volumev3":
			o.volume, err = openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{})
			if err != nil {
				return fmt.Errorf("unable to create V3 volume client: %w", err)
			}
			hasBlockStorage = true
		}
	}

	// Check if we need to disable services that are enabled by the user
	if !hasOrchestration {
		if o.services["stacks"] {
			o.Log.Warn("Disabling \"stacks\" service because orchestration is not available at the endpoint!")
			delete(o.services, "stacks")
		}
	}
	if !hasBlockStorage {
		for _, s := range []string{"cinder_services", "storage_pools", "volumes"} {
			if o.services[s] {
				o.Log.Warnf("Disabling %q service because block-storage is not available at the endpoint!", s)
				delete(o.services, s)
			}
		}
	}

	// Prepare cross-dependency information
	o.openstackFlavors = make(map[string]flavors.Flavor)
	o.openstackProjects = make(map[string]projects.Project)
	if slices.Contains(o.EnabledServices, "servers") {
		// We need the flavors to output machine details for servers
		page, err := flavors.ListDetail(o.compute, nil).AllPages(ctx)
		if err != nil {
			return fmt.Errorf("unable to list flavors: %w", err)
		}
		extractedflavors, err := flavors.ExtractFlavors(page)
		if err != nil {
			return fmt.Errorf("unable to extract flavors: %w", err)
		}
		for _, flavor := range extractedflavors {
			o.openstackFlavors[flavor.ID] = flavor
		}

		// We need the project to deliver a human readable name in servers
		page, err = projects.ListAvailable(o.identity).AllPages(ctx)
		if err != nil {
			return fmt.Errorf("unable to list projects: %w", err)
		}
		extractedProjects, err := projects.ExtractProjects(page)
		if err != nil {
			return fmt.Errorf("unable to extract projects: %w", err)
		}
		for _, project := range extractedProjects {
			o.openstackProjects[project.ID] = project
		}
	}

	return nil
}

func (o *OpenStack) Gather(acc telegraf.Accumulator) error {
	ctx := context.Background()
	callDuration := make(map[string]interface{}, len(o.services))

	for service := range o.services {
		var err error

		start := time.Now()
		switch service {
		case "services":
			// As Services are already gathered in Init(), using this to accumulate them.
			for _, service := range o.openstackServices {
				tags := map[string]string{
					"name": service.Type,
				}
				fields := map[string]interface{}{
					"service_id":      service.ID,
					"service_enabled": service.Enabled,
				}
				acc.AddFields("openstack_service", fields, tags)
			}
			continue
		case "projects":
			err = o.gatherProjects(ctx, acc)
		case "hypervisors":
			err = o.gatherHypervisors(ctx, acc)
		case "flavors":
			err = o.gatherFlavors(ctx, acc)
		case "volumes":
			err = o.gatherVolumes(ctx, acc)
		case "storage_pools":
			err = o.gatherStoragePools(ctx, acc)
		case "subnets":
			err = o.gatherSubnets(ctx, acc)
		case "ports":
			err = o.gatherPorts(ctx, acc)
		case "networks":
			err = o.gatherNetworks(ctx, acc)
		case "aggregates":
			err = o.gatherAggregates(ctx, acc)
		case "nova_services":
			err = o.gatherNovaServices(ctx, acc)
		case "cinder_services":
			err = o.gatherCinderServices(ctx, acc)
		case "agents":
			err = o.gatherAgents(ctx, acc)
		case "servers":
			err = o.gatherServers(ctx, acc)
		case "serverdiagnostics":
			err = o.gatherServerDiagnostics(ctx, acc)
		case "stacks":
			err = o.gatherStacks(ctx, acc)
		default:
			return fmt.Errorf("invalid service %q", service)
		}
		if err != nil {
			acc.AddError(fmt.Errorf("failed to get resource %q: %w", service, err))
		}
		callDuration[service] = time.Since(start).Nanoseconds()
	}

	if o.MeasureRequest {
		for service, duration := range callDuration {
			acc.AddFields("openstack_request_duration", map[string]interface{}{service: duration}, make(map[string]string))
		}
	}

	return nil
}

func (o *OpenStack) Stop() {
	if o.client != nil {
		o.client.CloseIdleConnections()
	}
}

func (o *OpenStack) availableServicesFromAuth(provider *gophercloud.ProviderClient) (bool, error) {
	authResult := provider.GetAuthResult()
	if authResult == nil {
		return false, nil
	}

	resultV3, ok := authResult.(tokens.CreateResult)
	if !ok {
		return false, nil
	}
	catalog, err := resultV3.ExtractServiceCatalog()
	if err != nil {
		return false, err
	}

	if len(catalog.Entries) == 0 {
		return false, nil
	}

	o.openstackServices = make(map[string]services.Service, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		o.openstackServices[entry.ID] = services.Service{
			ID:      entry.ID,
			Type:    entry.Type,
			Enabled: true,
		}
	}

	return true, nil
}

// availableServices collects the available endpoint services via API
func (o *OpenStack) availableServices(ctx context.Context) error {
	page, err := services.List(o.identity, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list services: %w", err)
	}
	extractedServices, err := services.ExtractServices(page)
	if err != nil {
		return fmt.Errorf("unable to extract services: %w", err)
	}

	o.openstackServices = make(map[string]services.Service, len(extractedServices))
	for _, service := range extractedServices {
		o.openstackServices[service.ID] = service
	}

	return nil
}

// gatherStacks collects and accumulates stacks data from the OpenStack API.
func (o *OpenStack) gatherStacks(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := stacks.List(o.stack, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list stacks: %w", err)
	}
	extractedStacks, err := stacks.ExtractStacks(page)
	if err != nil {
		return fmt.Errorf("unable to extract stacks: %w", err)
	}
	for _, stack := range extractedStacks {
		tags := map[string]string{
			"description": stack.Description,
			"name":        stack.Name,
		}
		for _, stackTag := range stack.Tags {
			tags[o.TagPrefix+stackTag] = o.TagValue
		}
		fields := map[string]interface{}{
			"status":        strings.ToLower(stack.Status),
			"id":            stack.ID,
			"status_reason": stack.StatusReason,
			"creation_time": o.convertTimeFormat(stack.CreationTime),
			"updated_time":  o.convertTimeFormat(stack.UpdatedTime),
		}
		acc.AddFields("openstack_stack", fields, tags)
	}

	return nil
}

// gatherNovaServices collects and accumulates nova_services data from the OpenStack API.
func (o *OpenStack) gatherNovaServices(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := nova_services.List(o.compute, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list nova_services: %w", err)
	}
	novaServices, err := nova_services.ExtractServices(page)
	if err != nil {
		return fmt.Errorf("unable to extract nova_services: %w", err)
	}
	for _, novaService := range novaServices {
		tags := map[string]string{
			"name":         novaService.Binary,
			"host_machine": novaService.Host,
			"state":        novaService.State,
			"status":       strings.ToLower(novaService.Status),
			"zone":         novaService.Zone,
		}
		fields := map[string]interface{}{
			"id":              novaService.ID,
			"disabled_reason": novaService.DisabledReason,
			"forced_down":     novaService.ForcedDown,
			"updated_at":      o.convertTimeFormat(novaService.UpdatedAt),
		}
		acc.AddFields("openstack_nova_service", fields, tags)
	}

	return nil
}

// gatherCinderServices collects and accumulates cinder_services data from the OpenStack API.
func (o *OpenStack) gatherCinderServices(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := cinder_services.List(o.volume, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list cinder_services: %w", err)
	}
	cinderServices, err := cinder_services.ExtractServices(page)
	if err != nil {
		return fmt.Errorf("unable to extract cinder_services: %w", err)
	}
	for _, cinderService := range cinderServices {
		tags := map[string]string{
			"name":         cinderService.Binary,
			"cluster":      cinderService.Cluster,
			"host_machine": cinderService.Host,
			"state":        cinderService.State,
			"status":       strings.ToLower(cinderService.Status),
			"zone":         cinderService.Zone,
		}
		fields := map[string]interface{}{
			"id":                 cinderService.ActiveBackendID,
			"disabled_reason":    cinderService.DisabledReason,
			"frozen":             cinderService.Frozen,
			"replication_status": cinderService.ReplicationStatus,
			"updated_at":         o.convertTimeFormat(cinderService.UpdatedAt),
		}
		acc.AddFields("openstack_cinder_service", fields, tags)
	}

	return nil
}

// gatherSubnets collects and accumulates subnets data from the OpenStack API.
func (o *OpenStack) gatherSubnets(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := subnets.List(o.network, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list subnets: %w", err)
	}
	extractedSubnets, err := subnets.ExtractSubnets(page)
	if err != nil {
		return fmt.Errorf("unable to extract subnets: %w", err)
	}
	for _, subnet := range extractedSubnets {
		var allocationPools []string
		for _, pool := range subnet.AllocationPools {
			allocationPools = append(allocationPools, pool.Start+"-"+pool.End)
		}
		tags := map[string]string{
			"network_id":        subnet.NetworkID,
			"name":              subnet.Name,
			"description":       subnet.Description,
			"ip_version":        strconv.Itoa(subnet.IPVersion),
			"cidr":              subnet.CIDR,
			"gateway_ip":        subnet.GatewayIP,
			"tenant_id":         subnet.TenantID,
			"project_id":        subnet.ProjectID,
			"ipv6_address_mode": subnet.IPv6AddressMode,
			"ipv6_ra_mode":      subnet.IPv6RAMode,
			"subnet_pool_id":    subnet.SubnetPoolID,
		}
		for _, subnetTag := range subnet.Tags {
			tags[o.TagPrefix+subnetTag] = o.TagValue
		}
		fields := map[string]interface{}{
			"id":               subnet.ID,
			"dhcp_enabled":     subnet.EnableDHCP,
			"dns_nameservers":  strings.Join(subnet.DNSNameservers[:], ","),
			"allocation_pools": strings.Join(allocationPools[:], ","),
		}
		acc.AddFields("openstack_subnet", fields, tags)
	}
	return nil
}

// gatherPorts collects and accumulates ports data from the OpenStack API.
func (o *OpenStack) gatherPorts(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := ports.List(o.network, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list ports: %w", err)
	}
	extractedPorts, err := ports.ExtractPorts(page)
	if err != nil {
		return fmt.Errorf("unable to extract ports: %w", err)
	}
	for _, port := range extractedPorts {
		tags := map[string]string{
			"network_id":   port.NetworkID,
			"name":         port.Name,
			"description":  port.Description,
			"status":       strings.ToLower(port.Status),
			"tenant_id":    port.TenantID,
			"project_id":   port.ProjectID,
			"device_owner": port.DeviceOwner,
			"device_id":    port.DeviceID,
		}
		for _, portTag := range port.Tags {
			tags[o.TagPrefix+portTag] = o.TagValue
		}
		fields := map[string]interface{}{
			"id":                    port.ID,
			"mac_address":           port.MACAddress,
			"admin_state_up":        port.AdminStateUp,
			"fixed_ips":             len(port.FixedIPs),
			"allowed_address_pairs": len(port.AllowedAddressPairs),
			"security_groups":       strings.Join(port.SecurityGroups[:], ","),
		}
		if len(port.FixedIPs) > 0 {
			for _, ip := range port.FixedIPs {
				fields["subnet_id"] = ip.SubnetID
				fields["ip_address"] = ip.IPAddress
				acc.AddFields("openstack_port", fields, tags)
			}
		} else {
			acc.AddFields("openstack_port", fields, tags)
		}
	}
	return nil
}

// gatherNetworks collects and accumulates networks data from the OpenStack API.
func (o *OpenStack) gatherNetworks(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := networks.List(o.network, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list networks: %w", err)
	}
	extractedNetworks, err := networks.ExtractNetworks(page)
	if err != nil {
		return fmt.Errorf("unable to extract networks: %w", err)
	}
	for _, network := range extractedNetworks {
		tags := map[string]string{
			"name":        network.Name,
			"description": network.Description,
			"status":      strings.ToLower(network.Status),
			"tenant_id":   network.TenantID,
			"project_id":  network.ProjectID,
		}
		for _, networkTag := range network.Tags {
			tags[o.TagPrefix+networkTag] = o.TagValue
		}
		fields := map[string]interface{}{
			"id":                      network.ID,
			"admin_state_up":          network.AdminStateUp,
			"subnets":                 len(network.Subnets),
			"shared":                  network.Shared,
			"availability_zone_hints": strings.Join(network.AvailabilityZoneHints[:], ","),
			"updated_at":              o.convertTimeFormat(network.UpdatedAt),
			"created_at":              o.convertTimeFormat(network.CreatedAt),
		}
		if len(network.Subnets) > 0 {
			for _, subnet := range network.Subnets {
				fields["subnet_id"] = subnet
				acc.AddFields("openstack_network", fields, tags)
			}
		} else {
			acc.AddFields("openstack_network", fields, tags)
		}
	}
	return nil
}

// gatherAgents collects and accumulates agents data from the OpenStack API.
func (o *OpenStack) gatherAgents(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := agents.List(o.network, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list neutron agents: %w", err)
	}
	extractedAgents, err := agents.ExtractAgents(page)
	if err != nil {
		return fmt.Errorf("unable to extract neutron agents: %w", err)
	}
	for _, agent := range extractedAgents {
		tags := map[string]string{
			"agent_type":        agent.AgentType,
			"availability_zone": agent.AvailabilityZone,
			"binary":            agent.Binary,
			"description":       agent.Description,
			"agent_host":        agent.Host,
			"topic":             agent.Topic,
		}
		fields := map[string]interface{}{
			"id":                  agent.ID,
			"admin_state_up":      agent.AdminStateUp,
			"alive":               agent.Alive,
			"resources_synced":    agent.ResourcesSynced,
			"created_at":          o.convertTimeFormat(agent.CreatedAt),
			"started_at":          o.convertTimeFormat(agent.StartedAt),
			"heartbeat_timestamp": o.convertTimeFormat(agent.HeartbeatTimestamp),
		}
		acc.AddFields("openstack_neutron_agent", fields, tags)
	}
	return nil
}

// gatherAggregates collects and accumulates aggregates data from the OpenStack API.
func (o *OpenStack) gatherAggregates(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := aggregates.List(o.compute).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list aggregates: %w", err)
	}
	extractedAggregates, err := aggregates.ExtractAggregates(page)
	if err != nil {
		return fmt.Errorf("unable to extract aggregates: %w", err)
	}
	for _, aggregate := range extractedAggregates {
		tags := map[string]string{
			"availability_zone": aggregate.AvailabilityZone,
			"name":              aggregate.Name,
		}
		fields := map[string]interface{}{
			"id":              aggregate.ID,
			"aggregate_hosts": len(aggregate.Hosts),
			"deleted":         aggregate.Deleted,
			"created_at":      o.convertTimeFormat(aggregate.CreatedAt),
			"updated_at":      o.convertTimeFormat(aggregate.UpdatedAt),
			"deleted_at":      o.convertTimeFormat(aggregate.DeletedAt),
		}
		if len(aggregate.Hosts) > 0 {
			for _, host := range aggregate.Hosts {
				fields["aggregate_host"] = host
				acc.AddFields("openstack_aggregate", fields, tags)
			}
		} else {
			acc.AddFields("openstack_aggregate", fields, tags)
		}
	}
	return nil
}

// gatherProjects collects and accumulates projects data from the OpenStack API.
func (o *OpenStack) gatherProjects(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := projects.List(o.identity, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list projects: %w", err)
	}
	extractedProjects, err := projects.ExtractProjects(page)
	if err != nil {
		return fmt.Errorf("unable to extract projects: %w", err)
	}
	for _, project := range extractedProjects {
		o.openstackProjects[project.ID] = project
		tags := map[string]string{
			"description": project.Description,
			"domain_id":   project.DomainID,
			"name":        project.Name,
			"parent_id":   project.ParentID,
		}
		for _, projectTag := range project.Tags {
			tags[o.TagPrefix+projectTag] = o.TagValue
		}
		fields := map[string]interface{}{
			"id":        project.ID,
			"is_domain": project.IsDomain,
			"enabled":   project.Enabled,
			"projects":  len(extractedProjects),
		}
		acc.AddFields("openstack_identity", fields, tags)
	}
	return nil
}

// gatherHypervisors collects and accumulates hypervisors data from the OpenStack API.
func (o *OpenStack) gatherHypervisors(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := hypervisors.List(o.compute, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list hypervisors: %w", err)
	}
	extractedHypervisors, err := hypervisors.ExtractHypervisors(page)
	if err != nil {
		return fmt.Errorf("unable to extract hypervisors: %w", err)
	}

	for _, hypervisor := range extractedHypervisors {
		tags := map[string]string{
			"cpu_vendor":              hypervisor.CPUInfo.Vendor,
			"cpu_arch":                hypervisor.CPUInfo.Arch,
			"cpu_model":               hypervisor.CPUInfo.Model,
			"status":                  strings.ToLower(hypervisor.Status),
			"state":                   hypervisor.State,
			"hypervisor_hostname":     hypervisor.HypervisorHostname,
			"hypervisor_type":         hypervisor.HypervisorType,
			"hypervisor_version":      strconv.Itoa(hypervisor.HypervisorVersion),
			"service_host":            hypervisor.Service.Host,
			"service_id":              hypervisor.Service.ID,
			"service_disabled_reason": hypervisor.Service.DisabledReason,
		}
		for _, cpuFeature := range hypervisor.CPUInfo.Features {
			tags["cpu_feature_"+cpuFeature] = "true"
		}
		fields := map[string]interface{}{
			"id":                   hypervisor.ID,
			"host_ip":              hypervisor.HostIP,
			"cpu_topology_sockets": hypervisor.CPUInfo.Topology.Sockets,
			"cpu_topology_cores":   hypervisor.CPUInfo.Topology.Cores,
			"cpu_topology_threads": hypervisor.CPUInfo.Topology.Threads,
			"current_workload":     hypervisor.CurrentWorkload,
			"disk_available_least": hypervisor.DiskAvailableLeast,
			"free_disk_gb":         hypervisor.FreeDiskGB,
			"free_ram_mb":          hypervisor.FreeRamMB,
			"local_gb":             hypervisor.LocalGB,
			"local_gb_used":        hypervisor.LocalGBUsed,
			"memory_mb":            hypervisor.MemoryMB,
			"memory_mb_used":       hypervisor.MemoryMBUsed,
			"running_vms":          hypervisor.RunningVMs,
			"vcpus":                hypervisor.VCPUs,
			"vcpus_used":           hypervisor.VCPUsUsed,
		}
		acc.AddFields("openstack_hypervisor", fields, tags)
	}

	return nil
}

// gatherFlavors collects and accumulates flavors data from the OpenStack API.
func (o *OpenStack) gatherFlavors(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := flavors.ListDetail(o.compute, nil).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list flavors: %w", err)
	}
	extractedflavors, err := flavors.ExtractFlavors(page)
	if err != nil {
		return fmt.Errorf("unable to extract flavors: %w", err)
	}
	for _, flavor := range extractedflavors {
		o.openstackFlavors[flavor.ID] = flavor
		tags := map[string]string{
			"name":      flavor.Name,
			"is_public": strconv.FormatBool(flavor.IsPublic),
		}
		fields := map[string]interface{}{
			"id":          flavor.ID,
			"disk":        flavor.Disk,
			"ram":         flavor.RAM,
			"rxtx_factor": flavor.RxTxFactor,
			"swap":        flavor.Swap,
			"vcpus":       flavor.VCPUs,
			"ephemeral":   flavor.Ephemeral,
		}
		acc.AddFields("openstack_flavor", fields, tags)
	}
	return nil
}

// gatherVolumes collects and accumulates volumes data from the OpenStack API.
func (o *OpenStack) gatherVolumes(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := volumes.List(o.volume, &volumes.ListOpts{AllTenants: o.AllTenants}).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list volumes: %w", err)
	}
	extractedVolumes, err := volumes.ExtractVolumes(page)
	if err != nil {
		return fmt.Errorf("unable to extract volumes: %w", err)
	}
	for _, volume := range extractedVolumes {
		tags := map[string]string{
			"status":               strings.ToLower(volume.Status),
			"availability_zone":    volume.AvailabilityZone,
			"name":                 volume.Name,
			"description":          volume.Description,
			"volume_type":          volume.VolumeType,
			"snapshot_id":          volume.SnapshotID,
			"source_volid":         volume.SourceVolID,
			"bootable":             volume.Bootable,
			"replication_status":   strings.ToLower(volume.ReplicationStatus),
			"consistency_group_id": volume.ConsistencyGroupID,
		}
		fields := map[string]interface{}{
			"id":                volume.ID,
			"size":              volume.Size,
			"total_attachments": len(volume.Attachments),
			"encrypted":         volume.Encrypted,
			"multiattach":       volume.Multiattach,
			"created_at":        o.convertTimeFormat(volume.CreatedAt),
			"updated_at":        o.convertTimeFormat(volume.UpdatedAt),
		}
		if o.OutputSecrets {
			tags["user_id"] = volume.UserID
		}
		if len(volume.Attachments) > 0 {
			for _, attachment := range volume.Attachments {
				if !o.HumanReadableTS {
					fields["attachment_attached_at"] = attachment.AttachedAt.UnixNano()
				} else {
					fields["attachment_attached_at"] = attachment.AttachedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
				}
				tags["attachment_attachment_id"] = attachment.AttachmentID
				tags["attachment_device"] = attachment.Device
				tags["attachment_host_name"] = attachment.HostName
				fields["attachment_server_id"] = attachment.ServerID
				acc.AddFields("openstack_volume", fields, tags)
			}
		} else {
			acc.AddFields("openstack_volume", fields, tags)
		}
	}
	return nil
}

// gatherStoragePools collects and accumulates storage pools data from the OpenStack API.
func (o *OpenStack) gatherStoragePools(ctx context.Context, acc telegraf.Accumulator) error {
	results, err := schedulerstats.List(o.volume, &schedulerstats.ListOpts{Detail: true}).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list storage pools: %w", err)
	}
	storagePools, err := schedulerstats.ExtractStoragePools(results)
	if err != nil {
		return fmt.Errorf("unable to extract storage pools: %w", err)
	}
	for _, storagePool := range storagePools {
		tags := map[string]string{
			"name":                storagePool.Capabilities.VolumeBackendName,
			"driver_version":      storagePool.Capabilities.DriverVersion,
			"storage_protocol":    storagePool.Capabilities.StorageProtocol,
			"vendor_name":         storagePool.Capabilities.VendorName,
			"volume_backend_name": storagePool.Capabilities.VolumeBackendName,
		}
		fields := map[string]interface{}{
			"total_capacity_gb": storagePool.Capabilities.TotalCapacityGB,
			"free_capacity_gb":  storagePool.Capabilities.FreeCapacityGB,
		}
		acc.AddFields("openstack_storage_pool", fields, tags)
	}
	return nil
}

func (o *OpenStack) gatherServers(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := servers.List(o.compute, &servers.ListOpts{AllTenants: o.AllTenants}).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list servers: %w", err)
	}
	extractedServers, err := servers.ExtractServers(page)
	if err != nil {
		return fmt.Errorf("unable to extract servers: %w", err)
	}

	for i := range extractedServers {
		server := &extractedServers[i]

		// Try derive the associated project
		project := "unknown"
		if p, ok := o.openstackProjects[server.TenantID]; ok {
			project = p.Name
		}

		// Try to derive the hostname
		var hostname string
		if server.Host != "" {
			hostname = server.Host
		} else if server.Hostname != nil && *server.Hostname != "" {
			hostname = *server.Hostname
		} else if server.HypervisorHostname != "" {
			hostname = server.HypervisorHostname
		} else {
			hostname = server.HostID
		}

		tags := map[string]string{
			"tenant_id": server.TenantID,
			"name":      server.Name,
			"host_id":   server.HostID,
			"status":    strings.ToLower(server.Status),
			"key_name":  server.KeyName,
			"host_name": hostname,
			"project":   project,
		}

		// Extract the flavor details to avoid joins (ignore errors and leave as zero values)
		var vcpus, ram, disk int
		if flavorIDInterface, found := server.Flavor["id"]; found {
			if flavorID, ok := flavorIDInterface.(string); ok {
				tags["flavor"] = flavorID
				if flavor, ok := o.openstackFlavors[flavorID]; ok {
					vcpus = flavor.VCPUs
					ram = flavor.RAM
					disk = flavor.Disk
				}
			}
		}
		if imageIDInterface, found := server.Image["id"]; found {
			if imageID, ok := imageIDInterface.(string); ok {
				tags["image"] = imageID
			}
		}
		fields := map[string]interface{}{
			"id":               server.ID,
			"progress":         server.Progress,
			"accessIPv4":       server.AccessIPv4,
			"accessIPv6":       server.AccessIPv6,
			"addresses":        len(server.Addresses),
			"security_groups":  len(server.SecurityGroups),
			"volumes_attached": len(server.AttachedVolumes),
			"fault_code":       server.Fault.Code,
			"fault_details":    server.Fault.Details,
			"fault_message":    server.Fault.Message,
			"vcpus":            vcpus,
			"ram_mb":           ram,
			"disk_gb":          disk,
			"fault_created":    o.convertTimeFormat(server.Fault.Created),
			"updated":          o.convertTimeFormat(server.Updated),
			"created":          o.convertTimeFormat(server.Created),
		}
		if o.OutputSecrets {
			tags["user_id"] = server.UserID
			fields["adminPass"] = server.AdminPass
		}
		if len(server.AttachedVolumes) == 0 {
			acc.AddFields("openstack_server", fields, tags)
		} else {
			for _, AttachedVolume := range server.AttachedVolumes {
				fields["volume_id"] = AttachedVolume.ID
				acc.AddFields("openstack_server", fields, tags)
			}
		}
	}
	return nil
}

func (o *OpenStack) gatherServerDiagnostics(ctx context.Context, acc telegraf.Accumulator) error {
	page, err := servers.List(o.compute, &servers.ListOpts{AllTenants: o.AllTenants}).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("unable to list servers: %w", err)
	}
	extractedServers, err := servers.ExtractServers(page)
	if err != nil {
		return fmt.Errorf("unable to extract servers: %w", err)
	}

	for i := range extractedServers {
		server := &extractedServers[i]
		if server.Status != "ACTIVE" {
			continue
		}
		diagnostic, err := diagnostics.Get(ctx, o.compute, server.ID).Extract()
		if err != nil {
			acc.AddError(fmt.Errorf("unable to get diagnostics for server %q: %w", server.ID, err))
			continue
		}

		portName := make(map[string]bool)
		storageName := make(map[string]bool)
		memoryStats := make(map[string]interface{})
		cpus := make(map[string]interface{})
		for k, v := range diagnostic {
			if typePort.MatchString(k) {
				portName[strings.Split(k, "_")[0]] = true
			} else if typeCPU.MatchString(k) {
				cpus[k] = v
			} else if typeStorage.MatchString(k) {
				storageName[strings.Split(k, "_")[0]] = true
			} else {
				memoryStats[k] = v
			}
		}
		nPorts := strconv.Itoa(len(portName))
		nDisks := strconv.Itoa(len(storageName))

		// Add metrics for disks
		fields := map[string]interface{}{
			"memory":         memoryStats["memory"],
			"memory-actual":  memoryStats["memory-actual"],
			"memory-rss":     memoryStats["memory-rss"],
			"memory-swap_in": memoryStats["memory-swap_in"],
		}
		for k, v := range cpus {
			fields[k] = v
		}
		for key := range storageName {
			fields["disk_errors"] = diagnostic[key+"_errors"]
			fields["disk_read"] = diagnostic[key+"_read"]
			fields["disk_read_req"] = diagnostic[key+"_read_req"]
			fields["disk_write"] = diagnostic[key+"_write"]
			fields["disk_write_req"] = diagnostic[key+"_write_req"]
			tags := map[string]string{
				"server_id":   server.ID,
				"no_of_ports": nPorts,
				"no_of_disks": nDisks,
				"disk_name":   key,
			}
			acc.AddFields("openstack_server_diagnostics", fields, tags)
		}

		// Add metrics for network ports
		fields = map[string]interface{}{
			"memory":         memoryStats["memory"],
			"memory-actual":  memoryStats["memory-actual"],
			"memory-rss":     memoryStats["memory-rss"],
			"memory-swap_in": memoryStats["memory-swap_in"],
		}
		for k, v := range cpus {
			fields[k] = v
		}
		for key := range portName {
			fields["port_rx"] = diagnostic[key+"_rx"]
			fields["port_rx_drop"] = diagnostic[key+"_rx_drop"]
			fields["port_rx_errors"] = diagnostic[key+"_rx_errors"]
			fields["port_rx_packets"] = diagnostic[key+"_rx_packets"]
			fields["port_tx"] = diagnostic[key+"_tx"]
			fields["port_tx_drop"] = diagnostic[key+"_tx_drop"]
			fields["port_tx_errors"] = diagnostic[key+"_tx_errors"]
			fields["port_tx_packets"] = diagnostic[key+"_tx_packets"]
			tags := map[string]string{
				"server_id":   server.ID,
				"no_of_ports": nPorts,
				"no_of_disks": nDisks,
				"port_name":   key,
			}
			acc.AddFields("openstack_server_diagnostics", fields, tags)
		}
	}
	return nil
}

// convertTimeFormat, to convert time format based on HumanReadableTS
func (o *OpenStack) convertTimeFormat(t time.Time) interface{} {
	if o.HumanReadableTS {
		return t.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return t.UnixNano()
}

func init() {
	inputs.Add("openstack", func() telegraf.Input {
		return &OpenStack{
			Domain:     "default",
			Project:    "admin",
			TagPrefix:  "openstack_tag_",
			TagValue:   "true",
			AllTenants: true,
		}
	})
}
