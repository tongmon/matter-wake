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

// chunkImage is the picture of one chunk and the world version it shows.
type chunkImage struct {
	img     *ebiten.Image
	version uint64
}

// Renderer draws what the camera sees of one world. It keeps a picture of
// each chunk in view between frames and uploads a chunk again only when
// it changed since (world.World.ChunkVersion), so a quiet world costs
// little to draw. Pictures of chunks that leave the view go back to a
// pool.
type Renderer struct {
	pal  *palette.Palette
	mats *material.Table
	// chunks holds the pictures of the chunks in view, by chunk index;
	// spare holds pictures for reuse.
	chunks map[int]*chunkImage
	spare  []*ebiten.Image
	// pix holds the pixels of the chunk being uploaded.
	pix []byte
	op  ebiten.DrawImageOptions
}

// New returns a renderer for the cells of mats.
func New(mats *material.Table) *Renderer {
	return &Renderer{
		pal:    palette.New(mats),
		mats:   mats,
		chunks: map[int]*chunkImage{},
		pix:    make([]byte, 4*world.ChunkSize*world.ChunkSize),
	}
}

// DrawWorld draws the cells of w that the camera at (camX, camY) sees, one
// pixel per cell, over the background; dst is the view. w must be the
// world the renderer was made for, since the renderer keeps what it drew
// of it. Cells of blocks not in memory are left as background.
func (r *Renderer) DrawWorld(dst *ebiten.Image, w *world.World, camX, camY int) {
	dst.Fill(background)
	vw, vh := dst.Bounds().Dx(), dst.Bounds().Dy()
	cols := (w.Width() + world.ChunkSize - 1) / world.ChunkSize
	cx0, cy0 := max(camX, 0)/world.ChunkSize, max(camY, 0)/world.ChunkSize
	cx1 := min(camX+vw, w.Width()-1) / world.ChunkSize
	cy1 := min(camY+vh, w.Height()-1) / world.ChunkSize
	r.forget(cx0, cy0, cx1, cy1, cols)
	for cy := cy0; cy <= cy1; cy++ {
		for cx := cx0; cx <= cx1; cx++ {
			x, y := cx*world.ChunkSize, cy*world.ChunkSize
			if _, ok := w.At(x, y); !ok {
				continue // its block is not in memory
			}
			c := r.chunk(w, cx, cy, cy*cols+cx)
			r.op.GeoM.Reset()
			r.op.GeoM.Translate(float64(x-camX), float64(y-camY))
			dst.DrawImage(c.img, &r.op)
		}
	}
}

// chunk returns the picture of chunk (cx, cy), the chunk with index i,
// uploading it first if it is new or the chunk changed.
func (r *Renderer) chunk(w *world.World, cx, cy, i int) *chunkImage {
	c, ok := r.chunks[i]
	if !ok {
		c = &chunkImage{img: r.image()}
		r.chunks[i] = c
	} else if w.ChunkVersion(cx, cy) <= c.version {
		return c
	}
	x0, y0 := cx*world.ChunkSize, cy*world.ChunkSize
	x1, y1 := min(x0+world.ChunkSize, w.Width()), min(y0+world.ChunkSize, w.Height())
	// A chunk at the world's edge fills only part of its picture; the
	// rest stays clear.
	clear(r.pix)
	for y := y0; y < y1; y++ {
		r.pal.Fill(r.pix[4*(y-y0)*world.ChunkSize:], w.Row(x0, x1, y))
	}
	c.img.WritePixels(r.pix)
	c.version = w.Version()
	return c
}

// image returns a spare chunk picture or a new one.
func (r *Renderer) image() *ebiten.Image {
	if n := len(r.spare); n > 0 {
		img := r.spare[n-1]
		r.spare = r.spare[:n-1]
		return img
	}
	return ebiten.NewImage(world.ChunkSize, world.ChunkSize)
}

// forget returns to the pool the pictures of chunks outside the chunk
// rectangle [cx0, cx1] × [cy0, cy1] of a world with cols chunks a row.
func (r *Renderer) forget(cx0, cy0, cx1, cy1, cols int) {
	for i, c := range r.chunks {
		if cx, cy := i%cols, i/cols; cx < cx0 || cx > cx1 || cy < cy0 || cy > cy1 {
			r.spare = append(r.spare, c.img)
			delete(r.chunks, i)
		}
	}
}

// DrawOverlay draws the brush outline at the cursor, a status line with
// the camera, the blocks in memory, how many chunks the last tick ran, the
// bodies in memory and how many of them wait frozen for a block, the cells
// waiting to be put back, and what the inventory
// holds of each slot's material. The cursor and the camera are in world
// cells.
func (r *Renderer) DrawOverlay(dst *ebiten.Image, g *game.Game, in game.Input) {
	radius := float32(g.Tool().Radius) + 0.5
	sx, sy := float32(in.CursorX-in.CameraX)+0.5, float32(in.CursorY-in.CameraY)+0.5
	vector.StrokeCircle(dst, sx, sy, radius, 1, brushStroke, false)
	var held strings.Builder
	if g.Creative() {
		held.WriteString("creative (places without the inventory)  ")
	}
	for i, id := range g.Slots() {
		fmt.Fprintf(&held, "%d %s %d  ", i+1, r.mats.Def(id).Name, g.Inventory(id))
	}
	w := g.World()
	status := fmt.Sprintf("%s  tick %d  TPS %.0f  FPS %.0f  awake %d chunks  %d blocks  camera %d,%d\n"+
		"%d bodies (%d frozen)  %d queued\n%s\n"+
		"LMB place  RMB dig  B body  1-%d material  WASD/arrows move (Shift faster)",
		g.Selected().Name, w.Tick(), ebiten.ActualTPS(), ebiten.ActualFPS(), w.Awake(), g.Blocks(),
		in.CameraX, in.CameraY, g.Bodies(), g.Frozen(), w.QueueLen(), held.String(), len(g.Slots()))
	ebitenutil.DebugPrint(dst, status)
}
