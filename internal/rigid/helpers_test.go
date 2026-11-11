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
// data/materials.json, but wood of buoyancy b (0 there), then ice, glass
// and what they turn into, the glass that shatters and its powder, as
// there; trunk, a test terrain that turns into static wood; rock, one that
// turns into a static body that keeps its largest piece; and pane, one
// that turns into static glass that shatters.
func testTable(tb testing.TB, b float64) *material.Table {
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
		{Name: "stone", Kind: material.Static, Density: 10, Hardness: 10, HP: 100000},
		{Name: "sand", Kind: material.Powder, Density: 6, FallAccel: q(0.225), SinkChance: 25, SlideChance: 50, Hardness: 4, HP: 800},
		{
			Name: "water", Kind: material.Liquid, Density: 4, FallAccel: q(0.375), SinkChance: 50,
			FlowSpeed: q(0.9), FlowDamping: q(0.8), Dispersion: 4,
		},
		{
			Name: "wood", Kind: material.Solid, Density: 6, Hardness: 8, HP: 5000, Friction: q(0.9), Restitution: q(0.2), Buoyancy: q(b),
			Anchor: material.AnchorBottom,
		},
		{Name: "ice", Kind: material.Static, Density: 10, HP: 30000, Body: "ice_body"},
		{
			Name: "ice_body", Kind: material.Solid, Density: 6, HP: 10000, Friction: q(0.05), Restitution: q(0.2),
			Anchor: material.AnchorTop, Cracks: true, SleepTo: "ice",
		},
		{Name: "glass", Kind: material.Static, Density: 10, HP: 30000, Body: "glass_slab"},
		{
			Name: "glass_body", Kind: material.Solid, Density: 6, HP: 200, Friction: q(0.3), Restitution: q(0.2),
			Anchor: material.AnchorLargest, Shatter: "glass_powder", Cracks: true,
		},
		{Name: "glass_powder", Kind: material.Powder, Density: 6, FallAccel: q(0.275), SinkChance: 25, SlideChance: 50, HP: 1000},
		{Name: "trunk", Kind: material.Static, Density: 11, Hardness: 8, HP: 5000, Body: "wood"},
		{Name: "rock", Kind: material.Static, Density: 10, HP: 1000, Body: "rock_body"},
		{Name: "rock_body", Kind: material.Solid, Density: 10, HP: 1000, Friction: q(0.6), Anchor: material.AnchorLargest},
		{
			Name: "glass_slab", Kind: material.Solid, Density: 6, HP: 10000, Friction: q(0.9),
			Anchor: material.AnchorLargest, Cracks: true, SleepTo: "glass",
		},
		{Name: "pane", Kind: material.Static, Density: 10, HP: 30000, Body: "glass_body"},
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
	return newSystemBuoyancy(tb, width, height, workers, 0.7)
}

// newSystemBuoyancy is newSystem with wood of buoyancy b.
func newSystemBuoyancy(tb testing.TB, width, height, workers int, b float64) (*System, *world.World, ids) {
	tb.Helper()
	s, w, id := openSystem(tb, width, height, workers, b)
	tb.Cleanup(w.Close)
	tb.Cleanup(s.Close)
	return s, w, id
}

// openSystem is newSystemBuoyancy for a caller that closes the System and
// the world itself, such as a benchmark that makes one per iteration:
// Box2D holds 128 worlds at once.
func openSystem(tb testing.TB, width, height, workers int, b float64) (*System, *world.World, ids) {
	tb.Helper()
	return openSeededSystem(tb, width, height, workers, b, 1)
}

// newSeededSystem is newSystem, run on one worker, for a world of seed.
func newSeededSystem(tb testing.TB, width, height int, seed uint64) (*System, *world.World, ids) {
	tb.Helper()
	s, w, id := openSeededSystem(tb, width, height, 1, 0.7, seed)
	tb.Cleanup(w.Close)
	tb.Cleanup(s.Close)
	return s, w, id
}

// openSeededSystem is openSystem for a world of seed.
func openSeededSystem(tb testing.TB, width, height, workers int, b float64, seed uint64) (*System, *world.World, ids) {
	tb.Helper()
	tab := testTable(tb, b)
	w, err := world.New(world.Config{Width: width, Height: height, Seed: seed, Workers: workers, Materials: tab})
	if err != nil {
		tb.Fatal(err)
	}
	s, err := New(w, tab)
	if err != nil {
		w.Close()
		tb.Fatal(err)
	}
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
	if err := s.Step(); err != nil {
		tb.Fatal(err)
	}
}

// drop drops a w × h body of wood with its top-left pixel at (x, y), and
// fails the test if it cannot.
func drop(tb testing.TB, s *System, m material.ID, x, y, w, h int) {
	tb.Helper()
	ok, err := s.Drop(m, x, y, w, h, 0, 0, 0)
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
const testMaterials = 15

// cold holds the test materials that turn into bodies and what they turn
// into.
type cold struct {
	ice, iceBody, glass, slab, glassBody, powder, trunk, rock, pane material.ID
}

// coldIDs returns the IDs of the materials of cold in s's table.
func coldIDs(s *System) cold {
	m := s.mats.MustLookup
	return cold{
		m("ice"), m("ice_body"), m("glass"), m("glass_slab"), m("glass_body"), m("glass_powder"),
		m("trunk"), m("rock"), m("pane"),
	}
}

// cells returns how many cells of the world hold each material of
// testTable, plus the cells waiting in its queue and those flying as
// particles.
func cells(w *world.World) []int {
	c := make([]int, testMaterials)
	w.Counts(c)
	w.Queued(c)
	w.Flying(c)
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

// collide runs Collide and fails the test on an error.
func collide(tb testing.TB, s *System) {
	tb.Helper()
	if err := s.Collide(); err != nil {
		tb.Fatal(err)
	}
}

// classAt returns the class the chunk that owns cell (x, y) last built
// its collision from for it.
func classAt(s *System, x, y int) world.Class {
	i := (y/world.ChunkSize)*s.cols + x/world.ChunkSize
	c := s.chunkCells(i)
	return s.terrain[i].cls[(y-c.y0+1)*(c.x1-c.x0+2)+x-c.x0+1]
}

// cellDepth returns how deep cell (x, y) and b's shape, where b was last
// written, overlap in cells, in float64; 0 or less when they do not.
func cellDepth(b *body, x, y int) float64 {
	c, s := float64(b.wq.C), float64(b.wq.S)
	dx := float64(x) + 0.5 - float64(b.wp.X)*cellsPerMeter
	dy := float64(y) + 0.5 - float64(b.wp.Y)*cellsPerMeter
	hw, hh := float64(b.w)/2, float64(b.h)/2
	r := (math.Abs(c) + math.Abs(s)) / 2
	return min(
		0.5+math.Abs(c)*hw+math.Abs(s)*hh-math.Abs(dx),
		0.5+math.Abs(s)*hw+math.Abs(c)*hh-math.Abs(dy),
		r+hw-math.Abs(c*dx+s*dy),
		r+hh-math.Abs(c*dy-s*dx),
	)
}

// bodyOverlap returns how deep the shapes of a and b overlap in cells,
// where Box2D has them now; 0 when they do not.
func bodyOverlap(s *System, a, b *body) float64 {
	shape := func(b *body) (x, y, c, sn, hw, hh float64) {
		p, q := s.phys.Transform(b.id)
		return float64(p.X) * cellsPerMeter, float64(p.Y) * cellsPerMeter, float64(q.C), float64(q.S), float64(b.w) / 2, float64(b.h) / 2
	}
	ax, ay, ac, as, ahw, ahh := shape(a)
	bx, by, bc, bs, bhw, bhh := shape(b)
	depth := math.Inf(1)
	for _, n := range [][2]float64{{ac, as}, {-as, ac}, {bc, bs}, {-bs, bc}} {
		ra := ahw*math.Abs(ac*n[0]+as*n[1]) + ahh*math.Abs(-as*n[0]+ac*n[1])
		rb := bhw*math.Abs(bc*n[0]+bs*n[1]) + bhh*math.Abs(-bs*n[0]+bc*n[1])
		depth = min(depth, ra+rb-math.Abs((bx-ax)*n[0]+(by-ay)*n[1]))
	}
	return max(depth, 0)
}

// heldIf returns a pixel the image holds if held is set, else one gone.
func heldIf(held bool) pixel {
	if held {
		return pixel{bits: pixelHeld}
	}
	return pixel{}
}
