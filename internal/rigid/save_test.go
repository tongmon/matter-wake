package rigid

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

// newStreamedSystem returns a world of cols × rows blocks, all in memory
// and empty but for a stone floor along the bottom, its System and the
// test materials.
func newStreamedSystem(tb testing.TB, cols, rows int) (*System, *world.World, ids) {
	tb.Helper()
	tab := testTable(tb, 0)
	w, err := world.New(world.Config{
		Width: cols * world.BlockSize, Height: rows * world.BlockSize, Seed: 1, Workers: 1, Materials: tab, Streamed: true,
	})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(w.Close)
	for by := range rows {
		for bx := range cols {
			if err := w.LoadBlock(bx, by, new(world.Block)); err != nil {
				tb.Fatal(err)
			}
		}
	}
	id := ids{tab.MustLookup("stone"), tab.MustLookup("sand"), tab.MustLookup("water"), tab.MustLookup("wood")}
	w.Fill(0, w.Height()-4, w.Width(), w.Height(), id.stone)
	s, err := New(w, tab)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(s.Close)
	return s, w, id
}

// grid returns every cell of w in memory, row by row.
func grid(w *world.World) []world.Cell {
	var all []world.Cell
	for y := range w.Height() {
		for x := range w.Width() {
			c, _ := w.At(x, y)
			all = append(all, c)
		}
	}
	return all
}

// solids returns how many cells of block (bx, by) hold a solid.
func solids(s *System, b *world.Block) int {
	n := 0
	for _, c := range b {
		if s.kinds[c.Mat] == material.Solid {
			n++
		}
	}
	return n
}

// A body taken out with its block and loaded again is the body it was:
// the same record, mass, inertia and center of mass to the bit, the same
// shapes, and written back it covers the same cells. That holds for a box
// still whole and a piece of a broken plank, moving and turned.
func TestLoadedBodyIsTheSame(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 1)
	drop(t, s, id.wood, 100, 300, 16, 12)
	drop(t, s, id.wood, 200, 300, 40, 6)
	s.Write()
	breakOff(t, s, column(220, 300, 306)...)
	s.phys.SetVelocity(s.bodies[0].id, physics.Vec2{X: 1.5, Y: -2}, 0.75)
	for range 20 {
		tick(t, s, w)
	}
	s.Write()
	if len(s.bodies) < 3 {
		t.Fatalf("%d bodies; the test needs a box and two pieces", len(s.bodies))
	}
	before := s.Records(nil)
	type inner struct {
		mass, inertia, buoyancy float32
		lc                      physics.Vec2
		cover                   []bool
	}
	var kept []inner
	for i := range s.bodies {
		b := &s.bodies[i]
		kept = append(kept, inner{b.mass, b.inertia, b.buoyancy, b.lc, slices.Clone(b.cover)})
	}
	cells := grid(w)
	recs := s.Unload(0, 0, nil)
	if len(recs) != len(before) || len(s.bodies) != 0 {
		t.Fatalf("unloaded %d of %d bodies, %d left", len(recs), len(before), len(s.bodies))
	}
	for i, c := range grid(w) {
		if s.kinds[c.Mat] == material.Solid {
			t.Fatalf("cell %d still holds a body's pixel after the bodies left", i)
		}
	}
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	if got := s.Records(nil); !reflect.DeepEqual(got, before) {
		t.Fatalf("loaded bodies\n%+v\ndiffer from those unloaded\n%+v", got, before)
	}
	for i := range s.bodies {
		b := &s.bodies[i]
		if got := (inner{b.mass, b.inertia, b.buoyancy, b.lc, b.cover}); !reflect.DeepEqual(got, kept[i]) {
			t.Fatalf("body %d came back with mass, inertia, buoyancy, center and cover %+v, want %+v", i, got, kept[i])
		}
	}
	s.Write()
	if !slices.Equal(grid(w), cells) {
		t.Fatal("the loaded bodies, written, cover other cells than they did")
	}
}

// A body belongs to the block that holds its center of mass, and leaves
// with it, though its pixels reach into the next block. Its pixels there
// go too: the block that stays holds no pixel of a body that is not in
// memory, and the block that leaves holds no body's pixel as a cell, the
// pixels of a body that stays included.
func TestBodyLeavesWithTheBlockOfItsCenter(t *testing.T) {
	s, w, id := newStreamedSystem(t, 2, 1)
	floor := w.Height() - 4
	// Centered at x = 516, in block 1, with 8 columns in block 0; and one
	// centered at x = 504, in block 0, with 4 columns in block 1.
	drop(t, s, id.wood, 508, floor-12, 16, 12)
	drop(t, s, id.wood, 492, floor-30, 24, 6)
	for range 90 {
		tick(t, s, w)
	}
	keyOf := map[uint32]bool{}
	for _, r := range s.Records(nil) {
		keyOf[r.Key] = true
	}
	if len(keyOf) != 2 {
		t.Fatalf("%d bodies; the test needs 2", len(keyOf))
	}
	recs := s.Unload(1, 0, nil)
	if len(recs) != 1 || recs[0].Key != 0 {
		t.Fatalf("block (1, 0) took %d bodies, want the one of key 0", len(recs))
	}
	left := s.Records(nil)
	if len(left) != 1 || left[0].Key != 1 {
		t.Fatalf("%d bodies stayed, want the one of key 1", len(left))
	}
	cells, err := w.UnloadBlock(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n := solids(s, cells); n != 0 {
		t.Fatalf("the block that left holds %d cells of bodies' pixels", n)
	}
	for y := range w.Height() {
		for x := range world.BlockSize {
			c, _ := w.At(x, y)
			if b, _ := c.Body(); s.kinds[c.Mat] == material.Solid && (int(b) >= len(s.bodies) || s.bodies[b].key != 1) {
				t.Fatalf("cell (%d, %d) still shows a pixel of a body that left", x, y)
			}
		}
	}
	// The body that stayed comes back where it is: what lies in the block
	// that left is not written.
	tick(t, s, w)
	if n, r := owned(w, id.wood, 0); n == 0 || r.x1 > world.BlockSize {
		t.Fatalf("the body that stayed shows %d pixels in %+v", n, r)
	}
}

// A body that sat on a body that left is woken, as Box2D wakes the bodies
// touching one it removes, and falls.
func TestBodiesOnALeavingBodyFall(t *testing.T) {
	s, w, id := newStreamedSystem(t, 2, 1)
	floor := w.Height() - 4
	// A plank centered in block 1 that reaches far into block 0, and a box
	// on it there, too far from block 1 to reach it.
	drop(t, s, id.wood, 300, floor-6, 440, 6)
	drop(t, s, id.wood, 350, floor-18, 16, 12)
	for range 300 {
		tick(t, s, w)
	}
	top := &s.bodies[1]
	if s.phys.Awake(top.id) || s.phys.Awake(s.bodies[0].id) {
		t.Fatal("the bodies did not fall asleep")
	}
	y := top.p.Y
	s.Unload(1, 0, nil)
	if _, err := w.UnloadBlock(1, 0); err != nil {
		t.Fatal(err)
	}
	top = &s.bodies[0]
	if !s.phys.Awake(top.id) {
		t.Fatal("the body that rested on the one that left sleeps on")
	}
	for range 60 {
		tick(t, s, w)
	}
	if p, _ := s.phys.Transform(top.id); p.Y <= y {
		t.Fatalf("the body stayed at y %v; it rested at %v on what left", p.Y, y)
	}
}

// A body asleep comes back asleep; a body whose reach goes past its own
// block comes back awake, since what lies there may have changed while it
// was away.
func TestSleepingBodyComesBackAsleep(t *testing.T) {
	s, w, id := newStreamedSystem(t, 2, 1)
	floor := w.Height() - 4
	drop(t, s, id.wood, 200, floor-12, 16, 12)
	drop(t, s, id.wood, 497, floor-12, 12, 12) // center x = 503, 5 cells from block 1
	for range 300 {
		tick(t, s, w)
	}
	for i := range s.bodies {
		if s.phys.Awake(s.bodies[i].id) {
			t.Fatalf("body %d did not fall asleep", i)
		}
	}
	recs := s.Unload(0, 0, nil)
	for _, r := range recs {
		if r.Awake {
			t.Fatalf("body of key %d recorded awake", r.Key)
		}
	}
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	if s.phys.Awake(s.bodies[0].id) {
		t.Fatal("the body in the middle of its block came back awake")
	}
	if !s.phys.Awake(s.bodies[1].id) {
		t.Fatal("the body that reaches the next block came back asleep")
	}
}

// A body whose reach for the coming Step meets a block not in memory
// stops where it is, frozen, and so does a body that would touch it; they
// keep their pixels in the grid. When the block comes back they go on,
// the frozen body with the velocity it had.
func TestBodyFreezesAtTheEdgeOfMemory(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 2)
	s.Unload(0, 1, nil)
	if _, err := w.UnloadBlock(0, 1); err != nil {
		t.Fatal(err)
	}
	drop(t, s, id.wood, 200, 440, 16, 12)
	var frozenAt physics.Vec2
	var v physics.Vec2
	for i := range 120 {
		b := &s.bodies[0]
		p, _ := s.phys.Transform(b.id)
		if !b.frozen {
			v, _ = s.phys.Velocity(b.id)
		}
		tick(t, s, w)
		if b.frozen && frozenAt == (physics.Vec2{}) {
			frozenAt = p
			if i == 0 || v.Y <= 0 {
				t.Fatalf("froze at tick %d moving %v; the test needs a falling body", i, v)
			}
		}
	}
	b := &s.bodies[0]
	if !b.frozen {
		t.Fatal("the body falling toward the block not in memory did not freeze")
	}
	if p, _ := s.phys.Transform(b.id); p != frozenAt {
		t.Fatalf("the frozen body moved from %v to %v", frozenAt, p)
	}
	if b.near.y1 <= world.BlockSize || b.box.y1 > world.BlockSize {
		t.Fatalf("the body froze with its reach %+v and its cells %+v; it should reach row %d with its reach only",
			b.near, b.box, world.BlockSize)
	}
	// A body dropped on top freezes before it touches the frozen one.
	drop(t, s, id.wood, 200, 400, 16, 12)
	for range 120 {
		tick(t, s, w)
	}
	// Adding a body may have moved the slots.
	b, top := &s.bodies[0], &s.bodies[1]
	if !top.frozen {
		t.Fatal("the body falling onto the frozen one did not freeze")
	}
	if n, _ := owned(w, id.wood, 0); n != 16*12 {
		t.Fatalf("the frozen body shows %d pixels, want %d", n, 16*12)
	}
	if err := w.LoadBlock(0, 1, new(world.Block)); err != nil {
		t.Fatal(err)
	}
	tick(t, s, w)
	if b.frozen || top.frozen {
		t.Fatal("the bodies stay frozen with the block back in memory")
	}
	if got, _ := s.phys.Velocity(b.id); got.Y < v.Y {
		t.Fatalf("the body goes on at %v, slower than the %v it froze with", got, v)
	}
}

// A block's bodies and the cells it held come back in key order among the
// bodies in memory: the slots stay in key order and every cell links to
// the slot its body holds.
func TestLoadedBodiesTakeTheirKeysPlaces(t *testing.T) {
	s, w, id := newStreamedSystem(t, 2, 1)
	floor := w.Height() - 4
	for i := range 6 {
		x := 60 + 120*i // keys 0..5, centers in blocks 0, 0, 0, 0, 1, 1
		drop(t, s, id.wood, x, floor-12, 16, 12)
	}
	for range 30 {
		tick(t, s, w)
	}
	recs := s.Unload(0, 0, nil)
	if len(recs) != 4 {
		t.Fatalf("block (0, 0) took %d bodies, want 4", len(recs))
	}
	if err := s.Load(0, 0, recs[1:3]); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(0, 0, []save.Body{recs[3], recs[0]}); err == nil {
		t.Fatal("Load took bodies out of key order")
	}
	if err := s.Load(0, 0, []save.Body{recs[0], recs[3]}); err != nil {
		t.Fatal(err)
	}
	// A key already in memory comes from a save mixed after a crash; the
	// copy in memory stays.
	if err := s.Load(0, 0, recs[2:3]); err != nil {
		t.Fatal(err)
	}
	for i := range s.bodies {
		if s.bodies[i].key != uint32(i) {
			t.Fatalf("slot %d holds key %d", i, s.bodies[i].key)
		}
	}
	tick(t, s, w)
	for i := range s.bodies {
		if n, _ := owned(w, id.wood, uint16(i)); n != 16*12 {
			t.Fatalf("slot %d shows %d pixels, want %d", i, n, 16*12)
		}
	}
	// New bodies take keys after every key loaded.
	s.nextKey = 0
	if err := s.Load(1, 0, s.Unload(1, 0, nil)); err != nil {
		t.Fatal(err)
	}
	if s.NextKey() != 6 {
		t.Fatalf("next key %d after loading keys up to 5", s.NextKey())
	}
}

// Load refuses records no body can be made of, and adds none of them.
func TestLoadRefusesBadRecords(t *testing.T) {
	s, _, id := newStreamedSystem(t, 1, 1)
	good := func() save.Body {
		return save.Body{
			Key: 1, Mat: id.wood, W: 2, H: 1, Left: -1, Top: -0.5,
			Pixels: []save.Pixel{{OK: true}, {OK: true}}, Whole: true, X: 10, Y: 10, C: 1,
		}
	}
	bad := map[string]func(b *save.Body){
		"not a solid":       func(b *save.Body) { b.Mat = id.sand },
		"no such material":  func(b *save.Body) { b.Mat = 99 },
		"no pixels":         func(b *save.Body) { b.Pixels = []save.Pixel{{}, {}} },
		"wrong pixel count": func(b *save.Body) { b.Pixels = b.Pixels[:1] },
		"too big":           func(b *save.Body) { b.W, b.H = 1<<9, 1<<9 },
		"no rotation":       func(b *save.Body) { b.C = 0 },
		"not finite":        func(b *save.Body) { b.VX = float32(math.Inf(1)) },
		"outside the world": func(b *save.Body) { b.X = 60 },
	}
	for name, edit := range bad {
		b := good()
		b.Key = 2
		edit(&b)
		if err := s.Load(0, 0, []save.Body{good(), b}); err == nil {
			t.Errorf("%s: Load took the record", name)
		}
		if len(s.bodies) != 0 {
			t.Fatalf("%s: Load added %d bodies before it failed", name, len(s.bodies))
		}
	}
}

// Reset starts Box2D anew, only with no body in memory.
func TestReset(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 1)
	drop(t, s, id.wood, 100, 300, 16, 12)
	tick(t, s, w)
	if err := s.Reset(); err == nil {
		t.Fatal("Reset with a body in memory")
	}
	recs := s.Unload(0, 0, nil)
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	for range 240 {
		tick(t, s, w)
	}
	if n, r := owned(w, id.wood, 0); n != 16*12 || r.y1 != w.Height()-4 {
		t.Fatalf("after Reset the body shows %d pixels down to row %d, want %d down to the floor at %d", n, r.y1, 16*12, w.Height()-4)
	}
}

// A tick with frozen bodies allocates nothing, as one without.
func TestFrozenTickDoesNotAllocate(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 2)
	s.Unload(0, 1, nil)
	if _, err := w.UnloadBlock(0, 1); err != nil {
		t.Fatal(err)
	}
	drop(t, s, id.wood, 200, 480, 16, 12)
	drop(t, s, id.wood, 200, 440, 16, 12)
	for range 60 {
		tick(t, s, w)
	}
	if s.Frozen() != 2 {
		t.Fatalf("%d bodies frozen; the test needs 2", s.Frozen())
	}
	if n := testing.AllocsPerRun(50, func() { tick(t, s, w) }); n != 0 {
		t.Fatalf("a tick with frozen bodies allocates %v times", n)
	}
}
