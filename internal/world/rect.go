package world

// rect is a half-open rectangle of cells, [x0, x1) × [y0, y1). It is empty
// when it has no width or no height; the zero value is empty.
type rect struct{ x0, y0, x1, y1 int }

func (r rect) empty() bool {
	return r.x0 >= r.x1 || r.y0 >= r.y1
}

// has reports whether cell (x, y) lies in r.
func (r rect) has(x, y int) bool {
	return x >= r.x0 && x < r.x1 && y >= r.y0 && y < r.y1
}

// add grows r to cover o as well.
func (r *rect) add(o rect) {
	switch {
	case o.empty():
	case r.empty():
		*r = o
	default:
		r.x0, r.y0 = min(r.x0, o.x0), min(r.y0, o.y0)
		r.x1, r.y1 = max(r.x1, o.x1), max(r.y1, o.y1)
	}
}

// clip returns the part of r inside o, which may be empty.
func (r rect) clip(o rect) rect {
	return rect{max(r.x0, o.x0), max(r.y0, o.y0), min(r.x1, o.x1), min(r.y1, o.y1)}
}

// readers returns the cells whose update may change when a cell of r
// changes. An update that does nothing decides so from its own cell, the
// two beside it and the three below it, so these are r widened by one cell
// on each side and grown by one row up.
func (r rect) readers() rect {
	return rect{r.x0 - 1, r.y0 - 1, r.x1 + 1, r.y1}
}

// area returns how many cells r covers, 0 for an empty one.
func (r rect) area() int {
	if r.empty() {
		return 0
	}
	return (r.x1 - r.x0) * (r.y1 - r.y0)
}

// corners returns r as x0, y0, x1, y1, all zero when r is empty.
func (r rect) corners() [4]int {
	if r.empty() {
		return [4]int{}
	}
	return [4]int{r.x0, r.y0, r.x1, r.y1}
}

// rectOf returns the rectangle with corners a, the empty rectangle when a
// covers no cell.
func rectOf(a [4]int) rect {
	r := rect{a[0], a[1], a[2], a[3]}
	if r.empty() {
		return rect{}
	}
	return r
}
