package app

import (
	"testing"
	"time"
)

const ms = time.Millisecond

func TestSpreadQuantiles(t *testing.T) {
	var s spread
	for range 50 {
		s.add(50 * time.Microsecond)
	}
	for range 49 {
		s.add(2050 * time.Microsecond)
	}
	s.add(30 * ms)
	tests := []struct {
		pct  int
		want time.Duration
	}{
		// Quantiles are the top of their bucket, a tenth of a millisecond.
		{50, 100 * time.Microsecond},
		{51, 2100 * time.Microsecond},
		{99, 2100 * time.Microsecond},
		// The slowest is exact.
		{100, 30 * ms},
	}
	for _, tt := range tests {
		if got := s.quantile(tt.pct); got != tt.want {
			t.Errorf("quantile(%d) = %v, want %v", tt.pct, got, tt.want)
		}
	}
	if s.n != 100 || s.max != 30*ms {
		t.Errorf("n, max = %d, %v; want 100, 30ms", s.n, s.max)
	}
	if s.over != 1 {
		t.Errorf("over = %d, want 1: only 30 ms is longer than a frame", s.over)
	}
}

// A quantile is never more than the slowest sample, and one past the last
// bucket is the slowest sample itself.
func TestSpreadQuantilesStayAtMost(t *testing.T) {
	var s spread
	s.add(1020 * time.Microsecond)
	if got := s.quantile(50); got != 1020*time.Microsecond {
		t.Errorf("one sample: median = %v, want the sample", got)
	}
	s.add(250 * ms)
	s.add(260 * ms)
	if got := s.quantile(60); got != 260*ms {
		t.Errorf("past the buckets: quantile = %v, want the slowest, 260ms", got)
	}
	var none spread
	if got := none.quantile(50); got != 0 {
		t.Errorf("no samples: quantile = %v, want 0", got)
	}
}

// A frame is the ticks since the last draw and that draw.
func TestTimingFrames(t *testing.T) {
	var tm timing
	tm.addTick(5 * ms)
	tm.addTick(5 * ms)
	tm.addDraw(2 * ms)
	tm.addTick(ms)
	tm.addDraw(ms)
	if tm.tick.n != 3 || tm.draw.n != 2 || tm.frame.n != 2 {
		t.Fatalf("counts: %d ticks, %d draws, %d frames; want 3, 2, 2", tm.tick.n, tm.draw.n, tm.frame.n)
	}
	if tm.frame.max != 12*ms {
		t.Errorf("slowest frame = %v, want 12ms", tm.frame.max)
	}
	if got := tm.frame.quantile(50); got != 2*ms {
		t.Errorf("median frame = %v, want 2ms", got)
	}
}

func TestSpreadString(t *testing.T) {
	var s spread
	if got := s.String(); got != "none" {
		t.Errorf("empty: %q, want none", got)
	}
	s.add(1450 * time.Microsecond)
	s.add(20 * ms)
	if got, want := s.String(), "median 1.5 ms, p99 20.0 ms, max 20.0 ms"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestTimingDoesNotAllocate(t *testing.T) {
	var tm timing
	if n := testing.AllocsPerRun(100, func() {
		tm.addTick(3 * ms)
		tm.addDraw(ms)
	}); n != 0 {
		t.Fatalf("timing allocates %v times a frame", n)
	}
}
