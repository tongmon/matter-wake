package game

import (
	"hash/fnv"
	"reflect"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/world"
)

// testStream is the stream design.md 7절 settles on, Noita's numbers.
var testStream = Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 12, Check: 60}

// newTerrainGame starts a terrain session in a world of 8×8 blocks.
func newTerrainGame(t *testing.T, workers int) *Game {
	t.Helper()
	g, err := New(&Config{
		Width: 4096, Height: 4096, Seed: 3, Workers: workers, Materials: testTable(t),
		Slots: []string{"sand", "water", "stone"}, Tool: testTool,
		Scene: SceneTerrain, ViewWidth: 480, ViewHeight: 270, Stream: testStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

// checkResidency fails unless every block of the area the camera at
// (camX, camY) requests is in memory and the game lists exactly the blocks
// in memory.
func checkResidency(t *testing.T, g *Game, camX, camY int) {
	t.Helper()
	need := g.areaBlocks(g.requestArea(camX, camY))
	for by := need.y0; by < need.y1; by++ {
		for bx := need.x0; bx < need.x1; bx++ {
			if !g.world.Resident(bx, by) {
				t.Fatalf("camera (%d, %d): block (%d, %d) of the camera's area is not in memory", camX, camY, bx, by)
			}
		}
	}
	cols, rows := g.world.Blocks()
	n := 0
	for by := range rows {
		for bx := range cols {
			if g.world.Resident(bx, by) {
				n++
			}
		}
	}
	if n != len(g.resident) {
		t.Fatalf("camera (%d, %d): %d blocks in memory, %d listed", camX, camY, n, len(g.resident))
	}
}

func TestTerrainStartsWithTheViewAroundSpawn(t *testing.T) {
	g := newTerrainGame(t, 1)
	x, y := g.Spawn()
	if x < 0 || y < 0 || x+480 > 4096 || y+270 > 4096 {
		t.Fatalf("spawn camera (%d, %d) is not inside the world", x, y)
	}
	checkResidency(t, g, x, y)
	// Ground fills the bottom of the view and sky its top.
	if c, _ := g.world.At(x+240, y); c.Mat != 0 {
		t.Errorf("the top middle of the spawn view holds material %d, not sky", c.Mat)
	}
}

// Walking the camera across the world keeps the camera's area in memory.
func TestStreamFollowsTheCamera(t *testing.T) {
	g := newTerrainGame(t, 2)
	x, y := g.Spawn()
	for i := range 120 {
		// Right, then down, then back left: 32 cells a tick.
		switch {
		case i < 50:
			x += 32
		case i < 80:
			y += 32
		default:
			x -= 32
		}
		x, y = min(max(x, 0), 4096-480), min(max(y, 0), 4096-270)
		step(t, g, Input{CameraX: x, CameraY: y})
		checkResidency(t, g, x, y)
	}
}

// A block that leaves memory comes back exactly as it left, with its
// bodies: the camera goes far away and returns. The bodies leave with the
// blocks, and come back with the same keys, images, transforms, velocities
// and sleep, to cover the same cells.
func TestLeavingAndComingBackKeepsTheBlocks(t *testing.T) {
	g := newTerrainBodyGame(t, 2)
	x, y := g.Spawn()
	for i := range 240 {
		in := Input{CameraX: x, CameraY: y}
		if i < 40 && i%8 == 0 {
			in.CursorX, in.CursorY, in.Drop = x+120+10*i, y+60, true
		}
		step(t, g, in)
	}
	if g.Bodies() != 5 {
		t.Fatalf("%d bodies; the test needs the 5 dropped", g.Bodies())
	}
	// Written where Box2D's last Step left them, as the next tick would.
	g.bodies.Write()
	bodies := g.BodyRecords(nil)
	before := map[[2]int]uint64{}
	for _, b := range g.resident {
		before[b] = blockSum(g, b)
	}
	// Room for no more than the camera's area, and a check every tick, so
	// the blocks left behind go.
	g.limits.Target = 1
	base := g.world.Tick()
	for now := range uint32(30) {
		g.streaming.Interval = 1
		if err := g.streamAt(base+now+1, 0, 4096-270); err != nil {
			t.Fatal(err)
		}
	}
	gone := 0
	for b := range before {
		if !g.world.Resident(b[0], b[1]) {
			gone++
		}
	}
	if gone != len(before) {
		t.Fatalf("%d of %d blocks left memory when the camera went to the far corner", gone, len(before))
	}
	if g.Bodies() != 0 {
		t.Fatalf("%d bodies stayed in memory without their blocks", g.Bodies())
	}
	if err := g.streamAt(base+31, x, y); err != nil {
		t.Fatal(err)
	}
	if got := g.BodyRecords(nil); !reflect.DeepEqual(got, bodies) {
		t.Fatalf("the bodies came back as\n%+v\nwant\n%+v", got, bodies)
	}
	g.bodies.Write()
	for b, sum := range before {
		if got := blockSum(g, b); got != sum {
			t.Fatalf("block %v came back different", b)
		}
	}
}

// newTerrainBodyGame is newTerrainGame with the body tool.
func newTerrainBodyGame(t *testing.T, workers int) *Game {
	t.Helper()
	g, err := New(&Config{
		Width: 4096, Height: 4096, Seed: 3, Workers: workers, Materials: testTable(t),
		Slots: []string{"sand", "water", "stone"}, Tool: testTool, Body: BodyTool{Material: "wood", Width: 16, Height: 12},
		Scene: SceneTerrain, ViewWidth: 480, ViewHeight: 270, Stream: testStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

// blockSum hashes the cells of block b, which must be in memory.
func blockSum(g *Game, b [2]int) uint64 {
	h := fnv.New64a()
	for y := b[1] * world.BlockSize; y < (b[1]+1)*world.BlockSize; y++ {
		for _, c := range g.world.Row(b[0]*world.BlockSize, (b[0]+1)*world.BlockSize, y) {
			_, _ = h.Write([]byte{byte(c.Mat), c.Tone, byte(c.Last), byte(c.Vy), byte(c.Ax), byte(c.Damage)})
		}
	}
	return h.Sum64()
}

// The terrain's result does not depend on the worker count, streaming
// included.
func TestTerrainIsIndependentOfWorkers(t *testing.T) {
	var sums []uint64
	for _, workers := range []int{1, 4} {
		g := newTerrainGame(t, workers)
		x, y := g.Spawn()
		for i := range 90 {
			in := Input{CameraX: x + 16*i, CameraY: y, CursorX: x + 16*i + 240, CursorY: y + 200, Dig: i%3 == 0}
			step(t, g, in)
		}
		sums = append(sums, g.world.Checksum())
	}
	if sums[0] != sums[1] {
		t.Fatalf("1 worker checksum %#x, 4 workers %#x", sums[0], sums[1])
	}
}

func TestNewRejectsBadTerrain(t *testing.T) {
	base := Config{
		Width: 1024, Height: 1024, Seed: 1, Materials: testTable(t), Slots: []string{"sand"}, Tool: testTool,
		Scene: SceneTerrain, ViewWidth: 480, ViewHeight: 270, Stream: testStream,
	}
	tests := []struct {
		name    string
		edit    func(c *Config)
		wantErr string
	}{
		{"unknown scene", func(c *Config) { c.Scene = "cave" }, "unknown scene"},
		{"no view", func(c *Config) { c.ViewWidth = 0 }, "view"},
		{"negative grid", func(c *Config) { c.Stream.Grid = -1 }, "stream"},
		{"negative lead", func(c *Config) { c.Stream.Lead = -1 }, "stream"},
		{"negative lead cap", func(c *Config) { c.Stream.LeadMax = -1 }, "stream"},
		{"no target", func(c *Config) { c.Stream.Target = 0 }, "stream"},
		{"no checks", func(c *Config) { c.Stream.Check = 0 }, "stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.edit(&cfg)
			g, err := New(&cfg)
			if err == nil {
				g.Close()
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

// The spawn view lies in the world even when the world is too small for
// the terrain's ground depth, so the session starts with blocks to play
// in. Found by the Codex review of streaming (2026-11-06).
func TestSpawnStaysInTheWorld(t *testing.T) {
	for _, size := range [][2]int{{1024, 512}, {4096, 4096}, {300, 200}} {
		g, err := New(&Config{
			Width: size[0], Height: size[1], Seed: 0, Workers: 1, Materials: testTable(t),
			Slots: []string{"sand"}, Tool: testTool,
			Scene: SceneTerrain, ViewWidth: 480, ViewHeight: 270, Stream: testStream,
		})
		if err != nil {
			t.Fatal(err)
		}
		x, y := g.Spawn()
		if x < 0 || y < 0 || x+min(480, size[0]) > size[0] || y+min(270, size[1]) > size[1] {
			t.Errorf("%dx%d world: spawn camera (%d, %d) puts the view outside", size[0], size[1], x, y)
		}
		if g.Blocks() == 0 {
			t.Errorf("%dx%d world: no block in memory at the start", size[0], size[1])
		}
		g.Close()
	}
}

// A resumed session starts where it left off, and refuses a state it
// could not have saved.
func TestResume(t *testing.T) {
	tab := testTable(t)
	base := Config{
		Width: 2048, Height: 2048, Seed: 1, Workers: 1, Materials: tab, Slots: []string{"sand", "water", "stone"}, Tool: testTool,
		Scene: SceneTerrain, ViewWidth: 480, ViewHeight: 270, Stream: testStream,
	}
	held := make([]int, tab.Len())
	held[tab.MustLookup("stone")] = 7
	cfg := base
	// The camera's area at (100, 900), checked last at tick 470 with the
	// interval halved, as a session that saved there would have left it.
	saved := StreamState{Area: [4]int{0, 832, 608, 398}, Requested: true, LastCheck: 470, Interval: 15}
	cfg.Resume = &Resume{Tick: 500, Slot: 3, Inventory: held, CameraX: 100, CameraY: 900, HasCamera: true, Stream: &saved}
	g, err := New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if x, y := g.Spawn(); x != 100 || y != 900 {
		t.Errorf("Spawn() = %d, %d, want the saved 100, 900", x, y)
	}
	if got := g.StreamState(); got != saved {
		t.Errorf("StreamState() = %+v, want the saved %+v", got, saved)
	}
	if g.World().Tick() != 500 || g.Slot() != 3 || g.Inventory(tab.MustLookup("stone")) != 7 {
		t.Errorf("resumed at tick %d, slot %d, holding %d stone; want 500, 3, 7",
			g.World().Tick(), g.Slot(), g.Inventory(tab.MustLookup("stone")))
	}
	tests := []struct {
		name    string
		edit    func(c *Config)
		wantErr string
	}{
		{"sandbox", func(c *Config) { c.Scene = SceneSandbox }, "only the terrain"},
		{"slot 0", func(c *Config) { c.Resume.Slot = 0 }, "slot"},
		{"slot past the last", func(c *Config) { c.Resume.Slot = 4 }, "slot"},
		{"short inventory", func(c *Config) { c.Resume.Inventory = held[:2] }, "inventory"},
		{"negative count", func(c *Config) { c.Resume.Inventory = []int{0, -1, 0, 0, 0, 0} }, "holds -1"},
		{"check after the tick", func(c *Config) {
			c.Resume.Tick = 10
			c.Resume.Stream = &StreamState{Requested: true, LastCheck: 11, Interval: 60}
		}, "check"},
		{"no interval", func(c *Config) {
			c.Resume.Stream = &StreamState{Requested: true, Interval: 0}
		}, "interval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			c.Resume = &Resume{Slot: 1, Inventory: make([]int, tab.Len())}
			tt.edit(&c)
			g, err := New(&c)
			if err == nil {
				g.Close()
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

// Upcoming lists the blocks a wider margin would need that are not in
// memory, nearest the camera first, and changes nothing.
func TestUpcoming(t *testing.T) {
	g := newTerrainGame(t, 1)
	x, y := g.Spawn()
	before := g.world.Checksum()
	ahead := g.Upcoming(x, y, 512)
	if len(ahead) == 0 {
		t.Fatal("no blocks ahead with 512 more cells of margin")
	}
	for i, b := range ahead {
		if g.world.Resident(b[0], b[1]) {
			t.Fatalf("block %v is in memory but listed as upcoming", b)
		}
		if i > 0 {
			d := func(b [2]int) int {
				dx, dy := 2*b[0]*512+512-(2*x+480), 2*b[1]*512+512-(2*y+270)
				return dx*dx + dy*dy
			}
			if d(ahead[i-1]) > d(b) {
				t.Fatalf("block %v comes after the farther %v", b, ahead[i-1])
			}
		}
	}
	if len(g.Upcoming(x, y, 0)) != 0 {
		t.Fatal("blocks the view needs now are listed as upcoming, though they are in memory")
	}
	if g.world.Checksum() != before || len(g.resident) != 4 {
		t.Fatal("Upcoming changed the world")
	}
}

// Upcoming allocates nothing once warm: the app calls it every few ticks.
func TestUpcomingDoesNotAllocate(t *testing.T) {
	g := newTerrainGame(t, 1)
	x, y := g.Spawn()
	g.Upcoming(x, y, 512)
	if n := testing.AllocsPerRun(20, func() { g.Upcoming(x, y, 512) }); n != 0 {
		t.Fatalf("Upcoming allocates %v times a call", n)
	}
}
