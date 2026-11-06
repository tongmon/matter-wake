// Package render draws a session with Ebitengine.
package render

import (
	"fmt"
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

// Renderer draws worlds of one size. It keeps the cell image and its
// pixel buffer between frames, so drawing does not allocate.
type Renderer struct {
	cells *ebiten.Image
	pix   []byte
	pal   *palette.Palette
	mats  *material.Table
}

// New returns a renderer for worlds of width × height cells.
func New(mats *material.Table, width, height int) *Renderer {
	return &Renderer{
		cells: ebiten.NewImage(width, height),
		pix:   make([]byte, 4*width*height),
		pal:   palette.New(mats),
		mats:  mats,
	}
}

// DrawWorld draws the cells, one pixel per cell, over the background.
func (r *Renderer) DrawWorld(dst *ebiten.Image, w *world.World) {
	r.pal.Fill(r.pix, w.Cells())
	r.cells.WritePixels(r.pix)
	dst.Fill(background)
	dst.DrawImage(r.cells, nil)
}

// DrawOverlay draws the brush outline at the cursor, a status line and
// what the inventory holds of each slot's material.
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
	status := fmt.Sprintf("%s  tick %d  TPS %.0f  FPS %.0f\n%s\nLMB place  RMB dig  1-%d material",
		g.Selected().Name, g.World().Tick(), ebiten.ActualTPS(), ebiten.ActualFPS(), held.String(), len(g.Slots()))
	ebitenutil.DebugPrint(dst, status)
}
