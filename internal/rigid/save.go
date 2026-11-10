package rigid

import (
	"fmt"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

// The bodies of a big world live with its 512×512 blocks (design.md 8절).
// A block has no bodies of its own: when it leaves memory, every body that
// touches it leaves with it (leavesWith, Unload), recorded as save.Body,
// and when it comes back they are made again (Load). A body whose next
// Step may reach a block not in memory waits, frozen, until the block is
// back (freeze).

// unloadMargin widens, in cells, the box around a body when a block
// leaving memory looks for the bodies it takes: 🧪 one, as Box2D's AABB is
// a little bigger than its body in Noita, which took a resting box one
// pixel from the block and left one two pixels from it (C137,
// BODY_BLOCK_RESIDENCY 3.2).
const unloadMargin = 1

// leavesWith reports whether b leaves memory with the block of cells area:
// whether the box around its turned rectangle, where Box2D has it now,
// widened by unloadMargin, touches the block. So a body across two blocks
// leaves with the first of them to go, whole, wherever its center is, as
// in Noita, and it is saved with that block alone (C137). The box is the
// rectangle's own extent, not the cells it covers, which reach a cell
// further when an edge lies on a cell's edge.
func (s *System) leavesWith(b *body, area rect) bool {
	p, q := s.phys.Transform(b.id)
	wx, wy := b.middle(q.C, q.S)
	hw, hh := float32(b.w)/2, float32(b.h)/2
	ex := float32(abs32(q.C)*hw) + float32(abs32(q.S)*hh) + unloadMargin
	ey := float32(abs32(q.S)*hw) + float32(abs32(q.C)*hh) + unloadMargin
	cx, cy := float32(p.X*cellsPerMeter)+wx, float32(p.Y*cellsPerMeter)+wy
	return cx-ex <= float32(area.x1) && float32(area.x0) <= cx+ex &&
		cy-ey <= float32(area.y1) && float32(area.y0) <= cy+ey
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
		Pixels: make([]save.Pixel, len(b.px)), Whole: b.cover == nil, Static: b.static,
		X: p.X, Y: p.Y, C: q.C, S: q.S, VX: v.X, VY: v.Y, Spin: spin, Awake: awake,
	}
	for k, px := range b.px {
		r.Pixels[k] = save.Pixel{Damage: px.dmg, Tone: px.tone, OK: px.ok(), Crack: px.marked()}
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

// Unload takes out the bodies that leave memory with block (bx, by)
// (leavesWith), before its cells do: it erases their pixels, in the block
// and in the blocks that stay alike, appends a record of each to out, in
// key order, and removes them from Box2D, which wakes the bodies that
// touched them. A body that does not leave but still has pixels in the
// block, written before it moved away, is erased too and written again by
// the next Write, outside the block. So no body's pixel leaves with the
// block as a cell, and none is left behind (C137). The block's terrain
// collision is dropped.
func (s *System) Unload(bx, by int, out []save.Body) []save.Body {
	area := blockCells(bx, by)
	// Record every body that leaves before removing any: removing one
	// wakes the bodies it touched, which would then be recorded awake
	// (Codex review).
	dead := false
	for i := range s.bodies {
		b := &s.bodies[i]
		if b.dead = s.leavesWith(b, area); b.dead {
			out = append(out, s.record(b))
			dead = true
		}
	}
	for i := range s.bodies {
		b := &s.bodies[i]
		switch {
		case b.dead:
			s.erase(b, uint16(i))
			if b.written {
				s.gone = append(s.gone, b.box)
			}
			s.phys.DestroyBody(b.id)
		case b.written && b.box.overlaps(area):
			s.erase(b, uint16(i))
			b.redraw = true
		}
	}
	if dead {
		s.compact()
	}
	s.dropTerrain(area)
	return out
}

// UnloadBlocks takes every body out with blocks, as Unload would for each
// block in turn, each body with the first block of them it touches, and
// returns each block's records. It records every body before removing any,
// so a body that rested on one leaving with an earlier block is recorded
// as it was, not as Box2D woke it (Codex review). A save takes out all the
// blocks in memory this way.
func (s *System) UnloadBlocks(blocks [][2]int) [][]save.Body {
	out := make([][]save.Body, len(blocks))
	dead := false
	for i := range s.bodies {
		b := &s.bodies[i]
		for k, at := range blocks {
			if s.leavesWith(b, blockCells(at[0], at[1])) {
				out[k] = append(out[k], s.record(b))
				b.dead, dead = true, true
				break
			}
		}
	}
	for i := range s.bodies {
		if b := &s.bodies[i]; b.dead {
			s.erase(b, uint16(i))
			if b.written {
				s.gone = append(s.gone, b.box)
			}
			s.phys.DestroyBody(b.id)
		}
	}
	if dead {
		s.compact()
	}
	for _, at := range blocks {
		s.dropTerrain(blockCells(at[0], at[1]))
	}
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
// are. Their pixels go into the grid at the next Write. They come back
// awake, with the velocities they left with (restoreMotion). A body whose
// key is already in memory is left out: only a save mixed by a crash
// holds one twice. Load fails, adding no body, if a record is not one a
// body can be made of or the keys are not in increasing order. New bodies
// get keys after every key loaded. The bodies in memory that reach into
// the block are written again at the next Write: written while it was
// away, they left out their cells there, and one that does not move, as a
// static body, would never write them (Codex review).
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
	made := make([]body, 0, len(recs))
	for i := range recs {
		r := &recs[i]
		if _, held := s.slotOf(r.Key); held {
			continue
		}
		b, err := s.remake(r)
		if err != nil {
			for k := range made {
				s.phys.DestroyBody(made[k].id)
			}
			return err
		}
		made = append(made, b)
		s.nextKey = max(s.nextKey, r.Key+1)
	}
	area := blockCells(bx, by)
	for i := range s.bodies {
		if b := &s.bodies[i]; b.written && b.box.overlaps(area) {
			b.redraw = true
		}
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
	case r.W < 1 || r.H < 1 || r.W*r.H > MaxPixels || len(r.Pixels) != r.W*r.H:
		return fmt.Errorf("rigid: body %d has %d pixels for a %d×%d image; a body holds 1 to %d", r.Key, len(r.Pixels), r.W, r.H, MaxPixels)
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
	// A piece keeps the frame of the body it broke from, so its origin may
	// lie outside the world; the middle of its image may not (Codex
	// review).
	mx := (r.Left + float32(r.CX) + float32(r.W)/2) / cellsPerMeter
	my := (r.Top + float32(r.CY) + float32(r.H)/2) / cellsPerMeter
	x := r.X + float32(r.C*mx) - float32(r.S*my)
	y := r.Y + float32(r.S*mx) + float32(r.C*my)
	if !(x >= 0 && y >= 0 && x <= wm && y <= hm) {
		return fmt.Errorf("rigid: body %d lies at (%v, %v) m, outside the world", r.Key, x, y)
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
// pixels or that the terrain turned into gets shapes from them, both to
// the bit as before. A static body is static again.
func (s *System) remake(r *save.Body) (body, error) {
	b := body{
		key: r.Key, mat: r.Mat, static: r.Static, w: r.W, h: r.H, left: r.Left, top: r.Top, cx: r.CX, cy: r.CY,
		px: make([]pixel, len(r.Pixels)), buoyancy: s.pieceBuoyancy(r.Mat),
	}
	for k, p := range r.Pixels {
		b.px[k] = pixel{dmg: p.Damage, tone: p.Tone}
		if p.OK {
			b.px[k].bits = pixelHeld
			if p.Crack {
				b.px[k].bits |= pixelMarked
			}
			b.n++
		}
	}
	b.cells = make([]cellAt, 0, b.n+b.w+b.h+1)
	at, rot := physics.Vec2{X: r.X, Y: r.Y}, physics.Rot{C: r.C, S: r.S}
	add := s.phys.AddDynamicBody
	if b.static {
		add = s.phys.AddStaticBody
	}
	id, err := add(at, rot)
	if err != nil {
		return body{}, fmt.Errorf("rigid: %w", err)
	}
	b.id = id
	if r.Whole {
		d := s.mats.Def(b.mat)
		// As add makes it: the box's center of mass is its origin.
		err = s.phys.SetBox(id, float32(b.w)/(2*cellsPerMeter), float32(b.h)/(2*cellsPerMeter), physics.Surface{
			Density: float32(d.Density) * densityScale, Friction: q8(d.Friction), Restitution: q8(d.Restitution), Hits: d.Cracks,
		})
		b.mass, b.inertia = s.phys.Mass(id), s.phys.Inertia(id)
	} else {
		err = s.reshape(&b)
	}
	if err != nil {
		s.phys.DestroyBody(id)
		return body{}, err
	}
	if !b.static {
		v, spin := restoreMotion(r)
		s.phys.SetVelocity(id, v, spin)
	}
	return b, nil
}

// restoreMotion returns the velocity of its center of mass and the angular
// velocity a body made again from r starts with. It keeps what r holds,
// so a body that left falling goes on falling, as the developer chose
// (2026-11-09). Noita's come back at rest (C137, BODY_BLOCK_RESIDENCY
// 6.1-6.2); to do as Noita does, return zeros.
//
// Every body comes back awake, as Noita's do, though r says whether it
// slept: a body made asleep in Box2D sleeps alone, with no touching contact
// to the bodies beside it, so Box2D would not wake it when what holds it
// up is dug away or cut (three Codex reviews). Awake, the bodies settle
// and fall asleep together again within a second.
func restoreMotion(r *save.Body) (physics.Vec2, float32) {
	return physics.Vec2{X: r.VX, Y: r.VY}, r.Spin
}

// Reset starts Box2D anew, as New does: a world with only the walls, and
// no terrain collision. The bodies must all have been taken out first.
// A save does this, so the session it saves goes on from the same state as
// one opened from it (design.md 8절). The cracks walking through the cells
// walk on, as they do in a session opened from the save (SetCracks).
func (s *System) Reset() error {
	if len(s.bodies) > 0 {
		return fmt.Errorf("rigid: Reset with %d bodies in memory", len(s.bodies))
	}
	// Close first: Box2D holds 128 worlds, and the new one takes the old
	// one's place.
	s.phys.Close()
	phys, err := physics.NewWorld(worldConfig)
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
	s.worn, s.gone, s.freed, s.seeds = s.worn[:0], s.gone[:0], s.freed[:0], s.seeds[:0]
	s.broken = false
	return s.wall()
}

// Cracks appends to out the cracks walking through the cells, in the
// order they were made, as a session saves them: they walk in the world,
// not with a block.
func (s *System) Cracks(out []save.SessionCrack) []save.SessionCrack {
	for i := range s.movers {
		m := &s.movers[i]
		out = append(out, save.SessionCrack{
			X: m.x, Y: m.y, AX: m.ax, AY: m.ay, Energy: m.energy, Split: m.split, Forks: m.forks,
			LastX: m.lx, LastY: m.ly, Started: m.started, Random: m.r.State(),
		})
	}
	return out
}

// SetCracks replaces the cracks walking through the cells with cs, for a
// session opened again, so they walk on as they would have. A crack whose
// energy or branches are out of what a crack can have, or whose way is not
// finite, is an error, and nothing changes.
func (s *System) SetCracks(cs []save.SessionCrack) error {
	for i := range cs {
		c := &cs[i]
		switch {
		case !finite(c.X) || !finite(c.Y) || !finite(c.AX) || !finite(c.AY):
			return fmt.Errorf("rigid: crack %d at (%v, %v) along (%v, %v)", i, c.X, c.Y, c.AX, c.AY)
		case c.Energy > crackEnergy || c.Split < 0 || c.Split > crackSplit || c.Forks < 0 || c.Forks > crackForks:
			return fmt.Errorf("rigid: crack %d has energy %d, branches at %d, %d more times", i, c.Energy, c.Split, c.Forks)
		}
	}
	s.movers = s.movers[:0]
	for i := range cs {
		c := &cs[i]
		s.movers = append(s.movers, mover{
			x: c.X, y: c.Y, ax: c.AX, ay: c.AY, energy: c.Energy, split: c.Split, forks: c.Forks,
			lx: c.LastX, ly: c.LastY, started: c.Started, r: rng.New(c.Random),
		})
	}
	return nil
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
// one goes on with the velocity it had. Noita's go on and are lost (C137);
// the developer chose to freeze them (2026-11-09). The cells do the same: a
// chunk whose updates may reach a block not in memory does not run. With
// every block in memory, as in the rooms, nothing freezes. A static body
// never does: it does not move, and it leaves memory with the first block
// it touches, whole.
func (s *System) freeze() {
	s.cold = s.cold[:0]
	whole := s.w.AllResident()
	for i := range s.bodies {
		b := &s.bodies[i]
		b.cold = false
		if whole || !b.written || b.static {
			continue
		}
		b.reach = s.reach(b)
		if s.reachesOut(b.reach) {
			b.cold = true
			s.cold = append(s.cold, int32(i))
		}
	}
	// Spread to the bodies whose reach meets a frozen one's. It looks at
	// every body for each frozen one; few freeze, and only at the edge of
	// what is in memory.
	for k := 0; k < len(s.cold); k++ {
		f := s.bodies[s.cold[k]].reach
		for i := range s.bodies {
			if o := &s.bodies[i]; !o.cold && !o.static && o.written && o.reach.overlaps(f) {
				o.cold = true
				s.cold = append(s.cold, int32(i))
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

// reach returns the cells b may touch in the coming Step: its box, where it
// was last written, widened by nearCells and by as far as any point of its
// image can go in a tick, moving with its velocity and turning about its
// center of mass. A frozen body has the velocity it froze with. The
// products are converted one by one, so the result is the same on every
// CPU (docs/pitfalls.md).
func (s *System) reach(b *body) rect {
	v, spin := b.fv, b.fspin
	if !b.frozen {
		s.velocity(b)
		v, spin = b.v, b.spin
	}
	// The farthest a point of the image lies from the center of mass, in
	// cells: from the center of mass to the middle of the image, and from
	// there to a corner.
	mx := b.left + float32(b.cx) + float32(b.w)/2 - float32(b.lc.X*cellsPerMeter)
	my := b.top + float32(b.cy) + float32(b.h)/2 - float32(b.lc.Y*cellsPerMeter)
	hw, hh := float32(b.w)/2, float32(b.h)/2
	r := hypot32(mx, my) + hypot32(hw, hh)
	d := float32(float32(speed(v)*cellsPerMeter)+float32(abs32(spin)*r)) * tickSeconds
	m := nearCells + int(math.Ceil(float64(d)))
	return rect{b.box.x0 - m, b.box.y0 - m, b.box.x1 + m, b.box.y1 + m}
}

// hypot32 returns √(x² + y²).
func hypot32(x, y float32) float32 {
	return float32(math.Sqrt(float64(float32(x*x) + float32(y*y))))
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
