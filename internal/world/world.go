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
// the result is the same for any number of workers. Each row's scan
// direction is a hash of (seed, tick, chunk, row) instead, so only the
// cells that move draw from the stream.
//
// A chunk scans only the cells in its change rectangle: those that moved,
// changed or drew a random number in its last run, and those around any
// cell that changed since. A chunk whose rectangle is empty sleeps. The
// skipped cells would not have done anything: an update that does nothing
// decides so from the cell, the two beside it and the three below it, so a
// cell that did nothing last time does nothing again until one of those
// changes, and then it is marked (TestSleepMatchesScanningEverything).
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
//
// A cell is 16 bytes: 14 of fields and 2 of padding, left free on purpose
// until a field needs them (TestCellSize).
type Cell struct {
	// Mat is what the cell holds.
	Mat material.ID
	// Tone varies the cell's color; it is set when the cell is created.
	Tone uint8
	// Falling is set while a powder is in motion: from its first fall until
	// it lies blocked below with both sides blocked.
	Falling bool
	// Last marks the cell done for a tick: the cell moved in it, or a
	// moving cell pushed it out of a place the scan had already passed. A
	// done cell is not updated again in that tick. A cell that stays put
	// is not marked, so a tick leaves a resting cell as it was whether it
	// visits the cell or not.
	Last uint32
	// Vy is the speed of a falling cell, in cells per tick.
	Vy fixed.Q8
	// Ax is a liquid's sideways push; its sign is the direction it flows.
	Ax fixed.Q8
	// Damage is the share of its material's hp the cell has lost, in steps
	// of 1/DamageScale. It moves with the cell and never heals; a new cell
	// starts at 0. Only Wear changes it.
	Damage uint16
}

type chunk struct {
	cx, cy int
	// cells are the chunk's cells.
	cells rect
	// i indexes the world's state of each chunk.
	i int
	// phase is the index of the phase the chunk runs in.
	phase int
	// first is set for the chunks of the first phase of their row, which
	// run before the chunks beside them.
	first bool
}

// job is one chunk's run in a tick.
type job struct {
	c chunk
	// s is the chunk's random stream for the tick.
	s rng.Stream
	// live is what the run scans. It starts as the chunk's change
	// rectangle and grows as cells change, so the run also reaches the
	// cells it has not passed yet that a change may set moving.
	live rect
	// next is what the run marked in its own chunk for the chunk's next
	// run.
	next rect
	// path covers the places the cell being updated has been.
	path rect
	// out covers the changes the run marked outside its chunk. They are
	// merged into the chunks there after the phase.
	out rect
	// Each run writes its job for every cell it changes. The padding keeps
	// two jobs off one cache line, so workers do not slow each other down.
	_ [64]byte
}

// passed reports whether the tick's scan has already been over (x, y), a
// place the cells of the job's chunk can reach below their own row: every
// row of chunks below has run, and so has the job's own row except, while
// the first phase runs, the chunks beside it.
func (j *job) passed(x, y int) bool {
	c := j.c.cells
	return !j.c.first || y >= c.y1 || x >= c.x0 && x < c.x1
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
	// bounds are all the cells.
	bounds rect
	// chunks holds every chunk, row by row; cols is how many a row has.
	chunks []chunk
	cols   int
	// dirty is each chunk's change rectangle: the cells its next run
	// scans. An empty one means the chunk sleeps.
	dirty []rect
	// keep holds what was marked in a chunk earlier in the tick than it
	// runs. The run scans it, and so does the run after.
	keep []rect
	// phases run in order: for each row of chunks from the bottom, its even
	// columns, then its odd columns.
	phases [][]chunk
	// jobs are the runs of the phase being run; stepChunk reads them.
	jobs []job
	// awake is how many chunks the last tick ran.
	awake int
	// scanAll makes every chunk scan all its cells every tick, for tests
	// that check that sleeping changes nothing.
	scanAll bool
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
		bounds: rect{0, 0, cfg.Width, cfg.Height},
		cols:   (cfg.Width + ChunkSize - 1) / ChunkSize,
	}
	rows := (cfg.Height + ChunkSize - 1) / ChunkSize
	for cy := range rows {
		for cx := range w.cols {
			x0, y0 := cx*ChunkSize, cy*ChunkSize
			w.chunks = append(w.chunks, chunk{
				cx: cx, cy: cy, i: len(w.chunks), first: cx%2 == 0,
				cells: rect{x0, y0, min(x0+ChunkSize, cfg.Width), min(y0+ChunkSize, cfg.Height)},
			})
		}
	}
	w.dirty = make([]rect, len(w.chunks))
	w.keep = make([]rect, len(w.chunks))
	for cy := rows - 1; cy >= 0; cy-- {
		for parity := range 2 {
			var phase []chunk
			for cx := parity; cx < w.cols; cx += 2 {
				w.chunks[cy*w.cols+cx].phase = len(w.phases)
				phase = append(phase, w.chunks[cy*w.cols+cx])
			}
			if len(phase) > 0 {
				w.phases = append(w.phases, phase)
				w.jobs = make([]job, max(len(w.jobs), len(phase)))
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

// Chunks returns how many chunks the grid is cut into.
func (w *World) Chunks() int { return len(w.chunks) }

// Awake returns how many chunks the last tick ran. The others slept:
// nothing in them could move.
func (w *World) Awake() int { return w.awake }

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
	w.touch(rect{x, y, x + 1, y + 1})
}

// Fill sets every cell in the half-open rectangle [x0, x1) × [y0, y1) to id.
func (w *World) Fill(x0, y0, x1, y1 int, id material.ID) {
	r := rect{x0, y0, x1, y1}.clip(w.bounds)
	for y := r.y0; y < r.y1; y++ {
		for x := r.x0; x < r.x1; x++ {
			w.cells[y*w.width+x] = Cell{Mat: id, Tone: w.tone(x, y)}
		}
	}
	w.touch(r)
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
	if changed > 0 {
		w.touch(rect{cx - r, cy - r, cx + r + 1, cy + r + 1})
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
	var b [14]byte
	for _, c := range w.cells {
		b[0], b[1] = byte(c.Mat), byte(c.Mat>>8)
		b[2] = c.Tone
		b[3], b[4], b[5], b[6] = byte(c.Last), byte(c.Last>>8), byte(c.Last>>16), byte(c.Last>>24)
		b[7], b[8] = byte(c.Vy), byte(c.Vy>>8)
		b[9], b[10] = byte(c.Ax), byte(c.Ax>>8)
		b[11] = 0
		if c.Falling {
			b[11] = 1
		}
		b[12], b[13] = byte(c.Damage), byte(c.Damage>>8)
		_, _ = h.Write(b[:])
	}
	return h.Sum64()
}

func (w *World) tone(x, y int) uint8 {
	return uint8(rng.Hash3(w.seed, uint64(x), uint64(y), uint64(w.tick)) >> 56)
}

// Step advances the world by one tick. It runs the chunks whose change
// rectangle is not empty, phase by phase, and after each phase merges what
// the runs marked in the chunks around them, so a chunk that runs later in
// the tick already sees it.
func (w *World) Step() {
	w.tick++
	w.awake = 0
	for p, phase := range w.phases {
		n := 0
		for _, c := range phase {
			live := w.dirty[c.i]
			if w.scanAll {
				live = c.cells
			}
			if live.empty() {
				continue
			}
			w.dirty[c.i], w.keep[c.i] = w.keep[c.i], rect{}
			w.jobs[n] = job{c: c, live: live}
			n++
		}
		if n == 0 {
			continue
		}
		w.awake += n
		w.pool.Run(n, w.stepChunkFn)
		// Bounding boxes merge in any order to the same result, so the
		// worker count does not matter.
		for k := range n {
			w.wake(w.jobs[k].out, p)
		}
	}
}

// stepChunk runs one job of the current phase: it updates the cells of its
// rectangle from the bottom row up, each row in a random direction. The
// rectangle may grow as it goes.
func (w *World) stepChunk(i int) {
	j := &w.jobs[i]
	c := j.c
	j.s = rng.New(rng.Hash3(w.seed, uint64(w.tick), uint64(c.cx), uint64(c.cy)))
	for y := j.live.y1 - 1; y >= j.live.y0; y-- {
		if w.scansRight(c.cx, y) {
			for x := j.live.x0; x < j.live.x1; x++ {
				w.update(x, y, j)
			}
		} else {
			for x := j.live.x1 - 1; x >= j.live.x0; x-- {
				w.update(x, y, j)
			}
		}
	}
	w.dirty[c.i].add(j.next)
}

// mark records that the cells in r changed during the job's run, so that
// every cell whose update reads them is updated: in this run if the scan
// has not passed it yet, and in the chunk's next run. The part outside the
// job's chunk waits in j.out until the phase is over.
func (w *World) mark(j *job, r rect) {
	m := r.readers()
	own := m.clip(j.c.cells)
	j.live.add(own)
	j.next.add(own)
	if own != m {
		// Past the world's edge too; wake clips it.
		j.out.add(m)
	}
}

// touch records that the cells in r changed between ticks, so that every
// cell whose update reads them is updated in the next tick.
func (w *World) touch(r rect) {
	w.wake(r.readers(), len(w.phases))
}

// wake adds the cells of r, marked in phase p of a tick, to the change
// rectangles of the chunks they lie in. A chunk that runs later in the
// tick scans them in this tick and the next, as a run does with what it
// marks in its own chunk: a cell that is done for the tick when the scan
// reaches it gets its update in the next one.
func (w *World) wake(r rect, p int) {
	r = r.clip(w.bounds)
	if r.empty() {
		return
	}
	for cy := r.y0 / ChunkSize; cy <= (r.y1-1)/ChunkSize; cy++ {
		for cx := r.x0 / ChunkSize; cx <= (r.x1-1)/ChunkSize; cx++ {
			c := &w.chunks[cy*w.cols+cx]
			part := r.clip(c.cells)
			w.dirty[c.i].add(part)
			if c.phase > p {
				w.keep[c.i].add(part)
			}
		}
	}
}

// rowSalt keys the scan directions apart from the chunks' random streams.
const rowSalt = 0x6a09e667f3bcc909

// scansRight reports whether this tick scans row y of the chunks in column
// cx from left to right. It hashes (seed, tick, chunk, row) rather than
// drawing from the chunk's stream, so a row that is skipped, or that holds
// nothing that can move, shifts no other cell's random numbers
// (TestRowsWithNothingToMoveDrawNoRandomNumbers).
func (w *World) scansRight(cx, y int) bool {
	return rng.Hash3(w.seed^rowSalt, uint64(w.tick), uint64(cx), uint64(y))>>63 == 1
}
