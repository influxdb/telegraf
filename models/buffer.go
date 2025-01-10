package models

import (
	"fmt"
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/selfstat"
)

var (
	AgentMetricsWritten  = selfstat.Register("agent", "metrics_written", make(map[string]string))
	AgentMetricsRejected = selfstat.Register("agent", "metrics_rejected", make(map[string]string))
	AgentMetricsDropped  = selfstat.Register("agent", "metrics_dropped", make(map[string]string))

	registerGob = sync.OnceFunc(func() { metric.Init() })
)

type Transaction struct {
	// Batch of metrics to write
	Batch []telegraf.Metric

	// Accept denotes the indices of metrics that were successfully written
	Accept []int
	// Reject denotes the indices of metrics that were not written but should
	// not be requeued
	Reject []int

	// Marks this transaction as valid
	valid bool

	// Internal state that can be used by the buffer implementation
	state interface{}
}

func (tx *Transaction) AcceptAll() {
	tx.Accept = make([]int, len(tx.Batch))
	for i := range tx.Batch {
		tx.Accept[i] = i
	}
}

func (tx *Transaction) KeepAll() {}

func (tx *Transaction) InferKeep() []int {
	used := make([]bool, len(tx.Batch))
	for _, idx := range tx.Accept {
		used[idx] = true
	}
	for _, idx := range tx.Reject {
		used[idx] = true
	}

	keep := make([]int, 0, len(tx.Batch))
	for i := range tx.Batch {
		if !used[i] {
			keep = append(keep, i)
		}
	}
	return keep
}

type Buffer interface {
	// Len returns the number of metrics currently in the buffer.
	Len() int

	// Add adds metrics to the buffer and returns number of dropped metrics.
	Add(metrics ...telegraf.Metric) int

	// Batch starts a transaction by returning a slice of metrics up to the
	// given batch-size starting from the oldest metric in the buffer. Metrics
	// are ordered from oldest to newest and must not be modified by the plugin.
	BeginTransaction(batchSize int) *Transaction

	// Flush ends a metric and persists the buffer state
	EndTransaction(*Transaction)

	// Stats returns the buffer statistics such as rejected, dropped and accepted metrics
	Stats() BufferStats

	// Close finalizes the buffer and closes all open resources
	Close() error
}

// BufferStats holds common metrics used for buffer implementations.
// Implementations of Buffer should embed this struct in them.
type BufferStats struct {
	MetricsAdded    selfstat.Stat
	MetricsWritten  selfstat.Stat
	MetricsRejected selfstat.Stat
	MetricsDropped  selfstat.Stat
	BufferSize      selfstat.Stat
	BufferLimit     selfstat.Stat
}

// NewBuffer returns a new empty Buffer with the given capacity.
func NewBuffer(name, id, alias string, capacity int, strategy, path string) (Buffer, error) {
	registerGob()

	bs := NewBufferStats(name, alias, capacity)

	switch strategy {
	case "", "memory":
		return NewMemoryBuffer(capacity, bs)
	case "disk":
		return NewDiskBuffer(name, id, path, bs)
	}
	return nil, fmt.Errorf("invalid buffer strategy %q", strategy)
}

func NewBufferStats(name, alias string, capacity int) BufferStats {
	tags := map[string]string{"output": name}
	if alias != "" {
		tags["alias"] = alias
	}

	bs := BufferStats{
		MetricsAdded: selfstat.Register(
			"write",
			"metrics_added",
			tags,
		),
		MetricsWritten: selfstat.Register(
			"write",
			"metrics_written",
			tags,
		),
		MetricsRejected: selfstat.Register(
			"write",
			"metrics_rejected",
			tags,
		),
		MetricsDropped: selfstat.Register(
			"write",
			"metrics_dropped",
			tags,
		),
		BufferSize: selfstat.Register(
			"write",
			"buffer_size",
			tags,
		),
		BufferLimit: selfstat.Register(
			"write",
			"buffer_limit",
			tags,
		),
	}
	bs.BufferSize.Set(int64(0))
	bs.BufferLimit.Set(int64(capacity))
	return bs
}

func (b *BufferStats) metricAdded() {
	b.MetricsAdded.Incr(1)
}

func (b *BufferStats) metricWritten(m telegraf.Metric) {
	AgentMetricsWritten.Incr(1)
	b.MetricsWritten.Incr(1)
	m.Accept()
}

func (b *BufferStats) metricRejected(m telegraf.Metric) {
	AgentMetricsRejected.Incr(1)
	b.MetricsRejected.Incr(1)
	m.Reject()
}

func (b *BufferStats) metricDropped(m telegraf.Metric) {
	AgentMetricsDropped.Incr(1)
	b.MetricsDropped.Incr(1)
	m.Reject()
}
