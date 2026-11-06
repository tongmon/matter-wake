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
	if size := unsafe.Sizeof(Cell{}); size != 16 {
		t.Fatalf("Cell is %d bytes, want 16", size)
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

// A powder that starts to fall from rest moves one cell without speeding
// up; it accelerates from its second tick of falling on.
func TestPowderFirstFallTickDoesNotAccelerate(t *testing.T) {
	w, ids := newTestWorld(t, 8, 20, 1)
	w.Set(3, 0, ids.sand)
	w.Step()
	x, y, _ := find(w, ids.sand)
	if c, _ := w.At(x, y); y != 1 || c.Vy != 0 || !c.Falling {
		t.Fatalf("after its first tick sand is at y=%d with speed %v, falling %v; want y=1, speed 0, falling",
			y, c.Vy, c.Falling)
	}
	w.Step()
	x, y, _ = find(w, ids.sand)
	if c, _ := w.At(x, y); c.Vy != w.mats[ids.sand].fallAccel {
		t.Fatalf("after its second tick sand has speed %v, want one step of acceleration %v",
			c.Vy, w.mats[ids.sand].fallAccel)
	}
}

// A falling powder now and then pauses for a tick, the more often the
// weaker it accelerates: with no acceleration, half the time.
func TestPowderPausesWhileFalling(t *testing.T) {
	w, ids := newTestWorldWith(t, 8, 300, 1, func(d []material.Def) { d[defSand].FallAccel = 0 })
	w.Set(3, 0, ids.sand)
	for range 200 {
		w.Step()
	}
	// Without pauses it would fall one cell every tick.
	if _, y, _ := find(w, ids.sand); y < 60 || y > 140 {
		t.Fatalf("sand without acceleration fell %d cells in 200 ticks, want about 100", y)
	}
}

// A falling powder that lands keeps its speed while it can still move
// along, and comes to rest once both sides are blocked too.
func TestPowderKeepsSpeedUntilItRests(t *testing.T) {
	w, ids := newTestWorld(t, 9, 3, 1)
	w.Fill(0, 2, 9, 3, ids.stone)
	w.Set(4, 1, ids.sand)
	c := &w.cells[w.Width()+4]
	c.Vy, c.Falling = 3*fixed.One, true
	w.Step()
	if got, _ := w.At(4, 1); got.Vy != 3*fixed.One || !got.Falling {
		t.Fatalf("landed sand with open sides has speed %v, falling %v; want speed 3 kept, falling", got.Vy, got.Falling)
	}
	w.Set(3, 1, ids.stone)
	w.Set(5, 1, ids.stone)
	w.Step()
	if got, _ := w.At(4, 1); got.Falling {
		t.Fatal("sand blocked below and on both sides is still falling")
	}
}

// A falling powder whose sideways drift is blocked stops there, instead of
// falling on straight down.
func TestPowderStopsWhenItsDriftIsBlocked(t *testing.T) {
	w, ids := newTestWorld(t, 3, 400, 1)
	w.Fill(0, 0, 1, 400, ids.stone)
	w.Fill(2, 0, 3, 400, ids.stone)
	w.Set(1, 0, ids.sand)
	prev := fixed.Q8(0)
	for range 120 {
		w.Step()
		_, y, _ := find(w, ids.sand)
		c, _ := w.At(1, y)
		if y < w.Height()-1 && prev > fixed.One && c.Vy == 0 {
			return
		}
		prev = c.Vy
	}
	t.Fatal("sand in a one-cell shaft never lost its speed, though its drift keeps hitting the walls")
}

// Cells that cannot move draw no random numbers, so whether they are
// updated does not change anything else in their chunk. Cell sleep will
// rely on this.
func TestRestingCellsDrawNoRandomNumbers(t *testing.T) {
	run := func(fill func(w *World, ids testIDs)) *World {
		w, ids := newTestWorld(t, 64, 64, 1)
		w.Fill(0, 63, 64, 64, ids.stone)
		w.Fill(30, 0, 31, 63, ids.stone)
		fill(w, ids)
		for y := range 30 {
			for x := 36; x < 60; x++ {
				switch (x + y) % 3 {
				case 0:
					w.Set(x, y, ids.sand)
				case 1:
					w.Set(x, y, ids.water)
				}
			}
		}
		for range 150 {
			w.Step()
		}
		return w
	}
	resting := run(func(w *World, ids testIDs) {
		w.Fill(0, 50, 30, 63, ids.sand)
		w.Fill(0, 44, 30, 50, ids.water)
	})
	stone := run(func(w *World, ids testIDs) { w.Fill(0, 44, 30, 63, ids.stone) })
	for y := range 64 {
		for x := 31; x < 64; x++ {
			if a, b := resting.cells[y*64+x], stone.cells[y*64+x]; a != b {
				t.Fatalf("cell (%d, %d) differs when a resting sand bed and pool share the chunk: %+v, %+v", x, y, a, b)
			}
		}
	}
}

// A row's scan direction does not come from its chunk's random stream, so
// rows that hold nothing that can move leave the other rows' random
// numbers alone, however many there are. Chunk sleep, which skips such
// rows, relies on this.
func TestRowsWithNothingToMoveDrawNoRandomNumbers(t *testing.T) {
	run := func(height int) *World {
		w, ids := newTestWorld(t, 64, height, 1)
		// Below the floor the chunk has only air, as many rows as the
		// height leaves.
		w.Fill(0, 30, 64, 31, ids.stone)
		for y := range 30 {
			for x := range 64 {
				switch (x + y) % 3 {
				case 0:
					w.Set(x, y, ids.sand)
				case 1:
					w.Set(x, y, ids.water)
				}
			}
		}
		for range 150 {
			w.Step()
		}
		return w
	}
	short, tall := run(40), run(60)
	for y := range 31 {
		for x := range 64 {
			if a, b := short.cells[y*64+x], tall.cells[y*64+x]; a != b {
				t.Fatalf("cell (%d, %d) differs with 9 or 29 rows of air below the floor: %+v, %+v", x, y, a, b)
			}
		}
	}
}

// Only a cell that moves is stamped with the tick, so a tick leaves a cell
// that stays put exactly as it was, whether it visits the cell or not.
func TestOnlyMovingCellsAreStamped(t *testing.T) {
	w, ids := newTestWorld(t, 8, 8, 1)
	w.Set(1, 7, ids.sand)  // rests on the floor
	w.Set(4, 7, ids.water) // both sides open: rests (TestLiquidWithBothSidesOpenRests)
	w.Set(6, 0, ids.sand)  // falls
	w.Step()
	for _, at := range [][2]int{{1, 7}, {4, 7}} {
		if c, _ := w.At(at[0], at[1]); c.Last != 0 {
			t.Errorf("the resting cell at %v was stamped with tick %d", at, c.Last)
		}
	}
	if c, _ := w.At(6, 1); c.Mat != ids.sand || c.Last != w.Tick() {
		t.Errorf("the sand that fell to (6, 1) is %+v, want sand stamped with tick %d", c, w.Tick())
	}
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
