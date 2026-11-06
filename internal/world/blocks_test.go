package world

import (
	"strings"
	"testing"
)

// Taking a block out of memory and putting it back between ticks changes
// nothing: the world matches one that kept the block. The world is 2×2
// blocks, those on the right and at the bottom only partly in it.
func TestUnloadingAndLoadingKeepsTheWorld(t *testing.T) {
	for _, workers := range []int{1, 4} {
		kept, ids := newTestWorld(t, 600, 600, workers)
		moved, _ := newTestWorld(t, 600, 600, workers)
		for _, w := range []*World{kept, moved} {
			scatter(w, ids, 17)
		}
		for tick := range 120 {
			if tick%20 == 5 {
				b, err := moved.UnloadBlock(1, tick/20%2)
				if err != nil {
					t.Fatal(err)
				}
				if err := moved.LoadBlock(1, tick/20%2, b); err != nil {
					t.Fatal(err)
				}
			}
			kept.Step()
			moved.Step()
			if tick%4 != 3 {
				continue
			}
			if a, b := kept.Checksum(), moved.Checksum(); a != b {
				t.Fatalf("workers=%d tick %d: checksum %#x after unloading and loading a block, %#x without", workers, kept.Tick(), b, a)
			}
		}
	}
}

// A chunk whose cells could reach a block that is not in memory waits; the
// rest of the world goes on. Once the block is back the chunk runs again.
func TestChunksWaitForMissingNeighbors(t *testing.T) {
	w, ids := newTestWorld(t, 1024, 512, 1)
	w.Set(100, 0, ids.sand)
	// Within MaxReach of block (1, 0), which starts at x = 512.
	w.Set(500, 0, ids.sand)
	if _, err := w.UnloadBlock(1, 0); err != nil {
		t.Fatal(err)
	}
	for range 60 {
		w.Step()
	}
	if c, _ := w.At(100, 0); c.Mat == ids.sand {
		t.Fatal("sand far from the missing block did not fall")
	}
	if c, _ := w.At(500, 0); c.Mat != ids.sand {
		t.Fatal("sand next to the missing block moved, though its update could reach into it")
	}
	if err := w.LoadBlock(1, 0, new(Block)); err != nil {
		t.Fatal(err)
	}
	w.Step()
	if c, _ := w.At(500, 0); c.Mat == ids.sand {
		t.Fatal("sand next to the block did not fall once the block was back")
	}
}

// Cells of a block that is not in memory cannot be read or changed, and
// Count and Checksum leave them out.
func TestMissingBlocksHaveNoCells(t *testing.T) {
	w, ids := newTestWorld(t, 1024, 512, 1)
	w.Fill(0, 0, 1024, 10, ids.stone)
	if _, err := w.UnloadBlock(1, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.At(600, 5); ok {
		t.Fatal("At read a cell of a missing block")
	}
	w.Set(600, 5, ids.sand)
	w.Fill(600, 20, 700, 30, ids.sand)
	if n := w.Paint(600, 50, 5, ids.sand, false); n != 0 {
		t.Fatalf("Paint changed %d cells of a missing block", n)
	}
	if w.Wear(600, 5, 1) {
		t.Fatal("Wear wore down a cell of a missing block")
	}
	if n := w.Count(ids.stone); n != 512*10 {
		t.Fatalf("Count found %d stone cells, want the %d of the block in memory", n, 512*10)
	}
	if n := w.Count(ids.sand); n != 0 {
		t.Fatalf("%d sand cells were set in a missing block", n)
	}
}

func TestBlockErrors(t *testing.T) {
	w, _ := newTestWorld(t, 1024, 512, 1)
	if err := w.LoadBlock(0, 0, new(Block)); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("loading a block in memory: error %v", err)
	}
	if err := w.LoadBlock(2, 0, new(Block)); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("loading a block outside the world: error %v", err)
	}
	if err := w.LoadBlock(1, 0, nil); err == nil {
		t.Error("loading a nil block gave no error")
	}
	if _, err := w.UnloadBlock(0, 1); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("unloading a block outside the world: error %v", err)
	}
	if _, err := w.UnloadBlock(1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := w.UnloadBlock(1, 0); err == nil || !strings.Contains(err.Error(), "not in memory") {
		t.Errorf("unloading a block twice: error %v", err)
	}
}

// A streamed world starts with no block in memory, so even the fixed big
// world is cheap to make, and it ticks only what is loaded.
func TestStreamedWorldStartsEmpty(t *testing.T) {
	w, err := New(Config{Width: 16384, Height: 16384, Seed: 1, Workers: 1, Materials: testTable(t, nil), Streamed: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if cols, rows := w.Blocks(); cols != 32 || rows != 32 {
		t.Fatalf("Blocks() = %d, %d, want 32, 32", cols, rows)
	}
	if w.Resident(0, 0) {
		t.Fatal("a streamed world starts with block (0, 0) in memory")
	}
	if err := w.LoadBlock(5, 7, new(Block)); err != nil {
		t.Fatal(err)
	}
	w.Set(5*BlockSize+10, 7*BlockSize+10, 1)
	w.Step()
	if w.Awake() == 0 {
		t.Fatal("the loaded block did not run")
	}
}

// BenchmarkStepBigWorldAsleep ticks the fixed big world with 3×3 blocks in
// memory and nothing in them able to move.
func BenchmarkStepBigWorldAsleep(b *testing.B) {
	tab := testTable(b, nil)
	w, err := New(Config{Width: 16384, Height: 16384, Seed: 1, Workers: 1, Materials: tab, Streamed: true})
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	for by := 10; by < 13; by++ {
		for bx := 10; bx < 13; bx++ {
			if err := w.LoadBlock(bx, by, new(Block)); err != nil {
				b.Fatal(err)
			}
		}
	}
	w.Fill(10*BlockSize, 12*BlockSize, 13*BlockSize, 13*BlockSize, tab.MustLookup("sand"))
	w.Step()
	w.Step()
	if w.Awake() != 0 {
		b.Fatalf("%d chunks are awake", w.Awake())
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Step()
	}
}
