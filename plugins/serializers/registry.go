package serializers

import "github.com/influxdata/telegraf"

// Creator is the function to create a new serializer
type Creator func() telegraf.Serializer

// Serializers contains the registry of all known serializers (following the new style)
var Serializers = make(map[string]Creator)

// Add adds a serializer to the registry. Usually this function is called in the plugin's init function
func Add(name string, creator Creator) {
	Serializers[name] = creator
}
