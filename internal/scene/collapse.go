package scene

import (
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// The collapse room's fill: the percent of its cells that are sand and
// water. The rest is air (🧪).
const (
	collapseSand  = 30
	collapseWater = 30
)

// Collapse lays out the heaviest load a screen can hold, as after a large
// explosion or cave-in: a room with stone walls and floor, everything
// inside them sand, water or air at random from seed, so it all falls at
// once at the start (design.md 2절). The layout scales with the world
// size.
func Collapse(w *world.World, mats *material.Table, seed uint64) error {
	stone, sand, water, err := lookup(mats)
	if err != nil {
		return err
	}
	width, height := w.Width(), w.Height()
	wall := max(width/120, 2)
	w.Fill(0, height-wall, width, height, stone)
	w.Fill(0, 0, wall, height, stone)
	w.Fill(width-wall, 0, width, height, stone)
	s := rng.New(seed)
	for y := range height - wall {
		for x := wall; x < width-wall; x++ {
			switch n := s.IntN(100); {
			case n < collapseSand:
				w.Set(x, y, sand)
			case n < collapseSand+collapseWater:
				w.Set(x, y, water)
			}
		}
	}
	return nil
}
