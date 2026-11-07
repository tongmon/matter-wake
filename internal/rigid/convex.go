package rigid

import (
	"cmp"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

// maxCorners is the most corners a polygon of a body's shapes has: Box2D's
// limit. 🔶 Noita merges up to 6 (C92).
const maxCorners = physics.MaxPolygonVertices

// point is a point of a body's image, in pixels from its top-left corner.
type point struct{ x, y float64 }

// sedge is an edge of an outline that is not level, from its top end
// (x0, y0) to its bottom end (x1, y1), in pixels. An outline runs with its
// pixels on its right, so wind is +1 where it runs up the edge, which has
// pixels east of it, and -1 where it runs down. The sum of the winds of the
// edges west of a point is above 0 inside the shape.
type sedge struct {
	x0, y0, x1, y1 int
	wind           int
}

// slope compares the slopes, in x per y, of edges a and b: below 0 when
// a's is less.
func slope(a, b *sedge) int {
	return cmp.Compare(int64(a.x1-a.x0)*int64(b.y1-b.y0), int64(b.x1-b.x0)*int64(a.y1-a.y0))
}

// xAt returns the x of e at height y, between its ends. At a whole y it is
// exact up to one rounding, so edges that meet at a lattice point agree on
// it; elsewhere products are converted one by one so they do not fuse.
func (e *sedge) xAt(y float64) float64 {
	dx, dy := int64(e.x1-e.x0), int64(e.y1-e.y0)
	if f := math.Floor(y); f == y {
		return float64(int64(e.x0)*dy+(int64(f)-int64(e.y0))*dx) / float64(dy)
	}
	return float64(e.x0) + float64(float64(y-float64(e.y0))*float64(dx))/float64(dy)
}

// part is a convex polygon the shaper builds down the slabs, one trapezoid
// at a time: its corners down its left and its right side, top first, and
// the edges its last trapezoid lay between.
type part struct {
	left, right [maxCorners]point
	nl, nr      int
	l, r        int
}

// corners returns how many corners p has: a top or bottom that is a point
// counts once.
func (p *part) corners() int {
	n := p.nl + p.nr
	if p.left[0] == p.right[0] {
		n--
	}
	if p.left[p.nl-1] == p.right[p.nr-1] {
		n--
	}
	return n
}

// shaper turns the image of a body into convex polygons for Box2D. It
// traces the outlines of the image's pixels and simplifies them to bodyTol
// as Noita does (C68, C69), then cuts the shape inside into trapezoids
// along level lines through every corner and every crossing of two edges,
// and joins the trapezoids down into convex polygons of up to maxCorners
// corners. Which side of the outlines is inside comes from their winding,
// so holes stay out (C34), and the crossings simplification can make
// between outlines still give a shape. This is our own decomposition:
// Noita triangulates the outlines and merges the triangles (C92 to C108).
// The polygons are in pts, each ending where ends says, in pixels.
type shaper struct {
	tr      tracer
	edges   []sedge
	ys      []float64
	active  []int
	prev    []part
	cur     []part
	matched []bool
	pts     []point
	ends    []int
	used    []bool
}

// shape fills sh with the convex polygons of the image of w × h pixels
// whose pixels has marks.
func (sh *shaper) shape(has []bool, w, h int) {
	sh.pts, sh.ends = sh.pts[:0], sh.ends[:0]
	sh.outline(has, w, h)
	sh.events()
	sh.prev = sh.prev[:0]
	for s := 0; s+1 < len(sh.ys); s++ {
		sh.slab(sh.ys[s], sh.ys[s+1])
	}
	for i := range sh.prev {
		sh.emit(&sh.prev[i])
	}
	sh.prev = sh.prev[:0]
}

// outline traces the image's outlines and lists their edges that are not
// level in sh.edges, in the order of their tops.
func (sh *shaper) outline(has []bool, w, h int) {
	t := &sh.tr
	t.body = true
	n := (w + 2) * (h + 2)
	t.cls = slices.Grow(t.cls[:0], n)[:n]
	for k := range t.cls {
		i, j := k%(w+2)-1, k/(w+2)-1
		t.cls[k] = world.Open
		if i >= 0 && j >= 0 && i < w && j < h && has[j*w+i] {
			t.cls[k] = world.Fixed
		}
	}
	t.outline(0, 0, w, h)
	sh.edges = sh.edges[:0]
	for _, sp := range t.spans {
		loop := t.corners[sp.start:sp.end]
		for k := range loop {
			a, b := loop[k], loop[(k+1)%len(loop)]
			switch {
			case a.y > b.y:
				sh.edges = append(sh.edges, sedge{b.x, b.y, a.x, a.y, 1})
			case a.y < b.y:
				sh.edges = append(sh.edges, sedge{a.x, a.y, b.x, b.y, -1})
			}
		}
	}
	slices.SortStableFunc(sh.edges, func(a, b sedge) int { return cmp.Compare(a.y0, b.y0) })
}

// events lists in sh.ys, in order, every height where a slab begins or
// ends: the ends of the edges and where two edges cross.
func (sh *shaper) events() {
	sh.ys = sh.ys[:0]
	for i := range sh.edges {
		a := &sh.edges[i]
		sh.ys = append(sh.ys, float64(a.y0), float64(a.y1))
		for j := i + 1; j < len(sh.edges) && sh.edges[j].y0 < a.y1; j++ {
			if y, ok := crossing(a, &sh.edges[j]); ok {
				sh.ys = append(sh.ys, y)
			}
		}
	}
	slices.Sort(sh.ys)
	sh.ys = slices.Compact(sh.ys)
}

// crossing returns the height where edges a and b cross, if they do
// between the heights both span, not at their ends. b starts no higher
// than a. The test is exact, on whole numbers.
func crossing(a, b *sedge) (float64, bool) {
	lo, hi := int64(b.y0), int64(min(a.y1, b.y1))
	if lo >= hi {
		return 0, false
	}
	dxa, dya := int64(a.x1-a.x0), int64(a.y1-a.y0)
	dxb, dyb := int64(b.x1-b.x0), int64(b.y1-b.y0)
	// a's x minus b's, times dya·dyb, is c0 + c1·y.
	c0 := (int64(a.x0)*dya-int64(a.y0)*dxa)*dyb - (int64(b.x0)*dyb-int64(b.y0)*dxb)*dya
	c1 := dxa*dyb - dxb*dya
	if s0, s1 := cmp.Compare(c0+c1*lo, 0), cmp.Compare(c0+c1*hi, 0); s0*s1 >= 0 {
		return 0, false
	}
	return float64(-c0) / float64(c1), true
}

// slab cuts the shape between heights ya and yb into trapezoids, from
// west to east, and joins each to the polygon above it if that stays
// convex and small enough.
func (sh *shaper) slab(ya, yb float64) {
	ym := ya + (yb-ya)/2
	sh.active = sh.active[:0]
	for i := range sh.edges {
		e := &sh.edges[i]
		if float64(e.y0) > ya {
			break
		}
		if float64(e.y1) >= yb {
			sh.active = append(sh.active, i)
		}
	}
	// No two edges cross inside the slab, so their order in its middle is
	// their order all across it.
	slices.SortFunc(sh.active, func(a, b int) int {
		ea, eb := &sh.edges[a], &sh.edges[b]
		if c := cmp.Compare(ea.xAt(ym), eb.xAt(ym)); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	sh.cur = sh.cur[:0]
	sh.matched = slices.Grow(sh.matched[:0], len(sh.prev))[:len(sh.prev)]
	clear(sh.matched)
	wind, from := 0, -1
	for _, i := range sh.active {
		before := wind
		wind += sh.edges[i].wind
		switch {
		case before <= 0 && wind > 0:
			from = i
		case before > 0 && wind <= 0:
			sh.trapezoid(from, i, ya, yb)
		}
	}
	for i := range sh.prev {
		if !sh.matched[i] {
			sh.emit(&sh.prev[i])
		}
	}
	sh.prev, sh.cur = sh.cur, sh.prev
}

// trapezoid adds the trapezoid between edges l and r from height ya to yb:
// to the polygon above it, or as a new one.
func (sh *shaper) trapezoid(l, r int, ya, yb float64) {
	el, er := &sh.edges[l], &sh.edges[r]
	tl, tr := point{el.xAt(ya), ya}, point{er.xAt(ya), ya}
	bl, br := point{el.xAt(yb), yb}, point{er.xAt(yb), yb}
	if tl.x >= tr.x && bl.x >= br.x {
		return // edges that run together: nothing between them
	}
	for k := range sh.prev {
		p := &sh.prev[k]
		if sh.matched[k] || tl.x >= tr.x || p.left[p.nl-1] != tl || p.right[p.nr-1] != tr {
			continue
		}
		sh.matched[k] = true
		if q, ok := sh.join(p, l, r, bl, br); ok {
			sh.cur = append(sh.cur, q)
			return
		}
		sh.emit(p)
		break
	}
	sh.cur = append(sh.cur, part{left: [maxCorners]point{tl, bl}, right: [maxCorners]point{tr, br}, nl: 2, nr: 2, l: l, r: r})
}

// join returns p with the trapezoid under it between edges l and r, whose
// bottom corners are bl and br, if that is convex and has at most
// maxCorners corners. A side that goes on along the same line keeps its
// corners; one that turns adds one.
func (sh *shaper) join(p *part, l, r int, bl, br point) (part, bool) {
	q := *p
	switch c := slope(&sh.edges[l], &sh.edges[p.l]); {
	case l == p.l || c == 0:
		q.left[q.nl-1] = bl
	case c > 0 && q.nl < maxCorners:
		q.left[q.nl] = bl
		q.nl++
	default:
		return q, false
	}
	switch c := slope(&sh.edges[r], &sh.edges[p.r]); {
	case r == p.r || c == 0:
		q.right[q.nr-1] = br
	case c < 0 && q.nr < maxCorners:
		q.right[q.nr] = br
		q.nr++
	default:
		return q, false
	}
	q.l, q.r = l, r
	return q, q.corners() <= maxCorners
}

// emit adds p to the polygons: down its left side and up its right.
func (sh *shaper) emit(p *part) {
	start := len(sh.pts)
	sh.pts = append(sh.pts, p.left[:p.nl]...)
	for k := p.nr - 1; k >= 0; k-- {
		c := p.right[k]
		if (k == p.nr-1 && c == p.left[p.nl-1]) || (k == 0 && c == p.left[0]) {
			continue
		}
		sh.pts = append(sh.pts, c)
	}
	if len(sh.pts)-start < 3 {
		sh.pts = sh.pts[:start]
		return
	}
	sh.ends = append(sh.ends, len(sh.pts))
}

// rectangles fills sh with rectangles that cover the image's pixels
// exactly, for when Box2D can make nothing of its shapes (C71): each runs
// along a row as far as there are pixels and down as far as the rows below
// have the same run.
func (sh *shaper) rectangles(has []bool, w, h int) {
	sh.pts, sh.ends = sh.pts[:0], sh.ends[:0]
	sh.used = slices.Grow(sh.used[:0], len(has))[:len(has)]
	clear(sh.used)
	free := func(i, j int) bool { return has[j*w+i] && !sh.used[j*w+i] }
	for j := range h {
		for i := 0; i < w; i++ {
			if !free(i, j) {
				continue
			}
			i1 := i + 1
			for i1 < w && free(i1, j) {
				i1++
			}
			j1 := j + 1
			for ; j1 < h; j1++ {
				run := true
				for x := i; x < i1 && run; x++ {
					run = free(x, j1)
				}
				if !run {
					break
				}
			}
			for y := j; y < j1; y++ {
				for x := i; x < i1; x++ {
					sh.used[y*w+x] = true
				}
			}
			x0, y0, x1, y1 := float64(i), float64(j), float64(i1), float64(j1)
			sh.pts = append(sh.pts, point{x0, y0}, point{x1, y0}, point{x1, y1}, point{x0, y1})
			sh.ends = append(sh.ends, len(sh.pts))
			i = i1 - 1
		}
	}
}
