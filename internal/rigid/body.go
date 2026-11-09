package rigid

import (
	"fmt"
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
// material, some of which may be gone, and the cells it was last written
// to.
//
// The image lies in the body's frame, Box2D's, where the body was first
// made: a rectangle centered on the origin. A piece broken off later has
// the frame of the body it broke from and an image cropped to the piece,
// so a cell maps to the same pixel through either.
type body struct {
	id  physics.Body
	key uint32
	mat material.ID
	w   int
	h   int
	// left and top are where, in the frame, the first body's image began,
	// in cells from the origin; cx and cy are where this image begins in
	// that one, in pixels. Pixel (i, j) covers the frame's cells from
	// (left + cx + i, top + cy + j).
	left, top float32
	cx, cy    int
	// px holds the image's pixels, row by row, and n counts those it still
	// holds.
	px []pixel
	n  int
	// dirty is set when pixels left the image since the last Break, which
	// splits the body and shapes it anew; redraw has the next Write write
	// it again though it did not move. dead marks a body Break removes.
	dirty, redraw, dead bool
	// cover marks, once the body has lost pixels, the pixels its shapes
	// cover: those it has and those a shape reaches into, where the
	// outline was simplified. Nil while the body is whole: its shape is
	// the rectangle of its image.
	cover []bool
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
	// v and spin are the body's velocity, read once per tick when needed
	// (haveV).
	v     physics.Vec2
	spin  float32
	haveV bool
	// mass is the body's mass in kilograms, inertia its rotational
	// inertia about its center of mass in kg m², lc its center of mass in
	// its frame, in meters, all from its shapes; buoyancy is its
	// material's.
	mass, inertia, buoyancy float32
	lc                      physics.Vec2
	// The liquid cells this tick's write pushed away: how many (dragN),
	// the sums of their offsets from the center of mass in cells (dragX,
	// dragY) and of their squared distances from it (dragR2). The drag
	// of each is linear in its offset, so the sums give the total (C42).
	dragN, dragX, dragY, dragR2 float32
	// frozen is set while the body waits, out of Box2D's simulation, for a
	// block not in memory that it may reach (freeze); fv, fspin and fawake
	// are the velocity it had then and whether it was awake, which it gets
	// back when it goes on. cold marks the bodies freeze finds this tick,
	// and reach the cells it found the body may touch.
	frozen, fawake, cold bool
	fv                   physics.Vec2
	fspin                float32
	reach                rect
}

// pixel is a pixel of a body's image: the damage it has taken
// (world.Cell.Damage) and its tone, which move with it, and whether the
// image still holds it. One small value, so writing a cell reads one
// place.
type pixel struct {
	dmg  uint16
	tone uint8
	ok   bool
}

// cellAt is a cell's position in the world.
type cellAt struct{ x, y int32 }

// newBody returns a body of material m, w × h pixels, all there, centered
// on the origin of its frame, for Box2D's body id. Its pixels' tones come
// from (seed, key, pixel), so they stay with the body wherever it goes.
func newBody(id physics.Body, key uint32, m material.ID, w, h int, seed uint64) body {
	n := w * h
	b := body{
		id: id, key: key, mat: m, w: w, h: h,
		left: -float32(w) / 2, top: -float32(h) / 2,
		px: make([]pixel, n), n: n,
		// A turned rectangle holds at most its area, half its perimeter
		// and one cell centers.
		cells: make([]cellAt, 0, n+w+h+1),
	}
	for k := range b.px {
		b.px[k] = pixel{tone: uint8(rng.Hash3(seed^toneSalt, uint64(key), uint64(k), 0) >> 56), ok: true}
	}
	return b
}

// middle returns where the middle of b's image lies from its origin, in
// cells, with b turned by (co, si). For a body as it was made it is the
// origin.
func (b *body) middle(co, si float32) (float32, float32) {
	mx := b.left + float32(b.cx) + float32(b.w)/2
	my := b.top + float32(b.cy) + float32(b.h)/2
	return float32(co*mx) - float32(si*my), float32(si*mx) + float32(co*my)
}

// center returns where b's center of mass lies from its origin, in cells,
// with b turned by (co, si).
func (b *body) center(co, si float32) (float32, float32) {
	lx, ly := b.lc.X*cellsPerMeter, b.lc.Y*cellsPerMeter
	return float32(co*lx) - float32(si*ly), float32(si*lx) + float32(co*ly)
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
// takes the pixel it falls in, unless that pixel is gone. So a turned body
// has no holes and nothing outside its rectangle; a pixel may fill two
// cells or none. Each row of cells is scanned only across the span the
// rectangle crosses. The multiplications are converted one by one, so Go
// cannot fuse them with an addition and round differently on another CPU
// (docs/pitfalls.md).
func (s *System) write(b *body, slot uint16) {
	// The origin, split into a whole cell and the rest: offsets from the
	// whole cell are small and exact, wherever the body is.
	px, py := float32(b.p.X*cellsPerMeter), float32(b.p.Y*cellsPerMeter)
	ox, oy := int(math.Floor(float64(px))), int(math.Floor(float64(py)))
	fx, fy := px-float32(ox), py-float32(oy)
	co, si := b.q.C, b.q.S
	hw, hh := float32(b.w)/2, float32(b.h)/2
	// The middle of the image in the frame and from the origin, and the
	// center of mass from the origin: cells' offsets from it give their
	// speed and the drag of the liquid they push away.
	mx, my := b.left+float32(b.cx)+hw, b.top+float32(b.cy)+hh
	wx, wy := b.middle(co, si)
	gx, gy := b.center(co, si)
	// The half width and height of the box around the turned rectangle,
	// and the rows it covers.
	ex := float32(abs32(co)*hw) + float32(abs32(si)*hh)
	ey := float32(abs32(si)*hw) + float32(abs32(co)*hh)
	r0, r1 := int(math.Floor(float64(fy+wy-ey))), int(math.Floor(float64(fy+wy+ey)))
	b.box = boxAt(b, b.p, b.q)
	b.haveV, b.blocked = false, 0
	b.dragN, b.dragX, b.dragY, b.dragR2 = 0, 0, 0, 0
	b.cells = b.cells[:0]
	// Read once: claim's writes through b could otherwise make Go read
	// them again for every cell.
	left, top, cx, cy, iw, ih, img := b.left, b.top, b.cx, b.cy, b.w, b.h, b.px
	for r := r0; r <= r1; r++ {
		dy := float32(r) + 0.5 - fy
		sv, cv := float32(si*dy), float32(co*dy)
		lo, hi, ok := within(co, sv-mx, hw+sampleEdge, wx-ex-1, wx+ex+1)
		if !ok {
			continue
		}
		if lo, hi, ok = within(-si, cv-my, hh+sampleEdge, lo, hi); !ok {
			continue
		}
		for k := int(math.Ceil(float64(lo + fx - 0.5))); k <= int(math.Floor(float64(hi+fx-0.5))); k++ {
			dx := float32(k) + 0.5 - fx
			// The pixel of the first image the center falls in, then of
			// this one. Pieces of one body share the first, so a cell
			// maps to the same pixel through any of them.
			i := int(math.Floor(float64(float32(co*dx)+sv-left))) - cx
			j := int(math.Floor(float64(cv-float32(si*dx)-top))) - cy
			// A cell whose center falls outside the image, or on a pixel
			// that is gone, stays out.
			if uint(i) >= uint(iw) || uint(j) >= uint(ih) || !img[j*iw+i].ok {
				continue
			}
			x, y := ox+k, oy+r
			if s.claim(b, slot, j*iw+i, x, y, dx-gx, dy-gy) {
				b.cells = append(b.cells, cellAt{int32(x), int32(y)})
			}
		}
	}
	if b.written {
		b.shiftX = float32(b.p.X-b.wp.X) * cellsPerMeter
		b.shiftY = float32(b.p.Y-b.wp.Y) * cellsPerMeter
	} else {
		// Written for the first time, the body has not moved yet, but may
		// already be moving: made again with its velocity, or broken off
		// a moving body. Where its velocity takes it in a tick stands for
		// the shift, so the terrain is made collision where its first Step
		// takes it (Codex review).
		s.velocity(b)
		b.shiftX = float32(b.v.X*tickSeconds) * cellsPerMeter
		b.shiftY = float32(b.v.Y*tickSeconds) * cellsPerMeter
	}
	b.wp, b.wq, b.written, b.force, b.redraw = b.p, b.q, true, false, false
	s.w.Flush()
}

// boxAt returns the cells b's turned rectangle covers with its origin at p
// turned by q: those write samples, and its box once written there.
func boxAt(b *body, p physics.Vec2, q physics.Rot) rect {
	px, py := float32(p.X*cellsPerMeter), float32(p.Y*cellsPerMeter)
	ox, oy := int(math.Floor(float64(px))), int(math.Floor(float64(py)))
	fx, fy := px-float32(ox), py-float32(oy)
	co, si := q.C, q.S
	hw, hh := float32(b.w)/2, float32(b.h)/2
	wx, wy := b.middle(co, si)
	ex := float32(abs32(co)*hw) + float32(abs32(si)*hh)
	ey := float32(abs32(si)*hw) + float32(abs32(co)*hh)
	return rect{
		ox + int(math.Floor(float64(fx+wx-ex))), oy + int(math.Floor(float64(fy+wy-ey))),
		ox + int(math.Floor(float64(fx+wx+ex))) + 1, oy + int(math.Floor(float64(fy+wy+ey))) + 1,
	}
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
// world's queue of cells to put back; a liquid drags on the body as it
// goes (C42, push). Another body's cell goes to the body
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
		b.dragN++
		b.dragX += rx
		b.dragY += ry
		b.dragR2 += float32(rx*rx) + float32(ry*ry)
		s.w.Displace(x, y)
	}
	px := b.px[k]
	s.w.Put(x, y, world.BodyCell(b.mat, px.tone, px.dmg, slot, uint16(k)))
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
// b at (rx, ry) cells from its center of mass.
func (s *System) pixelSpeed2(b *body, rx, ry float32) float32 {
	s.velocity(b)
	mx, my := rx/cellsPerMeter, ry/cellsPerMeter
	vx := b.v.X - float32(b.spin*my)
	vy := b.v.Y + float32(b.spin*mx)
	return float32(vx*vx) + float32(vy*vy)
}

// reaches reports whether cell (x, y) and b's shapes, where b was last
// written, overlap by more than keepOutDepth cells: along each of the
// cell's axes and the body's, their extents overlap by that much with the
// rectangle of b's image, and, once b has lost pixels, with one of the
// pixels its shapes cover (cover). The multiplications are converted one
// by one, as in write.
func (b *body) reaches(x, y int) bool {
	px, py := float32(b.wp.X*cellsPerMeter), float32(b.wp.Y*cellsPerMeter)
	ox, oy := int(math.Floor(float64(px))), int(math.Floor(float64(py)))
	co, si := b.wq.C, b.wq.S
	wx, wy := b.middle(co, si)
	dx := float32(x-ox) + 0.5 - (px - float32(ox)) - wx
	dy := float32(y-oy) + 0.5 - (py - float32(oy)) - wy
	ac, as := abs32(co), abs32(si)
	hw, hh := float32(b.w)/2, float32(b.h)/2
	const d = keepOutDepth - 0.5 // the cell's half side counts once per axis
	cell := (ac + as) / 2
	lu, lv := float32(co*dx)+float32(si*dy), float32(co*dy)-float32(si*dx)
	if abs32(dx) >= float32(ac*hw)+float32(as*hh)-d ||
		abs32(dy) >= float32(as*hw)+float32(ac*hh)-d ||
		abs32(lu) >= hw+cell-keepOutDepth ||
		abs32(lv) >= hh+cell-keepOutDepth {
		return false
	}
	return b.cover == nil || b.reachesCover(lu+hw, lv+hh, co, si, cell)
}

// reachesCover reports whether a cell whose center lies at (fi, fj) in b's
// image, in pixels, overlaps by more than keepOutDepth a pixel b's shapes
// cover, with b turned by (co, si); cell is the cell's half extent along
// b's axes. A cell overlaps only pixels whose centers lie within about a
// pixel and a half of its own.
func (b *body) reachesCover(fi, fj, co, si, cell float32) bool {
	lim := cell + 0.5 - keepOutDepth
	i0, j0 := int(math.Floor(float64(fi)))-2, int(math.Floor(float64(fj)))-2
	for j := max(j0, 0); j <= min(j0+4, b.h-1); j++ {
		for i := max(i0, 0); i <= min(i0+4, b.w-1); i++ {
			if !b.cover[j*b.w+i] {
				continue
			}
			au, av := fi-float32(i)-0.5, fj-float32(j)-0.5
			if abs32(au) < lim && abs32(av) < lim &&
				abs32(float32(co*au)-float32(si*av)) < lim && abs32(float32(si*au)+float32(co*av)) < lim {
				return true
			}
		}
	}
	return false
}

// velocity reads b's velocity from Box2D into b.v and b.spin, once per
// tick.
func (s *System) velocity(b *body) {
	if !b.haveV {
		b.v, b.spin = s.phys.Velocity(b.id)
		b.haveV = true
	}
}

// liquidK scales the drag of a liquid cell on a body (C42). Noita's.
const liquidK = float32(-0.05)

// push adds to b, for the coming Step, what liquid does to it as in Noita:
// the drag of the liquid cells this tick's write pushed away (C42), which
// wakes it, and the buoyancy of liquid under it (C41), which does not.
func (s *System) push(b *body) error {
	drag := b.dragN > 0
	lift := b.buoyancy > 0 && s.overLiquid(b)
	if !drag && !lift {
		return nil
	}
	s.velocity(b)
	var f physics.Vec2
	var torque float32
	if drag {
		const m2 = cellsPerMeter * cellsPerMeter
		c := float32(speed(b.v)*liquidK) * b.mass
		f, torque = settleDrag(c, b.mass, b.inertia, b.v, b.spin,
			contacts{b.dragN, b.dragX / cellsPerMeter, b.dragY / cellsPerMeter, b.dragR2 / m2})
		b.dragN, b.dragX, b.dragY, b.dragR2 = 0, 0, 0, 0
	}
	if lift {
		// buoyancy rounds its products, so they do not fuse with these
		// additions on a CPU with FMA (docs/pitfalls.md).
		up := buoyancy(b.buoyancy, b.mass, b.v)
		f = physics.Vec2{X: f.X + up.X, Y: f.Y + up.Y}
	}
	if err := s.phys.Push(b.id, f, torque, drag); err != nil {
		return fmt.Errorf("rigid: %w", err)
	}
	return nil
}

// overLiquid reports whether the cell just under b, in the column of its
// center of mass, holds a liquid: the one point where b feels the liquid
// that holds it up (🔶; Noita samples one point too, C41).
func (s *System) overLiquid(b *body) bool {
	if !b.written {
		return false
	}
	gx, _ := b.center(b.q.C, b.q.S)
	x := int(math.Floor(float64(float32(b.p.X*cellsPerMeter) + gx)))
	c, ok := s.w.At(x, b.box.y1)
	return ok && s.kinds[c.Mat] == material.Liquid
}

// contacts sums the liquid cells a body pushed away: how many there were
// (n), their offsets from its center of mass in meters (sx, sy) and their
// squared distances from it (r2).
type contacts struct{ n, sx, sy, r2 float32 }

// speed returns |v|.
func speed(v physics.Vec2) float32 {
	return float32(math.Sqrt(float64(float32(v.X*v.X) + float32(v.Y*v.Y))))
}

// liquidDrag returns the force and torque the liquid cells k put on a body
// moving at v and spinning at w (C42). Each cell at r from the center of
// mass pushes against the velocity of that point, v + w × r, times c, with
// the torque r × F. Noita's c is |v| K m, from the body's speed and mass.
// That is linear in r, so the sums in k give the total. Products are
// rounded on return, so a caller's additions cannot fuse with them.
func liquidDrag(c float32, v physics.Vec2, w float32, k contacts) (physics.Vec2, float32) {
	f := physics.Vec2{
		X: float32(c * (float32(k.n*v.X) - float32(w*k.sy))),
		Y: float32(c * (float32(k.n*v.Y) + float32(w*k.sx))),
	}
	return f, float32(c * (float32(v.Y*k.sx) - float32(v.X*k.sy) + float32(w*k.r2)))
}

// settleDrag returns the drag of the liquid cells k on a body of mass m and
// rotational inertia inertia, moving at v and spinning at w, as a force
// and torque for the coming tick: Noita's (liquidDrag, with c from the
// speed now) at the velocity the body ends the tick with, not at the one it
// starts with (backward Euler). Box2D holds a force through the whole tick,
// and a wide body entering water fast would get more than all its speed
// taken away, leaving faster than it came (Codex review). This way the drag
// only slows it; at the speeds bodies sink at, the two hardly differ.
func settleDrag(c, m, inertia float32, v physics.Vec2, w float32, k contacts) (physics.Vec2, float32) {
	// (M - dt c A) u' = M u, where the drag is c A u with
	// A = [[n, 0, -sy], [0, n, sx], [-sy, sx, r2]] and u = (vx, vy, w).
	dk := -float64(c) * float64(tickSeconds)
	mm, ii := float64(m), float64(inertia)
	n, sx, sy, r2 := float64(k.n), float64(k.sx), float64(k.sy), float64(k.r2)
	vx, vy := float64(v.X), float64(v.Y)
	a := mm + float64(dk*n)
	den := ii + float64(dk*r2) - float64(float64(dk*dk)*(float64(sx*sx)+float64(sy*sy)))/a
	num := float64(ii*float64(w)) + float64(float64(dk*mm)*(float64(sy*vx)-float64(sx*vy)))/a
	w1 := num / den
	v1 := physics.Vec2{
		X: float32((float64(mm*vx) + float64(float64(dk*sy)*w1)) / a),
		Y: float32((float64(mm*vy) - float64(float64(dk*sx)*w1)) / a),
	}
	return liquidDrag(c, v1, float32(w1), k)
}

// buoyancy returns the force liquid under a body of buoyancy b and mass m,
// moving at v, holds it up with (C41): -b m (v + g). At b = 1 it cancels
// gravity and brakes the body to a stop. It adds no torque. Products are
// rounded on return, so a caller's additions cannot fuse with them.
func buoyancy(b, m float32, v physics.Vec2) physics.Vec2 {
	k := -b * m
	return physics.Vec2{X: float32(k * v.X), Y: float32(k * (v.Y + gravity))}
}

// rect is a half-open rectangle of cells, [x0, x1) × [y0, y1).
type rect struct{ x0, y0, x1, y1 int }

func (r rect) overlaps(o rect) bool {
	return r.x0 < o.x1 && o.x0 < r.x1 && r.y0 < o.y1 && o.y0 < r.y1
}
