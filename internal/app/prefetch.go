package app

import (
	"fmt"
	"sync"

	"github.com/tongmon/matter-wake/internal/scene"
	"github.com/tongmon/matter-wake/internal/world"
)

// Prefetch settings (🧪, design.md 7절).
const (
	// prefetchMargin is how much farther than the stream's margin the
	// blocks are got ready ahead of the camera: one block.
	prefetchMargin = world.BlockSize
	// prefetchWorkers read and make blocks in the background.
	prefetchWorkers = 2
)

// prefetchStore is the game.Store of a session that saves on disk. It does
// the slow work away from the tick: it reads, or makes from the terrain,
// the blocks the camera will need next (Prefetch), and it writes the
// blocks the game saves in the background. A tick then only copies blocks
// in and out of memory, where reading or making one took several
// milliseconds and a row of new blocks stalled the game for two frames.
//
// What a block holds is the same as without it, so a session's result is
// too: the latest copy saved, or what the disk or the terrain held. A
// block read ahead before the game saved it again is thrown away.
type prefetchStore struct {
	disk    *diskStore
	terrain *scene.Terrain
	mu      sync.Mutex
	// ready holds blocks got ready ahead, until Load takes them.
	ready map[[2]int]*world.Block
	// pending holds copies of saved blocks not yet written.
	pending map[[2]int]*world.Block
	// busy marks the blocks a worker is getting ready. The writer waits on
	// idle until a block it writes is not busy: on Windows a file a worker
	// holds open cannot be replaced.
	busy map[[2]int]bool
	idle *sync.Cond
	// want is Prefetch's scratch set, kept so it does not allocate.
	want map[[2]int]bool
	// saves counts each block's saves, so a read that started before one
	// is known to be stale.
	saves map[[2]int]uint64
	// err is the first error writing a block.
	err error
	// jobs feeds the workers; writes feeds the writer, one entry a save.
	jobs, writes chan [2]int
	// written counts saves not yet written; workers count the goroutines.
	written, workers sync.WaitGroup
}

func newPrefetchStore(disk *diskStore, terrain *scene.Terrain) *prefetchStore {
	s := &prefetchStore{
		disk: disk, terrain: terrain,
		ready: map[[2]int]*world.Block{}, pending: map[[2]int]*world.Block{},
		busy: map[[2]int]bool{}, saves: map[[2]int]uint64{}, want: map[[2]int]bool{},
		jobs: make(chan [2]int, 64), writes: make(chan [2]int, 64),
	}
	s.idle = sync.NewCond(&s.mu)
	s.workers.Add(prefetchWorkers + 1)
	for range prefetchWorkers {
		go s.prepare()
	}
	go s.write()
	return s
}

// Load returns block (bx, by) as last saved, or nil if it never was: from
// the copies waiting to be written, from the blocks got ready ahead, or
// read from disk now.
func (s *prefetchStore) Load(bx, by int) (*world.Block, error) {
	key := [2]int{bx, by}
	s.mu.Lock()
	if s.err != nil {
		err := s.err
		s.mu.Unlock()
		return nil, err
	}
	if c := s.pending[key]; c != nil {
		b := new(world.Block)
		*b = *c
		s.mu.Unlock()
		return b, nil
	}
	if b := s.ready[key]; b != nil {
		delete(s.ready, key)
		s.mu.Unlock()
		return b, nil
	}
	s.mu.Unlock()
	return s.disk.Load(bx, by)
}

// Save keeps a copy of block (bx, by) and writes it in the background.
func (s *prefetchStore) Save(bx, by int, b *world.Block) error {
	key := [2]int{bx, by}
	c := new(world.Block)
	*c = *b
	s.mu.Lock()
	if s.err != nil {
		err := s.err
		s.mu.Unlock()
		return err
	}
	s.pending[key] = c
	s.saves[key]++
	delete(s.ready, key)
	s.mu.Unlock()
	s.written.Add(1)
	s.writes <- key
	return nil
}

// Prefetch gets the blocks listed ready in the background, nearest first
// as listed, and forgets blocks got ready earlier that are no longer
// listed, so the blocks waiting stay few. It never waits, and once warm it
// does not allocate.
func (s *prefetchStore) Prefetch(blocks [][2]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.want)
	for _, b := range blocks {
		s.want[b] = true
	}
	for key := range s.ready {
		if !s.want[key] {
			delete(s.ready, key)
		}
	}
	for _, key := range blocks {
		if s.ready[key] != nil || s.pending[key] != nil || s.busy[key] {
			continue
		}
		select {
		case s.jobs <- key:
			s.busy[key] = true
		default:
			return // the workers are behind; the rest waits for the next call
		}
	}
}

// prepare is a worker: it reads or makes the blocks Prefetch asks for. A
// block whose read fails is left to Load, which reports the error if the
// game comes to need it.
func (s *prefetchStore) prepare() {
	defer s.workers.Done()
	for key := range s.jobs {
		s.mu.Lock()
		saves, pending := s.saves[key], s.pending[key] != nil
		s.mu.Unlock()
		var b *world.Block
		if !pending {
			var err error
			if b, err = s.disk.Load(key[0], key[1]); err == nil && b == nil {
				b = s.terrain.Block(key[0], key[1])
			} else if err != nil {
				b = nil
			}
		}
		s.done(key, b, saves)
	}
}

// done records that a worker has finished with the block at key: b is
// what it read or made, or nil, from a read begun when the block had been
// saved saves times. A block saved again since is not kept.
func (s *prefetchStore) done(key [2]int, b *world.Block, saves uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.busy, key)
	if b != nil && s.saves[key] == saves && s.pending[key] == nil {
		s.ready[key] = b
	}
	s.idle.Broadcast()
}

// write is the writer: it writes the latest copy of each block saved.
func (s *prefetchStore) write() {
	defer s.workers.Done()
	for key := range s.writes {
		s.mu.Lock()
		// No worker starts on a block waiting to be written, so once the
		// ones at work on it are done, the file is the writer's alone.
		for s.busy[key] {
			s.idle.Wait()
		}
		c := s.pending[key]
		s.mu.Unlock()
		if c != nil {
			err := s.disk.Save(key[0], key[1], c)
			s.mu.Lock()
			if err != nil && s.err == nil {
				s.err = err
			}
			if s.pending[key] == c {
				delete(s.pending, key)
			}
			s.mu.Unlock()
		}
		s.written.Done()
	}
}

// Flush waits until every block saved is written and returns the first
// error writing one.
func (s *prefetchStore) Flush() error {
	s.written.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close writes what is left and stops the background work.
func (s *prefetchStore) Close() error {
	err := s.Flush()
	close(s.jobs)
	close(s.writes)
	s.workers.Wait()
	if err != nil {
		return fmt.Errorf("app: save: %w", err)
	}
	return nil
}
