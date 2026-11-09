package world

import (
	"cmp"
	"fmt"
	"math"
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

// candidate is a chunk that would run this tick, as hold ranks it.
type candidate struct {
	priority uint64
	waited   uint32
	i        int
}

// byRank orders candidates for the budget: lower priority first, then the
// one that has waited longer, then the lower chunk index, so the order is
// the same on every run.
func byRank(a, b candidate) int {
	switch {
	case a.priority != b.priority:
		return cmp.Compare(a.priority, b.priority)
	case a.waited != b.waited:
		return cmp.Compare(b.waited, a.waited)
	}
	return cmp.Compare(a.i, b.i)
}

// halvings caps how many times a wait halves a chunk's distance: past it
// the distance is 0, and the wait alone decides.
const halvings = 63

// hold picks the chunks that wait this tick. When the chunks that would
// run hold more cells to scan than the budget, they are taken nearest the
// focus first, each that fits what is left of the budget, and the rest
// wait. The first chunk runs even if it alone is bigger than the budget,
// so the world always moves on.
//
// The distance a chunk is ranked by halves for every tick in a row it has
// waited, so a far chunk comes before the near ones within a few dozen
// ticks however busy they are. Among equal distances the chunk that has
// waited longer goes first; once every distance is 0 they take turns, so
// any number of busy chunks gets through. Chunks woken later in the tick
// that were not made to wait run as they would without a budget.
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
				w.order = append(w.order, candidate{priority: w.priority(c), waited: w.waited[c.i], i: c.i})
			}
		}
	}
	if total <= w.budget {
		return
	}
	// The chunk under the focus has priority 0 however often it runs; one
	// that waits reaches 0 too and then comes first
	// (TestBudgetLetsWaitingChunksPassTheFocusChunk).
	slices.SortFunc(w.order, byRank)
	used := 0
	for k, o := range w.order {
		if cost := w.scanned(&w.chunks[o.i]).area(); k == 0 || used+cost <= w.budget {
			used += cost
			continue
		}
		w.held[o.i] = w.tick
		if w.waited[o.i] < maxWaited {
			w.waited[o.i]++
		}
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
	return d >> min(w.waited[c.i], halvings)
}

// maxWaited caps how many ticks in a row a chunk's wait is counted, far
// more than any chunk waits.
const maxWaited = math.MaxUint32

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
	// budget, at most math.MaxUint32.
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
		w.dirty[i], w.keep[i], w.waited[i] = rectOf(s.Dirty), rectOf(s.Keep), uint32(s.Waited)
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

// ChangeRect returns the change rectangle of chunk (cx, cy): the cells
// [x0, x1) × [y0, y1) its next run scans. It is empty, x0 == x1, while the
// chunk sleeps. It is for drawing and changes nothing.
func (w *World) ChangeRect(cx, cy int) (x0, y0, x1, y1 int) {
	r := w.dirty[cy*w.cols+cx]
	if r.empty() {
		return 0, 0, 0, 0
	}
	return r.x0, r.y0, r.x1, r.y1
}

// Held reports whether chunk (cx, cy) waited in the last tick: it had
// cells to scan but was over the budget. It is for drawing.
func (w *World) Held(cx, cy int) bool {
	return w.tick > 0 && w.held[cy*w.cols+cx] == w.tick
}
