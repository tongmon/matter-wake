package world

import (
	"fmt"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
)

// newBudgetWorld returns a world of the test materials that scans at most
// budget cells a tick.
func newBudgetWorld(tb testing.TB, width, height, workers, budget int) (*World, testIDs) {
	tb.Helper()
	tab := testTable(tb, nil)
	w, err := New(Config{Width: width, Height: height, Seed: 1, Workers: workers, Materials: tab, Budget: budget})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(w.Close)
	return w, testIDs{stone: tab.MustLookup("stone"), sand: tab.MustLookup("sand"), water: tab.MustLookup("water")}
}

// A budget that every tick fits in changes nothing.
func TestBudgetThatFitsChangesNothing(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			free, ids := newTestWorld(t, 200, 150, workers)
			// 4×3 chunks of at most 64×64 cells each.
			fits, _ := newBudgetWorld(t, 200, 150, workers, 12*ChunkSize*ChunkSize)
			for _, w := range []*World{free, fits} {
				scatter(w, ids, 13)
			}
			for tick := range 500 {
				for _, w := range []*World{free, fits} {
					edit(w, ids, tick)
					w.Step()
				}
				if a, b := free.Checksum(), fits.Checksum(); a != b {
					t.Fatalf("tick %d: checksum %#x with a budget that fits, %#x without", free.Tick(), b, a)
				}
				if n := fits.Waiting(); n != 0 {
					t.Fatalf("tick %d: %d chunks waited under a budget that fits", fits.Tick(), n)
				}
			}
		})
	}
}

// topRows is how many rows fillTops fills, so each chunk of a one-row
// world has topRows×ChunkSize cells to scan.
const topRows = 16

// fillTops puts sand in the top rows of every chunk of a one-row world, in
// the air, so every chunk is busy.
func fillTops(w *World, ids testIDs) {
	w.Fill(0, 0, w.Width(), topRows, ids.sand)
}

// chunkSum hashes the cells of chunk cx of a one-row world.
func chunkSum(w *World, cx int) uint64 {
	var h uint64
	for y := range min(ChunkSize, w.Height()) {
		for x := cx * ChunkSize; x < min((cx+1)*ChunkSize, w.Width()); x++ {
			c, _ := w.At(x, y)
			h = h*1099511628211 + uint64(c.Mat)<<32 + uint64(c.Vy)
		}
	}
	return h
}

// Over the budget, the chunks nearest the focus run and the others wait as
// they are, keeping what they have to scan.
func TestBudgetHoldsFarChunks(t *testing.T) {
	// One row of 8 chunks, all busy; a budget of what two of them scan.
	w, ids := newBudgetWorld(t, 8*ChunkSize, ChunkSize, 2, 2*topRows*ChunkSize)
	w.SetFocus(0, ChunkSize/2)
	fillTops(w, ids)
	var before [8]uint64
	for cx := range before {
		before[cx] = chunkSum(w, cx)
	}
	w.Step()
	if w.Awake() != 2 || w.Waiting() != 6 {
		t.Fatalf("%d chunks ran and %d waited, want 2 and 6", w.Awake(), w.Waiting())
	}
	for cx := range 2 {
		if chunkSum(w, cx) == before[cx] {
			t.Errorf("chunk %d, nearest the focus, did not change", cx)
		}
	}
	// Chunk 2 may take sand from chunk 1 beside it; the rest must not change.
	for cx := 3; cx < 8; cx++ {
		if chunkSum(w, cx) != before[cx] {
			t.Errorf("chunk %d changed though it waited", cx)
		}
		if w.dirty[cx].empty() {
			t.Errorf("chunk %d lost what it has to scan while it waited", cx)
		}
	}
}

// The focus decides which chunks go first.
func TestBudgetFollowsTheFocus(t *testing.T) {
	w, ids := newBudgetWorld(t, 8*ChunkSize, ChunkSize, 2, 2*topRows*ChunkSize)
	w.SetFocus(8*ChunkSize, ChunkSize/2)
	fillTops(w, ids)
	before := chunkSum(w, 0)
	far := chunkSum(w, 7)
	w.Step()
	if chunkSum(w, 7) == far {
		t.Error("chunk 7, nearest the focus, did not change")
	}
	if chunkSum(w, 0) != before {
		t.Error("chunk 0, farthest from the focus, changed")
	}
}

// A chunk far from the focus gets its turn even while the chunks near it
// keep the tick over the budget.
func TestBudgetGetsToFarChunks(t *testing.T) {
	w, ids := newBudgetWorld(t, 8*ChunkSize, ChunkSize, 2, 2*ChunkSize*ChunkSize)
	w.SetFocus(0, ChunkSize/2)
	w.Fill(7*ChunkSize, 0, 8*ChunkSize, 8, ids.sand)
	far := chunkSum(w, 7)
	for tick := range 64 {
		// Sand pours into the two chunks nearest the focus every tick.
		w.Fill(0, 0, 2*ChunkSize, 8, ids.sand)
		w.Fill(0, ChunkSize-8, 2*ChunkSize, ChunkSize, material.Air)
		w.Step()
		if chunkSum(w, 7) != far {
			if tick == 0 {
				t.Fatal("chunk 7 ran in the first tick: the budget is not tested")
			}
			return
		}
	}
	t.Fatal("chunk 7 never ran in 64 ticks while the chunks near the focus stayed busy")
}

// However small the budget, the nearest chunk runs, so the world never
// stops.
func TestBudgetAlwaysRunsOneChunk(t *testing.T) {
	w, ids := newBudgetWorld(t, 4*ChunkSize, ChunkSize, 1, 1)
	fillTops(w, ids)
	w.Step()
	if w.Awake() != 1 || w.Waiting() != 3 {
		t.Fatalf("%d chunks ran and %d waited, want 1 and 3", w.Awake(), w.Waiting())
	}
}

// With a budget, the result still does not depend on the worker count.
func TestBudgetIsIndependentOfWorkers(t *testing.T) {
	var want uint64
	for _, workers := range []int{1, 2, 3, 8} {
		w, ids := newBudgetWorld(t, 200, 150, workers, 3*ChunkSize*ChunkSize)
		w.SetFocus(150, 40)
		scatter(w, ids, 11)
		waited := 0
		for range 200 {
			w.Step()
			waited += w.Waiting()
		}
		if waited == 0 {
			t.Fatal("no chunk ever waited: the budget is not tested")
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

func TestBudgetDoesNotAllocate(t *testing.T) {
	w, ids := newBudgetWorld(t, 256, 192, 4, 2*ChunkSize*ChunkSize)
	scatter(w, ids, 3)
	if n := testing.AllocsPerRun(20, w.Step); n != 0 {
		t.Fatalf("Step allocates %v times per tick under a budget", n)
	}
	if w.Waiting() == 0 {
		t.Fatal("no chunk waited: the budget is not tested")
	}
}

func TestNewRejectsANegativeBudget(t *testing.T) {
	if _, err := New(Config{Width: 8, Height: 8, Materials: testTable(t, nil), Budget: -1}); err == nil {
		t.Fatal("New accepted a negative budget")
	}
}
