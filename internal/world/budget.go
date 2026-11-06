package world

import "slices"

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

// maxPriority caps a priority so it fits above a chunk index in hold's
// entries.
const maxPriority = 1<<31 - 1

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
				w.order = append(w.order, w.priority(c)<<32|uint64(c.i))
			}
		}
	}
	if total <= w.budget {
		return
	}
	// Ties go to the lower chunk index, so the order is the same on every
	// run.
	slices.Sort(w.order)
	used := 0
	for _, o := range w.order {
		i := int(o & (1<<32 - 1))
		if used < w.budget {
			used += w.scanned(&w.chunks[i]).area()
			continue
		}
		w.held[i] = w.tick
		w.waited[i] = min(w.waited[i]+1, 63)
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
