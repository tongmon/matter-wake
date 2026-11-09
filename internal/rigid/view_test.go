package rigid

import (
	"math"
	"testing"

	"github.com/tongmon/matter-wake/internal/physics"
)

// A body dropped moving starts at exactly the velocity asked for, in
// cells per second, before any Step.
func TestDropStartsMoving(t *testing.T) {
	s, _, id := newSystem(t, 100, 60, 1)
	if ok, err := s.Drop(id.wood, 40, 10, 10, 4, 0, 50, -100); !ok || err != nil {
		t.Fatalf("Drop: %v, %v", ok, err)
	}
	v, spin := s.phys.Velocity(s.bodies[0].id)
	if v != (physics.Vec2{X: 5, Y: -10}) || spin != 0 {
		t.Fatalf("the body starts at %v m/s spinning %v, want (5, -10) and 0", v, spin)
	}
}

// A turned body may go where its turned rectangle is free, even if the
// unturned one would not be, and not where the turned one meets terrain.
func TestDropChecksTheTurnedBody(t *testing.T) {
	s, w, id := newSystem(t, 100, 60, 1)
	// A pillar of stone across where a long body would lie flat.
	w.Fill(62, 20, 64, 40, id.stone)
	quarter := float32(math.Pi / 2)
	if ok, _ := s.Drop(id.wood, 40, 28, 30, 4, 0, 0, 0); ok {
		t.Fatal("a flat body over the pillar fit")
	}
	// Standing up about the same middle, it is 4 wide and clear of it.
	if ok, err := s.Drop(id.wood, 40, 28, 30, 4, quarter, 0, 0); !ok || err != nil {
		t.Fatalf("the standing body did not fit: %v, %v", ok, err)
	}
	s.Write()
	// Turned 45°, a body whose corners reach the pillar does not fit.
	if ok, _ := s.Drop(id.wood, 48, 45, 12, 12, quarter/2, 0, 0); ok {
		t.Fatal("a turned body reaching the pillar fit")
	}
	// Unturned in the same place, its corner stays clear of the floor.
	w.Fill(0, 58, 100, 60, id.stone)
	if ok, err := s.Drop(id.wood, 70, 46, 12, 12, 0, 0, 0); !ok || err != nil {
		t.Fatalf("an unturned body above the floor did not fit: %v, %v", ok, err)
	}
	if ok, _ := s.Drop(id.wood, 84, 46, 12, 12, quarter/2, 0, 0); ok {
		t.Fatal("a body turned 45° into the floor fit")
	}
}

// The drawn edges are the bodies' shapes and the terrain's chains, in
// cells.
func TestEdges(t *testing.T) {
	s, w, id := newSystem(t, 200, 60, 1)
	w.Fill(0, 50, 200, 60, id.stone)
	drop(t, s, id.wood, 40, 30, 10, 4)
	tick(t, s, w)
	body, frozen := s.BodyEdges(nil, nil)
	if len(body) != 4 || len(frozen) != 0 {
		t.Fatalf("a box has %d edges and %d frozen, want 4 and 0", len(body), len(frozen))
	}
	for _, e := range body {
		for _, p := range [][2]float32{{e.X0, e.Y0}, {e.X1, e.Y1}} {
			if p[0] < 39 || p[0] > 51 || p[1] < 29 || p[1] > 35.5 {
				t.Fatalf("an edge of the box reaches %v, outside (40, 30)-(50, 34) and its first tick's fall", p)
			}
		}
	}
	chains := s.TerrainEdges(0, 0, 200, 60, nil)
	if len(chains) == 0 {
		t.Fatal("no terrain edges near the body")
	}
	for _, e := range chains {
		if e.Y0 != 50 || e.Y1 != 50 {
			t.Fatalf("a terrain edge %+v is not on the floor's top, y = 50", e)
		}
	}
	// Only the chunk the body is in is collision.
	if got := s.TerrainEdges(130, 0, 200, 60, nil); len(got) != 0 {
		t.Fatalf("%d terrain edges in a chunk no body is near", len(got))
	}
}
