package filter

import (
	"strings"

	"github.com/gobwas/glob"
)

type Filter interface {
	Match(string) bool
}

// Compile takes a list of string filters and returns a Filter interface
// for matching a given string against the filter list. The filter list
// supports glob matching with separators too, ie:
//
//	f, _ := Compile([]string{"cpu", "mem", "net*"})
//	f.Match("cpu")     // true
//	f.Match("network") // true
//	f.Match("memory")  // false
//
// separators are only to be used for globbing filters, ie:
//
//	f, _ := Compile([]string{"cpu.*.count"}, '.')
//	f.Match("cpu.count")     // false
//	f.Match("cpu.measurement.count") // true
//	f.Match("cpu.field.measurement.count")  // false
//
// Compile will return nil if the filter list is empty.
func Compile(filters []string, separators ...rune) (Filter, error) {
	// return if there is nothing to compile
	if len(filters) == 0 {
		return nil, nil
	}

	// check if we can compile a non-glob filter
	noGlob := len(separators) == 0
	for _, filter := range filters {
		if hasMeta(filter) {
			noGlob = false
			break
		}
	}

	switch {
	case noGlob:
		// return non-globbing filter if not needed.
		return compileFilterNoGlob(filters), nil
	case len(filters) == 1:
		return glob.Compile(filters[0], separators...)
	default:
		return glob.Compile("{"+strings.Join(filters, ",")+"}", separators...)
	}
}

func MustCompile(filters []string, separators ...rune) Filter {
	f, err := Compile(filters, separators...)
	if err != nil {
		panic(err)
	}
	return f
}

// hasMeta reports whether path contains any magic glob characters.
func hasMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

type filter struct {
	m map[string]struct{}
}

func (f *filter) Match(s string) bool {
	_, ok := f.m[s]
	return ok
}

type filtersingle struct {
	s string
}

func (f *filtersingle) Match(s string) bool {
	return f.s == s
}

func compileFilterNoGlob(filters []string) Filter {
	if len(filters) == 1 {
		return &filtersingle{s: filters[0]}
	}
	out := filter{m: make(map[string]struct{})}
	for _, filter := range filters {
		out.m[filter] = struct{}{}
	}
	return &out
}

type IncludeExcludeFilter struct {
	include        Filter
	exclude        Filter
	includeDefault bool
	excludeDefault bool
}

func NewIncludeExcludeFilter(include, exclude []string) (Filter, error) {
	return NewIncludeExcludeFilterDefaults(include, exclude, true, false)
}

func NewIncludeExcludeFilterDefaults(include, exclude []string, includeDefault, excludeDefault bool) (Filter, error) {
	in, err := Compile(include)
	if err != nil {
		return nil, err
	}

	ex, err := Compile(exclude)
	if err != nil {
		return nil, err
	}

	return &IncludeExcludeFilter{in, ex, includeDefault, excludeDefault}, nil
}

func (f *IncludeExcludeFilter) Match(s string) bool {
	if f.include != nil {
		if !f.include.Match(s) {
			return false
		}
	} else if !f.includeDefault {
		return false
	}

	if f.exclude != nil {
		if f.exclude.Match(s) {
			return false
		}
	} else if f.excludeDefault {
		return false
	}

	return true
}
