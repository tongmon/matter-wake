package rigid

import (
	"cmp"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// Collision cracks and sleep conversion (docs/briefs/ice-glass.md, C138,
// C139).
//
// A body of a material that cracks (material.Def.Cracks) that hits
// something faster than crackMin is checked crackChecks times in that
// tick, spread over the points where it hit: with a chance that grows with
// the point's speed, and always from crackSure on, a crack sets out from
// the point into the body. A crack walks the world's cells, as Noita's
// crack mover does (C139): in some ticks it takes a few steps, zigzagging
// about the way it set out, and marks each pixel of a material that cracks
// it steps on, which is then drawn bright. When it ends in a tick in which
// it marked something, every marked pixel of the body it marked last is
// taken out for good, counted by material (Lost), and Step breaks the body
// (Break): it splits, or shatters if its material shatters, as glass does.
// A crack walks where the cells are, not with the body: a body that moves
// under it is scratched.
//
// A dynamic body of a material that turns back into terrain far from the
// camera (material.Def.SleepTo) does so when the camera has moved far
// (Settle).

// The approach speeds of a crack, in cells per second (C138): none at
// crackMin or slower; above it, a crack if a uniform draw U in [0, 1)
// times crackSure is at most the speed, so about half the time just above
// crackMin, and always from crackSure on. 🧪 Noita's.
const (
	crackMin  = 50
	crackSure = 100
)

// crackChecks is how many checks a body that hit something fast gets in a
// tick, spread over the points where it hit, one after another. 🔶🧪 Noita's
// bodies made 4 cracks a hit, whatever their size (C139 5); why 4 is not
// known. Box2D reports each point of a contact, 2 for a box that lands
// flat, and a floor whose collision changes chunk under the body adds a
// contact.
const crackChecks = 4

// How a crack walks, 🧪 Noita's (C139 3). It sets out with crackEnergy, a
// step's worth each. In its first tick it moves; in each later one it moves
// when a draw in [0, 101) is at most crackSpeed, 21 ticks in 101. A tick it
// moves takes crackMinSteps to crackMaxSteps steps of a cell along the way
// it set out, turned for that tick (crackTurn). crackJitter steps in 101
// instead move it -1, 0 or 1 cells along x or y, alike. A crack may branch
// up to crackForks times, at an energy of crackSplit at most.
const (
	crackEnergy   = 200
	crackSpeed    = 20
	crackMinSteps = 4
	crackMaxSteps = 10
	crackJitter   = 15
	crackForks    = 2
	crackSplit    = 20
)

// crackShade is the damage a pixel a crack marked shows: full, which the
// palette draws 60% of the way to white, as Noita's crack blends a cell's
// color with white once (CELL_DAMAGE_VISUALS 3).
const crackShade = math.MaxUint16

// Salts that key the cracks' random numbers apart from the others'.
const (
	crackSalt = 0x510e527fade682d1
	pathSalt  = 0x9b05688c2b3e6c1f
	speckSalt = 0xcbbb9d5dc1059ed8
)

// speckSpeed is how fast, in cells per second, a cell a crack takes out
// flies off as a speck: up to half of it either way along each axis
// (C139 4.2). 🧪 Noita's.
const speckSpeed = 15.0

// crackCheck is a point where a body of a material that cracks hit
// something: the body's key, the point and the direction into the body, in
// cells, and the approach speed in cells per second.
type crackCheck struct {
	key          uint32
	x, y, dx, dy float32
	speed        float32
}

// mover is a crack walking through the world's cells (C139): where it is,
// (x, y) in cells, and the way it set out, (ax, ay), a unit vector into the
// body it hit; the energy it has left, how many more times it may branch
// and the energy it branches at; the cell of its last step, (lx, ly), once
// started; whether it ends this tick; and its own random numbers, so no
// other crack changes its path.
type mover struct {
	x, y, ax, ay  float32
	energy, split int32
	forks         int32
	lx, ly        int32
	started, done bool
	r             rng.Stream
}

// newMover returns a crack at (x, y) setting out along (ax, ay), with the
// random numbers of seed.
func newMover(x, y, ax, ay float32, seed uint64) mover {
	m := mover{x: x, y: y, ax: ax, ay: ay, energy: crackEnergy, r: rng.New(seed)}
	m.forks = int32(m.r.IntN(crackForks + 1))
	m.split = int32(m.r.IntN(crackSplit + 1))
	return m
}

// crack checks the bodies of materials that crack that the last Step
// reported hit something, in the order of their keys, and adds the cracks
// the checks make. A body's points are sorted and checked in turn,
// crackChecks times in all; the draw for its n-th check in a tick comes
// from (seed, tick, key, n), and the crack it makes gets the random numbers
// of (seed, tick, key, n+1), salted apart, so neither Box2D's order nor the
// worker count changes them.
func (s *System) crack() {
	s.hits = s.phys.Hits(s.hits[:0])
	if len(s.hits) == 0 {
		return
	}
	s.checks = s.checks[:0]
	for i := range s.hits {
		h := &s.hits[i]
		speed := float32(h.Speed * cellsPerMeter)
		if !(speed > crackMin) {
			continue
		}
		x, y := float32(h.Point.X*cellsPerMeter), float32(h.Point.Y*cellsPerMeter)
		// The normal points from A to B: into A is against it.
		s.addCheck(h.A, x, y, -h.Normal.X, -h.Normal.Y, speed)
		s.addCheck(h.B, x, y, h.Normal.X, h.Normal.Y, speed)
	}
	slices.SortFunc(s.checks, func(a, b crackCheck) int {
		return cmp.Or(cmp.Compare(a.key, b.key), cmp.Compare(a.x, b.x), cmp.Compare(a.y, b.y),
			cmp.Compare(a.dx, b.dx), cmp.Compare(a.dy, b.dy), cmp.Compare(a.speed, b.speed))
	})
	seed, tick := s.w.Seed(), uint64(s.w.Tick())
	for i := 0; i < len(s.checks); {
		j := i + 1
		for j < len(s.checks) && s.checks[j].key == s.checks[i].key {
			j++
		}
		points := s.checks[i:j]
		for n := range uint64(crackChecks) {
			c := &points[n%uint64(len(points))]
			h := rng.Hash3(seed^crackSalt, tick, uint64(c.key), n)
			// U in [0, 1) times crackSure, at most the speed. Uniform in 53 bits.
			if u := float64(h>>11) / (1 << 53); u*crackSure <= float64(c.speed) {
				s.movers = append(s.movers, newMover(c.x, c.y, c.dx, c.dy, rng.Hash3(seed^pathSalt, tick, uint64(c.key), n+1)))
			}
		}
		i = j
	}
}

// addCheck lists body id's side of a hit at (x, y), into it along (dx, dy),
// if id is a body of a material that cracks.
func (s *System) addCheck(id physics.Body, x, y, dx, dy, speed float32) {
	for slot := range s.bodies {
		if b := &s.bodies[slot]; b.id == id {
			if s.cracks[b.mat] {
				s.checks = append(s.checks, crackCheck{b.key, x, y, dx, dy, speed})
			}
			return
		}
	}
}

// moveCracks walks every crack one tick, in the order they were made, and
// drops those that ended. A crack that branches adds its branches after
// the others, to walk from the next tick.
func (s *System) moveCracks() {
	n := len(s.movers)
	if n == 0 {
		return
	}
	for i := range n {
		s.walk(i)
	}
	kept := s.movers[:0]
	for _, m := range s.movers {
		if !m.done {
			kept = append(kept, m)
		}
	}
	s.movers = kept
}

// walk walks crack i one tick, as Noita's crack mover does a frame (C139
// 3). Unless it has not started, it rests the tick but 21 times in 101.
// Otherwise it turns its axis by this tick's turn, which it does not keep,
// and steps: each step spends a unit of energy and marks the pixel the
// cell it is in shows, if the pixel can crack, and, when the step came
// diagonally, the pixel of the cell beside it in the row it came from, so
// the line is 4-connected; then it moves, a cell along the turned axis or a
// jitter. The last step decides first: a crack whose last step marked
// nothing ends, and, if it marked something this tick, takes out every
// marked pixel of the body it marked last (crumble), whatever energy it has
// left. One whose last step marked a pixel goes on while it has energy; if
// it has none, it ends there and takes nothing out. That is the order
// Noita's mover checks them in (C139 3.4).
func (s *System) walk(i int) {
	m := &s.movers[i]
	if m.started && m.r.IntN(101) > crackSpeed {
		return
	}
	q := physics.MakeRot(crackTurn(&m.r))
	dx, dy := float32(q.C*m.ax)-float32(q.S*m.ay), float32(q.S*m.ax)+float32(q.C*m.ay)
	steps := crackMinSteps + m.r.IntN(crackMaxSteps-crackMinSteps+1)
	last, on := -1, false
	for range steps {
		m.energy--
		if m.forks > 0 && m.energy == m.split {
			s.fork(i)
			m = &s.movers[i]
		}
		x, y := int32(math.Floor(float64(m.x))), int32(math.Floor(float64(m.y)))
		var slot int
		if slot, on = s.markCrack(int(x), int(y)); on {
			last = slot
		}
		if m.started && x != m.lx && y != m.ly {
			if slot, ok := s.markCrack(int(x), int(m.ly)); ok {
				last = slot
			}
		}
		m.lx, m.ly, m.started = x, y, true
		if m.r.IntN(101) < crackJitter {
			d := float32(m.r.IntN(3) - 1)
			if m.r.IntN(101) < 50 {
				m.x += d
			} else {
				m.y += d
			}
		} else {
			m.x, m.y = m.x+dx, m.y+dy
		}
	}
	switch {
	case !on:
		if last >= 0 {
			s.crumble(last)
		}
		m.done = true
	case m.energy <= 0:
		m.done = true
	}
}

// crackTurn draws the turn of a crack's tick, in radians (C139 3.2): 32
// times in 129 a big one, 0.7 to 1.75 either way alike, otherwise one of
// 0.25 at most.
func crackTurn(r *rng.Stream) float32 {
	if r.IntN(129) < 32 {
		t := float32(unit(r)*1.05) + 0.7
		if r.IntN(129) < 64 {
			return -t
		}
		return t
	}
	return float32(unit(r)*0.5) - 0.25
}

// unit draws a uniform number in [0, 1] in steps of 1/32767, as Noita's
// cracks do.
func unit(r *rng.Stream) float32 {
	return float32(r.IntN(32768)) / 32767
}

// fork branches crack i where it is, as Noita's does when its energy comes
// to its branching energy (C139 3.4): two cracks, turned 0.3 to 1.5 radians
// to either side of its axis, with half its energy and one branch fewer,
// and random numbers of their own. They walk from the next tick; the crack
// itself finishes its tick and ends. A branch is born with its energy at
// its branching energy, and a step spends energy before it checks, so a
// branch never branches again: the short side branches C139 reads in
// Noita's (3.4), though one may have a branch left (Codex review).
func (s *System) fork(i int) {
	p := &s.movers[i]
	p.done = true
	parent := *p
	for _, base := range [2]float32{-1.5, 0.3} {
		q := physics.MakeRot(float32(unit(&p.r)*1.2) + base)
		c := parent
		c.ax, c.ay = float32(q.C*parent.ax)-float32(q.S*parent.ay), float32(q.S*parent.ax)+float32(q.C*parent.ay)
		c.forks, c.energy, c.split = parent.forks-1, parent.energy/2, parent.split/2
		c.done = false
		c.r = rng.New(p.r.Uint64())
		s.movers = append(s.movers, c)
		p = &s.movers[i]
	}
}

// markCrack marks the pixel the cell at (x, y) shows, if it is a pixel of a
// material that cracks, and reports whether it is, with its body's slot.
// A marked pixel is drawn bright, as at full damage (crackShade).
func (s *System) markCrack(x, y int) (int, bool) {
	c, ok := s.w.At(x, y)
	if !ok || !s.cracks[c.Mat] {
		return 0, false
	}
	slot, k := c.Body()
	if int(slot) >= len(s.bodies) {
		return 0, false
	}
	b := &s.bodies[slot]
	if int(k) >= len(b.px) || !b.px[k].ok() {
		return 0, false
	}
	if !b.px[k].marked() {
		b.px[k].bits |= pixelMarked
		s.showDamage(b, slot, k, false)
	}
	return int(slot), true
}

// crumble takes every marked pixel out of the body at slot, those every
// crack marked, as a crack that ends does (C139 4.2).
func (s *System) crumble(slot int) {
	b := &s.bodies[slot]
	for k := range b.px {
		if b.px[k].bits == pixelHeld|pixelMarked {
			s.takeOut(b, slot, k)
		}
	}
}

// takeOut takes pixel k out of b, at slot, for good: it leaves the image
// and the cells that show it, which fly off as specks, and counts as lost.
// Step breaks b after the cracks.
func (s *System) takeOut(b *body, slot, k int) {
	b.n--
	b.dirty, s.broken = true, true
	s.lost[b.mat]++
	s.showDamage(b, uint16(slot), uint16(k), true)
	b.px[k] = pixel{}
}

// speck throws c, a cell at (x, y) that shows pixel k of b and that a
// crack takes out, off as a speck that only shows it, as Noita does (C139
// 4.2): its velocity along each axis is U × speckSpeed − speckSpeed/2 cells
// per second. The draws come from a hash of the body, the pixel, the cell
// and the tick.
func (s *System) speck(b *body, k uint16, x, y int, c world.Cell) {
	r := rng.New(rng.Hash3(s.w.Seed()^speckSalt, uint64(s.w.Tick()), uint64(b.key)<<16|uint64(k),
		uint64(uint32(x))<<32|uint64(uint32(y))))
	vx := float32(unit(&r)*speckSpeed) - speckSpeed/2
	vy := float32(unit(&r)*speckSpeed) - speckSpeed/2
	look := world.Cell{Mat: c.Mat, Tone: c.Tone, Damage: c.Damage}
	s.w.Spray(look, x, y, int32(float32(vx*perTick)), int32(float32(vy*perTick)), r.Uint64())
}

// Walking returns how many cracks walk through the cells.
func (s *System) Walking() int {
	return len(s.movers)
}

// Lost adds to n[id], for every material id, how many pixels of it cracks
// have taken out of the bodies, which no longer exist anywhere. n must
// have an entry for every material.
func (s *System) Lost(n []int) {
	for id, k := range s.lost {
		n[id] += k
	}
}

// The distances, in cells from the camera's middle, at which a dynamic
// body of a material that turns back into terrain does so: from
// settleNear if it sleeps, from settleFar in any case. The camera's middle
// must have moved more than SettleMove since the last check. 🧪 Noita's
// (C138).
const (
	settleNear = 350
	settleFar  = 750
	// SettleMove is how far, in cells, the camera's middle moves between
	// two checks for bodies to turn back into terrain (Settle).
	SettleMove = 512
)

// Settle turns back into terrain, cell for pixel, every dynamic body of a
// material with a SleepTo whose center of mass lies settleFar or more from
// (cx, cy), the camera's middle in cells, or settleNear or more if it is
// asleep, as Noita does when its camera has moved far (C138). Static and
// frozen bodies stay. Each pixel's cell is the one its center falls in,
// as in shatter. Settle counts what it turned (Turned).
//
// Which bodies turn is settled before any does: turning one takes it out of
// Box2D, which wakes the bodies it touched, and a stack asleep would turn
// only in part (Codex review).
func (s *System) Settle(cx, cy float32) error {
	s.settling = s.settling[:0]
	for slot := range s.bodies {
		b := &s.bodies[slot]
		if b.static || b.frozen || !b.written || s.mats.SleepOf(b.mat) == material.Air {
			continue
		}
		p, q := s.phys.Transform(b.id)
		gx, gy := b.center(q.C, q.S)
		dx := float32(p.X*cellsPerMeter) + gx - cx
		dy := float32(p.Y*cellsPerMeter) + gy - cy
		d2 := float32(dx*dx) + float32(dy*dy)
		if d2 >= settleFar*settleFar || d2 >= settleNear*settleNear && !s.phys.Awake(b.id) {
			s.settling = append(s.settling, slot)
		}
	}
	for _, slot := range s.settling {
		if err := s.dissolve(slot, s.mats.SleepOf(s.bodies[slot].mat), s.lost); err != nil {
			return err
		}
	}
	if len(s.settling) > 0 {
		s.compact()
	}
	return nil
}
