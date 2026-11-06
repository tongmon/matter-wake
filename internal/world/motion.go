package world

import (
	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// Motion limits (🧪). They also bound how far one cell update reaches:
// a fall at maxFall moves at most fallReach cells sideways, and a liquid
// then flows at most its dispersion further. Both must stay within MaxReach
// (TestMotionStaysWithinReach, New).
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

// spot is where a moving cell is: its index and coordinates.
type spot struct{ i, x, y int }

// update moves one cell. A powder falls if it can and otherwise may slide
// down diagonally; a liquid follows updateLiquid.
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
	at := spot{i, x, y}
	if p.kind == material.Liquid {
		w.updateLiquid(at, p, s)
		return
	}
	if _, fell := w.fall(at, p, s); !fell {
		w.slide(at, p, s)
	}
}

// updateLiquid falls if it can, then flows sideways from wherever it ended
// up, in the same update. Only a liquid that did neither and has no
// sideways push left slides down diagonally.
func (w *World) updateLiquid(at spot, p *matProps, s *rng.Stream) {
	at, fell := w.fall(at, p, s)
	blocked := !fell || !w.enterable(at.x, at.y+1, p, s)
	at, flowed := w.flow(at, p, blocked)
	if fell || flowed || w.cells[at.i].Ax.Abs() >= fixed.One {
		return
	}
	w.slide(at, p, s)
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
// Hitting something mid-fall stops it. A powder whose drift is blocked
// falls on straight down if it can; a liquid stops there. It returns where
// the cell ended and whether it fell.
func (w *World) fall(at spot, p *matProps, s *rng.Stream) (spot, bool) {
	if !w.enterable(at.x, at.y+1, p, s) {
		w.cells[at.i].Vy = 0
		return at, false
	}
	v := w.cells[at.i].Vy.Add(p.fallAccel).Clamp(0, maxFall)
	push := w.cells[at.i].Ax
	at.i = w.swap(at.i, at.x, at.y+1)
	at.y++
	drift := 0
	for left := v.Sub(fixed.One); left > 0; left = left.Sub(2 * fixed.One) {
		if s.Bool() {
			drift = pickDrift(push, s)
		}
		nx := at.x + drift
		if !w.enterable(nx, at.y+1, p, s) {
			if p.kind == material.Liquid || nx == at.x || !w.enterable(at.x, at.y+1, p, s) {
				v = 0
				break
			}
			nx = at.x
		}
		at.i = w.swap(at.i, nx, at.y+1)
		at.x, at.y = nx, at.y+1
	}
	w.cells[at.i].Vy = v
	return at, true
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

// flow moves a liquid sideways. The liquid keeps a sideways push from tick
// to tick, so it flows on in one direction instead of choosing anew every
// tick. While the liquid cannot move down, the push grows (growPush). While
// the push is at least one, the liquid moves one cell that way, up to its
// dispersion, and the push is damped after each cell. Something in the way
// stops the liquid but keeps the push. It returns where the liquid ended
// and whether it moved.
func (w *World) flow(at spot, p *matProps, blocked bool) (spot, bool) {
	push := w.cells[at.i].Ax
	if blocked {
		push = w.growPush(push, at.x, at.y, p)
	}
	moved := false
	for range p.dispersion {
		if push.Abs() < fixed.One {
			break
		}
		dir := push.Sign()
		if !w.isEmpty(at.x+dir, at.y) {
			break
		}
		at.i = w.swap(at.i, at.x+dir, at.y)
		at.x += dir
		moved = true
		push = push.Mul(p.flowDamping)
	}
	w.cells[at.i].Ax = push
	return at, moved
}

// growPush adds flowSpeed to a liquid's push toward its only open side. With
// both sides open or both blocked the push stays as it is.
func (w *World) growPush(push fixed.Q8, x, y int, p *matProps) fixed.Q8 {
	left, right := w.isEmpty(x-1, y), w.isEmpty(x+1, y)
	switch {
	case left && !right:
		push = push.Sub(p.flowSpeed)
	case right && !left:
		push = push.Add(p.flowSpeed)
	}
	return push.Clamp(-maxPush, maxPush)
}

// slide moves a cell blocked below one cell down to the left or right,
// trying a random side first. A powder slides only with its slide chance,
// which sets how steep its heaps are. A liquid that slides goes on flowing
// that way: its push becomes one cell in that direction.
func (w *World) slide(at spot, p *matProps, s *rng.Stream) {
	if p.kind == material.Powder && !chance(s, p.slideChance) {
		return
	}
	dir := 1
	if s.Bool() {
		dir = -1
	}
	for _, d := range [2]int{dir, -dir} {
		if w.enterable(at.x+d, at.y+1, p, s) {
			j := w.swap(at.i, at.x+d, at.y+1)
			if p.kind == material.Liquid {
				w.cells[j].Ax = fixed.FromInt(d)
			}
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

// liquidReach is how far sideways one update of a liquid with the given
// dispersion can read or write: a fall's drift, then its sideways flow,
// which always looks at least one cell to each side.
func liquidReach(dispersion uint8) int {
	return fallReach() + max(1, int(dispersion))
}
