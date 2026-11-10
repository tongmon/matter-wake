package rigid

import (
	"math"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/rng"
)

// image parses rows of '#' (a pixel) and '.' (none) into an image.
func image(rows ...string) (px []pixel, w, h int) {
	w, h = len(rows[0]), len(rows)
	px = make([]pixel, w*h)
	for j, row := range rows {
		for i, c := range row {
			px[j*w+i] = heldIf(c == '#')
		}
	}
	return px, w, h
}

// polygon returns the corners of polygon k of sh.
func (sh *shaper) polygon(k int) []point {
	start := 0
	if k > 0 {
		start = sh.ends[k-1]
	}
	return sh.pts[start:sh.ends[k]]
}

// area returns a polygon's area, whichever way it runs.
func area(p []point) float64 {
	a := 0.0
	for k := range p {
		q, r := p[k], p[(k+1)%len(p)]
		a += q.x*r.y - r.x*q.y
	}
	return math.Abs(a) / 2
}

// totalArea returns the area of all of sh's polygons.
func totalArea(sh *shaper) float64 {
	a := 0.0
	for k := range sh.ends {
		a += area(sh.polygon(k))
	}
	return a
}

// inside reports whether (x, y) lies strictly inside convex polygon p.
func inside(p []point, x, y float64) bool {
	sign := 0.0
	for k := range p {
		q, r := p[k], p[(k+1)%len(p)]
		c := (r.x-q.x)*(y-q.y) - (r.y-q.y)*(x-q.x)
		if math.Abs(c) < 1e-9 {
			return false
		}
		if sign == 0 {
			sign = c
		} else if sign*c < 0 {
			return false
		}
	}
	return true
}

// covers returns how many of sh's polygons hold (x, y).
func covers(sh *shaper, x, y float64) int {
	n := 0
	for k := range sh.ends {
		if inside(sh.polygon(k), x, y) {
			n++
		}
	}
	return n
}

// checkPolygons fails the test unless every polygon of sh has 3 to 8
// corners, none repeated, and is convex.
func checkPolygons(t *testing.T, sh *shaper, what string) {
	t.Helper()
	for k := range sh.ends {
		p := sh.polygon(k)
		if len(p) < 3 || len(p) > maxCorners {
			t.Fatalf("%s: polygon %d has %d corners: %v", what, k, len(p), p)
		}
		pos, neg := false, false
		for i := range p {
			a, b, c := p[i], p[(i+1)%len(p)], p[(i+2)%len(p)]
			if a == b {
				t.Fatalf("%s: polygon %d repeats a corner: %v", what, k, p)
			}
			cr := (b.x-a.x)*(c.y-b.y) - (b.y-a.y)*(c.x-b.x)
			pos = pos || cr > 1e-9
			neg = neg || cr < -1e-9
		}
		if pos && neg {
			t.Fatalf("%s: polygon %d is not convex: %v", what, k, p)
		}
		if area(p) < 1e-9 {
			t.Fatalf("%s: polygon %d has no area: %v", what, k, p)
		}
	}
}

func shapeOf(t *testing.T, rows ...string) *shaper {
	t.Helper()
	var sh shaper
	px, w, h := image(rows...)
	sh.shape(px, w, h)
	checkPolygons(t, &sh, strings.Join(rows, "/"))
	return &sh
}

// A rectangle is one polygon of four corners (C34: squares of 3 or more
// pixels keep their area).
func TestShapeOfARectangle(t *testing.T) {
	sh := shapeOf(t, "#####", "#####", "#####")
	if len(sh.ends) != 1 || len(sh.polygon(0)) != 4 || totalArea(sh) != 15 {
		t.Fatalf("a 5×3 rectangle makes %d polygons %v of area %v, want one of 4 corners and area 15", len(sh.ends), sh.pts, totalArea(sh))
	}
	sh = shapeOf(t, "#")
	if len(sh.ends) != 1 || totalArea(sh) != 1 {
		t.Fatalf("one pixel makes %v, want one unit square", sh.pts)
	}
}

// An L is two rectangles and a U three, of the pixels' area (C34,
// DESTRUCTION_BRIDGE:16-18).
func TestShapeOfAnLAndAU(t *testing.T) {
	sh := shapeOf(t, "###...", "###...", "######", "######")
	if len(sh.ends) != 2 || totalArea(sh) != 18 {
		t.Fatalf("an L makes %d polygons of area %v, want 2 of area 18", len(sh.ends), totalArea(sh))
	}
	sh = shapeOf(t, "##..##", "##..##", "######", "######")
	if len(sh.ends) != 3 || totalArea(sh) != 20 {
		t.Fatalf("a U makes %d polygons of area %v, want 3 of area 20", len(sh.ends), totalArea(sh))
	}
}

// A hole is no part of the shapes: a ring is four rectangles around it,
// of the pixels' area (C34: 320 cells, four rectangles, area 320).
func TestShapeLeavesHolesOut(t *testing.T) {
	sh := shapeOf(t,
		"#######",
		"#######",
		"##...##",
		"##...##",
		"##...##",
		"#######",
		"#######",
	)
	if len(sh.ends) != 4 || totalArea(sh) != 40 {
		t.Fatalf("a ring makes %d polygons of area %v, want 4 of area 40", len(sh.ends), totalArea(sh))
	}
	if covers(sh, 3.5, 3.5) != 0 {
		t.Fatal("a polygon covers the hole")
	}
}

// A staircase's outline is simplified: its steps become one slanted side,
// so its shapes are few and their area is not the pixels' (C34: 336 cells
// of stairs make 337).
func TestShapeSimplifiesStairs(t *testing.T) {
	rows := make([]string, 12)
	for j := range rows {
		rows[j] = strings.Repeat("#", j+1) + strings.Repeat(".", 11-j)
	}
	sh := shapeOf(t, rows...)
	if len(sh.ends) > 2 {
		t.Fatalf("a staircase of 12 steps makes %d polygons, want at most 2: %v", len(sh.ends), sh.pts)
	}
	if a := totalArea(sh); a == 78 || math.Abs(a-78) > 12 {
		t.Fatalf("the staircase's 78 pixels make an area of %v, want near it but not it", a)
	}
}

// Two blocks that touch only at a corner are shapes that touch only at
// that corner.
func TestShapeKeepsCornersApart(t *testing.T) {
	sh := shapeOf(t, "##..", "##..", "..##", "..##")
	if totalArea(sh) != 8 || covers(sh, 2, 2) != 0 {
		t.Fatalf("two corner-touching blocks make %v of area %v", sh.pts, totalArea(sh))
	}
}

// For any image the polygons are convex, of at most 8 corners, and do not
// overlap; a pixel whose eight neighbors are pixels too lies inside one,
// and the middle of an empty pixel among empty ones lies in none. Points
// more than the simplification's tolerance from an edge keep their side.
func TestShapesOfRandomImages(t *testing.T) {
	r := rng.New(11)
	var sh shaper
	for round := range 300 {
		w, h := 1+r.IntN(24), 1+r.IntN(24)
		px := make([]pixel, w*h)
		fill := 30 + r.IntN(65)
		for k := range px {
			px[k] = heldIf(r.IntN(100) < fill)
		}
		sh.shape(px, w, h)
		checkPolygons(t, &sh, "random image")
		at := func(i, j int) bool { return i >= 0 && j >= 0 && i < w && j < h && px[j*w+i].ok() }
		for j := -1; j <= h; j++ {
			for i := -1; i <= w; i++ {
				all, none := true, true
				for dj := -1; dj <= 1; dj++ {
					for di := -1; di <= 1; di++ {
						all = all && at(i+di, j+dj)
						none = none && !at(i+di, j+dj)
					}
				}
				n := covers(&sh, float64(i)+0.5, float64(j)+0.5)
				switch {
				case n > 1:
					t.Fatalf("round %d: %d polygons overlap at pixel (%d, %d)", round, n, i, j)
				case all && n == 0:
					t.Fatalf("round %d: pixel (%d, %d) deep in the image is in no polygon", round, i, j)
				case none && n != 0:
					t.Fatalf("round %d: empty pixel (%d, %d) is in a polygon", round, i, j)
				}
			}
		}
		// Overlaps away from pixel centers.
		for range 200 {
			x, y := float64(r.IntN(1<<20))/(1<<20)*float64(w), float64(r.IntN(1<<20))/(1<<20)*float64(h)
			if n := covers(&sh, x, y); n > 1 {
				t.Fatalf("round %d: %d polygons overlap at (%v, %v)", round, n, x, y)
			}
		}
	}
}

// The rectangles of the fallback cover exactly the pixels (C71).
func TestRectanglesCoverThePixels(t *testing.T) {
	r := rng.New(5)
	var sh shaper
	for range 100 {
		w, h := 1+r.IntN(20), 1+r.IntN(20)
		px := make([]pixel, w*h)
		n := 0
		for k := range px {
			px[k] = heldIf(r.IntN(2) == 0)
			if px[k].ok() {
				n++
			}
		}
		sh.rectangles(px, w, h)
		checkPolygons(t, &sh, "rectangles")
		if a := totalArea(&sh); a != float64(n) {
			t.Fatalf("the rectangles of %d pixels have area %v", n, a)
		}
		for j := range h {
			for i := range w {
				if got, want := covers(&sh, float64(i)+0.5, float64(j)+0.5), px[j*w+i].ok(); (got == 1) != want || got > 1 {
					t.Fatalf("pixel (%d, %d) %v is in %d rectangles", i, j, want, got)
				}
			}
		}
	}
}
