package world

import (
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// perSecond converts cells per second to a particle's 1/ParticleOne cells
// per tick.
func perSecond(cells int) int32 {
	return int32(cells * ParticleOne / 60)
}

// totalOf returns how many cells of id the world holds in the grid, the
// queue and the particles.
func totalOf(w *World, id material.ID) int {
	n := make([]int, testMaterialsLen)
	w.Counts(n)
	w.Queued(n)
	w.Flying(n)
	return n[id]
}

// A launched cell leaves the grid, flies as a particle on Noita's
// gravity, and goes back into the grid through the queue where it stops:
// on the floor. It is counted all the way. The world is wide enough that
// the cells its path checks, which swing aside (TestPathSwingsAside), stay
// in it.
func TestLaunchedCellFliesAndLands(t *testing.T) {
	w, ids := newTestWorld(t, 256, 128, 1)
	w.Fill(0, 120, 256, 128, ids.stone)
	w.Set(128, 100, ids.water)
	w.Launch(128, 100, 0, perSecond(-120), 1)
	w.Flush()
	if c, _ := w.At(128, 100); c.Mat != material.Air {
		t.Fatalf("the launched cell left %d behind, want air", c.Mat)
	}
	if w.Particles() != 1 || totalOf(w, ids.water) != 1 {
		t.Fatalf("%d particles and %d water after the launch, want 1 and 1", w.Particles(), totalOf(w, ids.water))
	}
	top := 100
	for tick := 0; w.Particles() > 0; tick++ {
		if tick > 300 {
			t.Fatal("the particle still flies after 300 ticks")
		}
		w.Step()
		if n := totalOf(w, ids.water); n != 1 {
			t.Fatalf("tick %d: %d water in the grid, queue and particles, want 1", w.Tick(), n)
		}
		if w.Particles() > 0 {
			_, y := w.particles[0].At()
			top = min(top, y)
		}
	}
	// 2 cells a tick up against 100/3600 cells per tick², from the middle
	// of row 100, the first tick at a quarter of that while it still
	// passes: the top is near 25.9 (sum of the steps).
	if top != 25 {
		t.Fatalf("the particle rose to row %d, want 25", top)
	}
	// On the way down the checked cells reach the floor before the
	// particle does, far aside: it stops on the last step it passed, and
	// its cell comes back 42 cells aside.
	if x, y, ok := find(w, ids.water); !ok || y != 119 || x != 86 {
		t.Fatalf("the water came back at (%d, %d), %v; want (86, 119) on the floor", x, y, ok)
	}
}

// The cells a path checks lean aside, as Noita's do (C82, C140 6): the
// last point checked is a cell's corner, while the particle is anywhere in
// its cell, and the steps go along the line from that corner to the
// target, from the particle. A drop thrown straight up checks cells
// further aside each tick, from one side to the other, until near the top
// they leave the grid: it stops there, and its cell comes back 31 cells
// aside, as Noita's stopped 28 cells aside at the edge of the area it
// keeps particles in.
func TestPathSwingsAside(t *testing.T) {
	w, ids := newTestWorld(t, 64, 128, 1)
	w.Set(32, 100, ids.water)
	w.Launch(32, 100, 0, perSecond(-120), 1)
	w.Flush()
	widest := int64(0)
	for w.Particles() > 0 {
		if w.Tick() > 300 {
			t.Fatal("the particle still flies after 300 ticks")
		}
		p := &w.particles[0]
		widest = max(widest, abs64(p.LX>>particleShift-p.X>>particleShift))
		w.Step()
	}
	if widest != 31 {
		t.Fatalf("the checked cells swung %d cells aside, want 31", widest)
	}
	if c, _ := w.At(63, 24); c.Mat != ids.water {
		t.Fatalf("(63, 24) holds %d after the particle stopped, want its water", c.Mat)
	}
}

// A particle in free flight follows the integration Noita's does (C84):
// it heads where its velocity at the start of the tick takes it, then
// gains gravity. It leaves the grid passing, so its first tick gains a
// quarter of gravity; the empty cell it steps into ends that.
func TestParticlesFallWithGravity(t *testing.T) {
	w, ids := newTestWorld(t, 64, 512, 1)
	w.Set(10, 10, ids.water)
	vx, vy := perSecond(30), perSecond(-30)
	// Slower than 50 cells/s upward: it passes through anything, but there
	// is nothing to pass through, and the first empty cell ends it.
	w.Launch(10, 10, vx, vy, 7)
	w.Flush()
	x0, y0 := w.particles[0].X, w.particles[0].Y
	for n := int64(1); n <= 100; n++ {
		w.Step()
		p := &w.particles[0]
		wantX := x0 + n*int64(vx)
		wantY := y0 + n*int64(vy) + (n-1)*passGravity + (n-1)*(n-2)/2*particleGravity
		if p.X != wantX || p.Y != wantY {
			t.Fatalf("after %d ticks the particle is at (%d, %d), want (%d, %d)", n, p.X, p.Y, wantX, wantY)
		}
		if p.Flags&Passing != 0 {
			t.Fatalf("after %d ticks in the air the particle still passes", n)
		}
	}
}

// launchRaw adds a particle of the cell at (x, y), from its middle, moving
// down at vy and passing through matter denser than density, as Launch
// does but without its draws.
func launchRaw(w *World, x, y int, vy int32, density uint8) {
	w.Launch(x, y, 0, vy, 1)
	p := &w.particles[len(w.particles)-1]
	p.Density = density
	w.Flush()
}

// A particle that leaves the grid passes through matter while it rises, at
// a quarter of gravity, through at most passCells cells: here, up through
// stone, after which it stops inside the stone, and its cell goes back
// above the stone, where the queue finds empty space.
func TestPassingThroughMatterRunsOut(t *testing.T) {
	w, ids := newTestWorld(t, 64, 256, 1)
	w.Fill(0, 100, 64, 200, ids.stone)
	w.Set(30, 190, ids.water)
	launchRaw(w, 30, 190, perSecond(-180), 0)
	w.Step()
	p := w.particles[0]
	if p.Flags&Passing == 0 {
		t.Fatal("the particle stopped passing in the stone")
	}
	if want := perSecond(-180) + passGravity; p.VY != want {
		t.Fatalf("while passing the particle's speed went to %d, want %d: a quarter of gravity", p.VY, want)
	}
	last := 190
	for w.Particles() > 0 {
		_, last = w.particles[0].At()
		w.Step()
	}
	// From row 190 it may pass 20 cells, one more as it runs out, and the
	// odd cell its steps skip: their count comes from the corner of the
	// cell it last checked, as Noita's does, so it may fall a cell short.
	if last < 190-passCells-6 || last > 190-passCells+1 {
		t.Fatalf("the particle passed up to row %d, want about %d", last, 190-passCells)
	}
	if _, y, ok := find(w, ids.water); !ok || y != 99 {
		t.Fatalf("the water went back in row %d, %v; want 99, on top of the stone", y, ok)
	}
}

// A particle passes only through matter denser than its own: one with
// water's density stops passing in water, and the water after that stops
// it.
func TestDensityEndsPassing(t *testing.T) {
	w, ids := newTestWorld(t, 64, 256, 1)
	w.Fill(0, 100, 64, 200, ids.water)
	launchRaw(w, 31, 190, perSecond(-180), w.mats[ids.water].density)
	w.Step()
	if w.Particles() != 0 {
		t.Fatalf("a particle with water's density still flies in water: %+v", w.particles[0])
	}
	if n := totalOf(w, ids.water); n != 64*100 {
		t.Fatalf("%d water, want %d", n, 64*100)
	}
}

// Water passes through a body's cells, as in Noita: water (4) is lighter
// than wood (6). Sand, as dense as wood, does not.
func TestWaterPassesThroughABody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		passes bool
	}{{"water", true}, {"sand", false}} {
		t.Run(tc.name, func(t *testing.T) {
			w, ids := newTestWorld(t, 64, 256, 1)
			w.Fill(20, 150, 40, 160, ids.wood)
			id := map[string]material.ID{"water": ids.water, "sand": ids.sand}[tc.name]
			w.Set(30, 160, id)
			density := w.mats[id].density
			launchRaw(w, 30, 160, perSecond(-180), density)
			w.Step()
			if passed := w.Particles() > 0 && w.particles[0].Flags&Passing != 0; passed != tc.passes {
				t.Fatalf("%s passing through wood: %v, want %v", tc.name, passed, tc.passes)
			}
		})
	}
}

// A particle that falls stops passing and stops at the first matter it
// meets, above it: water falling on a pool lands on its surface.
func TestFallingParticleStopsOnTheSurface(t *testing.T) {
	w, ids := newTestWorld(t, 64, 256, 1)
	w.Fill(0, 200, 64, 256, ids.water)
	w.Set(30, 50, ids.water)
	launchRaw(w, 30, 50, perSecond(60), 0)
	if w.particles[0].Flags&Passing == 0 {
		t.Fatal("a launched particle does not start passing")
	}
	for range 300 {
		w.Step()
		if w.Particles() > 0 && w.particles[0].Flags&Passing != 0 {
			t.Fatal("a falling particle still passes")
		}
		if w.Particles() == 0 {
			break
		}
	}
	if w.Particles() != 0 {
		t.Fatal("the particle never landed")
	}
	// The checked cells lean aside (TestPathSwingsAside): it lands a cell
	// aside.
	if c, _ := w.At(29, 199); c.Mat != ids.water {
		t.Fatalf("(29, 199) holds %d after the landing, want water on the surface", c.Mat)
	}
	if n := totalOf(w, ids.water); n != 64*56+1 {
		t.Fatalf("%d water, want %d", n, 64*56+1)
	}
}

// Launch draws a particle's density as Noita does (C81): one leaving
// upward faster than 50 cells/s keeps its cell's density about half the
// time; a slower one never does.
func TestLaunchDrawsTheDensity(t *testing.T) {
	w, ids := newTestWorld(t, 512, 64, 1)
	for x := range 512 {
		w.Set(x, 10, ids.water)
		w.Set(x, 20, ids.water)
	}
	for x := range 512 {
		w.Launch(x, 10, 0, perSecond(-51), uint64(x))
		w.Launch(x, 20, 0, perSecond(-49), uint64(x))
	}
	dense := [2]int{}
	for i, p := range w.particles {
		if p.Density != 0 {
			dense[i%2]++
		}
	}
	if dense[0] < 200 || dense[0] > 310 || dense[1] != 0 {
		t.Fatalf("%d of 512 fast and %d of 512 slow particles keep the density, want about 256 and 0", dense[0], dense[1])
	}
}

// Launch leaves air and a body's pixel alone.
func TestLaunchTakesOnlyCellsThatMove(t *testing.T) {
	w, ids := newTestWorld(t, 64, 64, 1)
	w.Put(5, 5, BodyCell(ids.wood, 0, 0, 0, 0))
	w.Flush()
	w.Launch(5, 5, 0, 0, 1)
	w.Launch(6, 5, 0, 0, 1)
	w.Launch(-1, 5, 0, 0, 1)
	if w.Particles() != 0 {
		t.Fatalf("%d particles from air, a body's pixel and outside the grid", w.Particles())
	}
}

// fill adds specks with flags until the pool they count against is full.
// They go in the next tick.
func fill(w *World, flags ParticleFlags, mat material.ID) {
	for w.pooled[poolOf(flags)] < maxParticles {
		w.particles = append(w.particles, Particle{X: 1 << particleShift, Y: 1 << particleShift, Cell: Cell{Mat: mat}, Flags: flags})
		w.pooled[poolOf(flags)]++
	}
}

// Particles count against pools, as Noita's do (C140 7): a cell's particle
// and a crack's speck against one, a speck a body pushing matter throws
// against another. When a pool is full, a launched cell goes into the
// queue instead, and no speck is made; the other pool still takes its own.
func TestFullPoolQueuesTheCell(t *testing.T) {
	w, ids := newTestWorld(t, 64, 64, 1)
	fill(w, Speck, ids.wood)
	w.Set(30, 30, ids.water)
	w.Launch(30, 30, 0, 0, 1)
	w.Spray(Cell{Mat: ids.wood}, 30, 30, 0, 0, 1)
	if w.Particles() != maxParticles || w.QueueLen() != 1 {
		t.Fatalf("%d particles and %d queued, want %d and 1", w.Particles(), w.QueueLen(), maxParticles)
	}
	at := int64(30)<<particleShift + ParticleOne/2
	w.Spatter(Cell{Mat: ids.water}, at, at, 0, 0, 1, true)
	if w.Particles() != maxParticles+1 {
		t.Fatalf("%d particles after a body's speck, want %d: it has its own pool", w.Particles(), maxParticles+1)
	}
	fill(w, Speck|Pushed, ids.water)
	w.Spatter(Cell{Mat: ids.water}, at, at, 0, 0, 1, true)
	if w.Particles() != 2*maxParticles {
		t.Fatalf("%d particles, want both pools full at %d", w.Particles(), 2*maxParticles)
	}
	w.Step()
	if w.pooled != [pools]int{0, 1} || w.Particles() != 1 {
		t.Fatalf("pools hold %v and %d particles after the filling specks went, want [0 1] and 1", w.pooled, w.Particles())
	}
}

// A body's speck passes from the start, as Noita's (C140 4.1), but it is
// denser than anything: the first matter it meets ends its passing, and,
// a speck of a liquid, the next stops it and it goes. It changes no cell.
func TestBodySpeckStopsAtTheSecondMatter(t *testing.T) {
	w, ids := newTestWorld(t, 64, 128, 1)
	w.Fill(0, 40, 64, 128, ids.water)
	w.Step()
	cells := snapshot(w)
	// From the middle of the water, flying up at 60 cells/s: a cell a tick.
	at := int64(30)<<particleShift + ParticleOne/2
	w.Spatter(Cell{Mat: ids.water}, at, int64(80)<<particleShift+ParticleOne/2, 0, perSecond(-60), 1, true)
	p := &w.particles[0]
	if p.Flags != Speck|Pushed|Passing || p.Density != speckDensity || p.Pass != passCells {
		t.Fatalf("a body's speck has flags %#x, density %d and %d cells to pass; want passing, %d and %d",
			p.Flags, p.Density, p.Pass, speckDensity, passCells)
	}
	w.Step()
	if w.Particles() != 1 || w.particles[0].Flags&Passing != 0 {
		t.Fatal("the first water did not end the speck's passing")
	}
	w.Step()
	if w.Particles() != 0 {
		t.Fatal("the next water did not stop the speck")
	}
	if got := snapshot(w); !slices.EqualFunc(got, cells, func(a, b Cell) bool { return a.Mat == b.Mat && a.Tone == b.Tone }) {
		t.Fatal("a speck changed a cell")
	}
}

// A speck of a solid falls, bounces off the floor with most of its speed
// gone (C86), comes to rest and goes, and never changes a cell.
func TestSpeckBouncesAndGoes(t *testing.T) {
	w, ids := newTestWorld(t, 64, 128, 1)
	w.Fill(0, 120, 64, 128, ids.stone)
	w.Step()
	cells := snapshot(w)
	w.Spray(Cell{Mat: ids.wood}, 30, 100, perSecond(5), perSecond(30), 3)
	if w.Particles() != 1 || w.particles[0].Flags != Speck|Bounces {
		t.Fatalf("Spray made %d particles, the first %+v; want a speck that bounces", w.Particles(), w.particles)
	}
	if l := w.particles[0].Life; l < speckLife || l >= speckLife+speckLifeSpread {
		t.Fatalf("a speck lives %d ticks, want 300 to 600", l)
	}
	bounced := false
	for tick := 0; w.Particles() > 0; tick++ {
		if tick > 700 {
			t.Fatal("the speck is still there after 700 ticks")
		}
		before := w.particles[0].VY
		w.Step()
		if w.Particles() > 0 && before > 0 && w.particles[0].VY < 0 {
			bounced = true
			if -w.particles[0].VY > (before+particleGravity)*3/10+1 {
				t.Fatalf("the speck bounced from %d to %d, keeping more than 30%%", before, w.particles[0].VY)
			}
		}
	}
	if !bounced {
		t.Fatal("the speck never bounced")
	}
	if got := snapshot(w); !slices.EqualFunc(got, cells, func(a, b Cell) bool { return a.Mat == b.Mat && a.Tone == b.Tone }) {
		t.Fatal("a speck changed a cell")
	}
}

// A speck of a liquid does not bounce; a speck goes when its life runs
// out.
func TestSpeckLifeRunsOut(t *testing.T) {
	w, ids := newTestWorld(t, 64, 1024, 1)
	w.Spray(Cell{Mat: ids.water}, 30, 10, 0, 0, 1)
	if w.particles[0].Flags != Speck {
		t.Fatalf("a speck of water has flags %#x, want only Speck", w.particles[0].Flags)
	}
	w.particles[0].Life = 5
	for range 5 {
		w.Step()
	}
	if w.Particles() != 1 {
		t.Fatal("the speck went before its life ran out")
	}
	w.Step()
	if w.Particles() != 0 {
		t.Fatal("the speck is still there after its life ran out")
	}
}

// The particles in a block leave with it, in their order; the others stay.
// Put back after the block comes back, they fly on as before.
func TestParticlesLeaveWithTheirBlock(t *testing.T) {
	w, ids := newTestWorld(t, 1024, 512, 1)
	for x := range 20 {
		w.Set(500+x, 300, ids.water)
	}
	for x := range 20 {
		w.Launch(500+x, 300, perSecond(10), perSecond(-30), uint64(x))
	}
	w.Flush()
	w.Spray(Cell{Mat: ids.wood}, 100, 100, 0, 0, 9)
	all := w.AppendParticles(nil)
	var want []Particle
	for _, p := range all {
		if x, _ := p.At(); x < BlockSize {
			want = append(want, p)
		}
	}
	got := w.TakeParticles(0, 0, nil)
	if !slices.Equal(got, want) || len(got) != 13 {
		t.Fatalf("took %d particles of block (0, 0), want the %d there in order", len(got), len(want))
	}
	if w.Particles() != len(all)-len(got) {
		t.Fatalf("%d particles stay, want %d", w.Particles(), len(all)-len(got))
	}
	blk, err := w.UnloadBlock(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Relaunch(got); err == nil {
		t.Fatal("Relaunch put particles in a block not in memory")
	}
	if err := w.LoadBlock(0, 0, blk); err != nil {
		t.Fatal(err)
	}
	if err := w.Relaunch(got); err != nil {
		t.Fatal(err)
	}
	if n := totalOf(w, ids.water); n != 20 {
		t.Fatalf("%d water after the round trip, want 20", n)
	}
	bad := got[0]
	bad.Flags |= Bounces
	if err := w.Relaunch([]Particle{bad}); err == nil {
		t.Fatal("Relaunch took a cell's particle that bounces")
	}
	bad = got[0]
	bad.Flags |= Pushed
	if err := w.Relaunch([]Particle{bad}); err == nil {
		t.Fatal("Relaunch took a cell's particle in the pool of a body's specks")
	}
	bad = got[0]
	bad.VY = maxSpeed + 1
	if err := w.Relaunch([]Particle{bad}); err == nil {
		t.Fatal("Relaunch took a particle faster than any")
	}
}

// Flying particles do not make a tick allocate.
func TestFlyingDoesNotAllocate(t *testing.T) {
	w, ids := newTestWorld(t, 256, 2048, 1)
	for x := 64; x < 192; x++ {
		w.Set(x, 1500, ids.water)
		w.Launch(x, 1500, perSecond(x%7-3), perSecond(-200), uint64(x))
	}
	w.Flush()
	if n := testing.AllocsPerRun(20, w.Step); n != 0 {
		t.Fatalf("Step with particles allocates %v times per tick", n)
	}
	if w.Particles() != 128 {
		t.Fatalf("%d particles fly, want all 128 still", w.Particles())
	}
}

// isqrtDigits is the square root rounded down, a binary digit at a time:
// the first isqrt, slower in the particles' loop, kept as an oracle.
func isqrtDigits(v uint64) uint64 {
	var r uint64
	bit := uint64(1) << 62
	for bit > v {
		bit >>= 2
	}
	for bit != 0 {
		if v >= r+bit {
			v -= r + bit
			r = r>>1 + bit
		} else {
			r >>= 1
		}
		bit >>= 2
	}
	return r
}

func TestIsqrt(t *testing.T) {
	s := rng.New(5)
	vals := []uint64{0, 1, 2, 3, 4, 15, 16, 17, 1 << 40, 1<<40 - 1, 1<<62 + 12345, 1<<64 - 1}
	for range 100000 {
		vals = append(vals, s.Uint64()>>s.IntN(64))
	}
	for _, v := range vals {
		if got, want := isqrt(v), isqrtDigits(v); got != want {
			t.Fatalf("isqrt(%d) = %d, want %d", v, got, want)
		}
	}
}

// BenchmarkParticles moves 10,000 particles one tick: water thrown up out
// of a deep pool, passing up through it, as a fast body's splash is. It
// reports the cost of a tick of particles alone, without the cells' tick.
func BenchmarkParticles(b *testing.B) {
	w, ids := newTestWorld(b, 1024, 1024, 1)
	w.Fill(0, 700, 1024, 1024, ids.water)
	for i := range 10000 {
		x, y := 12+i%1000, 900+i/1000*10
		w.Launch(x, y, perSecond(i%61-30), perSecond(-150-i%100), uint64(i))
	}
	w.Flush()
	saved := w.AppendParticles(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.particles = append(w.particles[:0], saved...)
		w.fresh = w.fresh[:0]
		w.moveParticles()
	}
}

// Particles that come back with their block keep the limit too: past it a
// cell's particle hands its cell to the queue, where it is, and a speck
// is dropped (Codex review).
func TestRelaunchKeepsTheLimit(t *testing.T) {
	w, ids := newTestWorld(t, 1024, 512, 1)
	for x := range 10 {
		w.Set(600+x, 300, ids.water)
		w.Launch(600+x, 300, 0, perSecond(-30), uint64(x))
	}
	w.Flush()
	w.Spray(Cell{Mat: ids.wood}, 620, 300, 0, 0, 1)
	gone := w.TakeParticles(1, 0, nil)
	if len(gone) != 11 {
		t.Fatalf("took %d particles, want 11", len(gone))
	}
	for w.pooled[cellPool] < maxParticles-4 {
		w.particles = append(w.particles, Particle{X: 1 << particleShift, Y: 1 << particleShift, Cell: Cell{Mat: ids.wood}, Life: 10, Flags: Speck})
		w.pooled[cellPool]++
	}
	if err := w.Relaunch(gone); err != nil {
		t.Fatal(err)
	}
	if w.Particles() != maxParticles {
		t.Fatalf("%d particles fly after the block came back, want the limit %d", w.Particles(), maxParticles)
	}
	if n := totalOf(w, ids.water); n != 10 {
		t.Fatalf("%d water in the grid, queue and particles, want 10", n)
	}
	if w.QueueLen() != 6 {
		t.Fatalf("%d cells wait in the queue, want the 6 past the limit", w.QueueLen())
	}
}
