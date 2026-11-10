package physics

import (
	"math"
	"testing"
)

// lShape is an L of two rectangles in a body's frame: 2 m × 1 m along the
// top and 1 m × 1 m under its left end. Its area is 3 m², and its
// centroid is at (5/6, 5/6).
func lShape() Polygons {
	return Polygons{
		Points: []Vec2{{0, 0}, {2, 0}, {2, 1}, {0, 1}, {0, 1}, {1, 1}, {1, 2}, {0, 2}},
		Ends:   []int{4, 8},
	}
}

func near(a, b, tol float32) bool {
	return math.Abs(float64(a-b)) <= float64(tol)
}

// SetShapes replaces a body's shapes and gives it the mass, center of mass
// and inertia of the new ones.
func TestSetShapesSetsTheMass(t *testing.T) {
	w := newTestWorld(t)
	b, err := w.AddDynamicBox(Box{Center: Vec2{3, 4}, HalfW: 1, HalfH: 0.5, Density: 2})
	if err != nil {
		t.Fatal(err)
	}
	n, err := w.SetShapes(b, lShape(), Surface{Density: 2, Friction: 0.5})
	if err != nil || n != 2 {
		t.Fatalf("SetShapes made %d shapes, %v; want 2", n, err)
	}
	if m := w.Mass(b); !near(m, 6, 1e-5) {
		t.Fatalf("an L of 3 m² at density 2 weighs %v, want 6", m)
	}
	if c := w.LocalCenter(b); !near(c.X, 5.0/6, 1e-5) || !near(c.Y, 5.0/6, 1e-5) {
		t.Fatalf("the L's center of mass is %v, want (5/6, 5/6)", c)
	}
	// About its centroid: each rectangle's own inertia plus its mass times
	// its squared distance from the L's centroid.
	want := float32(4*(4+1)/12.0 + 4*(1.0/36+1.0/9) + 2*(1+1)/12.0 + 2*(1.0/9+4.0/9))
	if got := w.Inertia(b); !near(got, want, 1e-4) {
		t.Fatalf("the L's inertia is %v, want %v", got, want)
	}
	if p, _ := w.Transform(b); p != (Vec2{3, 4}) {
		t.Fatalf("SetShapes moved the body's origin to %v", p)
	}
}

// When its shapes change, a moving body keeps the velocity of the points
// of its frame: its center of mass moves by Δc, so the center's velocity
// becomes v + ω × Δc (C35). A sleeping body is woken.
func TestSetShapesKeepsTheFramesMotion(t *testing.T) {
	w := newTestWorld(t)
	b, err := w.AddDynamicBox(Box{Center: Vec2{0, 0}, HalfW: 1, HalfH: 1, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	w.SetVelocity(b, Vec2{3, -1}, 2)
	// One unit square in the top-right quarter: Δc = (0.5, 0.5).
	sq := Polygons{Points: []Vec2{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, Ends: []int{4}}
	if n, err := w.SetShapes(b, sq, Surface{Density: 1}); err != nil || n != 1 {
		t.Fatalf("SetShapes made %d shapes, %v", n, err)
	}
	v, spin := w.Velocity(b)
	if !near(v.X, 3-2*0.5, 1e-6) || !near(v.Y, -1+2*0.5, 1e-6) || spin != 2 {
		t.Fatalf("after the shapes changed the velocity is %v spinning %v, want (2, 0) spinning 2", v, spin)
	}
	if !w.Awake(b) {
		t.Fatal("SetShapes left the body asleep")
	}
}

// A body made by AddDynamicBody starts where it is told, turned as told,
// awake, and moves once it has shapes and a velocity.
func TestAddDynamicBody(t *testing.T) {
	w := newTestWorld(t)
	q := Rot{C: 0.6, S: 0.8}
	b, err := w.AddDynamicBody(Vec2{1, 2}, q)
	if err != nil {
		t.Fatal(err)
	}
	if p, r := w.Transform(b); p != (Vec2{1, 2}) || r != q {
		t.Fatalf("the body starts at %v turned %v, want (1, 2) turned %v", p, r, q)
	}
	if n, err := w.SetShapes(b, lShape(), Surface{Density: 1}); err != nil || n != 2 {
		t.Fatalf("SetShapes made %d shapes, %v", n, err)
	}
	w.SetVelocity(b, Vec2{5, 0}, -1)
	if v, spin := w.Velocity(b); v != (Vec2{5, 0}) || spin != -1 {
		t.Fatalf("SetVelocity gave %v spinning %v", v, spin)
	}
	if !w.Awake(b) {
		t.Fatal("a new body is asleep")
	}
	if _, err := w.AddDynamicBody(Vec2{0, 0}, Rot{C: 2, S: 0}); err == nil {
		t.Error("a rotation of length 2 was accepted")
	}
}

// A body made by AddStaticBody stays where it is told with the shapes it
// is given, and a dynamic body falling on it comes to rest on them. It has
// no mass and no velocity, and SetVelocity does not move it.
func TestAddStaticBody(t *testing.T) {
	w := newTestWorld(t)
	q := Rot{C: 1}
	s, err := w.AddStaticBody(Vec2{0, 5}, q)
	if err != nil {
		t.Fatal(err)
	}
	// A floor 4 m wide and 1 m thick, its top at y = 5: the test world's
	// gravity points to -y.
	floor := Polygons{Points: []Vec2{{-2, -1}, {2, -1}, {2, 0}, {-2, 0}}, Ends: []int{4}}
	if n, err := w.SetShapes(s, floor, Surface{Density: 1, Friction: 0.6}); err != nil || n != 1 {
		t.Fatalf("SetShapes made %d shapes, %v", n, err)
	}
	if m := w.Mass(s); m != 0 {
		t.Fatalf("a static body weighs %v", m)
	}
	w.SetVelocity(s, Vec2{1, 0}, 1)
	b, err := w.AddDynamicBox(Box{Center: Vec2{0, 7}, HalfW: 0.5, HalfH: 0.5, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	for range 120 {
		w.Step(1.0/60, 4)
	}
	if p, r := w.Transform(s); p != (Vec2{0, 5}) || r != q {
		t.Fatalf("the static body moved to %v turned %v", p, r)
	}
	if v, spin := w.Velocity(s); v != (Vec2{}) || spin != 0 {
		t.Fatalf("the static body moves at %v spinning %v", v, spin)
	}
	if p := w.Position(b); !near(p.Y, 5.5, 0.02) {
		t.Fatalf("the box rests at y = %v, want 5.5 on the static floor", p.Y)
	}
}

// Polygons whose corners Box2D cannot make a hull of, all on a line or
// within its weld distance, are skipped; SetShapes reports how many it
// made. Polygons of too many or too few corners are an error.
func TestSetShapesSkipsDegeneratePolygons(t *testing.T) {
	w := newTestWorld(t)
	b, err := w.AddDynamicBody(Vec2{0, 0}, Rot{C: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := Polygons{
		Points: []Vec2{{0, 0}, {1, 0}, {2, 0}, {0, 0}, {1, 0}, {1, 1}, {0, 0}, {0.001, 0}, {0, 0.001}},
		Ends:   []int{3, 6, 9},
	}
	if n, err := w.SetShapes(b, p, Surface{Density: 1}); err != nil || n != 1 {
		t.Fatalf("SetShapes made %d shapes, %v; want only the triangle", n, err)
	}
	if m := w.Mass(b); !near(m, 0.5, 1e-6) {
		t.Fatalf("the body weighs %v, want the triangle's 0.5", m)
	}
	for _, bad := range []Polygons{
		{Points: make([]Vec2, 9), Ends: []int{9}},
		{Points: make([]Vec2, 2), Ends: []int{2}},
		{Points: []Vec2{{0, 0}, {1, 0}, {float32(math.NaN()), 1}}, Ends: []int{3}},
		{Points: make([]Vec2, 4), Ends: []int{3, 2}},
	} {
		if _, err := w.SetShapes(b, bad, Surface{Density: 1}); err == nil {
			t.Errorf("SetShapes accepted %+v", bad)
		}
	}
	if _, err := w.SetShapes(b, lShape(), Surface{Density: -1}); err == nil {
		t.Error("SetShapes accepted a negative density")
	}
}

// SetShapes allocates nothing once its buffers have grown.
func TestSetShapesDoesNotAllocate(t *testing.T) {
	w := newTestWorld(t)
	b, err := w.AddDynamicBody(Vec2{0, 0}, Rot{C: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := lShape()
	if n := testing.AllocsPerRun(50, func() { _, _ = w.SetShapes(b, p, Surface{Density: 1}) }); n != 0 {
		t.Fatalf("SetShapes allocates %v times", n)
	}
}

// SetBox makes the box AddDynamicBox makes, to the bit: the same mass,
// inertia and center of mass, so a box body taken out of the world can
// be made again as it was.
func TestSetBoxMakesTheSameBox(t *testing.T) {
	w := newTestWorld(t)
	s := Surface{Density: 0.6, Friction: 0.9, Restitution: 0.2}
	box, err := w.AddDynamicBox(Box{Center: Vec2{3, 4}, HalfW: 0.8, HalfH: 0.6, Density: s.Density, Friction: s.Friction, Restitution: s.Restitution})
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.AddDynamicBody(Vec2{30, 4}, Rot{C: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetBox(again, 0.8, 0.6, s); err != nil {
		t.Fatal(err)
	}
	if w.Mass(again) != w.Mass(box) || w.Inertia(again) != w.Inertia(box) || w.LocalCenter(again) != w.LocalCenter(box) {
		t.Fatalf("SetBox made mass %v, inertia %v, center %v; AddDynamicBox %v, %v, %v",
			w.Mass(again), w.Inertia(again), w.LocalCenter(again), w.Mass(box), w.Inertia(box), w.LocalCenter(box))
	}
	if err := w.SetBox(again, 0, 1, s); err == nil {
		t.Fatal("SetBox took a box of no width")
	}
}

// A disabled body stays where it is through Steps; enabled again, it is
// awake and moves. A body put to sleep stays asleep.
func TestDisableAndSleep(t *testing.T) {
	w := newTestWorld(t)
	b, err := w.AddDynamicBox(Box{Center: Vec2{0, 10}, HalfW: 0.5, HalfH: 0.5, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	w.Disable(b)
	p := w.Position(b)
	for range 30 {
		w.Step(dt, subSteps)
	}
	if w.Position(b) != p || w.Awake(b) {
		t.Fatalf("the disabled body went from %v to %v, awake %v", p, w.Position(b), w.Awake(b))
	}
	w.Enable(b)
	if !w.Awake(b) {
		t.Fatal("the enabled body is not awake")
	}
	w.Step(dt, subSteps)
	if w.Position(b) == p {
		t.Fatal("the enabled body did not move")
	}
	w.Sleep(b)
	p = w.Position(b)
	w.Step(dt, subSteps)
	if w.Awake(b) || w.Position(b) != p {
		t.Fatalf("the sleeping body woke or moved: awake %v at %v", w.Awake(b), w.Position(b))
	}
}
