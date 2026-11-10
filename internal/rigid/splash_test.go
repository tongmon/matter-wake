package rigid

import (
	"math"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

// A body moving down faster than splashSpeed throws the water in its way
// back against its motion as particles, at half to three quarters of its
// speed, turned at most splashTurn either way (C134 5.1e). Slower, the
// water goes into the queue, as before.
func TestFastBodyThrowsWaterUp(t *testing.T) {
	for _, tt := range []struct {
		name   string
		vx, vy float32
		thrown bool
	}{
		{"slow", 0, 20, false},
		{"fast", 0, 30, true},
		{"fast and sideways", 40, 60, true},
		{"fast up", 0, -60, false},
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
			if !tt.thrown {
				if w.Particles() != 0 || w.QueueLen() != 100 {
					t.Fatalf("%d particles and %d queued, want none and 100", w.Particles(), w.QueueLen())
				}
				return
			}
			if w.Particles() != 100 || w.QueueLen() != 0 {
				t.Fatalf("%d particles and %d queued, want 100 and none", w.Particles(), w.QueueLen())
			}
			speed := math.Hypot(float64(tt.vx), float64(tt.vy))
			back := math.Atan2(-float64(tt.vy), -float64(tt.vx))
			for _, p := range w.AppendParticles(nil) {
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
// queue and the air is the same every tick.
func TestBoxSplashesThePool(t *testing.T) {
	s, w, id := newSystem(t, 128, 160, 1)
	w.Fill(0, 150, 128, 160, id.stone)
	w.Fill(0, 100, 128, 150, id.water)
	drop(t, s, id.wood, 50, 20, 20, 10)
	start := cells(w)[id.water]
	high, flew := 100, 0
	for range 400 {
		tick(t, s, w)
		if got := cells(w)[id.water]; got != start {
			t.Fatalf("tick %d: %d water in the grid, queue and particles, want %d", w.Tick(), got, start)
		}
		flew = max(flew, w.Particles())
		for _, p := range w.AppendParticles(nil) {
			_, y := p.At()
			high = min(high, y)
		}
	}
	if flew == 0 {
		t.Fatal("no water flew")
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
