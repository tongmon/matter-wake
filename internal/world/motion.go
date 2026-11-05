package world

import (
	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// Motion limits (🧪). They also bound how far one cell update reaches:
// a fall at maxFall moves at most fallReach cells sideways, which must stay
// within MaxReach (TestMotionStaysWithinReach).
const (
	// maxFall caps falling speed, in cells per tick.
	maxFall = 12 * fixed.One
	// maxPush caps a liquid's stored sideways push.
	maxPush = 8 * fixed.One
)

// matProps are one material's values, laid out for the update loop.
type matProps struct {
	kind        material.Kind
	density     uint8
	sinkChance  uint8
	slideChance uint8
	dispersion  uint8
	fallAccel   fixed.Q8
	flowSpeed   fixed.Q8
	flowDamping fixed.Q8
}

func propsOf(d *material.Def) matProps {
	return matProps{
		kind:        d.Kind,
		density:     d.Density,
		sinkChance:  d.SinkChance,
		slideChance: d.SlideChance,
		dispersion:  d.Dispersion,
		fallAccel:   d.FallAccel,
		flowSpeed:   d.FlowSpeed,
		flowDamping: d.FlowDamping,
	}
}

// update moves one cell: it falls if it can, a liquid flows sideways if it
// cannot fall, and a cell that did neither may slide down diagonally.
func (w *World) update(x, y int, s *rng.Stream) {
	i := y*w.width + x
	c := &w.cells[i]
	if c.Mat == material.Air {
		return // most cells; skip the material lookup
	}
	p := &w.mats[c.Mat]
	switch p.kind {
	case material.Empty, material.Static:
		return
	case material.Powder, material.Liquid:
	}
	if c.Last == w.tick {
		return
	}
	c.Last = w.tick
	if w.fall(i, x, y, p, s) {
		return
	}
	if p.kind == material.Liquid && w.flow(i, x, y, p, s) {
		return
	}
	w.slide(i, x, y, p, s)
}

// enterable reports whether a moving cell with values p may move into
// (x, y): into empty space always, into a lighter liquid with p's sink
// chance, never into anything else.
func (w *World) enterable(x, y int, p *matProps, s *rng.Stream) bool {
	if !w.InBounds(x, y) {
		return false
	}
	t := &w.mats[w.cells[y*w.width+x].Mat]
	switch t.kind {
	case material.Empty:
		return true
	case material.Liquid:
		return t.density < p.density && chance(s, p.sinkChance)
	case material.Static, material.Powder:
	}
	return false
}

func (w *World) isEmpty(x, y int) bool {
	return w.InBounds(x, y) && w.mats[w.cells[y*w.width+x].Mat].kind == material.Empty
}

// swap exchanges the cell at index i with the cell at (x, y) and returns
// the index the cell from i moved to.
func (w *World) swap(i, x, y int) int {
	j := y*w.width + x
	w.cells[i], w.cells[j] = w.cells[j], w.cells[i]
	return j
}

// chance returns true with the given percent probability. It draws a
// random number only when the outcome is not certain.
func chance(s *rng.Stream, percent uint8) bool {
	switch {
	case percent >= 100:
		return true
	case percent == 0:
		return false
	default:
		return s.IntN(100) < int(percent)
	}
}

// fall moves a cell down when it can, faster the longer it falls: its speed
// grows by fallAccel each tick, and at speed v it moves one cell plus one
// more for every two whole units of v beyond the first. During those extra
// cells it now and then picks a sideways drift, the way a stream frays.
// Hitting something mid-fall stops it.
func (w *World) fall(i, x, y int, p *matProps, s *rng.Stream) bool {
	if !w.enterable(x, y+1, p, s) {
		w.cells[i].Vy = 0
		return false
	}
	v := w.cells[i].Vy.Add(p.fallAccel).Clamp(0, maxFall)
	push := w.cells[i].Ax
	i = w.swap(i, x, y+1)
	y++
	drift := 0
	for left := v.Sub(fixed.One); left > 0; left = left.Sub(2 * fixed.One) {
		if s.Bool() {
			drift = pickDrift(push, s)
		}
		nx := x + drift
		if !w.enterable(nx, y+1, p, s) {
			if nx == x || !w.enterable(x, y+1, p, s) {
				v = 0
				break
			}
			nx = x
		}
		i = w.swap(i, nx, y+1)
		x, y = nx, y+1
	}
	w.cells[i].Vy = v
	return true
}

// pickDrift chooses a falling cell's sideways drift: the direction a liquid
// was flowing, or else mostly straight down with a small chance either way.
func pickDrift(push fixed.Q8, s *rng.Stream) int {
	if push.Abs() >= fixed.One {
		return push.Sign()
	}
	switch s.IntN(8) {
	case 0:
		return -1
	case 1:
		return 1
	default:
		return 0
	}
}

// flow moves a liquid that cannot fall sideways. It keeps a sideways push
// from tick to tick, so it flows on in one direction instead of choosing
// anew every tick: the push grows by flowSpeed each tick toward an open
// side, moves the liquid one cell while it is at least one, and is damped
// after each cell. Hitting something takes the push away.
func (w *World) flow(i, x, y int, p *matProps, s *rng.Stream) bool {
	left, right := w.isEmpty(x-1, y), w.isEmpty(x+1, y)
	push := w.cells[i].Ax
	switch {
	case !left && !right:
		w.cells[i].Ax = 0
		return false
	case left && !right:
		push = min(push, 0).Sub(p.flowSpeed)
	case right && !left:
		push = max(push, 0).Add(p.flowSpeed)
	case push > 0:
		push = push.Add(p.flowSpeed)
	case push < 0:
		push = push.Sub(p.flowSpeed)
	case s.Bool():
		push = p.flowSpeed
	default:
		push = -p.flowSpeed
	}
	push = push.Clamp(-maxPush, maxPush)
	moved := false
	for range p.dispersion {
		if push.Abs() < fixed.One {
			break
		}
		dir := push.Sign()
		if !w.isEmpty(x+dir, y) {
			push = 0
			break
		}
		i = w.swap(i, x+dir, y)
		x += dir
		moved = true
		push = push.Mul(p.flowDamping)
	}
	w.cells[i].Ax = push
	return moved
}

// slide moves a cell blocked below one cell down to the left or right. A
// liquid tries the way it was flowing first; a powder picks at random and
// slides only with its slide chance, which sets how steep its heaps are.
func (w *World) slide(i, x, y int, p *matProps, s *rng.Stream) {
	if p.kind == material.Powder && !chance(s, p.slideChance) {
		return
	}
	dir := w.cells[i].Ax.Sign()
	if dir == 0 {
		dir = 1
		if s.Bool() {
			dir = -1
		}
	}
	for _, d := range [2]int{dir, -dir} {
		if w.enterable(x+d, y+1, p, s) {
			w.swap(i, x+d, y+1)
			return
		}
	}
}

// fallReach is how far sideways a fall at maxFall can drift: one cell for
// each extra cell it moves.
func fallReach() int {
	steps := 0
	for left := maxFall.Sub(fixed.One); left > 0; left = left.Sub(2 * fixed.One) {
		steps++
	}
	return steps
}
