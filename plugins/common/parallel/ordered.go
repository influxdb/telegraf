package parallel

import (
	"sync"

	"github.com/influxdata/telegraf"
)

type Ordered struct {
	wg sync.WaitGroup
	fn func(telegraf.Metric) []telegraf.Metric

	// queue of jobs coming in. Workers pick jobs off this queue for processing
	workerQueue chan job

	// queue of ordered metrics going out
	queue chan futureMetric
}

func NewOrdered(acc telegraf.Accumulator, fn func(telegraf.Metric) []telegraf.Metric, orderedQueueSize, workerCount int) *Ordered {
	p := &Ordered{
		fn:          fn,
		workerQueue: make(chan job, workerCount),
		queue:       make(chan futureMetric, orderedQueueSize),
	}
	p.startWorkers(workerCount)
	p.wg.Add(1)
	go func() {
		p.readQueue(acc)
		p.wg.Done()
	}()
	return p
}

func (p *Ordered) Enqueue(metric telegraf.Metric) {
	future := make(futureMetric)
	p.queue <- future

	// write the future to the worker pool. Order doesn't matter now because the
	// outgoing p.queue will enforce order regardless of the order the jobs are
	// completed in
	p.workerQueue <- job{
		future: future,
		metric: metric,
	}
}

func (p *Ordered) readQueue(acc telegraf.Accumulator) {
	// wait for the response from each worker in order
	for mCh := range p.queue {
		// allow each worker to write out multiple metrics
		for metrics := range mCh {
			for _, m := range metrics {
				acc.AddMetric(m)
			}
		}
	}
}

func (p *Ordered) startWorkers(count int) {
	p.wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			for job := range p.workerQueue {
				job.future <- p.fn(job.metric)
				close(job.future)
			}
			p.wg.Done()
		}()
	}
}

func (p *Ordered) Stop() {
	close(p.queue)
	close(p.workerQueue)
	p.wg.Wait()
}

type futureMetric chan []telegraf.Metric

type job struct {
	future futureMetric
	metric telegraf.Metric
}
