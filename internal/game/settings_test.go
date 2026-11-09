package game

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/tongmon/matter-wake/internal/material"
)

// newBodyGame returns a game of the test table with a body tool of wood
// and a tool of the given radius.
func newBodyGame(t *testing.T, radius int) *Game {
	t.Helper()
	g, err := New(&Config{
		Width: 128, Height: 96, Seed: 1, Workers: 2, Materials: testTable(t),
		Slots: []string{"sand", "water", "stone"}, Tool: Tool{Power: 10, Rate: 300, Radius: radius},
		Body: BodyTool{Material: "wood", Width: 8, Height: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

// A square brush covers the whole square within its radius, a round one
// the disc, for digging and placing alike.
func TestBrushShapes(t *testing.T) {
	for _, tt := range []struct {
		shape Shape
		want  int
	}{{ShapeCircle, 13}, {ShapeSquare, 25}} {
		g := newBodyGame(t, 0)
		step(t, g, &Input{Change: Change{What: ChangeBrush | ChangeMode, Radius: 2, Shape: tt.shape, Creative: true}})
		if g.Tool().Radius != 2 || g.Tool().Shape != tt.shape || !g.Creative() {
			t.Fatalf("after the change the tool is %+v, creative %t", g.Tool(), g.Creative())
		}
		sand := g.mats.MustLookup("sand")
		before := g.World().Count(sand)
		// Place in the air between the sandbox's blocks and count before
		// anything has fallen far.
		step(t, g, &Input{CursorX: 60, CursorY: 40, Place: true, Select: 1})
		n := make([]int, g.mats.Len())
		g.Made(n)
		if got := g.World().Count(sand) - before; got != tt.want || n[sand] != tt.want {
			t.Fatalf("%s brush of radius 2 placed %d sand and counted %d made, want %d", tt.shape, got, n[sand], tt.want)
		}
	}
}

// Creative mode turns on and off as the session goes. While on it places
// without the inventory and counts what it makes; off, it places from the
// inventory again.
func TestCreativeModeChanges(t *testing.T) {
	g := newBodyGame(t, 1)
	sand := g.mats.MustLookup("sand")
	start := g.World().Count(sand)
	step(t, g, &Input{CursorX: 60, CursorY: 40, Place: true, Select: 1})
	if g.World().Count(sand) != start {
		t.Fatal("play mode placed sand from an empty inventory")
	}
	step(t, g, &Input{CursorX: 60, CursorY: 40, Place: true, Change: Change{What: ChangeMode, Creative: true}})
	made := make([]int, g.mats.Len())
	g.Made(made)
	if made[sand] == 0 || made[sand] != g.World().Count(sand)-start {
		t.Fatalf("creative mode made %d sand, counted %d", g.World().Count(sand)-start, made[sand])
	}
	step(t, g, &Input{CursorX: 60, CursorY: 30, Place: true, Change: Change{What: ChangeMode}})
	again := make([]int, g.mats.Len())
	g.Made(again)
	if g.Creative() || again[sand] != made[sand] {
		t.Fatalf("back in play mode: creative %t, made %d, want %d", g.Creative(), again[sand], made[sand])
	}
}

// The body tool drops a body turned and moving as the tool says, and a
// change of the tool takes effect on its tick, before the drop.
func TestBodyToolTurnsAndThrows(t *testing.T) {
	g := newBodyGame(t, 1)
	wood := g.mats.MustLookup("wood")
	tool := BodyTool{Material: "wood", Width: 10, Height: 4, Angle: 90, VX: 50, VY: -30}
	step(t, g, &Input{CursorX: 64, CursorY: 40, Drop: true, Change: Change{What: ChangeBody, Body: tool}})
	if g.Body() != tool || g.Dropped() != 1 {
		t.Fatalf("body tool %+v, %d dropped; want %+v and 1", g.Body(), g.Dropped(), tool)
	}
	recs := g.BodyRecords(nil)
	if len(recs) != 1 || recs[0].W != 10 || recs[0].H != 4 || recs[0].Mat != wood {
		t.Fatalf("records %+v, want one 10×4 wooden body", recs)
	}
	// One Step of Box2D has run: gravity, 12 m/s², adds 0.2 m/s down.
	if r := recs[0]; r.VX != 5 || r.VY < -3 || r.VY > -2.7 || r.S < 0.999 {
		t.Fatalf("the body moves at (%v, %v) m/s turned by (%v, %v), want (5, about -2.8) turned 90°", r.VX, r.VY, r.C, r.S)
	}
	// Turned 90°, it stands 4 wide and 10 tall about its middle.
	if c, _ := g.World().At(64, 36); c.Mat != wood {
		t.Fatalf("no body cell at (64, 36): %+v", c)
	}
	if c, _ := g.World().At(59, 40); c.Mat == wood {
		t.Fatalf("a body cell at (59, 40), where only an unturned body reaches: %+v", c)
	}
}

// A change that cannot be made fails the tick and changes nothing.
func TestBadChangesFail(t *testing.T) {
	tests := []struct {
		name    string
		change  Change
		wantErr string
	}{
		{"radius over the most", Change{What: ChangeBrush, Radius: MaxRadius + 1}, "radius"},
		{"negative radius", Change{What: ChangeBrush, Radius: -1}, "radius"},
		{"unknown shape", Change{What: ChangeBrush, Radius: 1, Shape: 9}, "shape"},
		{"body of sand", Change{What: ChangeBody, Body: BodyTool{Material: "sand", Width: 1, Height: 1}}, "not a solid"},
		{"unknown body", Change{What: ChangeBody, Body: BodyTool{Material: "oak", Width: 1, Height: 1}}, "unknown"},
		{"body too big", Change{What: ChangeBody, Body: BodyTool{Material: "wood", Width: 300, Height: 300}}, "size"},
		{"body turned too far", Change{What: ChangeBody, Body: BodyTool{Material: "wood", Width: 1, Height: 1, Angle: 400}}, "angle"},
		{"body too fast", Change{What: ChangeBody, Body: BodyTool{Material: "wood", Width: 1, Height: 1, VY: MaxSpeed + 1}}, "velocity"},
		{"unknown change", Change{What: 1 << 6}, "unknown"},
		{"good brush, bad body", Change{
			What: ChangeBrush | ChangeMode | ChangeBody, Radius: 9, Creative: true, Body: BodyTool{Material: "oak"},
		}, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newBodyGame(t, 3)
			tool, body := g.Tool(), g.Body()
			err := g.Step(&Input{Change: tt.change})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Step error = %v, want one mentioning %q", err, tt.wantErr)
			}
			if g.Tool() != tool || g.Body() != body || g.Creative() {
				t.Fatalf("a failed change left the tool %+v, the body tool %+v, creative %t", g.Tool(), g.Body(), g.Creative())
			}
		})
	}
}

// No body tool, as a change can set, drops nothing.
func TestNoBodyToolDropsNothing(t *testing.T) {
	g := newBodyGame(t, 1)
	step(t, g, &Input{CursorX: 64, CursorY: 40, Drop: true, Change: Change{What: ChangeBody}})
	if g.Body() != (BodyTool{}) || g.Bodies() != 0 {
		t.Fatalf("body tool %+v and %d bodies, want none", g.Body(), g.Bodies())
	}
}

// Changing settings every tick does not allocate either.
func TestStepWithChangesDoesNotAllocate(t *testing.T) {
	g := newBodyGame(t, 2)
	in := &Input{CursorX: 30, CursorY: 30, Change: Change{
		What: ChangeBrush | ChangeMode | ChangeBody, Radius: 3, Shape: ShapeSquare, Creative: true,
		Body: BodyTool{Material: "wood", Width: 4, Height: 4, Angle: 30, VX: 20},
	}}
	if n := testing.AllocsPerRun(20, func() { step(t, g, in) }); n != 0 {
		t.Fatalf("a tick that changes settings allocates %v times", n)
	}
}

// The shipped table's materials all fit a brush slot or the body tool.
func TestEveryKindHasATool(t *testing.T) {
	tab := testTable(t)
	for id := range tab.Len() {
		switch d := tab.Def(material.ID(id)); d.Kind {
		case material.Solid:
			if _, err := bodyMaterial(BodyTool{Material: d.Name, Width: 1, Height: 1}, tab); err != nil {
				t.Errorf("%s: %v", d.Name, err)
			}
		case material.Empty, material.Powder, material.Liquid, material.Static:
		}
	}
}

// Input stays under 128 bytes, which gocritic lets a range loop copy
// (docs/pitfalls.md).
func TestInputStaysSmall(t *testing.T) {
	if n := unsafe.Sizeof(Input{}); n >= 128 {
		t.Fatalf("Input is %d bytes; keep it under 128", n)
	}
}
