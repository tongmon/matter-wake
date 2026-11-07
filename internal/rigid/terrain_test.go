package rigid

import (
	"math"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
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

// A 45° staircase becomes one straight edge along its outer corners,
// between the lattice edges an open chain keeps at its ends. A bump of one
// cell stays, with a corner cut within the tolerance.
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
	if len(chains) != 1 || !equalPoints(chains[0].Points, pts(0, 7, 0, 6, 6, 0, 7, 0)) {
		t.Fatalf("chains %+v, want one edge from (0, 6) to (6, 0) between the steps at the ends", chains)
	}
	chains, _ = outlineOf(t, []string{
		"........",
		"........",
		"........",
		"...#....",
		"########",
		"########",
	})
	if len(chains) != 1 || !equalPoints(chains[0].Points, pts(0, 3, 2, 3, 2, 2, 3, 3, 6, 3)) {
		t.Fatalf("chains %+v, want the bump kept with its top-right corner cut", chains)
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

// Where an outline crosses a chunk border, each chunk's ghost point runs
// on in the direction of the other chunk's first or last edge as it was
// simplified, so Box2D sees the seam as the slope it is, not a corner.
// Stairs simplified into diagonals must keep this too (Codex review).
func TestGhostsFollowTheNeighborsEdges(t *testing.T) {
	for _, fill := range []func(w *world.World, stone material.ID){
		func(w *world.World, stone material.ID) { w.Fill(0, 40, 128, 64, stone) },
		func(w *world.World, stone material.ID) { // a 45° slope across x = 64
			for x := range 128 {
				w.Fill(x, 63-x/2, x+1, 64, stone)
			}
		},
		func(w *world.World, stone material.ID) { // 45° stairs
			for x := range 128 {
				w.Fill(x, max(64-x+40, 0), x+1, 64, stone)
			}
		},
	} {
		s, w, id := newSystem(t, 128, 64, 1)
		fill(w, id.stone)
		var ends [2]physics.Chain
		for i := range 2 {
			c := s.chunkCells(i)
			cls := make([]world.Class, (c.x1-c.x0+2)*(c.y1-c.y0+2))
			w.Classify(c.x0-1, c.y0-1, c.x1+1, c.y1+1, cls)
			tr := &tracer{cls: cls}
			chains := tr.outline(c.x0, c.y0, c.x1-c.x0, c.y1-c.y0)
			for _, ch := range chains {
				if !ch.Loop && (ch.Points[0].X == 6.4 || ch.Points[len(ch.Points)-1].X == 6.4) {
					ends[i] = physics.Chain{Points: append([]physics.Vec2(nil), ch.Points...), Before: ch.Before, After: ch.After}
				}
			}
		}
		a, b := ends[0], ends[1]
		if len(a.Points) == 0 || len(b.Points) == 0 {
			t.Fatalf("no chain meets the border: %+v %+v", a, b)
		}
		v := a.Points[len(a.Points)-1]
		if v != b.Points[0] {
			t.Fatalf("the left chunk ends at %v, the right starts at %v", v, b.Points[0])
		}
		if !sameWay(v, a.After, b.Points[1]) || !sameWay(v, b.Before, a.Points[len(a.Points)-2]) {
			t.Fatalf("at %v the left ghost %v does not run along the right edge to %v, or the right ghost %v along the left edge from %v",
				v, a.After, b.Points[1], b.Before, a.Points[len(a.Points)-2])
		}
	}
}

// sameWay reports whether p and q lie in the same direction from v.
func sameWay(v, p, q physics.Vec2) bool {
	px, py, qx, qy := float64(p.X-v.X), float64(p.Y-v.Y), float64(q.X-v.X), float64(q.Y-v.Y)
	return math.Abs(px*qy-py*qx) < 1e-6 && px*qx+py*qy > 0
}

// Sand that settles against a turned body, in cells its shape reaches
// into, is not terrain while the body is there: Box2D would push the body
// out of it. Sand that was terrain before stays terrain when a body
// reaches into it, so a body never loses what it rests on. Once the body
// is gone, the sand is terrain again.
func TestSettledSandInABodyIsNotTerrain(t *testing.T) {
	s, w, id := newSystem(t, 128, 128, 1)
	if err := s.add(id.wood, 30, 30, 16, 12, 30*math.Pi/180); err != nil {
		t.Fatal(err)
	}
	b := &s.bodies[0]
	s.Write()
	collide(t, s)
	// Settled sand in every free cell around the body.
	var in, out []cellAt
	for y := b.box.y0 - 2; y < b.box.y1+2; y++ {
		for x := b.box.x0 - 2; x < b.box.x1+2; x++ {
			if c, _ := w.At(x, y); c.Mat != material.Air {
				continue
			}
			w.Put(x, y, world.Cell{Mat: id.sand, Rest: world.SettleTicks})
			switch d := cellDepth(b, x, y); {
			case d > 0.11:
				in = append(in, cellAt{int32(x), int32(y)})
			case d < 0.09:
				out = append(out, cellAt{int32(x), int32(y)})
			}
		}
	}
	w.Flush()
	collide(t, s)
	if len(in) == 0 {
		t.Fatal("no sand reaches into the body")
	}
	for _, at := range in {
		if cl := classAt(s, int(at.x), int(at.y)); cl != world.Open {
			t.Fatalf("sand at %v, inside the body, is terrain (%v)", at, cl)
		}
	}
	for _, at := range out {
		if cl := classAt(s, int(at.x), int(at.y)); cl != world.Settled {
			t.Fatalf("sand at %v, outside the body, is not terrain (%v)", at, cl)
		}
	}
	// Turned a little further, the body reaches into sand that is terrain
	// now; that sand stays terrain.
	setPose(b, float64(b.p.X)*cellsPerMeter, float64(b.p.Y)*cellsPerMeter, 36)
	s.sync()
	collide(t, s)
	kept := 0
	for _, at := range out {
		if cellDepth(b, int(at.x), int(at.y)) > 0.11 {
			kept++
			if cl := classAt(s, int(at.x), int(at.y)); cl != world.Settled {
				t.Fatalf("terrain sand at %v stopped being terrain when the body reached into it", at)
			}
		}
	}
	if kept == 0 {
		t.Fatal("turning the body reached into no terrain sand")
	}
	// Moved away, the body leaves all the sand terrain.
	setPose(b, 63, 36, 30)
	s.sync()
	collide(t, s)
	for _, at := range append(in, out...) {
		if c, _ := w.At(int(at.x), int(at.y)); c.Mat == id.sand {
			if cl := classAt(s, int(at.x), int(at.y)); cl != world.Settled {
				t.Fatalf("sand at %v is not terrain after the body left", at)
			}
		}
	}
}

// Every chunk that sees a cell, its owner and the chunks beside it whose
// outlines end there, takes the same class for it, sand kept out of a
// body included: else a seam could lose or gain an edge.
func TestChunksAgreeOnSandInBodies(t *testing.T) {
	s, w, id := newSystem(t, 128, 128, 1)
	if err := s.add(id.wood, 56, 52, 16, 12, 40*math.Pi/180); err != nil {
		t.Fatal(err)
	}
	b := &s.bodies[0]
	s.Write()
	collide(t, s)
	for y := b.box.y0 - 2; y < b.box.y1+2; y++ {
		for x := b.box.x0 - 2; x < b.box.x1+2; x++ {
			if c, _ := w.At(x, y); c.Mat == material.Air {
				w.Put(x, y, world.Cell{Mat: id.sand, Rest: world.SettleTicks})
			}
		}
	}
	w.Flush()
	collide(t, s)
	compared := 0
	for i := range s.terrain {
		ch := &s.terrain[i]
		if !ch.built {
			continue
		}
		c := s.chunkCells(i)
		for y := max(c.y0-1, 0); y < min(c.y1+1, w.Height()); y++ {
			for x := max(c.x0-1, 0); x < min(c.x1+1, w.Width()); x++ {
				owner := (y/world.ChunkSize)*s.cols + x/world.ChunkSize
				if owner == i || !s.terrain[owner].built {
					continue
				}
				compared++
				if got, want := ch.cls[(y-c.y0+1)*(c.x1-c.x0+2)+x-c.x0+1], classAt(s, x, y); got != want {
					t.Fatalf("chunk %d takes (%d, %d) as %v, its owner %d as %v", i, x, y, got, owner, want)
				}
			}
		}
	}
	if compared == 0 {
		t.Fatal("no chunk saw another's cells")
	}
}

// Boxes stacked turned, with sand poured over them, stay apart: sand
// settling against them does not push one into another a little more each
// time it settles (it did, 12 cells deep).
func TestStackUnderSandStaysApart(t *testing.T) {
	s, w, id := newSystem(t, 192, 160, 1)
	w.Fill(0, 150, 192, 160, id.stone)
	for _, at := range [][2]int{{77, 100}, {91, 70}, {72, 40}} {
		drop(t, s, id.wood, at[0], at[1], 16, 12)
	}
	deepest := 0.0
	for tk := range 2000 {
		if tk >= 240 && tk < 1600 {
			for y := 2; y < 5; y++ {
				for x := 70; x < 120; x++ {
					if c, _ := w.At(x, y); c.Mat == material.Air {
						w.Set(x, y, id.sand)
					}
				}
			}
		}
		tick(t, s, w)
		if tk < 300 {
			continue // the landing, before any sand
		}
		for i := range s.bodies {
			for j := i + 1; j < len(s.bodies); j++ {
				deepest = max(deepest, bodyOverlap(s, &s.bodies[i], &s.bodies[j]))
			}
		}
	}
	if deepest > 1 {
		t.Fatalf("two bodies overlap %.2f cells under the sand", deepest)
	}
}

// A turned box dropped on a bed of settled sand comes to rest on it and
// stays there: the sand it lands on was terrain before it came, so the
// box does not sink through it.
func TestTurnedBoxRestsOnSettledSand(t *testing.T) {
	for _, deg := range []float32{0, 30, 45} {
		s, w, id := newSystem(t, 160, 160, 1)
		w.Fill(0, 150, 160, 160, id.stone)
		w.Fill(0, 110, 160, 150, id.sand)
		for range world.SettleTicks + 2 {
			tick(t, s, w)
		}
		if err := s.add(id.wood, 72, 30, 16, 12, deg*math.Pi/180); err != nil {
			t.Fatal(err)
		}
		bottom := func() float64 {
			b := &s.bodies[0]
			p, q := s.phys.Transform(b.id)
			c, sn := math.Abs(float64(q.C)), math.Abs(float64(q.S))
			return float64(p.Y)*cellsPerMeter + c*float64(b.h)/2 + sn*float64(b.w)/2
		}
		for range 300 {
			tick(t, s, w)
		}
		rest := bottom()
		for range 300 {
			tick(t, s, w)
		}
		// A fast landing pushes some settled sand away (C134 5.1c), so the
		// box may end a few cells below the bed's top, but no further, and
		// not lower later.
		if end := bottom(); rest > 116 || end > rest+0.1 {
			t.Fatalf("%v°: the box rests with its bottom at %.2f, then %.2f; the sand's top is at 110", deg, rest, end)
		}
	}
}

// A body that moves without changing a cell, all its pixels kept out by
// resting sand, still gives back the sand it reached into: the chunk is
// judged again though its cells did not change (Codex review).
func TestSandComesBackWhenABodyLeavesUnseen(t *testing.T) {
	s, w, id := newSystem(t, 128, 128, 1)
	if err := s.add(id.wood, 40, 40, 4, 4, 30*math.Pi/180); err != nil {
		t.Fatal(err)
	}
	b := &s.bodies[0]
	s.Write()
	collide(t, s)
	for y := 30; y < 60; y++ {
		for x := 30; x < 60; x++ {
			w.Put(x, y, world.Cell{Mat: id.sand, Rest: world.SettleTicks})
		}
	}
	w.Flush()
	collide(t, s)
	var kept []cellAt
	for y := 30; y < 60; y++ {
		for x := 30; x < 60; x++ {
			if classAt(s, x, y) == world.Open {
				kept = append(kept, cellAt{int32(x), int32(y)})
			}
		}
	}
	if len(kept) == 0 {
		t.Fatal("no sand is kept out of the body")
	}
	v := w.Version()
	setPose(b, float64(b.p.X)*cellsPerMeter+6, float64(b.p.Y)*cellsPerMeter, 30)
	s.sync()
	if w.Version() != v || len(b.cells) != 0 {
		t.Fatalf("the body changed cells moving through resting sand (%d cells)", len(b.cells))
	}
	collide(t, s)
	for _, at := range kept {
		if cellDepth(b, int(at.x), int(at.y)) <= 0 {
			if cl := classAt(s, int(at.x), int(at.y)); cl != world.Settled {
				t.Fatalf("sand at %v is still kept out after the body left it", at)
			}
		}
	}
}
