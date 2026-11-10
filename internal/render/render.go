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
	// The views' buffers (views.go): the edges to draw, the frozen and the
	// static bodies', the queued cells and the path they are stroked as.
	edges, frozen, static []game.Edge
	queued                []world.Queued
	path                  vector.Path
	stroke                vector.StrokeOptions
	drawPath              vector.DrawPathOptions
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

// Status is what the status line shows besides the session itself.
type Status struct {
	// CursorX, CursorY, CameraX and CameraY are the cursor and the top-left
	// cell of the view, in world cells.
	CursorX, CursorY int
	CameraX, CameraY int
	// Radius and Shape are the brush's, outlined at the cursor; a radius
	// below 0 draws no outline.
	Radius int
	Shape  game.Shape
	// Selected is the material the brush places.
	Selected string
	// Time names the time controls' state when the ticks do not run at
	// full speed, such as paused; empty otherwise.
	Time string
	// Cols is how many characters a line may hold, 0 for any: past it the
	// status wraps.
	Cols int
}

// DrawOverlay draws the brush outline at the cursor, a status line with
// the camera, the blocks in memory, how many chunks the last tick ran, the
// bodies in memory and how many of them wait frozen for a block, the cells
// waiting to be put back, and what the inventory holds of each slot's
// material, and a line of keys.
func (r *Renderer) DrawOverlay(dst *ebiten.Image, g *game.Game, s *Status) {
	sx, sy := float32(s.CursorX-s.CameraX), float32(s.CursorY-s.CameraY)
	switch {
	case s.Radius < 0:
	case s.Shape == game.ShapeSquare:
		side := float32(2*s.Radius + 1)
		vector.StrokeRect(dst, sx-float32(s.Radius), sy-float32(s.Radius), side, side, 1, brushStroke, false)
	default:
		vector.StrokeCircle(dst, sx+0.5, sy+0.5, float32(s.Radius)+0.5, 1, brushStroke, false)
	}
	w := g.World()
	first := []string{s.Selected, fmt.Sprintf("tick %d", w.Tick())}
	if s.Time != "" {
		first = append(first, s.Time)
	}
	first = append(first, fmt.Sprintf("TPS %.0f", ebiten.ActualTPS()), fmt.Sprintf("FPS %.0f", ebiten.ActualFPS()),
		fmt.Sprintf("awake %d chunks", w.Awake()), fmt.Sprintf("%d blocks", g.Blocks()),
		fmt.Sprintf("camera %d,%d", s.CameraX, s.CameraY))
	var held []string
	if g.Creative() {
		held = append(held, "creative (places without the inventory)")
	}
	for i, id := range g.Slots() {
		held = append(held, fmt.Sprintf("%d %s %d", i+1, r.mats.Def(id).Name, g.Inventory(id)))
	}
	keys := []string{
		"LMB place", "RMB dig", "B body", fmt.Sprintf("1-%d material", min(len(g.Slots()), 9)),
		"WASD/arrows move (Shift faster)", "Tab dashboard",
	}
	var b strings.Builder
	for _, line := range [][]string{
		first,
		{fmt.Sprintf("%d bodies (%d frozen)", g.Bodies(), g.Frozen()), fmt.Sprintf("%d queued", w.QueueLen())},
		held,
		keys,
	} {
		wrap(&b, line, s.Cols)
	}
	ebitenutil.DebugPrint(dst, b.String())
}

// wrap writes parts to b two spaces apart, starting a new line where the
// next part would run past cols characters (any, for 0), and ends the
// line.
func wrap(b *strings.Builder, parts []string, cols int) {
	n := 0
	for i, p := range parts {
		switch {
		case i == 0:
		case cols > 0 && n+2+len(p) > cols:
			b.WriteByte('\n')
			n = 0
		default:
			b.WriteString("  ")
			n += 2
		}
		b.WriteString(p)
		n += len(p)
	}
	b.WriteByte('\n')
}
