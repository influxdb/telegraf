package vsphere

import (
	"context"
	"reflect"
	"strings"

	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var childTypes map[string][]string

var addFields map[string][]string

var containers map[string]interface{}

// Finder allows callers to find resources in vCenter given a query string.
type Finder struct {
	client *Client
}

// ResourceFilter is a convenience class holding a finder and a set of paths. It is useful when you need a
// self contained object capable of returning a certain set of resources.
type ResourceFilter struct {
	finder       *Finder
	resType      string
	paths        []string
	excludePaths []string
}

// FindAll returns the union of resources found given the supplied resource type and paths.
func (f *Finder) FindAll(ctx context.Context, resType string, paths, excludePaths []string, dst interface{}) error {
	objs := make(map[string]types.ObjectContent)
	for _, p := range paths {
		if err := f.findResources(ctx, resType, p, objs); err != nil {
			return err
		}
	}
	if len(excludePaths) > 0 {
		excludes := make(map[string]types.ObjectContent)
		for _, p := range excludePaths {
			if err := f.findResources(ctx, resType, p, excludes); err != nil {
				return err
			}
		}
		for k := range excludes {
			delete(objs, k)
		}
	}
	return objectContentToTypedArray(objs, dst)
}

// Find returns the resources matching the specified path.
func (f *Finder) Find(ctx context.Context, resType, path string, dst interface{}) error {
	objs := make(map[string]types.ObjectContent)
	err := f.findResources(ctx, resType, path, objs)
	if err != nil {
		return err
	}
	return objectContentToTypedArray(objs, dst)
}

func (f *Finder) findResources(ctx context.Context, resType, path string, objs map[string]types.ObjectContent) error {
	p := strings.Split(path, "/")
	flt := make([]property.Match, len(p)-1)
	for i := 1; i < len(p); i++ {
		flt[i-1] = property.Match{"name": p[i]}
	}
	err := f.descend(ctx, f.client.Client.ServiceContent.RootFolder, resType, flt, 0, objs)
	if err != nil {
		return err
	}
	f.client.log.Debugf("Find(%s, %s) returned %d objects", resType, path, len(objs))
	return nil
}

func (f *Finder) descend(ctx context.Context, root types.ManagedObjectReference, resType string,
	tokens []property.Match, pos int, objs map[string]types.ObjectContent) error {
	isLeaf := pos == len(tokens)-1

	// No more tokens to match?
	if pos >= len(tokens) {
		return nil
	}

	// Determine child types

	ct, ok := childTypes[root.Reference().Type]
	if !ok {
		// We don't know how to handle children of this type. Stop descending.
		return nil
	}

	m := view.NewManager(f.client.Client.Client)
	v, err := m.CreateContainerView(ctx, root, ct, false)
	if err != nil {
		return err
	}
	defer v.Destroy(ctx) //nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway
	var content []types.ObjectContent

	fields := []string{"name"}
	recurse := tokens[pos]["name"] == "**"

	objectTypes := ct
	if isLeaf {
		if af, ok := addFields[resType]; ok {
			fields = append(fields, af...)
		}
		if recurse {
			// Special case: The last token is a recursive wildcard, so we can grab everything
			// recursively in a single call.
			v2, err := m.CreateContainerView(ctx, root, []string{resType}, true)
			if err != nil {
				return err
			}
			defer v2.Destroy(ctx) //nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway
			err = v2.Retrieve(ctx, []string{resType}, fields, &content)
			if err != nil {
				return err
			}
			for _, c := range content {
				objs[c.Obj.String()] = c
			}
			return nil
		}
		objectTypes = []string{resType} // Only load wanted object type at leaf level
	}
	err = v.Retrieve(ctx, objectTypes, fields, &content)
	if err != nil {
		return err
	}

	rerunAsLeaf := false
	for _, c := range content {
		if !matchName(tokens[pos], c.PropSet) {
			continue
		}

		// Already been here through another path? Skip!
		if _, ok := objs[root.Reference().String()]; ok {
			continue
		}

		if c.Obj.Type == resType && isLeaf {
			// We found what we're looking for. Consider it a leaf and stop descending
			objs[c.Obj.String()] = c
			continue
		}

		// Deal with recursive wildcards (**)
		var inc int
		if recurse {
			inc = 0 // By default, we stay on this token
			// Lookahead to next token.
			if matchName(tokens[pos+1], c.PropSet) {
				// Are we looking ahead at a leaf node that has the wanted type?
				// Rerun the entire level as a leaf. This is needed since all properties aren't loaded
				// when we're processing non-leaf nodes.
				if pos == len(tokens)-2 {
					if c.Obj.Type == resType {
						rerunAsLeaf = true
						continue
					}
				} else if _, ok := containers[c.Obj.Type]; ok {
					// Tokens match and we're looking ahead at a container type that's not a leaf
					// Consume this token and the next.
					inc = 2
				}
			}
		} else {
			// The normal case: Advance to next token before descending
			inc = 1
		}
		err := f.descend(ctx, c.Obj, resType, tokens, pos+inc, objs)
		if err != nil {
			return err
		}
	}

	if rerunAsLeaf {
		// We're at a "pseudo leaf", i.e. we looked ahead a token and found that this level contains leaf nodes.
		// Rerun the entire level as a leaf to get those nodes. This will only be executed when pos is one token
		// before the last, to pos+1 will always point to a leaf token.
		return f.descend(ctx, root, resType, tokens, pos+1, objs)
	}

	return nil
}

func objectContentToTypedArray(objs map[string]types.ObjectContent, dst interface{}) error {
	rt := reflect.TypeOf(dst)
	if rt == nil || rt.Kind() != reflect.Ptr {
		panic("need pointer")
	}

	rv := reflect.ValueOf(dst).Elem()
	if !rv.CanSet() {
		panic("cannot set dst")
	}
	for _, p := range objs {
		v, err := mo.ObjectContentToType(p)
		if err != nil {
			return err
		}

		vt := reflect.TypeOf(v)

		if !rv.Type().AssignableTo(vt) {
			// For example: dst is []ManagedEntity, res is []HostSystem
			if field, ok := vt.FieldByName(rt.Elem().Elem().Name()); ok && field.Anonymous {
				rv.Set(reflect.Append(rv, reflect.ValueOf(v).FieldByIndex(field.Index)))
				continue
			}
		}

		rv.Set(reflect.Append(rv, reflect.ValueOf(v)))
	}
	return nil
}

// FindAll finds all resources matching the paths that were specified upon creation of
// the ResourceFilter.
func (r *ResourceFilter) FindAll(ctx context.Context, dst interface{}) error {
	return r.finder.FindAll(ctx, r.resType, r.paths, r.excludePaths, dst)
}

func matchName(f property.Match, props []types.DynamicProperty) bool {
	for _, prop := range props {
		if prop.Name == "name" {
			return f.Property(prop)
		}
	}
	return false
}

func init() {
	childTypes = map[string][]string{
		"HostSystem":             {"VirtualMachine"},
		"ResourcePool":           {"VirtualMachine"},
		"ComputeResource":        {"HostSystem", "ResourcePool", "VirtualApp"},
		"ClusterComputeResource": {"HostSystem", "ResourcePool", "VirtualApp"},
		"Datacenter":             {"Folder"},
		"Folder": {
			"Folder",
			"Datacenter",
			"VirtualMachine",
			"ComputeResource",
			"ClusterComputeResource",
			"Datastore",
		},
	}

	addFields = map[string][]string{
		"HostSystem":   {"parent", "summary.customValue", "customValue"},
		"ResourcePool": {"parent", "customValue"},
		"VirtualMachine": {"runtime.host", "config.guestId", "config.uuid", "runtime.powerState",
			"summary.customValue", "summary.config.memorySizeMB", "guest.guestId", "guest.net", "guest.hostName",
			"resourcePool", "customValue"},
		"Datastore":              {"parent", "info", "customValue"},
		"ClusterComputeResource": {"parent", "customValue"},
		"Datacenter":             {"parent", "customValue"},
		"HostNumericSensorInfo":  {"parent", "temperature", "baseUnits"},
	}

	containers = map[string]interface{}{
		"HostSystem":      nil,
		"ComputeResource": nil,
		"Datacenter":      nil,
		"ResourcePool":    nil,
		"Folder":          nil,
		"VirtualApp":      nil,
	}
}
