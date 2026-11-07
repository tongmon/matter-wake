package rigid

import (
	"math"
	"testing"

	"github.com/tongmon/matter-wake/internal/physics"
)

// Buoyancy is Noita's force exactly: the first frame it held up a wood
// piece of mass 3.072 kg falling at 5.6 m/s into water, with buoyancy 1,
// it pushed up with 54.0672 N and no sideways force (C41).
func TestBuoyancyMatchesNoita(t *testing.T) {
	f := buoyancy(1, 3.0719998, physics.Vec2{X: 0, Y: 5.6000003})
	if f.X != 0 || math.Abs(float64(f.Y)+54.06719787457455) > 1e-4 {
		t.Fatalf("buoyancy pushes %v, want (0, -54.0672)", f)
	}
	f = buoyancy(0.7, 2, physics.Vec2{X: 1, Y: -12})
	if math.Abs(float64(f.X)+1.4) > 1e-6 || f.Y != 0 {
		t.Fatalf("buoyancy 0.7 of a 2 kg body rising at 12 m/s pushes %v, want (-1.4, 0)", f)
	}
}

// The drag of one liquid cell is Noita's exactly: in the first frame a
// spinning wood piece entered water, the cell at (2.85, 1.01) m from its
// center of mass pushed it with (-0.6612, -6.8901) N and a torque of
// -18.969 N m (C42). The drag of several cells is the sum of each.
func TestLiquidDragMatchesNoita(t *testing.T) {
	v, w, m := physics.Vec2{X: 1.5, Y: 5.600000187754631}, float32(0.75), float32(3.0719998282589662)
	rx := float32(31.74999887594767 - 28.899999016247637)
	ry := float32(-28.100000885892506 - -29.11000088918826)
	f, torque := liquidDrag(v, w, m, 1, rx, ry, float32(rx*rx)+float32(ry*ry))
	near := func(got float32, want float64) bool {
		return math.Abs(float64(got)-want) <= 1e-5*math.Max(1, math.Abs(want))
	}
	if !near(f.X, -0.6611833738588991) || !near(f.Y, -6.890109665766398) || !near(torque, -18.9690163709755) {
		t.Fatalf("drag of one cell is %v and torque %v, want (-0.6612, -6.8901) and -18.969", f, torque)
	}
	// Two cells, each on its own and summed.
	f1, t1 := liquidDrag(v, w, m, 1, rx, ry, float32(rx*rx)+float32(ry*ry))
	f2, t2 := liquidDrag(v, w, m, 1, -0.4, 0.3, 0.25)
	f12, t12 := liquidDrag(v, w, m, 2, rx-0.4, ry+0.3, float32(rx*rx)+float32(ry*ry)+0.25)
	if !near(f12.X, float64(f1.X+f2.X)) || !near(f12.Y, float64(f1.Y+f2.Y)) || !near(t12, float64(t1+t2)) {
		t.Fatalf("two cells drag %v %v together, %v %v and %v %v apart", f12, t12, f1, t1, f2, t2)
	}
	// A body that only spins feels no drag: the speed of its center of
	// mass scales the force (C42).
	if f, torque := liquidDrag(physics.Vec2{}, 5, m, 3, 1, -2, 7); f != (physics.Vec2{}) || torque != 0 {
		t.Fatalf("a body spinning in place feels %v and torque %v", f, torque)
	}
}

// dropInPool drops a 16×12 wood box of buoyancy b into deep water and
// returns its center's depth below the surface, in cells, and its downward
// speed in m/s, every tick.
func dropInPool(t *testing.T, b float64, ticks int) (depth, speed []float64) {
	t.Helper()
	s, w, id := newSystemBuoyancy(t, 128, 256, 1, b)
	w.Fill(0, 250, 128, 256, id.stone)
	w.Fill(0, 80, 128, 250, id.water)
	drop(t, s, id.wood, 56, 40, 16, 12)
	for range ticks {
		tick(t, s, w)
		p, _ := s.phys.Transform(s.bodies[0].id)
		v, _ := s.phys.Velocity(s.bodies[0].id)
		depth = append(depth, float64(p.Y)*cellsPerMeter-80)
		speed = append(speed, float64(v.Y))
	}
	return depth, speed
}

// In water, a wood box of buoyancy 0.7 (Noita's, ✅ 2026-11-08) keeps
// sinking, slower than one without buoyancy; one of buoyancy 1 comes to a
// stop under the surface and stays there.
func TestWoodSinksSlowlyInWater(t *testing.T) {
	const ticks = 600
	d0, v0 := dropInPool(t, 0, ticks)
	d7, v7 := dropInPool(t, 0.7, ticks)
	d1, v1 := dropInPool(t, 1, ticks)
	for _, k := range []int{120, 180, 240} {
		t.Logf("tick %d: depth %.1f / %.1f / %.1f cells, speed %.2f / %.2f / %.2f m/s (buoyancy 0 / 0.7 / 1)", k, d0[k], d7[k], d1[k], v0[k], v7[k], v1[k])
	}
	if !(d7[180] < d0[180]) || !(v7[180] < v0[180]) {
		t.Errorf("at tick 180 buoyancy 0.7 is %.1f cells down at %.2f m/s, buoyancy 0 %.1f at %.2f: want slower", d7[180], v7[180], d0[180], v0[180])
	}
	if !(d7[240] > d7[180]+1) {
		t.Errorf("buoyancy 0.7 stopped sinking: %.1f cells down at tick 180, %.1f at 240", d7[180], d7[240])
	}
	if d1[ticks-1] < 0 || math.Abs(d1[ticks-1]-d1[ticks-121]) > 0.5 {
		t.Errorf("buoyancy 1 is %.1f cells down two seconds before the end and %.1f at it: want still, under the surface", d1[ticks-121], d1[ticks-1])
	}
}

// A body spinning as it sinks through water loses its spin to the drag of
// the water it pushes away; in air it keeps it.
func TestSpinSlowsInWater(t *testing.T) {
	spinAfter := func(water bool) float32 {
		s, w, id := newSystemBuoyancy(t, 128, 400, 1, 0.7)
		w.Fill(0, 390, 128, 400, id.stone)
		if water {
			w.Fill(0, 60, 128, 390, id.water)
		}
		drop(t, s, id.wood, 56, 100, 16, 12)
		b := &s.bodies[0]
		for range 5 {
			s.Write()
			collide(t, s)
			if err := s.phys.Push(b.id, physics.Vec2{}, 40, true); err != nil {
				t.Fatal(err)
			}
			w.Step()
			if err := s.Step(); err != nil {
				t.Fatal(err)
			}
		}
		for range 60 {
			tick(t, s, w)
		}
		_, spin := s.phys.Velocity(b.id)
		return spin
	}
	air, water := spinAfter(false), spinAfter(true)
	t.Logf("spin after a second: %.2f rad/s in air, %.2f in water", air, water)
	if math.Abs(float64(water)) > 0.5*math.Abs(float64(air)) {
		t.Fatalf("spinning through water left %v rad/s of the %v it keeps in air", water, air)
	}
}
