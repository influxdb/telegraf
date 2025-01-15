package mavlink

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/bluenviron/gomavlib/v3"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
)

// Convert a Mavlink event into a struct containing Metric data.
func convertEventFrameToMetric(frm *gomavlib.EventFrame, msgFilter filter.Filter) telegraf.Metric {
	m := frm.Message()
	t := reflect.TypeOf(m)
	v := reflect.ValueOf(m)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		v = v.Elem()
	}

	name := internal.SnakeCase(strings.TrimPrefix(t.Name(), "Message"))

	if msgFilter != nil && !msgFilter.Match(name) {
		return nil
	}

	tags := map[string]string{
		"sys_id": strconv.FormatUint(uint64(frm.SystemID()), 10),
	}
	fields := make(map[string]interface{}, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)
		fields[internal.SnakeCase(field.Name)] = value.Interface()
	}

	return metric.New(name, tags, fields, time.Now())
}
