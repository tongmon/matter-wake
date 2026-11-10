package physics

import (
	"testing"
)

// hitWorld returns a world with gravity down along -y and the given hit
// speed, a static ground whose top is at y = 0, and a 1 m box above it
// whose shape reports hits if hits is set, moving down at speed.
func hitWorld(t *testing.T, hitSpeed, speed float32, hits bool) (*World, Body, Body) {
	t.Helper()
	w, err := NewWorld(Config{Gravity: Vec2{0, -10}, HitSpeed: hitSpeed})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	ground, err := w.AddStaticBox(Box{Center: Vec2{0, -1}, HalfW: 10, HalfH: 1, Friction: 0.6})
	if err != nil {
		t.Fatal(err)
	}
	box, err := w.AddDynamicBox(Box{Center: Vec2{0, 3}, HalfW: 0.5, HalfH: 0.5, Density: 1, Friction: 0.6, Hits: hits})
	if err != nil {
		t.Fatal(err)
	}
	w.SetVelocity(box, Vec2{0, -speed}, 0)
	return w, ground, box
}

// firstHit steps w until a hit, for at most two seconds, and returns it
// with the step it came in.
func firstHit(t *testing.T, w *World) (Hit, int) {
	t.Helper()
	var hits []Hit
	for step := range 120 {
		w.Step(1.0/60, 4)
		if hits = w.Hits(hits[:0]); len(hits) > 0 {
			return hits[0], step
		}
	}
	return Hit{}, -1
}

// A box that lands on the ground reports one hit, between the two of them,
// at the speed it came down with, the point under it and the normal along
// the line between them.
func TestHitsReportTheApproachSpeed(t *testing.T) {
	w, ground, box := hitWorld(t, 1, 5, true)
	h, step := firstHit(t, w)
	if step < 0 {
		t.Fatal("no hit")
	}
	if between := h.A == box && h.B == ground || h.A == ground && h.B == box; !between {
		t.Fatalf("the hit is between %#x and %#x, want the box and the ground", h.A, h.B)
	}
	// It falls 2.5 m from 5 m/s under 10 m/s²: about 8.7 m/s on landing.
	if h.Speed < 8 || h.Speed > 9.5 {
		t.Fatalf("the box hit at %v m/s, want about 8.7", h.Speed)
	}
	if h.Point.Y < -0.05 || h.Point.Y > 0.05 || h.Point.X < -0.6 || h.Point.X > 0.6 {
		t.Fatalf("the hit is at %v, want under the box on the ground's top", h.Point)
	}
	if h.Normal.X != 0 || (h.Normal.Y != 1 && h.Normal.Y != -1) {
		t.Fatalf("the normal is %v, want vertical", h.Normal)
	}
}

// A box so fast it crosses the space to the ground in a fraction of a step
// still reports its hit, at about its speed: Box2D stops it at the ground
// with continuous collision, and the next step's contact sees it come.
// Noita's 910 px/s bounce made no crack check (C138); the developer chose
// that faster hits crack too (2026-11-10).
func TestFastHitIsReported(t *testing.T) {
	for _, speed := range []float32{91, 200} {
		w, _, _ := hitWorld(t, 1, speed, true)
		h, step := firstHit(t, w)
		if step < 0 {
			t.Fatalf("a box at %v m/s reported no hit", speed)
		}
		if h.Speed < speed*0.9 {
			t.Fatalf("a box at %v m/s hit at %v m/s", speed, h.Speed)
		}
	}
}

// Contacts of shapes that do not report hits, and hits slower than the
// world's hit speed, are not listed.
func TestHitsNeedAShapeThatReportsAndSpeed(t *testing.T) {
	w, _, _ := hitWorld(t, 1, 5, false)
	if _, step := firstHit(t, w); step >= 0 {
		t.Fatal("a box whose shape does not report hits reported one")
	}
	w, _, _ = hitWorld(t, 20, 5, true)
	if h, step := firstHit(t, w); step >= 0 {
		t.Fatalf("a hit at %v m/s under a hit speed of 20 was listed", h.Speed)
	}
}

// Hits allocates nothing once its buffers have grown.
func TestHitsDoesNotAllocate(t *testing.T) {
	w, _, _ := hitWorld(t, 1, 5, true)
	if _, step := firstHit(t, w); step < 0 {
		t.Fatal("no hit")
	}
	out := make([]Hit, 0, 8)
	if n := testing.AllocsPerRun(10, func() { out = w.Hits(out[:0]) }); n != 0 {
		t.Fatalf("Hits allocates %v times", n)
	}
}
