// Package palette turns cells into pixels.
//
// It holds no Ebitengine state, so what the renderer uploads can be
// tested without a GPU.
package palette

import (
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

// Palette maps each material and tone to a color.
type Palette struct {
	colors []material.Color
	spread []uint8
}

// New builds a palette from the materials' colors.
func New(t *material.Table) *Palette {
	p := &Palette{colors: make([]material.Color, t.Len()), spread: make([]uint8, t.Len())}
	for i := range t.Len() {
		d := t.Def(material.ID(i))
		p.colors[i] = d.Color
		p.spread[i] = d.ToneSpread
	}
	return p
}

// Fill writes the color of each cell into dst as premultiplied RGBA, four
// bytes per cell, which is the layout ebiten.Image.WritePixels takes. It
// panics if dst is shorter than 4*len(cells).
func (p *Palette) Fill(dst []byte, cells []world.Cell) {
	dst = dst[:4*len(cells)]
	for i, c := range cells {
		p.put(dst[4*i:4*i+4], c)
	}
}

// FillRect writes the colors of the cells of w in [x0, x1) × [y0, y1)
// into dst, row by row, four bytes per cell as Fill does. The rectangle
// must lie in one block in memory (world.World.Row), as a chunk does, and
// dst must hold 4 bytes per cell.
func (p *Palette) FillRect(dst []byte, w *world.World, x0, y0, x1, y1 int) {
	n := x1 - x0
	for y := y0; y < y1; y++ {
		p.Fill(dst[4*(y-y0)*n:], w.Row(x0, x1, y))
	}
}

// RGBA returns the color of c as premultiplied red, green, blue and alpha,
// as Fill writes it: for a particle, drawn alone.
func (p *Palette) RGBA(c world.Cell) [4]byte {
	var px [4]byte
	p.put(px[:], c)
	return px
}

// damageTint is how far toward white a cell is drawn at full damage, in
// 256ths: about 60% (🧪). Damage lightens while tone only darkens, so a
// worn cell never looks like natural variation.
const damageTint = 154

func (p *Palette) put(px []byte, c world.Cell) {
	col := p.colors[c.Mat]
	// A cell's tone darkens it by up to spread/255 of its base color.
	shade := 255 - uint32(c.Tone)*uint32(p.spread[c.Mat])/255
	// Its damage then moves it k/256 of the way toward white.
	k := uint32(c.Damage) * damageTint / world.DamageScale
	a := uint32(col.A)
	px[0] = byte(lighten(uint32(col.R)*shade/255, k) * a / 255)
	px[1] = byte(lighten(uint32(col.G)*shade/255, k) * a / 255)
	px[2] = byte(lighten(uint32(col.B)*shade/255, k) * a / 255)
	px[3] = byte(a)
}

// lighten moves the channel value v k/256 of the way toward 255.
func lighten(v, k uint32) uint32 {
	return v + (255-v)*k/256
}
