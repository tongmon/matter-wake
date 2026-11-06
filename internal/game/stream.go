package game

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

// Stream sets which blocks of a big world stay in memory (🧪, design.md
// 7절).
type Stream struct {
	// Margin is how far past each side of the view, in cells, the blocks
	// the view needs reach.
	Margin int
	// MaxBlocks is how many blocks may stay in memory. Past it, the blocks
	// the view does not need go, those farthest from the camera first. The
	// blocks the view needs always stay, even more than MaxBlocks of them.
	MaxBlocks int
}

// Store keeps the blocks a big world unloads, so they come back as they
// left.
type Store interface {
	// Load returns block (bx, by) as it was last saved, or nil if it never
	// was.
	Load(bx, by int) (*world.Block, error)
	// Save keeps a copy of block (bx, by): it must not hold on to b,
	// which the session may go on using.
	Save(bx, by int, b *world.Block) error
}

// stream keeps in memory the blocks the view at (camX, camY) needs and
// unloads, while more than MaxBlocks are in memory, those it does not need,
// farthest from the camera first. A block comes from the store if it was
// saved, else from the terrain.
func (g *Game) stream(camX, camY int) error {
	need := g.needed(camX, camY)
	for by := need.y0; by < need.y1; by++ {
		for bx := need.x0; bx < need.x1; bx++ {
			if err := g.loadBlock(bx, by); err != nil {
				return err
			}
		}
	}
	return g.evict(need, camX, camY)
}

// load puts in memory the blocks listed that are not.
func (g *Game) load(blocks [][2]int) error {
	for _, b := range blocks {
		if err := g.loadBlock(b[0], b[1]); err != nil {
			return err
		}
	}
	return nil
}

// loadBlock puts block (bx, by) in memory, unless it is already: from the
// store if it was saved, else from the terrain.
func (g *Game) loadBlock(bx, by int) error {
	if g.world.Resident(bx, by) {
		return nil
	}
	b, err := g.store.Load(bx, by)
	if err != nil {
		return fmt.Errorf("game: block (%d, %d): %w", bx, by, err)
	}
	if b == nil {
		b = g.terrain.Block(bx, by)
	}
	if err := g.world.LoadBlock(bx, by, b); err != nil {
		return fmt.Errorf("game: %w", err)
	}
	g.resident = append(g.resident, [2]int{bx, by})
	return nil
}

// evict saves and unloads, while more than MaxBlocks are in memory, the
// blocks outside need, farthest from the camera at (camX, camY) first.
func (g *Game) evict(need blockRange, camX, camY int) error {
	if len(g.resident) <= g.limits.MaxBlocks {
		return nil
	}
	// Farthest first; equal distances in block order, so the choice never
	// depends on how resident happens to be ordered.
	cx, cy := 2*camX+g.viewW, 2*camY+g.viewH
	dist := func(b [2]int) int {
		dx, dy := 2*b[0]*world.BlockSize+world.BlockSize-cx, 2*b[1]*world.BlockSize+world.BlockSize-cy
		return dx*dx + dy*dy
	}
	slices.SortFunc(g.resident, func(a, b [2]int) int {
		if da, db := dist(a), dist(b); da != db {
			return db - da
		}
		if a[1] != b[1] {
			return a[1] - b[1]
		}
		return a[0] - b[0]
	})
	kept := g.resident[:0]
	excess := len(g.resident) - g.limits.MaxBlocks
	for _, b := range g.resident {
		if excess == 0 || need.has(b) {
			kept = append(kept, b)
			continue
		}
		blk, err := g.world.UnloadBlock(b[0], b[1])
		if err != nil {
			return fmt.Errorf("game: %w", err)
		}
		if err := g.store.Save(b[0], b[1], blk); err != nil {
			return fmt.Errorf("game: block (%d, %d): %w", b[0], b[1], err)
		}
		excess--
	}
	g.resident = kept
	return nil
}

// Upcoming returns the blocks the view at (camX, camY) would need with
// extra more cells of margin on each side that are not in memory: those
// the camera will likely ask for next, nearest first. The app prepares
// them in the background, so loading them later does not stall a tick. It
// changes nothing; which blocks load and when is still Step's alone.
func (g *Game) Upcoming(camX, camY, extra int) [][2]int {
	if g.terrain == nil {
		return nil
	}
	near := g.around(camX, camY, g.limits.Margin+extra)
	var out [][2]int
	for by := near.y0; by < near.y1; by++ {
		for bx := near.x0; bx < near.x1; bx++ {
			if !g.world.Resident(bx, by) {
				out = append(out, [2]int{bx, by})
			}
		}
	}
	cx, cy := 2*camX+g.viewW, 2*camY+g.viewH
	dist := func(b [2]int) int {
		dx, dy := 2*b[0]*world.BlockSize+world.BlockSize-cx, 2*b[1]*world.BlockSize+world.BlockSize-cy
		return dx*dx + dy*dy
	}
	slices.SortStableFunc(out, func(a, b [2]int) int { return dist(a) - dist(b) })
	return out
}

// blockRange is a half-open range of blocks, [x0, x1) × [y0, y1).
type blockRange struct{ x0, y0, x1, y1 int }

func (r blockRange) has(b [2]int) bool {
	return b[0] >= r.x0 && b[0] < r.x1 && b[1] >= r.y0 && b[1] < r.y1
}

// needed returns the blocks that the view at (camX, camY), widened by the
// margin on each side, overlaps. Noita keeps the same: its keep-alive
// widens the area by 256 on each side and takes the blocks it overlaps
// (WORLD_STREAMING:26-32).
func (g *Game) needed(camX, camY int) blockRange {
	return g.around(camX, camY, g.limits.Margin)
}

// around returns the blocks that the view at (camX, camY), widened by m
// cells on each side, overlaps.
func (g *Game) around(camX, camY, m int) blockRange {
	cols, rows := g.world.Blocks()
	x0, y0 := max(camX-m, 0), max(camY-m, 0)
	x1, y1 := min(camX+g.viewW+m, g.world.Width()), min(camY+g.viewH+m, g.world.Height())
	if x0 >= x1 || y0 >= y1 {
		return blockRange{}
	}
	return blockRange{
		x0 / world.BlockSize, y0 / world.BlockSize,
		min((x1-1)/world.BlockSize+1, cols), min((y1-1)/world.BlockSize+1, rows),
	}
}

// memoryStore keeps unloaded blocks in memory, encoded as a save would be:
// about 200KB a block instead of 4MB. Replays and shots use it, so they
// never touch the disk.
type memoryStore struct {
	mats   *material.Table
	blocks map[[2]int][]byte
}

func newMemoryStore(mats *material.Table) *memoryStore {
	return &memoryStore{mats: mats, blocks: map[[2]int][]byte{}}
}

func (s *memoryStore) Load(bx, by int) (*world.Block, error) {
	data, ok := s.blocks[[2]int{bx, by}]
	if !ok {
		return nil, nil
	}
	// The world holds the block now; the copy here would go stale.
	delete(s.blocks, [2]int{bx, by})
	b, err := save.DecodeBlock(bytes.NewReader(data), s.mats)
	if err != nil {
		return nil, fmt.Errorf("memory store: %w", err)
	}
	return b, nil
}

func (s *memoryStore) Save(bx, by int, b *world.Block) error {
	var buf bytes.Buffer
	if err := save.EncodeBlock(&buf, b, s.mats); err != nil {
		return fmt.Errorf("memory store: %w", err)
	}
	s.blocks[[2]int{bx, by}] = buf.Bytes()
	return nil
}
