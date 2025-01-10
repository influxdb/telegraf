package models

import (
	"sync"

	"github.com/influxdata/telegraf"
)

// MemoryBuffer stores metrics in a circular buffer.
type MemoryBuffer struct {
	sync.Mutex
	BufferStats

	buf   []telegraf.Metric
	first int // index of the first/oldest metric
	last  int // one after the index of the last/newest metric
	size  int // number of metrics currently in the buffer
	cap   int // the capacity of the buffer

	batchFirst int // index of the first metric in the batch
	batchSize  int // number of metrics currently in the batch
}

func NewMemoryBuffer(capacity int, stats BufferStats) (*MemoryBuffer, error) {
	return &MemoryBuffer{
		BufferStats: stats,
		buf:         make([]telegraf.Metric, capacity),
		cap:         capacity,
	}, nil
}

func (b *MemoryBuffer) Len() int {
	b.Lock()
	defer b.Unlock()

	return b.length()
}

func (b *MemoryBuffer) Add(metrics ...telegraf.Metric) int {
	b.Lock()
	defer b.Unlock()

	dropped := 0
	for i := range metrics {
		if n := b.addMetric(metrics[i]); n != 0 {
			dropped += n
		}
	}

	b.BufferSize.Set(int64(b.length()))
	return dropped
}

func (b *MemoryBuffer) BeginTransaction(batchSize int) *Transaction {
	b.Lock()
	defer b.Unlock()

	outLen := min(b.size, batchSize)
	if outLen == 0 {
		return &Transaction{}
	}

	b.batchFirst = b.first
	b.batchSize = outLen
	batchIndex := b.batchFirst
	batch := make([]telegraf.Metric, outLen)
	for i := range batch {
		batch[i] = b.buf[batchIndex]
		b.buf[batchIndex] = nil
		batchIndex = b.next(batchIndex)
	}

	b.first = b.nextby(b.first, b.batchSize)
	b.size -= outLen
	return &Transaction{Batch: batch, valid: true}
}

func (b *MemoryBuffer) EndTransaction(tx *Transaction) {
	b.Lock()
	defer b.Unlock()

	// Ignore invalid transactions and make sure they can only be finished once
	if !tx.valid {
		return
	}
	tx.valid = false

	// Accept metrics
	for _, idx := range tx.Accept {
		b.metricWritten(tx.Batch[idx])
	}

	// Reject metrics
	for _, idx := range tx.Reject {
		b.metricRejected(tx.Batch[idx])
	}

	// Keep metrics
	keep := tx.InferKeep()
	if len(keep) > 0 {
		restore := min(len(keep), b.cap-b.size)
		b.first = b.prevby(b.first, restore)
		b.size = min(b.size+restore, b.cap)

		// Restore the metrics that fit into the buffer
		current := b.first
		for i := 0; i < restore; i++ {
			b.buf[current] = tx.Batch[keep[i]]
			current = b.next(current)
		}

		// Drop all remaining metrics
		for i := restore; i < len(keep); i++ {
			b.metricDropped(tx.Batch[keep[i]])
		}
	}

	b.resetBatch()
	b.BufferSize.Set(int64(b.length()))
}

func (b *MemoryBuffer) Close() error {
	return nil
}

func (b *MemoryBuffer) Stats() BufferStats {
	return b.BufferStats
}

func (b *MemoryBuffer) length() int {
	return min(b.size+b.batchSize, b.cap)
}

func (b *MemoryBuffer) addMetric(m telegraf.Metric) int {
	dropped := 0
	// Check if Buffer is full
	if b.size == b.cap {
		b.metricDropped(b.buf[b.last])
		dropped++

		if b.batchSize > 0 {
			b.batchSize--
			b.batchFirst = b.next(b.batchFirst)
		}
	}

	b.metricAdded()

	b.buf[b.last] = m
	b.last = b.next(b.last)

	if b.size == b.cap {
		b.first = b.next(b.first)
	}

	b.size = min(b.size+1, b.cap)
	return dropped
}

// next returns the next index with wrapping.
func (b *MemoryBuffer) next(index int) int {
	index++
	if index == b.cap {
		return 0
	}
	return index
}

// nextby returns the index that is count newer with wrapping.
func (b *MemoryBuffer) nextby(index, count int) int {
	index += count
	index %= b.cap
	return index
}

// prevby returns the index that is count older with wrapping.
func (b *MemoryBuffer) prevby(index, count int) int {
	index -= count
	for index < 0 {
		index += b.cap
	}

	index %= b.cap
	return index
}

func (b *MemoryBuffer) resetBatch() {
	b.batchFirst = 0
	b.batchSize = 0
}
