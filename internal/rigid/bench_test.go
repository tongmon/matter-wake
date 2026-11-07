package rigid

import (
	"math"
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

// BenchmarkWriteTurned erases and writes 64 bodies of 16×12 pixels, turned
// from 0° to 177°, moving them a third of a cell each time: the write
// stage alone, without a scene that changes with how bodies are written.
func BenchmarkWriteTurned(b *testing.B) {
	s, _, id := newSystem(b, 512, 256, 1)
	for k := range 64 {
		if err := s.add(id.wood, 8+k%16*30, 20+k/16*50, 16, 12, float32(k)*math.Pi/64); err != nil {
			b.Fatal(err)
		}
	}
	for i := range s.bodies {
		s.bodies[i].p, s.bodies[i].q = s.phys.Transform(s.bodies[i].id)
	}
	s.sync()
	b.ResetTimer()
	for n := range b.N {
		d := float32(0.03)
		if n%2 == 1 {
			d = -d
		}
		for i := range s.bodies {
			s.bodies[i].p.X += d
		}
		s.sync()
	}
}

// stages holds the time of each stage of each tick, and of the three that
// are the bodies' own together.
type stages struct {
	write, collide, cells, step, bodies []time.Duration
}

// BenchmarkBodies runs the heavy-load scene and reports, for each stage of
// a tick, its median, 95th percentile and largest time in µs: writing the
// bodies' pixels, making the terrain near them collision, the cells' tick
// with the queue's reinsertion (on 2 workers, the reference CPU's cores,
// design.md 2절), and Box2D's Step. "bodies" is the three stages that are
// the bodies' own, together in each tick: what the 3 ms target covers.
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
			if err := s.Step(); err != nil {
				b.Fatal(err)
			}
			t4 := now()
			all.write = append(all.write, t1-t0)
			all.collide = append(all.collide, t2-t1)
			all.cells = append(all.cells, t3-t2)
			all.step = append(all.step, t4-t3)
			all.bodies = append(all.bodies, t2-t0+t4-t3)
		}
		b.StopTimer()
		s.Close()
		w.Close()
		b.StartTimer()
	}
	for _, st := range []struct {
		name string
		d    []time.Duration
	}{{"write", all.write}, {"terrain", all.collide}, {"cells", all.cells}, {"step", all.step}, {"bodies", all.bodies}} {
		slices.Sort(st.d)
		n := len(st.d)
		b.ReportMetric(float64(st.d[n/2])/1e3, st.name+"-p50-µs")
		b.ReportMetric(float64(st.d[n*95/100])/1e3, st.name+"-p95-µs")
		b.ReportMetric(float64(st.d[n-1])/1e3, st.name+"-max-µs")
	}
}

// newStructure lays out the large break of Noita's C132, our way: one
// wooden body of 320 × 192 pixels in a room, resting on a stone floor,
// cut into 10 × 8 blocks of 24 × 16 pixels joined by necks 2 pixels wide
// (32,992 pixels; Noita's had 33,280). It returns the System, the world
// and the cells of the necks.
func newStructure(tb testing.TB) (*System, *world.World, [][2]int) {
	tb.Helper()
	s, w, id := newSystem(tb, 512, 256, 2)
	w.Fill(0, 250, 512, 256, id.stone)
	const x0, y0 = 96, 40
	place(tb, s, id.wood, x0, y0, 320, 192, 0)
	var gaps, necks [][2]int
	for j := range 192 {
		for i := range 320 {
			bi, bj := i%32, j%24
			if bi < 24 && bj < 16 {
				continue // a block
			}
			// A neck runs from a block to the next, across the middle of
			// the gap beside it.
			across := bj >= 7 && bj < 9 && bi >= 24 && i < 320-8
			down := bi >= 11 && bi < 13 && bj >= 16 && j < 192-8
			if across || down {
				necks = append(necks, [2]int{x0 + i, y0 + j})
			} else {
				gaps = append(gaps, [2]int{x0 + i, y0 + j})
			}
		}
	}
	debris := breakOff(tb, s, gaps...)
	if len(s.bodies) != 1 || slices.ContainsFunc(debris, func(n int) bool { return n != 0 }) {
		tb.Fatalf("the structure is %d bodies and %v debris", len(s.bodies), debris)
	}
	s.Write()
	return s, w, necks
}

// BenchmarkBreak cuts every neck of the C132 structure at once and
// reports how long Break takes to make the 80 blocks bodies, and the
// Write after it that writes them (break-ms, write-ms); and how long
// Break takes to shape the whole structure anew when one pixel is dug off
// its corner, as every tick of digging into it does (notch-ms). Noita
// took 73 to 79 ms to break its structure, and 93 to 98 ms for the whole
// queue (C132).
func BenchmarkBreak(b *testing.B) {
	var brk, write, notch []time.Duration
	now := newClock(b)
	debris := make([]int, testMaterials)
	for range b.N {
		b.StopTimer()
		s, _, necks := newStructure(b)
		s.Wear(96, 40, 1<<30)
		b.StartTimer()
		t0 := now()
		if err := s.Break(debris); err != nil {
			b.Fatal(err)
		}
		notch = append(notch, now()-t0)
		b.StopTimer()
		s.Write()
		for _, c := range necks {
			s.Wear(c[0], c[1], 1<<30)
		}
		b.StartTimer()
		t1 := now()
		if err := s.Break(debris); err != nil {
			b.Fatal(err)
		}
		t2 := now()
		s.Write()
		t3 := now()
		brk, write = append(brk, t2-t1), append(write, t3-t2)
		if len(s.bodies) != 80 {
			b.Fatalf("the structure broke into %d bodies, want 80", len(s.bodies))
		}
	}
	for _, st := range []struct {
		name string
		d    []time.Duration
	}{{"break", brk}, {"write", write}, {"notch", notch}} {
		slices.Sort(st.d)
		b.ReportMetric(float64(st.d[len(st.d)/2])/1e6, st.name+"-ms")
	}
}

// BenchmarkBreakMany cuts 512 small bodies in two at once, the other load
// of C132: each 32 × 16 pixels, two halves joined by a neck 2 pixels
// high, as bridges. It reports how long Break takes to make the 512 new
// bodies (break-ms). Noita took 44 to 45 ms for its 512 (C132).
func BenchmarkBreakMany(b *testing.B) {
	var brk []time.Duration
	now := newClock(b)
	debris := make([]int, testMaterials)
	for range b.N {
		b.StopTimer()
		s, w, id := newSystem(b, 1280, 420, 2)
		w.Fill(0, 410, 1280, 420, id.stone)
		var gaps, necks [][2]int
		for k := range 512 {
			x0, y0 := 4+(k%32)*40, 4+(k/32)*25
			place(b, s, id.wood, x0, y0, 32, 16, 0)
			for j := range 16 {
				for i := 15; i < 17; i++ {
					if j == 7 || j == 8 {
						necks = append(necks, [2]int{x0 + i, y0 + j})
					} else {
						gaps = append(gaps, [2]int{x0 + i, y0 + j})
					}
				}
			}
		}
		breakOff(b, s, gaps...)
		s.Write()
		for _, c := range necks {
			s.Wear(c[0], c[1], 1<<30)
		}
		b.StartTimer()
		t0 := now()
		if err := s.Break(debris); err != nil {
			b.Fatal(err)
		}
		brk = append(brk, now()-t0)
		if len(s.bodies) != 1024 {
			b.Fatalf("512 bridges broke into %d bodies, want 1024", len(s.bodies))
		}
	}
	slices.Sort(brk)
	b.ReportMetric(float64(brk[len(brk)/2])/1e6, "break-ms")
}
