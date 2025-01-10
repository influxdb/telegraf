package testutil

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/serializers/influx"
)

var localhost = "localhost"

const (
	DefaultDelta   = 0.001
	DefaultEpsilon = 0.1
)

// GetLocalHost returns the DOCKER_HOST environment variable, parsing
// out any scheme or ports so that only the IP address is returned.
func GetLocalHost() string {
	if dockerHostVar := os.Getenv("DOCKER_HOST"); dockerHostVar != "" {
		u, err := url.Parse(dockerHostVar)
		if err != nil {
			return dockerHostVar
		}

		// split out the ip addr from the port
		host, _, err := net.SplitHostPort(u.Host)
		if err != nil {
			return dockerHostVar
		}

		return host
	}
	return localhost
}

// MockMetrics returns a mock []telegraf.Metric object for using in unit tests
// of telegraf output sinks.
func MockMetrics() []telegraf.Metric {
	metrics := make([]telegraf.Metric, 0)
	// Create a new point batch
	metrics = append(metrics, TestMetric(1.0))
	return metrics
}

func MockMetricsWithValue(value float64) []telegraf.Metric {
	metrics := make([]telegraf.Metric, 0)
	// Create a new point batch
	metrics = append(metrics, TestMetric(value))
	return metrics
}

// TestMetric Returns a simple test point:
//
//	measurement -> "test1" or name
//	tags -> "tag1":"value1"
//	value -> value
//	time -> time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
func TestMetric(value interface{}, name ...string) telegraf.Metric {
	if value == nil {
		panic("Cannot use a nil value")
	}
	measurement := "test1"
	if len(name) > 0 {
		measurement = name[0]
	}
	tags := map[string]string{"tag1": "value1"}
	pt := metric.New(
		measurement,
		tags,
		map[string]interface{}{"value": value},
		time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC),
	)
	return pt
}

// OnlyTags returns an option for keeping only "Tags" for a given Metric
func OnlyTags() cmp.Option {
	f := func(p cmp.Path) bool { return p.String() != "Tags" && p.String() != "" }
	return cmp.FilterPath(f, cmp.Ignore())
}

func PrintMetrics(m []telegraf.Metric) {
	s := &influx.Serializer{
		SortFields:  true,
		UintSupport: true,
	}
	if err := s.Init(); err != nil {
		panic(err)
	}
	buf, err := s.SerializeBatch(m)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(buf))
}

// DefaultSampleConfig returns the sample config with the default parameters
// uncommented to also be able to test the validity of default setting.
func DefaultSampleConfig(sampleConfig string) []byte {
	re := regexp.MustCompile(`(?m)(^\s+)#\s*`)
	return []byte(re.ReplaceAllString(sampleConfig, "$1"))
}

func WithinDefaultDelta(dt float64) bool {
	if dt < -DefaultDelta || dt > DefaultDelta {
		return false
	}
	return true
}
