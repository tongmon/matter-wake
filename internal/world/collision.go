package world

import "github.com/tongmon/matter-wake/internal/material"

// Class is how rigid bodies meet a cell of the terrain.
type Class uint8

// The classes, as Noita sorts cells for its terrain collision (C133 in
// docs/briefs/body-grid-coupling.md).
const (
	// Open cells are passed through: air, liquids, powders that move or
	// have not settled, and the pixels of bodies, which collide as bodies.
	Open Class = iota
	// Fixed cells are static terrain. Cells outside the world or in a
	// block not in memory count as fixed, so bodies stay where the cells
	// are.
	Fixed
	// Settled cells are powders that have rested SettleTicks ticks.
	Settled
)

// Classify writes the class of every cell of [x0, x1) × [y0, y1) to dst,
// row by row. dst must hold (x1-x0)·(y1-y0) entries. It reads each row a
// block's run at a time.
func (w *World) Classify(x0, y0, x1, y1 int, dst []Class) {
	k := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; {
			if !w.has(x, y) {
				dst[k] = Fixed
				k, x = k+1, x+1
				continue
			}
			end := min(x1, (x>>blockShift+1)<<blockShift, w.width)
			for _, c := range w.Row(x, end, y) {
				dst[k] = w.class(&c)
				k++
			}
			x = end
		}
	}
}

func (w *World) class(c *Cell) Class {
	switch w.mats[c.Mat].kind {
	case material.Static:
		return Fixed
	case material.Powder:
		if c.Settled() {
			return Settled
		}
	case material.Empty, material.Liquid, material.Solid:
	}
	return Open
}
