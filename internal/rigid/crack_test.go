package rigid

import (
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
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

// A crack runs from the contact point into the body, at most 45° from the
// way in, and takes out at most crackSteps pixels, the ones on its way. On
// average over many cracks into a wide body it takes about as many as
// Noita's did, 3.9 a crack (C138 3.4).
func TestCrackRunsIntoTheBody(t *testing.T) {
	s, w, _ := newSystem(t, 300, 120, 1)
	c := coldIDs(s)
	// Within the window around the cell dug in the middle of its top.
	w.Fill(30, 40, 270, 80, c.ice)
	digCell(t, s, 150, 40)
	convert(t, s)
	b := &s.bodies[0]
	total, cracks := 0, 0
	for x := 33; x < 267; x += 6 {
		before := b.n
		// The top of the slab, at y = 40, into it: down.
		s.runCrack(&crackCheck{key: b.key, slot: 0, x: float32(x) + 0.5, y: 40, dx: 0, dy: 1, speed: 200},
			rng.Hash3(9, uint64(x), 0, 0))
		took := before - b.n
		if took < 1 || took > crackSteps {
			t.Fatalf("a crack at x = %d took %d pixels, want 1 to %d", x, took, crackSteps)
		}
		total += took
		cracks++
	}
	// Every pixel taken lies within crackSteps rows of the top and within a
	// cone of 45° about the way in.
	for j := range b.h {
		for i := range b.w {
			if b.px[j*b.w+i].ok {
				continue
			}
			x := 30 + i
			if x != 150 && (j >= crackSteps || x < 29 || x >= 271) {
				t.Fatalf("pixel (%d, %d) was taken, outside where the cracks ran", i, j)
			}
		}
	}
	t.Logf("%d cracks took %d pixels, %.2f each", cracks, total, float64(total)/float64(cracks))
	if mean := float64(total) / float64(cracks); mean < 3 || mean > crackSteps {
		t.Fatalf("a crack took %.1f pixels on average, want 3 to %d", mean, crackSteps)
	}
	if n := lostBy(s)[c.iceBody]; n != total {
		t.Fatalf("%d pixels counted lost, %d taken", n, total)
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
func TestFastHitCracks(t *testing.T) {
	s, w, id := newSystem(t, 200, 400, 1)
	c := coldIDs(s)
	w.Fill(0, 390, 200, 400, id.stone)
	throw(t, s, c.iceBody, 90, 100, 16, 8, 1500)
	play(t, s, w, 60)
	if lostBy(s)[c.iceBody] == 0 {
		t.Fatal("ice that hit the floor at 1500 cells per second did not crack")
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
