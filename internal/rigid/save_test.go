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

// A block leaving memory takes every body that touches it, whole, wherever
// its center is: one whose cells are one cell away from the block too, but
// not one two cells away, as Noita took a box one pixel from the block and
// left one two pixels away (C137, BODY_BLOCK_RESIDENCY 3.2). The same holds
// from either side. The block that leaves holds no body's pixel as a cell,
// and the block that stays no pixel of a body that left.
func TestBodiesLeaveWithEveryBlockTheyTouch(t *testing.T) {
	s, w, id := newStreamedSystem(t, 2, 1)
	// Keys 0 to 5: centered in block 1 with 4 columns in block 0; centered
	// in block 0 with 4 columns in block 1; in block 0 one cell from block
	// 1; in block 0 two cells from it; in block 1 one cell from block 0;
	// in block 1 two cells from it.
	for k, x := range []int{508, 492, 495, 494, 513, 514} {
		width := 16
		if k == 1 {
			width = 24
		}
		drop(t, s, id.wood, x, 60+50*k, width, 12)
	}
	s.Write()
	keys := func(recs []save.Body) []uint32 {
		var k []uint32
		for _, r := range recs {
			k = append(k, r.Key)
		}
		return k
	}
	check := func(bx int, leaving, staying []uint32) []save.Body {
		t.Helper()
		recs := s.Unload(bx, 0, nil)
		if got := keys(recs); !slices.Equal(got, leaving) {
			t.Fatalf("block (%d, 0) took bodies %v, want %v", bx, got, leaving)
		}
		if got := keys(s.Records(nil)); !slices.Equal(got, staying) {
			t.Fatalf("bodies %v stayed, want %v", got, staying)
		}
		left := blockCells(bx, 0)
		for y := range w.Height() {
			for x := range w.Width() {
				c, _ := w.At(x, y)
				if s.kinds[c.Mat] != material.Solid {
					continue
				}
				if x >= left.x0 && x < left.x1 {
					t.Fatalf("cell (%d, %d) of the block that leaves shows a body's pixel", x, y)
				}
				if b, _ := c.Body(); int(b) >= len(s.bodies) || !slices.Contains(staying, s.bodies[b].key) {
					t.Fatalf("cell (%d, %d) shows a pixel of a body that left", x, y)
				}
			}
		}
		return recs
	}
	recs := check(1, []uint32{0, 1, 2, 4, 5}, []uint32{3})
	if err := s.Load(1, 0, recs); err != nil {
		t.Fatal(err)
	}
	s.Write()
	check(0, []uint32{0, 1, 2, 3, 4}, []uint32{5})
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
	if b.reach.y1 <= world.BlockSize || b.box.y1 > world.BlockSize {
		t.Fatalf("the body froze with its reach %+v and its cells %+v; it should reach row %d with its reach only",
			b.reach, b.box, world.BlockSize)
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

// A piece keeps the frame of the body it broke from, so its origin can lie
// outside the world while the piece is in it; Load takes such a record.
// Found by the Codex review of body saves (2026-11-09).
func TestLoadTakesAPieceWhoseOriginIsOutside(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 1)
	drop(t, s, id.wood, 100, 300, 300, 6) // origin at x = 250
	s.Write()
	breakOff(t, s, column(349, 300, 306)...)
	recs := s.Unload(0, 0, nil)
	if len(recs) != 2 || recs[1].W != 50 {
		t.Fatalf("%d records; the test needs the plank and a piece of 50 pixels", len(recs))
	}
	// The piece lies 100 to 150 cells right of its origin. Put the origin 5
	// m left of the world: the piece is at x 50 to 100.
	piece := recs[1]
	piece.X = -5
	if err := s.Load(0, 0, []save.Body{piece}); err != nil {
		t.Fatalf("Load refused a piece inside the world whose origin is not: %v", err)
	}
	s.Write()
	if n, r := owned(w, id.wood, 0); n != 50*6 || r.x0 != 50 {
		t.Fatalf("the piece shows %d pixels from x %d, want %d from 50", n, r.x0, 50*6)
	}
	// One wholly outside the world is still refused.
	piece.X = -20
	piece.Key = 7
	if err := s.Load(0, 0, []save.Body{piece}); err == nil {
		t.Fatal("Load took a piece outside the world")
	}
}

// A body made again with its velocity takes that velocity into account in
// its reach from its first tick, so a fast body freezes before it can move
// into a block not in memory. Found by the Codex review of body saves
// (2026-11-09).
func TestFastLoadedBodyFreezesBeforeItMoves(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 2)
	drop(t, s, id.wood, 200, 492, 16, 12)
	s.Write()
	recs := s.Unload(0, 0, nil)
	s.Unload(0, 1, nil)
	if _, err := w.UnloadBlock(0, 1); err != nil {
		t.Fatal(err)
	}
	// 60 m/s down: 10 cells a tick, more than the reach's margin. The box
	// ends at row 504, 8 cells over block (0, 1).
	recs[0].VY = 60
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	tick(t, s, w)
	tick(t, s, w)
	b := &s.bodies[0]
	if !b.frozen {
		t.Fatal("the fast body did not freeze")
	}
	if b.box.y1 > world.BlockSize {
		t.Fatalf("the fast body reached row %d, into the block not in memory, before it froze", b.box.y1)
	}
}

// fastBody sets up a world of two blocks, one over the other, with the
// lower one not in memory and a body made again 8 cells over it, moving
// as edit says: what a block coming back with a moving body looks like.
func fastBody(t *testing.T, w, h int, edit func(r *save.Body)) (*System, *world.World) {
	t.Helper()
	s, wd, id := newStreamedSystem(t, 1, 2)
	drop(t, s, id.wood, 256-w/2, world.BlockSize-8-h, w, h)
	s.Write()
	recs := s.Unload(0, 0, nil)
	s.Unload(0, 1, nil)
	if _, err := wd.UnloadBlock(0, 1); err != nil {
		t.Fatal(err)
	}
	edit(&recs[0])
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	return s, wd
}

// A frozen body dug while it waits is written again where it is, and stays
// frozen: what it may reach is where its velocity takes it, not how far it
// moved since it was last written. Found by the second Codex review of
// body saves (2026-11-09).
func TestDugFrozenBodyStaysFrozen(t *testing.T) {
	s, w := fastBody(t, 16, 12, func(r *save.Body) { r.VY = 60 })
	tick(t, s, w)
	b := &s.bodies[0]
	if !b.frozen {
		t.Fatal("the fast body did not freeze")
	}
	breakOff(t, s, [2]int{b.box.x0 + 3, b.box.y0 + 3})
	for range 3 {
		tick(t, s, w)
	}
	b = &s.bodies[0]
	if !b.frozen || b.box.y1 > world.BlockSize {
		t.Fatalf("the dug body is frozen %v and reaches row %d; the block below is not in memory", b.frozen, b.box.y1)
	}
}

// A spinning body's reach covers how far its ends turn in a tick, so a
// long plank turning fast freezes before an end swings into a block not in
// memory. Found by the second Codex review of body saves (2026-11-09).
func TestSpinningBodyFreezesBeforeItTurnsIn(t *testing.T) {
	s, w := fastBody(t, 200, 6, func(r *save.Body) { r.Spin = 6 })
	tick(t, s, w)
	tick(t, s, w)
	b := &s.bodies[0]
	if !b.frozen || b.box.y1 > world.BlockSize {
		t.Fatalf("the spinning plank is frozen %v and reaches row %d; the block below is not in memory", b.frozen, b.box.y1)
	}
}

// A frozen body that goes on with its velocity when the block comes back
// finds the terrain it moves into made collision first: a thin floor just
// inside that block stops it. Found by the third Codex review of body
// saves (2026-11-09).
func TestThawedBodyStopsOnTheFloorBelow(t *testing.T) {
	s, w := fastBody(t, 16, 12, func(r *save.Body) { r.VY = 120 })
	tick(t, s, w)
	if !s.bodies[0].frozen {
		t.Fatal("the fast body did not freeze")
	}
	// The block comes back with a floor one row thick six rows into it,
	// less than the 20 cells the body moves in a tick.
	blk := new(world.Block)
	for x := range world.BlockSize {
		blk[6*world.BlockSize+x] = world.Cell{Mat: s.mats.MustLookup("stone")}
	}
	if err := w.LoadBlock(0, 1, blk); err != nil {
		t.Fatal(err)
	}
	for range 60 {
		tick(t, s, w)
	}
	if b := &s.bodies[0]; b.box.y1 > world.BlockSize+6 {
		t.Fatalf("the body went through the floor at row %d: its cells reach row %d", world.BlockSize+6, b.box.y1)
	}
}

// A body made again asleep wakes when what held it up is dug away before
// it was first written, as when the player digs under it on the first
// tick after opening a save. Found by the third Codex review of body saves
// (2026-11-09).
func TestRestoredSleeperFallsWhenItsSupportGoes(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 1)
	w.Fill(180, 300, 240, 302, id.stone) // a ledge
	drop(t, s, id.wood, 200, 288, 16, 12)
	for range 300 {
		tick(t, s, w)
	}
	if s.phys.Awake(s.bodies[0].id) {
		t.Fatal("the box on the ledge did not fall asleep")
	}
	recs := s.Unload(0, 0, nil)
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	w.Fill(180, 300, 240, 302, 0) // dug away before the first tick
	for range 60 {
		tick(t, s, w)
	}
	if b := &s.bodies[0]; b.box.y0 <= 300 {
		t.Fatalf("the box hangs at row %d with nothing under it", b.box.y0)
	}
}

// Sleeping bodies that leave together are recorded asleep, though taking
// out the first wakes the ones it touched. Found by the fourth Codex
// review of body saves (2026-11-09).
func TestSleepingStackLeavesAsleep(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 1)
	floor := w.Height() - 4
	drop(t, s, id.wood, 200, floor-12, 16, 12)
	drop(t, s, id.wood, 202, floor-24, 12, 12)
	for range 300 {
		tick(t, s, w)
	}
	for i := range s.bodies {
		if s.phys.Awake(s.bodies[i].id) {
			t.Fatalf("body %d of the stack did not fall asleep", i)
		}
	}
	for _, r := range s.Unload(0, 0, nil) {
		if r.Awake {
			t.Fatalf("body %d of the sleeping stack left awake", r.Key)
		}
	}
}

// A frozen spinning body's terrain is made collision as far as it turns
// in a tick before it is let go: when the block below comes back with a
// floor, the floor under the plank's ends is collision in the tick it
// thaws. (Whether Box2D then catches an end swinging in within one step
// is Box2D's: it finds contacts once a step.) Found by the fourth Codex
// review of body saves (2026-11-09).
func TestThawedSpinningBodyHasItsTerrainFirst(t *testing.T) {
	s, w := fastBody(t, 200, 6, func(r *save.Body) { r.Spin = 6 })
	tick(t, s, w)
	if !s.bodies[0].frozen {
		t.Fatal("the spinning plank did not freeze")
	}
	blk := new(world.Block)
	for x := range world.BlockSize {
		blk[1*world.BlockSize+x] = world.Cell{Mat: s.mats.MustLookup("stone")}
	}
	if err := w.LoadBlock(0, 1, blk); err != nil {
		t.Fatal(err)
	}
	s.Write()
	collide(t, s)
	// The plank spans x 156 to 356; the floor is row 513, in chunk row 8.
	for cx := 156 / world.ChunkSize; cx <= 355/world.ChunkSize; cx++ {
		if ch := &s.terrain[8*s.cols+cx]; !ch.built || ch.body == 0 {
			t.Fatalf("the floor in chunk (%d, 8) is not collision when the plank is let go", cx)
		}
	}
	w.Step()
	if err := s.Step(); err != nil {
		t.Fatal(err)
	}
	if s.bodies[0].frozen {
		t.Fatal("the plank stays frozen with the block back")
	}
}

// Taking out every block at once, as a save does, records each body as it
// was before any left: a sleeping box on a plank that leaves with the first
// block is saved asleep with the second. Found by the fifth Codex review of
// body saves (2026-11-09).
func TestUnloadBlocksKeepsSleepAcrossBlocks(t *testing.T) {
	s, w, id := newStreamedSystem(t, 2, 1)
	floor := w.Height() - 4
	drop(t, s, id.wood, 300, floor-6, 440, 6)  // touches both blocks
	drop(t, s, id.wood, 600, floor-18, 16, 12) // on it, in block 1 alone
	for range 300 {
		tick(t, s, w)
	}
	for i := range s.bodies {
		if s.phys.Awake(s.bodies[i].id) {
			t.Fatalf("body %d did not fall asleep", i)
		}
	}
	out := s.UnloadBlocks([][2]int{{0, 0}, {1, 0}})
	if len(out) != 2 || len(out[0]) != 1 || len(out[1]) != 1 || len(s.bodies) != 0 {
		t.Fatalf("blocks took %v bodies, %d stayed; want the plank with block 0 and the box with block 1", out, len(s.bodies))
	}
	if out[0][0].Key != 0 || out[1][0].Key != 1 || out[0][0].Awake || out[1][0].Awake {
		t.Fatalf("records %+v and %+v; want the plank and the box, both asleep", out[0][0], out[1][0])
	}
}

// A body made again asleep on another falls when the one under it is dug
// away whole: made asleep, the two have no touching contact for Box2D to
// wake the upper one by, so the bodies next to one that is removed are
// woken. Found by the sixth Codex review of body saves (2026-11-09).
func TestRestoredStackFallsWhenTheBottomIsDugAway(t *testing.T) {
	s, w, id := newStreamedSystem(t, 1, 1)
	floor := w.Height() - 4
	drop(t, s, id.wood, 200, floor-12, 16, 12)
	drop(t, s, id.wood, 202, floor-24, 12, 12)
	for range 300 {
		tick(t, s, w)
	}
	recs := s.Unload(0, 0, nil)
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tick(t, s, w)
	}
	if s.phys.Awake(s.bodies[1].id) {
		t.Fatal("the upper box woke by itself; the test needs it asleep")
	}
	_, r := owned(w, id.wood, 0)
	var cells [][2]int
	for y := r.y0; y < r.y1; y++ {
		for x := r.x0; x < r.x1; x++ {
			if c, _ := w.At(x, y); s.kinds[c.Mat] == material.Solid {
				if b, _ := c.Body(); b == 0 {
					cells = append(cells, [2]int{x, y})
				}
			}
		}
	}
	breakOff(t, s, cells...)
	if len(s.bodies) != 1 {
		t.Fatalf("%d bodies after digging the bottom one away", len(s.bodies))
	}
	top := s.bodies[0].box.y0
	for range 60 {
		tick(t, s, w)
	}
	if b := &s.bodies[0]; b.box.y0 <= top {
		t.Fatalf("the upper box stays at row %d with nothing under it", b.box.y0)
	}
}
