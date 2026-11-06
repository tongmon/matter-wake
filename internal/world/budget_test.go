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

// The chunk under the focus has distance 0 however often it runs. A
// chunk that waits still gets past it (Codex review).
func TestBudgetLetsWaitingChunksPassTheFocusChunk(t *testing.T) {
	w, ids := newBudgetWorld(t, 4*ChunkSize, ChunkSize, 1, 1)
	w.SetFocus(ChunkSize/2, ChunkSize/2)
	w.Fill(3*ChunkSize, 0, 4*ChunkSize, 8, ids.sand)
	far := chunkSum(w, 3)
	for range 200 {
		// The focus chunk stays busy every tick.
		w.Fill(0, 0, ChunkSize, 8, ids.sand)
		w.Fill(0, ChunkSize-8, ChunkSize, ChunkSize, material.Air)
		w.Step()
		if chunkSum(w, 3) != far {
			return
		}
	}
	t.Fatal("chunk 3 never ran in 200 ticks while the chunk under the focus stayed busy")
}

// copyWorld returns a new world of w's size, seed, tick and budget with
// copies of w's blocks in memory.
func copyWorld(t *testing.T, w *World, workers int) *World {
	t.Helper()
	c, err := New(Config{
		Width: w.Width(), Height: w.Height(), Seed: w.Seed(), Workers: workers, Materials: testTable(t, nil),
		Streamed: true, Tick: w.Tick(), Budget: w.budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	for _, i := range w.resident {
		b := new(Block)
		*b = *w.blocks[i]
		if err := c.LoadBlock(i%w.bcols, i/w.bcols, b); err != nil {
			t.Fatal(err)
		}
	}
	c.SetFocus(w.focusX, w.focusY)
	return c
}

// A world loaded from a save goes on exactly as the saved one when it gets
// the saved chunks' states back, budget and all (Codex review): which
// chunks wait depends on what each has left to scan and how long it has
// waited.
func TestChunkStatesCarryTheSchedule(t *testing.T) {
	saved, ids := newBudgetWorld(t, 2*BlockSize, BlockSize, 2, 4*ChunkSize*ChunkSize)
	saved.SetFocus(100, 100)
	scatter(saved, ids, 7)
	for range 40 {
		saved.Step()
	}
	if saved.Waiting() == 0 {
		t.Fatal("no chunk waits at the save: the test needs a world over its budget")
	}
	restored := copyWorld(t, saved, 4)
	if err := restored.SetChunkStates(saved.ChunkStates()); err != nil {
		t.Fatal(err)
	}
	// Without the states, loading wakes every chunk whole.
	unrestored := copyWorld(t, saved, 4)
	diverged := false
	for range 100 {
		for _, w := range []*World{saved, restored, unrestored} {
			w.Step()
		}
		if a, b := saved.Checksum(), restored.Checksum(); a != b {
			t.Fatalf("tick %d: the restored world's checksum %#x, the saved one's %#x", saved.Tick(), b, a)
		}
		diverged = diverged || unrestored.Checksum() != saved.Checksum()
	}
	if !diverged {
		t.Fatal("the world loaded without the states never diverged: the test does not need them")
	}
}

func TestSetChunkStatesRejectsBadStates(t *testing.T) {
	w, _ := newBudgetWorld(t, BlockSize, BlockSize, 1, 0)
	tests := []struct {
		name string
		s    ChunkState
	}{
		{"outside the grid", ChunkState{CX: 8, CY: 0}},
		{"negative chunk", ChunkState{CX: -1, CY: 0}},
		{"rectangle outside the chunk", ChunkState{CX: 1, CY: 0, Dirty: [4]int{0, 0, 10, 10}}},
		{"keep outside the chunk", ChunkState{CX: 0, CY: 1, Keep: [4]int{0, 0, 10, 10}}},
		{"negative wait", ChunkState{CX: 0, CY: 0, Waited: -1}},
		{"wait past the cap", ChunkState{CX: 0, CY: 0, Waited: maxWaited + 1}},
	}
	for _, tt := range tests {
		if err := w.SetChunkStates([]ChunkState{tt.s}); err == nil {
			t.Errorf("%s: SetChunkStates accepted %+v", tt.name, tt.s)
		}
	}
}

// The budget is a cap: a chunk that does not fit what is left waits, even
// when less is left than one chunk scans. Only the first chunk may be
// bigger than the budget (second Codex review).
func TestBudgetIsNeverOvershot(t *testing.T) {
	// Four chunks of topRows×ChunkSize cells each, and room for one and a
	// cell.
	w, ids := newBudgetWorld(t, 4*ChunkSize, ChunkSize, 1, topRows*ChunkSize+1)
	fillTops(w, ids)
	w.Step()
	if w.Awake() != 1 || w.Waiting() != 3 {
		t.Fatalf("%d chunks ran and %d waited, want 1 and 3", w.Awake(), w.Waiting())
	}
}

// A block taken out of memory forgets how long its chunks waited, as it
// forgets what they had to scan: a save does not record the chunks of
// blocks out of memory, so a session opened again must find the same when
// the block comes back (second Codex review).
func TestUnloadingForgetsTheWaits(t *testing.T) {
	w, ids := newBudgetWorld(t, 2*BlockSize, BlockSize, 1, ChunkSize*ChunkSize)
	w.SetFocus(0, 0)
	scatter(w, ids, 5)
	w.Step()
	waited := 0
	for _, s := range w.ChunkStates() {
		if s.CX >= blockChunks && s.Waited > 0 {
			waited++
		}
	}
	if waited == 0 {
		t.Fatal("no chunk of block (1, 0) waited: the test does not reach the budget")
	}
	b, err := w.UnloadBlock(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.LoadBlock(1, 0, b); err != nil {
		t.Fatal(err)
	}
	for _, s := range w.ChunkStates() {
		if s.CX >= blockChunks && s.Waited != 0 {
			t.Fatalf("chunk (%d, %d) still counts %d ticks of waiting after its block left memory", s.CX, s.CY, s.Waited)
		}
	}
}

// With more chunks busy than a wait can count to in its distance halving,
// the longest-waiting chunk still goes first, so every chunk takes its
// turn (third Codex review).
func TestBudgetServesEveryBusyChunk(t *testing.T) {
	const n = 128
	w, ids := newBudgetWorld(t, n*ChunkSize, ChunkSize, 1, 1)
	ran := make([]bool, n)
	left := n
	for tick := range 4 * n {
		// Sand lands on every chunk every tick, so all of them stay busy.
		w.Fill(0, 0, w.Width(), 1, ids.sand)
		w.Step()
		for cx := range n {
			if !ran[cx] && w.waited[cx] == 0 {
				ran[cx] = true
				left--
			}
		}
		if left == 0 {
			t.Logf("every chunk ran by tick %d", tick+1)
			return
		}
	}
	t.Fatalf("%d of %d busy chunks never ran in %d ticks", left, n, 4*n)
}
