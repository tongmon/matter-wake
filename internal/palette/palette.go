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

func (p *Palette) put(px []byte, c world.Cell) {
	col := p.colors[c.Mat]
	// A cell's tone darkens it by up to spread/255 of its base color.
	shade := 255 - uint32(c.Tone)*uint32(p.spread[c.Mat])/255
	a := uint32(col.A)
	px[0] = byte(uint32(col.R) * shade / 255 * a / 255)
	px[1] = byte(uint32(col.G) * shade / 255 * a / 255)
	px[2] = byte(uint32(col.B) * shade / 255 * a / 255)
	px[3] = byte(a)
}
