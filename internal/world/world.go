// Package world holds the cell grid and advances it one tick at a time.
//
// The grid is cut into 64×64 chunks. A tick updates the rows of chunks
// from the bottom up, so a falling body moves as one piece: the chunk below
// a boundary has always made room before the chunk above it runs. Each
// row of chunks runs in two phases, the even columns and then the odd
// ones, so chunks of one phase are a whole chunk apart. As long as no cell
// update reaches more than MaxReach cells outside its chunk, chunks of one
// phase never touch the same cell and run in parallel. Each chunk draws
// its random numbers from its own stream keyed by (seed, tick, chunk), so
// the result is the same for any number of workers.
package world

import (
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/sched"
)

// ChunkSize is the side of the square chunks a tick is scheduled in.
const ChunkSize = 64

// MaxReach is the furthest a cell update may read or write outside the
// chunk it belongs to. It keeps two chunks of one phase apart.
const MaxReach = ChunkSize/2 - 1

// Cell is one grid position. The zero value is air.
type Cell struct {
	// Mat is what the cell holds.
	Mat material.ID
	// Tone varies the cell's color; it is set when the cell is created.
	Tone uint8
	// Last is the tick in which the cell was last updated, so a cell that
	// moved ahead of the scan is not updated twice in one tick.
	Last uint32
	// Vy is the speed of a falling cell, in cells per tick.
	Vy fixed.Q8
	// Ax is a liquid's sideways push; its sign is the direction it flows.
	Ax fixed.Q8
}

type chunk struct {
	cx, cy         int
	x0, y0, x1, y1 int
}

// Config describes a new world.
type Config struct {
	Width, Height int
	// Seed keys every random choice the world makes.
	Seed uint64
	// Workers is how many goroutines a tick may use; 1 or less runs on the
	// caller's goroutine.
	Workers   int
	Materials *material.Table
}

// World is a grid of cells. It is not safe for concurrent use.
type World struct {
	width, height int
	cells         []Cell
	tick          uint32
	seed          uint64
	// mats holds every material's values. Indexing an array of
	// MaxMaterials entries with a material.ID needs no bounds check.
	mats *[material.MaxMaterials]matProps
	pool *sched.Pool
	// phases run in order: for each row of chunks from the bottom, its even
	// columns, then its odd columns.
	phases [][]chunk
	// current is the phase being run; stepChunk reads it.
	current []chunk
	// stepChunkFn is stepChunk bound once, so Step does not allocate.
	stepChunkFn func(int)
}

// New returns a world filled with air.
func New(cfg Config) (*World, error) {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("world: size %dx%d is not positive", cfg.Width, cfg.Height)
	}
	if cfg.Materials == nil {
		return nil, errors.New("world: no material table")
	}
	mats := new([material.MaxMaterials]matProps)
	for i := range cfg.Materials.Len() {
		d := cfg.Materials.Def(material.ID(i))
		if r := liquidReach(d.Dispersion); d.Kind == material.Liquid && r > MaxReach {
			return nil, fmt.Errorf("world: material %q disperses %d cells, which with a fall's drift reaches %d, at most %d",
				d.Name, d.Dispersion, r, MaxReach)
		}
		mats[i] = propsOf(&d)
	}
	w := &World{
		width:  cfg.Width,
		height: cfg.Height,
		cells:  make([]Cell, cfg.Width*cfg.Height),
		seed:   cfg.Seed,
		mats:   mats,
		pool:   sched.New(cfg.Workers),
	}
	rows := (cfg.Height + ChunkSize - 1) / ChunkSize
	for cy := rows - 1; cy >= 0; cy-- {
		for parity := range 2 {
			var phase []chunk
			for cx := parity; cx*ChunkSize < cfg.Width; cx += 2 {
				c := chunk{cx: cx, cy: cy, x0: cx * ChunkSize, y0: cy * ChunkSize}
				c.x1 = min(c.x0+ChunkSize, cfg.Width)
				c.y1 = min(c.y0+ChunkSize, cfg.Height)
				phase = append(phase, c)
			}
			if len(phase) > 0 {
				w.phases = append(w.phases, phase)
			}
		}
	}
	w.stepChunkFn = w.stepChunk
	return w, nil
}

// Close releases the world's worker goroutines.
func (w *World) Close() {
	w.pool.Close()
}

// Width returns the number of columns.
func (w *World) Width() int { return w.width }

// Height returns the number of rows.
func (w *World) Height() int { return w.height }

// Tick returns how many ticks have run.
func (w *World) Tick() uint32 { return w.tick }

// Cells returns the grid in row-major order. Callers must not modify it.
func (w *World) Cells() []Cell { return w.cells }

// InBounds reports whether (x, y) is inside the grid.
func (w *World) InBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < w.width && y < w.height
}

// At returns the cell at (x, y), or false if it is outside the grid.
func (w *World) At(x, y int) (Cell, bool) {
	if !w.InBounds(x, y) {
		return Cell{}, false
	}
	return w.cells[y*w.width+x], true
}

// Set puts a new cell of material id at (x, y). Positions outside the grid
// are ignored.
func (w *World) Set(x, y int, id material.ID) {
	if !w.InBounds(x, y) {
		return
	}
	w.cells[y*w.width+x] = Cell{Mat: id, Tone: w.tone(x, y)}
}

// Fill sets every cell in the half-open rectangle [x0, x1) × [y0, y1) to id.
func (w *World) Fill(x0, y0, x1, y1 int, id material.ID) {
	for y := max(y0, 0); y < min(y1, w.height); y++ {
		for x := max(x0, 0); x < min(x1, w.width); x++ {
			w.cells[y*w.width+x] = Cell{Mat: id, Tone: w.tone(x, y)}
		}
	}
}

// Paint sets the cells within radius r of (cx, cy) to id and returns how
// many cells changed. With onlyEmpty it changes only empty cells.
func (w *World) Paint(cx, cy, r int, id material.ID, onlyEmpty bool) int {
	changed := 0
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy > r*r || !w.InBounds(x, y) {
				continue
			}
			c := &w.cells[y*w.width+x]
			if c.Mat == id || (onlyEmpty && w.mats[c.Mat].kind != material.Empty) {
				continue
			}
			*c = Cell{Mat: id, Tone: w.tone(x, y)}
			changed++
		}
	}
	return changed
}

// Count returns how many cells hold id.
func (w *World) Count(id material.ID) int {
	n := 0
	for _, c := range w.cells {
		if c.Mat == id {
			n++
		}
	}
	return n
}

// Checksum hashes the whole grid. Equal grids give equal checksums, so
// tests use it to compare runs.
func (w *World) Checksum() uint64 {
	h := fnv.New64a()
	var b [11]byte
	for _, c := range w.cells {
		b[0], b[1] = byte(c.Mat), byte(c.Mat>>8)
		b[2] = c.Tone
		b[3], b[4], b[5], b[6] = byte(c.Last), byte(c.Last>>8), byte(c.Last>>16), byte(c.Last>>24)
		b[7], b[8] = byte(c.Vy), byte(c.Vy>>8)
		b[9], b[10] = byte(c.Ax), byte(c.Ax>>8)
		_, _ = h.Write(b[:])
	}
	return h.Sum64()
}

func (w *World) tone(x, y int) uint8 {
	return uint8(rng.Hash3(w.seed, uint64(x), uint64(y), uint64(w.tick)) >> 56)
}

// Step advances the world by one tick.
func (w *World) Step() {
	w.tick++
	for _, phase := range w.phases {
		w.current = phase
		w.pool.Run(len(phase), w.stepChunkFn)
	}
	w.current = nil
}

// stepChunk updates the cells of one chunk of the current phase, from the
// bottom row up, each row in a random direction.
func (w *World) stepChunk(i int) {
	c := w.current[i]
	s := rng.New(rng.Hash3(w.seed, uint64(w.tick), uint64(c.cx), uint64(c.cy)))
	for y := c.y1 - 1; y >= c.y0; y-- {
		if s.Bool() {
			for x := c.x0; x < c.x1; x++ {
				w.update(x, y, &s)
			}
		} else {
			for x := c.x1 - 1; x >= c.x0; x-- {
				w.update(x, y, &s)
			}
		}
	}
}
