package world

import (
	"fmt"
	"slices"
)

// SetFocus sets the cell the budget measures distance from, the middle of
// what the player sees. Until it is set, the focus is the middle of the
// world.
func (w *World) SetFocus(x, y int) {
	w.focusX, w.focusY = x, y
}

// Waiting returns how many chunks the last tick made wait: they had cells
// to scan but were over the budget.
func (w *World) Waiting() int { return w.waiting }

// scanned returns the cells chunk c would scan if it ran now.
func (w *World) scanned(c *chunk) rect {
	if w.scanAll {
		return c.cells
	}
	return w.dirty[c.i]
}

// hold sorts one entry a chunk: its priority, then how long it has waited
// (longest first), then its index. maxPriority and maxChunks keep the
// parts apart.
const (
	maxPriority = 1<<31 - 1
	waitShift   = 24
	maxChunks   = 1 << waitShift
	prioShift   = 32
)

// hold picks the chunks that wait this tick. When the chunks that would
// run hold more cells to scan than the budget, they run nearest the focus
// first until the budget is spent, and the rest wait. A chunk that would
// otherwise come past the budget still runs if it is the first, so the
// world always moves on.
//
// The distance a chunk is ranked by halves for every tick in a row it has
// waited, so a far chunk comes before the near ones within a few dozen
// ticks however busy they are. Chunks woken later in the tick that were
// not made to wait run as they would without a budget.
func (w *World) hold() {
	total := 0
	w.order = w.order[:0]
	for _, b := range w.resident {
		bx, by := b%w.bcols, b/w.bcols
		for cy := by * blockChunks; cy < min((by+1)*blockChunks, w.rows); cy++ {
			for cx := bx * blockChunks; cx < min((bx+1)*blockChunks, w.cols); cx++ {
				c := &w.chunks[cy*w.cols+cx]
				r := w.scanned(c)
				if r.empty() || !w.reachable(c) {
					continue
				}
				total += r.area()
				w.order = append(w.order, w.priority(c)<<prioShift|uint64(maxWaited-w.waited[c.i])<<waitShift|uint64(c.i))
			}
		}
	}
	if total <= w.budget {
		return
	}
	// Ties go to the chunk that has waited longer, then to the lower chunk
	// index, so the order is the same on every run. The chunk under the
	// focus has priority 0 however often it runs; one that waits reaches 0
	// too and then comes first (TestBudgetLetsWaitingChunksPassTheFocusChunk).
	slices.Sort(w.order)
	used := 0
	for _, o := range w.order {
		i := int(o & (maxChunks - 1))
		if used < w.budget {
			used += w.scanned(&w.chunks[i]).area()
			continue
		}
		w.held[i] = w.tick
		w.waited[i] = min(w.waited[i]+1, maxWaited)
		w.waiting++
	}
}

// priority ranks chunk c for the budget, lower first: the squared distance
// from the focus to the middle of the chunk, halved for each tick in a row
// the chunk has waited.
func (w *World) priority(c *chunk) uint64 {
	dx := (c.cells.x0+c.cells.x1)/2 - w.focusX
	dy := (c.cells.y0+c.cells.y1)/2 - w.focusY
	d := uint64(dx)*uint64(dx) + uint64(dy)*uint64(dy)
	return min(d>>w.waited[c.i], maxPriority)
}

// maxWaited caps how many ticks in a row a chunk's wait is counted.
const maxWaited = 63

// ChunkState is where one chunk stands between ticks: what it has left to
// scan and how long it has waited for the budget. A save keeps it, so a
// world loaded again goes on as the saved one would have (SetChunkStates).
type ChunkState struct {
	CX, CY int
	// Dirty is what the chunk's next run scans, and Keep what was marked
	// in it earlier in a tick than it ran, which the run after scans
	// again. Each is x0, y0, x1, y1, half-open, all zero when empty.
	Dirty, Keep [4]int
	// Waited is how many ticks in a row the chunk has waited for the
	// budget, at most 63.
	Waited int
}

// ChunkStates returns the state of each chunk of the blocks in memory that
// has something to scan or has waited, in chunk order. The others have
// nothing to scan.
func (w *World) ChunkStates() []ChunkState {
	var s []ChunkState
	for _, i := range w.residentChunks() {
		d, k, n := w.dirty[i], w.keep[i], w.waited[i]
		if d.empty() && k.empty() && n == 0 {
			continue
		}
		c := &w.chunks[i]
		s = append(s, ChunkState{CX: c.cx, CY: c.cy, Dirty: d.corners(), Keep: k.corners(), Waited: int(n)})
	}
	return s
}

// SetChunkStates gives the chunks of the blocks in memory the states
// listed, and the others nothing to scan, as ChunkStates returned them
// for a saved world. Loading blocks wakes them whole; this puts back what
// the saved world had instead. It changes nothing if a state is not for a
// chunk in memory or does not fit it.
func (w *World) SetChunkStates(states []ChunkState) error {
	seen := make(map[int]bool, len(states))
	for _, s := range states {
		if s.CX < 0 || s.CY < 0 || s.CX >= w.cols || s.CY >= w.rows {
			return fmt.Errorf("world: chunk (%d, %d) is outside the grid", s.CX, s.CY)
		}
		c := &w.chunks[s.CY*w.cols+s.CX]
		switch {
		case w.blocks[(s.CY/blockChunks)*w.bcols+s.CX/blockChunks] == nil:
			return fmt.Errorf("world: chunk (%d, %d) is not in memory", s.CX, s.CY)
		case !fits(s.Dirty, c.cells) || !fits(s.Keep, c.cells):
			return fmt.Errorf("world: chunk (%d, %d) has a rectangle outside it", s.CX, s.CY)
		case s.Waited < 0 || s.Waited > maxWaited:
			return fmt.Errorf("world: chunk (%d, %d) waited %d ticks, want 0 to %d", s.CX, s.CY, s.Waited, maxWaited)
		case seen[c.i]:
			return fmt.Errorf("world: chunk (%d, %d) is listed twice", s.CX, s.CY)
		}
		seen[c.i] = true
	}
	for _, i := range w.residentChunks() {
		w.dirty[i], w.keep[i], w.waited[i] = rect{}, rect{}, 0
	}
	for _, s := range states {
		i := s.CY*w.cols + s.CX
		w.dirty[i], w.keep[i], w.waited[i] = rectOf(s.Dirty), rectOf(s.Keep), uint8(s.Waited)
	}
	return nil
}

// residentChunks returns the indexes of the chunks of the blocks in
// memory, in order.
func (w *World) residentChunks() []int {
	var out []int
	for _, b := range w.resident {
		bx, by := b%w.bcols, b/w.bcols
		for cy := by * blockChunks; cy < min((by+1)*blockChunks, w.rows); cy++ {
			for cx := bx * blockChunks; cx < min((bx+1)*blockChunks, w.cols); cx++ {
				out = append(out, cy*w.cols+cx)
			}
		}
	}
	slices.Sort(out)
	return out
}

// fits reports whether corners a are empty or inside cells.
func fits(a [4]int, cells rect) bool {
	r := rectOf(a)
	return r.empty() || r.clip(cells) == r
}
