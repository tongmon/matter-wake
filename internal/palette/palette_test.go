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

func TestFillDoesNotAllocate(t *testing.T) {
	p := testPalette(t)
	cells := make([]world.Cell, 1024)
	dst := make([]byte, 4*len(cells))
	if n := testing.AllocsPerRun(20, func() { p.Fill(dst, cells) }); n != 0 {
		t.Fatalf("Fill allocates %v times per call", n)
	}
}
