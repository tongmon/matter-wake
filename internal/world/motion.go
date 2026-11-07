package world

import (
	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

// SettleTicks is how many ticks a powder must end at rest before it counts
// as settled ground for rigid bodies (Cell.Rest, Classify). 🧪 Noita's
// threshold (docs/briefs/body-grid-coupling.md).
const SettleTicks = 20

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
	// hp is how much damage uses a cell up (Wear).
	hp uint32
}

func propsOf(d *material.Def) matProps {
	return matProps{
		hp:          d.HP,
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

// spot is where a moving cell is: where it is kept (World.pos) and its
// coordinates.
type spot struct{ i, x, y int }

// update moves one cell by updatePowder or updateLiquid. A cell that cannot
// move draws no random numbers, so skipping it changes nothing else in its
// chunk (TestRestingCellsDrawNoRandomNumbers). A cell that moved, changed
// or drew a random number marks where it has been, so it and the cells
// around it are updated again.
func (w *World) update(x, y int, j *job) {
	i := w.pos(x, y)
	c := w.cell(i)
	if c.Mat == material.Air {
		return // most cells; skip the material lookup
	}
	p := &w.mats[c.Mat]
	switch p.kind {
	case material.Empty, material.Static, material.Solid:
		return
	case material.Powder, material.Liquid:
	}
	if c.Last == w.tick {
		return
	}
	before, s := *c, j.s
	j.path = rect{x, y, x + 1, y + 1}
	at := spot{i, x, y}
	if p.kind == material.Liquid {
		w.updateLiquid(at, p, j)
	} else {
		w.updatePowder(at, p, j)
	}
	// A cell that moved left another one at i.
	if *c != before || j.s != s {
		w.mark(j, j.path)
	}
}

// updatePowder moves a powder. A falling powder now and then pauses for a
// tick (pauses). One that starts to fall from rest moves one cell without
// speeding up; after that it falls by fall. One blocked below keeps its
// speed and may slide down diagonally. It comes to rest, no longer falling,
// once both sides are blocked as well. Each update that ends at rest
// without moving counts a tick of Rest, up to SettleTicks; moving resets it
// (swap).
func (w *World) updatePowder(at spot, p *matProps, j *job) {
	c := w.cell(at.i)
	if c.Falling && w.mayMove(at, p) && pauses(p, &j.s) {
		return
	}
	if !c.Falling {
		if w.enterable(at.x, at.y+1, p, &j.s) {
			k := w.swap(at.i, at.x, at.y+1, j)
			moved := w.cell(k)
			moved.Vy, moved.Falling = 0, true
			return
		}
	} else if _, fell := w.fall(at, p, j); fell {
		return
	}
	if w.slide(at, p, j) {
		return
	}
	if !w.isEmpty(at.x-1, at.y) && !w.isEmpty(at.x+1, at.y) {
		c.Falling = false
	}
	if !c.Falling && c.Rest < SettleTicks {
		c.Rest++
	}
}

// mayMove reports, without drawing random numbers, whether a powder could
// move: whether it might enter the cell below or, if it slides at all, one
// of the cells diagonally below.
func (w *World) mayMove(at spot, p *matProps) bool {
	return w.entryTo(at.x, at.y+1, p) != blocked ||
		p.slideChance > 0 && (w.entryTo(at.x-1, at.y+1, p) != blocked || w.entryTo(at.x+1, at.y+1, p) != blocked)
}

// pauses reports whether a falling powder sits out this tick. The weaker it
// accelerates, the more often it pauses: with fallAccel a it pauses with
// chance (1 − 4a) / 2, so never once a is a quarter cell per tick or more.
func pauses(p *matProps, s *rng.Stream) bool {
	n := int(fixed.One) - 4*int(p.fallAccel)
	return n > 0 && s.IntN(2*int(fixed.One)) < n
}

// updateLiquid falls if it can, then flows sideways from wherever it ended
// up, in the same update. Only a liquid that did neither and has no
// sideways push left slides down diagonally. A liquid blocked below loses
// its speed.
func (w *World) updateLiquid(at spot, p *matProps, j *job) {
	at, fell := w.fall(at, p, j)
	if !fell {
		w.cell(at.i).Vy = 0
	}
	blocked := !fell || !w.enterable(at.x, at.y+1, p, &j.s)
	at, flowed := w.flow(at, p, blocked, j)
	if fell || flowed || w.cell(at.i).Ax.Abs() >= fixed.One {
		return
	}
	w.slide(at, p, j)
}

// entry is whether a moving cell may move into a position.
type entry uint8

const (
	blocked entry = iota // never
	free                 // always: the position is empty
	sink                 // with the mover's sink chance: a lighter liquid
)

// entryTo tells whether a moving cell with values p may move into (x, y).
// It draws no random numbers.
func (w *World) entryTo(x, y int, p *matProps) entry {
	if !w.InBounds(x, y) {
		return blocked
	}
	t := &w.mats[w.cell(w.pos(x, y)).Mat]
	switch t.kind {
	case material.Empty:
		return free
	case material.Liquid:
		if t.density < p.density && p.sinkChance > 0 {
			return sink
		}
	case material.Static, material.Powder, material.Solid:
	}
	return blocked
}

// enterable reports whether a moving cell with values p moves into (x, y)
// now: into empty space always, into a lighter liquid with p's sink
// chance, never into anything else.
func (w *World) enterable(x, y int, p *matProps, s *rng.Stream) bool {
	switch w.entryTo(x, y, p) {
	case free:
		return true
	case sink:
		return chance(s, p.sinkChance)
	case blocked:
	}
	return false
}

func (w *World) isEmpty(x, y int) bool {
	return w.InBounds(x, y) && w.mats[w.cell(w.pos(x, y)).Mat].kind == material.Empty
}

// swap moves the cell at index i to (x, y), the cell there to i, and
// returns the index the moving cell went to. The moving cell is then done
// for the tick (Cell.Last) and no longer at rest, and so is the cell it
// pushes out if the scan has passed where that cell was: it had its update
// there, moving or not (TestDisplacedCellIsNotUpdatedTwice).
func (w *World) swap(i, x, y int, j *job) int {
	k := w.pos(x, y)
	from, to := w.cell(i), w.cell(k)
	*from, *to = *to, *from
	to.Last, to.Rest = w.tick, 0
	if from.Mat != material.Air && j.passed(x, y) {
		from.Last = w.tick
	}
	j.path.add(rect{x, y, x + 1, y + 1})
	return k
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
// Hitting something mid-fall, drifted or not, stops it with speed 0. It
// returns where the cell ended and whether it fell; a cell that could not
// fall keeps its speed.
func (w *World) fall(at spot, p *matProps, j *job) (spot, bool) {
	if !w.enterable(at.x, at.y+1, p, &j.s) {
		return at, false
	}
	v := w.cell(at.i).Vy.Add(p.fallAccel).Clamp(0, maxFall)
	push := w.cell(at.i).Ax
	at.i = w.swap(at.i, at.x, at.y+1, j)
	at.y++
	drift := 0
	for left := v.Sub(fixed.One); left > 0; left = left.Sub(2 * fixed.One) {
		if j.s.Bool() {
			drift = pickDrift(push, &j.s)
		}
		nx := at.x + drift
		if !w.enterable(nx, at.y+1, p, &j.s) {
			v = 0
			break
		}
		at.i = w.swap(at.i, nx, at.y+1, j)
		at.x, at.y = nx, at.y+1
	}
	w.cell(at.i).Vy = v
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
func (w *World) flow(at spot, p *matProps, blocked bool, j *job) (spot, bool) {
	push := w.cell(at.i).Ax
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
		at.i = w.swap(at.i, at.x+dir, at.y, j)
		at.x += dir
		moved = true
		push = push.Mul(p.flowDamping)
	}
	w.cell(at.i).Ax = push
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
// trying a random side first, and reports whether it moved. A powder
// slides only with its slide chance, which sets how fast its heaps spread
// (a settled heap is 45° whatever the chance), and is falling afterwards.
// A liquid that slides goes on flowing that way: its push becomes one cell
// in that direction. With neither diagonal open it draws no random numbers.
func (w *World) slide(at spot, p *matProps, j *job) bool {
	if w.entryTo(at.x-1, at.y+1, p) == blocked && w.entryTo(at.x+1, at.y+1, p) == blocked {
		return false
	}
	if p.kind == material.Powder && !chance(&j.s, p.slideChance) {
		return false
	}
	dir := 1
	if j.s.Bool() {
		dir = -1
	}
	for _, d := range [2]int{dir, -dir} {
		if w.enterable(at.x+d, at.y+1, p, &j.s) {
			k := w.swap(at.i, at.x+d, at.y+1, j)
			if p.kind == material.Liquid {
				w.cell(k).Ax = fixed.FromInt(d)
			} else {
				w.cell(k).Falling = true
			}
			return true
		}
	}
	return false
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
