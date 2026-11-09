package rigid

import (
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// A dropped body's pixels go into the grid at the next Write, one cell
// each, linked to the body and the pixel, with the body's tones.
func TestDropWritesTheBody(t *testing.T) {
	s, w, id := newSystem(t, 200, 100, 1)
	drop(t, s, id.wood, 40, 20, 16, 12)
	s.Write()
	b := &s.bodies[0]
	for j := range 12 {
		for i := range 16 {
			c, _ := w.At(40+i, 20+j)
			body, pixel := c.Body()
			k := j*16 + i
			if c.Mat != id.wood || body != 0 || int(pixel) != k || c.Tone != b.px[k].tone {
				t.Fatalf("(%d, %d) is %+v (body %d, pixel %d), want pixel %d of body 0", 40+i, 20+j, c, body, pixel, k)
			}
		}
	}
	if n, _ := owned(w, id.wood, 0); n != 16*12 {
		t.Fatalf("%d cells hold the body, want %d", n, 16*12)
	}
	n := make([]int, testMaterials)
	s.Pixels(n)
	if n[id.wood] != 16*12 || s.Bodies() != 1 {
		t.Fatalf("Pixels counts %v and Bodies %d", n, s.Bodies())
	}
}

// Drop refuses a place that holds what the body cannot push away, and
// bodies that are not solid or have no pixels.
func TestDropRefuses(t *testing.T) {
	s, w, id := newSystem(t, 100, 60, 1)
	w.Fill(0, 50, 100, 60, id.stone)
	w.Fill(0, 40, 30, 50, id.sand)
	drop(t, s, id.wood, 60, 10, 10, 10)
	s.Write() // a body is in the grid from the next Write on
	for _, at := range [][2]int{{40, 45}, {10, 35}, {55, 5}, {-5, 10}, {95, 10}} {
		if ok, err := s.Drop(id.wood, at[0], at[1], 10, 10, 0, 0, 0); ok || err != nil {
			t.Errorf("Drop at %v: %v, %v; want refused", at, ok, err)
		}
	}
	if _, err := s.Drop(id.water, 10, 10, 4, 4, 0, 0, 0); err == nil {
		t.Error("Drop made a body of water")
	}
	if _, err := s.Drop(id.wood, 10, 10, 0, 4, 0, 0, 0); err == nil {
		t.Error("Drop made a body of no pixels")
	}
	if s.Bodies() != 1 {
		t.Fatalf("%d bodies, want the first one only", s.Bodies())
	}
}

// A body dropped on stone falls, comes to rest on it, unturned, with its
// bottom row on the row above the floor, and falls asleep. A sleeping
// body's cells are left alone.
func TestBoxRestsOnStone(t *testing.T) {
	s, w, id := newSystem(t, 200, 100, 1)
	w.Fill(0, 90, 200, 100, id.stone)
	drop(t, s, id.wood, 40, 20, 16, 12)
	for range 300 {
		tick(t, s, w)
	}
	n, r := owned(w, id.wood, 0)
	if want := (rect{40, 78, 56, 90}); n != 16*12 || r != want {
		t.Fatalf("the body covers %d cells in %v, want %d in %v", n, r, 16*12, want)
	}
	if s.phys.Awake(s.bodies[0].id) {
		t.Fatal("the body is still awake after 5 seconds at rest")
	}
	v := w.Version()
	s.Write()
	if err := s.Collide(); err != nil {
		t.Fatal(err)
	}
	if w.Version() != v {
		t.Fatal("writing a sleeping body changed the world")
	}
}

// A turned body samples its image at the center of every cell: each cell
// whose center lies inside the body's rectangle holds the pixel the center
// falls in, and no cell outside it holds one. So the body has no holes at
// any angle, for odd and even sides, wherever its center lies between
// cells. The float32 sampling agrees with float64 except where a center
// lies on a pixel's edge.
func TestTurnedBodyHasNoHoles(t *testing.T) {
	for _, size := range [][2]int{{16, 12}, {17, 11}, {40, 8}, {5, 3}} {
		s, w, id := newSystem(t, 128, 128, 1)
		if err := s.add(id.wood, 40, 40, size[0], size[1], 0); err != nil {
			t.Fatal(err)
		}
		b := &s.bodies[0]
		fewest, most := math.MaxInt, 0
		for step := range 720 {
			deg := float64(step) / 2
			setPose(b, 64+float64(step%7)*0.13, 64+float64(step%5)*0.17, deg)
			s.sync()
			n, _ := owned(w, id.wood, 0)
			fewest, most = min(fewest, n), max(most, n)
			reach := (b.w+b.h)/2 + 2
			for y := 64 - reach; y <= 64+reach; y++ {
				for x := 64 - reach; x <= 64+reach; x++ {
					c, _ := w.At(x, y)
					body, pixel := c.Body()
					in := c.Mat == id.wood && body == 0
					fi, fj := imageAt(b, x, y)
					inside := fi > edge && fi < float64(b.w)-edge && fj > edge && fj < float64(b.h)-edge
					outside := fi < -edge || fi > float64(b.w)+edge || fj < -edge || fj > float64(b.h)+edge
					switch {
					case inside && !in:
						t.Fatalf("%dx%d at %v°: (%d, %d) is inside the body at pixel (%.3f, %.3f) but holds %+v", b.w, b.h, deg, x, y, fi, fj, c)
					case outside && in:
						t.Fatalf("%dx%d at %v°: (%d, %d) is outside the body at (%.3f, %.3f) but holds pixel %d", b.w, b.h, deg, x, y, fi, fj, pixel)
					case inside && !onEdge(fi) && !onEdge(fj):
						if want := int(math.Floor(fj))*b.w + int(math.Floor(fi)); int(pixel) != want || c.Tone != b.px[want].tone {
							t.Fatalf("%dx%d at %v°: (%d, %d) holds pixel %d, want %d", b.w, b.h, deg, x, y, pixel, want)
						}
					}
				}
			}
		}
		// The area is all that sets the count, give or take the cells along
		// the edges.
		if area := b.w * b.h; fewest < area-(b.w+b.h) || most > area+b.w+b.h {
			t.Errorf("%dx%d covers %d to %d cells, want about %d", b.w, b.h, fewest, most, area)
		}
		t.Logf("%dx%d (%d pixels) covers %d to %d cells", b.w, b.h, b.w*b.h, fewest, most)
	}
}

// write scans each row only across the span the rectangle crosses. Over
// random bodies and poses, near the axes too, it writes the same cells as
// sampling every cell of the box around the body with the same float32
// arithmetic.
func TestRowSpansMissNoCell(t *testing.T) {
	s, w, id := newSystem(t, 256, 256, 1)
	r := rng.New(3)
	for n := range 2000 {
		bw, bh := 1+r.IntN(40), 1+r.IntN(40)
		if err := s.add(id.wood, 10, 10, bw, bh, 0); err != nil {
			t.Fatal(err)
		}
		b := &s.bodies[len(s.bodies)-1]
		deg := float64(r.IntN(3600)) / 10
		if n%2 == 0 {
			// Within a hair of a right angle.
			deg = float64(r.IntN(4)*90) + float64(r.IntN(201)-100)*1e-6
		}
		setPose(b, 128+float64(r.IntN(1000))/1000, 128+float64(r.IntN(1000))/1000, deg)
		slot := uint16(len(s.bodies) - 1)
		s.write(b, slot)
		want := sampleEveryCell(b)
		got := map[cellAt]uint16{}
		for y := range 256 {
			for x := range 256 {
				if c, _ := w.At(x, y); c.Mat == id.wood {
					if owner, pixel := c.Body(); owner == slot {
						got[cellAt{int32(x), int32(y)}] = pixel
					}
				}
			}
		}
		if !maps.Equal(got, want) {
			t.Fatalf("%dx%d at %v°: wrote %d cells, sampling every cell finds %d", bw, bh, deg, len(got), len(want))
		}
		for at := range want {
			if !b.box.overlaps(rect{int(at.x), int(at.y), int(at.x) + 1, int(at.y) + 1}) {
				t.Fatalf("%dx%d at %v°: (%d, %d) is outside the box %v", bw, bh, deg, at.x, at.y, b.box)
			}
		}
		s.erase(b, slot)
	}
}

// sampleEveryCell maps every cell of the box around b back into its image,
// as write does, and returns the pixel each cell inside it samples.
func sampleEveryCell(b *body) map[cellAt]uint16 {
	px, py := float32(b.p.X*cellsPerMeter), float32(b.p.Y*cellsPerMeter)
	ox, oy := int(math.Floor(float64(px))), int(math.Floor(float64(py)))
	fx, fy := px-float32(ox), py-float32(oy)
	co, si := b.q.C, b.q.S
	hw, hh := float32(b.w)/2, float32(b.h)/2
	reach := b.w + b.h
	cells := map[cellAt]uint16{}
	for r := -reach; r <= reach; r++ {
		dy := float32(r) + 0.5 - fy
		sv, cv := float32(si*dy), float32(co*dy)
		for k := -reach; k <= reach; k++ {
			dx := float32(k) + 0.5 - fx
			i := int(math.Floor(float64(float32(co*dx) + sv + hw)))
			j := int(math.Floor(float64(cv - float32(si*dx) + hh)))
			if i >= 0 && i < b.w && j >= 0 && j < b.h {
				cells[cellAt{int32(ox + k), int32(oy + r)}] = uint16(j*b.w + i)
			}
		}
	}
	return cells
}

// A body too small for any cell's center, one pixel turned 45° about a
// cell's corner, has no cells in the grid, but the terrain around it is
// still made collision.
func TestBodyWithoutCellsStillCollides(t *testing.T) {
	s, w, id := newSystem(t, 100, 100, 1)
	w.Fill(0, 52, 100, 100, id.stone)
	if err := s.add(id.wood, 49, 49, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	b := &s.bodies[0]
	setPose(b, 50, 50, 45)
	s.sync()
	if n, _ := owned(w, id.wood, 0); n != 0 {
		t.Fatalf("the body holds %d cells, want none", n)
	}
	if err := s.Collide(); err != nil {
		t.Fatal(err)
	}
	if !b.near.overlaps(rect{49, 49, 51, 51}) || !slices.Contains(s.wanted, 0) {
		t.Fatalf("the body near %v wants chunks %v, want its own", b.near, s.wanted)
	}
}

// Sand poured on a turned body that keeps rocking slightly stays on its
// surface: with no holes, no grain gets more than a cell inside its edge.
func TestSandStaysOutOfATurnedBody(t *testing.T) {
	for _, deg := range []float64{30, 45} {
		s, w, id := newSystem(t, 160, 160, 1)
		w.Fill(0, 150, 160, 160, id.stone)
		if err := s.add(id.wood, 68, 92, 24, 16, 0); err != nil {
			t.Fatal(err)
		}
		b := &s.bodies[0]
		for tick := range 600 {
			phase := 2 * math.Pi * float64(tick)
			setPose(b, 80+0.3*math.Sin(phase/70), 100, deg+1.5*math.Sin(phase/90))
			s.sync()
			if tick < 200 {
				for x := 70; x < 90; x += 2 {
					if c, _ := w.At(x, 2); c.Mat == material.Air {
						w.Set(x, 2, id.sand)
					}
				}
			}
			w.Step()
		}
		deep := 0
		for y := range 160 {
			for x := range 160 {
				if c, _ := w.At(x, y); c.Mat != id.sand {
					continue
				}
				if fi, fj := imageAt(b, x, y); fi > 1 && fi < float64(b.w)-1 && fj > 1 && fj < float64(b.h)-1 {
					deep++
				}
			}
		}
		if deep > 0 {
			t.Errorf("%v°: %d grains of sand are more than a cell inside the body", deg, deep)
		}
	}
}

// Static terrain keeps its cells: a body's pixels over it are not written,
// but the body's image keeps them.
func TestStaticKeepsItsCells(t *testing.T) {
	s, w, id := newSystem(t, 100, 100, 1)
	w.Fill(0, 50, 100, 100, id.stone)
	if err := s.add(id.wood, 20, 45, 10, 10, 0); err != nil {
		t.Fatal(err)
	}
	s.Write()
	if n, r := owned(w, id.wood, 0); n != 50 || r != (rect{20, 45, 30, 50}) {
		t.Fatalf("the body covers %d cells in %v, want the 50 above the stone", n, r)
	}
	n := make([]int, testMaterials)
	s.Pixels(n)
	if n[id.wood] != 100 {
		t.Fatalf("the image counts %d pixels, want 100", n[id.wood])
	}
	if c := cells(w); c[id.stone] != 100*50 {
		t.Fatalf("%d stone cells, want %d", c[id.stone], 100*50)
	}
}

// A wood body dropped into water sinks (its buoyancy, 0.7, is below 1),
// pushing the water it meets into the queue, which puts it back behind
// the body. The water in the grid plus in the queue never changes.
func TestBodyKeepsTheWater(t *testing.T) {
	s, w, id := newSystem(t, 128, 128, 1)
	w.Fill(0, 120, 128, 128, id.stone)
	w.Fill(0, 60, 128, 120, id.water)
	drop(t, s, id.wood, 50, 20, 16, 12)
	start := cells(w)[id.water]
	queued := 0
	for range 400 {
		tick(t, s, w)
		if got := cells(w)[id.water]; got != start {
			t.Fatalf("tick %d: %d water in the grid and queue, want %d", w.Tick(), got, start)
		}
		queued = max(queued, w.QueueLen())
	}
	if queued == 0 {
		t.Fatal("the body never pushed water into the queue")
	}
	if _, r := owned(w, id.wood, 0); r.y1 != 120 {
		t.Fatalf("the body ends at %v, want on the floor at row 120", r)
	}
}

// Settled powder gives way only to a body big and fast enough at that
// pixel: below 5 (m/s)² never, from 21 always, and in between with a
// chance that grows across the band.
func TestSettledSandGivesWayByPixelSpeed(t *testing.T) {
	s, _, id := newSystem(t, 64, 64, 1)
	if err := s.add(id.wood, 10, 10, 10, 10, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.add(id.wood, 30, 10, 6, 20, 0); err != nil {
		t.Fatal(err)
	}
	big, small := &s.bodies[0], &s.bodies[1]
	settled := world.Cell{Mat: id.sand, Rest: world.SettleTicks}
	young := world.Cell{Mat: id.sand, Rest: world.SettleTicks - 1}
	speed := func(b *body, v physics.Vec2, spin float32) {
		b.v, b.spin, b.haveV = v, spin, true
	}
	speed(big, physics.Vec2{X: 0, Y: 2}, 0)
	if s.givesWay(big, 0, settled, 0, 0) {
		t.Error("settled sand gave way at 4 (m/s)²")
	}
	speed(big, physics.Vec2{X: 0, Y: 5}, 0)
	if !s.givesWay(big, 0, settled, 0, 0) {
		t.Error("settled sand stood at 25 (m/s)²")
	}
	if s.givesWay(big, 0, young, 0, 0) {
		t.Error("sand that has not settled gave way")
	}
	speed(small, physics.Vec2{X: 0, Y: 5}, 0)
	if s.givesWay(small, 0, settled, 0, 0) {
		t.Error("settled sand gave way to a body 6 cells wide")
	}
	// Spinning, a pixel 10 cells (1 m) from the center moves at the spin.
	speed(big, physics.Vec2{}, 5)
	if !s.givesWay(big, 0, settled, 10, 0) || s.givesWay(big, 0, settled, 1, 0) {
		t.Error("the pixel speed does not follow the spin")
	}
	// At 9 (m/s)², a quarter of the way into the band.
	speed(big, physics.Vec2{X: 0, Y: 3}, 0)
	gave := 0
	for k := range 4000 {
		if s.givesWay(big, k, settled, 0, 0) {
			gave++
		}
	}
	if gave < 900 || gave > 1100 {
		t.Fatalf("at 9 (m/s)² settled sand gave way to %d of 4000 pixels, want about 1000", gave)
	}
}

// A body that covered another's pixels gives them back when it moves away:
// the covered body, though it did not move, is rewritten in the same tick.
func TestCoveredBodyComesBack(t *testing.T) {
	s, w, id := newSystem(t, 100, 100, 1)
	for _, x := range []int{20, 26} {
		if err := s.add(id.wood, x, 20, 10, 10, 0); err != nil {
			t.Fatal(err)
		}
	}
	a, b := &s.bodies[0], &s.bodies[1]
	for i := range s.bodies {
		s.bodies[i].p, s.bodies[i].q = s.phys.Transform(s.bodies[i].id)
	}
	s.write(a, 0)
	s.write(b, 1)
	if n, _ := owned(w, id.wood, 0); n != 60 {
		t.Fatalf("the first body holds %d cells, want 60: the second, with the higher key, takes the 40 they share", n)
	}
	// The first body is written again; the second keeps what it took.
	s.write(a, 0)
	if n, _ := owned(w, id.wood, 1); n != 100 {
		t.Fatalf("the second body holds %d cells after the first was written again, want 100", n)
	}
	// Move the second body away.
	b.p.X += 3
	s.sync()
	if !a.written || a.moved {
		t.Fatal("the first body moved")
	}
	if n, _ := owned(w, id.wood, 0); n != 100 {
		t.Fatalf("the first body holds %d cells after the second moved away, want all 100", n)
	}
}

// The same scene runs the same for any number of workers and every time.
func TestBodiesRunTheSame(t *testing.T) {
	run := func(workers int) (uint64, [3]physics.Vec2) {
		s, w, id := newSystem(t, 256, 160, workers)
		w.Fill(0, 150, 256, 160, id.stone)
		w.Fill(0, 110, 256, 150, id.water)
		w.Fill(150, 60, 220, 110, id.sand)
		for k, x := range []int{30, 100, 170} {
			drop(t, s, id.wood, x, 10+k*8, 12+4*k, 10)
		}
		var at [3]physics.Vec2
		for range 300 {
			tick(t, s, w)
		}
		for k := range at {
			at[k], _ = s.phys.Transform(s.bodies[k].id)
		}
		return w.Checksum(), at
	}
	sum1, at1 := run(1)
	sum4, at4 := run(4)
	again, atAgain := run(1)
	if sum1 != sum4 || at1 != at4 {
		t.Fatalf("1 worker %016x %v, 4 workers %016x %v", sum1, at1, sum4, at4)
	}
	if sum1 != again || at1 != atAgain {
		t.Fatalf("two runs differ: %016x %v and %016x %v", sum1, at1, again, atAgain)
	}
}

// Writing bodies and making their terrain collision allocates nothing once
// the terrain around them is built.
func TestTickDoesNotAllocate(t *testing.T) {
	s, w, id := newSystem(t, 128, 256, 1)
	w.Fill(0, 250, 128, 256, id.stone)
	w.Fill(0, 200, 128, 250, id.water)
	drop(t, s, id.wood, 50, 10, 16, 12)
	// Build the terrain everywhere first; afterwards the body only finds
	// chunks whose classes it must compare.
	for i := range s.terrain {
		if err := s.refresh(i); err != nil {
			t.Fatal(err)
		}
	}
	for range 10 {
		tick(t, s, w)
	}
	debris := make([]int, testMaterials)
	allocs := testing.AllocsPerRun(100, func() {
		if err := s.Break(debris); err != nil {
			t.Fatal(err)
		}
		s.Write()
		if err := s.Collide(); err != nil {
			t.Fatal(err)
		}
		w.Step()
		if err := s.Step(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("a tick allocates %v times", allocs)
	}
}

// Bodies never leave the world: its edges are walls.
func TestBodyStaysInTheWorld(t *testing.T) {
	s, w, id := newSystem(t, 64, 64, 1)
	drop(t, s, id.wood, 2, 2, 10, 10)
	for range 200 {
		tick(t, s, w)
	}
	n, r := owned(w, id.wood, 0)
	if n == 0 || r.x0 < 0 || r.y1 > 64 {
		t.Fatalf("the body covers %d cells in %v", n, r)
	}
	if r.y1 != 64 {
		t.Fatalf("the body rests at %v, want on the world's bottom edge", r)
	}
}

// Pixels counts every body's image, in the grid or not.
func TestPixelsCountsImages(t *testing.T) {
	s, w, id := newSystem(t, 100, 100, 1)
	drop(t, s, id.wood, 10, 10, 10, 10)
	drop(t, s, id.wood, 40, 10, 5, 3)
	n := make([]int, testMaterials)
	s.Pixels(n)
	if want := []int{0, 0, 0, 0, 115}; !slices.Equal(n, want) {
		t.Fatalf("Pixels %v, want %v", n, want)
	}
	if c := cells(w); c[material.Air] != 100*100 {
		t.Fatalf("Drop changed the grid before Write: %v", c)
	}
}

// A pixel covered by two later bodies in turn comes back once both have
// moved away, whichever moves first (Codex review).
func TestPixelCoveredTwiceComesBack(t *testing.T) {
	for _, first := range []int{1, 2} {
		s, w, id := newSystem(t, 100, 100, 1)
		for _, x := range []int{20, 24, 28} {
			if err := s.add(id.wood, x, 20, 10, 10, 0); err != nil {
				t.Fatal(err)
			}
		}
		for i := range s.bodies {
			s.bodies[i].p, s.bodies[i].q = s.phys.Transform(s.bodies[i].id)
		}
		s.sync()
		// Move the two later bodies away one at a time.
		for _, k := range []int{first, 3 - first} {
			s.bodies[k].p.Y += 3
			s.sync()
		}
		if n, _ := owned(w, id.wood, 0); n != 100 {
			t.Fatalf("moving body %d first, the first body holds %d cells after both moved away, want 100", first, n)
		}
	}
}
