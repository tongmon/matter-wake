package rigid

import (
	"math"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

// A body moving down faster than splashSpeed throws the water in its way
// back against its motion as particles, at half to three quarters of its
// speed, turned at most splashTurn either way (C134 5.1e). Slower, the
// water goes into the queue, as before. Faster than sprayFast either way,
// each cell pushed sprays a speck too (C140 3.1).
func TestFastBodyThrowsWaterUp(t *testing.T) {
	for _, tt := range []struct {
		name   string
		vx, vy float32
		thrown bool
		specks int
	}{
		{"slow", 0, 20, false, 0},
		{"fast", 0, 30, true, 100},
		{"fast and sideways", 40, 60, true, 100},
		{"fast up", 0, -60, false, 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, w, id := newSystem(t, 128, 128, 1)
			w.Fill(0, 40, 128, 128, id.water)
			if ok, err := s.Drop(id.wood, 50, 60, 10, 10, 0, tt.vx, tt.vy); err != nil || !ok {
				t.Fatalf("Drop: %v, %v", ok, err)
			}
			start := cells(w)[id.water]
			s.Write()
			if got := cells(w)[id.water]; got != start {
				t.Fatalf("%d water in the grid, queue and particles after the write, want %d", got, start)
			}
			var thrown []world.Particle
			specks := 0
			for _, p := range w.AppendParticles(nil) {
				if p.Flags&world.Speck != 0 {
					specks++
				} else {
					thrown = append(thrown, p)
				}
			}
			if specks != tt.specks {
				t.Fatalf("%d specks, want %d", specks, tt.specks)
			}
			if !tt.thrown {
				if len(thrown) != 0 || w.QueueLen() != 100 {
					t.Fatalf("%d cells thrown and %d queued, want none and 100", len(thrown), w.QueueLen())
				}
				return
			}
			if len(thrown) != 100 || w.QueueLen() != 0 {
				t.Fatalf("%d cells thrown and %d queued, want 100 and none", len(thrown), w.QueueLen())
			}
			speed := math.Hypot(float64(tt.vx), float64(tt.vy))
			back := math.Atan2(-float64(tt.vy), -float64(tt.vx))
			for _, p := range thrown {
				vx, vy := float64(p.VX)/float64(perTick), float64(p.VY)/float64(perTick)
				got := math.Hypot(vx, vy)
				turn := math.Remainder(math.Atan2(vy, vx)-back, 2*math.Pi)
				if got < splashSlow*speed-0.1 || got > (splashSlow+splashSpread)*speed+0.1 || math.Abs(turn) > splashTurn+1e-3 {
					t.Fatalf("a particle flies at %.2f cells/s, turned %.3f rad from straight back; want %.1f to %.1f and at most %v",
						got, turn, splashSlow*speed, (splashSlow+splashSpread)*speed, splashTurn)
				}
				if p.Cell.Mat != id.water || p.Flags&world.Passing == 0 {
					t.Fatalf("a particle of material %d with flags %#x, want water passing", p.Cell.Mat, p.Flags)
				}
			}
		})
	}
}

// A box dropped into a pool splashes: water flies up out of the pool,
// through the box, and comes down again, and the water in the grid, the
// queue and the air is the same every tick. The specks it sprays as it
// pushes the water (C140 3.1) are born in its front row, meet the box or
// the water at once and go within a few ticks, as Noita's did: for its 40
// × 10 bar, also 10 pixels tall, half within 7 frames and none past 34
// (C140 4.2).
func TestBoxSplashesThePool(t *testing.T) {
	s, w, id := newSystem(t, 128, 160, 1)
	w.Fill(0, 150, 128, 160, id.stone)
	w.Fill(0, 100, 128, 150, id.water)
	drop(t, s, id.wood, 50, 20, 20, 10)
	start := cells(w)[id.water]
	high, flew := 100, 0
	seen := map[uint64][2]int{}
	for range 400 {
		tick(t, s, w)
		if got := cells(w)[id.water]; got != start {
			t.Fatalf("tick %d: %d water in the grid, queue and particles, want %d", w.Tick(), got, start)
		}
		flew = max(flew, w.Particles())
		for _, p := range w.AppendParticles(nil) {
			if p.Flags&world.Pushed != 0 {
				life, ok := seen[p.Seed]
				if !ok {
					life[0] = int(w.Tick())
				}
				life[1] = int(w.Tick())
				seen[p.Seed] = life
				continue
			}
			_, y := p.At()
			high = min(high, y)
		}
	}
	if flew == 0 {
		t.Fatal("no water flew")
	}
	quick, longest := 0, 0
	for _, life := range seen {
		ticks := life[1] - life[0] + 1
		longest = max(longest, ticks)
		if ticks <= 7 {
			quick++
		}
	}
	if len(seen) == 0 || 2*quick < len(seen) || longest > 34 {
		t.Fatalf("%d specks, %d of them seen at most 7 ticks, the longest %d ticks; want half within 7 and none past 34",
			len(seen), quick, longest)
	}
	// The box hits the water at about 10 m/s and throws it up at 5 to 7.5
	// m/s: 10 to 20 rows above the surface, less what it spends passing.
	if high > 95 {
		t.Fatalf("the water rose to row %d, want well above the surface at 100", high)
	}
	if w.Particles() != 0 {
		t.Fatalf("%d particles still fly after the box sank", w.Particles())
	}
}

// sprayed is a speck's velocity, in cells per second, and where it is, in
// cells.
type sprayed struct{ vx, vy, x, y float64 }

// specksOf returns the particles of w as sprayed.
func specksOf(w *world.World) []sprayed {
	var out []sprayed
	for _, p := range w.AppendParticles(nil) {
		out = append(out, sprayed{
			float64(p.VX) / float64(perTick), float64(p.VY) / float64(perTick),
			float64(p.X) / world.ParticleOne, float64(p.Y) / world.ParticleOne,
		})
	}
	return out
}

// spans checks that every speck's velocity lies within the ranges, give
// or take rounding, and that together they cover most of them.
func spans(t *testing.T, got []sprayed, xlo, xhi, ylo, yhi float64) {
	t.Helper()
	const slack = 0.02
	lx, hx, ly, hy := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, g := range got {
		if g.vx < xlo-slack || g.vx > xhi+slack || g.vy < ylo-slack || g.vy > yhi+slack {
			t.Fatalf("a speck flies at (%.2f, %.2f) cells/s, want x in [%.2f, %.2f] and y in [%.2f, %.2f]",
				g.vx, g.vy, xlo, xhi, ylo, yhi)
		}
		lx, hx, ly, hy = min(lx, g.vx), max(hx, g.vx), min(ly, g.vy), max(hy, g.vy)
	}
	if hx-lx < 0.9*(xhi-xlo) || hy-ly < 0.9*(yhi-ylo) {
		t.Fatalf("the specks fly at x in [%.2f, %.2f] and y in [%.2f, %.2f], want most of [%.2f, %.2f] and [%.2f, %.2f]",
			lx, hx, ly, hy, xlo, xhi, ylo, yhi)
	}
}

// A pixel pushing water sprays a speck of it as Noita does (C140 3.1):
// with up to 1.5 m/s of noise along each axis of the pixel's velocity,
// across up to 2.2 times that either way, along y 0.3 to 1.1 times it
// turned back, all times a quarter of the body's shorter side, from a
// twentieth of that side above the cell, passing through matter. A 20 × 20
// box falling at 13 m/s sprays at ±16.5 and -79.75 to -17.25 cells/s, as
// Noita's did (-15.7 to 16.4 and -81.1 to -4.1 over its whole plunge); a
// 40 × 10 bar half that. At 2 m/s the pixel sprays nothing.
func TestPushSpraysAsNoita(t *testing.T) {
	for _, tt := range []struct {
		name       string
		w, h       int
		v          float32
		side, rise float64
	}{
		{"box", 20, 20, 13, 20, 1},
		{"bar", 40, 10, 13, 10, 0.5},
		{"slow box", 20, 20, 2, 20, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, w, id := newSystem(t, 64, 64, 1)
			if err := s.add(id.wood, 4, 4, tt.w, tt.h, 0); err != nil {
				t.Fatal(err)
			}
			b := &s.bodies[0]
			b.v, b.spin, b.haveV = physics.Vec2{Y: tt.v}, 0, true
			water := world.Cell{Mat: id.water, Tone: 7}
			for y := 30; y < 40; y++ {
				for x := range 64 {
					s.spray(b, x, y, water, 0, 0)
				}
			}
			got := specksOf(w)
			if tt.v*tt.v <= sprayFast*sprayFast {
				if len(got) != 0 {
					t.Fatalf("%d specks at %v m/s, want none", len(got), tt.v)
				}
				return
			}
			if len(got) != 640 {
				t.Fatalf("%d specks from 640 cells, want 640", len(got))
			}
			for i, p := range w.AppendParticles(nil) {
				x, y := i%64, 30+i/64
				if p.Flags != world.Speck|world.Pushed|world.Passing || p.Cell != water {
					t.Fatalf("a speck of %+v with flags %#x, want the water's look, passing", p.Cell, p.Flags)
				}
				if got[i].x != float64(x)+0.5 || math.Abs(got[i].y-(float64(y)+0.5-tt.rise)) > 1e-4 {
					t.Fatalf("a speck from (%d, %d) starts at (%.4f, %.4f), want %.1f above the middle", x, y, got[i].x, got[i].y, tt.rise)
				}
			}
			k := tt.side / 4
			v := float64(tt.v)
			spans(t, got, -2.2*1.5*k, 2.2*1.5*k, -1.1*(v+1.5)*k, -0.3*(v-1.5)*k)
		})
	}
}

// A pixel that cannot push its cell away sprays a speck of what stays as
// Noita does (C140 3.2), from 3 m/s: no noise, across between -1.2 and 0.8
// times the pixel's x, along y as a push does, all times 0.5, 5 or 10 as
// the pixel moves at most 5, at most 7 or faster than 7 m/s, from 0.1, 1
// or 2 cells above, not passing. Stone's specks bounce; sand's do not.
func TestStuckPixelSpraysWhatStays(t *testing.T) {
	for _, tt := range []struct {
		name        string
		vx, vy      float32
		scale, rise float64
	}{
		{"3 m/s", 0, 3, 0, 0},
		{"4 m/s", 0, 4, 0.5, 0.1},
		{"6 m/s", 0, 6, 5, 1},
		{"8 m/s", 0, 8, 10, 2},
		{"5 m/s sideways and down", 5, 5, 10, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, w, id := newSystem(t, 64, 64, 1)
			if err := s.add(id.wood, 4, 4, 6, 6, 0); err != nil {
				t.Fatal(err)
			}
			b := &s.bodies[0]
			b.v, b.spin, b.haveV = physics.Vec2{X: tt.vx, Y: tt.vy}, 0, true
			stone, sand := world.Cell{Mat: id.stone, Tone: 3}, world.Cell{Mat: id.sand, Tone: 5}
			for x := range 64 {
				s.stuck(b, x, 30, stone, 0, 0)
				s.stuck(b, x, 40, sand, 0, 0)
			}
			got := specksOf(w)
			if tt.scale == 0 {
				if len(got) != 0 {
					t.Fatalf("%d specks at 3 m/s, want none", len(got))
				}
				return
			}
			for i, p := range w.AppendParticles(nil) {
				want, from := world.Speck|world.Pushed|world.Bounces, 30
				if i%2 == 1 {
					want, from = world.Speck|world.Pushed, 40
				}
				if p.Flags != want {
					t.Fatalf("a speck of material %d with flags %#x, want %#x", p.Cell.Mat, p.Flags, want)
				}
				if math.Abs(got[i].y-(float64(from)+0.5-tt.rise)) > 1e-4 {
					t.Fatalf("a speck from row %d starts at %.4f, want %.1f above the middle", from, got[i].y, tt.rise)
				}
			}
			vx, vy := float64(tt.vx), float64(tt.vy)
			if vx == 0 {
				for _, g := range got {
					if g.vx != 0 {
						t.Fatalf("a speck flies across at %.2f cells/s from a pixel moving straight down", g.vx)
					}
				}
				spans(t, got, 0, 0, -1.1*vy*tt.scale, -0.3*vy*tt.scale)
				return
			}
			spans(t, got, -1.2*vx*tt.scale, 0.8*vx*tt.scale, -1.1*vy*tt.scale, -0.3*vy*tt.scale)
		})
	}
}

// A crack throws each cell it takes out off as a speck that shows the
// marked pixel, flying up to 7.5 cells/s along each axis (C139 4.2). The
// specks hold no cell: what the crack took out stays lost.
func TestCrackThrowsSpecks(t *testing.T) {
	s, w, _ := newSystem(t, 200, 100, 1)
	c := coldIDs(s)
	laid(t, s, c.iceBody, 50, 40, 100, 3)
	for x := 60; x < 70; x++ {
		if _, ok := s.markCrack(x, 41); !ok {
			t.Fatalf("cell (%d, 41) could not be marked", x)
		}
	}
	slot, _ := s.markCrack(60, 41)
	s.crumble(slot)
	lost := lostBy(s)[c.iceBody]
	if lost != 10 || w.Particles() != 10 {
		t.Fatalf("the crack took out %d pixels and threw %d specks, want 10 and 10", lost, w.Particles())
	}
	flying := make([]int, testMaterials)
	w.Flying(flying)
	for id, n := range flying {
		if n != 0 {
			t.Fatalf("%d cells of material %d fly; specks hold none", n, id)
		}
	}
	const most = speckSpeed / 2 * float64(perTick)
	for _, p := range w.AppendParticles(nil) {
		x, y := p.At()
		if p.Flags != world.Speck|world.Bounces || p.Cell.Mat != c.iceBody || p.Cell.Damage != crackShade {
			t.Fatalf("a speck at (%d, %d) of material %d, damage %d and flags %#x; want the marked ice, bouncing",
				x, y, p.Cell.Mat, p.Cell.Damage, p.Flags)
		}
		if y != 41 || x < 60 || x >= 70 {
			t.Fatalf("a speck at (%d, %d), want in the cells taken out", x, y)
		}
		if math.Abs(float64(p.VX)) > most || math.Abs(float64(p.VY)) > most {
			t.Fatalf("a speck flies at (%d, %d), faster than %v either way", p.VX, p.VY, most)
		}
	}
	for x := 60; x < 70; x++ {
		if cell, _ := w.At(x, 41); cell.Mat != material.Air {
			t.Fatalf("cell (%d, 41) holds %d, want taken out", x, cell.Mat)
		}
	}
}
