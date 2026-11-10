package world

import (
	"fmt"
	"math/bits"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// Particles follow Noita's (C81~C84, C86, C134 and C139 in
// docs/briefs/particles.md).
//
// A cell hit hard enough leaves the grid for a while and flies as a
// particle that carries the cell (Launch): water a falling body splashes.
// A particle starts out passing through matter, a limited number of cells,
// while it rises; once it falls, or meets empty space, the next matter it
// meets stops it, and it hands its cell to the queue of cells to put back,
// which puts it in the grid in that same tick if it can. A speck is a
// particle without a cell, only a look: the dust a crack throws off
// (Spray). A speck of a solid bounces off matter a few times, and it
// lives a few seconds at most.
//
// Particles move after the cells' tick and before the queue, one after
// another in the order they were made, on the caller's goroutine. Each
// draws its random numbers from its own seed and the tick, so neither the
// order nor the worker count changes them. They read the grid and never
// write it: a cell leaves the grid as a displaced one does (Launch marks
// it for Flush) and comes back through the queue, which marks it. So the
// chunks' change rectangles hold as before.

// ParticleOne is one cell in a particle's position (Particle.X, Y) and one
// cell per tick in its velocity (VX, VY): they count 1/65536 cells. Q8
// would be too coarse for the particles' gravity, under 1/35 of a cell per
// tick per tick.
const ParticleOne = 1 << particleShift

const particleShift = 16

// How particles move, 🧪 Noita's (C82, C84), in ParticleOne per tick.
const (
	// particleGravity pulls particles down: 100 cells/s².
	particleGravity = 100 * ParticleOne / (60 * 60)
	// passGravity pulls a particle passing through matter: a quarter.
	passGravity = particleGravity / 4
	// passCells is how many cells of matter a particle that leaves the
	// grid may pass through.
	passCells = 20
	// passFast: a particle that leaves upward faster than 50 cells/s
	// passes, half the time, only through matter denser than its own cell;
	// otherwise through any (C81).
	passFast = -50 * ParticleOne / 60
	// maxSweep is the most cells a particle's path checks in a tick.
	maxSweep = 60
	// A speck lives speckLife ticks and up to speckLifeSpread-1 more: 5 to
	// 10 s.
	speckLife       = 300
	speckLifeSpread = 301
	// maxParticles is the most particles that fly at once, Noita's pool.
	// Past it a cell goes into the queue instead, and a speck is not made
	// (🔶: Noita's is not known).
	maxParticles = 100_000
	// maxSpeed bounds a particle's speed along each axis: 64 cells a tick,
	// faster than anything throws one or than one falls across the biggest
	// world. It only keeps the arithmetic in range; Noita has none.
	maxSpeed = 64 * ParticleOne
)

// How a particle that bounces does (C86), 🧪 Noita's, in ParticleOne per
// tick: a speed across the way it hit under bounceSlowX gets a kick of up
// to bounceKick either way; then both axes keep bounceKeep to bounceKeep +
// bounceKeepSpread of themselves. Slower than bounceLifeSpeed, a speck
// with more than a second to live gets 0.5 to 1.5 s; slower than
// bounceStop, it no longer bounces.
const (
	bounceSlowX      = 10 * ParticleOne / 60
	bounceKick       = 10 * ParticleOne / 60
	bounceKeep       = ParticleOne / 10
	bounceKeepSpread = ParticleOne / 5
	bounceLifeSpeed  = 40 * ParticleOne / 60
	bounceStop       = 10 * ParticleOne / 60
	bounceLife       = 30
	bounceLifeSpread = 61
)

// solidDensityScale turns a solid's density, on Noita's scale (wood 6, the
// mass of a body), into that of the other kinds, five times Noita's (water
// 20, Noita's 4). 🔶 Only a particle passing through a body's cells
// compares the two, so a drop of water passes through wood, as in Noita,
// and a grain of sand does not.
const solidDensityScale = 5

// particleSalt keys a particle's random numbers in a tick apart from its
// first ones.
const particleSalt = 0x1f83d9abfb41bd6b

// ParticleFlags say what a particle is and how it moves.
type ParticleFlags uint8

const (
	// Speck marks a particle without a cell: its Cell is only what it
	// shows, and it lives Life ticks.
	Speck ParticleFlags = 1 << iota
	// Passing marks a particle passing through matter: through Pass more
	// cells, those denser than its Density.
	Passing
	// Bounces marks a speck that bounces off matter rather than stopping.
	Bounces
	// ParticleFlagsAll are all the flags.
	ParticleFlagsAll = Speck | Passing | Bounces
)

// Particle is a cell, or a speck, out of the grid and flying.
type Particle struct {
	// X and Y are where it is, and LX and LY the last point its path
	// checked, in 1/ParticleOne cells.
	X, Y, LX, LY int64
	// VX and VY are its velocity, in 1/ParticleOne cells per tick, y down.
	VX, VY int32
	// Cell is the cell it carries back into the grid, or what a speck
	// shows: its material, tone and damage.
	Cell Cell
	// Seed keys its random numbers.
	Seed uint64
	// Life is how many more ticks a speck lives; 0 for a cell's particle.
	Life int32
	// Density is how dense matter must be for it to pass through, while
	// Passing.
	Density uint8
	// Pass is how many more cells of matter it may pass through, while
	// Passing. It goes below 0 as it runs out.
	Pass int8
	// Flags say what it is and how it moves.
	Flags ParticleFlags
}

// At returns the cell (x, y) the particle is in.
func (p *Particle) At() (x, y int) {
	return int(p.X >> particleShift), int(p.Y >> particleShift)
}

// Launch takes the cell at (x, y) out of the grid into a particle that
// flies from its middle at (vx, vy), in 1/ParticleOne cells per tick, with
// the random numbers of seed: a liquid or powder a body hits hard. A
// particle leaving upward faster than 50 cells/s passes, half the time,
// only through matter denser than its cell, otherwise through any (C81).
// When as many particles fly as may, the cell goes into the queue of cells
// to put back instead (Displace). Air, a body's pixel and positions
// outside the grid or memory are left alone. The change waits for Flush,
// as Displace's does.
func (w *World) Launch(x, y int, vx, vy int32, seed uint64) {
	if !w.has(x, y) {
		return
	}
	c := w.cell(w.pos(x, y))
	if k := w.mats[c.Mat].kind; k == material.Empty || k == material.Solid {
		return
	}
	if len(w.particles) >= maxParticles {
		w.Displace(x, y)
		return
	}
	s := rng.New(seed)
	density := w.mats[c.Mat].density
	if vy > passFast || s.IntN(101) < 50 {
		density = 0
	}
	px, py := int64(x)<<particleShift+ParticleOne/2, int64(y)<<particleShift+ParticleOne/2
	w.particles = append(w.particles, Particle{
		X: px, Y: py, LX: px, LY: py, VX: clampSpeed(vx), VY: clampSpeed(vy),
		Cell: *c, Seed: s.Uint64(), Density: density, Pass: passCells, Flags: Passing,
	})
	*c = Cell{}
	w.pending.add(rect{x, y, x + 1, y + 1})
}

// Spray adds a speck that shows c, flying from the middle of cell (x, y)
// at (vx, vy), in 1/ParticleOne cells per tick, with the random numbers of
// seed: the dust of a pixel a crack took out. It lives 5 to 10 s, bounces
// off matter if c is not a liquid or powder, and changes no cell. When as
// many particles fly as may, or (x, y) is outside the grid or memory, it
// adds nothing.
func (w *World) Spray(c Cell, x, y int, vx, vy int32, seed uint64) {
	if !w.has(x, y) || len(w.particles) >= maxParticles || int(c.Mat) >= len(w.mats) {
		return
	}
	s := rng.New(seed)
	flags := Speck
	if k := w.mats[c.Mat].kind; k != material.Liquid && k != material.Powder {
		flags |= Bounces
	}
	px, py := int64(x)<<particleShift+ParticleOne/2, int64(y)<<particleShift+ParticleOne/2
	w.particles = append(w.particles, Particle{
		X: px, Y: py, LX: px, LY: py, VX: clampSpeed(vx), VY: clampSpeed(vy),
		Cell: c, Seed: s.Uint64(), Life: int32(speckLife + s.IntN(speckLifeSpread)), Flags: flags,
	})
}

// Particles returns how many particles fly, specks too.
func (w *World) Particles() int {
	return len(w.particles)
}

// Flying adds to n[id], for every material id, how many cells of it fly as
// particles. Specks hold no cell. n must have an entry for every material.
func (w *World) Flying(n []int) {
	for i := range w.particles {
		if p := &w.particles[i]; p.Flags&Speck == 0 {
			n[p.Cell.Mat]++
		}
	}
}

// AppendParticles appends to out the particles that fly, in the order
// they move. It is for drawing and saving and changes nothing.
func (w *World) AppendParticles(out []Particle) []Particle {
	return append(out, w.particles...)
}

// TakeParticles takes out of the world the particles in block (bx, by),
// by the cell they are in, and appends them to out in their order; the
// others keep theirs. A block about to leave memory takes its particles
// with it, as it takes its queued cells (TakeQueued).
func (w *World) TakeParticles(bx, by int, out []Particle) []Particle {
	r := rect{bx * BlockSize, by * BlockSize, (bx + 1) * BlockSize, (by + 1) * BlockSize}
	kept := w.particles[:0]
	for _, p := range w.particles {
		if r.has(p.At()) {
			out = append(out, p)
		} else {
			kept = append(kept, p)
		}
	}
	clear(w.particles[len(kept):])
	w.particles = kept
	return out
}

// Relaunch puts particles TakeParticles took back into the world, after
// their block is in memory again, to fly on after the others. It fails,
// and adds none, if one is in a cell outside the world or not in memory,
// or holds what no particle could.
func (w *World) Relaunch(ps []Particle) error {
	for i := range ps {
		if err := w.checkParticle(&ps[i]); err != nil {
			return err
		}
	}
	w.particles = append(w.particles, ps...)
	return nil
}

// checkParticle reports what p holds that no particle could.
func (w *World) checkParticle(p *Particle) error {
	x, y := p.At()
	switch {
	case !w.has(x, y):
		return fmt.Errorf("world: a particle in (%d, %d), which is not in memory", x, y)
	case int(p.Cell.Mat) >= len(w.mats):
		return fmt.Errorf("world: a particle of material %d, which the table does not have", p.Cell.Mat)
	case p.Flags&^ParticleFlagsAll != 0:
		return fmt.Errorf("world: a particle with flags %#x", p.Flags)
	case p.Flags&Speck != 0 && (p.Life < 0 || p.Flags&Passing != 0):
		return fmt.Errorf("world: a speck with %d ticks to live and flags %#x", p.Life, p.Flags)
	case p.Flags&Speck == 0 && (p.Life != 0 || p.Flags&Bounces != 0 || w.mats[p.Cell.Mat].kind == material.Empty):
		return fmt.Errorf("world: a cell's particle of material %d with %d ticks to live and flags %#x",
			p.Cell.Mat, p.Life, p.Flags)
	case p.Pass < -2 || p.Pass > passCells:
		return fmt.Errorf("world: a particle that may pass %d more cells, not -2 to %d", p.Pass, passCells)
	case clampSpeed(p.VX) != p.VX || clampSpeed(p.VY) != p.VY:
		return fmt.Errorf("world: a particle moving %d, %d, faster than %d", p.VX, p.VY, maxSpeed)
	case abs64(p.X-p.LX) > 2*maxSpeed || abs64(p.Y-p.LY) > 2*maxSpeed:
		return fmt.Errorf("world: a particle at (%d, %d) whose path last checked (%d, %d)", p.X, p.Y, p.LX, p.LY)
	}
	return nil
}

// moveParticles moves every particle one tick, in their order, and drops
// those that stopped: a cell's goes into the queue, to go back into the
// grid with the others after this.
func (w *World) moveParticles() {
	if len(w.particles) == 0 {
		return
	}
	kept := w.particles[:0]
	for i := range w.particles {
		p := w.particles[i]
		if w.move(&p) {
			kept = append(kept, p)
			continue
		}
		if p.Flags&Speck == 0 {
			x, y := p.At()
			w.fresh = append(w.fresh, Queued{Cell: p.Cell, X: int32(x), Y: int32(y)})
		}
	}
	clear(w.particles[len(kept):])
	w.particles = kept
}

// move moves p one tick, as Noita moves a particle a frame (C84): where it
// heads is set by the velocity it had, which then gains gravity, a quarter
// of it while passing through matter. A speck ages. A particle that
// passes and now falls stops passing. Then its path to where it heads is
// checked (sweep). It reports whether p flies on.
func (w *World) move(p *Particle) bool {
	tx, ty := p.X+int64(p.VX), p.Y+int64(p.VY)
	g := int32(particleGravity)
	if p.Flags&Passing != 0 {
		g = passGravity
	}
	p.VY = clampSpeed(p.VY + g)
	if p.Flags&Speck != 0 {
		p.Life--
		if p.Life < 0 {
			return false
		}
	}
	if p.Flags&Passing != 0 && p.VY > 0 {
		p.Flags &^= Passing
	}
	return w.sweep(p, tx, ty)
}

// sweep moves p toward (tx, ty) along its path, as Noita's does (C82). A
// target outside the grid or memory stops p where it is. Otherwise the
// path goes a cell's length at a time from p toward the target, as many
// steps as the distance from the last point it checked, rounded, and at
// most maxSweep, checking each cell it steps into but the one p is in.
// Empty space ends passing. While p passes, matter costs a cell of its
// passing, and matter no denser than p ends it; p still steps in. Matter
// stops p where it is, or bounces it if it bounces. A path that is not
// stopped ends at the target, whatever it checked. It reports whether p
// flies on.
//
// 🔶 Noita steps along the line from the last point it checked, the
// corner of a cell, to the target, starting from the particle. For a slow
// particle the two are apart, and the steps swing further aside each tick:
// a drop thrown straight up checked cells 30 to the side at the top. So
// the steps here go from the particle straight to the target; their count
// is still Noita's.
func (w *World) sweep(p *Particle, tx, ty int64) bool {
	if !w.has(int(tx>>particleShift), int(ty>>particleShift)) {
		return false
	}
	steps := min(int((distance(tx-p.LX, ty-p.LY)+ParticleOne/2)>>particleShift), maxSweep)
	if dx, dy := tx-p.X, ty-p.Y; steps > 0 && (dx != 0 || dy != 0) {
		length := distance(dx, dy)
		ux, uy := dx*ParticleOne/length, dy*ParticleOne/length
		x, y := p.X, p.Y
		for k := range steps {
			x, y = x+ux, y+uy
			cx, cy := int(x>>particleShift), int(y>>particleShift)
			if px, py := p.At(); k > 0 && cx == px && cy == py {
				continue
			}
			if !w.has(cx, cy) {
				return false
			}
			switch meet(p, &w.mats[w.cell(w.pos(cx, cy)).Mat]) {
			case enters:
			case bounces:
				w.bounce(p, ux, uy)
				return true
			case stops:
				return false
			}
			p.X, p.Y = x, y
			p.LX, p.LY = int64(cx)<<particleShift, int64(cy)<<particleShift
		}
	}
	p.X, p.Y = tx, ty
	return true
}

// meeting is what becomes of a particle that meets a cell on its path.
type meeting uint8

const (
	enters  meeting = iota // it steps into the cell
	bounces                // it bounces off the cell, staying where it is
	stops                  // it stops where it is
)

// meet returns what becomes of p meeting a cell of t (C82). Empty space
// ends passing and lets p in. While p passes, matter costs a cell of its
// passing, matter no denser than p ends it, and p steps in all the same.
// Otherwise matter stops p, or bounces it if it bounces.
func meet(p *Particle, t *matProps) meeting {
	switch {
	case t.kind == material.Empty:
		p.Flags &^= Passing
	case p.Flags&Passing != 0:
		if p.Pass < 0 {
			p.Flags &^= Passing
		}
		if p.Pass > -2 {
			p.Pass--
		}
		if uint16(p.Density) >= solidity(t) {
			p.Flags &^= Passing
		}
	case p.Flags&Bounces != 0:
		return bounces
	default:
		return stops
	}
	return enters
}

// solidity returns the density a particle passing through a cell of t
// compares with its own: a solid's on the scale of the others.
func solidity(t *matProps) uint16 {
	if t.kind == material.Solid {
		return uint16(t.density) * solidDensityScale
	}
	return uint16(t.density)
}

// bounce turns p back off matter it hit heading along (ux, uy), as Noita
// bounces a particle that is not a liquid's (C86): the speed along the
// path's larger axis turns around, a slow speed across x gets a kick, and
// both axes lose most of their speed. A slow speck with long to live gets
// little, and a very slow one stops bouncing. p stays where it is.
func (w *World) bounce(p *Particle, ux, uy int64) {
	s := rng.New(rng.Hash3(p.Seed^particleSalt, uint64(w.tick), 0, 0))
	if abs64(ux) > abs64(uy) {
		p.VX = -p.VX
	} else {
		p.VY = -p.VY
	}
	if abs64(int64(p.VX)) < bounceSlowX {
		p.VX += int32(s.IntN(2*bounceKick+1) - bounceKick)
	}
	keep := int64(bounceKeep + s.IntN(bounceKeepSpread+1))
	p.VX = int32(int64(p.VX) * keep / ParticleOne)
	p.VY = int32(int64(p.VY) * keep / ParticleOne)
	speed2 := int64(p.VX)*int64(p.VX) + int64(p.VY)*int64(p.VY)
	if p.Life > 60 && speed2 < bounceLifeSpeed*bounceLifeSpeed {
		p.Life = int32(bounceLife + s.IntN(bounceLifeSpread))
	}
	if speed2 < bounceStop*bounceStop {
		p.Flags &^= Bounces
	}
}

// clampSpeed returns v held within maxSpeed either way.
func clampSpeed(v int32) int32 {
	return min(max(v, -maxSpeed), maxSpeed)
}

// abs64 returns |v|.
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// distance returns the length of (dx, dy), rounded down. Both must lie
// within 2^31 either way.
func distance(dx, dy int64) int64 {
	return int64(isqrt(uint64(dx*dx) + uint64(dy*dy)))
}

// isqrt returns the square root of v rounded down, in integers, so it is
// the same on every CPU: Newton's steps from a power of two at or above the
// root, which only come down until they reach it.
func isqrt(v uint64) uint64 {
	if v < 2 {
		return v
	}
	x := uint64(1) << ((bits.Len64(v) + 1) / 2)
	for {
		y := (x + v/x) / 2
		if y >= x {
			return x
		}
		x = y
	}
}
