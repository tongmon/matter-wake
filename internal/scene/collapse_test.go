package scene

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

func newCollapse(t *testing.T, seed uint64) (*world.World, *material.Table) {
	t.Helper()
	tab := loadTable(t)
	w, err := world.New(world.Config{Width: 480, Height: 270, Seed: seed, Workers: 4, Materials: tab})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	if err := Collapse(w, tab, seed); err != nil {
		t.Fatal(err)
	}
	return w, tab
}

// TestCollapseGolden pins how the collapse scene falls. After an intended
// change to the simulation rules, rerun with -update and review the diff
// of testdata/collapse.golden in the PR.
func TestCollapseGolden(t *testing.T) {
	w, tab := newCollapse(t, 1)
	var b strings.Builder
	for _, tick := range []uint32{0, 1, 10, 60, 300} {
		for w.Tick() < tick {
			w.Step()
		}
		fmt.Fprintf(&b, "tick %d sand %d water %d checksum %016x\n",
			tick, w.Count(tab.MustLookup("sand")), w.Count(tab.MustLookup("water")), w.Checksum())
	}
	compareGolden(t, filepath.Join("testdata", "collapse.golden"), b.String())
}

// The room has stone walls and a floor, and everything inside them is
// sand, water or air in about the shares of collapseSand and
// collapseWater.
func TestCollapseFillsTheRoom(t *testing.T) {
	w, tab := newCollapse(t, 1)
	stone, sand, water := tab.MustLookup("stone"), tab.MustLookup("sand"), tab.MustLookup("water")
	wall := max(w.Width()/120, 2)
	counts := map[material.ID]int{}
	for y := range w.Height() {
		for x := range w.Width() {
			c, _ := w.At(x, y)
			inside := x >= wall && x < w.Width()-wall && y < w.Height()-wall
			if !inside {
				if c.Mat != stone {
					t.Fatalf("(%d, %d) is %d, want stone: it is wall or floor", x, y, c.Mat)
				}
				continue
			}
			counts[c.Mat]++
		}
	}
	room := (w.Width() - 2*wall) * (w.Height() - wall)
	for _, tt := range []struct {
		name string
		id   material.ID
		pct  int
	}{
		{"sand", sand, collapseSand},
		{"water", water, collapseWater},
		{"air", material.Air, 100 - collapseSand - collapseWater},
	} {
		if got, want := counts[tt.id]*100, tt.pct*room; got < want-room || got > want+room {
			t.Errorf("%s fills %d of %d cells, want about %d%%", tt.name, counts[tt.id], room, tt.pct)
		}
	}
	if n := counts[stone]; n != 0 {
		t.Errorf("%d stone cells inside the room, want none", n)
	}
}

func TestCollapseDependsOnTheSeed(t *testing.T) {
	a, _ := newCollapse(t, 1)
	b, _ := newCollapse(t, 2)
	if a.Checksum() == b.Checksum() {
		t.Fatal("seeds 1 and 2 made the same room")
	}
}

func TestCollapseNeedsItsMaterials(t *testing.T) {
	tab, err := material.NewTable([]material.Def{{Name: "air", Kind: material.Empty}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := world.New(world.Config{Width: 64, Height: 64, Materials: tab})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := Collapse(w, tab, 1); err == nil {
		t.Fatal("Collapse accepted a table without stone, sand and water")
	}
}
