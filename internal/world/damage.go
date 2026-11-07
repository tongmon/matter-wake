package world

// DamageScale is how many steps of Cell.Damage make up a material's whole
// hp. Damage is kept as a share of the hp rather than an amount, so one
// 16-bit field serves materials of any hp.
const DamageScale = 1 << 16

// Wear deals amount damage to the cell at (x, y) and reports whether that
// uses it up: whether its damage reaches its material's hp. A cell that is
// not used up keeps the damage, and it moves with the cell. A used-up cell
// is left as it is; what becomes of it is the caller's choice, so the
// caller removes it. Outside the grid Wear does nothing.
//
// The damage is converted to steps of 1/DamageScale of the hp, rounded up,
// so the smallest damage still counts one step. A material of hp 0 is used
// up by any damage. The rounding can save the last wear when hp / amount
// is just above a whole number (TestWearTakesHPOverAmount).
func (w *World) Wear(x, y int, amount uint32) bool {
	if amount == 0 || !w.has(x, y) {
		return false
	}
	c := w.cell(w.pos(x, y))
	damage, gone := Worn(w.mats[c.Mat].hp, c.Damage, amount)
	if gone {
		return true
	}
	c.Damage = damage
	// Damage changes only how the cell is drawn, but marking it is what
	// tells the renderer to draw it again.
	w.touch(rect{x, y, x + 1, y + 1})
	return false
}

// Worn returns the damage, in steps of 1/DamageScale of hp, of something
// of hp hp that had damage and takes amount more, and whether that uses it
// up; then the damage returned means nothing. It is Wear's rule, for
// what keeps its damage outside the grid, such as the pixels of rigid
// bodies. No damage leaves it as it was.
func Worn(hp uint32, damage uint16, amount uint32) (uint16, bool) {
	if amount == 0 {
		return damage, false
	}
	if hp == 0 {
		return damage, true
	}
	// At most 2^32 · 2^16, which fits a uint64.
	steps := (uint64(amount)*DamageScale + uint64(hp) - 1) / uint64(hp)
	if uint64(damage)+steps >= DamageScale {
		return damage, true
	}
	return damage + uint16(steps), false
}
