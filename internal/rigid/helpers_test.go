package rigid

import (
	"math"
	"testing"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

type ids struct {
	stone, sand, water, wood material.ID
}

// testTable returns air, stone, sand, water and wood with the values of
// data/materials.json.
func testTable(tb testing.TB) *material.Table {
	tb.Helper()
	q := func(f float64) fixed.Q8 {
		v, err := fixed.FromFloat(f)
		if err != nil {
			tb.Fatal(err)
		}
		return v
	}
	tab, err := material.NewTable([]material.Def{
		{Name: "air", Kind: material.Empty},
		{Name: "stone", Kind: material.Static, Density: 255, Hardness: 10, HP: 100000},
		{Name: "sand", Kind: material.Powder, Density: 30, FallAccel: q(0.225), SinkChance: 25, SlideChance: 50, Hardness: 4, HP: 800},
		{
			Name: "water", Kind: material.Liquid, Density: 20, FallAccel: q(0.375), SinkChance: 50,
			FlowSpeed: q(0.9), FlowDamping: q(0.8), Dispersion: 4,
		},
		{Name: "wood", Kind: material.Solid, Density: 6, Friction: q(0.9), Restitution: q(0.2)},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return tab
}

// newSystem returns a world of width × height cells, run on workers, its
// System and the test materials.
func newSystem(tb testing.TB, width, height, workers int) (*System, *world.World, ids) {
	tb.Helper()
	tab := testTable(tb)
	w, err := world.New(world.Config{Width: width, Height: height, Seed: 1, Workers: workers, Materials: tab})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(w.Close)
	s, err := New(w, tab)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(s.Close)
	return s, w, ids{tab.MustLookup("stone"), tab.MustLookup("sand"), tab.MustLookup("water"), tab.MustLookup("wood")}
}

// tick runs one tick in the game's order.
func tick(tb testing.TB, s *System, w *world.World) {
	tb.Helper()
	s.Write()
	if err := s.Collide(); err != nil {
		tb.Fatal(err)
	}
	w.Step()
	s.Step()
}

// drop drops a w × h body of wood with its top-left pixel at (x, y), and
// fails the test if it cannot.
func drop(tb testing.TB, s *System, m material.ID, x, y, w, h int) {
	tb.Helper()
	ok, err := s.Drop(m, x, y, w, h)
	if err != nil || !ok {
		tb.Fatalf("Drop at (%d, %d): %v, %v", x, y, ok, err)
	}
}

// owned returns how many cells hold a pixel of the body at slot, and the
// rectangle around them.
func owned(w *world.World, wood material.ID, slot uint16) (n int, r rect) {
	r = rect{w.Width(), w.Height(), -1, -1}
	for y := range w.Height() {
		for x := range w.Width() {
			c, _ := w.At(x, y)
			if b, _ := c.Body(); c.Mat == wood && b == slot {
				n++
				r.x0, r.y0 = min(r.x0, x), min(r.y0, y)
				r.x1, r.y1 = max(r.x1, x+1), max(r.y1, y+1)
			}
		}
	}
	return n, r
}

// testMaterials is how many materials testTable holds.
const testMaterials = 5

// cells returns how many cells of the world hold each material of
// testTable, plus the cells waiting in its queue.
func cells(w *world.World) []int {
	c := make([]int, testMaterials)
	w.Counts(c)
	w.Queued(c)
	return c
}

// setPose puts b's center at (x, y) in cells, turned by deg degrees, as
// though Box2D had moved it there.
func setPose(b *body, x, y, deg float64) {
	a := deg * math.Pi / 180
	b.p = physics.Vec2{X: float32(x / cellsPerMeter), Y: float32(y / cellsPerMeter)}
	b.q = physics.Rot{C: float32(math.Cos(a)), S: float32(math.Sin(a))}
}

// imageAt returns where the center of cell (x, y) falls in b's image, in
// pixels from its top-left corner, in float64.
func imageAt(b *body, x, y int) (fi, fj float64) {
	c, s := float64(b.q.C), float64(b.q.S)
	dx := float64(x) + 0.5 - float64(b.p.X)*cellsPerMeter
	dy := float64(y) + 0.5 - float64(b.p.Y)*cellsPerMeter
	return c*dx + s*dy + float64(b.w)/2, c*dy - s*dx + float64(b.h)/2
}

// edge is how near a pixel's edge a point may be and still round either
// way between float32 and float64.
const edge = 1e-3

// onEdge reports whether f lies within edge of a whole number.
func onEdge(f float64) bool {
	return math.Abs(f-math.Round(f)) < edge
}
