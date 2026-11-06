package game

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

// Stream sets which blocks of a big world stay in memory. It follows how
// Noita streams its world: the camera requests the blocks around the view
// and ahead of where it moves, and checks every so often unload the
// blocks farthest from the camera while too many are in memory (design.md
// 7절). Every number is a 🧪 setting.
type Stream struct {
	// Grid sets the camera's area: the view, its corner moved toward zero
	// onto a multiple of Grid, widened by Grid on each side.
	Grid int
	// Lead and LeadMax set the area ahead: when the camera's area moves,
	// the area moved Lead times as far again, at most LeadMax cells on
	// each axis, is requested too.
	Lead, LeadMax int
	// Target is how many blocks may stay in memory before checks unload
	// any. It is no cap: a check unloads one block, or more when far over
	// it, and the camera's area always stays.
	Target int
	// Check is how many ticks pass between checks while fewer than twice
	// Target blocks are in memory; at twice or more, each check halves it.
	Check int
}

// valid reports whether s describes a stream.
func (s Stream) valid() bool {
	return s.Grid >= 0 && s.Lead >= 0 && s.LeadMax >= 0 && s.Target >= 1 && s.Check >= 1
}

// StreamState is where the stream's requests and checks stand. A save
// keeps it, so a resumed session goes on as the saved one would have.
type StreamState struct {
	// Area is the camera's last area, as x, y, width and height in cells,
	// if Requested.
	Area      [4]int
	Requested bool
	// LastCheck is the tick of the last check, and Interval the ticks
	// from it to the next.
	LastCheck uint32
	Interval  int
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

// stream runs the stream for a tick with the camera at (camX, camY): it
// requests the camera's blocks, then checks whether blocks should go.
func (g *Game) stream(camX, camY int) error {
	return g.streamAt(g.world.Tick(), camX, camY)
}

// streamAt is stream at tick now.
func (g *Game) streamAt(now uint32, camX, camY int) error {
	if err := g.request(camX, camY); err != nil {
		return err
	}
	return g.check(now, camX, camY)
}

// request puts in memory the blocks of the camera's area at (camX, camY).
// When that area differs from the last one, it also puts in memory the
// blocks of the area ahead: the new area moved Lead times as far again as
// it moved. Noita requests the camera's area only when it changes; here
// it is made sure of every tick, since a check never unloads it, so the
// two differ only if a block went missing some other way.
func (g *Game) request(camX, camY int) error {
	s := &g.streaming
	area := g.requestArea(camX, camY)
	if err := g.loadArea(area); err != nil {
		return err
	}
	if err := g.loadRange(g.viewReach(camX, camY)); err != nil {
		return err
	}
	if s.Requested && area == s.Area {
		return nil
	}
	// The first area has nothing to lead from.
	if s.Requested {
		if err := g.loadArea(g.aheadOf(s.Area, area)); err != nil {
			return err
		}
	}
	s.Area, s.Requested = area, true
	return nil
}

// requestArea returns the camera's area for a camera at (camX, camY), as
// x, y, width and height in cells: the view with its corner moved toward
// zero onto the grid, widened by the grid on each side.
func (g *Game) requestArea(camX, camY int) [4]int {
	n := g.limits.Grid
	if n == 0 {
		return [4]int{camX, camY, g.viewW, g.viewH}
	}
	// Go's division truncates toward zero, as Noita's does.
	return [4]int{camX/n*n - n, camY/n*n - n, g.viewW + 2*n, g.viewH + 2*n}
}

// aheadOf returns the area ahead of cur, which the camera's area moved to
// from prev: cur moved Lead times as far again, at most LeadMax cells on
// each axis.
func (g *Game) aheadOf(prev, cur [4]int) [4]int {
	lead := func(d int) int {
		return min(max(g.limits.Lead*d, -g.limits.LeadMax), g.limits.LeadMax)
	}
	return [4]int{cur[0] + lead(cur[0]-prev[0]), cur[1] + lead(cur[1]-prev[1]), cur[2], cur[3]}
}

// areaBlocks returns the blocks an area touches, both ends included, so an
// area ending exactly on a block edge takes the next block too, as Noita's
// requests do. It is clipped to the world.
func (g *Game) areaBlocks(a [4]int) blockRange {
	cols, rows := g.world.Blocks()
	x0, y0 := max(blockOf(a[0]), 0), max(blockOf(a[1]), 0)
	x1 := min(blockOf(a[0]+a[2])+1, cols)
	y1 := min(blockOf(a[1]+a[3])+1, rows)
	if x0 >= x1 || y0 >= y1 {
		return blockRange{}
	}
	return blockRange{x0, y0, x1, y1}
}

// viewReach returns the blocks that the chunks the view at (camX, camY)
// shows may reach when they run: the chunks widened by world.MaxReach
// beside and below. The world runs a chunk only when all of those are in
// memory, and the camera's area can end short of them on the view's
// bottom and right, by as much as a chunk and its reach. Noita runs such
// an edge region on its own cells alone; here it would freeze on screen,
// so these blocks stay in memory too.
func (g *Game) viewReach(camX, camY int) blockRange {
	x0, y0 := chunkStart(camX)-world.MaxReach, chunkStart(camY)
	x1 := chunkStart(camX+g.viewW+world.ChunkSize-1) + world.MaxReach
	y1 := chunkStart(camY+g.viewH+world.ChunkSize-1) + world.MaxReach
	cols, rows := g.world.Blocks()
	r := blockRange{max(blockOf(x0), 0), max(blockOf(y0), 0), min(blockOf(x1-1)+1, cols), min(blockOf(y1-1)+1, rows)}
	if r.x0 >= r.x1 || r.y0 >= r.y1 {
		return blockRange{}
	}
	return r
}

// chunkStart returns the first cell coordinate of the chunk holding cell
// coordinate c.
func chunkStart(c int) int {
	if c < 0 {
		return -((-c + world.ChunkSize - 1) / world.ChunkSize) * world.ChunkSize
	}
	return c / world.ChunkSize * world.ChunkSize
}

// blockOf returns the block coordinate of cell coordinate c, rounding
// down, so cells left of or above the world fall in negative blocks.
func blockOf(c int) int {
	if c < 0 {
		return -((-c + world.BlockSize - 1) / world.BlockSize)
	}
	return c / world.BlockSize
}

// loadArea puts in memory the blocks of area a that are not.
func (g *Game) loadArea(a [4]int) error {
	return g.loadRange(g.areaBlocks(a))
}

// loadRange puts in memory the blocks of r that are not.
func (g *Game) loadRange(r blockRange) error {
	for by := r.y0; by < r.y1; by++ {
		for bx := r.x0; bx < r.x1; bx++ {
			if err := g.loadBlock(bx, by); err != nil {
				return err
			}
		}
	}
	return nil
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

// check runs a check at tick now if Interval ticks have passed since the
// last: with more than Target blocks in memory, it saves and unloads one,
// or as many as there are over three times Target, each the farthest from
// the camera at (camX, camY) outside the camera's area and the view's
// reach (viewReach). With Target or
// fewer it sets Interval back to Check; at twice Target or more it halves
// it, down to a tick. Noita times its checks in seconds of real time;
// here they count ticks, so a session's result never depends on how fast
// it ran.
func (g *Game) check(now uint32, camX, camY int) error {
	s, lim := &g.streaming, g.limits
	if int64(now)-int64(s.LastCheck) < int64(s.Interval) {
		return nil
	}
	s.LastCheck = now
	n := len(g.resident)
	if n <= lim.Target {
		s.Interval = lim.Check
		return nil
	}
	if n >= 2*lim.Target {
		s.Interval = max(s.Interval/2, 1)
	}
	tries := max(1, n-3*lim.Target)
	keep := [2]blockRange{g.areaBlocks(s.Area), g.viewReach(camX, camY)}
	cx, cy := blockOf(camX+g.viewW/2), blockOf(camY+g.viewH/2)
	for range tries {
		i := g.farthest(keep, cx, cy)
		if i < 0 {
			return nil
		}
		b := g.resident[i]
		blk, err := g.world.UnloadBlock(b[0], b[1])
		if err != nil {
			return fmt.Errorf("game: %w", err)
		}
		g.resident = slices.Delete(g.resident, i, i+1)
		if err := g.store.Save(b[0], b[1], blk); err != nil {
			return fmt.Errorf("game: block (%d, %d): %w", b[0], b[1], err)
		}
	}
	return nil
}

// farthest returns the index in resident of the block outside both
// ranges of keep that lies farthest, in whole blocks, from block (cx, cy);
// of equally far ones, the first in row order, so the choice never
// depends on the order of resident. It returns -1 if there is none, and
// never picks (cx, cy).
func (g *Game) farthest(keep [2]blockRange, cx, cy int) int {
	best, bestD := -1, 0
	for i, b := range g.resident {
		if keep[0].has(b) || keep[1].has(b) {
			continue
		}
		dx, dy := b[0]-cx, b[1]-cy
		d := dx*dx + dy*dy
		if d > bestD || (d == bestD && best >= 0 && rowOrder(b, g.resident[best]) < 0) {
			best, bestD = i, d
		}
	}
	return best
}

// rowOrder compares blocks a and b in row order: by row, then column.
func rowOrder(a, b [2]int) int {
	if a[1] != b[1] {
		return a[1] - b[1]
	}
	return a[0] - b[0]
}

// StreamState returns where the stream's requests and checks stand, for
// a save.
func (g *Game) StreamState() StreamState {
	return g.streaming
}

// Upcoming returns the blocks that the camera's area at (camX, camY),
// widened by extra cells on each side, touches and that are not in
// memory: those the camera will likely ask for next, nearest first. The
// app prepares them in the background, so loading them later does not
// stall a tick. It changes nothing; which blocks load and when is still
// Step's alone.
//
// The list is the game's own and holds until the next call, so asking
// every few ticks allocates nothing once warm.
func (g *Game) Upcoming(camX, camY, extra int) [][2]int {
	if g.terrain == nil {
		return nil
	}
	a := g.requestArea(camX, camY)
	near := g.areaBlocks([4]int{a[0] - extra, a[1] - extra, a[2] + 2*extra, a[3] + 2*extra})
	cx, cy := 2*camX+g.viewW, 2*camY+g.viewH
	g.ahead = g.ahead[:0]
	for by := near.y0; by < near.y1; by++ {
		for bx := near.x0; bx < near.x1; bx++ {
			if !g.world.Resident(bx, by) {
				dx, dy := 2*bx*world.BlockSize+world.BlockSize-cx, 2*by*world.BlockSize+world.BlockSize-cy
				g.ahead = append(g.ahead, aheadBlock{[2]int{bx, by}, dx*dx + dy*dy})
			}
		}
	}
	slices.SortStableFunc(g.ahead, func(a, b aheadBlock) int { return a.dist - b.dist })
	g.aheadList = g.aheadList[:0]
	for _, a := range g.ahead {
		g.aheadList = append(g.aheadList, a.at)
	}
	return g.aheadList
}

// aheadBlock is a block Upcoming lists and its distance from the camera,
// in the units its sort uses.
type aheadBlock struct {
	at   [2]int
	dist int
}

// blockRange is a half-open range of blocks, [x0, x1) × [y0, y1).
type blockRange struct{ x0, y0, x1, y1 int }

func (r blockRange) has(b [2]int) bool {
	return b[0] >= r.x0 && b[0] < r.x1 && b[1] >= r.y0 && b[1] < r.y1
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
