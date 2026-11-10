package rigid

import (
	"fmt"
	"math"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

// Terrain into bodies (docs/briefs/ice-glass.md). A static material whose
// cells turn into a body (material.Def.Body), as Noita's ice and glass do
// (C136), stays terrain until one of its cells is destroyed: then the
// cells of the material 4-connected to it become one body of the solid it
// names, whole, not only those near the damage. Cells left floating do not
// turn by themselves.

// convertWindow is the side of the square, centered on the cell a region
// is filled from, that the body it turns into is cut to: the region's cells
// outside it stay terrain, to turn when one of theirs is destroyed. 🔶🧪
// Noita's is 384 (C136); a body's image holds MaxPixels, 256², so ours is
// the largest square an image holds.
const convertWindow = 256

// seed is a terrain cell destroyed since the last Convert whose material
// turns into a body.
type seed struct {
	x, y int
	mat  material.ID
}

// neighbors are the cells next to a seed that Convert fills from, in
// Noita's order: below, left, right, above (C136).
var neighbors = [4][2]int{{0, 1}, {-1, 0}, {1, 0}, {0, -1}}

// Destroyed tells s that the terrain cell at (x, y), of material m, was
// destroyed. If m's cells turn into a body, the next Convert turns the
// cells of m 4-connected to it into one.
func (s *System) Destroyed(x, y int, m material.ID) {
	if s.mats.BodyOf(m) != material.Air {
		s.seeds = append(s.seeds, seed{x, y, m})
	}
}

// Convert turns, for each cell Destroyed told of since the last Convert,
// in order, the cells of its material 4-connected to each of its
// neighbors that still holds it (below, left, right, above) into one body
// of the solid the material turns into, within the square of
// convertWindow centered on that neighbor. The cells leave the grid and
// the body is written where they were at once, pixel for cell, with their
// tones; their damage starts anew, as a new material's does. The body is
// static if the solid has an anchor (material.Def.Anchor), as Noita makes
// it (C136), and dynamic otherwise. A region of fewer than minPiece cells
// stays terrain: no body has a single pixel. Convert counts the cells it
// turns (Turned).
func (s *System) Convert() error {
	for _, sd := range s.seeds {
		for _, d := range neighbors {
			if err := s.convertAt(sd.x+d[0], sd.y+d[1], sd.mat); err != nil {
				s.seeds = s.seeds[:0]
				return err
			}
		}
	}
	s.seeds = s.seeds[:0]
	return nil
}

// convertAt turns the cells of material m 4-connected to (x, y), within
// the window centered on it, into a body, if (x, y) holds m.
func (s *System) convertAt(x, y int, m material.ID) error {
	if c, ok := s.w.At(x, y); !ok || c.Mat != m || len(s.bodies) >= maxBodies {
		return nil
	}
	win := rect{
		max(x-convertWindow/2, 0), max(y-convertWindow/2, 0),
		min(x-convertWindow/2+convertWindow, s.w.Width()), min(y-convertWindow/2+convertWindow, s.w.Height()),
	}
	box, n := s.fill(x, y, m, win)
	if n < minPiece {
		return nil
	}
	solid := s.mats.BodyOf(m)
	b := body{
		key: s.nextKey, mat: solid, w: box.x1 - box.x0, h: box.y1 - box.y0,
		static: s.mats.Def(solid).Anchor != material.NoAnchor,
	}
	b.left, b.top = -float32(b.w)/2, -float32(b.h)/2
	b.px = make([]pixel, b.w*b.h)
	b.cells = make([]cellAt, 0, n+b.w+b.h+1)
	b.buoyancy = s.pieceBuoyancy(solid)
	ww := win.x1 - win.x0
	for _, k := range s.filled {
		cx, cy := win.x0+int(k)%ww, win.y0+int(k)/ww
		c, _ := s.w.At(cx, cy)
		b.px[(cy-box.y0)*b.w+cx-box.x0] = pixel{tone: c.Tone, ok: true}
		s.w.Put(cx, cy, world.Cell{})
	}
	b.n = n
	// The middle of the image is a whole cell or a half one, so the cells'
	// centers fall on the pixels' centers, as for a box the body tool
	// drops.
	at := physics.Vec2{
		X: (float32(box.x0) + float32(b.w)/2) / cellsPerMeter,
		Y: (float32(box.y0) + float32(b.h)/2) / cellsPerMeter,
	}
	add := s.phys.AddDynamicBody
	if b.static {
		add = s.phys.AddStaticBody
	}
	id, err := add(at, physics.Rot{C: 1})
	if err != nil {
		return fmt.Errorf("rigid: %w", err)
	}
	b.id = id
	s.nextKey++
	slot := len(s.bodies)
	s.bodies = append(s.bodies, b)
	nb := &s.bodies[slot]
	if err := s.reshape(nb); err != nil {
		return err
	}
	nb.p, nb.q = s.phys.Transform(id)
	s.write(nb, uint16(slot))
	s.turned[m] -= n
	s.turned[solid] += n
	return nil
}

// fill finds the cells of material m 4-connected to (x, y) within win,
// lists them in s.filled by their place in win, row by row, and returns the
// cells around them and how many there are. Cells outside the world or
// memory are not m.
func (s *System) fill(x, y int, m material.ID, win rect) (rect, int) {
	ww, wh := win.x1-win.x0, win.y1-win.y0
	n := ww * wh
	if cap(s.mark) < n {
		s.mark = make([]bool, convertWindow*convertWindow)
	}
	s.mark = s.mark[:n]
	clear(s.mark)
	s.filled = s.filled[:0]
	box := rect{x, y, x + 1, y + 1}
	start := int32((y-win.y0)*ww + x - win.x0)
	s.mark[start] = true
	s.filled = append(s.filled, start)
	for next := 0; next < len(s.filled); next++ {
		k := int(s.filled[next])
		cx, cy := win.x0+k%ww, win.y0+k/ww
		box = rect{min(box.x0, cx), min(box.y0, cy), max(box.x1, cx+1), max(box.y1, cy+1)}
		for _, d := range neighbors {
			nx, ny := cx+d[0], cy+d[1]
			if nx < win.x0 || ny < win.y0 || nx >= win.x1 || ny >= win.y1 {
				continue
			}
			at := (ny-win.y0)*ww + nx - win.x0
			if s.mark[at] {
				continue
			}
			if c, ok := s.w.At(nx, ny); ok && c.Mat == m {
				s.mark[at] = true
				s.filled = append(s.filled, int32(at))
			}
		}
	}
	return box, len(s.filled)
}

// Turned adds to n[id], for every material id, how many cells or pixels of
// it the bodies have turned into another material, less how many they have
// turned into it: terrain turned into a body (Convert), and a body turned
// into powder (shatter). Its sum over the materials is zero. n must have
// an entry for every material.
func (s *System) Turned(n []int) {
	for id, k := range s.turned {
		n[id] += k
	}
}

// pixelCell returns the cell the center of pixel (i, j) of b's image falls
// in, with b's origin at p turned by q.
func pixelCell(b *body, i, j int, p physics.Vec2, q physics.Rot) (int, int) {
	px, py := float32(p.X*cellsPerMeter), float32(p.Y*cellsPerMeter)
	u := b.left + float32(b.cx+i) + 0.5
	v := b.top + float32(b.cy+j) + 0.5
	x := int(math.Floor(float64(px + float32(q.C*u) - float32(q.S*v))))
	y := int(math.Floor(float64(py + float32(q.S*u) + float32(q.C*v))))
	return x, y
}
