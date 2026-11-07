package rigid

import (
	"slices"
	"testing"
	"time"

	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// heavyBodies is the heavy-load scene's number of dynamic bodies (🧪,
// design.md 2절): about the 454 to 457 Noita's base held (C132).
const heavyBodies = 450

// benchTicks is how long the scene runs: 3 seconds, in which the bodies
// fall into the room and land on each other, the sand and the water, all
// of them awake most of the time, as Noita's base kept 360 to 410 of its
// bodies awake (C132).
const benchTicks = 180

// newHeavyScene lays out the heavy-load scene in a room the size of the
// screen: stone walls and floor, a pool of water and heaps of sand on the
// floor, and heavyBodies small boxes in rows above them, 3 to 6 cells on
// a side, about the 18 pixels a body Noita's base wrote on average
// (C134: 5,372 pixels for 297 bodies).
func newHeavyScene(tb testing.TB, workers int) (*System, *world.World) {
	tb.Helper()
	s, w, id := newSystem(tb, 480, 270, workers)
	w.Fill(0, 264, 480, 270, id.stone)
	w.Fill(0, 0, 4, 270, id.stone)
	w.Fill(476, 0, 480, 270, id.stone)
	w.Fill(4, 236, 240, 264, id.water)
	for k := range 3 {
		x := 260 + 70*k
		for row := range 30 {
			w.Fill(x+row, 234+row, x+60-row, 235+row, id.sand)
		}
	}
	r := rng.New(7)
	for k := range heavyBodies {
		col, row := k%30, k/30
		bw, bh := 3+r.IntN(4), 3+r.IntN(4)
		drop(tb, s, id.wood, 8+col*15+r.IntN(4), 4+row*13+r.IntN(4), bw, bh)
	}
	return s, w
}

// stages holds the time of each stage of each tick.
type stages struct {
	write, collide, cells, step []time.Duration
}

// BenchmarkBodies runs the heavy-load scene and reports, for each stage of
// a tick, its median, 95th percentile and largest time in µs: writing the
// bodies' pixels, making the terrain near them collision, the cells' tick
// with the queue's reinsertion (on 2 workers, the reference CPU's cores,
// design.md 2절), and Box2D's Step.
func BenchmarkBodies(b *testing.B) {
	var all stages
	now := newClock(b)
	for range b.N {
		b.StopTimer()
		s, w := newHeavyScene(b, 2)
		b.StartTimer()
		for range benchTicks {
			t0 := now()
			s.Write()
			t1 := now()
			if err := s.Collide(); err != nil {
				b.Fatal(err)
			}
			t2 := now()
			w.Step()
			t3 := now()
			s.Step()
			t4 := now()
			all.write = append(all.write, t1-t0)
			all.collide = append(all.collide, t2-t1)
			all.cells = append(all.cells, t3-t2)
			all.step = append(all.step, t4-t3)
		}
		b.StopTimer()
		s.Close()
		w.Close()
		b.StartTimer()
	}
	for _, st := range []struct {
		name string
		d    []time.Duration
	}{{"write", all.write}, {"terrain", all.collide}, {"cells", all.cells}, {"step", all.step}} {
		slices.Sort(st.d)
		n := len(st.d)
		b.ReportMetric(float64(st.d[n/2])/1e3, st.name+"-p50-µs")
		b.ReportMetric(float64(st.d[n*95/100])/1e3, st.name+"-p95-µs")
		b.ReportMetric(float64(st.d[n-1])/1e3, st.name+"-max-µs")
	}
}
