package world

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
)

func TestNewRejectsBadConfig(t *testing.T) {
	tab := testTable(t, nil)
	// One cell more than a falling liquid may still flow.
	tooFar := testTable(t, func(d []material.Def) { d[defWater].Dispersion = uint8(MaxReach - fallReach() + 1) })
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"zero width", Config{Width: 0, Height: 8, Materials: tab}, "not positive"},
		{"no materials", Config{Width: 8, Height: 8}, "no material table"},
		{"reach too far", Config{Width: 8, Height: 8, Materials: tooFar}, "disperses"},
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
	if _, y, ok := find(w, ids.sand); !ok || y != w.Height()-1 {
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
	for range 200 {
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

// Skipping what lies outside the chunks' change rectangles must not change
// the world: tick by tick, a world that sleeps matches one that scans
// every cell of every chunk, with cells set, filled, painted and worn
// between ticks.
func TestSleepMatchesScanningEverything(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			sleeping, ids := newTestWorld(t, 200, 150, workers)
			scanning, _ := newTestWorld(t, 200, 150, workers)
			scanning.scanAll = true
			for _, w := range []*World{sleeping, scanning} {
				scatter(w, ids, 13)
			}
			flew := 0
			for tick := range 500 {
				for _, w := range []*World{sleeping, scanning} {
					edit(w, ids, tick)
					w.Step()
				}
				flew = max(flew, sleeping.Particles())
				if a, b := sleeping.Checksum(), scanning.Checksum(); a != b {
					t.Fatalf("tick %d: the sleeping world's checksum is %#x, the scanned world's %#x",
						sleeping.Tick(), a, b)
				}
			}
			if sleeping.Awake() >= scanning.Awake() {
				t.Fatalf("at the end %d chunks are awake, as many as the %d a full scan runs", sleeping.Awake(), scanning.Awake())
			}
			if flew < 30 {
				t.Fatalf("at most %d particles flew; the edits must throw some", flew)
			}
		})
	}
}

// edit changes w before the given tick the way the player's tools do,
// mostly where things have settled: it digs through the ledges, drops sand
// and water, and wears cells down; and the way bodies do: it throws cells
// up out of the grid as particles, and specks.
func edit(w *World, ids testIDs, tick int) {
	ledge := w.Height() * 6 / 10
	switch tick {
	case 150, 270:
		for x := range 40 {
			w.Launch(w.Width()/4+x, ledge-1-x%3, perSecond(x%9-4), perSecond(-30-5*x), uint64(x))
		}
	case 160:
		for x := range 20 {
			w.Spray(Cell{Mat: ids.wood}, w.Width()/2+x, 10, perSecond(x%5-2), perSecond(x%3), uint64(x))
		}
	case 200:
		w.Paint(w.Width()/2, ledge, 5, material.Air, false)
	case 230:
		w.Fill(20, 0, 50, 12, ids.sand)
	case 260:
		w.Paint(w.Width()*3/4, 20, 6, ids.water, true)
	case 300:
		w.Set(w.Width()/3, 2, ids.sand)
	case 330:
		w.Paint(w.Width()/4, w.Height()*8/10, 4, material.Air, false)
	}
	if tick >= 350 && tick < 400 {
		w.Wear(w.Width()/5, w.Height()-1, 900)
	}
}

// Layers that cannot move put their chunks to sleep after one tick.
func TestRestingLayersSleep(t *testing.T) {
	w, ids := newTestWorld(t, 256, 128, 1)
	// Water on sand, wall to wall.
	w.Fill(0, 64, 256, 96, ids.water)
	w.Fill(0, 96, 256, 128, ids.sand)
	w.Step()
	if w.Awake() == 0 {
		t.Fatal("the first tick after the fill ran no chunk")
	}
	// The sand counts its ticks at rest until it is settled; the tick
	// after the last count finds nothing to change.
	for range SettleTicks {
		w.Step()
	}
	w.Step()
	if n := w.Awake(); n != 0 {
		t.Fatalf("%d of %d chunks are awake in layers that cannot move", n, w.Chunks())
	}
}

// A chunk wakes when something changes in or next to it, and sleeps again
// once nothing in it can move. The world holds only sand: an unfilled row
// on top of a pool keeps moving under the liquid rule (design.md 7절).
func TestChunksWakeAndSleepAgain(t *testing.T) {
	w, ids := newTestWorld(t, 256, 128, 1)
	w.Fill(0, 96, 256, 128, ids.sand)
	settle(t, w)
	edits := []struct {
		name string
		edit func()
	}{
		{"set", func() { w.Set(100, 0, ids.sand) }},
		{"fill", func() { w.Fill(10, 10, 30, 30, ids.sand) }},
		{"paint", func() { w.Paint(200, 100, 4, material.Air, false) }},
	}
	for _, e := range edits {
		e.edit()
		w.Step()
		if w.Awake() == 0 {
			t.Fatalf("%s woke no chunk", e.name)
		}
		settle(t, w)
	}
}

// The renderer uploads only the chunks whose version grew, so every cell
// that changes, in a tick or between ticks and in damage too, must raise
// its chunk's version.
func TestChangesRaiseChunkVersions(t *testing.T) {
	w, ids := newTestWorld(t, 200, 150, 4)
	scatter(w, ids, 21)
	for tick := range 450 {
		before := snapshot(w)
		seen := w.Version()
		edit(w, ids, tick)
		w.Step()
		for i, c := range snapshot(w) {
			if c == before[i] {
				continue
			}
			x, y := i%w.width, i/w.width
			if v := w.ChunkVersion(x/ChunkSize, y/ChunkSize); v <= seen {
				t.Fatalf("tick %d: cell (%d, %d) changed from %+v to %+v, but its chunk's version %d is not above %d",
					w.Tick(), x, y, before[i], c, v, seen)
			}
		}
	}
}

// Damage changes only how a cell is drawn, but it must still raise the
// chunk's version, or the renderer would not show digging progress.
func TestWearRaisesChunkVersion(t *testing.T) {
	w, ids := newTestWorldWith(t, 128, 64, 1, withHP(10000, 0))
	w.Fill(0, 32, 128, 64, ids.stone)
	settle(t, w)
	seen := w.Version()
	if w.Wear(100, 40, 10) {
		t.Fatal("one small wear used the stone up")
	}
	if v := w.ChunkVersion(1, 0); v <= seen {
		t.Fatalf("wearing a cell left its chunk at version %d, not above %d", v, seen)
	}
	if v := w.ChunkVersion(0, 0); v > seen {
		t.Fatalf("wearing a cell in chunk (1, 0) raised chunk (0, 0) to version %d", v)
	}
}

// A world in which nothing can move keeps its chunk versions, so the
// renderer uploads nothing.
func TestSleepingChunksKeepTheirVersions(t *testing.T) {
	w, ids := newTestWorld(t, 256, 128, 1)
	w.Fill(0, 64, 256, 96, ids.water)
	w.Fill(0, 96, 256, 128, ids.sand)
	settle(t, w)
	seen := w.Version()
	for range 10 {
		w.Step()
	}
	for cy := range 2 {
		for cx := range 4 {
			if v := w.ChunkVersion(cx, cy); v > seen {
				t.Errorf("chunk (%d, %d) is at version %d after sleeping ticks, above %d", cx, cy, v, seen)
			}
		}
	}
}

// settle steps w until every chunk sleeps, and fails if that takes long.
func settle(t *testing.T, w *World) {
	t.Helper()
	for range 2000 {
		w.Step()
		if w.Awake() == 0 {
			return
		}
	}
	t.Fatalf("%d chunks are still awake after 2000 ticks", w.Awake())
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

// BenchmarkStep runs a busy world: a random mix falling onto ledges.
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

// BenchmarkStepScanAll runs the busy world of BenchmarkStep with every
// chunk scanned whole, as before chunks could sleep: the cost of keeping
// the change rectangles without the saving.
func BenchmarkStepScanAll(b *testing.B) {
	for _, workers := range []int{1, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			w, ids := newTestWorld(b, 1024, 1024, workers)
			w.scanAll = true
			scatter(w, ids, 5)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Step()
			}
		})
	}
}

// BenchmarkStepAsleep runs a world in which nothing can move.
func BenchmarkStepAsleep(b *testing.B) {
	w, ids := newTestWorld(b, 1024, 1024, 1)
	w.Fill(0, 512, 1024, 768, ids.water)
	w.Fill(0, 768, 1024, 1024, ids.sand)
	// The sand counts its ticks at rest until it is settled.
	for range SettleTicks + 2 {
		w.Step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Step()
	}
}
