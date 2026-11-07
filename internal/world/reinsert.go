package world

import (
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// The queue of cells to put back follows Noita's (C50, C114, C115 in
// docs/briefs/body-grid-coupling.md). Its numbers are Noita's.
const (
	// reinsertRows is how many rows a search looks at, from the cell's own
	// row up.
	reinsertRows = 129
	// reinsertDrift is how many rows up a search goes straight before an
	// obstacle may push it a column aside.
	reinsertDrift = 6
	// The retry list's budget: how many of its cells a tick tries. It is
	// full after any success or new failure, halves while above its floor
	// after a tick with none, and is capped while new cells wait too.
	retryFull  = 500
	retryFloor = 20
	retryCap   = 300
)

// reinsertSalt keys the queue's random numbers apart from the chunks'.
const reinsertSalt = 0xbb67ae8584caa73b

// queued is a cell out of the grid waiting to go back, and where it was
// taken from.
type queued struct {
	c    Cell
	x, y int32
}

// Queued adds to n[id], for every material id, how many cells of it wait
// in the queue to be put back. n must have an entry for every material.
func (w *World) Queued(n []int) {
	for _, q := range w.fresh {
		n[q.c.Mat]++
	}
	for _, q := range w.retry {
		n[q.c.Mat]++
	}
}

// QueueLen returns how many cells wait in the queue to be put back.
func (w *World) QueueLen() int {
	return len(w.fresh) + len(w.retry)
}

// reinsertQueued tries to put the queued cells back, after a tick's cell
// updates. First the cells that failed before (retry): as many as the
// budget allows, from a random place in the list on. Then every cell new
// to the queue (fresh); those that fail join the retry list. A cell that a
// heavier one pushes out of its place joins the retry list at once if a
// retry pushed it, so it may go back in this same tick, and the next
// tick's fresh cells if a fresh one did. The order depends only on the
// queue, the seed and the tick, so the result is the same on every run.
func (w *World) reinsertQueued() {
	if len(w.fresh) == 0 && len(w.retry) == 0 {
		return
	}
	s := rng.New(rng.Hash3(w.seed^reinsertSalt, uint64(w.tick), 0, 0))
	if len(w.retry) > 0 {
		w.retryCells(&s)
	}
	next := w.spare[:0]
	for _, q := range w.fresh {
		if !w.reinsert(q, &s, &next) {
			w.retry = append(w.retry, q)
			w.retryBudget = retryFull
		}
	}
	w.fresh, w.spare = next, w.fresh[:0]
}

// retryCells tries the budget's worth of the retry list. A cell put back
// leaves the list, the last cell taking its place.
func (w *World) retryCells(s *rng.Stream) {
	i := s.IntN(len(w.retry))
	budget := w.retryBudget
	if len(w.fresh) > 0 {
		budget = min(budget, retryCap)
	}
	budget = min(budget, len(w.retry))
	w.retryBudget = budget
	success := false
	for range budget {
		if len(w.retry) == 0 {
			break
		}
		if i >= len(w.retry) {
			i = 0
		}
		if w.reinsert(w.retry[i], s, &w.retry) {
			last := len(w.retry) - 1
			w.retry[i] = w.retry[last]
			w.retry = w.retry[:last]
			success = true
		} else {
			i++
		}
	}
	switch {
	case success:
		w.retryBudget = retryFull
	case w.retryBudget > retryFloor:
		w.retryBudget /= 2
	}
}

// reinsert looks for a place for q from the row it was taken from up,
// reinsertRows rows in all, and puts it there if it finds one: an empty
// cell, or a lighter cell that is neither static nor solid, which it
// pushes into pushed. A rigid body's pixel in the way ends the search, so
// a cell under a body waits until the body moves. Heavier cells and
// static ones are passed, and once the search is reinsertDrift rows up
// each one passed may move it a column aside. Rows outside the grid or
// memory are skipped. It reports whether q went back.
func (w *World) reinsert(q queued, s *rng.Stream, pushed *[]queued) bool {
	x, y0 := int(q.x), int(q.y)
	mover := &w.mats[q.c.Mat]
	for y := y0; y > y0-reinsertRows; y-- {
		if !w.has(x, y) {
			continue
		}
		o := w.cell(w.pos(x, y))
		t := &w.mats[o.Mat]
		switch {
		case t.kind == material.Empty:
			w.put(o, q.c, x, y)
			return true
		case t.kind == material.Solid:
			return false
		case t.kind != material.Static && t.density < mover.density:
			*pushed = append(*pushed, queued{c: *o, x: int32(x), y: int32(y)})
			w.put(o, q.c, x, y)
			return true
		}
		if y <= y0-reinsertDrift && s.IntN(101) < 20 {
			if s.IntN(101) < 50 {
				x++
			} else {
				x--
			}
		}
	}
	return false
}

// put puts c back at o, the cell at (x, y). It has moved, so it is no
// longer at rest.
func (w *World) put(o *Cell, c Cell, x, y int) {
	c.Rest = 0
	*o = c
	w.touch(rect{x, y, x + 1, y + 1})
}
