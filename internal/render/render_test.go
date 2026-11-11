package render

import (
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/palette"
	"github.com/tongmon/matter-wake/internal/world"
)

// The particles in view become two triangles each, the square of their
// cell in its color, and listing them again does not allocate (Codex
// review: drawn one by one, each boxed its color).
func TestDotsDoNotAllocate(t *testing.T) {
	tab, err := material.NewTable([]material.Def{
		{Name: "air", Kind: material.Empty},
		{Name: "water", Kind: material.Liquid, Density: 4, Color: material.Color{R: 40, G: 120, B: 210, A: 200}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := world.New(world.Config{Width: 128, Height: 128, Seed: 1, Materials: tab})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	water := tab.MustLookup("water")
	for x := range 100 {
		w.Set(x, 50, water)
		w.Launch(x, 50, 0, 0, uint64(x))
	}
	w.Flush()
	r := New(tab)
	// A view of 64 × 64 cells from (10, 20) sees 64 of them.
	r.buildDots(w, 10, 20, 64, 64)
	if len(r.dots) != 4*64 || len(r.dotIndex) != 6*64 {
		t.Fatalf("%d vertices and %d indices for 64 particles in view, want %d and %d",
			len(r.dots), len(r.dotIndex), 4*64, 6*64)
	}
	ps := w.AppendParticles(nil)
	c := palette.New(tab).RGBA(ps[10].Cell)
	if v := r.dots[0]; v.DstX != 0 || v.DstY != 30 || v.ColorA != float32(c[3])/255 || v.ColorR != float32(c[0])/255 {
		t.Fatalf("the first vertex is %+v, want at (0, 30) in the color %v", v, c)
	}
	if v := r.dots[3]; v.DstX != 1 || v.DstY != 31 {
		t.Fatalf("the fourth vertex is at (%v, %v), want (1, 31)", v.DstX, v.DstY)
	}
	if n := testing.AllocsPerRun(20, func() { r.buildDots(w, 10, 20, 64, 64) }); n != 0 {
		t.Fatalf("listing the particles allocates %v times", n)
	}
}
