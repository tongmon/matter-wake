package rigid

import (
	"fmt"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

// minPiece is the fewest pixels a piece of a broken body needs to be a
// body of its own. 🔶 Noita makes bodies of pieces of 2 pixels or more and
// leaves single pixels out (C77, C78); where they go is not known yet
// (C135). Here the pieces left out go to the inventory of whoever broke
// the body, with what was dug.
const minPiece = 2

// piece is a 4-connected piece of a body's image: how many pixels it has,
// the first of them in row order, and the pixels around it, [x0, x1) ×
// [y0, y1).
type piece struct {
	size, first    int
	x0, y0, x1, y1 int
}

// Wear deals amount damage to the body pixel the cell at (x, y) shows, as
// world.World.Wear does to terrain, and reports whether that uses the pixel
// up, with its material. A used-up pixel leaves the body's image and the
// cell becomes air; the caller keeps the material, and the next Break
// splits the body. A pixel that is not used up keeps its damage, which
// the cell shows and which moves with the body. A pixel two cells show
// wears once between Breaks. Wear does nothing to a cell that shows no
// pixel. Whether the tool may dig the material is the caller's choice.
func (s *System) Wear(x, y int, amount uint32) (material.ID, bool) {
	c, ok := s.w.At(x, y)
	if !ok || amount == 0 || s.kinds[c.Mat] != material.Solid {
		return 0, false
	}
	slot, k := c.Body()
	if int(slot) >= len(s.bodies) {
		return 0, false
	}
	b := &s.bodies[slot]
	// A pixel dug out earlier in the tick may still show in its other
	// cell until the body is written again.
	if int(k) >= len(b.px) || !b.px[k].ok {
		return 0, false
	}
	key := uint32(slot)<<16 | uint32(k)
	if slices.Contains(s.worn, key) {
		return 0, false
	}
	damage, gone := world.Worn(s.mats.Def(b.mat).HP, b.px[k].dmg, amount)
	if !gone {
		s.worn = append(s.worn, key)
		b.px[k].dmg = damage
		c.Damage = damage
		s.w.Put(x, y, c)
		s.w.Flush()
		return 0, false
	}
	b.px[k] = pixel{}
	b.n--
	b.dirty, s.broken = true, true
	s.w.Put(x, y, world.Cell{})
	s.w.Flush()
	return b.mat, true
}

// Break splits every body that lost pixels since the last Break, in key
// order, into its 4-connected pieces; pixels that touch only at a corner
// are apart (C77). The largest piece keeps the body: its slot, key and
// Box2D body, which moves on as before. The other pieces become new
// bodies, with keys in the order their first pixels come in the image's
// rows, in the body's frame and with its velocity, awake (C35, C78). Each
// body that changed gets shapes made from its pixels, and the next Write
// writes it. Pieces smaller than minPiece, and the pixels of a new piece
// whose cells static terrain or another body holds (C79), are no body's:
// Break adds them to debris, by material. A body left with no piece is
// removed, and the bodies after it move down a slot.
func (s *System) Break(debris []int) error {
	s.worn = s.worn[:0]
	if !s.broken {
		return nil
	}
	s.broken = false
	dead := false
	for slot, n := 0, len(s.bodies); slot < n; slot++ {
		if !s.bodies[slot].dirty {
			continue
		}
		gone, err := s.split(slot, debris)
		if err != nil {
			return err
		}
		dead = dead || gone
	}
	if dead {
		s.compact()
	}
	return nil
}

// split breaks the body at slot into its pieces and reports whether no
// piece is left of it.
func (s *System) split(slot int, debris []int) (bool, error) {
	b := &s.bodies[slot]
	b.dirty = false
	s.find(b)
	keep := -1
	for i := range s.pieces {
		p := &s.pieces[i]
		switch {
		case p.size < minPiece:
			s.drop(b, i, debris)
		case keep < 0 || p.size > s.pieces[keep].size:
			keep = i
		}
	}
	if keep < 0 {
		s.erase(b, uint16(slot))
		if b.written {
			s.gone = append(s.gone, b.box)
		}
		s.phys.DestroyBody(b.id)
		b.dead = true
		return true, nil
	}
	if len(s.pieces) > 1 {
		v, spin := s.phys.Velocity(b.id)
		p, q := s.phys.Transform(b.id)
		for i := range s.pieces {
			if i != keep && s.pieces[i].size >= minPiece {
				if err := s.detach(slot, i, p, q, v, spin, debris); err != nil {
					return false, err
				}
			}
		}
	}
	b = &s.bodies[slot]
	s.crop(b, keep)
	return false, s.reshape(b)
}

// find lists in s.pieces the 4-connected pieces of b's image, in the order
// their first pixels come in its rows, and marks in s.label each pixel's
// piece, -1 for none.
func (s *System) find(b *body) {
	n := b.w * b.h
	s.label = slices.Grow(s.label[:0], n)[:n]
	for k := range s.label {
		s.label[k] = -1
	}
	s.pieces = s.pieces[:0]
	for k, px := range b.px {
		if !px.ok || s.label[k] >= 0 {
			continue
		}
		id := int32(len(s.pieces))
		p := piece{first: k, x0: b.w, y0: b.h}
		s.label[k] = id
		s.queue = append(s.queue[:0], int32(k))
		for len(s.queue) > 0 {
			at := int(s.queue[len(s.queue)-1])
			s.queue = s.queue[:len(s.queue)-1]
			i, j := at%b.w, at/b.w
			p.size++
			p.x0, p.y0, p.x1, p.y1 = min(p.x0, i), min(p.y0, j), max(p.x1, i+1), max(p.y1, j+1)
			for _, nb := range [4][2]int{{i - 1, j}, {i + 1, j}, {i, j - 1}, {i, j + 1}} {
				if nb[0] < 0 || nb[1] < 0 || nb[0] >= b.w || nb[1] >= b.h {
					continue
				}
				if m := nb[1]*b.w + nb[0]; b.px[m].ok && s.label[m] < 0 {
					s.label[m] = id
					s.queue = append(s.queue, int32(m))
				}
			}
		}
		s.pieces = append(s.pieces, p)
	}
}

// drop takes piece i out of b's image into debris.
func (s *System) drop(b *body, i int, debris []int) {
	p := &s.pieces[i]
	for j := p.y0; j < p.y1; j++ {
		for x := p.x0; x < p.x1; x++ {
			if k := j*b.w + x; s.label[k] == int32(i) {
				b.px[k] = pixel{}
			}
		}
	}
	b.n -= p.size
	debris[b.mat] += p.size
}

// detach makes piece i of the body at slot a body of its own, in the body's
// frame at p, q and moving at v, spin, and takes it out of the body's
// image. Its pixels whose cells static terrain, the world's edge or
// another body hold go to debris instead (C79), and so does all of it if
// that leaves fewer than minPiece pixels or the world holds as many bodies
// as it can.
func (s *System) detach(slot, i int, p physics.Vec2, q physics.Rot, v physics.Vec2, spin float32, debris []int) error {
	par := &s.bodies[slot]
	pc := s.pieces[i]
	nb := body{
		key: s.nextKey, mat: par.mat, w: pc.x1 - pc.x0, h: pc.y1 - pc.y0,
		left: par.left, top: par.top, cx: par.cx + pc.x0, cy: par.cy + pc.y0,
		buoyancy: pieceBuoyancy(s.mats.Def(par.mat)),
	}
	n := nb.w * nb.h
	nb.px = make([]pixel, n)
	for j := pc.y0; j < pc.y1; j++ {
		for x := pc.x0; x < pc.x1; x++ {
			k := j*par.w + x
			if s.label[k] != int32(i) {
				continue
			}
			m := (j-pc.y0)*nb.w + x - pc.x0
			nb.px[m] = par.px[k]
			nb.n++
			par.px[k] = pixel{}
		}
	}
	par.n -= nb.n
	nb.n -= s.clean(&nb, uint16(slot), p, q)
	debris[nb.mat] += pc.size - nb.n
	if nb.n < minPiece || len(s.bodies) >= maxBodies {
		debris[nb.mat] += nb.n
		return nil
	}
	id, err := s.phys.AddDynamicBody(p, q)
	if err != nil {
		return fmt.Errorf("rigid: %w", err)
	}
	nb.id = id
	nb.cells = make([]cellAt, 0, nb.n+nb.w+nb.h+1)
	s.nextKey++
	s.bodies = append(s.bodies, nb)
	if err := s.reshape(&s.bodies[len(s.bodies)-1]); err != nil {
		return err
	}
	s.phys.SetVelocity(id, v, spin)
	return nil
}

// clean takes out of new piece b, in the frame at p, q, the pixels whose
// centers lie in cells outside the world or memory, of static terrain or
// of another body than the one at slot it broke from, and returns how many.
// Noita's clean_overlapping_pixels does the same to the pieces it sends
// off (C79); powders and liquids are left to the write, which pushes them
// away or stays out of them.
func (s *System) clean(b *body, slot uint16, p physics.Vec2, q physics.Rot) int {
	px, py := float32(p.X*cellsPerMeter), float32(p.Y*cellsPerMeter)
	removed := 0
	for j := range b.h {
		for i := range b.w {
			k := j*b.w + i
			if !b.px[k].ok {
				continue
			}
			u := b.left + float32(b.cx+i) + 0.5
			v := b.top + float32(b.cy+j) + 0.5
			x := int(math.Floor(float64(px + float32(q.C*u) - float32(q.S*v))))
			y := int(math.Floor(float64(py + float32(q.S*u) + float32(q.C*v))))
			c, ok := s.w.At(x, y)
			if ok {
				switch s.kinds[c.Mat] {
				case material.Static:
					ok = false
				case material.Solid:
					owner, _ := c.Body()
					ok = owner == slot
				case material.Empty, material.Powder, material.Liquid:
				}
			}
			if !ok {
				b.px[k] = pixel{}
				removed++
			}
		}
	}
	return removed
}

// crop cuts b's image down to piece keep, which holds all its pixels.
func (s *System) crop(b *body, keep int) {
	pc := s.pieces[keep]
	w, h := pc.x1-pc.x0, pc.y1-pc.y0
	if w == b.w && h == b.h {
		return
	}
	px := make([]pixel, w*h)
	for j := range h {
		from := (pc.y0+j)*b.w + pc.x0
		copy(px[j*w:(j+1)*w], b.px[from:from+w])
	}
	b.px = px
	b.w, b.h = w, h
	b.cx, b.cy = b.cx+pc.x0, b.cy+pc.y0
}

// reshape gives b's Box2D body the shapes of its image (shaper), or of its
// pixels' rectangles if Box2D can make none of those (C71), and reads back
// its mass. The next Write writes b again.
func (s *System) reshape(b *body) error {
	d := s.mats.Def(b.mat)
	surface := physics.Surface{Density: float32(d.Density) * densityScale, Friction: q8(d.Friction), Restitution: q8(d.Restitution)}
	s.shape.shape(b.px, b.w, b.h)
	made, err := s.setShapes(b, surface)
	if err != nil {
		return err
	}
	if made == 0 {
		s.shape.rectangles(b.px, b.w, b.h)
		if made, err = s.setShapes(b, surface); err != nil {
			return err
		}
		if made == 0 {
			return fmt.Errorf("rigid: Box2D made no shape of a body's %d pixels", b.n)
		}
	}
	b.mass, b.inertia, b.lc = s.phys.Mass(b.id), s.phys.Inertia(b.id), s.phys.LocalCenter(b.id)
	b.redraw = true
	return nil
}

// setShapes gives b's Box2D body the polygons in s.shape, which are in
// pixels of b's image, and returns how many Box2D made.
func (s *System) setShapes(b *body, surface physics.Surface) (int, error) {
	if len(s.shape.ends) == 0 {
		return 0, nil
	}
	ox, oy := float64(b.left)+float64(b.cx), float64(b.top)+float64(b.cy)
	s.polys.Points = s.polys.Points[:0]
	for _, pt := range s.shape.pts {
		s.polys.Points = append(s.polys.Points, physics.Vec2{
			X: float32((ox + pt.x) / cellsPerMeter), Y: float32((oy + pt.y) / cellsPerMeter),
		})
	}
	s.polys.Ends = append(s.polys.Ends[:0], s.shape.ends...)
	made, err := s.phys.SetShapes(b.id, s.polys, surface)
	if err != nil {
		return 0, fmt.Errorf("rigid: %w", err)
	}
	return made, nil
}

// compact removes the dead bodies, moving the bodies after each down a
// slot, in order, and the links of their cells with them.
func (s *System) compact() {
	j := 0
	for i := range s.bodies {
		if s.bodies[i].dead {
			continue
		}
		if i != j {
			s.bodies[j] = s.bodies[i]
			s.relink(&s.bodies[j], uint16(i), uint16(j))
		}
		j++
	}
	clear(s.bodies[j:])
	s.bodies = s.bodies[:j]
	s.w.Flush()
}

// relink moves the cells b holds from slot from to slot to.
func (s *System) relink(b *body, from, to uint16) {
	for _, at := range b.cells {
		x, y := int(at.x), int(at.y)
		c, ok := s.w.At(x, y)
		if !ok || s.kinds[c.Mat] != material.Solid {
			continue
		}
		if owner, k := c.Body(); owner == from {
			s.w.Put(x, y, world.BodyCell(c.Mat, c.Tone, c.Damage, to, k))
		}
	}
}
