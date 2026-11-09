package game

import (
	"fmt"
	"testing"

	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

// The two heaviest scenes of design.md 2절, which the reference CPU must
// run within the frame budget. Every number of the scenes is 🧪. The
// replay testdata/terrain_load.rpl of internal/replay plays the load
// scene in the game, and -scene collapse the collapse room.
const (
	// sceneTicks is how many ticks after its start a scene is measured.
	sceneTicks = 60
	// The load scene: in a terrain this large, the camera waits at the
	// spawn for loadWait ticks, then jumps to (loadX, loadY), in the caves.
	loadWorld    = 4096
	loadWait     = 60
	loadX, loadY = 1808, 2600
)

// sceneStream and sceneBudget are the stream and the budget of the game
// (internal/app).
var (
	sceneStream = Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 12, Check: 60}
	sceneBudget = 250_000
)

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
				if err := g.Step(&Input{}); err != nil {
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
				step(b, g, &Input{CameraX: loadX, CameraY: loadY})
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
					step(b, g, &Input{CameraX: loadX, CameraY: loadY})
					b.StartTimer()
				}
				step(b, g, &Input{CameraX: loadX, CameraY: loadY})
			}
			g.Close()
		})
	}
}

// The load scene wakes at least as many chunks as Noita does at most
// after a load, about 127 (references.md 1절), and many stay awake. Over
// the budget, some of them wait.
func TestLoadSceneWakesTheBlocks(t *testing.T) {
	g := newLoadScene(t, 2, loadSceneBlocks(t))
	defer g.Close()
	step(t, g, &Input{CameraX: loadX, CameraY: loadY})
	if n := g.World().Awake() + g.World().Waiting(); n < 128 {
		t.Fatalf("%d chunks ran or waited after the load, want 128 or more", n)
	}
	if g.World().Waiting() == 0 {
		t.Fatal("no chunk waited after the load: the scene does not reach the budget")
	}
	for range sceneTicks {
		step(t, g, &Input{CameraX: loadX, CameraY: loadY})
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
	cfg.ViewWidth, cfg.ViewHeight, cfg.Stream, cfg.Budget = 480, 270, sceneStream, sceneBudget
	g, err := New(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	return g
}

// newLoadScene starts the terrain of the load scene, with a store that
// holds copies of the ready blocks, and waits loadWait ticks at the spawn:
// the blocks there have had their first ticks before the jump, as in play.
func newLoadScene(tb testing.TB, workers int, ready map[[2]int]*world.Block) *Game {
	tb.Helper()
	store := readyStore{}
	for k, b := range ready {
		c := new(world.Block)
		*c = *b
		store[k] = c
	}
	g := newSceneGame(tb, &Config{Width: loadWorld, Height: loadWorld, Workers: workers, Scene: SceneTerrain}, store)
	x, y := g.Spawn()
	for range loadWait {
		step(tb, g, &Input{CameraX: x, CameraY: y})
	}
	return g
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
	step(tb, g, &Input{CameraX: loadX, CameraY: loadY})
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

func (s readyStore) Load(bx, by int) (*save.Block, error) {
	b := s[[2]int{bx, by}]
	delete(s, [2]int{bx, by})
	if b == nil {
		return nil, nil
	}
	return &save.Block{Cells: b}, nil
}

func (readyStore) Save(int, int, *save.Block) error { return nil }

// Saving the blocks takes each out and puts it back, which wakes it
// whole; the session must go on as if it had not been saved, also while
// chunks wait for the budget (Codex review).
func TestSavingBlocksChangesNothing(t *testing.T) {
	ready := loadSceneBlocks(t)
	saved, kept := newLoadScene(t, 2, ready), newLoadScene(t, 2, ready)
	defer saved.Close()
	defer kept.Close()
	in := Input{CameraX: loadX, CameraY: loadY}
	for range 10 {
		step(t, saved, &in)
		step(t, kept, &in)
	}
	if saved.World().Waiting() == 0 {
		t.Fatal("no chunk waits at the save: the test needs a session over its budget")
	}
	if err := saved.SaveBlocks(); err != nil {
		t.Fatal(err)
	}
	for range 60 {
		step(t, saved, &in)
		step(t, kept, &in)
		if a, b := saved.World().Checksum(), kept.World().Checksum(); a != b {
			t.Fatalf("tick %d: checksum %#x after saving the blocks, %#x without", saved.World().Tick(), a, b)
		}
	}
}
