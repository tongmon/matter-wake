package rigid

import (
	"math"
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

// place adds a body of m, w × h pixels with its top-left pixel at cell
// (x, y), turned by angle radians, and writes it where it is, as Write
// does after a Step that left it in place. It returns the body's slot.
func place(tb testing.TB, s *System, m material.ID, x, y, w, h int, angle float32) int {
	tb.Helper()
	if err := s.add(m, x, y, w, h, angle); err != nil {
		tb.Fatal(err)
	}
	s.Write()
	return len(s.bodies) - 1
}

// breakOff digs out at once every cell listed, as (x, y), and breaks the
// bodies they belonged to. It fails the test unless each cell holds a
// solid; two cells of a turned body may show one pixel, which the first
// digs out. It returns how many pixels each material lost to the
// inventory as debris, besides those dug.
func breakOff(tb testing.TB, s *System, at ...[2]int) []int {
	tb.Helper()
	for _, c := range at {
		if cell, ok := s.w.At(c[0], c[1]); !ok || s.kinds[cell.Mat] != material.Solid {
			tb.Fatalf("cell (%d, %d) holds no body pixel to dig: %+v", c[0], c[1], cell)
		}
		s.Wear(c[0], c[1], 1<<30)
	}
	debris := make([]int, testMaterials)
	if err := s.Break(debris); err != nil {
		tb.Fatal(err)
	}
	return debris
}

// column returns the cells (x, y0) to (x, y1-1).
func column(x, y0, y1 int) [][2]int {
	var c [][2]int
	for y := y0; y < y1; y++ {
		c = append(c, [2]int{x, y})
	}
	return c
}

// Digging a body's cell wears its pixel down like terrain: the damage
// shows in the cell, stays with the pixel when the body moves, and a pixel
// worn through leaves the image and its cell. A pixel worn from two cells
// in one tick wears once.
func TestDiggingWearsABodyLikeTerrain(t *testing.T) {
	s, w, id := newSystem(t, 128, 64, 1)
	slot := place(t, s, id.wood, 20, 20, 10, 4, 0)
	b := &s.bodies[slot]
	if _, ok := s.Wear(22, 21, 2000); ok {
		t.Fatal("2000 of 5000 hp wore the pixel through")
	}
	c, _ := w.At(22, 21)
	want := uint16((2000*world.DamageScale + 4999) / 5000)
	if c.Damage != want {
		t.Fatalf("the cell shows damage %d, want %d", c.Damage, want)
	}
	if _, ok := s.Wear(22, 21, 2000); ok {
		t.Fatal("the same pixel wore twice in one tick")
	}
	if c, _ := w.At(22, 21); c.Damage != want {
		t.Fatalf("a second wear in the tick changed the damage to %d", c.Damage)
	}
	// The pixel keeps its damage when the body moves 3 cells right.
	b.p.X += 0.3
	s.sync()
	if c, _ := w.At(25, 21); c.Damage != want {
		t.Fatalf("after the body moved its pixel shows damage %d, want %d", c.Damage, want)
	}
	if err := s.Break(make([]int, testMaterials)); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Wear(25, 21, 2000); ok {
		t.Fatal("4000 of 5000 hp wore the pixel through")
	}
	if err := s.Break(make([]int, testMaterials)); err != nil {
		t.Fatal(err)
	}
	m, ok := s.Wear(25, 21, 2000)
	if !ok || m != id.wood {
		t.Fatalf("6000 of 5000 hp left the pixel: %v %v", m, ok)
	}
	if c, _ := w.At(25, 21); c.Mat != material.Air {
		t.Fatalf("the dug cell holds %+v, want air", c)
	}
	n := make([]int, testMaterials)
	s.Pixels(n)
	if n[id.wood] != 39 {
		t.Fatalf("the image holds %d pixels after one was dug, want 39", n[id.wood])
	}
	if _, ok := s.Wear(5, 5, 2000); ok {
		t.Fatal("Wear wore a cell that holds no body")
	}
}

// Digging a column through a plank splits it in two bodies where it was:
// the larger keeps the plank's slot, key and Box2D body, and the smaller
// is a new body with a later key. Every pixel stays in its cell.
func TestDugPlankSplits(t *testing.T) {
	s, w, id := newSystem(t, 128, 64, 1)
	place(t, s, id.wood, 20, 20, 30, 6, 0)
	old := s.bodies[0]
	debris := breakOff(t, s, column(32, 20, 26)...)
	if len(s.bodies) != 2 {
		t.Fatalf("the plank broke into %d bodies, want 2", len(s.bodies))
	}
	if !slices.Equal(debris, make([]int, testMaterials)) {
		t.Fatalf("debris %v, want none", debris)
	}
	keep, piece := &s.bodies[0], &s.bodies[1]
	if keep.id != old.id || keep.key != old.key || piece.key <= keep.key {
		t.Fatalf("keys %d and %d, Box2D bodies %x and %x; want the plank's %d and %x kept by the first",
			keep.key, piece.key, keep.id, piece.id, old.key, old.id)
	}
	if keep.n != 17*6 || piece.n != 12*6 {
		t.Fatalf("the pieces hold %d and %d pixels, want 102 and 72", keep.n, piece.n)
	}
	s.Write()
	if n, r := owned(w, id.wood, 0); n != 102 || r != (rect{33, 20, 50, 26}) {
		t.Fatalf("the kept piece covers %d cells in %v, want 102 in [33, 50) × [20, 26)", n, r)
	}
	if n, r := owned(w, id.wood, 1); n != 72 || r != (rect{20, 20, 32, 26}) {
		t.Fatalf("the new piece covers %d cells in %v, want 72 in [20, 32) × [20, 26)", n, r)
	}
}

// A turned body that splits writes every pixel left in the cells it was
// in: the pieces share the body's frame, and a cell samples the same
// pixel through either.
func TestTurnedBodySplitsInPlace(t *testing.T) {
	s, w, id := newSystem(t, 128, 96, 1)
	place(t, s, id.wood, 30, 30, 40, 9, 0.5)
	before := map[[2]int]uint8{}
	for y := range 96 {
		for x := range 128 {
			if c, _ := w.At(x, y); c.Mat == id.wood {
				before[[2]int{x, y}] = c.Tone
			}
		}
	}
	// Dig the cells of a band across the body's middle.
	var cut [][2]int
	inCut := map[[2]int]bool{}
	for at := range before {
		if fi, _ := imageAt(&s.bodies[0], at[0], at[1]); fi >= 19 && fi < 21 {
			cut = append(cut, at)
			inCut[at] = true
		}
	}
	slices.SortFunc(cut, func(a, b [2]int) int { return (a[1]-b[1])*1000 + a[0] - b[0] })
	breakOff(t, s, cut...)
	if len(s.bodies) != 2 {
		t.Fatalf("the body broke into %d bodies, want 2", len(s.bodies))
	}
	s.Write()
	for at, tone := range before {
		if inCut[at] {
			continue
		}
		if c, _ := w.At(at[0], at[1]); c.Mat != id.wood || c.Tone != tone {
			t.Fatalf("cell %v held a pixel of tone %d, now %+v", at, tone, c)
		}
	}
}

// Pixels that touch only at a corner are not one piece (C77).
func TestCornersDoNotConnect(t *testing.T) {
	s, _, id := newSystem(t, 64, 64, 1)
	place(t, s, id.wood, 10, 10, 6, 6, 0)
	var cut [][2]int
	for y := range 3 {
		for x := range 3 {
			cut = append(cut, [2]int{13 + x, 10 + y}, [2]int{10 + x, 13 + y})
		}
	}
	breakOff(t, s, cut...)
	if len(s.bodies) != 2 || s.bodies[0].n != 9 || s.bodies[1].n != 9 {
		t.Fatalf("two 3×3 blocks touching at a corner make %d bodies", len(s.bodies))
	}
	// Of two pieces of one size, the one found first in row order keeps
	// the body: the top-left block.
	if s.bodies[0].cx != 0 || s.bodies[0].cy != 0 || s.bodies[1].cx != 3 || s.bodies[1].cy != 3 {
		t.Fatalf("the kept piece starts at pixel (%d, %d) of the image, the new one at (%d, %d)",
			s.bodies[0].cx, s.bodies[0].cy, s.bodies[1].cx, s.bodies[1].cy)
	}
}

// A piece smaller than minPiece pixels is no body: its pixels go to the
// inventory with what was dug.
func TestTinyPiecesGoToTheInventory(t *testing.T) {
	s, w, id := newSystem(t, 64, 64, 1)
	place(t, s, id.wood, 10, 10, 10, 3, 0)
	cut := append(column(11, 10, 13), [2]int{10, 10}, [2]int{10, 12})
	debris := breakOff(t, s, cut...)
	if len(s.bodies) != 1 || s.bodies[0].n != 24 {
		t.Fatalf("%d bodies, the first of %d pixels; want one of 24", len(s.bodies), s.bodies[0].n)
	}
	if debris[id.wood] != 1 {
		t.Fatalf("debris %v, want the lone pixel", debris)
	}
	s.Write()
	if c, _ := w.At(10, 11); c.Mat != material.Air {
		t.Fatalf("the lone pixel's cell holds %+v, want air", c)
	}
}

// A body dug away is gone, and the bodies after it move down a slot with
// their cells.
func TestDugAwayBodyIsGone(t *testing.T) {
	s, w, id := newSystem(t, 64, 64, 1)
	place(t, s, id.wood, 10, 10, 2, 2, 0)
	place(t, s, id.wood, 30, 10, 4, 3, 0)
	key := s.bodies[1].key
	breakOff(t, s, [2]int{10, 10}, [2]int{11, 10}, [2]int{10, 11}, [2]int{11, 11})
	if len(s.bodies) != 1 || s.bodies[0].key != key {
		t.Fatalf("%d bodies left, want the second alone", len(s.bodies))
	}
	if n, r := owned(w, id.wood, 0); n != 12 || r != (rect{30, 10, 34, 13}) {
		t.Fatalf("slot 0 owns %d cells in %v, want the second body's 12", n, r)
	}
	s.Write()
	if n, _ := owned(w, id.wood, 0); n != 12 {
		t.Fatalf("after a Write slot 0 owns %d cells", n)
	}
}

// The piece that keeps the body moves on as the body's frame did: its
// center of mass moved by Δc, so it takes v + ω × Δc. A new piece starts
// with the body's v and ω as they were, awake (C35).
func TestPiecesKeepTheParentsMotion(t *testing.T) {
	s, _, id := newSystem(t, 128, 64, 1)
	place(t, s, id.wood, 20, 20, 30, 6, 0)
	b := &s.bodies[0]
	v, spin := physics.Vec2{X: 1, Y: -2}, float32(3)
	s.phys.SetVelocity(b.id, v, spin)
	c0 := s.phys.LocalCenter(b.id)
	breakOff(t, s, column(32, 20, 26)...)
	keep, piece := &s.bodies[0], &s.bodies[1]
	c1 := s.phys.LocalCenter(keep.id)
	dc := physics.Vec2{X: c1.X - c0.X, Y: c1.Y - c0.Y}
	kv, kw := s.phys.Velocity(keep.id)
	if !near32(kv.X, v.X-spin*dc.Y, 1e-5) || !near32(kv.Y, v.Y+spin*dc.X, 1e-5) || kw != spin {
		t.Fatalf("the kept piece moves at %v spinning %v; Δc %v, want %v + %v × Δc", kv, kw, dc, v, spin)
	}
	if pv, pw := s.phys.Velocity(piece.id); pv != v || pw != spin {
		t.Fatalf("the new piece moves at %v spinning %v, want %v and %v", pv, pw, v, spin)
	}
	if !s.phys.Awake(keep.id) || !s.phys.Awake(piece.id) {
		t.Fatal("a piece is asleep")
	}
	// Both pieces are where the body was.
	p0, q0 := s.phys.Transform(keep.id)
	p1, q1 := s.phys.Transform(piece.id)
	if p0 != p1 || q0 != q1 {
		t.Fatalf("the pieces' frames differ: %v %v and %v %v", p0, q0, p1, q1)
	}
}

func near32(a, b, tol float32) bool {
	return math.Abs(float64(a-b)) <= float64(tol)
}

// A piece's mass, center of mass and inertia are its polygons', not its
// pixels' (C34), and the body keeps them.
func TestMassComesFromThePolygons(t *testing.T) {
	s, _, id := newSystem(t, 128, 64, 1)
	place(t, s, id.wood, 20, 20, 30, 6, 0)
	var notch [][2]int
	for x := 32; x < 35; x++ {
		notch = append(notch, column(x, 20, 23)...)
	}
	breakOff(t, s, notch...)
	b := &s.bodies[0]
	if b.mass != s.phys.Mass(b.id) || b.inertia != s.phys.Inertia(b.id) || b.lc != s.phys.LocalCenter(b.id) {
		t.Fatalf("the body keeps mass %v, inertia %v, center %v; Box2D has %v, %v, %v",
			b.mass, b.inertia, b.lc, s.phys.Mass(b.id), s.phys.Inertia(b.id), s.phys.LocalCenter(b.id))
	}
	// 30 × 6 less a 3 × 3 notch: rectangles, so the polygons' area is the
	// pixels'. Wood weighs 0.6 kg/m², a pixel 0.01 m².
	if !near32(b.mass, 171*0.006, 1e-5) {
		t.Fatalf("the notched plank weighs %v, want %v", b.mass, 171*0.006)
	}
	// The notch is on the top: the center of mass sinks below the frame's
	// origin, the plank's middle.
	if b.lc.Y <= 0 {
		t.Fatalf("the notched plank's center of mass is %v, want below its middle", b.lc)
	}
}

// A new piece drops the pixels whose cells hold static terrain, outside
// the world or another body's pixel: what Noita's clean_overlapping_pixels
// does (C79). They go to the inventory. The kept piece keeps its own.
func TestDetachedPieceDropsPixelsInStone(t *testing.T) {
	s, w, id := newSystem(t, 128, 64, 1)
	if err := s.add(id.wood, 20, 20, 20, 3, 0); err != nil {
		t.Fatal(err)
	}
	w.Set(22, 21, id.stone)
	w.Set(30, 21, id.stone)
	s.Write()
	debris := breakOff(t, s, column(25, 20, 23)...)
	if len(s.bodies) != 2 {
		t.Fatalf("%d bodies, want 2", len(s.bodies))
	}
	if s.bodies[0].n != 42 || s.bodies[1].n != 14 || debris[id.wood] != 1 {
		t.Fatalf("pieces of %d and %d pixels, debris %v; want 42, 14 and the pixel in stone", s.bodies[0].n, s.bodies[1].n, debris)
	}
}

// The liquid a moving piece pushes away drags it about its own center of
// mass, not the origin of the frame it shares with the body it broke from.
func TestPieceDragsAboutItsCenterOfMass(t *testing.T) {
	s, w, id := newSystem(t, 128, 64, 1)
	w.Fill(0, 10, 128, 60, id.water)
	place(t, s, id.wood, 20, 20, 40, 4, 0)
	breakOff(t, s, column(29, 20, 24)...)
	piece := &s.bodies[1]
	if piece.n != 36 {
		t.Fatalf("the left piece holds %d pixels, want 36", piece.n)
	}
	s.Write()
	piece.p.Y += 0.2
	s.sync()
	if piece.dragN == 0 {
		t.Fatal("the piece pushed no water away")
	}
	if mx := piece.dragX / piece.dragN; math.Abs(float64(mx)) > 0.01 {
		t.Fatalf("the water it pushed away is %v cells across from its center of mass on average, want 0", mx)
	}
}

// Buoyancy looks under a piece's own center of mass.
func TestPieceFloatsOnLiquidUnderItsCenterOfMass(t *testing.T) {
	s, w, id := newSystem(t, 128, 64, 1)
	place(t, s, id.wood, 20, 20, 40, 4, 0)
	breakOff(t, s, column(29, 20, 24)...)
	s.Write()
	// The row just under both pieces' boxes: the rectangles' edges round
	// a hair into row 24.
	w.Fill(20, 24, 29, 26, id.water)
	if !s.overLiquid(&s.bodies[1]) {
		t.Fatal("the left piece does not feel the water under it")
	}
	if s.overLiquid(&s.bodies[0]) {
		t.Fatal("the right piece feels water that is not under it")
	}
}

// For every material, the cells in the world and the queue plus what was
// dug and broken off plus the bodies' pixels stay the same while bodies
// fall, are dug apart and fall on in water and sand.
func TestBreakingConservesCells(t *testing.T) {
	s, w, id := newSystem(t, 256, 128, 2)
	w.Fill(0, 120, 256, 128, id.stone)
	w.Fill(0, 90, 128, 120, id.water)
	w.Fill(150, 70, 230, 120, id.sand)
	drop(t, s, id.wood, 40, 20, 40, 8)
	drop(t, s, id.wood, 160, 20, 30, 10)
	held := make([]int, testMaterials)
	count := func() []int {
		n := cells(w)
		n[material.Air], n[id.wood] = 0, 0
		s.Pixels(n)
		for m, k := range held {
			n[m] += k
		}
		return n
	}
	start := count()
	for k := range 240 {
		if k >= 60 && k < 120 && k%6 == 0 {
			// Dig a column through each body, one cell a time, where it
			// is now.
			for i := range s.bodies {
				b := &s.bodies[i]
				x := int(b.box.x0+b.box.x1) / 2
				for y := b.box.y0; y < b.box.y1; y++ {
					if c, ok := w.At(x, y); ok && c.Mat == id.wood {
						if m, dug := s.Wear(x, y, 1<<30); dug {
							held[m]++
						}
					}
				}
			}
		}
		if err := s.Break(held); err != nil {
			t.Fatal(err)
		}
		tick(t, s, w)
		if got := count(); !slices.Equal(got, start) {
			t.Fatalf("after tick %d the counts are %v, want %v", k, got, start)
		}
	}
	if len(s.bodies) <= 2 {
		t.Fatalf("%d bodies at the end; the digging broke none", len(s.bodies))
	}
}

// Breaking bodies apart runs the same for any number of workers.
func TestBreakingRunsTheSame(t *testing.T) {
	run := func(workers int) (uint64, []physics.Vec2) {
		s, w, id := newSystem(t, 256, 160, workers)
		w.Fill(0, 150, 256, 160, id.stone)
		w.Fill(0, 110, 256, 150, id.water)
		w.Fill(150, 60, 220, 110, id.sand)
		for k, x := range []int{30, 100, 170} {
			drop(t, s, id.wood, x, 10+k*8, 24+4*k, 10)
		}
		held := make([]int, testMaterials)
		for k := range 300 {
			if k == 40 || k == 90 {
				for i := range s.bodies {
					b := &s.bodies[i]
					for y := b.box.y0; y < b.box.y1; y++ {
						for _, x := range []int{b.box.x0 + 5, b.box.x0 + 9} {
							if c, ok := w.At(x, y); ok && c.Mat == id.wood {
								if m, dug := s.Wear(x, y, 1<<30); dug {
									held[m]++
								}
							}
						}
					}
				}
			}
			if err := s.Break(held); err != nil {
				t.Fatal(err)
			}
			tick(t, s, w)
		}
		at := make([]physics.Vec2, len(s.bodies))
		for k := range at {
			at[k], _ = s.phys.Transform(s.bodies[k].id)
		}
		return w.Checksum(), at
	}
	sum1, at1 := run(1)
	sum4, at4 := run(4)
	if len(at1) <= 3 {
		t.Fatalf("%d bodies; the digging broke none", len(at1))
	}
	if sum1 != sum4 || !slices.Equal(at1, at4) {
		t.Fatalf("1 worker %016x %v, 4 workers %016x %v", sum1, at1, sum4, at4)
	}
}

// Sand that settles in a notch dug into a body is terrain: it reaches into
// the rectangle of the body's image but not into a pixel the body still
// has, so nothing pushes the body out of it, and a body dropped onto it
// rests on it (Codex review).
func TestSandInANotchIsTerrain(t *testing.T) {
	s, w, id := newSystem(t, 128, 96, 1)
	w.Fill(0, 90, 128, 96, id.stone)
	place(t, s, id.wood, 40, 80, 30, 10, 0)
	var notch [][2]int
	for y := 80; y < 86; y++ {
		for x := 50; x < 60; x++ {
			notch = append(notch, [2]int{x, y})
		}
	}
	breakOff(t, s, notch...)
	s.Write()
	collide(t, s)
	w.Fill(50, 83, 60, 86, id.sand)
	for range world.SettleTicks + 5 {
		w.Step()
	}
	collide(t, s)
	for _, at := range [][2]int{{51, 85}, {55, 84}, {58, 83}} {
		if c := classAt(s, at[0], at[1]); c != world.Settled {
			t.Fatalf("settled sand at %v in the notch is %v to Box2D, want terrain", at, c)
		}
	}
	// Sand in the cells the body still covers stays out.
	if !s.bodies[0].reaches(45, 85) || s.bodies[0].reaches(55, 85) {
		t.Fatal("reaches does not follow the pixels the body has")
	}
}

// A turned body's pixel may show in two cells; wearing it through one
// shows the damage in both, though the body does not move and is not
// written again (Codex review).
func TestWornPixelShowsInEveryCell(t *testing.T) {
	s, w, id := newSystem(t, 128, 96, 1)
	place(t, s, id.wood, 40, 40, 20, 12, 0.6)
	where := map[uint16][][2]int{}
	for y := range 96 {
		for x := range 128 {
			if c, _ := w.At(x, y); c.Mat == id.wood {
				_, k := c.Body()
				where[k] = append(where[k], [2]int{x, y})
			}
		}
	}
	shown := 0
	for k, at := range where {
		if len(at) < 2 {
			continue
		}
		shown++
		if _, ok := s.Wear(at[0][0], at[0][1], 2000); ok {
			t.Fatalf("2000 of 5000 hp wore pixel %d through", k)
		}
		for _, a := range at {
			if c, _ := w.At(a[0], a[1]); c.Damage == 0 {
				t.Fatalf("pixel %d worn in cell %v shows no damage in cell %v", k, at[0], a)
			}
		}
	}
	if shown == 0 {
		t.Fatal("no pixel of the turned body shows in two cells")
	}
	// Nothing moved, so the next Write leaves the cells as they are.
	s.Write()
	for k, at := range where {
		for _, a := range at {
			if c, _ := w.At(a[0], a[1]); (c.Damage != 0) != (len(at) > 1) {
				t.Fatalf("after a Write pixel %d shows damage %d in cell %v", k, c.Damage, a)
			}
		}
	}
}
