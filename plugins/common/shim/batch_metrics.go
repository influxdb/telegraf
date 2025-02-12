package shim

import (
	"github.com/influxdata/telegraf"
	"sync"
)

type batchMetrics struct {
	metrics []telegraf.Metric
	wg      *sync.WaitGroup
	mu      *sync.RWMutex
}

func (bm *batchMetrics) add(metric telegraf.Metric) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.metrics = append(bm.metrics, metric)
}

func (bm *batchMetrics) clear() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.wg.Add(-len(bm.metrics))
	bm.metrics = bm.metrics[:0]
}

func (bm *batchMetrics) len() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return len(bm.metrics)
}
