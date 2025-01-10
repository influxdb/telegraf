package models

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/tidwall/wal"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

type DiskBuffer struct {
	BufferStats
	sync.Mutex

	file *wal.Log
	path string

	batchFirst uint64 // Index of the first metric in the batch
	batchSize  uint64 // Number of metrics currently in the batch

	// Ending point of metrics read from disk on telegraf launch.
	// Used to know whether to discard tracking metrics.
	originalEnd uint64

	// The WAL library currently has no way to "fully empty" the walfile. In this case,
	// we have to do our best and track that the walfile "should" be empty, so that next
	// write, we can remove the invalid entry (also skipping this entry if it is being read).
	isEmpty bool

	// The mask contains offsets of metric already removed during a previous
	// transaction. Metrics at those offsets should not be contained in new
	// batches.
	mask []int
}

func NewDiskBuffer(name, id, path string, stats BufferStats) (*DiskBuffer, error) {
	filePath := filepath.Join(path, id)
	walFile, err := wal.Open(filePath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open wal file: %w", err)
	}
	//nolint:errcheck // cannot error here
	if index, _ := walFile.FirstIndex(); index == 0 {
		// simple way to test if the walfile is freshly initialized, meaning no existing file was found
		log.Printf("I! WAL file not found for plugin outputs.%s (%s), "+
			"this can safely be ignored if you added this plugin instance for the first time", name, id)
	}

	buf := &DiskBuffer{
		BufferStats: stats,
		file:        walFile,
		path:        filePath,
	}
	if buf.length() > 0 {
		buf.originalEnd = buf.writeIndex()
	}
	return buf, nil
}

func (b *DiskBuffer) Len() int {
	b.Lock()
	defer b.Unlock()
	return b.length()
}

func (b *DiskBuffer) length() int {
	if b.isEmpty {
		return 0
	}

	return b.entries() - len(b.mask)
}

func (b *DiskBuffer) entries() int {
	if b.readIndex() == 0 {
		return 0
	}
	return int(b.writeIndex() - b.readIndex())
}

// readIndex is the first index to start reading metrics from, or the head of the buffer
func (b *DiskBuffer) readIndex() uint64 {
	index, err := b.file.FirstIndex()
	if err != nil {
		panic(err) // can only occur with a corrupt wal file
	}
	return index
}

// writeIndex is the first index to start writing metrics to, or the tail of the buffer
func (b *DiskBuffer) writeIndex() uint64 {
	index, err := b.file.LastIndex()
	if err != nil {
		panic(err) // can only occur with a corrupt wal file
	}
	return index + 1
}

func (b *DiskBuffer) Add(metrics ...telegraf.Metric) int {
	b.Lock()
	defer b.Unlock()

	dropped := 0
	for _, m := range metrics {
		if !b.addSingleMetric(m) {
			dropped++
		}
		// as soon as a new metric is added, if this was empty, try to flush the "empty" metric out
		b.handleEmptyFile()
	}
	b.BufferSize.Set(int64(b.length()))
	return dropped
}

func (b *DiskBuffer) addSingleMetric(m telegraf.Metric) bool {
	data, err := metric.ToBytes(m)
	if err != nil {
		panic(err)
	}
	err = b.file.Write(b.writeIndex(), data)
	if err == nil {
		b.metricAdded()
		return true
	}
	return false
}

func (b *DiskBuffer) BeginTransaction(batchSize int) *Transaction {
	b.Lock()
	defer b.Unlock()

	if b.length() == 0 {
		return &Transaction{}
	}
	b.batchFirst = b.readIndex()
	b.batchSize = 0

	metrics := make([]telegraf.Metric, 0, batchSize)
	offsets := make([]int, 0, batchSize)
	readIndex := b.batchFirst
	endIndex := b.writeIndex()
	offset := 0
	for batchSize > 0 && readIndex < endIndex {
		data, err := b.file.Read(readIndex)
		if err != nil {
			panic(err)
		}
		readIndex++
		offset++

		if slices.Contains(b.mask, offset) {
			// Metric is masked by a previous write and is scheduled for removal
			continue
		}

		// Validate that a tracking metric is from this instance of telegraf and skip ones from older instances.
		// A tracking metric can be skipped here because metric.Accept() is only called once data is successfully
		// written to an output, so any tracking metrics from older instances can be dropped and reacquired to
		// have an accurate tracking information.
		// There are two primary cases here:
		// - ErrSkipTracking:  means that the tracking information was unable to be found for a tracking ID.
		// - Outside of range: means that the metric was guaranteed to be left over from the previous instance
		//                     as it was here when we opened the wal file in this instance.
		m, err := metric.FromBytes(data)
		if err != nil {
			if errors.Is(err, metric.ErrSkipTracking) {
				// could not look up tracking information for metric, skip
				continue
			}
			// non-recoverable error in deserialization, abort
			log.Printf("E! raw metric data: %v", data)
			panic(err)
		}
		if _, ok := m.(telegraf.TrackingMetric); ok && readIndex < b.originalEnd {
			// tracking metric left over from previous instance, skip
			continue
		}

		metrics = append(metrics, m)
		offsets = append(offsets, offset)
		b.batchSize++
		batchSize--
	}
	return &Transaction{Batch: metrics, valid: true, state: offsets}
}

func (b *DiskBuffer) EndTransaction(tx *Transaction) {
	if len(tx.Batch) == 0 {
		return
	}

	// Ignore invalid transactions and make sure they can only be finished once
	if !tx.valid {
		return
	}
	tx.valid = false

	// Get the metric offsets from the transaction
	offsets := tx.state.([]int)

	b.Lock()
	defer b.Unlock()

	// Mark metrics which should be removed in the internal mask
	remove := make([]int, 0, len(tx.Accept)+len(tx.Reject))
	for _, idx := range tx.Accept {
		b.metricWritten(tx.Batch[idx])
		remove = append(remove, offsets[idx])
	}
	for _, idx := range tx.Reject {
		b.metricRejected(tx.Batch[idx])
		remove = append(remove, offsets[idx])
	}
	b.mask = append(b.mask, remove...)
	sort.Ints(b.mask)

	// Remove the metrics that are marked for removal from the front of the
	// WAL file. All other metrics must be kept.
	if len(b.mask) == 0 || b.mask[0] != 0 {
		// Mask is empty or the first index is not the front of the file, so
		// exit early as there is nothing to remove
		return
	}

	// Determine up to which index we can remove the entries from the WAL file
	var removeIdx int
	for i, offset := range b.mask {
		if offset != i {
			break
		}
		removeIdx = offset
	}

	// Remove the metrics in front from the WAL file
	b.isEmpty = b.entries()-removeIdx-1 <= 0
	if b.isEmpty {
		// WAL files cannot be fully empty but need to contain at least one
		// item to not throw an error
		if err := b.file.TruncateFront(b.writeIndex()); err != nil {
			log.Printf("E! batch length: %d, first: %d, size: %d", len(tx.Batch), b.batchFirst, b.batchSize)
			panic(err)
		}
	} else {
		if err := b.file.TruncateFront(b.batchFirst + uint64(removeIdx+1)); err != nil {
			log.Printf("E! batch length: %d, first: %d, size: %d", len(tx.Batch), b.batchFirst, b.batchSize)
			panic(err)
		}
	}

	// Truncate the mask and update the relative offsets
	b.mask = b.mask[:removeIdx]
	for i := range b.mask {
		b.mask[i] -= removeIdx
	}

	// check if the original end index is still valid, clear if not
	if b.originalEnd < b.readIndex() {
		b.originalEnd = 0
	}

	b.resetBatch()
	b.BufferSize.Set(int64(b.length()))
}

func (b *DiskBuffer) Stats() BufferStats {
	return b.BufferStats
}

func (b *DiskBuffer) Close() error {
	return b.file.Close()
}

func (b *DiskBuffer) resetBatch() {
	b.batchFirst = 0
	b.batchSize = 0
}

// This is very messy and not ideal, but serves as the only way I can find currently
// to actually treat the walfile as empty if needed, since Truncate() calls require
// that at least one entry remains in them otherwise they return an error.
// Related issue: https://github.com/tidwall/wal/issues/20
func (b *DiskBuffer) handleEmptyFile() {
	if !b.isEmpty {
		return
	}
	if err := b.file.TruncateFront(b.readIndex() + 1); err != nil {
		log.Printf("E! readIndex: %d, buffer len: %d", b.readIndex(), b.length())
		panic(err)
	}
	b.isEmpty = false
}
