package sched

import (
	"sync/atomic"
	"testing"
)

func TestRunCallsEveryJobOnce(t *testing.T) {
	for _, workers := range []int{0, 1, 2, 7} {
		p := New(workers)
		for _, count := range []int{0, 1, 2, 3, 100} {
			hits := make([]atomic.Int32, count)
			p.Run(count, func(i int) { hits[i].Add(1) })
			for i := range hits {
				if n := hits[i].Load(); n != 1 {
					t.Fatalf("workers=%d count=%d: job %d ran %d times", workers, count, i, n)
				}
			}
		}
		p.Close()
	}
}

func TestRunDoesNotAllocate(t *testing.T) {
	p := New(4)
	defer p.Close()
	var sum atomic.Int64
	job := func(i int) { sum.Add(int64(i)) }
	if n := testing.AllocsPerRun(100, func() { p.Run(64, job) }); n != 0 {
		t.Fatalf("Run allocates %v times per call", n)
	}
}
