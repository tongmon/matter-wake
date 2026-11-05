package world

import (
	"testing"
	"unsafe"

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
	for _, start := range []int{1, 30} { // against the left wall, and mid-floor
		w, ids := newTestWorld(t, 60, 2, 1)
		w.Set(0, 1, ids.stone)
		w.Set(start, 1, ids.water)
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
