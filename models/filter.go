package models

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
)

// TagFilter is the name of a tag, and the values on which to filter
type TagFilter struct {
	Name   string
	Values []string
	filter filter.Filter
}

func (tf *TagFilter) Compile() error {
	f, err := filter.Compile(tf.Values)
	if err != nil {
		return err
	}
	tf.filter = f
	return nil
}

// Filter containing drop/pass and include/exclude rules
type Filter struct {
	NameDrop           []string
	NameDropSeparators string
	nameDropFilter     filter.Filter
	NamePass           []string
	NamePassSeparators string
	namePassFilter     filter.Filter

	FieldExclude       []string
	fieldExcludeFilter filter.Filter
	FieldInclude       []string
	fieldIncludeFilter filter.Filter

	TagDropFilters []TagFilter
	TagPassFilters []TagFilter

	TagExclude       []string
	tagExcludeFilter filter.Filter
	TagInclude       []string
	tagIncludeFilter filter.Filter

	// New metric-filtering interface
	MetricPass   string
	metricFilter cel.Program

	selectActive bool
	modifyActive bool

	isActive bool
}

// Compile all Filter lists into filter.Filter objects.
func (f *Filter) Compile() error {
	f.selectActive = len(f.NamePass) > 0 || len(f.NameDrop) > 0
	f.selectActive = f.selectActive || len(f.TagPassFilters) > 0 || len(f.TagDropFilters) > 0
	f.selectActive = f.selectActive || f.MetricPass != ""

	f.modifyActive = len(f.FieldInclude) > 0 || len(f.FieldExclude) > 0
	f.modifyActive = f.modifyActive || len(f.TagInclude) > 0 || len(f.TagExclude) > 0

	f.isActive = f.selectActive || f.modifyActive

	if !f.isActive {
		return nil
	}

	if f.selectActive {
		var err error
		f.nameDropFilter, err = filter.Compile(f.NameDrop, []rune(f.NameDropSeparators)...)
		if err != nil {
			return fmt.Errorf("error compiling 'namedrop', %w", err)
		}
		f.namePassFilter, err = filter.Compile(f.NamePass, []rune(f.NamePassSeparators)...)
		if err != nil {
			return fmt.Errorf("error compiling 'namepass', %w", err)
		}

		for i := range f.TagPassFilters {
			if err := f.TagPassFilters[i].Compile(); err != nil {
				return fmt.Errorf("error compiling 'tagpass', %w", err)
			}
		}
		for i := range f.TagDropFilters {
			if err := f.TagDropFilters[i].Compile(); err != nil {
				return fmt.Errorf("error compiling 'tagdrop', %w", err)
			}
		}
	}

	if f.modifyActive {
		var err error
		f.fieldExcludeFilter, err = filter.Compile(f.FieldExclude)
		if err != nil {
			return fmt.Errorf("error compiling 'fieldexclude', %w", err)
		}
		f.fieldIncludeFilter, err = filter.Compile(f.FieldInclude)
		if err != nil {
			return fmt.Errorf("error compiling 'fieldinclude', %w", err)
		}

		f.tagExcludeFilter, err = filter.Compile(f.TagExclude)
		if err != nil {
			return fmt.Errorf("error compiling 'tagexclude', %w", err)
		}
		f.tagIncludeFilter, err = filter.Compile(f.TagInclude)
		if err != nil {
			return fmt.Errorf("error compiling 'taginclude', %w", err)
		}
	}

	return f.compileMetricFilter()
}

// Select returns true if the metric matches according to the
// namepass/namedrop, tagpass/tagdrop and metric filters.
// The metric is not modified.
func (f *Filter) Select(metric telegraf.Metric) (bool, error) {
	if !f.selectActive {
		return true, nil
	}

	if !f.shouldNamePass(metric.Name()) {
		return false, nil
	}

	if !f.shouldTagsPass(metric.TagList()) {
		return false, nil
	}

	if f.metricFilter != nil {
		result, _, err := f.metricFilter.Eval(map[string]interface{}{
			"name":   metric.Name(),
			"tags":   metric.Tags(),
			"fields": metric.Fields(),
			"time":   metric.Time(),
		})
		if err != nil {
			return true, err
		}
		if r, ok := result.Value().(bool); ok {
			return r, nil
		}
		return true, fmt.Errorf("invalid result type %T", result.Value())
	}

	return true, nil
}

// Modify removes any tags and fields from the metric according to the
// fieldinclude/fieldexclude and taginclude/tagexclude filters.
func (f *Filter) Modify(metric telegraf.Metric) {
	if !f.modifyActive {
		return
	}

	f.filterFields(metric)
	f.filterTags(metric)
}

// IsActive checking if filter is active
func (f *Filter) IsActive() bool {
	return f.isActive
}

// shouldNamePass returns true if the metric should pass, false if it should drop
// based on the drop/pass filter parameters
func (f *Filter) shouldNamePass(key string) bool {
	pass := func(f *Filter) bool {
		return f.namePassFilter.Match(key)
	}

	drop := func(f *Filter) bool {
		return !f.nameDropFilter.Match(key)
	}

	if f.namePassFilter != nil && f.nameDropFilter != nil {
		return pass(f) && drop(f)
	} else if f.namePassFilter != nil {
		return pass(f)
	} else if f.nameDropFilter != nil {
		return drop(f)
	}

	return true
}

// shouldTagsPass returns true if the metric should pass, false if it should drop
// based on the tagdrop/tagpass filter parameters
func (f *Filter) shouldTagsPass(tags []*telegraf.Tag) bool {
	return ShouldTagsPass(f.TagPassFilters, f.TagDropFilters, tags)
}

// filterFields removes fields according to fieldinclude/fieldexclude.
func (f *Filter) filterFields(metric telegraf.Metric) {
	filterKeys := make([]string, 0, len(metric.FieldList()))
	for _, field := range metric.FieldList() {
		if !ShouldPassFilters(f.fieldIncludeFilter, f.fieldExcludeFilter, field.Key) {
			filterKeys = append(filterKeys, field.Key)
		}
	}

	for _, key := range filterKeys {
		metric.RemoveField(key)
	}
}

// filterTags removes tags according to taginclude/tagexclude.
func (f *Filter) filterTags(metric telegraf.Metric) {
	filterKeys := make([]string, 0, len(metric.TagList()))
	for _, tag := range metric.TagList() {
		if !ShouldPassFilters(f.tagIncludeFilter, f.tagExcludeFilter, tag.Key) {
			filterKeys = append(filterKeys, tag.Key)
		}
	}

	for _, key := range filterKeys {
		metric.RemoveTag(key)
	}
}

// Compile the metric filter
func (f *Filter) compileMetricFilter() error {
	// Reset internal state
	f.metricFilter = nil

	// Initialize the expression
	expression := f.MetricPass

	// Check if we need to call into CEL at all and quit early
	if expression == "" {
		return nil
	}

	// Declare the computation environment for the filter including custom functions
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar("name", decls.String),
			decls.NewVar("tags", decls.NewMapType(decls.String, decls.String)),
			decls.NewVar("fields", decls.NewMapType(decls.String, decls.Dyn)),
			decls.NewVar("time", decls.Timestamp),
		),
		cel.Function(
			"now",
			cel.Overload("now", nil, cel.TimestampType),
			cel.SingletonFunctionBinding(func(_ ...ref.Val) ref.Val { return types.Timestamp{Time: time.Now()} }),
		),
		ext.Encoders(),
		ext.Math(),
		ext.Strings(),
	)
	if err != nil {
		return fmt.Errorf("creating environment failed: %w", err)
	}

	// Compile the program
	ast, issues := env.Compile(expression)
	if issues.Err() != nil {
		return issues.Err()
	}
	// Check if we got a boolean expression needed for filtering
	if ast.OutputType() != cel.BoolType {
		return errors.New("expression needs to return a boolean")
	}

	// Get the final program
	options := cel.EvalOptions(
		cel.OptOptimize,
	)
	f.metricFilter, err = env.Program(ast, options)
	return err
}

func ShouldPassFilters(include, exclude filter.Filter, key string) bool {
	if include != nil && exclude != nil {
		return include.Match(key) && !exclude.Match(key)
	} else if include != nil {
		return include.Match(key)
	} else if exclude != nil {
		return !exclude.Match(key)
	}
	return true
}

func ShouldTagsPass(passFilters, dropFilters []TagFilter, tags []*telegraf.Tag) bool {
	pass := func(tpf []TagFilter) bool {
		for _, pat := range tpf {
			if pat.filter == nil {
				continue
			}
			for _, tag := range tags {
				if tag.Key == pat.Name {
					if pat.filter.Match(tag.Value) {
						return true
					}
				}
			}
		}
		return false
	}

	drop := func(tdf []TagFilter) bool {
		for _, pat := range tdf {
			if pat.filter == nil {
				continue
			}
			for _, tag := range tags {
				if tag.Key == pat.Name {
					if pat.filter.Match(tag.Value) {
						return false
					}
				}
			}
		}
		return true
	}

	// Add additional logic in case where both parameters are set.
	// see: https://github.com/influxdata/telegraf/issues/2860
	if passFilters != nil && dropFilters != nil {
		// return true only in case when tag pass and won't be dropped (true, true).
		// in case when the same tag should be passed and dropped it will be dropped (true, false).
		return pass(passFilters) && drop(dropFilters)
	} else if passFilters != nil {
		return pass(passFilters)
	} else if dropFilters != nil {
		return drop(dropFilters)
	}

	return true
}
