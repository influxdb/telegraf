package gnmi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/influxdata/telegraf/internal"
)

type tagStore struct {
	unconditional map[string]string
	names         map[string]map[string]string
	elements      elementsStore
}

type elementsStore struct {
	required [][]string
	tags     map[string]map[string]string
}

func newTagStore(subs []tagSubscription) *tagStore {
	store := tagStore{
		unconditional: make(map[string]string),
		names:         make(map[string]map[string]string),
		elements: elementsStore{
			required: make([][]string, 0, len(subs)),
			tags:     make(map[string]map[string]string),
		},
	}
	for _, s := range subs {
		if s.Match == "elements" {
			store.elements.required = append(store.elements.required, s.Elements)
		}
	}

	return &store
}

// Store tags extracted from TagSubscriptions
func (s *tagStore) insert(subscription tagSubscription, path *pathInfo, values []updateField, tags map[string]string) error {
	switch subscription.Match {
	case "unconditional":
		for _, f := range values {
			tagName := subscription.Name
			if len(f.path.segments) > 0 {
				key := f.path.base()
				key = strings.ReplaceAll(key, "-", "_")
				tagName += "/" + key
			}
			sv, err := internal.ToString(f.value)
			if err != nil {
				return fmt.Errorf("conversion error for %v: %w", f.value, err)
			}
			if sv == "" {
				delete(s.unconditional, tagName)
			} else {
				s.unconditional[tagName] = sv
			}
		}
	case "name":
		// Get the lookup key
		key, found := tags["name"]
		if !found {
			return nil
		}

		// Make sure we have a valid map for the key
		if _, exists := s.names[key]; !exists {
			s.names[key] = make(map[string]string)
		}

		// Add the values
		for _, f := range values {
			tagName := subscription.Name
			if len(f.path.segments) > 0 {
				key := f.path.base()
				key = strings.ReplaceAll(key, "-", "_")
				tagName += "/" + key
			}
			sv, err := internal.ToString(f.value)
			if err != nil {
				return fmt.Errorf("conversion error for %v: %w", f.value, err)
			}
			if sv == "" {
				delete(s.names[key], tagName)
			} else {
				s.names[key][tagName] = sv
			}
		}
	case "elements":
		key, match := getElementsKeys(path, subscription.Elements)
		if !match || len(values) == 0 {
			return nil
		}

		// Make sure we have a valid map for the key
		if _, exists := s.elements.tags[key]; !exists {
			s.elements.tags[key] = make(map[string]string)
		}

		// Add the values
		for _, f := range values {
			tagName := subscription.Name
			if len(f.path.segments) > 0 {
				key := f.path.base()
				key = strings.ReplaceAll(key, "-", "_")
				tagName += "/" + key
			}
			sv, err := internal.ToString(f.value)
			if err != nil {
				return fmt.Errorf("conversion error for %v: %w", f.value, err)
			}
			if sv == "" {
				delete(s.elements.tags[key], tagName)
			} else {
				s.elements.tags[key][tagName] = sv
			}
		}
	default:
		return fmt.Errorf("unknown match strategy %q", subscription.Match)
	}

	return nil
}

func (s *tagStore) lookup(path *pathInfo, metricTags map[string]string) map[string]string {
	// Add all unconditional tags
	tags := make(map[string]string, len(s.unconditional))
	for k, v := range s.unconditional {
		tags[k] = v
	}

	// Match names
	key, found := metricTags["name"]
	if found {
		for k, v := range s.names[key] {
			tags[k] = v
		}
	}

	// Match elements
	for _, requiredKeys := range s.elements.required {
		key, match := getElementsKeys(path, requiredKeys)
		if !match {
			continue
		}
		for k, v := range s.elements.tags[key] {
			tags[k] = v
		}
	}

	return tags
}

func getElementsKeys(path *pathInfo, elements []string) (string, bool) {
	// Search for the required path elements and collect a ordered
	// list of their values to in the form
	//    elementName1={keyA=valueA,keyB=valueB,...},...,elementNameN={keyY=valueY,keyZ=valueZ}
	// where each elements' key-value list is enclosed in curly brackets.
	keyParts := make([]string, 0, len(elements))
	for _, requiredElement := range elements {
		var found bool
		var elementKVs []string
		for _, segment := range path.keyValues {
			if segment.name == requiredElement {
				for k, v := range segment.kv {
					elementKVs = append(elementKVs, k+"="+v)
				}
				found = true
				break
			}
		}

		// The element was not found, but all must match
		if !found {
			return "", false
		}

		// We need to order the element's key-value pairs as the map
		// returns elements in random order
		sort.Strings(elementKVs)

		// Collect the element
		keyParts = append(keyParts, requiredElement+"={"+strings.Join(elementKVs, ",")+"}")
	}
	return strings.Join(keyParts, ","), true
}
