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
//
// A tick may scan at most a budget of cells (Config.Budget). When the
// change rectangles of the chunks that would run hold more, the chunks
// nearest the focus run (SetFocus), and the others wait for a later tick
// with their rectangles as they are. A chunk that waits comes closer each
// tick it waits, so far chunks still get their turn.
//
// The cells are kept in blocks of 8×8 chunks (Block). A big world keeps
// only some blocks in memory, and its owner loads and unloads them
// (LoadBlock, UnloadBlock). A chunk runs only when every cell its updates
// could reach is in memory; the others wait as they are.
//
// Between ticks, rigid bodies (internal/rigid) write their pixels as cells
// of solid materials (BodyCell, Put), which the cell rules never move. A
// cell in a body's way waits in a queue (Displace), and after each tick's
// cell updates the queue puts what it can back, searching up from where
// each cell was (reinsert.go).
package world

import (
	"errors"
	"fmt"

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
// A cell is 16 bytes: 15 of fields and 1 of padding, left free on purpose
// until a field needs it (TestCellSize).
//
// A cell of a solid material is a pixel of a rigid body. The cell rules
// never move it, so it has no use for Vy and Ax and keeps in them the body
// and the pixel it is instead (BodyCell, Cell.Body).
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
	// Rest counts the ticks a powder has ended at rest, neither falling nor
	// moving, up to SettleTicks; it drops to 0 when the powder moves. A
	// powder that has rested SettleTicks ticks is settled: rigid bodies
	// collide with it (Classify). Other kinds keep 0.
	Rest uint8
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
	// Streamed starts the world with no block in memory, for a world too
	// big to hold: the caller loads the blocks it needs. Otherwise every
	// block starts in memory, filled with air.
	Streamed bool
	// Tick is how many ticks the world has run, for a world loaded from a
	// save: the next Step runs tick Tick+1, with that tick's random numbers.
	Tick uint32
	// Budget is how many cells a tick may scan, 0 for no limit. Over it,
	// the chunks farthest from the focus wait (SetFocus). It changes the
	// result, so a replay records it.
	Budget int
}

// World is a grid of cells. It is not safe for concurrent use.
type World struct {
	width, height int
	tick          uint32
	seed          uint64
	// blocks holds the blocks of cells row by row, nil for those not in
	// memory; bcols and brows are how many a row and a column have.
	blocks       []*Block
	bcols, brows int
	// resident lists the indexes of the blocks in memory, in order.
	resident []int
	// mats holds every material's values. Indexing an array of
	// MaxMaterials entries with a material.ID needs no bounds check.
	mats *[material.MaxMaterials]matProps
	pool *sched.Pool
	// bounds are all the cells.
	bounds rect
	// chunks holds every chunk, row by row; cols and rows are how many a
	// row and a column have.
	chunks     []chunk
	cols, rows int
	// dirty is each chunk's change rectangle: the cells its next run
	// scans. An empty one means the chunk sleeps.
	dirty []rect
	// keep holds what was marked in a chunk earlier in the tick than it
	// runs. The run scans it, and so does the run after.
	keep []rect
	// version grows with every tick and every change between ticks.
	// versions holds, for each chunk, the version of the last change marked
	// in it.
	version  uint64
	versions []uint64
	// jobs are the runs of the phase being run; stepChunk reads them.
	jobs []job
	// awake is how many chunks the last tick ran.
	awake int
	// budget is Config.Budget; focusX and focusY are the focus (SetFocus).
	budget         int
	focusX, focusY int
	// held holds, for each chunk, the last tick it was made to wait, and
	// waited how many ticks in a row it has waited since it last ran.
	// waiting is how many chunks waited in the last tick.
	held    []uint32
	waited  []uint32
	waiting int
	// order is hold's buffer: one entry a chunk that would run.
	order []candidate
	// pending covers the cells Put and Displace changed since the last
	// Flush.
	pending rect
	// fresh holds the cells new to the queue of cells to put back, retry
	// those that failed to go back before, and spare is fresh's other
	// buffer. retryBudget is how many retries the next tick may try
	// (reinsert.go).
	fresh, retry, spare []Queued
	retryBudget         int
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
	if cfg.Budget < 0 {
		return nil, fmt.Errorf("world: budget %d is negative", cfg.Budget)
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
		tick:   cfg.Tick,
		seed:   cfg.Seed,
		mats:   mats,
		pool:   sched.New(cfg.Workers),
		bounds: rect{0, 0, cfg.Width, cfg.Height},
		bcols:  (cfg.Width + BlockSize - 1) / BlockSize,
		brows:  (cfg.Height + BlockSize - 1) / BlockSize,
		cols:   (cfg.Width + ChunkSize - 1) / ChunkSize,
		rows:   (cfg.Height + ChunkSize - 1) / ChunkSize,
		budget: cfg.Budget,
		focusX: cfg.Width / 2,
		focusY: cfg.Height / 2,

		retryBudget: retryFull,
	}
	w.blocks = make([]*Block, w.bcols*w.brows)
	if !cfg.Streamed {
		for i := range w.blocks {
			// A new block is all air, so nothing in it needs marking.
			w.blocks[i] = new(Block)
			w.resident = append(w.resident, i)
		}
	}
	w.chunks = make([]chunk, 0, w.cols*w.rows)
	for cy := range w.rows {
		for cx := range w.cols {
			x0, y0 := cx*ChunkSize, cy*ChunkSize
			w.chunks = append(w.chunks, chunk{
				cx: cx, cy: cy, i: len(w.chunks), phase: w.phase(cy, cx%2), first: cx%2 == 0,
				cells: rect{x0, y0, min(x0+ChunkSize, cfg.Width), min(y0+ChunkSize, cfg.Height)},
			})
		}
	}
	w.dirty = make([]rect, len(w.chunks))
	w.keep = make([]rect, len(w.chunks))
	w.versions = make([]uint64, len(w.chunks))
	w.held = make([]uint32, len(w.chunks))
	w.waited = make([]uint32, len(w.chunks))
	w.order = make([]candidate, 0, len(w.chunks))
	// A phase runs at most every other chunk of a row.
	w.jobs = make([]job, (w.cols+1)/2)
	w.stepChunkFn = w.stepChunk
	return w, nil
}

// phase returns the index of the phase in which the chunks of row cy and
// parity run. A tick runs the phases in order: the rows of chunks from the
// bottom, each its even columns first, then its odd ones.
func (w *World) phase(cy, parity int) int {
	return (w.rows-1-cy)*2 + parity
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

// Seed returns the seed that keys the world's random choices.
func (w *World) Seed() uint64 { return w.seed }

// Chunks returns how many chunks the grid is cut into.
func (w *World) Chunks() int { return len(w.chunks) }

// Awake returns how many chunks the last tick ran. The others slept:
// nothing in them could move.
func (w *World) Awake() int { return w.awake }

// Version returns a number that grows with every tick and every Set, Fill,
// Paint and Wear.
func (w *World) Version() uint64 { return w.version }

// ChunkVersion returns the Version of the last change in or next to chunk
// (cx, cy). A cell of the chunk that changes after Version returned v
// makes ChunkVersion return more than v, so a renderer redraws only the
// chunks whose version passed the one it last drew. It panics for a chunk
// outside the grid.
func (w *World) ChunkVersion(cx, cy int) uint64 {
	if cx < 0 || cx >= w.cols || cy < 0 || cy*ChunkSize >= w.height {
		panic(fmt.Sprintf("world: chunk (%d, %d) is outside the grid", cx, cy))
	}
	return w.versions[cy*w.cols+cx]
}

// InBounds reports whether (x, y) is inside the grid.
func (w *World) InBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < w.width && y < w.height
}

// At returns the cell at (x, y), or false if it is outside the grid or
// its block is not in memory.
func (w *World) At(x, y int) (Cell, bool) {
	if !w.has(x, y) {
		return Cell{}, false
	}
	return *w.cell(w.pos(x, y)), true
}

// Set puts a new cell of material id at (x, y). Positions outside the grid
// or in a block not in memory are ignored.
func (w *World) Set(x, y int, id material.ID) {
	if !w.has(x, y) {
		return
	}
	*w.cell(w.pos(x, y)) = Cell{Mat: id, Tone: w.tone(x, y)}
	w.touch(rect{x, y, x + 1, y + 1})
}

// Fill sets every cell in the half-open rectangle [x0, x1) × [y0, y1) to
// id, except those in blocks not in memory.
func (w *World) Fill(x0, y0, x1, y1 int, id material.ID) {
	r := rect{x0, y0, x1, y1}.clip(w.bounds)
	for y := r.y0; y < r.y1; y++ {
		for x := r.x0; x < r.x1; x++ {
			if w.has(x, y) {
				*w.cell(w.pos(x, y)) = Cell{Mat: id, Tone: w.tone(x, y)}
			}
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
			if dx*dx+dy*dy > r*r || !w.has(x, y) {
				continue
			}
			c := w.cell(w.pos(x, y))
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

// Count returns how many cells in memory hold id.
func (w *World) Count(id material.ID) int {
	n := 0
	w.eachRow(func(row []Cell) {
		for _, c := range row {
			if c.Mat == id {
				n++
			}
		}
	})
	return n
}

// Counts adds to n[id], for every material id, how many cells in memory
// hold it, in one pass. n must have an entry for every material.
func (w *World) Counts(n []int) {
	w.eachRow(func(row []Cell) {
		for _, c := range row {
			n[c.Mat]++
		}
	})
}

// eachRow calls f with each row of cells in the world of each block in
// memory, the blocks in order.
func (w *World) eachRow(f func(row []Cell)) {
	for _, i := range w.resident {
		r := w.blockCells(i%w.bcols, i/w.bcols)
		for y := r.y0; y < r.y1; y++ {
			f(w.Row(r.x0, r.x1, y))
		}
	}
}

func (w *World) tone(x, y int) uint8 {
	return uint8(rng.Hash3(w.seed, uint64(x), uint64(y), uint64(w.tick)) >> 56)
}

// Step advances the world by one tick. It runs the chunks whose change
// rectangle is not empty, phase by phase, and after each phase merges what
// the runs marked in the chunks around them, so a chunk that runs later in
// the tick already sees it. Then it tries to put back the cells waiting in
// the queue (Displace).
func (w *World) Step() {
	w.Flush()
	w.tick++
	w.version++
	w.awake = 0
	w.waiting = 0
	if w.budget > 0 {
		w.hold()
	}
	// Only the rows of blocks in memory have chunks to run, so a big world
	// costs what its loaded blocks do.
	for end := len(w.resident); end > 0; {
		by := w.resident[end-1] / w.bcols
		start := end - 1
		for start > 0 && w.resident[start-1]/w.bcols == by {
			start--
		}
		for cy := min((by+1)*blockChunks, w.rows) - 1; cy >= by*blockChunks; cy-- {
			for parity := range 2 {
				w.runPhase(w.resident[start:end], cy, parity)
			}
		}
		end = start
	}
	w.reinsertQueued()
}

// runPhase runs the chunks of row cy and the given parity in the blocks
// listed, those whose change rectangle is not empty, that do not wait this
// tick and whose reach is in memory, and then merges what they marked
// around them.
func (w *World) runPhase(blocks []int, cy, parity int) {
	n := 0
	for _, b := range blocks {
		bx := b % w.bcols
		for cx := bx*blockChunks + parity; cx < min((bx+1)*blockChunks, w.cols); cx += 2 {
			c := &w.chunks[cy*w.cols+cx]
			live := w.scanned(c)
			if live.empty() || w.held[c.i] == w.tick || !w.reachable(c) {
				continue
			}
			w.waited[c.i] = 0
			w.dirty[c.i], w.keep[c.i] = w.keep[c.i], rect{}
			w.jobs[n] = job{c: *c, live: live}
			n++
		}
	}
	if n == 0 {
		return
	}
	w.awake += n
	w.pool.Run(n, w.stepChunkFn)
	// Bounding boxes merge in any order to the same result, so the worker
	// count does not matter.
	p := w.phase(cy, parity)
	for k := range n {
		w.wake(w.jobs[k].out, p)
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
	if !j.next.empty() {
		w.dirty[c.i].add(j.next)
		w.versions[c.i] = w.version
	}
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
	w.version++
	w.wake(r.readers(), 2*w.rows)
}

// wake adds the cells of r, marked in phase p of a tick, to the change
// rectangles of the chunks they lie in, and moves those chunks to the
// current version. A chunk that runs later in the tick scans them in this
// tick and the next, as a run does with what it marks in its own chunk: a
// cell that is done for the tick when the scan reaches it gets its update
// in the next one.
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
			w.versions[c.i] = w.version
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
