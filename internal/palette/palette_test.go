package palette

import (
	"bytes"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

func testPalette(t *testing.T) *Palette {
	t.Helper()
	tab, err := material.NewTable([]material.Def{
		{Name: "air", Kind: material.Empty},
		{Name: "rock", Kind: material.Static, Color: material.Color{R: 200, G: 100, B: 50, A: 255}, ToneSpread: 255},
		{Name: "glass", Kind: material.Static, Color: material.Color{R: 255, G: 255, B: 255, A: 128}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(tab)
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
