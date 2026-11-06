package world

import (
	"testing"

	"github.com/tongmon/matter-wake/internal/rng"
)

// Changing any one field of any one cell changes the checksum, and putting
// it back restores it. The world is 150 wide, so rows end partway through
// the four lanes of hashRow.
func TestChecksumSeesEveryField(t *testing.T) {
	w, ids := newTestWorld(t, 150, 130, 1)
	scatter(w, ids, 3)
	for range 20 {
		w.Step()
	}
	want := w.Checksum()
	edits := []struct {
		field string
		edit  func(c *Cell)
	}{
		{"Mat", func(c *Cell) { c.Mat ^= 1 }},
		{"Tone", func(c *Cell) { c.Tone ^= 0x80 }},
		{"Falling", func(c *Cell) { c.Falling = !c.Falling }},
		{"Last", func(c *Cell) { c.Last ^= 1 << 31 }},
		{"Vy", func(c *Cell) { c.Vy ^= -1 << 15 }},
		{"Ax", func(c *Cell) { c.Ax ^= 1 }},
		{"Damage", func(c *Cell) { c.Damage ^= 1 << 15 }},
	}
	s := rng.New(9)
	for _, e := range edits {
		for _, x := range []int{0, 1, 2, 3, 4, 146, 147, 148, 149, s.IntN(150)} {
			y := s.IntN(130)
			c := w.cell(w.pos(x, y))
			old := *c
			e.edit(c)
			if got := w.Checksum(); got == want {
				t.Errorf("changing %s of the cell at (%d, %d) left the checksum %#x", e.field, x, y, got)
			}
			*c = old
			if got := w.Checksum(); got != want {
				t.Fatalf("putting the cell at (%d, %d) back gave %#x, want %#x", x, y, got, want)
			}
		}
	}
}

// Swapping two different cells changes the checksum, whether they are
// hashed in the same lane (four apart) or in different ones.
func TestChecksumSeesOrder(t *testing.T) {
	w, ids := newTestWorld(t, 150, 130, 1)
	w.Set(10, 50, ids.sand)
	want := w.Checksum()
	for _, dx := range []int{1, 2, 4, 8} {
		a, b := w.cell(w.pos(10, 50)), w.cell(w.pos(10+dx, 50))
		*a, *b = *b, *a
		if got := w.Checksum(); got == want {
			t.Errorf("moving the sand %d cells right left the checksum %#x", dx, got)
		}
		*a, *b = *b, *a
	}
	a, b := w.cell(w.pos(10, 50)), w.cell(w.pos(10, 51))
	*a, *b = *b, *a
	if got := w.Checksum(); got == want {
		t.Errorf("moving the sand a row down left the checksum %#x", got)
	}
}

// BenchmarkChecksum hashes a busy 1024×1024 world, four blocks in memory.
func BenchmarkChecksum(b *testing.B) {
	w, ids := newTestWorld(b, 1024, 1024, 1)
	scatter(w, ids, 5)
	for range 30 {
		w.Step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Checksum()
	}
}
