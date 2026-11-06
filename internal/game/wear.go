package game

import (
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

// wearTable keeps the damage dealt to cells that are being dug. It is the
// one place that knows where that damage is stored and how long it lasts,
// so changing either changes only this file. 🔶 The developer has not yet
// chosen where damage should live (design.md 5절).
//
// Now it holds the damage of the cells under the brush in the last tick
// of digging, nothing else. A tick of digging calls begin, then dealt and
// keep for the cells under the brush in increasing cell order, then end:
// a cell not kept loses its damage. Letting go of the button calls forget.
//
// A cell is told apart from one that later takes its place by its material
// and its tone, which a cell keeps while it moves: a powder may fall away
// and another fall in.
type wearTable struct {
	// worn holds the damage of the last tick, in increasing cell order;
	// next is filled during a tick. Both are sized for the whole brush, so
	// a tick never allocates.
	worn, next []wear
	// rest is the part of worn not yet passed by dealt.
	rest []wear
}

// wear is the damage one cell has taken.
type wear struct {
	cell   int
	mat    material.ID
	tone   uint8
	damage uint32
}

// newWearTable returns a table for a brush of the given radius.
func newWearTable(radius int) wearTable {
	side := 2*radius + 1
	return wearTable{worn: make([]wear, 0, side*side), next: make([]wear, 0, side*side)}
}

// begin starts a tick of digging.
func (t *wearTable) begin() {
	t.rest, t.next = t.worn, t.next[:0]
}

// dealt returns the damage that c, at index cell of the grid, has taken so
// far. Cells must come in increasing order within a tick.
func (t *wearTable) dealt(cell int, c world.Cell) uint32 {
	for len(t.rest) > 0 && t.rest[0].cell < cell {
		t.rest = t.rest[1:]
	}
	if len(t.rest) > 0 && t.rest[0].cell == cell && t.rest[0].mat == c.Mat && t.rest[0].tone == c.Tone {
		return t.rest[0].damage
	}
	return 0
}

// keep records that c, still standing at index cell, has taken damage.
func (t *wearTable) keep(cell int, c world.Cell, damage uint32) {
	t.next = append(t.next, wear{cell: cell, mat: c.Mat, tone: c.Tone, damage: damage})
}

// end finishes a tick of digging. Cells not kept in it lose their damage.
func (t *wearTable) end() {
	t.worn, t.next = t.next, t.worn
	t.rest = nil
}

// forget drops all damage.
func (t *wearTable) forget() {
	t.worn = t.worn[:0]
}
