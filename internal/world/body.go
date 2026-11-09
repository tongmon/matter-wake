package world

import (
	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
)

// BodyCell returns the cell that is pixel pixel of body body, of material m
// (a solid), tone tone and damage damage (Wear), which the pixel keeps as
// it moves. The body and the pixel are kept where a moving cell keeps its
// motion (Cell), which a body pixel never has: the bodies move it, the
// cell rules never do.
func BodyCell(m material.ID, tone uint8, damage, body, pixel uint16) Cell {
	return Cell{Mat: m, Tone: tone, Vy: fixed.Q8(body), Ax: fixed.Q8(pixel), Damage: damage}
}

// Body returns the body and the pixel of its image that c is, for a cell
// of a solid material (BodyCell). For other cells it means nothing.
func (c Cell) Body() (body, pixel uint16) {
	return uint16(c.Vy), uint16(c.Ax)
}

// Resting reports whether c is a powder that has ended an update at rest
// and not moved since: neither falling nor new. Other kinds never rest.
func (c Cell) Resting() bool {
	return !c.Falling && c.Rest > 0
}

// Settled reports whether c is a powder that has rested SettleTicks ticks,
// ground that rigid bodies collide with (Classify).
func (c Cell) Settled() bool {
	return !c.Falling && c.Rest >= SettleTicks
}

// Put sets the cell at (x, y) to c between ticks and reports whether it
// could: positions outside the grid or in a block not in memory are left
// alone. Like Displace, it leaves the change to be marked by Flush, so a
// caller that changes many cells together marks them once; Step flushes
// what is left before it runs.
func (w *World) Put(x, y int, c Cell) bool {
	if !w.has(x, y) {
		return false
	}
	*w.cell(w.pos(x, y)) = c
	w.pending.add(rect{x, y, x + 1, y + 1})
	return true
}

// Displace takes the cell at (x, y) out of the grid, leaving air, and puts
// it in the queue of cells to put back (Reinsert). Outside the grid or
// memory it does nothing. The change waits for Flush, as Put's does.
func (w *World) Displace(x, y int) {
	if !w.has(x, y) {
		return
	}
	c := w.cell(w.pos(x, y))
	w.fresh = append(w.fresh, Queued{Cell: *c, X: int32(x), Y: int32(y)})
	*c = Cell{}
	w.pending.add(rect{x, y, x + 1, y + 1})
}

// Flush marks the cells Put and Displace changed since the last Flush, so
// that every cell whose update reads them is updated in the next tick.
func (w *World) Flush() {
	if !w.pending.empty() {
		w.touch(w.pending)
		w.pending = rect{}
	}
}
