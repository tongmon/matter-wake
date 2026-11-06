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
	if amount == 0 || !w.InBounds(x, y) {
		return false
	}
	c := &w.cells[y*w.width+x]
	hp := uint64(w.mats[c.Mat].hp)
	if hp == 0 {
		return true
	}
	// At most 2^32 · 2^16, which fits a uint64.
	steps := (uint64(amount)*DamageScale + hp - 1) / hp
	if uint64(c.Damage)+steps >= DamageScale {
		return true
	}
	c.Damage += uint16(steps)
	return false
}
