// Package rigid puts rigid bodies in the cell grid. A body is a rectangle
// of pixels of a solid material that Box2D (internal/physics) moves; its
// pixels are cells of the world, and the terrain's cells are what it
// collides with.
//
// Each tick runs in this order (docs/briefs/body-grid-coupling.md):
//
//  1. Write: every body that moved since it was written has its cells
//     erased, all bodies first; then those bodies are written again where
//     Box2D's last Step put them, in key order. Every cell whose center
//     falls inside a body's image, mapped back through its transform,
//     takes the pixel the center falls in, so a turned body has no holes;
//     a pixel may fill two cells or none. A cell in the way is pushed into
//     the world's queue of cells to put back (world.Displace) if it flows
//     or moves; static terrain, powder that has come to rest, and the
//     cells of later bodies stay.
//  2. Collide: the terrain near each body is made collision for Box2D, one
//     static body of edge chains per 64×64 chunk, rebuilt when the chunk's
//     cells change what bodies collide with (world.Classify). Settled
//     powder that newly reaches into a body's shape is left out while the
//     body is there, so Box2D does not push the body out of it.
//  3. The world's tick runs the cells, then puts back the queued ones.
//  4. Step pushes each body with what liquid does to it, as in Noita: the
//     drag of the liquid cells its Write pushed away (C42) and the
//     buoyancy of liquid under it (C41). Then it advances Box2D by one
//     tick.
//
// Bodies come from the body tool (Drop) and from the terrain: a dug cell of
// a material that turns into a body, such as ice, turns the cells of that
// material 4-connected to it into one body, static if its solid has an
// anchor (Convert, convert.go). Before Write, the bodies that lost pixels
// break into pieces, of which a static body keeps one static, or shatter
// into powder (Break, destroy.go).
//
// In a big world kept in memory by blocks, a body leaves memory with the
// first block it touches to go and comes back with it (Unload, Load), and
// a body that may reach a block not in memory waits, frozen, until it is
// back (save.go).
//
// Cells are 0.1 m on a side and the y axis points down, in the grid and in
// Box2D alike. Everything here runs on the caller's goroutine, in an order
// fixed by the bodies' keys and the chunks' indexes, so the result does
// not depend on the world's worker count.
package rigid

import (
	"fmt"
	"math"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

const (
	// cellsPerMeter is how many cells make a meter: a cell is 0.1 m, as in
	// Noita (✅ 2026-11-07, design.md 2절).
	cellsPerMeter = 10
	// gravity pulls bodies down, along +y, in m/s². 🧪 Noita's (C41).
	gravity = 12
	// tickSeconds is one tick of the game's 60 Hz.
	tickSeconds = float32(1.0 / 60)
	// subSteps is how many solver steps Box2D splits a tick into. 🧪 Box2D's
	// recommendation at 60 Hz.
	subSteps = 4
	// densityScale turns a solid's density into Box2D's kilograms per
	// square meter: Noita's wood of density 6 weighs 0.6 kg/m² (C31, C05).
	densityScale = 0.1
	// maxBodies and MaxPixels are what a cell's link to its body holds
	// (world.BodyCell): 16 bits each.
	maxBodies = 1 << 16
)

// worldConfig sets up the Box2D world: gravity down, and hits listed from
// the speed a crack starts at.
var worldConfig = physics.Config{Gravity: physics.Vec2{X: 0, Y: gravity}, HitSpeed: crackMin / cellsPerMeter}

// MaxPixels is the most pixels a body's image holds: a cell's link to its
// body numbers them in 16 bits (world.BodyCell).
const MaxPixels = 1 << 16

// System holds the rigid bodies of one world and the terrain collision
// near them. It is not safe for concurrent use. Box2D keeps its worlds in
// a global table without a lock, so make and close Systems from one
// goroutine at a time.
type System struct {
	w     *world.World
	phys  *physics.World
	mats  *material.Table
	kinds []material.Kind
	// bodies holds the bodies by slot, the index their cells link to. Keys
	// grow with the slots, so this is also key order: a body Break
	// removes leaves its slot to the bodies after it. nextKey is the key
	// of the next body made.
	bodies  []body
	nextKey uint32
	// worn lists the pixels Wear wore since the last Break without using
	// them up, as slot<<16 | pixel, so a pixel two cells show wears once a
	// tick. broken is set when a pixel left an image since then.
	worn   []uint32
	broken bool
	// gone lists the cells the bodies Break removed covered, for the next
	// Write to count as freed.
	gone []rect
	// The scratch space of Break: the piece of each pixel of the body it
	// splits, the pixels to visit, the pieces found, their shapes and the
	// polygons for Box2D.
	label  []int32
	queue  []int32
	pieces []piece
	shape  shaper
	polys  physics.Polygons
	// terrain holds the terrain collision of each chunk of the world, row
	// by row; cols is how many chunks a row has.
	terrain []chunk
	cols    int
	// wanted lists the chunks near a body this tick, and stamp, by chunk,
	// the last tick (counted by ticks) that listed it. nearHead holds, by
	// chunk, the first of the bodies near it in nearBody, a list linked by
	// nearNext and ended by -1; it is valid while the chunk's stamp is
	// this tick's.
	wanted   []int
	stamp    []uint32
	ticks    uint32
	nearHead []int32
	nearBody []int32
	nearNext []int32
	// rebuilt lists the chunks whose collision was rebuilt this tick, to
	// wake the bodies near them.
	rebuilt []int
	// trace is the scratch space of contour tracing.
	trace tracer
	// freed lists the cells each body erased in the last Write covered,
	// for that Write and the Collide after it.
	freed []rect
	// cold is freeze's scratch list of the bodies it stops, by slot.
	cold []int32
	// seeds lists the terrain cells destroyed since the last Convert
	// whose material turns into a body. mark and filled are Convert's
	// scratch space: the cells of the window it fills found so far, and
	// the list of them.
	seeds  []seed
	mark   []bool
	filled []int32
	// turned counts, by material, the cells and pixels the bodies turned
	// into another material, less those they turned into it (Turned).
	turned []int
	// spill holds the cells a body turned into cells puts in the queue.
	spill []world.Queued
	// hits and checks are crack's scratch space: the last Step's hits and
	// the contacts of bodies that crack among them. lost counts, by
	// material, the pixels cracks took out (Lost).
	hits   []physics.Hit
	checks []crackCheck
	lost   []int
	// settling is Settle's scratch list of the bodies that turn back into
	// terrain, by slot.
	settling []int
	// scratch holds the edges Box2D gives, for drawing (view.go).
	scratch []physics.Edge
}

// New returns a System for the bodies of w, whose cells are of mats. It
// walls the world's edges in for Box2D, so bodies stay in the world.
func New(w *world.World, mats *material.Table) (*System, error) {
	phys, err := physics.NewWorld(worldConfig)
	if err != nil {
		return nil, fmt.Errorf("rigid: %w", err)
	}
	s := &System{
		w: w, phys: phys, mats: mats, kinds: make([]material.Kind, mats.Len()),
		turned: make([]int, mats.Len()), lost: make([]int, mats.Len()),
	}
	for i := range s.kinds {
		s.kinds[i] = mats.Def(material.ID(i)).Kind
	}
	cols := (w.Width() + world.ChunkSize - 1) / world.ChunkSize
	rows := (w.Height() + world.ChunkSize - 1) / world.ChunkSize
	s.terrain = make([]chunk, cols*rows)
	s.stamp = make([]uint32, cols*rows)
	s.nearHead = make([]int32, cols*rows)
	for i := range s.nearHead {
		s.nearHead[i] = -1
	}
	s.cols = cols
	if err := s.wall(); err != nil {
		phys.Close()
		return nil, err
	}
	return s, nil
}

// wall puts a static box 1 m thick along each edge of the world, outside
// it.
func (s *System) wall() error {
	wm := float32(s.w.Width()) / cellsPerMeter
	hm := float32(s.w.Height()) / cellsPerMeter
	const t = 0.5 // half the thickness
	for _, b := range [...]physics.Box{
		{Center: physics.Vec2{X: -t, Y: hm / 2}, HalfW: t, HalfH: hm/2 + 2*t},
		{Center: physics.Vec2{X: wm + t, Y: hm / 2}, HalfW: t, HalfH: hm/2 + 2*t},
		{Center: physics.Vec2{X: wm / 2, Y: -t}, HalfW: wm/2 + 2*t, HalfH: t},
		{Center: physics.Vec2{X: wm / 2, Y: hm + t}, HalfW: wm/2 + 2*t, HalfH: t},
	} {
		b.Friction = terrainFriction
		if _, err := s.phys.AddStaticBox(b); err != nil {
			return fmt.Errorf("rigid: %w", err)
		}
	}
	return nil
}

// Close frees the System's Box2D world.
func (s *System) Close() {
	s.phys.Close()
}

// Bodies returns how many bodies there are.
func (s *System) Bodies() int {
	return len(s.bodies)
}

// Pixels adds to n[id], for every material id, how many pixels the bodies'
// images hold of it. Every pixel counts once, whatever the grid holds: a
// turned body covers more or fewer cells than it has pixels. n must have
// an entry for every material.
func (s *System) Pixels(n []int) {
	for i := range s.bodies {
		b := &s.bodies[i]
		n[b.mat] += b.n
	}
}

// Drop adds a body of material m, w × h pixels, whose image unturned would
// have its top-left pixel at cell (x, y), turned about its middle by angle
// radians (clockwise on screen, Box2D's sign with y down) and moving at
// (vx, vy) cells per second. It reports false, and adds nothing, when a
// cell under it is outside the world or memory, static, powder or another
// body, or when the world holds as many bodies as it can. Under it are the
// cells whose centers fall inside the turned rectangle, a little widened,
// so it covers every cell Write samples. Liquid and air give way when its
// pixels are written. The body's pixels go into the grid at the next Write.
func (s *System) Drop(m material.ID, x, y, w, h int, angle, vx, vy float32) (bool, error) {
	switch {
	case int(m) >= len(s.kinds) || s.kinds[m] != material.Solid:
		return false, fmt.Errorf("rigid: material %d is not a solid", m)
	case w < 1 || h < 1 || w > MaxPixels || h > MaxPixels || w*h > MaxPixels:
		return false, fmt.Errorf("rigid: a body of %d×%d pixels; it needs 1 to %d", w, h, MaxPixels)
	case !finite(angle) || !finite(vx) || !finite(vy):
		return false, fmt.Errorf("rigid: a body turned by %v moving at %v, %v; each must be finite", angle, vx, vy)
	case len(s.bodies) >= maxBodies:
		return false, nil
	}
	if !s.fits(x, y, w, h, physics.MakeRot(angle)) {
		return false, nil
	}
	if err := s.add(m, x, y, w, h, angle); err != nil {
		return false, err
	}
	if vx != 0 || vy != 0 {
		b := &s.bodies[len(s.bodies)-1]
		s.phys.SetVelocity(b.id, physics.Vec2{X: vx / cellsPerMeter, Y: vy / cellsPerMeter}, 0)
	}
	return true, nil
}

// fits reports whether a body of w × h pixels with the middle of
// rectangle (x, y, w, h), turned by q, may go there: no cell under it is
// outside the world or memory, static, powder or another body. A cell is
// under it when its center falls inside the rectangle widened by
// sampleEdge, as Write samples. Unturned, those are exactly the cells of
// the rectangle. The multiplications are converted one by one, as in
// write.
func (s *System) fits(x, y, w, h int, q physics.Rot) bool {
	co, si := q.C, q.S
	hw, hh := float32(w)/2, float32(h)/2
	mx, my := float32(x)+hw, float32(y)+hh
	ex := float32(abs32(co)*hw) + float32(abs32(si)*hh)
	ey := float32(abs32(si)*hw) + float32(abs32(co)*hh)
	for cy := int(math.Floor(float64(my - ey))); cy <= int(math.Floor(float64(my+ey))); cy++ {
		dy := float32(cy) + 0.5 - my
		for cx := int(math.Floor(float64(mx - ex))); cx <= int(math.Floor(float64(mx+ex))); cx++ {
			dx := float32(cx) + 0.5 - mx
			u, v := float32(co*dx)+float32(si*dy), float32(co*dy)-float32(si*dx)
			if abs32(u) > hw+sampleEdge || abs32(v) > hh+sampleEdge {
				continue
			}
			c, ok := s.w.At(cx, cy)
			if !ok {
				return false
			}
			switch s.kinds[c.Mat] {
			case material.Static, material.Powder, material.Solid:
				return false
			case material.Empty, material.Liquid:
			}
		}
	}
	return true
}

// add makes the body Drop checked, turned by angle radians about its
// center.
func (s *System) add(m material.ID, x, y, w, h int, angle float32) error {
	d := s.mats.Def(m)
	// The center is a whole cell or a half one, so the cell centers start
	// at pixel centers, away from the edges where rounding could go either
	// way.
	id, err := s.phys.AddDynamicBox(physics.Box{
		Center:      physics.Vec2{X: (float32(x) + float32(w)/2) / cellsPerMeter, Y: (float32(y) + float32(h)/2) / cellsPerMeter},
		Angle:       angle,
		HalfW:       float32(w) / (2 * cellsPerMeter),
		HalfH:       float32(h) / (2 * cellsPerMeter),
		Density:     float32(d.Density) * densityScale,
		Friction:    q8(d.Friction),
		Restitution: q8(d.Restitution),
		Hits:        d.Cracks,
	})
	if err != nil {
		return fmt.Errorf("rigid: %w", err)
	}
	slot := len(s.bodies)
	s.bodies = append(s.bodies, newBody(id, s.nextKey, m, w, h, s.w.Seed()))
	s.nextKey++
	b := &s.bodies[slot]
	// A box's center of mass is its center, the origin: lc stays zero,
	// exactly, where Box2D's sum over the box's triangles might round.
	b.mass, b.inertia, b.buoyancy = s.phys.Mass(id), s.phys.Inertia(id), s.pieceBuoyancy(m)
	return nil
}

// pieceBuoyancy returns the buoyancy (C41) of a body of material m, made
// whole or broken off another. 🔶 Every body takes its material's, pieces
// too. In Noita buoyancy belongs to the entity that holds a body, so
// bodies of a material alone and new pieces feel none (C135); ours are
// all bodies of a material, so the shipped wood has none either.
func (s *System) pieceBuoyancy(m material.ID) float32 {
	return q8(s.mats.Def(m).Buoyancy)
}

// q8 converts a material's fixed-point value to Box2D's float.
func q8(v fixed.Q8) float32 {
	return float32(v) / float32(fixed.One)
}

// Write erases the pixels of every body that moved since they were last
// written, then writes them where the bodies are now, in key order. A body
// whose position and rotation are exactly as they were, asleep or
// resting, keeps its cells.
func (s *System) Write() {
	for i := range s.bodies {
		b := &s.bodies[i]
		if b.static && b.written {
			// A static body is where it was made; Box2D need not be asked.
			b.p, b.q = b.wp, b.wq
			continue
		}
		b.p, b.q = s.phys.Transform(b.id)
	}
	s.sync()
}

// sync erases and writes the bodies whose transform p, q is not the one
// they were written at. A body that did not move but has cells other
// bodies keep it out of is written again too, where an erased body freed
// cells near it: they may be its cells again. So a cell covered by bodies
// that move away comes back, even while its body sleeps.
func (s *System) sync() {
	s.freed = append(s.freed[:0], s.gone...)
	s.gone = s.gone[:0]
	for i := range s.bodies {
		b := &s.bodies[i]
		b.moved = !b.written || b.redraw || b.p != b.wp || b.q != b.wq
		if b.moved {
			s.erase(b, uint16(i))
			if b.written {
				s.freed = append(s.freed, b.box)
			}
		} else {
			b.shiftX, b.shiftY = 0, 0
		}
	}
	for i := range s.bodies {
		b := &s.bodies[i]
		if b.moved || b.blocked == 0 {
			continue
		}
		for _, r := range s.freed {
			if r.overlaps(b.box) {
				b.force = true
				break
			}
		}
	}
	for i := range s.bodies {
		b := &s.bodies[i]
		if b.moved || b.force {
			s.write(b, uint16(i))
		}
	}
}

// Step stops the bodies that may reach a block not in memory and lets go
// of those that no longer may (freeze), pushes each other dynamic body with
// what liquid does to it (push), advances Box2D by one tick, then cracks
// the bodies of materials that crack where they hit something fast
// (crack.go) and breaks them (Break): pieces too small to be bodies, and
// powder of a shattered body with nowhere to go, count as lost.
func (s *System) Step() error {
	s.freeze()
	for i := range s.bodies {
		b := &s.bodies[i]
		if b.frozen || b.static {
			b.dragN, b.dragX, b.dragY, b.dragR2 = 0, 0, 0, 0
		} else if err := s.push(b); err != nil {
			return err
		}
		b.haveV = false
	}
	s.phys.Step(tickSeconds, subSteps)
	s.crack()
	// The bodies cracks took pixels out of split, or shatter, before the
	// tick ends: a block that leaves memory or a save before the next one
	// records them as they are (Codex review). The pieces too small to be
	// bodies are lost with the crack.
	return s.Break(s.lost)
}
