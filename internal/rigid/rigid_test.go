package rigid

import (
	"math"
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
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
			if c.Mat != id.wood || body != 0 || int(pixel) != k || c.Tone != b.tone[k] {
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
		if ok, err := s.Drop(id.wood, at[0], at[1], 10, 10); ok || err != nil {
			t.Errorf("Drop at %v: %v, %v; want refused", at, ok, err)
		}
	}
	if _, err := s.Drop(id.water, 10, 10, 4, 4); err == nil {
		t.Error("Drop made a body of water")
	}
	if _, err := s.Drop(id.wood, 10, 10, 0, 4); err == nil {
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

// A turned body maps each pixel's center through its transform and rounds
// down, as Noita does: at 45° some pixels meet in one cell, and the grid
// holds fewer cells than the image has pixels. The float32 mapping agrees
// with float64 except where a center lies on a cell's edge.
func TestTurnedBodyMapsPixelCenters(t *testing.T) {
	for _, deg := range []float32{0, 10, 30, 45, 60, 135, 180} {
		s, w, id := newSystem(t, 100, 100, 1)
		if err := s.add(id.wood, 40, 40, 17, 11, deg*math.Pi/180); err != nil {
			t.Fatal(err)
		}
		b := &s.bodies[0]
		b.p, b.q = s.phys.Transform(b.id)
		s.write(b, 0)
		n, _ := owned(w, id.wood, 0)
		placed := 0
		cellsHit := map[[2]int32]bool{}
		for k, on := range b.placed {
			if !on {
				t.Fatalf("%v°: pixel %d over air was not placed", deg, k)
			}
			placed++
			cellsHit[[2]int32{b.x[k], b.y[k]}] = true
			i, j := k%17, k/17
			u, v := float64(i)+0.5-8.5, float64(j)+0.5-5.5
			fx := float64(b.p.X)*10 + float64(b.q.C)*u - float64(b.q.S)*v
			fy := float64(b.p.Y)*10 + float64(b.q.S)*u + float64(b.q.C)*v
			near := math.Abs(fx-math.Round(fx)) < 1e-3 || math.Abs(fy-math.Round(fy)) < 1e-3
			if !near && (int32(math.Floor(fx)) != b.x[k] || int32(math.Floor(fy)) != b.y[k]) {
				t.Fatalf("%v°: pixel %d went to (%d, %d), float64 says (%v, %v)", deg, k, b.x[k], b.y[k], fx, fy)
			}
		}
		if n != len(cellsHit) || placed != 17*11 {
			t.Fatalf("%v°: %d cells in the grid, %d distinct cells mapped, %d placed", deg, n, len(cellsHit), placed)
		}
		switch deg {
		case 0, 180:
			if n != 17*11 {
				t.Errorf("%v°: %d cells, want all %d", deg, n, 17*11)
			}
		case 45:
			// Noita's 17×11 box covers 152 cells at 45° (C134).
			if n < 140 || n > 165 {
				t.Errorf("45°: %d cells, want about 150", n)
			}
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

// A body dropped into water sinks (there is no buoyancy yet), pushing the
// water it meets into the queue, which puts it back behind the body. The
// water in the grid plus in the queue never changes.
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
	s.erase(b, 1)
	if !a.force {
		t.Fatal("the first body was not marked to be written again")
	}
	s.write(b, 1)
	s.write(a, 0)
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
	allocs := testing.AllocsPerRun(100, func() {
		s.Write()
		if err := s.Collide(); err != nil {
			t.Fatal(err)
		}
		w.Step()
		s.Step()
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
