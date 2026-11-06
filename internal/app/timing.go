package app

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/tongmon/matter-wake/internal/game"
)

// frameBudget is how long a frame may take at game.Hz.
const frameBudget = time.Second / game.Hz

// timing measures the CPU time of every tick and every draw of a session,
// and of every frame: the ticks since the last draw and that draw. After a
// stall, as while the window opens, Ebitengine runs several ticks to catch
// up and one frame holds them all. GPU time is not in it, since Ebitengine
// sends the draw commands later.
type timing struct {
	tick, draw, frame spread
	// pending is the time of the ticks since the last draw.
	pending time.Duration
}

func (t *timing) addTick(d time.Duration) {
	t.tick.add(d)
	t.pending += d
}

func (t *timing) addDraw(d time.Duration) {
	t.draw.add(d)
	t.frame.add(t.pending + d)
	t.pending = 0
}

func (t *timing) report() {
	slog.Info("timing", "ticks", t.tick.n, "tick", t.tick.String(), "draw", t.draw.String(),
		"frame", t.frame.String(), "over_budget", t.frame.over,
		"budget", fmt.Sprintf("%.1f ms", millis(frameBudget)))
}

// Durations are counted in buckets of bucketWidth up to spreadBuckets of
// them, so a session of any length takes no more memory and allocates
// nothing while it runs.
const (
	bucketWidth   = 100 * time.Microsecond
	spreadBuckets = 1000
)

// spread is how a set of durations is spread out.
type spread struct {
	n   int
	max time.Duration
	// over counts the durations longer than frameBudget.
	over int
	// counts[i] counts the durations above i and at most i+1 bucket
	// widths, and the last one those longer than every bucket.
	counts [spreadBuckets + 1]int
}

func (s *spread) add(d time.Duration) {
	s.n++
	s.max = max(s.max, d)
	if d > frameBudget {
		s.over++
	}
	s.counts[min(max((d-1)/bucketWidth, 0), spreadBuckets)]++
}

// quantile returns how long pct percent of the durations took at most,
// rounded up to a bucket width and never more than the longest one.
func (s *spread) quantile(pct int) time.Duration {
	if s.n == 0 {
		return 0
	}
	rank := (pct*s.n + 99) / 100
	seen := 0
	for i, c := range s.counts[:spreadBuckets] {
		if seen += c; seen >= rank {
			return min(time.Duration(i+1)*bucketWidth, s.max)
		}
	}
	return s.max
}

func (s *spread) String() string {
	if s.n == 0 {
		return "none"
	}
	return fmt.Sprintf("median %.1f ms, p99 %.1f ms, max %.1f ms",
		millis(s.quantile(50)), millis(s.quantile(99)), millis(s.max))
}

func millis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
