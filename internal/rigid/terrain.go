package rigid

import (
	"fmt"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

const (
	// terrainFriction is the friction of every terrain edge. 🧪 Box2D's
	// default; Noita's terrain friction is not known.
	terrainFriction = 0.6
	// nearCells widens the cells a body may touch in the coming Step, to
	// make the terrain there collision first. 🧪
	nearCells = 4
	// A traced outline is simplified by dropping the corners that lie
	// within tolNum/tolDen cells of the edge that skips them. 🧪 0.75 makes
	// a 45° staircase one straight edge along its outer corners but keeps
	// a bump of one cell, one corner cut.
	tolNum = 3
	tolDen = 4
)

// chunk is the terrain collision of one chunk of the world: a static body
// of edge chains, or none when the chunk has no edge between solid and
// open cells.
type chunk struct {
	body physics.Body
	// built is set once cls holds the classes the body was made from:
	// those of the chunk's cells and one cell around it. version is the
	// highest world.ChunkVersion of the chunk and its neighbors then; the
	// classes can only have changed if it grew.
	built   bool
	version uint64
	cls     []world.Class
}

// Collide makes sure the terrain each body may touch in the coming Step is
// collision for Box2D, as the cells are now: it rebuilds the chunks near a
// body whose classes changed, in chunk order, and wakes the bodies near
// them, which may have slept on what is gone.
func (s *System) Collide() error {
	s.ticks++
	s.wanted = s.wanted[:0]
	for i := range s.bodies {
		s.want(&s.bodies[i])
	}
	slices.Sort(s.wanted)
	s.rebuilt = s.rebuilt[:0]
	for _, i := range s.wanted {
		if err := s.refresh(i); err != nil {
			return err
		}
	}
	if len(s.rebuilt) == 0 {
		return nil
	}
	for i := range s.bodies {
		b := &s.bodies[i]
		for _, c := range s.rebuilt {
			if b.near.overlaps(s.chunkCells(c)) {
				s.phys.Wake(b.id)
				break
			}
		}
	}
	return nil
}

// want lists the chunks b may touch in the coming Step: those it covers,
// and those it would cover moved as far again as in the last tick,
// widened by nearCells.
func (s *System) want(b *body) {
	if !b.written {
		return
	}
	r := b.box
	dx, dy := b.shiftX, b.shiftY
	r.x0 = min(r.x0, r.x0+int(math.Floor(float64(dx))))
	r.x1 = max(r.x1, r.x1+int(math.Ceil(float64(dx))))
	r.y0 = min(r.y0, r.y0+int(math.Floor(float64(dy))))
	r.y1 = max(r.y1, r.y1+int(math.Ceil(float64(dy))))
	r = rect{r.x0 - nearCells, r.y0 - nearCells, r.x1 + nearCells, r.y1 + nearCells}
	b.near = r
	x0, y0 := max(r.x0, 0), max(r.y0, 0)
	x1, y1 := min(r.x1, s.w.Width()), min(r.y1, s.w.Height())
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for cy := y0 / world.ChunkSize; cy <= (y1-1)/world.ChunkSize; cy++ {
		for cx := x0 / world.ChunkSize; cx <= (x1-1)/world.ChunkSize; cx++ {
			i := cy*s.cols + cx
			if s.stamp[i] != s.ticks {
				s.stamp[i] = s.ticks
				s.wanted = append(s.wanted, i)
			}
		}
	}
}

// chunkCells returns the cells of chunk i.
func (s *System) chunkCells(i int) rect {
	x0, y0 := (i%s.cols)*world.ChunkSize, (i/s.cols)*world.ChunkSize
	return rect{x0, y0, min(x0+world.ChunkSize, s.w.Width()), min(y0+world.ChunkSize, s.w.Height())}
}

// version returns the highest world.ChunkVersion of chunk i and the chunks
// around it. A change marked in one of them can change the classes of the
// cells around chunk i too, which decide where its edges end.
func (s *System) version(i int) uint64 {
	cx, cy := i%s.cols, i/s.cols
	rows := len(s.terrain) / s.cols
	v := uint64(0)
	for y := max(cy-1, 0); y <= min(cy+1, rows-1); y++ {
		for x := max(cx-1, 0); x <= min(cx+1, s.cols-1); x++ {
			v = max(v, s.w.ChunkVersion(x, y))
		}
	}
	return v
}

// refresh rebuilds the collision of chunk i if the classes of its cells
// and those around it changed since it was built.
func (s *System) refresh(i int) error {
	ch := &s.terrain[i]
	v := s.version(i)
	if ch.built && ch.version == v {
		return nil
	}
	c := s.chunkCells(i)
	w, h := c.x1-c.x0, c.y1-c.y0
	n := (w + 2) * (h + 2)
	t := &s.trace
	t.cls = slices.Grow(t.cls[:0], n)[:n]
	s.w.Classify(c.x0-1, c.y0-1, c.x1+1, c.y1+1, t.cls)
	ch.version = v
	if ch.built && slices.Equal(ch.cls, t.cls) {
		return nil
	}
	ch.cls = append(ch.cls[:0], t.cls...)
	ch.built = true
	if ch.body != 0 {
		s.phys.DestroyBody(ch.body)
		ch.body = 0
	}
	s.rebuilt = append(s.rebuilt, i)
	chains := t.outline(c.x0, c.y0, w, h)
	if len(chains) == 0 {
		return nil
	}
	b, err := s.phys.AddStaticChains(chains, terrainFriction)
	if err != nil {
		return fmt.Errorf("rigid: chunk (%d, %d): %w", i%s.cols, i/s.cols, err)
	}
	ch.body = b
	return nil
}

// The four directions an edge of the cell lattice runs in, in clockwise
// order on screen (y points down): east, south, west, north.
const (
	east = iota
	south
	west
	north
)

var (
	dirX = [4]int{1, 0, -1, 0}
	dirY = [4]int{0, 1, 0, -1}
)

func right(d int) int { return (d + 1) & 3 }
func left(d int) int  { return (d + 3) & 3 }

// half returns the offset, -1 or 0, from a lattice point to the cell whose
// center lies half a cell from it in the direction whose component is a,
// ±1.
func half(a int) int {
	if a > 0 {
		return 0
	}
	return -1
}

// vertex is a point of the cell lattice, in the chunk's own coordinates.
type vertex struct{ x, y int }

// span is one chain's points in tracer.pts.
type span struct {
	start, end    int
	loop          bool
	before, after physics.Vec2
}

// tracer finds the edges between solid and open cells of a chunk and joins
// them into chains. An edge runs with the solid cell on its right on
// screen, which is the side Box2D's one-sided chains collide on. A chunk
// owns the edges of its own solid cells; at its border an outline goes on
// in the next chunk, so it ends there in an open chain whose ghost points
// are where it goes on. An outline wholly inside the chunk is a loop.
type tracer struct {
	// cls holds the classes of the chunk's cells and one cell around them,
	// row by row; w and h are the chunk's size.
	cls  []world.Class
	w, h int
	// edges holds, for each lattice point of the chunk, a bit for each
	// direction an owned edge starts in from there; pred marks those with
	// an owned edge before them, and seen those traced.
	edges, pred, seen []uint8
	verts             []vertex
	keep              []bool
	pts               []physics.Vec2
	spans             []span
	chains            []physics.Chain
}

// solid reports whether cell (i, j) of the chunk, -1..w and -1..h, is one
// bodies collide with.
func (t *tracer) solid(i, j int) bool {
	return t.cls[(j+1)*(t.w+2)+i+1] != world.Open
}

// rightCell and leftCell return the cells on the right and the left on
// screen of the edge that starts at v in direction d.
func (t *tracer) rightCell(v vertex, d int) (int, int) {
	r := right(d)
	return v.x + half(dirX[d]+dirX[r]), v.y + half(dirY[d]+dirY[r])
}

func (t *tracer) leftCell(v vertex, d int) (int, int) {
	r := right(d)
	return v.x + half(dirX[d]-dirX[r]), v.y + half(dirY[d]-dirY[r])
}

// exists reports whether an edge starts at v in direction d: whether it
// has solid on its right and open on its left.
func (t *tracer) exists(v vertex, d int) bool {
	ri, rj := t.rightCell(v, d)
	li, lj := t.leftCell(v, d)
	return t.solid(ri, rj) && !t.solid(li, lj)
}

// owned reports whether the edge from v in direction d belongs to this
// chunk: whether its solid cell does.
func (t *tracer) owned(v vertex, d int) bool {
	i, j := t.rightCell(v, d)
	return i >= 0 && j >= 0 && i < t.w && j < t.h && t.exists(v, d)
}

// next returns the direction of the edge after one that arrives at v in
// direction d. It turns right, around the same solid cell, when the cell
// ahead on the right is open; else it goes straight when the cell ahead
// on the left is open; else it turns left. Turning right first keeps two
// solid cells that touch only at a corner apart.
func (t *tracer) next(v vertex, d int) int {
	if i, j := t.rightCell(v, d); !t.solid(i, j) {
		return right(d)
	}
	if i, j := t.leftCell(v, d); !t.solid(i, j) {
		return d
	}
	return left(d)
}

// prev returns the direction of the edge that arrives at v and goes on
// in direction d.
func (t *tracer) prev(v vertex, d int) int {
	for _, p := range [3]int{left(d), d, right(d)} {
		from := vertex{v.x - dirX[p], v.y - dirY[p]}
		if t.exists(from, p) && t.next(v, p) == d {
			return p
		}
	}
	panic(fmt.Sprintf("rigid: no edge arrives at %v before one going %d", v, d))
}

func (t *tracer) index(v vertex) int { return v.y*(t.w+1) + v.x }

// outline returns the chains of the chunk whose top-left cell is (x0, y0),
// w × h cells, from the classes in t.cls. The chains share t's buffers and
// last until the next call.
func (t *tracer) outline(x0, y0, w, h int) []physics.Chain {
	t.w, t.h = w, h
	n := (w + 1) * (h + 1)
	t.edges = clear0(t.edges, n)
	t.pred = clear0(t.pred, n)
	t.seen = clear0(t.seen, n)
	t.pts, t.spans = t.pts[:0], t.spans[:0]
	t.findEdges()
	t.eachEdge(func(v vertex, d int) {
		end := vertex{v.x + dirX[d], v.y + dirY[d]}
		if e := t.next(end, d); t.owned(end, e) {
			t.pred[t.index(end)] |= 1 << e
		}
	})
	// Chains that leave the chunk start where no owned edge leads in.
	t.eachEdge(func(v vertex, d int) {
		if t.pred[t.index(v)]&(1<<d) == 0 && t.seen[t.index(v)]&(1<<d) == 0 {
			t.trace(x0, y0, v, d)
		}
	})
	// What is left are loops.
	t.eachEdge(func(v vertex, d int) {
		if t.seen[t.index(v)]&(1<<d) == 0 {
			t.trace(x0, y0, v, d)
		}
	})
	t.chains = t.chains[:0]
	for _, sp := range t.spans {
		t.chains = append(t.chains, physics.Chain{
			Points: t.pts[sp.start:sp.end], Loop: sp.loop, Before: sp.before, After: sp.after,
		})
	}
	return t.chains
}

// findEdges marks in t.edges every edge of the chunk's solid cells that
// has an open cell on its other side, starting from the lattice point it
// starts at.
func (t *tracer) findEdges() {
	for j := range t.h {
		for i := range t.w {
			if !t.solid(i, j) {
				continue
			}
			if !t.solid(i, j-1) {
				t.edges[t.index(vertex{i, j})] |= 1 << east
			}
			if !t.solid(i+1, j) {
				t.edges[t.index(vertex{i + 1, j})] |= 1 << south
			}
			if !t.solid(i, j+1) {
				t.edges[t.index(vertex{i + 1, j + 1})] |= 1 << west
			}
			if !t.solid(i-1, j) {
				t.edges[t.index(vertex{i, j + 1})] |= 1 << north
			}
		}
	}
}

// eachEdge calls f with each owned edge, in lattice order.
func (t *tracer) eachEdge(f func(v vertex, d int)) {
	for k, bits := range t.edges {
		if bits == 0 {
			continue
		}
		v := vertex{k % (t.w + 1), k / (t.w + 1)}
		for d := range 4 {
			if bits&(1<<d) != 0 {
				f(v, d)
			}
		}
	}
}

// trace follows the outline from the owned edge at v in direction d
// through the owned edges after it. It ends back at that edge, a loop, or
// at the chunk's border, an open chain whose ghost points are the lattice
// points just past its ends. It simplifies the outline and adds it to
// t.pts.
func (t *tracer) trace(x0, y0 int, v vertex, d int) {
	start, first := v, d
	t.verts = append(t.verts[:0], v)
	for {
		t.seen[t.index(v)] |= 1 << d
		v = vertex{v.x + dirX[d], v.y + dirY[d]}
		e := t.next(v, d)
		if v == start && e == first {
			t.compact()
			t.add(x0, y0, span{loop: true})
			return
		}
		t.verts = append(t.verts, v)
		if !t.owned(v, e) || t.seen[t.index(v)]&(1<<e) != 0 {
			p := t.prev(start, first)
			sp := span{
				before: meters(x0, y0, vertex{start.x - dirX[p], start.y - dirY[p]}),
				after:  meters(x0, y0, vertex{v.x + dirX[e], v.y + dirY[e]}),
			}
			t.compact()
			t.add(x0, y0, sp)
			return
		}
		d = e
	}
}

// compact drops the points of t.verts that lie straight between their
// neighbors.
func (t *tracer) compact() {
	out := t.verts[:1]
	for k := 1; k < len(t.verts); k++ {
		if k+1 < len(t.verts) {
			a, b, c := out[len(out)-1], t.verts[k], t.verts[k+1]
			ux, uy, wx, wy := b.x-a.x, b.y-a.y, c.x-b.x, c.y-b.y
			if ux*wy == uy*wx && ux*wx+uy*wy > 0 {
				continue
			}
		}
		out = append(out, t.verts[k])
	}
	t.verts = out
}

// add simplifies t.verts and appends them to t.pts as the chain sp.
func (t *tracer) add(x0, y0 int, sp span) {
	n := len(t.verts)
	t.keep = clearBool(t.keep, n+1)
	if sp.loop {
		// Split the loop at the point farthest from its first, so each
		// half has two ends to keep.
		far := 0
		for k := range n {
			if dist2(t.verts[0], t.verts[k]) > dist2(t.verts[0], t.verts[far]) {
				far = k
			}
		}
		t.verts = append(t.verts, t.verts[0])
		t.keep[0], t.keep[far] = true, true
		t.simplify(0, far)
		t.simplify(far, n)
		t.verts = t.verts[:n]
		kept := 0
		for k := range n {
			if t.keep[k] {
				kept++
			}
		}
		if kept < 3 {
			// Too small to simplify: keep every corner.
			for k := range n {
				t.keep[k] = true
			}
		}
	} else {
		// An open chain keeps the corners next to its ends, so its first
		// and last edges run along the lattice, the way the next chunk's
		// ghost points run, however each chunk simplified the rest.
		first, last := min(1, n-1), max(n-2, 0)
		t.keep[0], t.keep[first], t.keep[last], t.keep[n-1] = true, true, true, true
		t.simplify(first, last)
	}
	sp.start = len(t.pts)
	for k := range n {
		if t.keep[k] {
			t.pts = append(t.pts, meters(x0, y0, t.verts[k]))
		}
	}
	sp.end = len(t.pts)
	t.spans = append(t.spans, sp)
}

// simplify keeps, between t.verts[a] and t.verts[b], the corner farthest
// from the segment between them if it is more than the tolerance away, and
// then does the same on each side of it (Douglas–Peucker). It measures to
// the segment, not the line through it, so an outline that turns back on
// itself keeps its turn. The arithmetic is on whole cells, so it is the
// same everywhere.
func (t *tracer) simplify(a, b int) {
	if b-a < 2 {
		return
	}
	p, q := t.verts[a], t.verts[b]
	lx, ly := int64(q.x-p.x), int64(q.y-p.y)
	ll := max(lx*lx+ly*ly, 1)
	far, farDist := -1, int64(-1)
	for k := a + 1; k < b; k++ {
		if d := segDist2(t.verts[k], p, lx, ly, ll); d > farDist {
			far, farDist = k, d
		}
	}
	// farDist is the squared distance times ll; compare it scaled.
	if farDist*tolDen*tolDen <= tolNum*tolNum*ll {
		return
	}
	t.keep[far] = true
	t.simplify(a, far)
	t.simplify(far, b)
}

// segDist2 returns the squared distance from v to the segment from p along
// (lx, ly), times ll, its squared length (1 for a segment of no length).
func segDist2(v, p vertex, lx, ly, ll int64) int64 {
	ex, ey := int64(v.x-p.x), int64(v.y-p.y)
	switch dot := ex*lx + ey*ly; {
	case dot <= 0:
		return (ex*ex + ey*ey) * ll
	case dot >= ll:
		fx, fy := ex-lx, ey-ly
		return (fx*fx + fy*fy) * ll
	}
	c := lx*ey - ly*ex
	return c * c
}

func dist2(a, b vertex) int {
	dx, dy := a.x-b.x, a.y-b.y
	return dx*dx + dy*dy
}

// meters returns lattice point v of the chunk whose top-left cell is
// (x0, y0) in meters.
func meters(x0, y0 int, v vertex) physics.Vec2 {
	return physics.Vec2{X: float32(x0+v.x) / cellsPerMeter, Y: float32(y0+v.y) / cellsPerMeter}
}

// clear0 returns s resized to n entries, all zero, reusing its memory.
func clear0(s []uint8, n int) []uint8 {
	s = slices.Grow(s[:0], n)[:n]
	clear(s)
	return s
}

func clearBool(s []bool, n int) []bool {
	s = slices.Grow(s[:0], n)[:n]
	clear(s)
	return s
}
