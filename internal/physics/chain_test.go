package physics

import (
	"math"
	"testing"
)

// newDownWorld returns a world whose gravity points down the grid's way,
// along +y, as the game's does.
func newDownWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld(Config{Gravity: Vec2{0, 10}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w
}

// floor returns an open chain along y = 0 from x = -10 to 10, running the
// way that keeps bodies above it, at smaller y.
func floor() Chain {
	return Chain{
		Points: []Vec2{{-10, 0}, {10, 0}},
		Before: Vec2{-11, 0},
		After:  Vec2{11, 0},
	}
}

// A chain holds up a box that falls on it from the side it faces, and a
// box below it falls on: chains collide on one side only.
func TestChainCollidesOnOneSide(t *testing.T) {
	w := newDownWorld(t)
	if _, err := w.AddStaticChains([]Chain{floor()}, 0.6); err != nil {
		t.Fatal(err)
	}
	above, err := w.AddDynamicBox(Box{Center: Vec2{0, -2}, HalfW: 0.5, HalfH: 0.5, Density: 1, Friction: 0.6})
	if err != nil {
		t.Fatal(err)
	}
	below, err := w.AddDynamicBox(Box{Center: Vec2{3, 0.6}, HalfW: 0.5, HalfH: 0.5, Density: 1, Friction: 0.6})
	if err != nil {
		t.Fatal(err)
	}
	for range 120 {
		w.Step(dt, subSteps)
	}
	if y := w.Position(above).Y; math.Abs(float64(y+0.5)) > 0.02 {
		t.Errorf("the box dropped on the floor rests at y = %v, want its bottom on y = 0 (-0.5)", y)
	}
	if y := w.Position(below).Y; y < 5 {
		t.Errorf("the box under the floor is at y = %v; it should have fallen on past the chain's back", y)
	}
}

// A loop chain wound clockwise on screen keeps bodies out of what it
// encloses.
func TestLoopChainIsSolid(t *testing.T) {
	w := newDownWorld(t)
	// A 4 m square from (-2, 0) to (2, 4): top edge left to right, then
	// down the right side, back along the bottom and up the left.
	loop := Chain{Points: []Vec2{{-2, 0}, {2, 0}, {2, 4}, {-2, 4}}, Loop: true}
	if _, err := w.AddStaticChains([]Chain{loop}, 0.6); err != nil {
		t.Fatal(err)
	}
	b, err := w.AddDynamicBox(Box{Center: Vec2{0, -3}, HalfW: 0.5, HalfH: 0.5, Density: 1, Friction: 0.6})
	if err != nil {
		t.Fatal(err)
	}
	for range 120 {
		w.Step(dt, subSteps)
	}
	if y := w.Position(b).Y; math.Abs(float64(y+0.5)) > 0.02 {
		t.Fatalf("the box rests at y = %v, want on top of the square (-0.5)", y)
	}
}

// Two open chains that meet carry a box sliding from one to the other
// without catching on the joint, when each names the other's next point.
func TestChainsJoinSmoothly(t *testing.T) {
	w := newDownWorld(t)
	left := Chain{Points: []Vec2{{-10, 0}, {0, 0}}, Before: Vec2{-11, 0}, After: Vec2{1, 0}}
	right := Chain{Points: []Vec2{{0, 0}, {10, 0}}, Before: Vec2{-1, 0}, After: Vec2{11, 0}}
	if _, err := w.AddStaticChains([]Chain{left}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddStaticChains([]Chain{right}, 0); err != nil {
		t.Fatal(err)
	}
	b, err := w.AddDynamicBox(Box{Center: Vec2{-5, -0.5}, HalfW: 0.5, HalfH: 0.5, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		w.Step(dt, subSteps)
	}
	w.setVelocity(b, Vec2{5, 0})
	for range 120 {
		w.Step(dt, subSteps)
		if v, _ := w.Velocity(b); w.Position(b).X < 3 && v.X < 4.9 {
			t.Fatalf("the box slowed to %v m/s at x = %v: it caught on the joint", v.X, w.Position(b).X)
		}
	}
	if x := w.Position(b).X; x < 3 {
		t.Fatalf("the box got to x = %v, want past the joint at 0", x)
	}
}

// Transform reads what Position and Rotation read, and Velocity follows
// gravity in free fall.
func TestTransformAndVelocity(t *testing.T) {
	w := newDownWorld(t)
	b, err := w.AddDynamicBox(Box{Center: Vec2{1, 2}, Angle: 0.5, HalfW: 0.5, HalfH: 0.25, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		w.Step(dt, subSteps)
	}
	p, q := w.Transform(b)
	if p != w.Position(b) || q != w.Rotation(b) {
		t.Fatalf("Transform is %v %v, Position and Rotation %v %v", p, q, w.Position(b), w.Rotation(b))
	}
	v, spin := w.Velocity(b)
	if math.Abs(float64(v.Y-5)) > 1e-3 || v.X != 0 || spin != 0 {
		t.Fatalf("after half a second of free fall the velocity is %v spinning %v, want (0, 5) and 0", v, spin)
	}
}

// A destroyed body leaves the world.
func TestDestroyBody(t *testing.T) {
	w := newDownWorld(t)
	b, err := w.AddDynamicBox(Box{HalfW: 1, HalfH: 1, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	w.DestroyBody(b)
	if n := w.awakeBodies(); n != 0 {
		t.Fatalf("%d bodies awake after destroying the only one", n)
	}
}

// Bad chains are errors, not Box2D's silent refusals.
func TestStaticChainsRefuseBadInput(t *testing.T) {
	w := newDownWorld(t)
	inf := float32(math.Inf(1))
	tests := []struct {
		name     string
		chains   []Chain
		friction float32
	}{
		{"one point", []Chain{{Points: []Vec2{{0, 0}}}}, 0.6},
		{"loop of two", []Chain{{Points: []Vec2{{0, 0}, {1, 0}}, Loop: true}}, 0.6},
		{"infinite point", []Chain{{Points: []Vec2{{0, 0}, {inf, 0}}}}, 0.6},
		{"infinite ghost", []Chain{{Points: []Vec2{{0, 0}, {1, 0}}, After: Vec2{inf, 0}}}, 0.6},
		{"negative friction", []Chain{floor()}, -1},
	}
	for _, tt := range tests {
		if _, err := w.AddStaticChains(tt.chains, tt.friction); err == nil {
			t.Errorf("%s: no error", tt.name)
		}
	}
}
