// Package sched runs batches of independent jobs on a fixed pool of
// goroutines.
//
// It is the only logic package allowed to use sync or start goroutines
// (see tools/archtest). Callers keep results deterministic by making the
// jobs of one batch independent: which goroutine runs a job, and in what
// order, must not change the outcome.
package sched

import (
	"sync"
	"sync/atomic"
)

// Pool runs jobs on a fixed set of worker goroutines. A Pool is used from
// one goroutine at a time.
type Pool struct {
	workers int
	start   []chan struct{}
	done    sync.WaitGroup
	next    atomic.Int64
	count   int64
	job     func(i int)
}

// New returns a pool with the given number of workers. With one worker or
// fewer, Run executes jobs inline and no goroutines are started.
func New(workers int) *Pool {
	p := &Pool{workers: max(workers, 1)}
	if p.workers == 1 {
		return p
	}
	p.start = make([]chan struct{}, p.workers)
	for i := range p.start {
		p.start[i] = make(chan struct{})
		go p.work(p.start[i])
	}
	return p
}

// Workers returns how many workers the pool has.
func (p *Pool) Workers() int {
	return p.workers
}

// Run calls job(i) for every i in [0, count) and returns when all calls
// have finished. Jobs may run concurrently and in any order.
//
// Run does not allocate, so pass a job that was created once (for
// example a method value stored in a field), not a new closure per call.
func (p *Pool) Run(count int, job func(i int)) {
	if p.workers == 1 || count <= 1 {
		for i := range count {
			job(i)
		}
		return
	}
	p.job = job
	p.count = int64(count)
	p.next.Store(0)
	// Wake no more workers than there are jobs.
	woken := p.start[:min(p.workers, count)]
	p.done.Add(len(woken))
	// The channel sends publish job and count to the workers.
	for _, c := range woken {
		c <- struct{}{}
	}
	p.done.Wait()
	p.job = nil
}

func (p *Pool) work(start <-chan struct{}) {
	for range start {
		for {
			i := p.next.Add(1) - 1
			if i >= p.count {
				break
			}
			p.job(int(i))
		}
		p.done.Done()
	}
}

// Close stops the workers. The pool must not be used afterwards.
func (p *Pool) Close() {
	for _, c := range p.start {
		close(c)
	}
	p.start = nil
}
