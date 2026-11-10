package rigid

import (
	"math"
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// play runs ticks ticks in the game's order, breaking the bodies that lost
// pixels first, and returns what breaking sent to debris.
func play(tb testing.TB, s *System, w interface{ Step() }, ticks int) []int {
	tb.Helper()
	debris := make([]int, testMaterials)
	for range ticks {
		if err := s.Break(debris); err != nil {
			tb.Fatal(err)
		}
		s.Write()
		collide(tb, s)
		w.Step()
		if err := s.Step(); err != nil {
			tb.Fatal(err)
		}
	}
	return debris
}

// lostBy returns what cracks have taken out, by material.
func lostBy(s *System) []int {
	n := make([]int, testMaterials)
	s.Lost(n)
	return n
}

// throw drops a body of m, w × h, with its top-left at (x, y), moving down
// at vy cells per second.
func throw(tb testing.TB, s *System, m material.ID, x, y, w, h int, vy float32) {
	tb.Helper()
	if ok, err := s.Drop(m, x, y, w, h, 0, 0, vy); err != nil || !ok {
		tb.Fatalf("Drop at (%d, %d): %v, %v", x, y, ok, err)
	}
}

// A body of a material that cracks takes nothing from a hit no faster
// than crackMin, and cracks at every hit from crackSure on (C138). A body
// of a material that does not crack never does.
func TestCracksNeedSpeed(t *testing.T) {
	for _, tt := range []struct {
		name  string
		mat   func(c cold, id ids) material.ID
		vy    float32
		crack bool
	}{
		// From 3 cells up, it lands at about 27 cells per second.
		{"ice landing slowly", func(c cold, _ ids) material.ID { return c.iceBody }, 0, false},
		{"ice thrown at 15 m/s", func(c cold, _ ids) material.ID { return c.iceBody }, 150, true},
		{"wood thrown at 15 m/s", func(_ cold, id ids) material.ID { return id.wood }, 150, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, w, id := newSystem(t, 200, 120, 1)
			w.Fill(0, 110, 200, 120, id.stone)
			m := tt.mat(coldIDs(s), id)
			throw(t, s, m, 90, 99, 16, 8, tt.vy)
			play(t, s, w, 60)
			if lost := lostBy(s)[m]; (lost > 0) != tt.crack {
				t.Fatalf("the body lost %d pixels to cracks; want some: %t", lost, tt.crack)
			}
		})
	}
}

// Above crackMin, a check cracks with chance speed / crackSure: about half
// the time just above crackMin, three times in four at 75 cells per
// second, and always from crackSure on (C138). The draw is the one crack
// makes.
func TestCrackChanceGrowsWithSpeed(t *testing.T) {
	const draws = 20000
	for _, tt := range []struct {
		speed    float64
		lo, high float64
	}{{51, 0.49, 0.53}, {75, 0.73, 0.77}, {100, 1, 1}, {400, 1, 1}} {
		n := 0
		for k := range uint64(draws) {
			h := rng.Hash3(1^crackSalt, k, 7, 0)
			if u := float64(h>>11) / (1 << 53); u*crackSure <= tt.speed {
				n++
			}
		}
		if f := float64(n) / draws; f < tt.lo || f > tt.high {
			t.Errorf("at %v cells per second %.3f of checks crack, want %v to %v", tt.speed, f, tt.lo, tt.high)
		}
	}
}

// / laid drops a body of m, w × h, with its top-left at (x, y), and writes
// it, without stepping Box2D: a body that stays where it is for a crack to
// walk through.
func laid(tb testing.TB, s *System, m material.ID, x, y, w, h int) {
	tb.Helper()
	drop(tb, s, m, x, y, w, h)
	s.Write()
}

// marked returns the pixels of b marked by cracks.
func marked(b *body) []int {
	var k []int
	for i, px := range b.px {
		if px.marked() {
			k = append(k, i)
		}
	}
	return k
}

// connected reports whether the pixels k of b are one 4-connected set.
func connected(b *body, k []int) bool {
	in := make(map[int]bool, len(k))
	for _, i := range k {
		in[i] = true
	}
	seen := map[int]bool{k[0]: true}
	todo := []int{k[0]}
	for len(todo) > 0 {
		i := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		x, y := i%b.w, i/b.w
		for _, n := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
			if n[0] < 0 || n[1] < 0 || n[0] >= b.w || n[1] >= b.h {
				continue
			}
			if j := n[1]*b.w + n[0]; in[j] && !seen[j] {
				seen[j] = true
				todo = append(todo, j)
			}
		}
	}
	return len(seen) == len(k)
}

// A crack walks as Noita's does (C139 3): its first frame always moves,
// later ones with chance 21/101, each 4 to 10 steps of a cell, each step
// spending a unit of energy. Each frame turns the axis it set out along,
// the way into the body, anew and does not keep the turn, so the walk
// zigzags about the axis: per step it goes on average 0.70 cells along it
// (the mean cosine of the turns, 0.824, times the 86/101 steps that do not
// jitter) and none across. Inside a body too big to leave, it ends when its
// energy, 200, runs out, and takes nothing out: its marks stay, a line of
// pixels 4-connected through diagonal steps, drawn as at full damage.
func TestCrackWalksAboutItsAxis(t *testing.T) {
	s, w, _ := newSystem(t, 260, 260, 1)
	c := coldIDs(s)
	laid(t, s, c.iceBody, 5, 5, 250, 250)
	b := &s.bodies[0]
	const movers = 200
	moved, later, steps, frames, forked := 0, 0, 0, 0, 0
	along, across := 0.0, 0.0
	for k := range uint64(movers) {
		s.movers = append(s.movers[:0], newMover(130.5, 230.5, 0, -1, rng.Hash3(5, k, 0, 0)))
		for f := 0; len(s.movers) > 0; f++ {
			m := s.movers[0]
			s.moveCracks()
			if len(s.movers) == 0 {
				break
			}
			if len(s.movers) == 2 {
				// It branched where its energy came to its branching energy,
				// into two cracks with half that energy, turned 0.3 to 1.5
				// radians to either side (C139 3.4), and ended.
				forked++
				l, r := s.movers[0], s.movers[1]
				cl, cr := float64(l.ax*m.ax+l.ay*m.ay), float64(r.ax*m.ax+r.ay*m.ay)
				sl, sr := float64(m.ax*l.ay-m.ay*l.ax), float64(m.ax*r.ay-m.ay*r.ax)
				for _, c := range []mover{l, r} {
					if c.energy != m.split/2 || c.forks != m.forks-1 || c.split != m.split/2 {
						t.Fatalf("a branch of a crack of split %d and %d forks has energy %d, split %d, forks %d",
							m.split, m.forks, c.energy, c.split, c.forks)
					}
				}
				if a, b := math.Atan2(sl, cl), math.Atan2(sr, cr); a < -1.51 || a > -0.29 || b < 0.29 || b > 1.51 {
					t.Fatalf("the branches turned %.2f and %.2f radians, want -1.5 to -0.3 and 0.3 to 1.5", a, b)
				}
				break
			}
			d := int(m.energy - s.movers[0].energy)
			switch {
			case f == 0 && (d < crackMinSteps || d > crackMaxSteps):
				t.Fatalf("mover %d took %d steps in its first frame, want %d to %d", k, d, crackMinSteps, crackMaxSteps)
			case d != 0 && (d < crackMinSteps || d > crackMaxSteps):
				t.Fatalf("mover %d took %d steps in frame %d, want 0 or %d to %d", k, d, f, crackMinSteps, crackMaxSteps)
			}
			if f > 0 {
				later++
				if d > 0 {
					moved++
				}
			}
			if d > 0 {
				frames++
				steps += d
				along += float64(m.y - s.movers[0].y)
				across += float64(s.movers[0].x - m.x)
			}
		}
		if k == 0 {
			line := marked(b)
			if len(line) < 20 || !connected(b, line) {
				t.Fatalf("the first crack marked %d pixels, 4-connected: %t; want a line of 20 or more",
					len(line), len(line) > 0 && connected(b, line))
			}
			if !slices.Contains(line, (230-5)*b.w+130-5) {
				t.Fatal("the first crack did not mark the pixel it started on")
			}
			if cell, _ := w.At(130, 230); cell.Damage != crackShade {
				t.Fatalf("a marked pixel's cell shows damage %d, want %d", cell.Damage, crackShade)
			}
		}
	}
	if n := lostBy(s)[c.iceBody]; n != 0 {
		t.Fatalf("cracks that ran out of energy inside the body took out %d pixels, want none", n)
	}
	rate := float64(moved) / float64(later)
	mean := float64(steps) / float64(frames)
	t.Logf("%d movers: moved in %.3f of later frames, %.2f steps a frame, %.3f cells along and %.3f across a step, %d branched",
		movers, rate, mean, along/float64(steps), across/float64(steps), forked)
	// A crack branches when it may, 2 in 3, and its energy, spent a step at
	// a time, comes to its branching energy.
	if forked < movers/2 {
		t.Errorf("%d of %d cracks branched, want about 2 in 3", forked, movers)
	}
	if want := 21.0 / 101; rate < want-0.012 || rate > want+0.012 {
		t.Errorf("movers moved in %.3f of their later frames, want %.3f", rate, want)
	}
	if mean < 6.85 || mean > 7.15 {
		t.Errorf("a moving frame took %.2f steps on average, want 7", mean)
	}
	if a := along / float64(steps); a < 0.66 || a > 0.74 {
		t.Errorf("a step went %.3f cells along the axis on average, want 0.70", a)
	}
	if a := across / float64(steps); a < -0.03 || a > 0.03 {
		t.Errorf("a step went %.3f cells across the axis on average, want 0", a)
	}
}

// When a crack ends in a frame in which it marked something, every marked
// pixel of the body it marked last is taken out, those other cracks marked
// too, and counts as lost (C139 4.2). The marks on another body stay. A
// crack whose last frame marked nothing takes nothing out, and the marks it
// leaves stay as bright lines (C139 4.3).
func TestCrackTakesOutTheMarksOfItsBody(t *testing.T) {
	// A plank 3 pixels thick, which a crack set off up from inside its
	// bottom row mostly leaves in its first frame; the first seed whose
	// crack ends in its first frame.
	for seed := uint64(0); ; seed++ {
		if seed == 50 {
			t.Fatal("no crack of 50 ended in its first frame")
		}
		s, w, _ := newSystem(t, 200, 100, 1)
		c := coldIDs(s)
		laid(t, s, c.iceBody, 50, 40, 100, 3)
		laid(t, s, c.iceBody, 170, 40, 20, 3)
		plank, other := &s.bodies[0], &s.bodies[1]
		for _, at := range [][2]int{{55, 41}, {145, 40}, {175, 42}} {
			if _, ok := s.markCrack(at[0], at[1]); !ok {
				t.Fatalf("cell %v could not be marked", at)
			}
		}
		s.movers = append(s.movers[:0], newMover(100.5, 42.5, 0, -1, seed))
		s.moveCracks()
		if len(s.movers) > 0 {
			continue
		}
		lost := lostBy(s)[c.iceBody]
		if lost < 3 || plank.n != 300-lost {
			t.Fatalf("the plank lost %d pixels, has %d of 300; want the 2 marked before, the crack's line, and no more", lost, plank.n)
		}
		if k := marked(plank); len(k) != 0 {
			t.Fatalf("the plank still has %d marked pixels", len(k))
		}
		for _, at := range [][2]int{{55, 41}, {145, 40}} {
			if cell, _ := w.At(at[0], at[1]); cell.Mat != material.Air {
				t.Fatalf("marked cell %v is %d, want taken out", at, cell.Mat)
			}
		}
		if k := marked(other); len(k) != 1 || other.n != 60 {
			t.Fatalf("the other body has %d marks and %d pixels, want its mark and all 60", len(k), other.n)
		}
		// A crack that leaves through the air, marking nothing, takes
		// nothing out.
		s.movers = append(s.movers[:0], newMover(180.5, 30.5, 0, -1, seed))
		s.moveCracks()
		if len(s.movers) != 0 || lostBy(s)[c.iceBody] != lost || len(marked(other)) != 1 {
			t.Fatalf("a crack through the air: %d movers left, %d lost, %d marks on the other body; want none, %d, 1",
				len(s.movers), lostBy(s)[c.iceBody], len(marked(other)), lost)
		}
		if cell, _ := w.At(175, 42); cell.Damage != crackShade {
			t.Fatalf("the mark left on the other body shows damage %d, want %d", cell.Damage, crackShade)
		}
		return
	}
}

// impact throws a body of m, w × h, down at vy cells per second onto a
// stone floor in a world of seed, and returns the system, the world, and
// the cracks the tick it lands makes, before they move.
func impact(tb testing.TB, seed uint64, m func(cold) material.ID, w, h int, vy float32) (*System, *world.World, []mover) {
	tb.Helper()
	s, wd, id := newSeededSystem(tb, 400, 300, seed)
	wd.Fill(0, 290, 400, 300, id.stone)
	throw(tb, s, m(coldIDs(s)), 160, 290-h-40, w, h, vy)
	for range 120 {
		if err := s.Break(make([]int, testMaterials)); err != nil {
			tb.Fatal(err)
		}
		s.Write()
		collide(tb, s)
		wd.Step()
		if err := s.advance(); err != nil {
			tb.Fatal(err)
		}
		s.crack()
		if len(s.movers) > 0 {
			return s, wd, slices.Clone(s.movers)
		}
		s.moveCracks()
		if err := s.breakCracked(); err != nil {
			tb.Fatal(err)
		}
	}
	tb.Fatal("the body made no crack")
	return nil, nil, nil
}

// A body that hits something fast makes 4 cracks in the tick it hits, as
// Noita's bodies did whatever their size (C139 5), spread over the points
// where it touches: here the corners of its bottom, and where the floor's
// collision changes chunk. Each sets out from its point straight into the
// body, up, whatever the speed (C139 3.1).
func TestHitMakesFourCracks(t *testing.T) {
	for _, tt := range []struct {
		name string
		w, h int
		xs   []float32
	}{
		{"64×24", 64, 24, []float32{160, 192, 224}},
		{"16×8", 16, 8, []float32{160, 176}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, made := impact(t, 1, func(c cold) material.ID { return c.iceBody }, tt.w, tt.h, 300)
			if len(made) != crackChecks {
				t.Fatalf("the hit made %d cracks, want %d", len(made), crackChecks)
			}
			starts := map[float32]bool{}
			for _, m := range made {
				x := float32(math.Round(float64(m.x)))
				starts[x] = true
				if !slices.Contains(tt.xs, x) || m.y < 289 || m.y > 291 {
					t.Fatalf("a crack starts at (%v, %v), want on the floor at x in %v", m.x, m.y, tt.xs)
				}
				if m.ax != 0 || m.ay != -1 || m.energy != crackEnergy || m.started {
					t.Fatalf("a crack sets out along (%v, %v) with energy %d, started %t; want up, %d, not yet",
						m.ax, m.ay, m.energy, m.started, crackEnergy)
				}
			}
			if len(starts) < 2 {
				t.Fatalf("the cracks start at %v, want at least both corners", starts)
			}
		})
	}
}

// Glass that shatters, thrown down fast enough, cracks where it lands and
// then turns whole into powder: the pixels the crack took are lost, the
// rest powder, so nothing is made (C136, C138).
func TestThrownGlassShattersWhereItLands(t *testing.T) {
	s, w, id := newSystem(t, 200, 120, 1)
	c := coldIDs(s)
	w.Fill(0, 110, 200, 120, id.stone)
	throw(t, s, c.glassBody, 90, 60, 12, 8, 150)
	debris := play(t, s, w, 60)
	if len(s.bodies) != 0 {
		t.Fatalf("%d bodies; the glass should have shattered", len(s.bodies))
	}
	lost := lostBy(s)[c.glassBody]
	if lost < 1 {
		t.Fatal("the glass lost no pixel to a crack")
	}
	if got := cells(w)[c.powder] + debris[c.powder]; got != 12*8-lost {
		t.Fatalf("%d cells of powder, want the %d pixels left after the crack", got, 12*8-lost)
	}
}

// A body that hits faster than Noita's 910 px/s, which made no crack check
// there, cracks: the developer chose that faster hits always crack
// (2026-11-10), and Box2D reports the hit (physics.TestFastHitIsReported).
// Its cracks are all made; what they take out depends on where they walk,
// as a body bouncing off fast can leave them behind in the air (C139 6).
func TestFastHitCracks(t *testing.T) {
	_, _, made := impact(t, 1, func(c cold) material.ID { return c.iceBody }, 16, 8, 1500)
	if len(made) != crackChecks {
		t.Fatalf("ice that hit the floor at 1500 cells per second made %d cracks, want %d", len(made), crackChecks)
	}
}

// A static body of a material that cracks cracks where something hits it
// fast, though it does not move: the contact's speed is what counts.
func TestStaticIceCracksWhereItIsHit(t *testing.T) {
	s, w, id := newSystem(t, 200, 120, 1)
	c := coldIDs(s)
	w.Fill(40, 80, 160, 90, c.ice)
	digCell(t, s, 40, 80)
	convert(t, s)
	throw(t, s, id.wood, 90, 40, 10, 6, 200)
	play(t, s, w, 60)
	if lostBy(s)[c.iceBody] == 0 {
		t.Fatal("the static ice lost no pixel where the wood hit it")
	}
	if !s.bodies[0].static {
		t.Fatal("the cracked ice is no longer static")
	}
}

// Far from the camera, a dynamic body of a material that turns back into
// terrain does so, cell for pixel: from settleFar whatever it does, from
// settleNear only asleep (C138). Nearer, awake bodies, static bodies and
// bodies of materials with no SleepTo stay.
func TestSettleTurnsFarBodiesIntoTerrain(t *testing.T) {
	s, w, id := newSystem(t, 1600, 200, 1)
	c := coldIDs(s)
	w.Fill(0, 190, 1600, 200, id.stone)
	// Static ice far away, and four bodies set down on the floor, too
	// gently to crack: ice at 0, 500 and 1000 cells from the camera, glass
	// that shatters at 1000.
	w.Fill(1400, 170, 1420, 180, c.ice)
	digCell(t, s, 1400, 170)
	convert(t, s)
	for _, x := range []int{100, 600, 1100} {
		throw(t, s, c.iceBody, x, 181, 16, 8, 0)
	}
	throw(t, s, c.glassBody, 1150, 181, 12, 8, 0)
	for i := 0; ; i++ {
		awake := false
		for k := range s.bodies {
			awake = awake || !s.bodies[k].static && s.phys.Awake(s.bodies[k].id)
		}
		if !awake {
			break
		}
		if i == 600 {
			t.Fatal("the bodies did not fall asleep")
		}
		play(t, s, w, 1)
	}
	camX, camY := float32(108), float32(174)
	// Woken, the ice 500 cells away stays; the one 1000 away does not.
	s.phys.Wake(s.bodies[2].id)
	if err := s.Settle(camX, camY); err != nil {
		t.Fatal(err)
	}
	// The static ice, the ice at 0 and 500, and the glass stay.
	if len(s.bodies) != 4 || w.Count(c.ice) != 16*8 {
		t.Fatalf("with the middle ice awake: %d bodies and %d ice cells; want 4 and the far one's %d", len(s.bodies), w.Count(c.ice), 16*8)
	}
	play(t, s, w, 120)
	if err := s.Settle(camX, camY); err != nil {
		t.Fatal(err)
	}
	if len(s.bodies) != 3 || w.Count(c.ice) != 2*16*8 {
		t.Fatalf("asleep: %d bodies and %d ice cells; want 3 and %d", len(s.bodies), w.Count(c.ice), 2*16*8)
	}
	// 199 cells of ice turned into the static body, then two bodies back.
	if n := turnedBy(s); n[c.iceBody] != 199-2*16*8 || n[c.ice] != -199+2*16*8 {
		t.Fatalf("turned %v; want 199 ice into a static body and 2 ice bodies of %d pixels back into ice", n, 16*8)
	}
	for k := range s.bodies {
		if b := &s.bodies[k]; b.mat == c.iceBody && !b.static {
			if p := s.phys.Position(b.id); p.X*cellsPerMeter > 300 {
				t.Fatalf("an ice body at %v stays", p)
			}
		}
	}
}

// A body a crack took pixels out of is split, or shattered, before the
// tick ends, so a block that leaves memory, or a save, between that tick
// and the next records it as it is: not still the whole box it was made
// as with pixels missing, which loading refuses. The pixels a crack cuts
// off from the rest, too few for a body, are lost with the crack.
func TestCrackedBodyLeavesWithItsBlockAsItIs(t *testing.T) {
	s, w, _ := newStreamedSystem(t, 1, 1)
	c := coldIDs(s)
	floor := w.Height() - 4
	throw(t, s, c.iceBody, 200, floor-30, 16, 6, 200)
	lost := 0
	for i := 0; lost == 0; i++ {
		if i == 120 {
			t.Fatal("the ice never cracked")
		}
		tick(t, s, w)
		lost = lostBy(s)[c.iceBody]
	}
	for i := range s.bodies {
		if s.bodies[i].dirty {
			t.Fatal("a cracked body waits for the next tick to split")
		}
	}
	recs := s.Unload(0, 0, nil)
	if err := s.Load(0, 0, recs); err != nil {
		t.Fatalf("a body unloaded the tick it cracked does not load: %v", err)
	}
}

// A stack of ice asleep far enough from the camera turns back into terrain
// whole: which bodies turn is settled before any does, as turning one
// takes it out of Box2D, which wakes the bodies it touched (Codex review).
func TestSettleTakesAWholeStack(t *testing.T) {
	s, w, id := newSystem(t, 1600, 200, 1)
	c := coldIDs(s)
	w.Fill(0, 190, 1600, 200, id.stone)
	throw(t, s, c.iceBody, 600, 183, 16, 6, 0)
	throw(t, s, c.iceBody, 602, 176, 16, 6, 0)
	for i := 0; s.phys.Awake(s.bodies[0].id) || s.phys.Awake(s.bodies[1].id); i++ {
		if i == 600 {
			t.Fatal("the stack did not fall asleep")
		}
		play(t, s, w, 1)
	}
	if err := s.Settle(100, 180); err != nil {
		t.Fatal(err)
	}
	if len(s.bodies) != 0 || w.Count(c.ice) != 2*16*6 {
		t.Fatalf("%d bodies and %d ice cells after the check; want none and %d", len(s.bodies), w.Count(c.ice), 2*16*6)
	}
}

// When a crack splits a body, at the end of the tick, the cells of its
// pixels link to the body and the pixel that now hold them before the
// tick ends: the next tick's tool digs before the bodies are written
// again, and must dig the pixel the cell shows (Codex review).
func TestCrackedBodyCellsLinkToTheirPieces(t *testing.T) {
	s, w, id := newSystem(t, 200, 120, 1)
	c := coldIDs(s)
	w.Fill(0, 110, 200, 120, id.stone)
	throw(t, s, c.iceBody, 90, 103, 30, 6, 0)
	for range 60 {
		tick(t, s, w)
	}
	// A crack down through the middle, as a crack that ends takes them out.
	b := &s.bodies[0]
	for j := range b.h {
		s.takeOut(b, 0, j*b.w+12)
	}
	if err := s.Step(); err != nil {
		t.Fatal(err)
	}
	if len(s.bodies) != 2 {
		t.Fatalf("%d bodies after the cut, want 2", len(s.bodies))
	}
	for y := range w.Height() {
		for x := range w.Width() {
			cell, _ := w.At(x, y)
			if s.kinds[cell.Mat] != material.Solid {
				continue
			}
			slot, k := cell.Body()
			if int(slot) >= len(s.bodies) || int(k) >= len(s.bodies[slot].px) || !s.bodies[slot].px[k].ok() {
				t.Fatalf("cell (%d, %d) links to body %d pixel %d, which is not one", x, y, slot, k)
			}
			if got := slices.Contains(s.bodies[slot].cells, cellAt{int32(x), int32(y)}); !got {
				t.Fatalf("cell (%d, %d) links to body %d, which does not list it", x, y, slot)
			}
		}
	}
	// Digging a cell of the piece wears the piece's pixel.
	piece := &s.bodies[1]
	at := piece.cells[0]
	if _, ok := s.Wear(int(at.x), int(at.y), 1<<30); !ok || piece.n == piece.w*piece.h {
		t.Fatal("digging a cell of the piece did not take a pixel out of it")
	}
}

// firstLoss throws a body as impact does and returns how many pixels the
// cracks of the tick it lands take out once they have all ended; no later
// hit makes more.
func firstLoss(tb testing.TB, seed uint64, m func(cold) material.ID, w, h int, vy float32) int {
	tb.Helper()
	s, wd, _ := impact(tb, seed, m, w, h, vy)
	s.moveCracks()
	if err := s.breakCracked(); err != nil {
		tb.Fatal(err)
	}
	for range 600 {
		if len(s.movers) == 0 {
			break
		}
		if err := s.Break(make([]int, testMaterials)); err != nil {
			tb.Fatal(err)
		}
		s.Write()
		collide(tb, s)
		wd.Step()
		if err := s.advance(); err != nil {
			tb.Fatal(err)
		}
		s.moveCracks()
		if err := s.breakCracked(); err != nil {
			tb.Fatal(err)
		}
	}
	if len(s.movers) > 0 {
		tb.Fatalf("%d cracks still walk 600 ticks after the hit", len(s.movers))
	}
	n := 0
	for _, k := range lostBy(s) {
		n += k
	}
	return n
}

// median returns the median of v, which it sorts.
func median(v []int) float64 {
	slices.Sort(v)
	if len(v)%2 == 1 {
		return float64(v[len(v)/2])
	}
	return float64(v[len(v)/2-1]+v[len(v)/2]) / 2
}

// The pixels a hit's cracks take out are about as many as Noita's took
// (C139 5): thrown at 150, 300 and 600 px/s onto rock, a 64×24 body of ice
// or of glass that does not shatter (crackability 100) lost 39 to 95 cells
// to the cracks of its first hit, 68 at the median, and a 64×4 slab of that
// glass 10 to 18, 11 at the median. The speed changes only whether cracks
// are made, not what they do, so the losses do not grow with it.
func TestCrackLossesMatchNoita(t *testing.T) {
	ice := func(c cold) material.ID { return c.iceBody }
	slab := func(c cold) material.ID { return c.slab }
	var thick, thin []int
	for seed := uint64(1); seed <= 6; seed++ {
		for _, v := range []float32{150, 300, 600} {
			thick = append(thick, firstLoss(t, seed, ice, 64, 24, v), firstLoss(t, seed, slab, 64, 24, v))
			thin = append(thin, firstLoss(t, seed, slab, 64, 4, v))
		}
	}
	t.Logf("64×24: %v", thick)
	t.Logf("64×4: %v", thin)
	mt, mn := median(thick), median(thin)
	t.Logf("medians: 64×24 %.1f (Noita 68), 64×4 %.1f (Noita 11)", mt, mn)
	if mt < 39 || mt > 95 {
		t.Errorf("a 64×24 body's first hit took out %.1f pixels at the median, want within Noita's 39 to 95", mt)
	}
	if mn < 10 || mn > 18 {
		t.Errorf("a 64×4 slab's first hit took out %.1f pixels at the median, want within Noita's 10 to 18", mn)
	}
}

// Walking cracks allocates nothing once its lists have grown: neither
// stepping and marking, nor ending and taking the marks out.
func TestWalkingCracksDoesNotAllocate(t *testing.T) {
	s, _, _ := newSystem(t, 260, 260, 1)
	c := coldIDs(s)
	laid(t, s, c.iceBody, 5, 5, 250, 250)
	laid(t, s, c.iceBody, 5, 256, 250, 3)
	var start []mover
	for k := range uint64(8) {
		start = append(start, newMover(130.5, 230.5, 0, -1, k), newMover(20.5+float32(k), 258.5, 0, -1, k))
	}
	s.movers = append(s.movers, start...)
	s.moveCracks()
	if n := testing.AllocsPerRun(50, func() {
		s.movers = append(s.movers[:0], start...)
		s.moveCracks()
	}); n != 0 {
		t.Fatalf("walking cracks allocates %v times", n)
	}
	if lostBy(s)[c.iceBody] == 0 {
		t.Fatal("no crack took anything out of the thin body; the test needs some to")
	}
}
