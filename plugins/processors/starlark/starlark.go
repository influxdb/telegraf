//go:generate ../../../tools/readme_config_includer/generator
package starlark

import (
	_ "embed"
	"errors"
	"fmt"

	"go.starlark.net/starlark"

	"github.com/influxdata/telegraf"
	common "github.com/influxdata/telegraf/plugins/common/starlark"
	"github.com/influxdata/telegraf/plugins/processors"
)

//go:embed sample.conf
var sampleConfig string

type Starlark struct {
	common.Common

	results []telegraf.Metric
}

func (*Starlark) SampleConfig() string {
	return sampleConfig
}

func (s *Starlark) Init() error {
	if err := s.Common.Init(); err != nil {
		return err
	}

	// The source should define an apply function.
	if err := s.AddFunction("apply", &common.Metric{}); err != nil {
		return err
	}

	// Preallocate a slice for return values.
	s.results = make([]telegraf.Metric, 0, 10)

	return nil
}

func (s *Starlark) Start(_ telegraf.Accumulator) error {
	return nil
}

func (s *Starlark) Add(origMetric telegraf.Metric, acc telegraf.Accumulator) error {
	parameters, found := s.GetParameters("apply")
	if !found {
		return errors.New("the parameters of the apply function could not be found")
	}
	parameters[0].(*common.Metric).Wrap(origMetric)

	returnValue, err := s.Call("apply")
	if err != nil {
		s.LogError(err)
		return err
	}

	switch rv := returnValue.(type) {
	case *starlark.List:
		iter := rv.Iterate()
		defer iter.Done()
		var v starlark.Value
		var origFound bool
		for iter.Next(&v) {
			switch v := v.(type) {
			case *common.Metric:
				m := v.Unwrap()
				if containsMetric(s.results, m) {
					s.Log.Errorf("Duplicate metric reference detected")
					continue
				}

				// Previous metric was found, accept the starlark metric, add
				// the original metric to the accumulator
				if v.ID != 0 {
					origFound = true
					s.results = append(s.results, origMetric)
					acc.AddMetric(origMetric)
					continue
				}

				s.results = append(s.results, m)
				acc.AddMetric(m)
			default:
				s.Log.Errorf("Invalid type returned in list: %s", v.Type())
			}
		}

		// If the script didn't return the original metrics, mark it as
		// successfully handled.
		if !origFound {
			origMetric.Drop()
		}

		// clear results
		for i := range s.results {
			s.results[i] = nil
		}
		s.results = s.results[:0]
	case *common.Metric:
		m := rv.Unwrap()
		// If we got the original metric back, use that and drop the new one.
		// Otherwise mark the original as accepted and use the new metric.
		if rv.ID != 0 {
			acc.AddMetric(origMetric)
		} else {
			origMetric.Accept()
			acc.AddMetric(m)
		}
	case starlark.NoneType:
		origMetric.Drop()
	default:
		return fmt.Errorf("invalid type returned: %T", rv)
	}

	return nil
}

func (s *Starlark) Stop() {}

func containsMetric(metrics []telegraf.Metric, target telegraf.Metric) bool {
	for _, m := range metrics {
		if m == target {
			return true
		}
	}
	return false
}

func init() {
	processors.AddStreaming("starlark", func() telegraf.StreamingProcessor {
		return &Starlark{
			Common: common.Common{
				StarlarkLoadFunc: common.LoadFunc,
			},
		}
	})
}
