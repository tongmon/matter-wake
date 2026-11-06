package palette

import (
	"bytes"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

func testPalette(t testing.TB) *Palette {
	t.Helper()
	return New(testTable(t))
}

func testTable(t testing.TB) *material.Table {
	t.Helper()
	tab, err := material.NewTable([]material.Def{
		{Name: "air", Kind: material.Empty},
		{Name: "rock", Kind: material.Static, Color: material.Color{R: 200, G: 100, B: 50, A: 255}, ToneSpread: 255, HP: 100},
		{Name: "glass", Kind: material.Static, Color: material.Color{R: 255, G: 255, B: 255, A: 128}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func TestFill(t *testing.T) {
	p := testPalette(t)
	cells := []world.Cell{
		{Mat: 0},
		{Mat: 1, Tone: 0},
		{Mat: 1, Tone: 255},
		{Mat: 2},
	}
	got := make([]byte, 4*len(cells))
	p.Fill(got, cells)
	want := []byte{
		0, 0, 0, 0, // air is transparent
		200, 100, 50, 255, // tone 0 keeps the base color
		0, 0, 0, 255, // full spread at full tone darkens to black
		128, 128, 128, 128, // alpha is premultiplied
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Fill = %v, want %v", got, want)
	}
}

// A damaged cell is drawn lighter, blended toward white by up to about 60%
// at full damage, so digging shows its progress. Tone only darkens, so the
// two never look alike.
func TestFillLightensDamagedCells(t *testing.T) {
	p := testPalette(t)
	cells := []world.Cell{
		{Mat: 1},
		{Mat: 1, Damage: world.DamageScale / 2},
		{Mat: 1, Damage: world.DamageScale - 1},
		{Mat: 2, Damage: world.DamageScale - 1},
	}
	got := make([]byte, 4*len(cells))
	p.Fill(got, cells)
	base := [3]float64{200, 100, 50}
	for i, share := range []float64{0, 0.5, 1} {
		for ch := range 3 {
			want := base[ch] + (255-base[ch])*0.6*share
			if g := float64(got[4*i+ch]); g < want-1.5 || g > want+1.5 {
				t.Errorf("cell %d channel %d = %v, want about %.1f (damage share %v)", i, ch, g, want, share)
			}
		}
		if got[4*i+3] != 255 {
			t.Errorf("cell %d alpha = %d, want 255", i, got[4*i+3])
		}
	}
	// White glass stays white; alpha is still premultiplied.
	if g := got[12:16]; !bytes.Equal(g, []byte{128, 128, 128, 128}) {
		t.Errorf("damaged glass = %v, want [128 128 128 128]", g)
	}
}

func TestFillDoesNotAllocate(t *testing.T) {
	p := testPalette(t)
	cells := make([]world.Cell, 1024)
	dst := make([]byte, 4*len(cells))
	if n := testing.AllocsPerRun(20, func() { p.Fill(dst, cells) }); n != 0 {
		t.Fatalf("Fill allocates %v times per call", n)
	}
}

// FillRect draws a rectangle of a world row by row, the same pixels Fill
// draws for those cells.
func TestFillRectMatchesFill(t *testing.T) {
	tab := testTable(t)
	w, err := world.New(world.Config{Width: 10, Height: 8, Seed: 3, Materials: tab})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Fill(2, 1, 9, 7, tab.MustLookup("rock"))
	w.Set(5, 3, tab.MustLookup("glass"))
	w.Wear(6, 4, 60)
	p := New(tab)
	const x0, y0, x1, y1 = 3, 2, 8, 6
	got := make([]byte, 4*(x1-x0)*(y1-y0))
	p.FillRect(got, w, x0, y0, x1, y1)
	want := make([]byte, len(got))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c, _ := w.At(x, y)
			p.Fill(want[4*((y-y0)*(x1-x0)+x-x0):], []world.Cell{c})
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("FillRect = %v, want %v", got, want)
	}
}

// BenchmarkFill colors a screen of 480×270 cells, what drawing the whole
// sandbox cost every frame before the renderer uploaded only changed
// chunks.
func BenchmarkFill(b *testing.B) {
	p := New(testTable(b))
	cells := make([]world.Cell, 480*270)
	for i := range cells {
		cells[i] = world.Cell{Mat: material.ID(i % 3), Tone: uint8(i)}
	}
	dst := make([]byte, 4*len(cells))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		p.Fill(dst, cells)
	}
}
