package metric

import (
	"encoding/binary"
	"hash/maphash"
	"sort"
	"time"

	"github.com/influxdata/telegraf"
)

// NewSeriesGrouper returns a type that can be used to group fields by series
// and time, so that fields which share these values will be combined into a
// single telegraf.Metric.
//
// This is useful to build telegraf.Metric's when all fields for a series are
// not available at once.
//
// ex:
// - cpu,host=localhost usage_time=42
// - cpu,host=localhost idle_time=42
// + cpu,host=localhost idle_time=42,usage_time=42
func NewSeriesGrouper() *SeriesGrouper {
	return &SeriesGrouper{
		metrics:  make(map[uint64]telegraf.Metric),
		ordered:  make([]telegraf.Metric, 0),
		hashSeed: maphash.MakeSeed(),
	}
}

type SeriesGrouper struct {
	metrics map[uint64]telegraf.Metric
	ordered []telegraf.Metric

	hashSeed maphash.Seed
}

// Add adds a field key and value to the series.
func (g *SeriesGrouper) Add(
	measurement string,
	tags map[string]string,
	tm time.Time,
	field string,
	fieldValue interface{},
) {
	taglist := make([]*telegraf.Tag, 0, len(tags))
	for k, v := range tags {
		taglist = append(taglist,
			&telegraf.Tag{Key: k, Value: v})
	}
	sort.Slice(taglist, func(i, j int) bool { return taglist[i].Key < taglist[j].Key })

	id := groupID(g.hashSeed, measurement, taglist, tm)
	m := g.metrics[id]
	if m == nil {
		m = New(measurement, tags, map[string]interface{}{field: fieldValue}, tm)
		g.metrics[id] = m
		g.ordered = append(g.ordered, m)
	} else {
		m.AddField(field, fieldValue)
	}
}

// AddMetric adds a metric to the series, merging with any previous matching metrics.
func (g *SeriesGrouper) AddMetric(
	metric telegraf.Metric,
) {
	id := groupID(g.hashSeed, metric.Name(), metric.TagList(), metric.Time())
	m := g.metrics[id]
	if m == nil {
		m = metric.Copy()
		g.metrics[id] = m
		g.ordered = append(g.ordered, m)
	} else {
		for _, f := range metric.FieldList() {
			m.AddField(f.Key, f.Value)
		}
	}
}

// Metrics returns the metrics grouped by series and time.
func (g *SeriesGrouper) Metrics() []telegraf.Metric {
	return g.ordered
}

func groupID(seed maphash.Seed, measurement string, taglist []*telegraf.Tag, tm time.Time) uint64 {
	var mh maphash.Hash
	mh.SetSeed(seed)

	mh.WriteString(measurement)
	mh.WriteByte(0)

	for _, tag := range taglist {
		mh.WriteString(tag.Key)
		mh.WriteByte(0)
		mh.WriteString(tag.Value)
		mh.WriteByte(0)
	}
	mh.WriteByte(0)

	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(tm.UnixNano()))
	mh.Write(tsBuf[:])

	return mh.Sum64()
}
