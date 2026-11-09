package world

import (
	"errors"
	"fmt"
	"slices"
)

// BlockSize is the side of the square blocks the world keeps in memory and
// on disk: 8×8 chunks. Only the blocks in memory (resident) have cells.
const BlockSize = 512

const (
	blockShift  = 9 // log2(BlockSize)
	blockMask   = BlockSize - 1
	blockCells  = BlockSize * BlockSize
	blockChunks = BlockSize / ChunkSize
)

// Block holds the cells of one block, row by row: cell (x, y) of the block
// is at y*BlockSize + x. In a block at the world's right or bottom edge the
// cells past the edge are not part of the world and stay air.
type Block [blockCells]Cell

// Blocks returns how many blocks a row of the world has and how many rows
// of blocks it has.
func (w *World) Blocks() (cols, rows int) { return w.bcols, w.brows }

// AllResident reports whether every block of the world is in memory.
func (w *World) AllResident() bool { return len(w.resident) == len(w.blocks) }

// Resident reports whether block (bx, by) is in memory.
func (w *World) Resident(bx, by int) bool {
	return w.blockInWorld(bx, by) && w.blocks[by*w.bcols+bx] != nil
}

// LoadBlock puts block (bx, by) in memory with the cells of b. The world
// takes b over; the caller must not use it afterwards. The block's cells
// and those around it are updated in the next tick, as after a Fill.
func (w *World) LoadBlock(bx, by int, b *Block) error {
	switch {
	case !w.blockInWorld(bx, by):
		return fmt.Errorf("world: block (%d, %d) is outside the world", bx, by)
	case w.blocks[by*w.bcols+bx] != nil:
		return fmt.Errorf("world: block (%d, %d) is already in memory", bx, by)
	case b == nil:
		return errors.New("world: no block to load")
	}
	i := by*w.bcols + bx
	w.blocks[i] = b
	k, _ := slices.BinarySearch(w.resident, i)
	w.resident = slices.Insert(w.resident, k, i)
	w.forgetChunks(bx, by)
	w.touch(w.blockCells(bx, by))
	return nil
}

// UnloadBlock takes block (bx, by) out of memory and returns its cells.
// Until it is loaded again, the chunks whose updates could reach it do not
// run, so nothing next to it changes.
func (w *World) UnloadBlock(bx, by int) (*Block, error) {
	if !w.blockInWorld(bx, by) {
		return nil, fmt.Errorf("world: block (%d, %d) is outside the world", bx, by)
	}
	i := by*w.bcols + bx
	b := w.blocks[i]
	if b == nil {
		return nil, fmt.Errorf("world: block (%d, %d) is not in memory", bx, by)
	}
	w.blocks[i] = nil
	k, _ := slices.BinarySearch(w.resident, i)
	w.resident = slices.Delete(w.resident, k, k+1)
	w.forgetChunks(bx, by)
	return b, nil
}

func (w *World) blockInWorld(bx, by int) bool {
	return bx >= 0 && by >= 0 && bx < w.bcols && by < w.brows
}

// blockCells returns the cells of block (bx, by) that are in the world.
func (w *World) blockCells(bx, by int) rect {
	return rect{bx * BlockSize, by * BlockSize, (bx + 1) * BlockSize, (by + 1) * BlockSize}.clip(w.bounds)
}

// forgetChunks empties the change rectangles of the chunks of block
// (bx, by) and forgets how long they waited for the budget. Loading the
// block marks all of it anyway, and a save records the chunks of the
// blocks in memory only, so a block that comes back starts the same in a
// session opened again (TestUnloadingForgetsTheWaits).
func (w *World) forgetChunks(bx, by int) {
	r := w.blockCells(bx, by)
	for cy := r.y0 / ChunkSize; cy <= (r.y1-1)/ChunkSize; cy++ {
		for cx := r.x0 / ChunkSize; cx <= (r.x1-1)/ChunkSize; cx++ {
			i := cy*w.cols + cx
			w.dirty[i], w.keep[i], w.waited[i] = rect{}, rect{}, 0
		}
	}
}

// has reports whether (x, y) is a cell in memory.
func (w *World) has(x, y int) bool {
	return w.InBounds(x, y) && w.blocks[(y>>blockShift)*w.bcols+x>>blockShift] != nil
}

// pos returns where the cell at (x, y) is kept: its block's index and its
// place in the block, in one number. (x, y) must be a cell in memory.
func (w *World) pos(x, y int) int {
	return ((y>>blockShift)*w.bcols+x>>blockShift)<<(2*blockShift) | (y&blockMask)<<blockShift | x&blockMask
}

// cell returns the cell kept at p, which pos returned.
func (w *World) cell(p int) *Cell {
	return &w.blocks[p>>(2*blockShift)][p&(blockCells-1)]
}

// reachable reports whether every cell a run of chunk c may read or write
// is in memory or outside the world: the cells of the chunk and those
// within MaxReach of it, beside and below.
func (w *World) reachable(c *chunk) bool {
	r := rect{c.cells.x0 - MaxReach, c.cells.y0, c.cells.x1 + MaxReach, c.cells.y1 + MaxReach}.clip(w.bounds)
	for by := r.y0 >> blockShift; by <= (r.y1-1)>>blockShift; by++ {
		for bx := r.x0 >> blockShift; bx <= (r.x1-1)>>blockShift; bx++ {
			if w.blocks[by*w.bcols+bx] == nil {
				return false
			}
		}
	}
	return true
}

// Row returns the cells [x0, x1) of row y, which must lie in one block in
// memory. Callers must not modify them.
func (w *World) Row(x0, x1, y int) []Cell {
	if !w.has(x0, y) || !w.has(x1-1, y) || x0>>blockShift != (x1-1)>>blockShift {
		panic(fmt.Sprintf("world: row %d from %d to %d is not in one block in memory", y, x0, x1))
	}
	b := w.blocks[(y>>blockShift)*w.bcols+x0>>blockShift]
	start := (y&blockMask)<<blockShift | x0&blockMask
	return b[start : start+x1-x0]
}
