package rigid

import (
	"fmt"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

// The bodies of a big world live with its 512×512 blocks (design.md 8절):
// a body belongs to the block that holds its center of mass (owner). When
// a block leaves memory its bodies leave with it (Unload), recorded as
// save.Body, and when it comes back they are made again (Load). A body
// whose next Step may reach a block not in memory waits, frozen, until the
// block is back (freeze).

// owner returns the block that owns b: the one that holds its center of
// mass where Box2D has it now, or, if that block is not in memory, the
// nearest block that is, by whole blocks, the first in row order of
// equally near ones. 🔶 Noita collects "the bodies in the block" when it
// unloads one, and what decides that is not known (C137 may answer).
func (s *System) owner(b *body) [2]int {
	p, q := s.phys.Transform(b.id)
	lx, ly := b.lc.X*cellsPerMeter, b.lc.Y*cellsPerMeter
	cx := float32(p.X*cellsPerMeter) + float32(q.C*lx) - float32(q.S*ly)
	cy := float32(p.Y*cellsPerMeter) + float32(q.S*lx) + float32(q.C*ly)
	cols, rows := s.w.Blocks()
	bx := min(max(int(math.Floor(float64(cx)))>>9, 0), cols-1)
	by := min(max(int(math.Floor(float64(cy)))>>9, 0), rows-1)
	if s.w.Resident(bx, by) {
		return [2]int{bx, by}
	}
	best, bestD := [2]int{bx, by}, -1
	for y := range rows {
		for x := range cols {
			if !s.w.Resident(x, y) {
				continue
			}
			if d := (x-bx)*(x-bx) + (y-by)*(y-by); bestD < 0 || d < bestD {
				best, bestD = [2]int{x, y}, d
			}
		}
	}
	return best
}

// blockCells returns the cells of block (bx, by).
func blockCells(bx, by int) rect {
	return rect{bx * world.BlockSize, by * world.BlockSize, (bx + 1) * world.BlockSize, (by + 1) * world.BlockSize}
}

// Records appends to out a record of every body, in key order, as Unload
// would take it: what a save keeps of it.
func (s *System) Records(out []save.Body) []save.Body {
	for i := range s.bodies {
		out = append(out, s.record(&s.bodies[i]))
	}
	return out
}

// record returns what a save keeps of b: its image, its transform and
// velocities where Box2D has them now, and whether it is awake. A frozen
// body has the velocity it froze with.
func (s *System) record(b *body) save.Body {
	p, q := s.phys.Transform(b.id)
	v, spin := s.motion(b)
	awake := b.fawake
	if !b.frozen {
		awake = s.phys.Awake(b.id)
	}
	r := save.Body{
		Key: b.key, Mat: b.mat, W: b.w, H: b.h, Left: b.left, Top: b.top, CX: b.cx, CY: b.cy,
		Pixels: make([]save.Pixel, len(b.px)), Whole: b.cover == nil,
		X: p.X, Y: p.Y, C: q.C, S: q.S, VX: v.X, VY: v.Y, Spin: spin, Awake: awake,
	}
	for k, px := range b.px {
		r.Pixels[k] = save.Pixel{Damage: px.dmg, Tone: px.tone, OK: px.ok}
	}
	return r
}

// motion returns b's velocity and angular velocity: Box2D's, or for a
// frozen body, which Box2D holds still, the ones it froze with.
func (s *System) motion(b *body) (physics.Vec2, float32) {
	if b.frozen {
		return b.fv, b.fspin
	}
	return s.phys.Velocity(b.id)
}

// Unload takes out the bodies block (bx, by) owns, before its cells leave
// memory: it erases their pixels, wherever they are, appends a record of
// each to out, in key order, and removes them from Box2D. The bodies that
// stay but have pixels in the block are erased too and written again by
// the next Write, outside it, so no body's pixel leaves with the block as
// a cell. 🔶 A body goes with its owner, though it reaches into a block
// that stays. Bodies near those that left are woken: Box2D does not wake
// what rested on a body it removes. The block's terrain collision is
// dropped.
func (s *System) Unload(bx, by int, out []save.Body) []save.Body {
	area := blockCells(bx, by)
	at := [2]int{bx, by}
	start := len(s.gone)
	dead := false
	for i := range s.bodies {
		b := &s.bodies[i]
		switch {
		case s.owner(b) == at:
			out = append(out, s.record(b))
			s.erase(b, uint16(i))
			if b.written {
				s.gone = append(s.gone, b.box)
			}
			s.phys.DestroyBody(b.id)
			b.dead, dead = true, true
		case b.written && b.box.overlaps(area):
			s.erase(b, uint16(i))
			b.redraw = true
		}
	}
	if dead {
		s.compact()
	}
	for i := range s.bodies {
		b := &s.bodies[i]
		for _, r := range s.gone[start:] {
			if b.written && !b.frozen && b.near.overlaps(r) {
				s.phys.Wake(b.id)
				break
			}
		}
	}
	s.dropTerrain(area)
	return out
}

// dropTerrain drops the terrain collision of the chunks in area: a block
// that left memory.
func (s *System) dropTerrain(area rect) {
	x0, y0 := max(area.x0, 0)/world.ChunkSize, max(area.y0, 0)/world.ChunkSize
	x1 := (min(area.x1, s.w.Width()) + world.ChunkSize - 1) / world.ChunkSize
	y1 := (min(area.y1, s.w.Height()) + world.ChunkSize - 1) / world.ChunkSize
	for cy := y0; cy < y1; cy++ {
		for cx := x0; cx < x1; cx++ {
			s.dropChunk(cy*s.cols + cx)
		}
	}
}

// dropChunk forgets the terrain collision of chunk i.
func (s *System) dropChunk(i int) {
	ch := &s.terrain[i]
	if ch.body != 0 {
		s.phys.DestroyBody(ch.body)
	}
	*ch = chunk{}
}

// Load makes again, as they were recorded, the bodies of block (bx, by),
// which has come back into memory, in key order among the bodies there
// are. Their pixels go into the grid at the next Write. A body asleep when
// it left comes back asleep, unless the cells it may touch in a Step reach
// past its block: those may have changed while it was away. A body whose
// key is already in memory is left out: only a save mixed by a crash
// holds one twice. Load fails, adding no body, if a record is not one a
// body can be made of or the keys are not in increasing order. New bodies
// get keys after every key loaded.
func (s *System) Load(bx, by int, recs []save.Body) error {
	for i := range recs {
		if err := s.checkRecord(&recs[i]); err != nil {
			return err
		}
		if i > 0 && recs[i].Key <= recs[i-1].Key {
			return fmt.Errorf("rigid: block (%d, %d) lists body %d after body %d", bx, by, recs[i].Key, recs[i-1].Key)
		}
	}
	if len(s.bodies)+len(recs) > maxBodies {
		return fmt.Errorf("rigid: %d bodies in memory and %d more coming, at most %d", len(s.bodies), len(recs), maxBodies)
	}
	home := blockCells(bx, by)
	made := make([]body, 0, len(recs))
	for i := range recs {
		r := &recs[i]
		if _, held := s.slotOf(r.Key); held {
			continue
		}
		b, err := s.remake(r, home)
		if err != nil {
			for _, m := range made {
				s.phys.DestroyBody(m.id)
			}
			return err
		}
		made = append(made, b)
		s.nextKey = max(s.nextKey, r.Key+1)
	}
	s.insert(made)
	return nil
}

// slotOf returns the slot of the body with key k, and whether there is
// one.
func (s *System) slotOf(k uint32) (int, bool) {
	return slices.BinarySearchFunc(s.bodies, k, func(b body, k uint32) int {
		switch {
		case b.key < k:
			return -1
		case b.key > k:
			return 1
		}
		return 0
	})
}

// insert puts the bodies made, in key order and none of whose keys are
// held, among the bodies at their keys' places. The bodies after a place
// move up a slot for each body put before them, and their cells are linked
// to their new slots, the last body first, so no link moves twice. The
// bodies made have no cells yet.
func (s *System) insert(made []body) {
	if len(made) == 0 {
		return
	}
	old := s.bodies
	merged := make([]body, 0, len(old)+len(made))
	var moves [][2]int
	i, j := 0, 0
	for i < len(old) || j < len(made) {
		if j == len(made) || i < len(old) && old[i].key < made[j].key {
			if len(merged) != i {
				moves = append(moves, [2]int{i, len(merged)})
			}
			merged = append(merged, old[i])
			i++
		} else {
			merged = append(merged, made[j])
			j++
		}
	}
	s.bodies = merged
	for k := len(moves) - 1; k >= 0; k-- {
		m := moves[k]
		s.relink(&s.bodies[m[1]], uint16(m[0]), uint16(m[1]))
	}
	if len(moves) > 0 {
		s.w.Flush()
	}
}

// checkRecord reports what keeps a body from being made of r.
func (s *System) checkRecord(r *save.Body) error {
	n := 0
	for _, p := range r.Pixels {
		if p.OK {
			n++
		}
	}
	wm, hm := float32(s.w.Width())/cellsPerMeter, float32(s.w.Height())/cellsPerMeter
	switch {
	case int(r.Mat) >= len(s.kinds) || s.kinds[r.Mat] != material.Solid:
		return fmt.Errorf("rigid: body %d is of material %d, not a solid", r.Key, r.Mat)
	case r.W < 1 || r.H < 1 || r.W*r.H > maxPixels || len(r.Pixels) != r.W*r.H:
		return fmt.Errorf("rigid: body %d has %d pixels for a %d×%d image; a body holds 1 to %d", r.Key, len(r.Pixels), r.W, r.H, maxPixels)
	case n == 0:
		return fmt.Errorf("rigid: body %d has no pixel left", r.Key)
	case r.CX < 0 || r.CY < 0:
		return fmt.Errorf("rigid: body %d begins at (%d, %d) in its first image", r.Key, r.CX, r.CY)
	}
	for _, f := range [...]float32{r.Left, r.Top, r.X, r.Y, r.C, r.S, r.VX, r.VY, r.Spin} {
		if !finite(f) {
			return fmt.Errorf("rigid: body %d has a value that is not finite", r.Key)
		}
	}
	// Box2D's own test for a rotation, with its tolerance.
	if qq := float32(r.C*r.C) + float32(r.S*r.S); qq <= 1-0.0006 || qq >= 1+0.0006 {
		return fmt.Errorf("rigid: body %d is turned by (%v, %v), not a rotation", r.Key, r.C, r.S)
	}
	if r.X < 0 || r.Y < 0 || r.X >= wm || r.Y >= hm {
		return fmt.Errorf("rigid: body %d is at (%v, %v) m, outside the world", r.Key, r.X, r.Y)
	}
	if r.Whole && (n != len(r.Pixels) || r.CX != 0 || r.CY != 0 || r.Left != -float32(r.W)/2 || r.Top != -float32(r.H)/2) {
		return fmt.Errorf("rigid: body %d is recorded whole but is not the box it was made as", r.Key)
	}
	return nil
}

// finite reports whether f is a number other than an infinity.
func finite(f float32) bool {
	return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}

// remake makes the body of record r, which checkRecord passed, in Box2D,
// as it was: a whole body is the box it was made as, a body that lost
// pixels gets shapes from them, both to the bit as before. It is asleep
// if r is, unless the cells it may touch reach past home, its block.
func (s *System) remake(r *save.Body, home rect) (body, error) {
	b := body{
		key: r.Key, mat: r.Mat, w: r.W, h: r.H, left: r.Left, top: r.Top, cx: r.CX, cy: r.CY,
		px: make([]pixel, len(r.Pixels)), buoyancy: pieceBuoyancy(s.mats.Def(r.Mat)),
	}
	for k, p := range r.Pixels {
		b.px[k] = pixel{dmg: p.Damage, tone: p.Tone, ok: p.OK}
		if p.OK {
			b.n++
		}
	}
	b.cells = make([]cellAt, 0, b.n+b.w+b.h+1)
	at, rot := physics.Vec2{X: r.X, Y: r.Y}, physics.Rot{C: r.C, S: r.S}
	id, err := s.phys.AddDynamicBody(at, rot)
	if err != nil {
		return body{}, fmt.Errorf("rigid: %w", err)
	}
	b.id = id
	if r.Whole {
		d := s.mats.Def(b.mat)
		// As add makes it: the box's center of mass is its origin.
		err = s.phys.SetBox(id, float32(b.w)/(2*cellsPerMeter), float32(b.h)/(2*cellsPerMeter), physics.Surface{
			Density: float32(d.Density) * densityScale, Friction: q8(d.Friction), Restitution: q8(d.Restitution),
		})
		b.mass, b.inertia = s.phys.Mass(id), s.phys.Inertia(id)
	} else {
		err = s.reshape(&b)
	}
	if err != nil {
		s.phys.DestroyBody(id)
		return body{}, err
	}
	s.phys.SetVelocity(id, physics.Vec2{X: r.VX, Y: r.VY}, r.Spin)
	box := boxAt(&b, at, rot)
	// Past the world's edge there is nothing to change.
	reach := rect{
		max(box.x0-nearCells, 0), max(box.y0-nearCells, 0),
		min(box.x1+nearCells, s.w.Width()), min(box.y1+nearCells, s.w.Height()),
	}
	if !r.Awake && reach.x0 >= home.x0 && reach.y0 >= home.y0 && reach.x1 <= home.x1 && reach.y1 <= home.y1 {
		s.phys.Sleep(id)
	}
	return b, nil
}

// Reset starts Box2D anew, as New does: a world with only the walls, and
// no terrain collision. The bodies must all have been taken out first.
// A save does this, so the session it saves goes on from the same state as
// one opened from it (design.md 8절).
func (s *System) Reset() error {
	if len(s.bodies) > 0 {
		return fmt.Errorf("rigid: Reset with %d bodies in memory", len(s.bodies))
	}
	// Close first: Box2D holds 128 worlds, and the new one takes the old
	// one's place.
	s.phys.Close()
	phys, err := physics.NewWorld(physics.Config{Gravity: physics.Vec2{X: 0, Y: gravity}})
	if err != nil {
		return fmt.Errorf("rigid: %w", err)
	}
	s.phys = phys
	clear(s.terrain)
	clear(s.stamp)
	for i := range s.nearHead {
		s.nearHead[i] = -1
	}
	s.ticks = 0
	s.worn, s.gone, s.freed = s.worn[:0], s.gone[:0], s.freed[:0]
	s.broken = false
	return s.wall()
}

// NextKey returns the key the next body made gets.
func (s *System) NextKey() uint32 {
	return s.nextKey
}

// SetNextKey sets the key the next body made gets, for a session opened
// from a save. Load raises it past the keys it loads.
func (s *System) SetNextKey(k uint32) {
	s.nextKey = k
}

// Frozen returns how many bodies wait, frozen, for a block not in memory.
func (s *System) Frozen() int {
	n := 0
	for i := range s.bodies {
		if s.bodies[i].frozen {
			n++
		}
	}
	return n
}

// freeze stops, before Box2D steps, every body that may reach a block not
// in memory in the coming Step, and every body that may touch such a body
// in it: Box2D disables them where they are, keeping their pixels in the
// grid, and they keep their velocity here. A body that no longer reaches
// one goes on with the velocity it had. 🔶 The cells do the same: a chunk
// whose updates may reach a block not in memory does not run. It needs
// the reach of each body (near) and the bodies near each chunk, which
// Collide lists.
func (s *System) freeze() {
	s.cold = s.cold[:0]
	for i := range s.bodies {
		b := &s.bodies[i]
		b.cold = b.written && s.reachesOut(b.near)
		if b.cold {
			s.cold = append(s.cold, int32(i))
		}
	}
	for k := 0; k < len(s.cold); k++ {
		f := s.bodies[s.cold[k]].near
		x0, y0 := max(f.x0, 0)/world.ChunkSize, max(f.y0, 0)/world.ChunkSize
		x1 := (min(f.x1, s.w.Width()) - 1) / world.ChunkSize
		y1 := (min(f.y1, s.w.Height()) - 1) / world.ChunkSize
		for cy := y0; cy <= y1; cy++ {
			for cx := x0; cx <= x1; cx++ {
				c := cy*s.cols + cx
				if s.stamp[c] != s.ticks {
					continue
				}
				for j := s.nearHead[c]; j >= 0; j = s.nearNext[j] {
					o := &s.bodies[s.nearBody[j]]
					if !o.cold && o.near.overlaps(f) {
						o.cold = true
						s.cold = append(s.cold, s.nearBody[j])
					}
				}
			}
		}
	}
	for i := range s.bodies {
		b := &s.bodies[i]
		switch {
		case b.cold && !b.frozen:
			b.fv, b.fspin = s.phys.Velocity(b.id)
			b.fawake = s.phys.Awake(b.id)
			s.phys.Disable(b.id)
			b.frozen = true
		case !b.cold && b.frozen:
			s.phys.Enable(b.id)
			if b.fawake {
				s.phys.SetVelocity(b.id, b.fv, b.fspin)
			}
			b.frozen = false
		}
	}
}

// reachesOut reports whether r reaches a block of the world not in memory.
func (s *System) reachesOut(r rect) bool {
	x0, y0 := max(r.x0, 0), max(r.y0, 0)
	x1, y1 := min(r.x1, s.w.Width()), min(r.y1, s.w.Height())
	if x0 >= x1 || y0 >= y1 {
		return false
	}
	for by := y0 / world.BlockSize; by <= (y1-1)/world.BlockSize; by++ {
		for bx := x0 / world.BlockSize; bx <= (x1-1)/world.BlockSize; bx++ {
			if !s.w.Resident(bx, by) {
				return true
			}
		}
	}
	return false
}
