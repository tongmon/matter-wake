package render

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

// Views are the test dashboard's overlays, each on or off. They only draw
// and cost nothing while off.
type Views uint8

const (
	// ViewCell shows the values of the cell under the cursor.
	ViewCell Views = 1 << iota
	// ViewBodies outlines the bodies' collision shapes, the frozen ones in
	// another color.
	ViewBodies
	// ViewChains draws the terrain collision the bodies made.
	ViewChains
	// ViewChunks outlines each chunk in view: awake or asleep, waiting for
	// the budget, and its change rectangle.
	ViewChunks
	// ViewQueue marks the cells waiting in the queue to be put back where
	// they were taken from.
	ViewQueue
)

// ViewNames are the views' words, in bit order: on the panel, in the -dash
// flag and in the status line.
var ViewNames = [...]string{"cell", "bodies", "chains", "chunks", "queue"}

var (
	chunkAwake  = color.RGBA{R: 0x40, G: 0x90, B: 0x40, A: 0xa0}
	chunkAsleep = color.RGBA{R: 0x50, G: 0x50, B: 0x60, A: 0x80}
	chunkHeld   = color.RGBA{R: 0xe0, G: 0x70, B: 0x20, A: 0xe0}
	changeRect  = color.RGBA{R: 0xf0, G: 0xe0, B: 0x30, A: 0xe0}
	bodyEdge    = color.RGBA{R: 0x40, G: 0xff, B: 0x80, A: 0xff}
	frozenEdge  = color.RGBA{R: 0x60, G: 0xa0, B: 0xff, A: 0xff}
	chainEdge   = color.RGBA{R: 0xff, G: 0x40, B: 0xc0, A: 0xff}
	queuedCell  = color.RGBA{R: 0x40, G: 0xf0, B: 0xf0, A: 0xff}
	boxFill     = color.RGBA{R: 0x10, G: 0x10, B: 0x18, A: 0xd8}
	boxStroke   = color.RGBA{R: 0xa0, G: 0xa0, B: 0xb0, A: 0xff}
)

// DrawViews draws the views that are on, other than the cell's, over what
// the camera at (camX, camY) sees of g. The cell's goes on top of
// everything else, with DrawCell.
func (r *Renderer) DrawViews(dst *ebiten.Image, g *game.Game, views Views, camX, camY int) {
	vw, vh := dst.Bounds().Dx(), dst.Bounds().Dy()
	if views&ViewChunks != 0 {
		r.drawChunks(dst, g.World(), camX, camY)
	}
	if views&ViewChains != 0 {
		r.edges = g.TerrainEdges(camX, camY, camX+vw, camY+vh, r.edges[:0])
		r.strokeEdges(dst, r.edges, camX, camY, chainEdge)
	}
	if views&ViewBodies != 0 {
		r.edges, r.frozen = g.BodyEdges(r.edges[:0], r.frozen[:0])
		r.strokeEdges(dst, r.edges, camX, camY, bodyEdge)
		r.strokeEdges(dst, r.frozen, camX, camY, frozenEdge)
	}
	if views&ViewQueue != 0 {
		r.queued = g.World().AppendQueued(r.queued[:0])
		for _, q := range r.queued {
			x, y := float32(int(q.X)-camX), float32(int(q.Y)-camY)
			if x >= -1 && y >= -1 && x <= float32(vw) && y <= float32(vh) {
				vector.FillRect(dst, x, y, 1, 1, queuedCell, false)
			}
		}
	}
}

// drawChunks outlines every chunk of w in memory that the camera at (camX,
// camY) sees: green if it has cells to scan, gray if it sleeps, orange if
// it waited for the budget in the last tick, with its change rectangle in
// yellow.
func (r *Renderer) drawChunks(dst *ebiten.Image, w *world.World, camX, camY int) {
	vw, vh := dst.Bounds().Dx(), dst.Bounds().Dy()
	cx0, cy0 := max(camX, 0)/world.ChunkSize, max(camY, 0)/world.ChunkSize
	cx1 := min(camX+vw, w.Width()-1) / world.ChunkSize
	cy1 := min(camY+vh, w.Height()-1) / world.ChunkSize
	for cy := cy0; cy <= cy1; cy++ {
		for cx := cx0; cx <= cx1; cx++ {
			x, y := cx*world.ChunkSize, cy*world.ChunkSize
			if _, ok := w.At(x, y); !ok {
				continue // its block is not in memory
			}
			x0, y0, x1, y1 := w.ChangeRect(cx, cy)
			edge := chunkAsleep
			switch {
			case w.Held(cx, cy):
				edge = chunkHeld
			case x0 < x1:
				edge = chunkAwake
			}
			sx, sy := float32(x-camX), float32(y-camY)
			vector.StrokeRect(dst, sx+0.5, sy+0.5, world.ChunkSize-1, world.ChunkSize-1, 1, edge, false)
			if x0 < x1 {
				vector.StrokeRect(dst, float32(x0-camX)+0.5, float32(y0-camY)+0.5,
					float32(x1-x0-1), float32(y1-y0-1), 1, changeRect, false)
			}
		}
	}
}

// edgesPerPath is how many edges strokeEdges strokes as one path. The
// vector package counts how often a path covers a pixel in 4 bits, so a
// pixel many edges meet at would vanish from a longer path
// (docs/pitfalls.md); a few edges cover a pixel at most a few times.
const edgesPerPath = 6

// strokeEdges draws edges, in world cells, a few at a time.
func (r *Renderer) strokeEdges(dst *ebiten.Image, edges []game.Edge, camX, camY int, clr color.Color) {
	r.stroke.Width = 1
	r.drawPath.ColorScale.Reset()
	r.drawPath.ColorScale.ScaleWithColor(clr)
	ox, oy := float32(camX), float32(camY)
	for len(edges) > 0 {
		n := min(len(edges), edgesPerPath)
		r.path.Reset()
		for _, e := range edges[:n] {
			r.path.MoveTo(e.X0-ox, e.Y0-oy)
			r.path.LineTo(e.X1-ox, e.Y1-oy)
		}
		vector.StrokePath(dst, &r.path, &r.stroke, &r.drawPath)
		edges = edges[n:]
	}
}

// DrawCell draws the values of the cell at (x, y) of g's world in a box
// beside the screen position (sx, sy), kept on the screen.
func (r *Renderer) DrawCell(dst *ebiten.Image, g *game.Game, x, y, sx, sy int) {
	text := r.cellText(g, x, y)
	lines, cols := 1, 0
	n := 0
	for _, c := range text {
		if c == '\n' {
			lines++
			cols, n = max(cols, n), 0
			continue
		}
		n++
	}
	cols = max(cols, n)
	w, h := cols*GlyphWidth+2*panelPad, lines*LineHeight+2*panelPad
	vw, vh := dst.Bounds().Dx(), dst.Bounds().Dy()
	bx, by := sx+12, sy+12
	if bx+w > vw {
		bx = sx - 12 - w
	}
	if by+h > vh {
		by = sy - 12 - h
	}
	bx, by = max(bx, 0), max(by, 0)
	vector.FillRect(dst, float32(bx), float32(by), float32(w), float32(h), boxFill, false)
	vector.StrokeRect(dst, float32(bx)+0.5, float32(by)+0.5, float32(w-1), float32(h-1), 1, boxStroke, false)
	ebitenutil.DebugPrintAt(dst, text, bx+panelPad-1, by+panelPad)
}

// cellText describes the cell at (x, y): where it is, its material and
// kind, and the values the cell rules keep.
func (r *Renderer) cellText(g *game.Game, x, y int) string {
	c, ok := g.World().At(x, y)
	if !ok {
		return fmt.Sprintf("%d,%d not in memory", x, y)
	}
	d := r.mats.Def(c.Mat)
	text := fmt.Sprintf("%d,%d %s (%s)\n", x, y, d.Name, d.Kind)
	if d.HP > 0 {
		// Damage is the share of hp lost, in 1/DamageScale.
		text += fmt.Sprintf("damage %d.%d%%  ", int(c.Damage)*100/world.DamageScale, int(c.Damage)*1000/world.DamageScale%10)
	}
	switch d.Kind {
	case material.Solid:
		body, pixel := c.Body()
		return text + fmt.Sprintf("\nbody %d pixel %d", body, pixel)
	case material.Powder:
		text += fmt.Sprintf("rest %d", c.Rest)
		if c.Settled() {
			text += " settled"
		}
		return text + fmt.Sprintf("\nfalling %t  vy %s\nlast %d", c.Falling, c.Vy, c.Last)
	case material.Liquid:
		return text + fmt.Sprintf("vy %s  push %s\nlast %d", c.Vy, c.Ax, c.Last)
	case material.Empty, material.Static:
	}
	return text + fmt.Sprintf("tone %d", c.Tone)
}
