package world

import (
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// A powder counts the ticks it ends at rest, up to SettleTicks, and starts
// over when it moves.
func TestRestCountsTicksAtRest(t *testing.T) {
	w, ids := newTestWorld(t, 64, 16, 1)
	w.Fill(0, 15, 64, 16, ids.stone)
	w.Fill(10, 14, 13, 15, ids.sand) // three grains side by side
	for tick := 1; tick <= SettleTicks+5; tick++ {
		w.Step()
		c, _ := w.At(11, 14)
		if want := uint8(min(tick, SettleTicks)); c.Rest != want || !c.Resting() {
			t.Fatalf("after %d ticks at rest Rest is %d (resting %v), want %d", tick, c.Rest, c.Resting(), want)
		}
		if settled := tick >= SettleTicks; c.Settled() != settled {
			t.Fatalf("after %d ticks at rest Settled is %v", tick, c.Settled())
		}
	}
	// Take the floor away: the middle grain falls and starts over.
	w.Set(11, 15, material.Air)
	w.Step()
	c, _ := w.At(11, 15)
	if c.Mat != ids.sand || c.Rest != 0 || c.Resting() {
		t.Fatalf("the grain that fell is %+v, want sand with Rest 0", c)
	}
}

// A new powder has not rested yet, and other kinds never rest.
func TestOnlyPowdersRest(t *testing.T) {
	w, ids := newTestWorld(t, 16, 8, 1)
	w.Fill(0, 7, 16, 8, ids.stone)
	w.Set(3, 6, ids.sand)
	w.Fill(8, 6, 16, 7, ids.water)
	if c, _ := w.At(3, 6); c.Resting() {
		t.Fatal("a powder that was just set is resting")
	}
	for range 2 * SettleTicks {
		w.Step()
	}
	for x := range 16 {
		for y := range 7 {
			if c, _ := w.At(x, y); c.Mat != ids.sand && c.Rest != 0 {
				t.Fatalf("%+v at (%d, %d) has Rest", c, x, y)
			}
		}
	}
}

// A body pixel keeps its link to its body and pixel, and the cell rules
// neither move it nor move into it: sand rests on it and water flows
// around it.
func TestBodyCellsStayPut(t *testing.T) {
	w, ids := newTestWorld(t, 32, 16, 1)
	w.Fill(0, 15, 32, 16, ids.stone)
	for x := 10; x < 20; x++ {
		w.Put(x, 8, BodyCell(ids.wood, 7, 513, uint16(x)))
	}
	w.Flush()
	w.Fill(12, 0, 16, 4, ids.sand)
	w.Fill(5, 0, 8, 3, ids.water)
	for range 200 {
		w.Step()
	}
	for x := 10; x < 20; x++ {
		c, _ := w.At(x, 8)
		if body, pixel := c.Body(); c.Mat != ids.wood || c.Tone != 7 || body != 513 || int(pixel) != x {
			t.Fatalf("the body cell at (%d, 8) is %+v (body %d, pixel %d)", x, c, body, pixel)
		}
	}
	if c, _ := w.At(14, 7); c.Mat != ids.sand {
		t.Fatalf("no sand rests on the body: (14, 7) holds %d", c.Mat)
	}
}

// Put and Displace leave their changes unmarked until Flush; Step flushes
// what is left, so nothing is missed.
func TestPutWaitsForFlush(t *testing.T) {
	w, ids := newTestWorld(t, 64, 64, 1)
	w.Fill(0, 63, 64, 64, ids.stone)
	settle(t, w)
	v := w.Version()
	w.Put(30, 10, Cell{Mat: ids.sand})
	if w.Version() != v {
		t.Fatal("Put marked its change at once")
	}
	w.Flush()
	if w.Version() == v {
		t.Fatal("Flush did not mark the change")
	}
	w.Put(31, 10, Cell{Mat: ids.sand})
	w.Step()
	for _, x := range []int{30, 31} {
		if c, _ := w.At(x, 10); c.Mat == ids.sand {
			t.Fatalf("the sand put at (%d, 10) did not fall", x)
		}
	}
}

// A displaced cell goes back where it was taken from when that place is
// free again after the tick.
func TestDisplacedCellGoesBack(t *testing.T) {
	w, ids := newTestWorld(t, 16, 16, 1)
	w.Fill(0, 15, 16, 16, ids.stone)
	w.Set(5, 14, ids.water)
	w.Displace(5, 14)
	if c, _ := w.At(5, 14); c.Mat != material.Air || w.QueueLen() != 1 {
		t.Fatalf("after Displace the cell is %d and the queue holds %d", c.Mat, w.QueueLen())
	}
	w.Step()
	if c, _ := w.At(5, 14); c.Mat != ids.water || w.QueueLen() != 0 {
		t.Fatalf("after the tick (5, 14) holds %d and the queue %d, want the water back", c.Mat, w.QueueLen())
	}
}

// The search goes up from where the cell was: it passes cells as heavy or
// heavier and static terrain, pushes a lighter cell out to take its place,
// and stops at a body's pixel.
func TestReinsertSearchesUp(t *testing.T) {
	tests := []struct {
		name string
		// column lists the cells from the start up, then air.
		column []string
		mover  string
		// want is how many rows up the mover lands, -1 for not at all;
		// pushed is the material pushed into the queue in its place.
		want   int
		pushed string
	}{
		{"free start", nil, "water", 0, ""},
		{"through water", []string{"water", "water"}, "water", 2, ""},
		{"through stone", []string{"stone", "sand"}, "water", 2, ""},
		{"pushes lighter", []string{"water"}, "sand", 0, "water"},
		{"stops at a body", []string{"water", "wood"}, "water", -1, ""},
		{"body at the start", []string{"wood"}, "sand", -1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := newTestWorld(t, 8, 200, 1)
			tab := testTable(t, nil)
			const x, y = 3, 150
			for k, name := range tt.column {
				id := tab.MustLookup(name)
				if tab.Def(id).Kind == material.Solid {
					w.Put(x, y-k, BodyCell(id, 0, 0, 0))
				} else {
					w.Put(x, y-k, Cell{Mat: id})
				}
			}
			w.fresh = append(w.fresh, queued{c: Cell{Mat: tab.MustLookup(tt.mover)}, x: x, y: y})
			s := rng.New(1)
			var pushed []queued
			got := w.reinsert(w.fresh[0], &s, &pushed)
			if got != (tt.want >= 0) {
				t.Fatalf("reinsert reported %v", got)
			}
			if tt.want >= 0 {
				if c, _ := w.At(x, y-tt.want); c.Mat != tab.MustLookup(tt.mover) {
					t.Fatalf("(%d, %d) holds %q, want the %s", x, y-tt.want, tab.Def(c.Mat).Name, tt.mover)
				}
			}
			switch {
			case tt.pushed == "" && len(pushed) != 0:
				t.Fatalf("pushed %v", pushed)
			case tt.pushed != "" && (len(pushed) != 1 || pushed[0].c.Mat != tab.MustLookup(tt.pushed) || pushed[0].x != x || pushed[0].y != y-int32(tt.want)):
				t.Fatalf("pushed %v, want the %s from where the mover landed", pushed, tt.pushed)
			}
		})
	}
}

// The search looks 129 rows up, the start included, and no further.
func TestReinsertLooks129Rows(t *testing.T) {
	for _, rows := range []int{128, 129} {
		w, ids := newTestWorld(t, 400, 300, 1)
		const x, y = 200, 250
		// Rows of stone the search passes, wall to wall so a drift aside
		// stays on stone, with air just above them.
		w.Fill(0, y-rows+1, 400, y+1, ids.stone)
		s := rng.New(1)
		var pushed []queued
		got := w.reinsert(queued{c: Cell{Mat: ids.water}, x: x, y: y}, &s, &pushed)
		if want := rows < reinsertRows; got != want {
			t.Fatalf("above %d rows of stone the search found a place: %v, want %v", rows, got, want)
		}
	}
}

// A cell that fails waits in the retry list. While every retry fails, the
// budget halves from 500 down to no more than 20, as Noita's does (C114),
// and it is full again once one succeeds.
func TestRetryBudget(t *testing.T) {
	w, ids := newTestWorld(t, 600, 4, 1)
	for x := range 501 {
		w.Put(x, 2, BodyCell(ids.wood, 0, 0, uint16(x)))
		w.fresh = append(w.fresh, queued{c: Cell{Mat: ids.water}, x: int32(x), y: 2})
	}
	w.Step()
	if len(w.retry) != 501 || len(w.fresh) != 0 || w.retryBudget != retryFull {
		t.Fatalf("after the first tick %d retry, %d fresh, budget %d", len(w.retry), len(w.fresh), w.retryBudget)
	}
	var budgets []int
	for range 7 {
		w.Step()
		budgets = append(budgets, w.retryBudget)
	}
	if want := []int{250, 125, 62, 31, 15, 15, 15}; !slices.Equal(budgets, want) {
		t.Fatalf("budgets %v, want %v", budgets, want)
	}
	w.Put(0, 2, Cell{})
	for range 40 {
		w.Step()
		if len(w.retry) == 500 {
			break
		}
	}
	if len(w.retry) != 500 || w.retryBudget != retryFull {
		t.Fatalf("after one place opened %d cells wait with budget %d, want 500 with %d", len(w.retry), w.retryBudget, retryFull)
	}
}

// Whatever is displaced, the cells in the grid plus those in the queue
// stay the same, tick after tick, and the result is the same for any
// number of workers.
func TestQueueKeepsEveryCell(t *testing.T) {
	run := func(workers int) uint64 {
		w, ids := newTestWorld(t, 200, 150, workers)
		w.Fill(0, 145, 200, 150, ids.stone)
		w.Fill(20, 60, 180, 100, ids.water)
		w.Fill(60, 20, 140, 60, ids.sand)
		start := counted(w)
		s := rng.New(9)
		for tick := range 120 {
			for range 40 {
				x, y := s.IntN(200), s.IntN(145)
				if c, _ := w.At(x, y); c.Mat == ids.sand || c.Mat == ids.water {
					w.Displace(x, y)
				}
			}
			w.Step()
			if got := counted(w); !slices.Equal(got, start) {
				t.Fatalf("%d workers, tick %d: cells plus queue %v, want %v", workers, tick, got, start)
			}
		}
		return w.Checksum()
	}
	if one, four := run(1), run(4); one != four {
		t.Fatalf("1 worker %016x, 4 workers %016x", one, four)
	}
}

func counted(w *World) []int {
	n := make([]int, len(testDefs()))
	w.Counts(n)
	w.Queued(n)
	n[material.Air] = 0
	return n
}

// Classify sorts the cells for rigid bodies: static terrain and what is not
// in memory are fixed, settled powder is settled, the rest is open.
func TestClassify(t *testing.T) {
	w, ids := newTestWorld(t, 16, 8, 1)
	w.Fill(0, 7, 16, 8, ids.stone)
	w.Set(2, 6, ids.sand)
	w.Set(5, 6, ids.water)
	w.Put(8, 6, BodyCell(ids.wood, 0, 0, 0))
	for range SettleTicks - 1 {
		w.Step()
	}
	got := make([]Class, 8)
	classify := func() {
		w.Classify(-1, 6, 5, 7, got[:6])
	}
	classify()
	// -1 is outside, then air, air, sand that has not settled yet, air,
	// air.
	if want := []Class{Fixed, Open, Open, Open, Open, Open}; !slices.Equal(got[:6], want) {
		t.Fatalf("before the sand settled: %v, want %v", got, want)
	}
	w.Step()
	classify()
	if want := []Class{Fixed, Open, Open, Settled, Open, Open}; !slices.Equal(got[:6], want) {
		t.Fatalf("after the sand settled: %v, want %v", got, want)
	}
	w.Classify(5, 6, 9, 8, got[:8])
	if want := []Class{Open, Open, Open, Open, Fixed, Fixed, Fixed, Fixed}; !slices.Equal(got[:8], want) {
		t.Fatalf("water, air, air, body and the stone below: %v, want %v", got[:8], want)
	}
}
