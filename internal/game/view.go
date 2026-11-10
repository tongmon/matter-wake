package game

import "github.com/tongmon/matter-wake/internal/rigid"

// Edge is a line from (X0, Y0) to (X1, Y1), in cells.
type Edge = rigid.Edge

// BodyEdges appends to out the edges of the collision shapes of every body
// in memory, in cells, those of the frozen bodies to frozen and those of
// the static bodies, which never move, to static. It is for the test
// dashboard's drawing and changes nothing.
func (g *Game) BodyEdges(out, frozen, static []Edge) ([]Edge, []Edge, []Edge) {
	return g.bodies.BodyEdges(out, frozen, static)
}

// TerrainEdges appends to out the edges of the terrain collision the
// bodies have made in the chunks that meet the cells [x0, x1) × [y0, y1),
// in cells. It is for the test dashboard's drawing and changes nothing.
func (g *Game) TerrainEdges(x0, y0, x1, y1 int, out []Edge) []Edge {
	return g.bodies.TerrainEdges(x0, y0, x1, y1, out)
}
