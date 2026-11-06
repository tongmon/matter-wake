package app

import (
	"testing"
	"time"
)

// maxClockStep is the coarsest step the clock may take: far finer than the
// buckets timing counts in, so one tick is measured, not rounded.
const maxClockStep = 10 * time.Microsecond

func newTestClock(t *testing.T) *clock {
	t.Helper()
	c, err := newClock()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The clock moves in steps fine enough to time a single tick. On Windows
// time.Now moves only about every half millisecond (pitfalls.md).
func TestClockStepIsFine(t *testing.T) {
	c := newTestClock(t)
	// A clock that never moves fails after this many reads in a row,
	// rather than spin until the test times out.
	const steps, maxReads = 100, 10_000_000
	smallest := time.Duration(1<<63 - 1)
	last := c.now()
	for range steps {
		next, reads := c.now(), 1
		for ; next == last && reads < maxReads; reads++ {
			next = c.now()
		}
		if next == last {
			t.Fatalf("the clock did not move in %d reads", maxReads)
		}
		if next < last {
			t.Fatalf("the clock went back from %v to %v", last, next)
		}
		smallest = min(smallest, next-last)
		last = next
	}
	t.Logf("the clock's smallest step is %v", smallest)
	if smallest > maxClockStep {
		t.Errorf("the clock's smallest step is %v, want at most %v", smallest, maxClockStep)
	}
}

// The clock counts real time: over a span long enough for time.Now's
// coarse steps not to matter, the two agree.
func TestClockKeepsTime(t *testing.T) {
	c := newTestClock(t)
	start, wallStart := c.now(), time.Now()
	time.Sleep(100 * ms)
	got, want := c.since(start), time.Since(wallStart)
	// Two of time.Now's steps on Windows, and some room for the reads.
	const tolerance = 2 * ms
	if got < want-tolerance || got > want+tolerance {
		t.Errorf("the clock measured %v where time.Now measured %v", got, want)
	}
}

func TestClockDoesNotAllocate(t *testing.T) {
	c := newTestClock(t)
	var d time.Duration
	if n := testing.AllocsPerRun(100, func() {
		d += c.since(c.now())
	}); n != 0 {
		t.Fatalf("reading the clock allocates %v times", n)
	}
	_ = d
}
