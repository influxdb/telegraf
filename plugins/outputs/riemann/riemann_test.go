package riemann

import (
	"fmt"
	"testing"
	"time"

	"github.com/amir/raidman"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestAttributes(t *testing.T) {
	tags := map[string]string{"tag1": "value1", "tag2": "value2"}

	r := &Riemann{}
	require.Equal(t,
		map[string]string{"tag1": "value1", "tag2": "value2"},
		r.attributes("test", tags))

	// enable measurement as attribute, should now be included
	r.MeasurementAsAttribute = true
	require.Equal(t,
		map[string]string{"tag1": "value1", "tag2": "value2", "measurement": "test"},
		r.attributes("test", tags))
}

func TestService(t *testing.T) {
	r := &Riemann{
		Separator: "/",
	}
	require.Equal(t, "test/value", r.service("test", "value"))

	// enable measurement as attribute, should not be part of service name anymore
	r.MeasurementAsAttribute = true
	require.Equal(t, "value", r.service("test", "value"))
}

func TestTags(t *testing.T) {
	tags := map[string]string{"tag1": "value1", "tag2": "value2"}

	// all tag values plus additional tag should be present
	r := &Riemann{
		Tags: []string{"test"},
	}
	require.Equal(t,
		[]string{"test", "value1", "value2"},
		r.tags(tags))

	// only tag2 value plus additional tag should be present
	r.TagKeys = []string{"tag2"}
	require.Equal(t,
		[]string{"test", "value2"},
		r.tags(tags))

	// only tag1 value should be present
	r.Tags = nil
	r.TagKeys = []string{"tag1"}
	require.Equal(t,
		[]string{"value1"},
		r.tags(tags))
}

func TestMetricEvents(t *testing.T) {
	r := &Riemann{
		TTL:                    20.0,
		Separator:              "/",
		MeasurementAsAttribute: false,
		DescriptionText:        "metrics from telegraf",
		Tags:                   []string{"telegraf"},
	}

	// build a single event
	m, _ := metric.New(
		"test1",
		map[string]string{"tag1": "value1", "host": "abc123"},
		map[string]interface{}{"value": 5.6},
		time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC),
	)

	events := r.buildRiemannEvents(m)
	require.Len(t, events, 1)

	// is event as expected?
	expectedEvent := &raidman.Event{
		Ttl:         20.0,
		Time:        1257894000,
		Tags:        []string{"telegraf", "value1"},
		Host:        "abc123",
		State:       "",
		Service:     "test1/value",
		Metric:      5.6,
		Description: "metrics from telegraf",
		Attributes:  map[string]string{"tag1": "value1"},
	}
	require.Equal(t, expectedEvent, events[0])

	// build 2 events
	m, _ = metric.New(
		"test2",
		map[string]string{"host": "xyz987"},
		map[string]interface{}{"point": 1},
		time.Date(2012, time.November, 2, 3, 0, 0, 0, time.UTC),
	)

	events = append(events, r.buildRiemannEvents(m)...)
	require.Len(t, events, 2)

	// first event should still be the same
	require.Equal(t, expectedEvent, events[0])

	// second event
	expectedEvent = &raidman.Event{
		Ttl:         20.0,
		Time:        1351825200,
		Tags:        []string{"telegraf"},
		Host:        "xyz987",
		State:       "",
		Service:     "test2/point",
		Metric:      int64(1),
		Description: "metrics from telegraf",
		Attributes:  map[string]string{},
	}
	require.Equal(t, expectedEvent, events[1])
}

func TestStateEvents(t *testing.T) {
	r := &Riemann{
		MeasurementAsAttribute: true,
	}

	// string metrics will be skipped unless explicitly enabled
	m, _ := metric.New(
		"test",
		map[string]string{"host": "host"},
		map[string]interface{}{"value": "running"},
		time.Date(2015, time.November, 9, 22, 0, 0, 0, time.UTC),
	)

	events := r.buildRiemannEvents(m)
	// no event should be present
	require.Len(t, events, 0)

	// enable string metrics as event states
	r.StringAsState = true
	events = r.buildRiemannEvents(m)
	require.Len(t, events, 1)

	// is event as expected?
	expectedEvent := &raidman.Event{
		Ttl:         0,
		Time:        1447106400,
		Tags:        nil,
		Host:        "host",
		State:       "running",
		Service:     "value",
		Metric:      nil,
		Description: "",
		Attributes:  map[string]string{"measurement": "test"},
	}
	require.Equal(t, expectedEvent, events[0])
}

func TestConnectAndWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	r := &Riemann{
		URL:                    fmt.Sprintf("tcp://%s:5555", testutil.GetLocalHost()),
		TTL:                    15.0,
		Separator:              "/",
		MeasurementAsAttribute: false,
		StringAsState:          true,
		DescriptionText:        "metrics from telegraf",
		Tags:                   []string{"docker"},
	}

	err := r.Connect()
	require.NoError(t, err)

	err = r.Write(testutil.MockMetrics())
	require.NoError(t, err)

	metrics := make([]telegraf.Metric, 0)
	metrics = append(metrics, testutil.TestMetric(2))
	metrics = append(metrics, testutil.TestMetric(3.456789))
	metrics = append(metrics, testutil.TestMetric(uint(0)))
	metrics = append(metrics, testutil.TestMetric("ok"))
	metrics = append(metrics, testutil.TestMetric("running"))
	err = r.Write(metrics)
	require.NoError(t, err)

<<<<<<< HEAD
	start := time.Now()
	for true {
		events, _ := r.client.Query(`tagged "docker"`)
		if len(events) > 0 {
			break
		}
		if time.Since(start) > time.Second {
			break
		}
	}

=======
>>>>>>> 613de8a80dbb12a2211a878b777771fc0af143bc
	// are there any "docker" tagged events in Riemann?
	events, err := r.client.Query(`tagged "docker"`)
	require.NoError(t, err)
	require.NotZero(t, len(events))

	// get Riemann events with state = "running", should be 1 event
	events, err = r.client.Query(`state = "running"`)
	require.NoError(t, err)
	require.Len(t, events, 1)

	// is event as expected?
	require.Equal(t, []string{"docker", "value1"}, events[0].Tags)
	require.Equal(t, "running", events[0].State)
	require.Equal(t, "test1/value", events[0].Service)
	require.Equal(t, "metrics from telegraf", events[0].Description)
	require.Equal(t, map[string]string{"tag1": "value1"}, events[0].Attributes)
}
