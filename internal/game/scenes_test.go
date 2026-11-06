package game

import (
	"fmt"
	"testing"

	"github.com/tongmon/matter-wake/internal/world"
)

// The two heaviest scenes of design.md 2절, which the reference CPU must
// run within the frame budget. Every number of the scenes is 🧪. The
// replay testdata/terrain_load.rpl of internal/replay plays the load
// scene in the game, and -scene collapse the collapse room.
const (
	// sceneTicks is how many ticks after its start a scene is measured.
	sceneTicks = 60
	// The load scene: in a terrain this large, the camera jumps from the
	// spawn to (loadX, loadY), in the caves.
	loadWorld    = 4096
	loadX, loadY = 1808, 2600
)

// sceneStream is the stream of the game (internal/app).
var sceneStream = Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 12, Check: 60}

// BenchmarkCollapseScene measures the first sceneTicks ticks of the
// collapse room, the size of the screen, while everything in it falls.
func BenchmarkCollapseScene(b *testing.B) {
	for _, workers := range []int{1, 2, 4} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			var g *Game
			for i := range b.N {
				if i%sceneTicks == 0 {
					b.StopTimer()
					if g != nil {
						g.Close()
					}
					g = newSceneGame(b, &Config{Width: 480, Height: 270, Workers: workers, Scene: SceneCollapse}, nil)
					b.StartTimer()
				}
				if err := g.Step(Input{}); err != nil {
					b.Fatal(err)
				}
			}
			g.Close()
		})
	}
}

// BenchmarkLoadScene measures the load scene: the camera jumps into the
// caves and every block it needs there loads at once and wakes. "load" is
// the tick that loads them, with their cells ready in the store as the
// app's prefetch has them, so making the terrain is not in it; "after" is
// the sceneTicks ticks after it.
func BenchmarkLoadScene(b *testing.B) {
	ready := loadSceneBlocks(b)
	for _, workers := range []int{1, 2, 4} {
		b.Run(fmt.Sprintf("workers=%d/load", workers), func(b *testing.B) {
			for range b.N {
				b.StopTimer()
				g := newLoadScene(b, workers, ready)
				b.StartTimer()
				step(b, g, Input{CameraX: loadX, CameraY: loadY})
				b.StopTimer()
				g.Close()
				b.StartTimer()
			}
		})
		b.Run(fmt.Sprintf("workers=%d/after", workers), func(b *testing.B) {
			var g *Game
			for i := range b.N {
				if i%sceneTicks == 0 {
					b.StopTimer()
					if g != nil {
						g.Close()
					}
					g = newLoadScene(b, workers, ready)
					step(b, g, Input{CameraX: loadX, CameraY: loadY})
					b.StartTimer()
				}
				step(b, g, Input{CameraX: loadX, CameraY: loadY})
			}
			g.Close()
		})
	}
}

// The load scene wakes at least as many chunks as Noita does at most
// after a load, about 127 (references.md 1절), and many stay awake.
func TestLoadSceneWakesTheBlocks(t *testing.T) {
	g := newLoadScene(t, 2, loadSceneBlocks(t))
	defer g.Close()
	step(t, g, Input{CameraX: loadX, CameraY: loadY})
	if n := g.World().Awake(); n < 128 {
		t.Fatalf("%d chunks awake after the load, want 128 or more", n)
	}
	for range sceneTicks {
		step(t, g, Input{CameraX: loadX, CameraY: loadY})
	}
	if n := g.World().Awake(); n < 64 {
		t.Fatalf("%d chunks awake %d ticks after the load, want 64 or more: the scene should stay busy", n, sceneTicks)
	}
}

// newSceneGame starts the session cfg describes with the test materials,
// tool and stream.
func newSceneGame(tb testing.TB, cfg *Config, store Store) *Game {
	tb.Helper()
	cfg.Seed, cfg.Materials, cfg.Slots, cfg.Tool, cfg.Store = 1, testTable(tb), []string{"sand", "water", "stone"}, testTool, store
	cfg.ViewWidth, cfg.ViewHeight, cfg.Stream = 480, 270, sceneStream
	g, err := New(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	return g
}

// newLoadScene starts the terrain of the load scene at the spawn, with a
// store that holds copies of the ready blocks.
func newLoadScene(tb testing.TB, workers int, ready map[[2]int]*world.Block) *Game {
	tb.Helper()
	store := readyStore{}
	for k, b := range ready {
		c := new(world.Block)
		*c = *b
		store[k] = c
	}
	return newSceneGame(tb, &Config{Width: loadWorld, Height: loadWorld, Workers: workers, Scene: SceneTerrain}, store)
}

// loadSceneBlocks makes the blocks the load scene's jump loads.
func loadSceneBlocks(tb testing.TB) map[[2]int]*world.Block {
	tb.Helper()
	g := newSceneGame(tb, &Config{Width: loadWorld, Height: loadWorld, Workers: 1, Scene: SceneTerrain}, nil)
	defer g.Close()
	before := map[[2]int]bool{}
	for _, b := range g.ResidentBlocks() {
		before[b] = true
	}
	step(tb, g, Input{CameraX: loadX, CameraY: loadY})
	ready := map[[2]int]*world.Block{}
	for _, b := range g.ResidentBlocks() {
		if !before[b] {
			ready[b] = g.terrain.Block(b[0], b[1])
		}
	}
	return ready
}

// readyStore hands out blocks made beforehand, once each, and keeps none.
type readyStore map[[2]int]*world.Block

func (s readyStore) Load(bx, by int) (*world.Block, error) {
	b := s[[2]int{bx, by}]
	delete(s, [2]int{bx, by})
	return b, nil
}

func (readyStore) Save(int, int, *world.Block) error { return nil }
