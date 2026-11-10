package rigid

import (
	"cmp"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/rng"
)

// Collision cracks and sleep conversion (docs/briefs/ice-glass.md, C138).
//
// A body of a material that cracks (material.Def.Cracks) checks each
// contact Box2D reports it hit something in faster than crackMin: with a
// chance that grows with the speed, and always from crackSure on, a crack
// runs from the contact point into the body and takes out the pixels on
// its way. They are gone, counted by material (Lost), as Noita's crack
// erases its cells. Step then breaks the body (Break): it splits, or
// shatters if its material shatters, as glass does.
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

// crackSteps is how many pixels a crack goes into its body, the first on
// the contact point. 🔶🧪 Our own: Noita's crack walker took about 3.9
// cells a crack (C138 3.4).
const crackSteps = 5

// Salts that key the cracks' random numbers apart from the others'.
const (
	crackSalt = 0x510e527fade682d1
	pathSalt  = 0x9b05688c2b3e6c1f
)

// The 8 directions a crack steps in, counterclockwise from +x in the
// image, whose y points down.
var (
	stepX = [8]int{1, 1, 0, -1, -1, -1, 0, 1}
	stepY = [8]int{0, 1, 1, 1, 0, -1, -1, -1}
)

// crackCheck is a contact a body of a material that cracks hit something
// in: the body's key and slot, the contact point and the direction into
// the body, in cells, and the approach speed in cells per second.
type crackCheck struct {
	key          uint32
	slot         int
	x, y, dx, dy float32
	speed        float32
}

// crack checks for a crack each contact of a body of a material that
// cracks that the last Step reported, in the order of the bodies' keys and
// the contacts' points, and runs the cracks. The draw for the n-th check
// of a body in a tick comes from (seed, tick, key, n), so neither Box2D's
// order nor the worker count changes it.
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
	n := uint64(0)
	for i := range s.checks {
		c := &s.checks[i]
		if i > 0 && s.checks[i-1].key != c.key {
			n = 0
		}
		h := rng.Hash3(s.w.Seed()^crackSalt, uint64(s.w.Tick()), uint64(c.key), n)
		n++
		// U in [0, 1) times crackSure, at most the speed. Uniform in 53 bits.
		if u := float64(h>>11) / (1 << 53); u*crackSure <= float64(c.speed) {
			s.runCrack(c, rng.Hash3(s.w.Seed()^pathSalt, uint64(s.w.Tick()), uint64(c.key), n))
		}
	}
}

// addCheck lists body id's side of a hit at (x, y), into it along (dx, dy),
// if id is a body of a material that cracks.
func (s *System) addCheck(id physics.Body, x, y, dx, dy, speed float32) {
	for slot := range s.bodies {
		if b := &s.bodies[slot]; b.id == id {
			if s.mats.Def(b.mat).Cracks {
				s.checks = append(s.checks, crackCheck{b.key, slot, x, y, dx, dy, speed})
			}
			return
		}
	}
}

// runCrack runs crack c through its body's image: crackSteps pixels from
// the contact point, in one of 8 directions, the one nearest the way into
// the body, turning 45° left or right a quarter of the time each, but
// never more than 45° from where it set out. Each pixel it reaches that the
// body has is taken out. The turns come from seed.
//
// The contact point lies on the body's outline, often at a corner of its
// shape, where it falls as easily just outside the image as inside. The
// crack starts half a pixel further in, on the nearest pixel of the image.
func (s *System) runCrack(c *crackCheck, seed uint64) {
	b := &s.bodies[c.slot]
	p, q := s.phys.Transform(b.id)
	// The point and the way into the body in its frame, then in its image's
	// pixels. The products are converted one by one, so the result is the
	// same on every CPU (docs/pitfalls.md).
	rx, ry := c.x-float32(p.X*cellsPerMeter), c.y-float32(p.Y*cellsPerMeter)
	u, v := float32(q.C*rx)+float32(q.S*ry), float32(q.C*ry)-float32(q.S*rx)
	du, dv := float32(q.C*c.dx)+float32(q.S*c.dy), float32(q.C*c.dy)-float32(q.S*c.dx)
	fi := u - b.left - float32(b.cx) + float32(du/2)
	fj := v - b.top - float32(b.cy) + float32(dv/2)
	i := min(max(int(math.Floor(float64(fi))), 0), b.w-1)
	j := min(max(int(math.Floor(float64(fj))), 0), b.h-1)
	start := octant(du, dv)
	d := start
	r := rng.New(seed)
	for range crackSteps {
		if uint(i) < uint(b.w) && uint(j) < uint(b.h) {
			if k := j*b.w + i; b.px[k].ok {
				s.takeOut(b, c.slot, k)
			}
		}
		switch r.IntN(4) {
		case 0:
			if turn := (d + 1) & 7; turn != (start+2)&7 {
				d = turn
			}
		case 1:
			if turn := (d + 7) & 7; turn != (start+6)&7 {
				d = turn
			}
		}
		i, j = i+stepX[d], j+stepY[d]
	}
}

// octant returns which of the 8 directions (stepX, stepY) is nearest to
// (x, y).
func octant(x, y float32) int {
	// tan(67.5°): beyond it a direction is nearer an axis than a diagonal.
	const steep = 2.4142135
	ax, ay := abs32(x), abs32(y)
	switch {
	case ax > float32(steep*ay):
		if x < 0 {
			return 4
		}
		return 0
	case ay > float32(steep*ax):
		if y < 0 {
			return 6
		}
		return 2
	case x >= 0 && y >= 0:
		return 1
	case x < 0 && y >= 0:
		return 3
	case x < 0:
		return 5
	}
	return 7
}

// takeOut takes pixel k out of b, at slot, for good: it leaves the image
// and the cells that show it, and counts as lost. Step breaks b after the
// cracks.
func (s *System) takeOut(b *body, slot, k int) {
	b.n--
	b.dirty, s.broken = true, true
	s.lost[b.mat]++
	s.showDamage(b, uint16(slot), uint16(k), true)
	b.px[k] = pixel{}
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
