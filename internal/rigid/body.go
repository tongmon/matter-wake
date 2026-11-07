package rigid

import (
	"math"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// How a body's pixel meets a settled powder, Noita's rule (C134 5.1c in
// docs/briefs/body-grid-coupling.md): a body with a side shorter than
// minSideOverSand never pushes it away. Otherwise the pixel's speed decides,
// its square in (m/s)²: below slowSand the powder stays, from fastSand on
// it is pushed away, and in between with a chance that grows from 0 to 1
// across the band (🧪, our own: Noita's is not known).
const (
	minSideOverSand = 8
	slowSand        = 5
	fastSand        = 21
)

// Salts that key a body's random choices apart from the world's.
const (
	toneSalt = 0x3c6ef372fe94f82b
	sandSalt = 0xa54ff53a5f1d36f1
)

// body is a rigid body: its Box2D handle, its image of w × h pixels of one
// material, and where each pixel was last written.
type body struct {
	id  physics.Body
	key uint32
	mat material.ID
	w   int
	h   int
	// tone holds each pixel's tone, row by row; its length is the number
	// of pixels.
	tone []uint8
	// x and y are where each pixel was last written, while placed is set.
	// A pixel is not placed when its cell kept what was there.
	x, y   []int32
	placed []bool
	// blocked counts the pixels that another body's pixels keep out of
	// their cells: those not written because a body with a higher key held
	// the cell, and those such a body took since.
	blocked int
	// wp and wq are the transform the pixels were last written at, once
	// written is set. p and q are this tick's, and moved whether they
	// differ.
	wp      physics.Vec2
	wq      physics.Rot
	written bool
	p       physics.Vec2
	q       physics.Rot
	moved   bool
	// force has this Write rewrite the body though it did not move: a body
	// that may have covered some of its pixels moved.
	force bool
	// box covers the cells the pixels mapped to at the last write, and
	// shiftX and shiftY are how far the body moved then, in cells. near is
	// box and where the shift would take it next, widened by nearCells:
	// the cells it may touch in the coming Step.
	box            rect
	shiftX, shiftY float32
	near           rect
	// v and spin are the body's velocity, read once per write when a pixel
	// needs its speed (haveV).
	v     physics.Vec2
	spin  float32
	haveV bool
}

// newBody returns a body of material m, w × h pixels, for Box2D's body id.
// Its pixels' tones come from (seed, key, pixel), so they stay with the
// body wherever it goes.
func newBody(id physics.Body, key uint32, m material.ID, w, h int, seed uint64) body {
	n := w * h
	b := body{
		id: id, key: key, mat: m, w: w, h: h,
		tone: make([]uint8, n), x: make([]int32, n), y: make([]int32, n),
		placed: make([]bool, n),
	}
	for k := range b.tone {
		b.tone[k] = uint8(rng.Hash3(seed^toneSalt, uint64(key), uint64(k), 0) >> 56)
	}
	return b
}

// erase empties the cells the pixels of b, at slot, were written to and
// still hold. A cell another body took since is that body's now.
func (s *System) erase(b *body, slot uint16) {
	for k, on := range b.placed {
		if !on {
			continue
		}
		x, y := int(b.x[k]), int(b.y[k])
		if c, ok := s.w.At(x, y); ok && s.kinds[c.Mat] == material.Solid {
			if owner, _ := c.Body(); owner == slot {
				s.w.Put(x, y, world.Cell{})
			}
		}
		b.placed[k] = false
	}
	s.w.Flush()
}

// write writes the pixels of b, at slot, where its transform puts them:
// each pixel's center, turned and moved with the body, rounded down on
// each axis to a cell. Two pixels may meet in one cell; the later one
// holds it. The multiplications are converted one by one, so Go cannot
// fuse them with an addition and round differently on another CPU
// (docs/pitfalls.md).
func (s *System) write(b *body, slot uint16) {
	px, py := float32(b.p.X*cellsPerMeter), float32(b.p.Y*cellsPerMeter)
	co, si := b.q.C, b.q.S
	hw, hh := float32(b.w)/2, float32(b.h)/2
	box := rect{math.MaxInt, math.MaxInt, math.MinInt, math.MinInt}
	b.haveV, b.blocked = false, 0
	k := 0
	for j := range b.h {
		v := float32(j) + 0.5 - hh
		sv, cv := float32(si*v), float32(co*v)
		for i := range b.w {
			u := float32(i) + 0.5 - hw
			rx := float32(co*u) - sv
			ry := float32(si*u) + cv
			x, y := int(math.Floor(float64(px+rx))), int(math.Floor(float64(py+ry)))
			box.x0, box.y0 = min(box.x0, x), min(box.y0, y)
			box.x1, box.y1 = max(box.x1, x+1), max(box.y1, y+1)
			b.placed[k] = s.claim(b, slot, k, x, y, rx, ry)
			k++
		}
	}
	if b.written {
		b.shiftX = float32(b.p.X-b.wp.X) * cellsPerMeter
		b.shiftY = float32(b.p.Y-b.wp.Y) * cellsPerMeter
	}
	b.box, b.wp, b.wq, b.written, b.force = box, b.p, b.q, true, false
	s.w.Flush()
}

// claim decides what becomes of the cell at (x, y) when pixel k of b, at
// slot, maps to it, and writes the pixel there if it may. (rx, ry) is the
// pixel's offset from the body's center in cells, for its speed. Air and
// the body's own cells take the pixel. Static terrain keeps its cell, as
// does a powder at rest unless it has settled and the body hits it hard
// enough (givesWay). Liquids and other powders are pushed into the
// world's queue of cells to put back. Another body's cell goes to the body
// with the higher key: the one written later, which takes it. Either way
// the body that loses the cell counts it as blocked.
func (s *System) claim(b *body, slot uint16, k, x, y int, rx, ry float32) bool {
	c, ok := s.w.At(x, y)
	if !ok {
		return false
	}
	switch s.kinds[c.Mat] {
	case material.Empty:
	case material.Solid:
		// A cell of the body's own is a pixel that met another.
		if other, _ := c.Body(); other != slot {
			if b.key < s.bodies[other].key {
				b.blocked++
				return false
			}
			s.bodies[other].blocked++
		}
	case material.Static:
		return false
	case material.Powder:
		if c.Resting() && !s.givesWay(b, k, c, rx, ry) {
			return false
		}
		s.w.Displace(x, y)
	case material.Liquid:
		s.w.Displace(x, y)
	}
	s.w.Put(x, y, world.BodyCell(b.mat, b.tone[k], slot, uint16(k)))
	b.x[k], b.y[k] = int32(x), int32(y)
	return true
}

// givesWay reports whether resting powder c gives way to pixel k of b. Only
// settled powder may, and only to a body big enough, moving fast enough at
// that pixel.
func (s *System) givesWay(b *body, k int, c world.Cell, rx, ry float32) bool {
	if !c.Settled() || min(b.w, b.h) < minSideOverSand {
		return false
	}
	v2 := s.pixelSpeed2(b, rx, ry)
	switch {
	case v2 < slowSand:
		return false
	case v2 >= fastSand:
		return true
	}
	// A chance from 0 at slowSand to 1 at fastSand, drawn from a hash so
	// the order pixels are written in does not matter.
	h := rng.Hash3(s.w.Seed()^sandSalt, uint64(s.w.Tick()), uint64(b.key), uint64(k))
	return float32(h>>40) < float32((v2-slowSand)/(fastSand-slowSand)*(1<<24))
}

// pixelSpeed2 returns the square of the speed, in (m/s)², of the point of
// b at (rx, ry) cells from its center, which is its center of mass.
func (s *System) pixelSpeed2(b *body, rx, ry float32) float32 {
	if !b.haveV {
		b.v, b.spin = s.phys.Velocity(b.id)
		b.haveV = true
	}
	mx, my := rx/cellsPerMeter, ry/cellsPerMeter
	vx := b.v.X - float32(b.spin*my)
	vy := b.v.Y + float32(b.spin*mx)
	return float32(vx*vx) + float32(vy*vy)
}

// rect is a half-open rectangle of cells, [x0, x1) × [y0, y1).
type rect struct{ x0, y0, x1, y1 int }

func (r rect) overlaps(o rect) bool {
	return r.x0 < o.x1 && o.x0 < r.x1 && r.y0 < o.y1 && o.y0 < r.y1
}
