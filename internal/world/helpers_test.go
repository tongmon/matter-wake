package world

import (
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

type testIDs struct {
	stone, sand, water, wood material.ID
}

// Indexes of the materials testDefs returns.
const (
	defStone = 1
	defSand  = 2
	defWater = 3
	defWood  = 4
)

// testDefs returns air, stone, sand, water and wood with the shipped
// values of data/materials.json. Tests change single values to isolate a
// rule.
func testDefs() []material.Def {
	q := func(f float64) fixed.Q8 {
		v, err := fixed.FromFloat(f)
		if err != nil {
			panic(err)
		}
		return v
	}
	return []material.Def{
		{Name: "air", Kind: material.Empty},
		{Name: "stone", Kind: material.Static, Density: 10},
		{
			Name: "sand", Kind: material.Powder, Density: 6,
			FallAccel: q(0.225), SinkChance: 25, SlideChance: 50,
		},
		{
			Name: "water", Kind: material.Liquid, Density: 4,
			FallAccel: q(0.375), SinkChance: 50,
			FlowSpeed: q(0.9), FlowDamping: q(0.8), Dispersion: 4,
		},
		{Name: "wood", Kind: material.Solid, Density: 6, Friction: q(0.9), Restitution: q(0.2)},
	}
}

// testTable builds a table from testDefs after edit changes them.
func testTable(tb testing.TB, edit func(defs []material.Def)) *material.Table {
	tb.Helper()
	defs := testDefs()
	if edit != nil {
		edit(defs)
	}
	tab, err := material.NewTable(defs)
	if err != nil {
		tb.Fatal(err)
	}
	return tab
}

func newTestWorld(tb testing.TB, width, height, workers int) (*World, testIDs) {
	tb.Helper()
	return newTestWorldWith(tb, width, height, workers, nil)
}

func newTestWorldWith(tb testing.TB, width, height, workers int, edit func(defs []material.Def)) (*World, testIDs) {
	tb.Helper()
	tab := testTable(tb, edit)
	w, err := New(Config{Width: width, Height: height, Seed: 1, Workers: workers, Materials: tab})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(w.Close)
	return w, testIDs{
		stone: tab.MustLookup("stone"), sand: tab.MustLookup("sand"), water: tab.MustLookup("water"), wood: tab.MustLookup("wood"),
	}
}

// find returns where the only cell of material id is, or false.
func find(w *World, id material.ID) (x, y int, ok bool) {
	for y := range w.Height() {
		for x := range w.Width() {
			if c, _ := w.At(x, y); c.Mat == id {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// at returns the cell at (x, y), which must be in memory, for a test to
// read or change in place.
func (w *World) at(x, y int) *Cell {
	return w.cell(w.pos(x, y))
}

// snapshot returns a copy of every cell of w, row by row. Every block must
// be in memory.
func snapshot(w *World) []Cell {
	cells := make([]Cell, 0, w.Width()*w.Height())
	for y := range w.Height() {
		for x := range w.Width() {
			cells = append(cells, *w.at(x, y))
		}
	}
	return cells
}

// scatter fills the top 40% of the world with a random mix and lays a few
// stone ledges below it.
func scatter(w *World, ids testIDs, seed uint64) {
	s := rng.New(seed)
	for y := range w.Height() * 4 / 10 {
		for x := range w.Width() {
			switch s.IntN(10) {
			case 0, 1, 2:
				w.Set(x, y, ids.sand)
			case 3, 4, 5:
				w.Set(x, y, ids.water)
			}
		}
	}
	for _, y := range []int{w.Height() * 6 / 10, w.Height() * 8 / 10} {
		for x := w.Width() / 8; x < w.Width()*7/8; x++ {
			if x%23 > 3 {
				w.Set(x, y, ids.stone)
			}
		}
	}
}

// render draws the grid as text for failure messages.
func render(w *World, ids testIDs) string {
	var b strings.Builder
	b.WriteByte('\n')
	for y := range w.Height() {
		for x := range w.Width() {
			c, _ := w.At(x, y)
			switch c.Mat {
			case ids.stone:
				b.WriteByte('#')
			case ids.sand:
				b.WriteByte('s')
			case ids.water:
				b.WriteByte('~')
			default:
				b.WriteByte('.')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
