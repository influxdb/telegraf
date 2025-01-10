//go:build linux

package ethtool

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/vishvananda/netns"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

var downInterfacesBehaviors = []string{"expose", "skip"}

const (
	tagInterface     = "interface"
	tagNamespace     = "namespace"
	tagDriverName    = "driver"
	fieldInterfaceUp = "interface_up"
)

type Ethtool struct {
	// This is the list of interface names to include
	InterfaceInclude []string `toml:"interface_include"`

	// This is the list of interface names to ignore
	InterfaceExclude []string `toml:"interface_exclude"`

	// Behavior regarding metrics for downed interfaces
	DownInterfaces string `toml:" down_interfaces"`

	// This is the list of namespace names to include
	NamespaceInclude []string `toml:"namespace_include"`

	// This is the list of namespace names to ignore
	NamespaceExclude []string `toml:"namespace_exclude"`

	// Normalization on the key names
	NormalizeKeys []string `toml:"normalize_keys"`

	Log telegraf.Logger `toml:"-"`

	interfaceFilter   filter.Filter
	namespaceFilter   filter.Filter
	includeNamespaces bool

	// the ethtool command
	command command
}

type command interface {
	init() error
	driverName(intf namespacedInterface) (string, error)
	interfaces(includeNamespaces bool) ([]namespacedInterface, error)
	stats(intf namespacedInterface) (map[string]uint64, error)
	get(intf namespacedInterface) (map[string]uint64, error)
}

type commandEthtool struct {
	log                 telegraf.Logger
	namespaceGoroutines map[string]*namespaceGoroutine
}

func (e *Ethtool) Init() error {
	var err error
	e.interfaceFilter, err = filter.NewIncludeExcludeFilter(e.InterfaceInclude, e.InterfaceExclude)
	if err != nil {
		return err
	}

	if e.DownInterfaces == "" {
		e.DownInterfaces = "expose"
	}

	if err = choice.Check(e.DownInterfaces, downInterfacesBehaviors); err != nil {
		return fmt.Errorf("down_interfaces: %w", err)
	}

	// If no namespace include or exclude filters were provided, then default
	// to just the initial namespace.
	e.includeNamespaces = len(e.NamespaceInclude) > 0 || len(e.NamespaceExclude) > 0
	if len(e.NamespaceInclude) == 0 && len(e.NamespaceExclude) == 0 {
		e.NamespaceInclude = []string{""}
	} else if len(e.NamespaceInclude) == 0 {
		e.NamespaceInclude = []string{"*"}
	}
	e.namespaceFilter, err = filter.NewIncludeExcludeFilter(e.NamespaceInclude, e.NamespaceExclude)
	if err != nil {
		return err
	}

	if command, ok := e.command.(*commandEthtool); ok {
		command.log = e.Log
	}

	return e.command.init()
}

func (e *Ethtool) Gather(acc telegraf.Accumulator) error {
	// Get the list of interfaces
	interfaces, err := e.command.interfaces(e.includeNamespaces)
	if err != nil {
		acc.AddError(err)
		return nil
	}

	// parallelize the ethtool call in event of many interfaces
	var wg sync.WaitGroup

	for _, iface := range interfaces {
		// Check this isn't a loop back and that its matched by the filter(s)
		if e.interfaceEligibleForGather(iface) {
			wg.Add(1)

			go func(i namespacedInterface) {
				e.gatherEthtoolStats(i, acc)
				wg.Done()
			}(iface)
		}
	}

	// Waiting for all the interfaces
	wg.Wait()
	return nil
}

func (e *Ethtool) interfaceEligibleForGather(iface namespacedInterface) bool {
	// Don't gather if it is a loop back, or it isn't matched by the filter
	if isLoopback(iface) || !e.interfaceFilter.Match(iface.Name) {
		return false
	}

	// Don't gather if it's not in a namespace matched by the filter
	if !e.namespaceFilter.Match(iface.namespace.name()) {
		return false
	}

	// For downed interfaces, gather only for "expose"
	if !interfaceUp(iface) {
		return e.DownInterfaces == "expose"
	}

	return true
}

// Gather the stats for the interface.
func (e *Ethtool) gatherEthtoolStats(iface namespacedInterface, acc telegraf.Accumulator) {
	tags := make(map[string]string)
	tags[tagInterface] = iface.Name
	tags[tagNamespace] = iface.namespace.name()

	driverName, err := e.command.driverName(iface)
	if err != nil {
		acc.AddError(fmt.Errorf("%q driver: %w", iface.Name, err))
		return
	}

	tags[tagDriverName] = driverName

	fields := make(map[string]interface{})
	stats, err := e.command.stats(iface)
	if err != nil {
		acc.AddError(fmt.Errorf("%q stats: %w", iface.Name, err))
		return
	}

	fields[fieldInterfaceUp] = interfaceUp(iface)
	for k, v := range stats {
		fields[e.normalizeKey(k)] = v
	}

	cmdget, err := e.command.get(iface)
	// error text is directly from running ethtool and syscalls
	if err != nil && err.Error() != "operation not supported" {
		acc.AddError(fmt.Errorf("%q get: %w", iface.Name, err))
		return
	}
	for k, v := range cmdget {
		fields[e.normalizeKey(k)] = v
	}

	acc.AddFields(pluginName, fields, tags)
}

// normalize key string; order matters to avoid replacing whitespace with
// underscores, then trying to trim those same underscores. Likewise with
// camelcase before trying to lower case things.
func (e *Ethtool) normalizeKey(key string) string {
	// must trim whitespace or this will have a leading _
	if inStringSlice(e.NormalizeKeys, "snakecase") {
		key = camelCase2SnakeCase(strings.TrimSpace(key))
	}
	// must occur before underscore, otherwise nothing to trim
	if inStringSlice(e.NormalizeKeys, "trim") {
		key = strings.TrimSpace(key)
	}
	if inStringSlice(e.NormalizeKeys, "lower") {
		key = strings.ToLower(key)
	}
	if inStringSlice(e.NormalizeKeys, "underscore") {
		key = strings.ReplaceAll(key, " ", "_")
	}
	// aws has a conflicting name that needs to be renamed
	if key == "interface_up" {
		key = "interface_up_counter"
	}

	return key
}

func camelCase2SnakeCase(value string) string {
	matchFirstCap := regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap := regexp.MustCompile("([a-z0-9])([A-Z])")

	snake := matchFirstCap.ReplaceAllString(value, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

func inStringSlice(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}

	return false
}

func isLoopback(iface namespacedInterface) bool {
	return (iface.Flags & net.FlagLoopback) != 0
}

func interfaceUp(iface namespacedInterface) bool {
	return (iface.Flags & net.FlagUp) != 0
}

func newCommandEthtool() *commandEthtool {
	return &commandEthtool{}
}

func (c *commandEthtool) init() error {
	// Create the goroutine for the initial namespace
	initialNamespace, err := netns.Get()
	if err != nil {
		return err
	}
	nspaceGoroutine := &namespaceGoroutine{
		namespaceName: "",
		handle:        initialNamespace,
		log:           c.log,
	}
	if err := nspaceGoroutine.start(); err != nil {
		c.log.Errorf(`Failed to start goroutine for the initial namespace: %s`, err)
		return err
	}
	c.namespaceGoroutines = map[string]*namespaceGoroutine{
		"": nspaceGoroutine,
	}
	return nil
}

func (*commandEthtool) driverName(intf namespacedInterface) (driver string, err error) {
	return intf.namespace.driverName(intf)
}

func (*commandEthtool) stats(intf namespacedInterface) (stats map[string]uint64, err error) {
	return intf.namespace.stats(intf)
}

func (*commandEthtool) get(intf namespacedInterface) (stats map[string]uint64, err error) {
	return intf.namespace.get(intf)
}

func (c *commandEthtool) interfaces(includeNamespaces bool) ([]namespacedInterface, error) {
	const namespaceDirectory = "/var/run/netns"

	initialNamespace, err := netns.Get()
	if err != nil {
		c.log.Errorf("Could not get initial namespace: %s", err)
		return nil, err
	}
	defer initialNamespace.Close()

	// Gather the list of namespace names to from which to retrieve interfaces.
	initialNamespaceIsNamed := false
	var namespaceNames []string
	// Handles are only used to create namespaced goroutines. We don't prefill
	// with the handle for the initial namespace because we've already created
	// its goroutine in Init().
	handles := make(map[string]netns.NsHandle)

	if includeNamespaces {
		namespaces, err := os.ReadDir(namespaceDirectory)
		if err != nil {
			c.log.Warnf("Could not find namespace directory: %s", err)
		}

		// We'll always have at least the initial namespace, so add one to ensure
		// we have capacity for it.
		namespaceNames = make([]string, 0, len(namespaces)+1)
		for _, namespace := range namespaces {
			name := namespace.Name()
			namespaceNames = append(namespaceNames, name)

			handle, err := netns.GetFromPath(filepath.Join(namespaceDirectory, name))
			if err != nil {
				c.log.Warnf("Could not get handle for namespace %q: %s", name, err.Error())
				continue
			}
			handles[name] = handle
			if handle.Equal(initialNamespace) {
				initialNamespaceIsNamed = true
			}
		}
	}

	// We don't want to gather interfaces from the same namespace twice, and
	// it's possible, though unlikely, that the initial namespace is also a
	// named interface.
	if !initialNamespaceIsNamed {
		namespaceNames = append(namespaceNames, "")
	}

	allInterfaces := make([]namespacedInterface, 0)
	for _, namespace := range namespaceNames {
		if _, ok := c.namespaceGoroutines[namespace]; !ok {
			c.namespaceGoroutines[namespace] = &namespaceGoroutine{
				namespaceName: namespace,
				handle:        handles[namespace],
				log:           c.log,
			}
			if err := c.namespaceGoroutines[namespace].start(); err != nil {
				c.log.Errorf("Failed to start goroutine for namespace %q: %s", namespace, err.Error())
				delete(c.namespaceGoroutines, namespace)
				continue
			}
		}

		interfaces, err := c.namespaceGoroutines[namespace].interfaces()
		if err != nil {
			c.log.Warnf("Could not get interfaces from namespace %q: %s", namespace, err.Error())
			continue
		}
		allInterfaces = append(allInterfaces, interfaces...)
	}

	return allInterfaces, nil
}

func init() {
	inputs.Add(pluginName, func() telegraf.Input {
		return &Ethtool{
			command: newCommandEthtool(),
		}
	})
}
