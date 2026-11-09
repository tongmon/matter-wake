package rigid

import (
	"github.com/tongmon/matter-wake/internal/physics"
	"github.com/tongmon/matter-wake/internal/world"
)

// Edge is a line from (X0, Y0) to (X1, Y1), in cells.
type Edge struct {
	X0, Y0, X1, Y1 float32
}

// BodyEdges appends to out the edges of every body's collision shapes, in
// cells, where Box2D has them now, in key order; frozen bodies' edges go
// to frozen instead. It is for drawing what Box2D collides and changes
// nothing.
func (s *System) BodyEdges(out, frozen []Edge) ([]Edge, []Edge) {
	for i := range s.bodies {
		b := &s.bodies[i]
		if b.frozen {
			frozen = s.edges(b.id, frozen)
		} else {
			out = s.edges(b.id, out)
		}
	}
	return out, frozen
}

// TerrainEdges appends to out the edges of the terrain collision of the
// chunks that meet the cells [x0, x1) × [y0, y1), in cells, in chunk
// order. Only the chunks near a body have any. It changes nothing.
func (s *System) TerrainEdges(x0, y0, x1, y1 int, out []Edge) []Edge {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, s.w.Width()), min(y1, s.w.Height())
	if x0 >= x1 || y0 >= y1 {
		return out
	}
	for cy := y0 / world.ChunkSize; cy <= (y1-1)/world.ChunkSize; cy++ {
		for cx := x0 / world.ChunkSize; cx <= (x1-1)/world.ChunkSize; cx++ {
			if b := s.terrain[cy*s.cols+cx].body; b != 0 {
				out = s.edges(b, out)
			}
		}
	}
	return out
}

// edges appends the edges of Box2D body b to out, in cells.
func (s *System) edges(b physics.Body, out []Edge) []Edge {
	s.scratch = s.phys.Edges(b, s.scratch[:0])
	for _, e := range s.scratch {
		out = append(out, Edge{
			X0: e.X0 * cellsPerMeter, Y0: e.Y0 * cellsPerMeter,
			X1: e.X1 * cellsPerMeter, Y1: e.Y1 * cellsPerMeter,
		})
	}
	return out
}
