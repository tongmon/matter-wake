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
// deletes single pixels (C77, C78, C135). Here the pieces left out go to
// the inventory of whoever broke the body, with what was dug, so no cell
// is lost.
const minPiece = 2

// piece is a 4-connected piece of a body's image: how many pixels it has
// and the pixels around it, [x0, x1) × [y0, y1).
type piece struct {
	size           int
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
	if int(k) >= len(b.px) || !b.px[k].ok() {
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
		s.showDamage(b, slot, k, false)
		return 0, false
	}
	b.px[k] = pixel{}
	b.n--
	b.dirty, s.broken = true, true
	s.w.Put(x, y, world.Cell{})
	s.w.Flush()
	return b.mat, true
}

// showDamage puts the damage of pixel k of b, at slot, into every cell
// that shows the pixel, or empties them if hide is set. A turned body's
// pixel may show in two cells, and a body that does not move is not
// written again. The cells that show it have their centers inside the
// pixel, where b was last written: within a cell of the cell its center is
// in.
func (s *System) showDamage(b *body, slot, k uint16, hide bool) {
	if !b.written {
		return
	}
	cx, cy := pixelCell(b, int(k)%b.w, int(k)/b.w, b.wp, b.wq)
	for y := cy - 1; y <= cy+1; y++ {
		for x := cx - 1; x <= cx+1; x++ {
			c, ok := s.w.At(x, y)
			if !ok || s.kinds[c.Mat] != material.Solid {
				continue
			}
			if owner, pixel := c.Body(); owner == slot && pixel == k {
				c.Damage = b.px[k].shade()
				if hide {
					c = world.Cell{}
				}
				s.w.Put(x, y, c)
			}
		}
	}
	s.w.Flush()
}

// Break splits every body that lost pixels since the last Break, in key
// order, into its 4-connected pieces; pixels that touch only at a corner
// are apart (C77). One piece keeps the body: its slot, key and Box2D body,
// which moves on as before. For a dynamic body it is the largest; a static
// body stays static in the piece its material's anchor picks (keeps,
// C136). The other pieces become new dynamic bodies, with keys in the
// order their first pixels come in the image's rows, in the body's frame
// and with its velocity, awake (C35, C78). Each body that changed gets
// shapes made from its pixels, and the next Write writes it. Pieces
// smaller than minPiece, and the pixels of a new piece whose cells static
// terrain or another body holds (C79), are no body's: Break adds them to
// debris, by material. A body of a material that shatters turns whole
// into its powder instead (shatter). A body left with no piece is removed,
// and the bodies after it move down a slot. Box2D wakes the bodies that
// touched shapes a body lost, static or not.
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
	if powder := s.mats.ShatterOf(b.mat); powder != material.Air {
		return true, s.shatter(slot, powder, debris)
	}
	s.find(b)
	keep := -1
	for i := range s.pieces {
		switch {
		case s.pieces[i].size < minPiece:
			s.drop(b, i, debris)
		case keep < 0 || s.keeps(b, i, keep):
			keep = i
		}
	}
	if keep < 0 {
		s.remove(slot)
		return true, nil
	}
	if len(s.pieces) > 1 {
		v, spin := s.motion(b)
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

// keeps reports whether piece i, rather than piece keep, found before it,
// keeps body b. A dynamic body keeps its largest piece. A static body
// keeps the one its material's anchor picks, as Noita's static bodies do
// (C136): the piece that reaches highest, so what hangs below a cut falls,
// as ice does; the one that reaches lowest, so what stands on a cut falls
// over, as wood does; or the largest. A static body never turns, so the
// image's rows are the world's. Of pieces alike the first found keeps it.
func (s *System) keeps(b *body, i, keep int) bool {
	p, k := &s.pieces[i], &s.pieces[keep]
	if b.static {
		switch s.mats.Def(b.mat).Anchor {
		case material.AnchorTop:
			return p.y0 < k.y0
		case material.AnchorBottom:
			return p.y1 > k.y1
		case material.NoAnchor, material.AnchorLargest:
		}
	}
	return p.size > k.size
}

// remove erases the body at slot from the grid and from Box2D and marks it
// dead, for compact to take out.
func (s *System) remove(slot int) {
	b := &s.bodies[slot]
	s.erase(b, uint16(slot))
	if b.written {
		s.gone = append(s.gone, b.box)
	}
	s.phys.DestroyBody(b.id)
	b.dead = true
}

// shatter turns the body at slot, which lost a pixel, into cells of
// powder, one for each pixel it has left, as Noita's glass turns whole into
// glass_broken (C136). Noita makes as many as the pixels it had; the
// developer chose those it has (2026-11-10), so no cell is made or lost.
// Powder that has nowhere in the world or memory to go goes to debris.
func (s *System) shatter(slot int, powder material.ID, debris []int) error {
	return s.dissolve(slot, powder, debris)
}

// dissolve turns the body at slot into cells of material into, one for
// each pixel it has left, and removes it. Each pixel's cell is the one its
// center falls in where the body was last written, and keeps the pixel's
// tone. Where another cell holds that cell, the new cell goes into the
// world's queue, to go back as a cell pushed out does; outside the world or
// memory it is counted in unplaced, by material. It counts what it turned
// (Turned).
func (s *System) dissolve(slot int, into material.ID, unplaced []int) error {
	b := &s.bodies[slot]
	p, q := b.wp, b.wq
	if !b.written {
		p, q = s.phys.Transform(b.id)
	}
	s.erase(b, uint16(slot))
	s.spill = s.spill[:0]
	for j := range b.h {
		for i := range b.w {
			px := b.px[j*b.w+i]
			if !px.ok() {
				continue
			}
			x, y := pixelCell(b, i, j, p, q)
			cell := world.Cell{Mat: into, Tone: px.tone}
			switch c, ok := s.w.At(x, y); {
			case !ok:
				unplaced[into]++
			case s.kinds[c.Mat] == material.Empty:
				s.w.Put(x, y, cell)
			default:
				s.spill = append(s.spill, world.Queued{Cell: cell, X: int32(x), Y: int32(y)})
			}
		}
	}
	s.w.Flush()
	s.turned[b.mat] -= b.n
	s.turned[into] += b.n
	if b.written {
		s.gone = append(s.gone, b.box)
	}
	s.phys.DestroyBody(b.id)
	b.dead = true
	if err := s.w.Requeue(s.spill); err != nil {
		return fmt.Errorf("rigid: %w", err)
	}
	return nil
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
		if !px.ok() || s.label[k] >= 0 {
			continue
		}
		id := int32(len(s.pieces))
		p := piece{x0: b.w, y0: b.h}
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
				if m := nb[1]*b.w + nb[0]; b.px[m].ok() && s.label[m] < 0 {
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
		buoyancy: s.pieceBuoyancy(par.mat),
		// Where its body was last written, which its pixels' cells show
		// (redrawInPlace).
		p: par.wp, q: par.wq, inherit: par.written,
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
	removed := 0
	for j := range b.h {
		for i := range b.w {
			k := j*b.w + i
			if !b.px[k].ok() {
				continue
			}
			x, y := pixelCell(b, i, j, p, q)
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
	surface := physics.Surface{
		Density: float32(d.Density) * densityScale, Friction: q8(d.Friction), Restitution: q8(d.Restitution), Hits: d.Cracks,
	}
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
	lc := b.lc
	b.mass, b.inertia, b.lc = s.phys.Mass(b.id), s.phys.Inertia(b.id), s.phys.LocalCenter(b.id)
	if b.frozen {
		// Box2D moves the velocity of the center of mass with it, v + ω ×
		// Δc, only for a body in the simulation; a frozen one keeps its
		// velocity here.
		_, q := s.phys.Transform(b.id)
		dx, dy := b.lc.X-lc.X, b.lc.Y-lc.Y
		wx, wy := float32(q.C*dx)-float32(q.S*dy), float32(q.S*dx)+float32(q.C*dy)
		b.fv = physics.Vec2{X: b.fv.X - float32(b.fspin*wy), Y: b.fv.Y + float32(b.fspin*wx)}
	}
	s.covered(b)
	b.redraw = true
	return nil
}

// covered marks in b.cover the pixels b's shapes, in s.shape, cover: the
// pixels b has, and the pixels gone that a shape overlaps. Simplifying the
// outline can let a shape reach up to bodyTol past the pixels; the
// terrain stays out of what the shapes cover (reaches).
func (s *System) covered(b *body) {
	n := b.w * b.h
	b.cover = slices.Grow(b.cover[:0], n)[:n]
	for k := range b.px {
		b.cover[k] = b.px[k].ok()
	}
	start := 0
	for _, end := range s.shape.ends {
		p := s.shape.pts[start:end]
		start = end
		x0, y0, x1, y1 := p[0].x, p[0].y, p[0].x, p[0].y
		for _, q := range p {
			x0, y0, x1, y1 = min(x0, q.x), min(y0, q.y), max(x1, q.x), max(y1, q.y)
		}
		for j := max(int(math.Floor(y0)), 0); j < min(int(math.Ceil(y1)), b.h); j++ {
			for i := max(int(math.Floor(x0)), 0); i < min(int(math.Ceil(x1)), b.w); i++ {
				if k := j*b.w + i; !b.cover[k] && overlapsPixel(p, i, j) {
					b.cover[k] = true
				}
			}
		}
	}
}

// overlapsPixel reports whether convex polygon p, in pixels, overlaps
// pixel (i, j) by some area: no side of p has all the pixel's corners on
// or outside it. p's bounding box overlaps the pixel's already.
func overlapsPixel(p []point, i, j int) bool {
	turn := 0.0
	for k := range p {
		a, b := p[k], p[(k+1)%len(p)]
		turn += float64(a.x*b.y) - float64(b.x*a.y)
	}
	corners := [4]point{{float64(i), float64(j)}, {float64(i + 1), float64(j)}, {float64(i + 1), float64(j + 1)}, {float64(i), float64(j + 1)}}
	for k := range p {
		a, b := p[k], p[(k+1)%len(p)]
		inside := false
		for _, c := range corners {
			if side := float64((b.x-a.x)*(c.y-a.y)) - float64((b.y-a.y)*(c.x-a.x)); side*turn > 0 {
				inside = true
				break
			}
		}
		if !inside {
			return false
		}
	}
	return true
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
