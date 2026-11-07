package rigid

import (
	"math"
	"testing"

	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// outlineOf traces a chunk of w × h cells from rows, which draw the chunk
// and one cell around it: # fixed, s settled, anything else open. It
// returns the chains with their points in cells.
func outlineOf(t *testing.T, rows []string) ([]physics.Chain, *tracer) {
	t.Helper()
	h, w := len(rows)-2, len(rows[0])-2
	tr := &tracer{}
	for _, r := range rows {
		if len(r) != w+2 {
			t.Fatalf("row %q is not %d wide", r, w+2)
		}
		for _, ch := range r {
			switch ch {
			case '#':
				tr.cls = append(tr.cls, world.Fixed)
			case 's':
				tr.cls = append(tr.cls, world.Settled)
			default:
				tr.cls = append(tr.cls, world.Open)
			}
		}
	}
	chains := tr.outline(0, 0, w, h)
	out := make([]physics.Chain, len(chains))
	for i, c := range chains {
		out[i] = physics.Chain{Loop: c.Loop, Before: inCells(c.Before), After: inCells(c.After)}
		for _, p := range c.Points {
			out[i].Points = append(out[i].Points, inCells(p))
		}
	}
	return out, tr
}

func inCells(p physics.Vec2) physics.Vec2 {
	return physics.Vec2{X: float32(math.Round(float64(p.X * cellsPerMeter))), Y: float32(math.Round(float64(p.Y * cellsPerMeter)))}
}

func pts(xy ...float32) []physics.Vec2 {
	p := make([]physics.Vec2, 0, len(xy)/2)
	for k := 0; k < len(xy); k += 2 {
		p = append(p, physics.Vec2{X: xy[k], Y: xy[k+1]})
	}
	return p
}

func equalPoints(a, b []physics.Vec2) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// A block inside the chunk is one loop around it, clockwise on screen,
// which is the side Box2D's chains collide on.
func TestOutlineOfAnIsland(t *testing.T) {
	chains, _ := outlineOf(t, []string{
		"........",
		"........",
		"........",
		"..sss...",
		"..###...",
		"........",
		"........",
	})
	if len(chains) != 1 || !chains[0].Loop {
		t.Fatalf("chains %+v, want one loop", chains)
	}
	if want := pts(1, 2, 4, 2, 4, 4, 1, 4); !equalPoints(chains[0].Points, want) {
		t.Fatalf("loop %v, want %v", chains[0].Points, want)
	}
}

// Ground that goes on past the chunk's sides is an open chain along its
// top, ending on the chunk's border, whose ghost points are where the
// ground goes on in the next chunks. Where solid meets solid across the
// border there is no edge.
func TestOutlineAcrossTheBorder(t *testing.T) {
	chains, _ := outlineOf(t, []string{
		"........",
		"........",
		"........",
		"########",
		"########",
		"########",
	})
	if len(chains) != 1 || chains[0].Loop {
		t.Fatalf("chains %+v, want one open chain", chains)
	}
	c := chains[0]
	if !equalPoints(c.Points, pts(0, 2, 6, 2)) || c.Before != (physics.Vec2{X: -1, Y: 2}) || c.After != (physics.Vec2{X: 7, Y: 2}) {
		t.Fatalf("chain %+v, want (0, 2) to (6, 2) with ghosts at x -1 and 7", c)
	}
}

// Cells that touch only at a corner are kept apart: two loops.
func TestOutlineKeepsCornersApart(t *testing.T) {
	chains, _ := outlineOf(t, []string{
		"......",
		".#....",
		"..#...",
		"......",
		"......",
	})
	if len(chains) != 2 || !chains[0].Loop || !chains[1].Loop {
		t.Fatalf("chains %+v, want two loops", chains)
	}
	if !equalPoints(chains[0].Points, pts(0, 0, 1, 0, 1, 1, 0, 1)) || !equalPoints(chains[1].Points, pts(1, 1, 2, 1, 2, 2, 1, 2)) {
		t.Fatalf("loops %v and %v, want the two cells' squares", chains[0].Points, chains[1].Points)
	}
}

// A 45° staircase becomes one straight edge along its outer corners. A
// bump of one cell stays, though its far side may become a ramp that ends
// within the tolerance of the floor.
func TestContourSimplifiesStairs(t *testing.T) {
	chains, _ := outlineOf(t, []string{
		"........#",
		".......##",
		"......###",
		".....####",
		"....#####",
		"...######",
		"..#######",
		".########",
		"#########",
	})
	if len(chains) != 1 || !equalPoints(chains[0].Points, pts(0, 7, 7, 0)) {
		t.Fatalf("chains %+v, want one edge from (0, 7) to (7, 0)", chains)
	}
	chains, _ = outlineOf(t, []string{
		"........",
		"........",
		"........",
		"...#....",
		"########",
		"########",
	})
	if len(chains) != 1 || !equalPoints(chains[0].Points, pts(0, 3, 2, 3, 2, 2, 6, 3)) {
		t.Fatalf("chains %+v, want the bump kept up to its top-left corner, then a ramp", chains)
	}
}

// On random terrain every edge between a solid and an open cell of the
// chunk lies within the tolerance of a chain, the chains are well formed,
// and Box2D takes them.
func TestOutlineCoversEveryEdge(t *testing.T) {
	phys, err := physics.NewWorld(physics.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer phys.Close()
	s := rng.New(5)
	for round := range 200 {
		w, h := 4+s.IntN(30), 4+s.IntN(30)
		fill := 20 + s.IntN(60)
		rows := make([]string, h+2)
		for j := range rows {
			b := make([]byte, w+2)
			for i := range b {
				b[i] = '.'
				if s.IntN(100) < fill {
					b[i] = '#'
				}
			}
			rows[j] = string(b)
		}
		chains, tr := outlineOf(t, rows)
		for _, c := range chains {
			checkChain(t, round, c, w, h)
		}
		for j := range h {
			for i := range w {
				if !tr.solid(i, j) {
					continue
				}
				for _, side := range [4][3]float32{{0, -1, 0}, {1, 0, 1}, {0, 1, 2}, {-1, 0, 3}} {
					if tr.solid(i+int(side[0]), j+int(side[1])) {
						continue
					}
					mx, my := float32(i)+0.5+side[0]/2, float32(j)+0.5+side[1]/2
					if d := nearest(chains, mx, my); d > float32(tolNum)/tolDen+1e-4 {
						t.Fatalf("round %d: the edge of cell (%d, %d) at (%v, %v) is %v cells from every chain\n%s",
							round, i, j, mx, my, d, joinRows(rows))
					}
				}
			}
		}
		if _, err := phys.AddStaticChains(tr.chains, terrainFriction); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
}

func checkChain(t *testing.T, round int, c physics.Chain, w, h int) {
	t.Helper()
	least := 2
	if c.Loop {
		least = 3
	}
	if len(c.Points) < least {
		t.Fatalf("round %d: chain %+v has too few points", round, c)
	}
	for k := range c.Points {
		if k+1 < len(c.Points) && c.Points[k] == c.Points[k+1] || c.Loop && k+1 == len(c.Points) && c.Points[k] == c.Points[0] {
			t.Fatalf("round %d: chain %+v repeats a point", round, c)
		}
	}
	if c.Loop {
		return
	}
	onBorder := func(p physics.Vec2) bool {
		return p.X == 0 || p.Y == 0 || p.X == float32(w) || p.Y == float32(h)
	}
	if !onBorder(c.Points[0]) || !onBorder(c.Points[len(c.Points)-1]) {
		t.Fatalf("round %d: open chain %+v does not end on the border of %d×%d", round, c, w, h)
	}
	if c.Before == c.Points[0] || c.After == c.Points[len(c.Points)-1] {
		t.Fatalf("round %d: open chain %+v has a ghost on an end", round, c)
	}
}

// nearest returns the distance from (x, y) to the nearest segment of the
// chains.
func nearest(chains []physics.Chain, x, y float32) float32 {
	best := float32(math.Inf(1))
	for _, c := range chains {
		n := len(c.Points)
		segs := n - 1
		if c.Loop {
			segs = n
		}
		for k := range segs {
			best = min(best, segDist(c.Points[k], c.Points[(k+1)%n], x, y))
		}
	}
	return best
}

func segDist(a, b physics.Vec2, x, y float32) float32 {
	dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
	px, py := float64(x-a.X), float64(y-a.Y)
	u := math.Max(0, math.Min(1, (px*dx+py*dy)/(dx*dx+dy*dy)))
	return float32(math.Hypot(px-u*dx, py-u*dy))
}

func joinRows(rows []string) string {
	s := ""
	for _, r := range rows {
		s += r + "\n"
	}
	return s
}

// Two chunks that a flat floor crosses meet in one point, and each one's
// ghost lies on the other's edge, so a body slides across without catching.
func TestChunksMeetAtTheirBorder(t *testing.T) {
	s, w, id := newSystem(t, 128, 64, 1)
	w.Fill(0, 40, 128, 64, id.stone)
	var ends [2]physics.Chain
	for i := range 2 {
		c := s.chunkCells(i)
		cls := make([]world.Class, (c.x1-c.x0+2)*(c.y1-c.y0+2))
		w.Classify(c.x0-1, c.y0-1, c.x1+1, c.y1+1, cls)
		tr := &tracer{cls: cls}
		chains := tr.outline(c.x0, c.y0, c.x1-c.x0, c.y1-c.y0)
		if len(chains) != 1 {
			t.Fatalf("chunk %d has %d chains, want 1", i, len(chains))
		}
		ends[i] = physics.Chain{Points: append([]physics.Vec2(nil), chains[0].Points...), Before: chains[0].Before, After: chains[0].After}
	}
	a, b := ends[0], ends[1]
	if a.Points[len(a.Points)-1] != b.Points[0] {
		t.Fatalf("the left chunk ends at %v, the right starts at %v", a.Points[len(a.Points)-1], b.Points[0])
	}
	if segDist(b.Points[0], b.Points[1], a.After.X, a.After.Y) > 1e-6 || segDist(a.Points[len(a.Points)-2], a.Points[len(a.Points)-1], b.Before.X, b.Before.Y) > 1e-6 {
		t.Fatalf("ghosts %v and %v are not on the other chunk's edge", a.After, b.Before)
	}
}

// A chunk is rebuilt only when what bodies collide with changes in or
// around it, and the bodies near it are woken.
func TestTerrainRebuildsOnlyOnChange(t *testing.T) {
	s, w, id := newSystem(t, 128, 128, 1)
	w.Fill(0, 100, 128, 128, id.stone)
	drop(t, s, id.wood, 30, 70, 12, 10)
	for range 200 {
		tick(t, s, w)
	}
	b := &s.bodies[0]
	if s.phys.Awake(b.id) {
		t.Fatal("the body did not fall asleep")
	}
	// Water flowing nearby changes cells but nothing a body collides with.
	w.Fill(60, 60, 70, 70, id.water)
	for range 3 {
		tick(t, s, w)
		if len(s.rebuilt) != 0 {
			t.Fatalf("water rebuilt chunks %v", s.rebuilt)
		}
	}
	// Digging out the stone under the body rebuilds its chunk and wakes it.
	w.Fill(20, 90, 50, 110, 0)
	tick(t, s, w)
	if len(s.rebuilt) == 0 || !s.phys.Awake(b.id) {
		t.Fatalf("digging under the body rebuilt %v and left it awake %v", s.rebuilt, s.phys.Awake(b.id))
	}
	for range 100 {
		tick(t, s, w)
	}
	if _, r := owned(w, id.wood, 0); r.y1 != 110 {
		t.Fatalf("the body is at %v, want fallen into the hole, on row 110", r)
	}
}
