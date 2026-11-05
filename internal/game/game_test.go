package game

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
)

func newTestGame(t *testing.T) *Game {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := material.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(Config{
		Width: 128, Height: 96, Seed: 1, Workers: 2, Materials: tab,
		Slots: []string{"sand", "water", "stone"}, BrushRadius: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

func TestSelectAndPlace(t *testing.T) {
	g := newTestGame(t)
	g.Step(Input{Select: 3})
	if got := g.Selected().Name; got != "stone" {
		t.Fatalf("slot 3 selected %q, want stone", got)
	}
	g.Step(Input{Select: 9})
	if got := g.Selected().Name; got != "stone" {
		t.Fatalf("an out-of-range slot changed the selection to %q", got)
	}
	stone := g.mats.MustLookup("stone")
	before := g.world.Count(stone)
	g.Step(Input{CursorX: 64, CursorY: 40, Place: true})
	if g.world.Count(stone) <= before {
		t.Fatal("placing stone added no stone")
	}
}

func TestDigClearsUnderTheCursor(t *testing.T) {
	g := newTestGame(t)
	// The sandbox floor is stone; dig into it.
	x, y := 64, g.world.Height()-1
	g.Step(Input{CursorX: x, CursorY: y, Dig: true})
	if c, _ := g.world.At(x, y); c.Mat != material.Air {
		t.Fatalf("cell under the cursor is %d after digging, want air", c.Mat)
	}
}

func TestNewRejectsUnknownSlot(t *testing.T) {
	tab, err := material.NewTable([]material.Def{{Name: "air", Kind: material.Empty}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Width: 8, Height: 8, Materials: tab, Slots: []string{"gold"}}); err == nil {
		t.Fatal("New accepted a slot naming an unknown material")
	}
}
