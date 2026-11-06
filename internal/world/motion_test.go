package world

import (
	"testing"
	"unsafe"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
)

// The tick is bound by memory bandwidth, so a larger cell is a slower
// world. Grow it only on purpose, with before/after benchmarks.
func TestCellSize(t *testing.T) {
	if size := unsafe.Sizeof(Cell{}); size != 12 {
		t.Fatalf("Cell is %d bytes, want 12", size)
	}
}

// One cell update must stay within MaxReach of its chunk, or two chunks of
// one phase could touch the same cell.
func TestMotionStaysWithinReach(t *testing.T) {
	if reach := fallReach(); reach < 1 || reach >= MaxReach {
		t.Fatalf("a fall at maxFall drifts %d cells; it must be in [1, %d)", reach, MaxReach)
	}
	if reach := liquidReach(0); reach > MaxReach {
		t.Fatalf("a liquid that falls and flows reaches %d cells, more than MaxReach %d", reach, MaxReach)
	}
	if _, err := New(Config{Width: 8, Height: 8, Materials: testTable(t, nil)}); err != nil {
		t.Fatalf("the test materials exceed the reach: %v", err)
	}
}

func TestFallingAccelerates(t *testing.T) {
	w, ids := newTestWorld(t, 32, 200, 1)
	w.Set(16, 0, ids.sand)
	landed := -1
	for tick := 1; tick <= 200; tick++ {
		w.Step()
		if _, y, _ := find(w, ids.sand); y == w.Height()-1 {
			landed = tick
			break
		}
	}
	// At one cell per tick the fall would take 199 ticks.
	if landed < 0 || landed > 100 {
		t.Fatalf("sand reached the floor at tick %d, want within 100", landed)
	}
	w.Step()
	x, y, _ := find(w, ids.sand)
	if c, _ := w.At(x, y); c.Vy != 0 {
		t.Fatalf("landed sand keeps speed %v", c.Vy)
	}
}

func TestFallSpeedIsCapped(t *testing.T) {
	w, ids := newTestWorld(t, 64, 1500, 1)
	w.Set(32, 0, ids.sand)
	for range 120 {
		w.Step()
		x, y, ok := find(w, ids.sand)
		if !ok {
			t.Fatal("the sand cell disappeared")
		}
		if c, _ := w.At(x, y); c.Vy > maxFall || c.Vy < 0 {
			t.Fatalf("fall speed %v is outside [0, %v]", c.Vy, maxFall)
		}
	}
}

// A liquid keeps flowing the way it started until something blocks it,
// instead of choosing a new direction every tick.
func TestLiquidKeepsFlowDirection(t *testing.T) {
	for _, tc := range []struct{ start, push int }{
		{1, 0},  // against the left wall, which leaves one side open
		{30, 2}, // mid-floor, already flowing
	} {
		start := tc.start
		w, ids := newTestWorld(t, 60, 2, 1)
		w.Set(0, 1, ids.stone)
		w.Set(start, 1, ids.water)
		w.cells[w.Width()+start].Ax = fixed.FromInt(tc.push)
		prevX, dir := start, 0
		for range 300 {
			w.Step()
			x, _, _ := find(w, ids.water)
			if x == prevX {
				continue
			}
			step := 1
			if x < prevX {
				step = -1
			}
			if dir != 0 && step != dir {
				if prevX != 1 && prevX != w.Width()-1 {
					t.Fatalf("start %d: water turned around at x=%d before reaching a wall", start, prevX)
				}
				break
			}
			dir, prevX = step, x
		}
		if dir == 0 {
			t.Fatalf("start %d: water never flowed", start)
		}
	}
}

func TestLiquidWithoutFlowSpeedStaysPut(t *testing.T) {
	w, ids := newTestWorldWith(t, 20, 2, 1, func(d []material.Def) { d[defWater].FlowSpeed = 0 })
	w.Set(10, 1, ids.water)
	for range 50 {
		w.Step()
	}
	if x, y, _ := find(w, ids.water); x != 10 || y != 1 {
		t.Fatalf("water with no flow speed moved to (%d, %d)", x, y)
	}
}

func TestSinkChance(t *testing.T) {
	setup := func(sink uint8) (*World, testIDs) {
		w, ids := newTestWorldWith(t, 4, 8, 1, func(d []material.Def) { d[defSand].SinkChance = sink })
		w.Fill(0, 4, 4, 8, ids.water)
		w.Fill(0, 3, 4, 4, ids.sand)
		return w, ids
	}
	w, ids := setup(0)
	for range 200 {
		w.Step()
	}
	for x := range 4 {
		if c, _ := w.At(x, 3); c.Mat != ids.sand {
			t.Fatalf("sand with sinkChance 0 sank:\n%s", render(w, ids))
		}
	}
	w, ids = setup(100)
	for range 20 {
		w.Step()
	}
	for x := range 4 {
		if c, _ := w.At(x, w.Height()-1); c.Mat != ids.sand {
			t.Fatalf("sand with sinkChance 100 did not reach the bottom in 20 ticks:\n%s", render(w, ids))
		}
	}
}

func TestSlideChanceZeroStacksAColumn(t *testing.T) {
	// The world is too short for the sand to fall fast enough to drift.
	w, ids := newTestWorldWith(t, 21, 5, 1, func(d []material.Def) { d[defSand].SlideChance = 0 })
	for range 5 {
		w.Set(10, 0, ids.sand)
		w.Step()
		w.Step()
	}
	for i, c := range w.Cells() {
		if c.Mat == ids.sand && i%w.Width() != 10 {
			t.Fatalf("sand with slideChance 0 left its column:\n%s", render(w, ids))
		}
	}
}

// A liquid keeps its sideways push while something blocks it, so it flows
// on the same way once the way is clear.
func TestLiquidKeepsPushWhenBlocked(t *testing.T) {
	w, ids := newTestWorld(t, 5, 1, 1)
	w.Set(1, 0, ids.stone)
	w.Set(3, 0, ids.stone)
	w.Set(2, 0, ids.water)
	push := 2 * fixed.One
	w.cells[2].Ax = push
	w.Step()
	if c, _ := w.At(2, 0); c.Ax != push {
		t.Fatalf("blocked water has push %v, want %v kept", c.Ax, push)
	}
}

// A liquid with both sides open does not build up a sideways push, so a
// lone drop on a flat floor rests where it lands.
func TestLiquidWithBothSidesOpenRests(t *testing.T) {
	w, ids := newTestWorld(t, 9, 1, 1)
	w.Set(4, 0, ids.water)
	for range 50 {
		w.Step()
	}
	x, _, _ := find(w, ids.water)
	if c, _ := w.At(x, 0); x != 4 || c.Ax != 0 {
		t.Fatalf("lone water at x=%d with push %v after 50 ticks, want it at rest at x=4", x, c.Ax)
	}
}

// A liquid that slides down diagonally goes on flowing that way: the slide
// sets its push to one cell in that direction.
func TestLiquidSlideSetsPush(t *testing.T) {
	w, ids := newTestWorldWith(t, 3, 3, 1, func(d []material.Def) { d[defWater].FlowSpeed = 0 })
	w.Set(1, 2, ids.stone)
	w.Set(1, 1, ids.water)
	w.Step()
	x, y, _ := find(w, ids.water)
	c, _ := w.At(x, y)
	if y != 2 || x == 1 || c.Ax != fixed.FromInt(x-1) {
		t.Fatalf("water at (%d, %d) with push %v, want it slid to a lower corner with push %v",
			x, y, c.Ax, fixed.FromInt(x-1))
	}
}

// A falling liquid flows sideways in the tick it lands, instead of resting
// for a tick on whatever it hit.
func TestLiquidFlowsInTheTickItLands(t *testing.T) {
	w, ids := newTestWorld(t, 20, 2, 1)
	w.Set(5, 0, ids.water)
	w.cells[5].Ax = 2 * fixed.One
	w.Step()
	if x, y, _ := find(w, ids.water); y != 1 || x <= 5 {
		t.Fatalf("water at (%d, %d) after one tick, want it on the floor right of x=5", x, y)
	}
}

// Water poured from a height spreads into a low mound. Before the liquid
// rules followed the observed ones, it piled up as high as sand.
func TestPouredWaterSpreadsInsteadOfPiling(t *testing.T) {
	water := pourOnFloor(t, false, 60, 1600)
	sand := pourOnFloor(t, true, 60, 1600)
	if water*2 > sand {
		t.Fatalf("poured water piles %d cells high and sand %d; want water at most half of sand", water, sand)
	}
}

// pourOnFloor paints a disk of sand or water drop cells above a walled stone
// floor each tick, as the sandbox brush does, until volume cells rest on the
// floor or the pile reaches the brush. It returns the pile's height under
// the pour.
func pourOnFloor(t *testing.T, sand bool, drop, volume int) int {
	t.Helper()
	const width, cx, brush = 401, 200, 4
	floor := drop + 10
	w, ids := newTestWorld(t, width, floor+1, 1)
	w.Fill(0, floor, width, floor+1, ids.stone)
	w.Fill(0, 0, 1, floor, ids.stone)
	w.Fill(width-1, 0, width, floor, ids.stone)
	id := ids.water
	if sand {
		id = ids.sand
	}
	column := func(x int) int {
		h := 0
		for y := floor - 1; y >= 0; y-- {
			if c, _ := w.At(x, y); c.Mat != id {
				break
			}
			h++
		}
		return h
	}
	for range 1000 {
		w.Paint(cx, floor-drop, brush, id, true)
		w.Step()
		resting := 0
		for x := 1; x < width-1; x++ {
			resting += column(x)
		}
		if resting >= volume || column(cx) >= drop-brush-1 {
			break
		}
	}
	return column(cx)
}

// Water poured into a basin settles to a nearly flat surface.
func TestWaterLevelsInABasin(t *testing.T) {
	w, ids := newTestWorld(t, 40, 20, 1)
	w.Fill(0, 0, 1, 20, ids.stone)
	w.Fill(39, 0, 40, 20, ids.stone)
	w.Fill(1, 4, 13, 20, ids.water)
	for range 600 {
		w.Step()
	}
	lo, hi := w.Height(), 0
	for x := 1; x < 39; x++ {
		depth := 0
		for y := range w.Height() {
			if c, _ := w.At(x, y); c.Mat == ids.water {
				depth++
			}
		}
		lo, hi = min(lo, depth), max(hi, depth)
	}
	if hi-lo > 1 {
		t.Fatalf("water depth varies from %d to %d across the basin:\n%s", lo, hi, render(w, ids))
	}
}
