// Package render draws a session with Ebitengine.
package render

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/palette"
	"github.com/tongmon/matter-wake/internal/world"
)

var (
	background  = color.RGBA{R: 0x14, G: 0x12, B: 0x1c, A: 0xff}
	brushStroke = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x80}
)

// Renderer draws one world. It keeps an image of the cells between frames
// and uploads only the chunks that changed since it last drew them
// (world.World.ChunkVersion), so a quiet world costs little to draw.
// Drawing does not allocate.
type Renderer struct {
	cells *ebiten.Image
	// chunks are the parts of cells that show the world's chunks, row by
	// row; cols is how many a row has.
	chunks []*ebiten.Image
	cols   int
	// pix holds the pixels of the chunk being uploaded.
	pix  []byte
	pal  *palette.Palette
	mats *material.Table
	// seen is the world version cells shows, once drawn is set.
	seen  uint64
	drawn bool
}

// New returns a renderer for a world of width × height cells.
func New(mats *material.Table, width, height int) *Renderer {
	r := &Renderer{
		cells: ebiten.NewImage(width, height),
		cols:  (width + world.ChunkSize - 1) / world.ChunkSize,
		pix:   make([]byte, 4*world.ChunkSize*world.ChunkSize),
		pal:   palette.New(mats),
		mats:  mats,
	}
	for y := 0; y < height; y += world.ChunkSize {
		for x := 0; x < width; x += world.ChunkSize {
			b := image.Rect(x, y, min(x+world.ChunkSize, width), min(y+world.ChunkSize, height))
			// SubImage of an *ebiten.Image is always an *ebiten.Image.
			r.chunks = append(r.chunks, r.cells.SubImage(b).(*ebiten.Image))
		}
	}
	return r
}

// DrawWorld draws the cells of w, one pixel per cell, over the background.
// w must be the world the renderer was made for, since the renderer keeps
// what it drew of it.
func (r *Renderer) DrawWorld(dst *ebiten.Image, w *world.World) {
	r.upload(w)
	dst.Fill(background)
	dst.DrawImage(r.cells, nil)
}

// upload writes into the cell image the chunks of w that changed since the
// last upload, or all of them the first time.
func (r *Renderer) upload(w *world.World) {
	for i, img := range r.chunks {
		if r.drawn && w.ChunkVersion(i%r.cols, i/r.cols) <= r.seen {
			continue
		}
		b := img.Bounds()
		pix := r.pix[:4*b.Dx()*b.Dy()]
		r.pal.FillRect(pix, w, b.Min.X, b.Min.Y, b.Max.X, b.Max.Y)
		img.WritePixels(pix)
	}
	r.seen, r.drawn = w.Version(), true
}

// DrawOverlay draws the brush outline at the cursor, a status line with
// how many chunks the last tick ran, and what the inventory holds of each
// slot's material.
func (r *Renderer) DrawOverlay(dst *ebiten.Image, g *game.Game, cursorX, cursorY int) {
	radius := float32(g.Tool().Radius) + 0.5
	vector.StrokeCircle(dst, float32(cursorX)+0.5, float32(cursorY)+0.5, radius, 1, brushStroke, false)
	var held strings.Builder
	if g.Creative() {
		held.WriteString("creative (places without the inventory)  ")
	}
	for i, id := range g.Slots() {
		fmt.Fprintf(&held, "%d %s %d  ", i+1, r.mats.Def(id).Name, g.Inventory(id))
	}
	w := g.World()
	status := fmt.Sprintf("%s  tick %d  TPS %.0f  FPS %.0f  awake %d/%d chunks\n%s\nLMB place  RMB dig  1-%d material",
		g.Selected().Name, w.Tick(), ebiten.ActualTPS(), ebiten.ActualFPS(), w.Awake(), w.Chunks(), held.String(), len(g.Slots()))
	ebitenutil.DebugPrint(dst, status)
}
