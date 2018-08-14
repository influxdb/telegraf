package vsphere

import (
	"context"
	"log"
	"sync"
)

// WorkerFunc is a function that is supposed to do the actual work
// of the WorkerPool. It is similar to the "map" portion of the
// map/reduce semantics, in that it takes a single value as an input,
// does some processing and returns a single result.
type WorkerFunc func(context.Context, interface{}) interface{}

// PushFunc is called from a FillerFunc to push a workitem onto
// the input channel. Wraps some logic for gracefulk shutdowns.
type PushFunc func(context.Context, interface{}) bool

// DrainerFunc represents a function used to "drain" the WorkerPool,
// i.e. pull out all the results generated by the workers and processing
// them. The DrainerFunc is called once per result produced.
type DrainerFunc func(context.Context, interface{})

// FillerFunc represents a function for filling the WorkerPool with jobs.
// It is called once and is responsible for pushing jobs onto the supplied channel.
type FillerFunc func(context.Context, PushFunc)

// WorkerPool implements a simple work pooling mechanism. It runs a predefined
// number of goroutines to process jobs. Jobs are inserted using the Fill call
// and results are retrieved through the Drain function.
type WorkerPool struct {
	wg  sync.WaitGroup
	In  chan interface{}
	Out chan interface{}
}

// NewWorkerPool creates a worker pool
func NewWorkerPool(bufsize int) *WorkerPool {
	return &WorkerPool{
		In:  make(chan interface{}, bufsize),
		Out: make(chan interface{}, bufsize),
	}
}

func (w *WorkerPool) push(ctx context.Context, job interface{}) bool {
	select {
	case w.In <- job:
		return true
	case <-ctx.Done():
		return false
	}
}

func (w *WorkerPool) pushOut(ctx context.Context, result interface{}) bool {
	select {
	case w.Out <- result:
		return true
	case <-ctx.Done():
		return false
	}
}

// Run takes a WorkerFunc and runs it in 'n' goroutines.
func (w *WorkerPool) Run(ctx context.Context, f WorkerFunc, n int) bool {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		var localWg sync.WaitGroup
		localWg.Add(n)
		for i := 0; i < n; i++ {
			go func() {
				defer localWg.Done()
				for {
					select {
					case job, ok := <-w.In:
						if !ok {
							return
						}
						w.pushOut(ctx, f(ctx, job))
					case <-ctx.Done():
						log.Printf("D! [input.vsphere]: Stop requested for worker pool. Exiting.")
						return
					}
				}
			}()
		}
		localWg.Wait()
		close(w.Out)
	}()
	return ctx.Err() == nil
}

// Fill runs a FillerFunc responsible for supplying work to the pool. You may only
// call Fill once. Calling it twice will panic.
func (w *WorkerPool) Fill(ctx context.Context, f FillerFunc) bool {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		f(ctx, w.push)
		close(w.In)
	}()
	return true
}

// Drain runs a DrainerFunc for each result generated by the workers.
func (w *WorkerPool) Drain(ctx context.Context, f DrainerFunc) bool {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for result := range w.Out {
			f(ctx, result)
		}
	}()
	w.wg.Wait()
	return ctx.Err() == nil
}
