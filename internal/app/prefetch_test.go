package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/scene"
	"github.com/tongmon/matter-wake/internal/world"
)

func newTestPrefetch(t *testing.T, dir string, mats *material.Table) (*prefetchStore, *scene.Terrain) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, blocksDir), 0o755); err != nil {
		t.Fatal(err)
	}
	tr, err := scene.NewTerrain(4, 4096, 4096, mats)
	if err != nil {
		t.Fatal(err)
	}
	s := newPrefetchStore(&diskStore{dir: dir, mats: mats}, tr)
	t.Cleanup(func() { _ = s.Close() })
	return s, tr
}

// waitReady waits until the store has got block b ready.
func waitReady(t *testing.T, s *prefetchStore, b [2]int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ready := s.ready[b] != nil
		s.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("block %v was not ready in time", b)
}

// A block saved comes back as saved, before it is written and after, and
// the written file holds it.
func TestPrefetchStoreKeepsWhatWasSaved(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	s, _ := newTestPrefetch(t, dir, mats)
	b := new(world.Block)
	b[7] = world.Cell{Mat: mats.MustLookup("sand"), Tone: 3, Last: 9}
	if err := s.Save(2, 3, b); err != nil {
		t.Fatal(err)
	}
	b[7].Tone = 100 // the store keeps its own copy
	got, err := s.Load(2, 3)
	if err != nil || got == nil || got[7].Tone != 3 {
		t.Fatalf("Load right after Save = %v, %v; want the saved copy", got, err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	onDisk, err := (&diskStore{dir: dir, mats: mats}).Load(2, 3)
	if err != nil || onDisk == nil || onDisk[7].Tone != 3 {
		t.Fatalf("the file holds %v, %v; want the saved copy", onDisk, err)
	}
}

// Blocks got ready ahead are what the terrain or the disk would give, and
// a save made after one was got ready wins.
func TestPrefetchStoreGetsBlocksReady(t *testing.T) {
	mats := testMaterials(t)
	s, tr := newTestPrefetch(t, t.TempDir(), mats)
	made := [2]int{4, 2}
	s.Prefetch([][2]int{made})
	waitReady(t, s, made)
	got, err := s.Load(made[0], made[1])
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != *tr.Block(made[0], made[1]) {
		t.Fatal("the block got ready differs from the terrain's")
	}

	saved := [2]int{5, 2}
	old := new(world.Block)
	old[0].Tone = 1
	if err := s.Save(saved[0], saved[1], old); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	s.Prefetch([][2]int{saved})
	waitReady(t, s, saved)
	newer := new(world.Block)
	newer[0].Tone = 2
	if err := s.Save(saved[0], saved[1], newer); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(saved[0], saved[1]); err != nil || got == nil || got[0].Tone != 2 {
		t.Fatalf("Load after a newer save = %v, %v; want the newer save", got, err)
	}
}

// A block that cannot be written fails the next Load, Save and Flush, so
// the session ends instead of losing it.
func TestPrefetchStoreReportsWriteErrors(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	s, _ := newTestPrefetch(t, dir, mats)
	if err := os.RemoveAll(filepath.Join(dir, blocksDir)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(0, 0, new(world.Block)); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err == nil {
		t.Fatal("Flush after a failed write gave no error")
	}
	if _, err := s.Load(1, 1); err == nil {
		t.Fatal("Load after a failed write gave no error")
	}
	if err := s.Save(1, 1, new(world.Block)); err == nil {
		t.Fatal("Save after a failed write gave no error")
	}
}

// Reading and writing in the background changes nothing in the game: a
// session on the store that gets blocks ready plays tick for tick as one
// on the plain disk store, while its camera crosses many blocks.
func TestPrefetchingChangesNothing(t *testing.T) {
	mats := testMaterials(t)
	plainDir, aheadDir := t.TempDir(), t.TempDir()
	plainCfg, err := openSave(plainDir, terrainConfig(mats), false)
	if err != nil {
		t.Fatal(err)
	}
	aheadCfg, err := openSave(aheadDir, terrainConfig(mats), false)
	if err != nil {
		t.Fatal(err)
	}
	ahead, _ := newTestPrefetch(t, aheadDir, mats)
	aheadCfg.Store = ahead
	plain, err := game.New(plainCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	fast, err := game.New(aheadCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	camX, camY := plain.Spawn()
	for i := range 300 {
		// Out to the right and back, 12 cells a tick.
		x := camX + 12*min(i, 300-i)
		in := game.Input{CameraX: x, CameraY: camY, CursorX: x + 240, CursorY: camY + 160, Dig: i%5 == 0}
		for _, g := range []*game.Game{plain, fast} {
			if err := g.Step(in); err != nil {
				t.Fatal(err)
			}
		}
		if i%4 == 0 {
			ahead.Prefetch(fast.Upcoming(in.CameraX, in.CameraY, prefetchMargin))
		}
		// The checksum hashes every block in memory, which is slow.
		if i%20 != 19 {
			continue
		}
		if a, b := plain.World().Checksum(), fast.World().Checksum(); a != b {
			t.Fatalf("tick %d: with blocks got ready ahead the checksum is %#x, without %#x", plain.World().Tick(), b, a)
		}
	}
}

// The writer waits while a worker still reads the block it writes: on
// Windows an open file cannot be replaced, and the failed save would end
// the session. Found by the Codex review of prefetching (2026-11-06).
func TestPrefetchStoreWritesAfterReads(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	s, _ := newTestPrefetch(t, dir, mats)
	key := [2]int{3, 1}
	v1 := new(world.Block)
	v1[0].Tone = 1
	if err := s.Save(key[0], key[1], v1); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	// A worker reads the file: it holds it open and the block is busy.
	f, err := os.Open(s.disk.path(key[0], key[1]))
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.busy[key] = true
	s.mu.Unlock()
	v2 := new(world.Block)
	v2[0].Tone = 2
	if err := s.Save(key[0], key[1], v2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	s.done(key, nil, 0)
	if err := s.Flush(); err != nil {
		t.Fatalf("saving a block a worker was reading: %v", err)
	}
	got, err := s.disk.Load(key[0], key[1])
	if err != nil || got == nil || got[0].Tone != 2 {
		t.Fatalf("the file holds %v, %v; want the second save", got, err)
	}
}

// Asking for blocks to be got ready allocates nothing once warm, since the
// app asks every few ticks.
func TestPrefetchDoesNotAllocate(t *testing.T) {
	mats := testMaterials(t)
	s, _ := newTestPrefetch(t, t.TempDir(), mats)
	blocks := [][2]int{{1, 1}, {2, 1}, {3, 1}}
	s.Prefetch(blocks)
	for _, b := range blocks {
		waitReady(t, s, b)
	}
	if n := testing.AllocsPerRun(20, func() { s.Prefetch(blocks) }); n != 0 {
		t.Fatalf("Prefetch allocates %v times a call", n)
	}
}
