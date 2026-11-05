package world

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

func TestNewRejectsBadConfig(t *testing.T) {
	tab := testTable(t, 4)
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"zero width", Config{Width: 0, Height: 8, Materials: tab}, "not positive"},
		{"no materials", Config{Width: 8, Height: 8}, "no material table"},
		{"reach too far", Config{Width: 8, Height: 8, Materials: testTable(t, MaxReach+1)}, "disperses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

func TestSandFallsToTheFloor(t *testing.T) {
	w, ids := newTestWorld(t, 8, 10, 1)
	w.Set(3, 0, ids.sand)
	for range w.Height() {
		w.Step()
	}
	if c, _ := w.At(3, w.Height()-1); c.Mat != ids.sand {
		t.Fatalf("sand did not land on the bottom row: %s", render(w, ids))
	}
}

func TestSandPilesInsteadOfStacking(t *testing.T) {
	w, ids := newTestWorld(t, 21, 12, 1)
	for range 60 {
		w.Set(10, 0, ids.sand)
		w.Step()
	}
	for range 30 {
		w.Step()
	}
	// A pile spreads: some sand must land away from the pour column.
	spread := 0
	for x := range w.Width() {
		if c, _ := w.At(x, w.Height()-1); c.Mat == ids.sand && x != 10 {
			spread++
		}
	}
	if spread < 4 {
		t.Fatalf("sand stacked in a column instead of piling:\n%s", render(w, ids))
	}
}

func TestSandSinksThroughWater(t *testing.T) {
	w, ids := newTestWorld(t, 6, 10, 1)
	w.Fill(0, 6, 6, 10, ids.water)
	w.Fill(0, 4, 6, 6, ids.sand)
	for range 80 {
		w.Step()
	}
	for x := range w.Width() {
		if c, _ := w.At(x, w.Height()-1); c.Mat != ids.sand {
			t.Fatalf("bottom row is not all sand after sinking:\n%s", render(w, ids))
		}
	}
}

func TestWaterSpreadsAcrossTheFloor(t *testing.T) {
	w, ids := newTestWorld(t, 40, 6, 1)
	// 72 water cells, enough to cover the 40-cell floor.
	w.Fill(14, 0, 26, 6, ids.water)
	for range 120 {
		w.Step()
	}
	for x := range w.Width() {
		if c, _ := w.At(x, w.Height()-1); c.Mat != ids.water {
			t.Fatalf("water did not cover the floor:\n%s", render(w, ids))
		}
	}
}

func TestStoneNeverMoves(t *testing.T) {
	w, ids := newTestWorld(t, 8, 8, 1)
	w.Fill(2, 0, 6, 2, ids.stone)
	for range 20 {
		w.Step()
	}
	for y := range 2 {
		for x := 2; x < 6; x++ {
			if c, _ := w.At(x, y); c.Mat != ids.stone {
				t.Fatalf("stone moved:\n%s", render(w, ids))
			}
		}
	}
}

func TestStepConservesCells(t *testing.T) {
	w, ids := newTestWorld(t, 150, 130, 4)
	scatter(w, ids, 7)
	before := [3]int{w.Count(ids.stone), w.Count(ids.sand), w.Count(ids.water)}
	for range 300 {
		w.Step()
	}
	after := [3]int{w.Count(ids.stone), w.Count(ids.sand), w.Count(ids.water)}
	if before != after {
		t.Fatalf("counts (stone, sand, water) changed from %v to %v", before, after)
	}
}

// The result of a tick must not depend on how many goroutines ran it.
func TestStepIsIndependentOfWorkers(t *testing.T) {
	var want uint64
	for _, workers := range []int{1, 2, 3, 8} {
		w, ids := newTestWorld(t, 200, 150, workers)
		scatter(w, ids, 11)
		for range 200 {
			w.Step()
		}
		got := w.Checksum()
		if workers == 1 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("workers=%d checksum %#x, workers=1 checksum %#x", workers, got, want)
		}
	}
}

func TestPaint(t *testing.T) {
	w, ids := newTestWorld(t, 20, 20, 1)
	if n := w.Paint(10, 10, 2, ids.stone, false); n != 13 {
		t.Fatalf("painted %d cells in a radius-2 disc, want 13", n)
	}
	if n := w.Paint(10, 10, 3, ids.sand, true); n != 29-13 {
		t.Fatalf("onlyEmpty painted %d cells, want %d", n, 29-13)
	}
	if n := w.Paint(10, 10, 3, material.Air, false); n != 29 {
		t.Fatalf("digging cleared %d cells, want 29", n)
	}
	if n := w.Paint(-5, -5, 2, ids.stone, false); n != 0 {
		t.Fatalf("painting outside the grid changed %d cells", n)
	}
}

func TestStepDoesNotAllocate(t *testing.T) {
	for _, workers := range []int{1, 4} {
		w, ids := newTestWorld(t, 256, 192, workers)
		scatter(w, ids, 3)
		if n := testing.AllocsPerRun(20, w.Step); n != 0 {
			t.Fatalf("workers=%d: Step allocates %v times per tick", workers, n)
		}
	}
}

func BenchmarkStep(b *testing.B) {
	for _, workers := range []int{1, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			w, ids := newTestWorld(b, 1024, 1024, workers)
			scatter(w, ids, 5)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Step()
			}
		})
	}
}

type testIDs struct {
	stone, sand, water material.ID
}

func testTable(tb testing.TB, waterDispersion int) *material.Table {
	tb.Helper()
	tab, err := material.NewTable([]material.Def{
		{Name: "air", Kind: material.Empty},
		{Name: "stone", Kind: material.Static, Density: 255},
		{Name: "sand", Kind: material.Powder, Density: 30},
		{Name: "water", Kind: material.Liquid, Density: 20, Dispersion: uint8(waterDispersion)},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return tab
}

func newTestWorld(tb testing.TB, width, height, workers int) (*World, testIDs) {
	tb.Helper()
	tab := testTable(tb, 4)
	w, err := New(Config{Width: width, Height: height, Seed: 1, Workers: workers, Materials: tab})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(w.Close)
	return w, testIDs{stone: tab.MustLookup("stone"), sand: tab.MustLookup("sand"), water: tab.MustLookup("water")}
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
