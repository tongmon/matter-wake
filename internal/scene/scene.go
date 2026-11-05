// Package scene builds the worlds a session starts in.
package scene

import (
	"fmt"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

// Sandbox lays out a test room: stone walls and floor, two ledges, a heap
// of sand and a block of water, both still in the air so they fall at the
// start. The layout scales with the world size.
func Sandbox(w *world.World, mats *material.Table) error {
	stone, sand, water, err := lookup(mats)
	if err != nil {
		return err
	}
	width, height := w.Width(), w.Height()
	wall := max(width/120, 2)
	w.Fill(0, height-wall, width, height, stone)
	w.Fill(0, 0, wall, height, stone)
	w.Fill(width-wall, 0, width, height, stone)
	w.Fill(width/8, height*55/100, width*3/8, height*55/100+wall, stone)
	w.Fill(width*5/8, height*70/100, width*7/8, height*70/100+wall, stone)
	w.Fill(width/6, height/10, width/3, height*3/10, sand)
	w.Fill(width*3/5, height/10, width*4/5, height*3/10, water)
	return nil
}

func lookup(mats *material.Table) (stone, sand, water material.ID, err error) {
	ids := [3]material.ID{}
	for i, name := range [3]string{"stone", "sand", "water"} {
		id, ok := mats.Lookup(name)
		if !ok {
			return 0, 0, 0, fmt.Errorf("scene: the material table has no %q", name)
		}
		ids[i] = id
	}
	return ids[0], ids[1], ids[2], nil
}
