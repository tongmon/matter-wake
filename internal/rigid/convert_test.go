package rigid

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// digCell digs terrain cell (x, y) out as the tool does: it becomes air,
// and s is told.
func digCell(tb testing.TB, s *System, x, y int) {
	tb.Helper()
	c, ok := s.w.At(x, y)
	if !ok || s.kinds[c.Mat] != material.Static {
		tb.Fatalf("cell (%d, %d) holds no terrain to dig: %+v", x, y, c)
	}
	s.w.Set(x, y, material.Air)
	s.Destroyed(x, y, c.Mat)
}

// convert runs Convert and fails the test on an error.
func convert(tb testing.TB, s *System) {
	tb.Helper()
	if err := s.Convert(); err != nil {
		tb.Fatal(err)
	}
}

// turnedBy returns what s has turned, by material.
func turnedBy(s *System) []int {
	n := make([]int, testMaterials)
	s.Turned(n)
	return n
}

// Digging a cell of ice turns the ice 4-connected to it, the whole block
// and not only what is near the cell, into one body of ice_body, static,
// as in Noita (C136): every cell keeps its place and tone, and holds a
// pixel of the body. The body stays where it is.
func TestDiggingIceMakesAStaticBody(t *testing.T) {
	s, w, id := newSystem(t, 200, 120, 1)
	c := coldIDs(s)
	w.Fill(0, 110, 200, 120, id.stone)
	w.Fill(60, 98, 100, 110, c.ice)
	before := grid(w)
	digCell(t, s, 80, 98)
	convert(t, s)
	if len(s.bodies) != 1 {
		t.Fatalf("%d bodies, want 1", len(s.bodies))
	}
	b := &s.bodies[0]
	if !b.static || b.mat != c.iceBody || b.n != 40*12-1 || b.w != 40 || b.h != 12 {
		t.Fatalf("the body is static %t, of material %d with %d pixels in %d×%d; want static ice_body, %d pixels in 40×12",
			b.static, b.mat, b.n, b.w, b.h, 40*12-1)
	}
	for y := 98; y < 110; y++ {
		for x := 60; x < 100; x++ {
			cell, _ := w.At(x, y)
			if x == 80 && y == 98 {
				if cell.Mat != material.Air {
					t.Fatalf("the dug cell holds %+v", cell)
				}
				continue
			}
			slot, _ := cell.Body()
			if cell.Mat != c.iceBody || slot != 0 || cell.Tone != before[y*w.Width()+x].Tone {
				t.Fatalf("cell (%d, %d) is %+v; want a pixel of body 0 with the ice's tone %d", x, y, cell, before[y*w.Width()+x].Tone)
			}
		}
	}
	if n := turnedBy(s); n[c.ice] != -(40*12-1) || n[c.iceBody] != 40*12-1 {
		t.Fatalf("turned %v; want %d ice into ice_body", n, 40*12-1)
	}
	after := grid(w)
	at, _ := s.phys.Transform(b.id)
	for range 120 {
		tick(t, s, w)
	}
	if !slices.Equal(grid(w), after) {
		t.Fatal("the static body's cells changed")
	}
	if p, _ := s.phys.Transform(s.bodies[0].id); p != at {
		t.Fatalf("the static body moved from %v to %v", at, p)
	}
}

// Ice in the air stays terrain until a cell of it is destroyed: having
// nothing under it does not turn it (C136). Once turned, the static body
// it becomes stays in the air too.
func TestFloatingIceStaysTerrain(t *testing.T) {
	s, w, _ := newSystem(t, 120, 120, 1)
	c := coldIDs(s)
	w.Fill(40, 20, 60, 30, c.ice)
	for range 120 {
		tick(t, s, w)
	}
	if len(s.bodies) != 0 || w.Count(c.ice) != 200 {
		t.Fatalf("%d bodies and %d ice cells after 2 s in the air; want none and 200", len(s.bodies), w.Count(c.ice))
	}
	digCell(t, s, 40, 20)
	convert(t, s)
	for range 120 {
		tick(t, s, w)
	}
	if len(s.bodies) != 1 || w.Count(c.iceBody) != 199 {
		t.Fatalf("%d bodies and %d ice_body cells; want 1 and 199", len(s.bodies), w.Count(c.iceBody))
	}
	if _, r := owned(w, c.iceBody, 0); r != (rect{40, 20, 60, 30}) {
		t.Fatalf("the static body covers %v, want where the ice was", r)
	}
}

// The cells' damage does not carry into the body: a cell turned into
// another material starts anew, as in Noita (C54).
func TestConvertStartsTheDamageAnew(t *testing.T) {
	s, w, _ := newSystem(t, 120, 120, 1)
	c := coldIDs(s)
	w.Fill(40, 20, 60, 30, c.ice)
	if w.Wear(45, 25, 10000) {
		t.Fatal("10000 of 30000 hp wore the ice through")
	}
	digCell(t, s, 40, 20)
	convert(t, s)
	if cell, _ := w.At(45, 25); cell.Mat != c.iceBody || cell.Damage != 0 {
		t.Fatalf("the worn cell turned into %+v; want ice_body without damage", cell)
	}
	for _, px := range s.bodies[0].px {
		if px.dmg != 0 {
			t.Fatal("a pixel of the new body has damage")
		}
	}
}

// A region wider than the window turns only within the window, centered
// on the cell it is filled from; the rest stays terrain, and turns when a
// cell of its own is dug. A window's square of 256² cells, the most a body
// holds, turns whole.
func TestConvertStopsAtTheWindow(t *testing.T) {
	s, w, _ := newSystem(t, 700, 400, 1)
	c := coldIDs(s)
	w.Fill(50, 50, 650, 60, c.ice)
	// Filled from (60, 51), below the dug cell: the window reaches x = 188.
	digCell(t, s, 60, 50)
	convert(t, s)
	if len(s.bodies) != 1 {
		t.Fatalf("%d bodies, want 1", len(s.bodies))
	}
	if n, r := owned(w, c.iceBody, 0); r != (rect{50, 50, 60 - convertWindow/2 + convertWindow, 60}) || n != 138*10-1 {
		t.Fatalf("the body covers %d cells in %v; want %d in [50, 188) × [50, 60)", n, r, 138*10-1)
	}
	if got := w.Count(c.ice); got != 600*10-138*10 {
		t.Fatalf("%d ice cells left, want %d", got, 600*10-138*10)
	}
	digCell(t, s, 400, 55)
	convert(t, s)
	if len(s.bodies) != 2 || s.bodies[1].w != convertWindow || s.bodies[1].h != 10 {
		t.Fatalf("after a dig in the middle: %d bodies, the second %d×%d; want 2, the second %d×10",
			len(s.bodies), s.bodies[1].w, s.bodies[1].h, convertWindow)
	}
	// A square block bigger than the window.
	w.Fill(0, 100, 700, 400, material.Air)
	w.Fill(40, 120, 340, 400, c.ice)
	digCell(t, s, 190, 260)
	convert(t, s)
	b := &s.bodies[len(s.bodies)-1]
	if b.w != convertWindow || b.h != convertWindow || b.n != MaxPixels-1 {
		t.Fatalf("the body of the square block is %d×%d with %d pixels; want %d² less the dug cell",
			b.w, b.h, b.n, convertWindow)
	}
}

// A region of fewer than minPiece cells stays terrain, as no body has a
// single pixel; two cells make a body.
func TestSmallRegionsStayTerrain(t *testing.T) {
	s, w, _ := newSystem(t, 120, 120, 1)
	c := coldIDs(s)
	w.Fill(10, 10, 12, 11, c.ice)
	digCell(t, s, 11, 10)
	convert(t, s)
	if len(s.bodies) != 0 || w.Count(c.ice) != 1 {
		t.Fatalf("a single ice cell: %d bodies, %d ice cells; want none and 1", len(s.bodies), w.Count(c.ice))
	}
	w.Fill(20, 10, 23, 11, c.ice)
	digCell(t, s, 22, 10)
	convert(t, s)
	if len(s.bodies) != 1 || s.bodies[0].n != 2 {
		t.Fatalf("two ice cells: %d bodies; want one of 2 pixels", len(s.bodies))
	}
}

// staticColumn turns a column of material m, 10 cells wide, of top rows,
// a row and bottom rows, with its top-left at (50, 20), into a static
// body, and returns the row between the parts.
func staticColumn(t *testing.T, s *System, m material.ID, top, bottom int) int {
	t.Helper()
	cut := 20 + top
	s.w.Fill(50, 20, 60, cut+1+bottom, m)
	// The cell dug to turn it, beside the column.
	s.w.Fill(49, 20, 50, 21, m)
	digCell(t, s, 49, 20)
	convert(t, s)
	if len(s.bodies) != 1 || !s.bodies[0].static {
		t.Fatalf("%d bodies; want one static", len(s.bodies))
	}
	return cut
}

// A static body cut in two keeps one piece static, the one its anchor
// picks, and the other becomes a dynamic body that falls (C136): ice
// keeps the piece that reaches highest, wood the one that reaches lowest,
// and a solid whose anchor is the largest the larger.
func TestStaticSplitKeepsItsAnchorPiece(t *testing.T) {
	for _, tt := range []struct {
		name        string
		mat         func(c cold) material.ID
		top, bottom int
		keepTop     bool
	}{
		{"ice, small top", func(c cold) material.ID { return c.ice }, 6, 20, true},
		{"ice, small bottom", func(c cold) material.ID { return c.ice }, 20, 6, true},
		{"wood, small top", func(c cold) material.ID { return c.trunk }, 6, 20, false},
		{"wood, small bottom", func(c cold) material.ID { return c.trunk }, 20, 6, false},
		{"largest, small top", func(c cold) material.ID { return c.rock }, 6, 20, false},
		{"largest, small bottom", func(c cold) material.ID { return c.rock }, 20, 6, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, w, _ := newSystem(t, 120, 120, 1)
			cut := staticColumn(t, s, tt.mat(coldIDs(s)), tt.top, tt.bottom)
			var row [][2]int
			for x := 50; x < 60; x++ {
				row = append(row, [2]int{x, cut})
			}
			breakOff(t, s, row...)
			if len(s.bodies) != 2 {
				t.Fatalf("%d bodies after the cut, want 2", len(s.bodies))
			}
			kept, fell := &s.bodies[0], &s.bodies[1]
			if !kept.static || fell.static || kept.key != 0 {
				t.Fatalf("the body that kept key %d is static %t and the new one static %t; want the first static only",
					kept.key, kept.static, fell.static)
			}
			s.Write()
			_, keptBox := owned(w, kept.mat, 0)
			if gotTop := keptBox.y0 == 20; gotTop != tt.keepTop {
				t.Fatalf("the static piece covers %v; the top piece should stay static: %t", keptBox, tt.keepTop)
			}
			at, _ := s.phys.Transform(kept.id)
			fellAt, _ := s.phys.Transform(fell.id)
			for range 30 {
				tick(t, s, w)
			}
			if p, _ := s.phys.Transform(s.bodies[0].id); p != at {
				t.Fatalf("the static piece moved from %v to %v", at, p)
			}
			if p, _ := s.phys.Transform(s.bodies[1].id); p.Y <= fellAt.Y {
				t.Fatalf("the dynamic piece went from %v to %v; it should fall", fellAt, p)
			}
		})
	}
}

// A body of glass that loses a pixel turns whole into glass powder, one
// cell for each pixel it has left (the developer's choice, 2026-11-10;
// Noita makes one for each pixel it had), where the pixels were, and is
// gone. Static or dynamic, turned or not, no cell is made or lost: powder
// that finds its cell taken waits in the queue.
func TestGlassShattersIntoPowder(t *testing.T) {
	s, w, id := newSystem(t, 200, 120, 1)
	c := coldIDs(s)
	w.Fill(0, 110, 200, 120, id.stone)
	w.Fill(40, 20, 70, 40, c.glass)
	digCell(t, s, 40, 20)
	convert(t, s)
	pane := s.bodies[0]
	if !pane.static || pane.n != 599 {
		t.Fatalf("the pane is static %t with %d pixels; want static with 599", pane.static, pane.n)
	}
	tones := map[[2]int]uint8{}
	for _, at := range pane.cells {
		cell, _ := w.At(int(at.x), int(at.y))
		tones[[2]int{int(at.x), int(at.y)}] = cell.Tone
	}
	debris := breakOff(t, s, [2]int{55, 30})
	if len(s.bodies) != 0 {
		t.Fatalf("%d bodies after the pane lost a pixel, want none", len(s.bodies))
	}
	if n := cells(w)[c.powder] + debris[c.powder]; n != 598 {
		t.Fatalf("the pane left %d cells of powder, want its 598 pixels", n)
	}
	for at, tone := range tones {
		if cell, _ := w.At(at[0], at[1]); at != [2]int{55, 30} && (cell.Mat != c.powder || cell.Tone != tone) {
			t.Fatalf("cell %v is %+v; want powder with the pixel's tone %d", at, cell, tone)
		}
	}
	if n := turnedBy(s); n[c.glass] != -599 || n[c.glassBody] != 1 || n[c.powder] != 598 {
		t.Fatalf("turned %v; want 599 glass into glass_body, 598 of it into powder", n)
	}
	// A thrown pane, turned so that some pixels share a cell.
	if ok, err := s.Drop(c.glassBody, 120, 30, 12, 8, 0.5, 30, -20); err != nil || !ok {
		t.Fatalf("Drop: %v, %v", ok, err)
	}
	for range 5 {
		tick(t, s, w)
	}
	before := cells(w)[c.powder]
	b := &s.bodies[0]
	k := b.cells[len(b.cells)/2]
	debris = breakOff(t, s, [2]int{int(k.x), int(k.y)})
	if n := cells(w)[c.powder] + debris[c.powder] - before; len(s.bodies) != 0 || n != 12*8-1 {
		t.Fatalf("the thrown pane left %d bodies and %d cells of powder; want none and %d", len(s.bodies), n, 12*8-1)
	}
	for range 120 {
		tick(t, s, w)
	}
	if n := cells(w)[c.powder] + debris[c.powder] - before; n != 12*8-1 {
		t.Fatalf("%d cells of the powder after it fell, want %d", n, 12*8-1)
	}
}

// A body asleep on a static body falls when what holds it up goes: when
// a notch is dug under it into the static body, which stays whole and
// changes no cell's class, so no terrain is rebuilt; when the static body
// shatters; and when a cut lets the part under it fall. Box2D wakes the
// bodies touching a shape it destroys, static or not.
func TestBodiesOnStaticBodiesFallWhenItBreaks(t *testing.T) {
	notch := func() (cells [][2]int) {
		for y := 60; y < 66; y++ {
			for x := 78; x < 92; x++ {
				cells = append(cells, [2]int{x, y})
			}
		}
		return cells
	}
	for _, tt := range []struct {
		name string
		mat  func(c cold) material.ID
		dig  [][2]int
	}{
		{"a notch under it", func(c cold) material.ID { return c.ice }, notch()},
		{"glass shatters", func(c cold) material.ID { return c.glass }, column(50, 60, 61)},
		{"ice is cut", func(c cold) material.ID { return c.ice }, column(70, 60, 70)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, w, id := newSystem(t, 200, 120, 1)
			w.Fill(0, 110, 200, 120, id.stone)
			w.Fill(40, 60, 100, 70, tt.mat(coldIDs(s)))
			digCell(t, s, 40, 60)
			convert(t, s)
			drop(t, s, id.wood, 80, 48, 10, 6)
			box := &s.bodies[1]
			for i := 0; s.phys.Awake(box.id); i++ {
				if i == 600 {
					t.Fatal("the box did not fall asleep on the static body")
				}
				tick(t, s, w)
			}
			at := s.phys.Position(box.id)
			breakOff(t, s, tt.dig...)
			for range 60 {
				tick(t, s, w)
			}
			// The box is the body of wood, wherever its slot went.
			for i := range s.bodies {
				if b := &s.bodies[i]; b.mat == id.wood {
					if p := s.phys.Position(b.id); p.Y <= at.Y+0.5 {
						t.Fatalf("the box stays at %v, from %v; it should fall", p, at)
					}
					return
				}
			}
			t.Fatal("the box is gone")
		})
	}
}

// A static body leaves memory with a block and comes back as it was:
// static, with the same record, pixels and shapes, written to the same
// cells. A static body never freezes, though a block not in memory is
// beside it.
func TestStaticBodyLeavesAndComesBack(t *testing.T) {
	s, w, _ := newStreamedSystem(t, 1, 2)
	c := coldIDs(s)
	w.Fill(100, 480, 160, 510, c.ice)
	digCell(t, s, 100, 480)
	convert(t, s)
	w.Fill(200, 480, 230, 490, c.ice)
	digCell(t, s, 200, 480)
	convert(t, s)
	breakOff(t, s, column(210, 481, 490)...)
	for range 10 {
		tick(t, s, w)
	}
	s.Write()
	before := s.Records(nil)
	edges, _, static := s.BodyEdges(nil, nil, nil)
	if len(static) == 0 {
		t.Fatal("no static edges")
	}
	edges = append(edges, static...)
	cellsBefore := grid(w)
	recs := s.Unload(0, 0, nil)
	if len(recs) != len(before) || !recs[0].Static {
		t.Fatalf("unloaded %d of %d bodies, the first static %t", len(recs), len(before), len(recs) > 0 && recs[0].Static)
	}
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	s.Write()
	if got := s.Records(nil); !reflect.DeepEqual(got, before) {
		t.Fatalf("loaded bodies\n%+v\ndiffer from those unloaded\n%+v", got, before)
	}
	if got, _, static := s.BodyEdges(nil, nil, nil); !slices.Equal(append(got, static...), edges) {
		t.Fatal("the loaded bodies have other shapes")
	}
	if !slices.Equal(grid(w), cellsBefore) {
		t.Fatal("the loaded bodies, written, cover other cells")
	}
	// The block below leaves; the static body at the edge of the one left
	// stays in the simulation.
	s.Unload(0, 1, nil)
	if _, err := w.UnloadBlock(0, 1); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		tick(t, s, w)
	}
	if b := &s.bodies[0]; !b.static || b.frozen {
		t.Fatalf("the static body is static %t, frozen %t; want static and not frozen", b.static, b.frozen)
	}
}

// A static body across two blocks that comes back with the first of them,
// while the second is not in memory yet, writes its pixels in the second
// once that comes back too: it never moves, so only the block's coming
// back has it written again (Codex review).
func TestStaticBodyFillsABlockThatComesBackLater(t *testing.T) {
	s, w, _ := newStreamedSystem(t, 2, 1)
	c := coldIDs(s)
	w.Fill(490, 400, 530, 420, c.ice)
	digCell(t, s, 490, 400)
	convert(t, s)
	tick(t, s, w)
	n := s.bodies[0].n
	if got := w.Count(c.iceBody); got != n {
		t.Fatalf("the static body shows %d of its %d pixels", got, n)
	}
	// Both blocks leave, the body with the first.
	recs := s.Unload(0, 0, nil)
	left, err := w.UnloadBlock(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	s.Unload(1, 0, nil)
	right, err := w.UnloadBlock(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The first comes back, then the second a few ticks later.
	if err := w.LoadBlock(0, 0, left); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tick(t, s, w)
	}
	if err := w.LoadBlock(1, 0, right); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(1, 0, nil); err != nil {
		t.Fatal(err)
	}
	tick(t, s, w)
	if got := w.Count(c.iceBody); got != n {
		t.Fatalf("with both blocks back, the static body shows %d of its %d pixels", got, n)
	}
}

// A tick with static bodies and pieces of them allocates nothing.
func TestTickWithStaticBodiesDoesNotAllocate(t *testing.T) {
	s, w, id := newSystem(t, 200, 120, 1)
	c := coldIDs(s)
	w.Fill(0, 110, 200, 120, id.stone)
	w.Fill(40, 60, 100, 70, c.ice)
	digCell(t, s, 40, 60)
	convert(t, s)
	breakOff(t, s, column(70, 61, 70)...)
	drop(t, s, id.wood, 120, 40, 10, 6)
	for range 30 {
		tick(t, s, w)
	}
	if n := testing.AllocsPerRun(30, func() { tick(t, s, w) }); n != 0 {
		t.Fatalf("a tick allocates %v times", n)
	}
}

// BenchmarkConvert times turning a square of ice of side n into one static
// body, from the dug cell in its middle: the fill, the shapes and the
// first write. Whole, the square is one polygon; with holes, one cell in
// 16 dug out at random, its shapes are many. 256 is the window, the most a
// body holds.
func BenchmarkConvert(b *testing.B) {
	for _, n := range []int{64, 128, 192, 256} {
		for _, holes := range []bool{false, true} {
			b.Run(fmt.Sprintf("side=%d/holes=%t", n, holes), func(b *testing.B) {
				s, w, _ := newSystem(b, 512, 512, 1)
				c := coldIDs(s)
				x0, mid := 128, 128+n/2
				for range b.N {
					b.StopTimer()
					s.Unload(0, 0, nil)
					w.Fill(0, 0, 512, 512, material.Air)
					w.Fill(x0, x0, x0+n, x0+n, c.ice)
					if holes {
						r := rng.New(uint64(n))
						for y := x0; y < x0+n; y++ {
							for x := x0; x < x0+n; x++ {
								if r.IntN(16) == 0 && (x != mid || y != mid) {
									w.Set(x, y, material.Air)
								}
							}
						}
					}
					b.StartTimer()
					digCell(b, s, mid, mid)
					convert(b, s)
				}
				b.StopTimer()
				if len(s.bodies) == 0 {
					b.Fatal("no body made")
				}
			})
		}
	}
}
