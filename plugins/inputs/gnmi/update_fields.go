package gnmi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/value"
)

type keyValuePair struct {
	key   []string
	value interface{}
}

type updateField struct {
	path  *pathInfo
	value interface{}
}

func (h *handler) newFieldsFromUpdate(path *pathInfo, update *gnmi.Update) ([]updateField, error) {
	if update.Val == nil || update.Val.Value == nil {
		return []updateField{{path: path}}, nil
	}

	// Apply some special handling for special types
	switch v := update.Val.Value.(type) {
	case *gnmi.TypedValue_AsciiVal: // not handled in ToScalar
		return []updateField{{path, v.AsciiVal}}, nil
	case *gnmi.TypedValue_JsonVal: // requires special path handling
		return h.processJSON(path, v.JsonVal)
	case *gnmi.TypedValue_JsonIetfVal: // requires special path handling
		return h.processJSONIETF(path, v.JsonIetfVal)
	}

	// Convert the protobuf "oneof" data to a Golang type.
	nativeType, err := value.ToScalar(update.Val)
	if err != nil {
		return nil, err
	}
	return []updateField{{path, nativeType}}, nil
}

func (h *handler) processJSON(path *pathInfo, data []byte) ([]updateField, error) {
	var nested interface{}
	if err := json.Unmarshal(data, &nested); err != nil {
		return nil, fmt.Errorf("failed to parse JSON value: %w", err)
	}

	// Flatten the JSON data to get a key-value map
	entries := flatten(nested)

	// Create an update-field with the complete path for all entries
	fields := make([]updateField, 0, len(entries))
	for _, entry := range entries {
		p := path.appendSegments(entry.key...)
		if h.enforceFirstNamespaceAsOrigin {
			p.enforceFirstNamespaceAsOrigin()
		}

		fields = append(fields, updateField{
			path:  p,
			value: entry.value,
		})
	}

	return fields, nil
}

func (h *handler) processJSONIETF(path *pathInfo, data []byte) ([]updateField, error) {
	var nested interface{}
	if err := json.Unmarshal(data, &nested); err != nil {
		return nil, fmt.Errorf("failed to parse JSON value: %w", err)
	}

	// Flatten the JSON data to get a key-value map
	entries := flatten(nested)

	// Lookup the data in the YANG model if any
	if h.decoder != nil {
		for i, e := range entries {
			var namespace, identifier string
			for _, k := range e.key {
				if n, _, found := strings.Cut(k, ":"); found {
					namespace = n
				}
			}

			// IETF nodes referencing YANG entries require a namespace
			if namespace == "" {
				continue
			}

			if a, b, found := strings.Cut(e.key[len(e.key)-1], ":"); !found {
				identifier = a
			} else {
				identifier = b
			}

			if decoded, err := h.decoder.DecodeLeafElement(namespace, identifier, e.value); err != nil {
				h.log.Debugf("Decoding %s:%s failed: %v", namespace, identifier, err)
			} else {
				entries[i].value = decoded
			}
		}
	}

	fields := make([]updateField, 0, len(entries))
	for _, entry := range entries {
		p := path.appendSegments(entry.key...)
		if h.enforceFirstNamespaceAsOrigin {
			p.enforceFirstNamespaceAsOrigin()
		}

		// Try to lookup the full path to decode the field according to the
		// YANG model if any
		if h.decoder != nil {
			origin, fieldPath := p.path()
			if decoded, err := h.decoder.DecodePathElement(origin, fieldPath, entry.value); err != nil {
				h.log.Debugf("Decoding %s failed: %v", p, err)
			} else {
				entry.value = decoded
			}
		}

		// Create an update-field with the complete path for all entries
		fields = append(fields, updateField{
			path:  p,
			value: entry.value,
		})
	}

	return fields, nil
}

func flatten(nested interface{}) []keyValuePair {
	var values []keyValuePair

	switch n := nested.(type) {
	case map[string]interface{}:
		for k, child := range n {
			for _, c := range flatten(child) {
				values = append(values, keyValuePair{
					key:   append([]string{k}, c.key...),
					value: c.value,
				})
			}
		}
	case []interface{}:
		for i, child := range n {
			k := strconv.Itoa(i)
			for _, c := range flatten(child) {
				values = append(values, keyValuePair{
					key:   append([]string{k}, c.key...),
					value: c.value,
				})
			}
		}
	default:
		values = append(values, keyValuePair{value: n})
	}

	return values
}
