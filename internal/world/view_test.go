package world

import "testing"

// A chunk's change rectangle covers what changed, and empties when the
// chunk falls asleep.
func TestChangeRect(t *testing.T) {
	w, id := newTestWorld(t, 128, 64, 1)
	for cx := range 2 {
		if x0, _, x1, _ := w.ChangeRect(cx, 0); x0 != x1 {
			t.Fatalf("chunk %d of a world of air has a change rectangle", cx)
		}
	}
	w.Set(70, 10, id.stone)
	x0, y0, x1, y1 := w.ChangeRect(1, 0)
	if x0 > 70 || x1 <= 70 || y0 > 10 || y1 <= 10 {
		t.Fatalf("chunk 1's change rectangle %d,%d-%d,%d misses the cell set at 70,10", x0, y0, x1, y1)
	}
	if a, _, b, _ := w.ChangeRect(0, 0); a != b {
		t.Fatal("setting a cell far from chunk 0 woke it")
	}
	w.Step()
	w.Step()
	if a, _, b, _ := w.ChangeRect(1, 0); a != b {
		t.Fatal("chunk 1 still has a change rectangle after its stone did nothing")
	}
}

// Held names the chunks the budget made wait in the last tick.
func TestHeld(t *testing.T) {
	w, id := newBudgetWorld(t, 256, 64, 1, 64*64)
	w.Fill(0, 0, 256, 32, id.sand)
	w.SetFocus(0, 0)
	if w.Held(3, 0) {
		t.Fatal("a chunk waited before any tick")
	}
	w.Step()
	if w.Held(0, 0) || !w.Held(3, 0) {
		t.Fatalf("held: nearest %t, farthest %t; want the far chunk only", w.Held(0, 0), w.Held(3, 0))
	}
}

// AppendQueued lists the cells waiting to go back.
func TestAppendQueued(t *testing.T) {
	w, id := newTestWorld(t, 64, 64, 1)
	w.Set(10, 20, id.water)
	w.Displace(10, 20)
	w.Flush()
	q := w.AppendQueued(nil)
	if len(q) != 1 || q[0].X != 10 || q[0].Y != 20 || q[0].Cell.Mat != id.water {
		t.Fatalf("queued %+v, want the water from 10,20", q)
	}
}
