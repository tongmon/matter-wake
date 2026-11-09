package world

import (
	"slices"
	"testing"
)

// The cells taken from a block that wait in the queue leave with it, in
// the queue's order, the retry list first; the cells of other blocks stay
// where they were. Put back after the block comes back, they wait as new
// cells, and none is lost or made on the way.
func TestQueuedCellsLeaveWithTheirBlock(t *testing.T) {
	w, ids := newTestWorld(t, 1024, 512, 1)
	for x := range 30 {
		w.Put(470+x, 101, Cell{Mat: ids.water})
		w.Put(100+x, 300, Cell{Mat: ids.water})
		w.Put(600+x, 300, Cell{Mat: ids.water})
	}
	w.Flush()
	// A body takes the water's places, as a body's write does: a body's
	// pixel ends every search for a place, so these cells fail and join the
	// retry list.
	for x := range 30 {
		w.Displace(470+x, 101)
		w.Put(470+x, 101, Cell{Mat: ids.wood})
	}
	w.Flush()
	w.Step()
	if len(w.retry) != 30 || len(w.fresh) != 0 {
		t.Fatalf("%d cells retry and %d are new; the test needs 30 retrying", len(w.retry), len(w.fresh))
	}
	for x := range 30 {
		w.Displace(100+x, 300)
		w.Displace(600+x, 300)
	}
	before := make([]int, testMaterialsLen)
	w.Counts(before)
	w.Queued(before)
	want := slices.Clone(w.retry)
	for _, q := range w.fresh {
		if q.X < BlockSize {
			want = append(want, q)
		}
	}
	got := w.TakeQueued(0, 0, nil)
	if !slices.Equal(got, want) {
		t.Fatalf("took %d cells of block (0, 0), want %d in the queue's order", len(got), len(want))
	}
	if w.QueueLen() != 30 {
		t.Fatalf("%d cells wait after taking block (0, 0)'s, want the 30 of block (1, 0)", w.QueueLen())
	}
	for _, q := range append(slices.Clone(w.retry), w.fresh...) {
		if q.X < BlockSize {
			t.Fatalf("a cell of block (0, 0) at (%d, %d) still waits", q.X, q.Y)
		}
	}
	blk, err := w.UnloadBlock(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.LoadBlock(0, 0, blk); err != nil {
		t.Fatal(err)
	}
	if err := w.Requeue(got); err != nil {
		t.Fatal(err)
	}
	after := make([]int, testMaterialsLen)
	w.Counts(after)
	w.Queued(after)
	if !slices.Equal(after, before) {
		t.Fatalf("cells and queued cells are %v after the round trip, %v before", after, before)
	}
	if len(w.retry) != 0 || len(w.fresh) != 90 {
		t.Fatalf("%d cells retry and %d are new after putting them back, want 0 and 90", len(w.retry), len(w.fresh))
	}
	// A cell from outside the world or from a block not in memory is a
	// caller's mistake.
	if err := w.Requeue([]Queued{{Cell: Cell{Mat: ids.water}, X: 2000, Y: 5}}); err == nil {
		t.Fatal("Requeue took a cell from outside the world")
	}
	if _, err := w.UnloadBlock(1, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Requeue([]Queued{{Cell: Cell{Mat: ids.water}, X: 600, Y: 5}}); err == nil {
		t.Fatal("Requeue took a cell from a block not in memory")
	}
}

// testMaterialsLen is how many materials testDefs holds.
const testMaterialsLen = 5
