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
// material, and the cells it was last written to.
type body struct {
	id  physics.Body
	key uint32
	mat material.ID
	w   int
	h   int
	// tone holds each pixel's tone, row by row; its length is the number
	// of pixels.
	tone []uint8
	// cells lists the cells the body was last written to. A cell the body
	// sampled but did not write, because it kept what was there, is not
	// listed.
	cells []cellAt
	// blocked counts the cells that another body's pixels keep the body
	// out of: those not written because a body with a higher key held the
	// cell, and those such a body took since.
	blocked int
	// wp and wq are the transform the body was last written at, once
	// written is set. p and q are this tick's, and moved whether they
	// differ.
	wp      physics.Vec2
	wq      physics.Rot
	written bool
	p       physics.Vec2
	q       physics.Rot
	moved   bool
	// force has this Write rewrite the body though it did not move: a body
	// that may have covered some of its cells moved.
	force bool
	// box covers the turned rectangle at the last write, and with it every
	// cell whose center fell inside the image, written or not; a body too
	// thin for any cell center still has one. shiftX and shiftY are how far
	// the body moved then, in cells. near is box and where the shift would
	// take it next, widened by nearCells: the cells it may touch in the
	// coming Step.
	box            rect
	shiftX, shiftY float32
	near           rect
	// v and spin are the body's velocity, read once per write when a cell
	// needs its speed (haveV).
	v     physics.Vec2
	spin  float32
	haveV bool
}

// cellAt is a cell's position in the world.
type cellAt struct{ x, y int32 }

// newBody returns a body of material m, w × h pixels, for Box2D's body id.
// Its pixels' tones come from (seed, key, pixel), so they stay with the
// body wherever it goes.
func newBody(id physics.Body, key uint32, m material.ID, w, h int, seed uint64) body {
	n := w * h
	b := body{
		id: id, key: key, mat: m, w: w, h: h, tone: make([]uint8, n),
		// A turned rectangle holds at most its area, half its perimeter
		// and one cell centers.
		cells: make([]cellAt, 0, n+w+h+1),
	}
	for k := range b.tone {
		b.tone[k] = uint8(rng.Hash3(seed^toneSalt, uint64(key), uint64(k), 0) >> 56)
	}
	return b
}

// erase empties the cells b, at slot, was written to and still holds. A
// cell another body took since is that body's now.
func (s *System) erase(b *body, slot uint16) {
	for _, at := range b.cells {
		x, y := int(at.x), int(at.y)
		if c, ok := s.w.At(x, y); ok && s.kinds[c.Mat] == material.Solid {
			if owner, _ := c.Body(); owner == slot {
				s.w.Put(x, y, world.Cell{})
			}
		}
	}
	b.cells = b.cells[:0]
	s.w.Flush()
}

// sampleEdge widens the rows' spans in write, in cells, so float32's
// rounding never leaves out a cell whose center lies inside the body.
const sampleEdge = 1.0 / 64

// write writes b, at slot, where its transform puts it: every cell whose
// center falls inside the body's image, mapped back through the transform,
// takes the pixel it falls in. So a turned body has no holes and nothing
// outside its rectangle; a pixel may fill two cells or none. Each row of
// cells is scanned only across the span the rectangle crosses. The
// multiplications are converted one by one, so Go cannot fuse them with an
// addition and round differently on another CPU (docs/pitfalls.md).
func (s *System) write(b *body, slot uint16) {
	// The center, split into a whole cell and the rest: offsets from the
	// whole cell are small and exact, wherever the body is.
	px, py := float32(b.p.X*cellsPerMeter), float32(b.p.Y*cellsPerMeter)
	ox, oy := int(math.Floor(float64(px))), int(math.Floor(float64(py)))
	fx, fy := px-float32(ox), py-float32(oy)
	co, si := b.q.C, b.q.S
	hw, hh := float32(b.w)/2, float32(b.h)/2
	// The half width and height of the box around the turned rectangle,
	// and the cells it covers.
	ex := float32(abs32(co)*hw) + float32(abs32(si)*hh)
	ey := float32(abs32(si)*hw) + float32(abs32(co)*hh)
	r0, r1 := int(math.Floor(float64(fy-ey))), int(math.Floor(float64(fy+ey)))
	b.box = rect{
		ox + int(math.Floor(float64(fx-ex))), oy + r0,
		ox + int(math.Floor(float64(fx+ex))) + 1, oy + r1 + 1,
	}
	b.haveV, b.blocked = false, 0
	b.cells = b.cells[:0]
	for r := r0; r <= r1; r++ {
		dy := float32(r) + 0.5 - fy
		sv, cv := float32(si*dy), float32(co*dy)
		lo, hi, ok := within(co, sv, hw+sampleEdge, -ex-1, ex+1)
		if !ok {
			continue
		}
		if lo, hi, ok = within(-si, cv, hh+sampleEdge, lo, hi); !ok {
			continue
		}
		for k := int(math.Ceil(float64(lo + fx - 0.5))); k <= int(math.Floor(float64(hi+fx-0.5))); k++ {
			dx := float32(k) + 0.5 - fx
			i := int(math.Floor(float64(float32(co*dx) + sv + hw)))
			j := int(math.Floor(float64(cv - float32(si*dx) + hh)))
			// A cell whose center falls outside the image stays out; an
			// image with pixels missing would leave those out here too.
			if i < 0 || i >= b.w || j < 0 || j >= b.h {
				continue
			}
			x, y := ox+k, oy+r
			if s.claim(b, slot, j*b.w+i, x, y, dx, dy) {
				b.cells = append(b.cells, cellAt{int32(x), int32(y)})
			}
		}
	}
	if b.written {
		b.shiftX = float32(b.p.X-b.wp.X) * cellsPerMeter
		b.shiftY = float32(b.p.Y-b.wp.Y) * cellsPerMeter
	}
	b.wp, b.wq, b.written, b.force = b.p, b.q, true, false
	s.w.Flush()
}

// within returns the offsets d in [lo, hi] for which |a·d + c| <= half, and
// false if there are none.
func within(a, c, half, lo, hi float32) (from, to float32, ok bool) {
	if a == 0 {
		return lo, hi, abs32(c) <= half
	}
	d0, d1 := (-half-c)/a, (half-c)/a
	if a < 0 {
		d0, d1 = d1, d0
	}
	from, to = max(lo, d0), min(hi, d1)
	return from, to, from <= to
}

// abs32 returns |f|.
func abs32(f float32) float32 {
	return math.Float32frombits(math.Float32bits(f) &^ (1 << 31))
}

// claim decides what becomes of the cell at (x, y) when b, at slot, samples
// its pixel k there, and writes the pixel there if it may. (rx, ry) is the
// cell's offset from the body's center in cells, for its speed. Air and
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
		// A cell of the body's own is one it held before it was written
		// again without moving.
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
	return true
}

// givesWay reports whether resting powder c gives way to pixel k of b, at
// (rx, ry) cells from its center. Only settled powder may, and only to a
// body big enough, moving fast enough at that point.
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
	// the order cells are written in does not matter.
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

// reaches reports whether cell (x, y) and b's shape, where b was last
// written, overlap by more than keepOutDepth cells: along each of the
// cell's axes and the body's, their extents overlap by that much. The
// multiplications are converted one by one, as in write.
func (b *body) reaches(x, y int) bool {
	px, py := float32(b.wp.X*cellsPerMeter), float32(b.wp.Y*cellsPerMeter)
	ox, oy := int(math.Floor(float64(px))), int(math.Floor(float64(py)))
	dx := float32(x-ox) + 0.5 - (px - float32(ox))
	dy := float32(y-oy) + 0.5 - (py - float32(oy))
	co, si := b.wq.C, b.wq.S
	ac, as := abs32(co), abs32(si)
	hw, hh := float32(b.w)/2, float32(b.h)/2
	const d = keepOutDepth - 0.5 // the cell's half side counts once per axis
	cell := (ac + as) / 2
	return abs32(dx) < float32(ac*hw)+float32(as*hh)-d &&
		abs32(dy) < float32(as*hw)+float32(ac*hh)-d &&
		abs32(float32(co*dx)+float32(si*dy)) < hw+cell-keepOutDepth &&
		abs32(float32(co*dy)-float32(si*dx)) < hh+cell-keepOutDepth
}

// rect is a half-open rectangle of cells, [x0, x1) × [y0, y1).
type rect struct{ x0, y0, x1, y1 int }

func (r rect) overlaps(o rect) bool {
	return r.x0 < o.x1 && o.x0 < r.x1 && r.y0 < o.y1 && o.y0 < r.y1
}
