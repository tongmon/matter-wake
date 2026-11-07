package game

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/scene"
	"github.com/tongmon/matter-wake/internal/world"
)

// testMaterials is the tests' own table, so tuning data/materials.json does
// not move them. Bedrock is harder than testTool can dig.
const testMaterials = `{"materials": [
	{"name": "air", "kind": "empty"},
	{"name": "stone", "kind": "static", "density": 255, "hardness": 10, "hp": 1000},
	{"name": "sand", "kind": "powder", "density": 30, "fallAccel": 0.225, "sinkChance": 25, "slideChance": 50,
	 "hardness": 4, "hp": 300},
	{"name": "water", "kind": "liquid", "density": 20, "fallAccel": 0.375, "sinkChance": 50,
	 "flowSpeed": 0.9, "flowDamping": 0.8, "dispersion": 4},
	{"name": "bedrock", "kind": "static", "density": 255, "hardness": 11, "hp": 1},
	{"name": "wood", "kind": "solid", "density": 6, "friction": 0.9, "restitution": 0.2}
]}`

// testTool digs stone (hardness 10, hp 1000) in four ticks and sand in one.
var testTool = Tool{Power: 10, Rate: 300, Radius: 3}

func testTable(t testing.TB) *material.Table {
	t.Helper()
	tab, err := material.ParseBytes([]byte(testMaterials))
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func newTestGame(t *testing.T, tool Tool) *Game {
	t.Helper()
	g, err := New(&Config{
		Width: 128, Height: 96, Seed: 1, Workers: 2, Materials: testTable(t),
		Slots: []string{"sand", "water", "stone"}, Tool: tool,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

// floor is a cell of the sandbox's stone floor.
func floor(g *Game) (x, y int) {
	return g.world.Width() / 2, g.world.Height() - 1
}

// step runs one tick of g and fails the test on an error.
func step(t testing.TB, g *Game, in Input) {
	t.Helper()
	if err := g.Step(in); err != nil {
		t.Fatal(err)
	}
}

func matAt(t *testing.T, g *Game, x, y int) material.ID {
	t.Helper()
	c, ok := g.world.At(x, y)
	if !ok {
		t.Fatalf("(%d, %d) is outside the world", x, y)
	}
	return c.Mat
}

func dig(x, y int) Input { return Input{CursorX: x, CursorY: y, Dig: true} }

func TestShippedTableWorks(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := material.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(&Config{
		Width: 128, Height: 96, Seed: 1, Workers: 1, Materials: tab,
		Slots: []string{"sand", "water", "stone"}, Tool: testTool,
	})
	if err != nil {
		t.Fatal(err)
	}
	g.Close()
}

func TestSelect(t *testing.T) {
	g := newTestGame(t, testTool)
	if got := g.Selected().Name; got != "sand" {
		t.Fatalf("slot 1 is selected at the start, got %q, want sand", got)
	}
	step(t, g, Input{Select: 3})
	if got := g.Selected().Name; got != "stone" {
		t.Fatalf("slot 3 selected %q, want stone", got)
	}
	step(t, g, Input{Select: 9})
	if got := g.Selected().Name; got != "stone" {
		t.Fatalf("an out-of-range slot changed the selection to %q", got)
	}
}

// A cell is dug out in the tick its damage reaches its hp: ceil(hp / rate)
// ticks of digging.
func TestDigTakesHPOverRate(t *testing.T) {
	tests := []struct {
		rate  uint32
		ticks int
	}{
		{1000, 1}, {5000, 1}, {999, 2}, {500, 2}, {300, 4}, {250, 4}, {7, 143},
	}
	for _, tt := range tests {
		g := newTestGame(t, Tool{Power: 10, Rate: tt.rate})
		stone := g.mats.MustLookup("stone")
		x, y := floor(g)
		for tick := 1; tick <= tt.ticks; tick++ {
			step(t, g, dig(x, y))
			if got := matAt(t, g, x, y); tick < tt.ticks && got != stone {
				t.Fatalf("rate %d: the stone is gone after %d ticks, want %d", tt.rate, tick, tt.ticks)
			}
		}
		if got := matAt(t, g, x, y); got != material.Air {
			t.Fatalf("rate %d: the cell holds %d after %d ticks, want air", tt.rate, got, tt.ticks)
		}
		if got := g.Inventory(stone); got != 1 {
			t.Fatalf("rate %d: the inventory holds %d stone, want 1", tt.rate, got)
		}
	}
}

// A tool digs a material whose hardness is at most its power, and no other.
func TestDigRefusesHarderMaterial(t *testing.T) {
	g := newTestGame(t, Tool{Power: 10, Rate: 5000})
	bedrock := g.mats.MustLookup("bedrock")
	x, y := 20, 20
	g.world.Set(x, y, bedrock)
	for range 100 {
		step(t, g, dig(x, y))
	}
	if got := matAt(t, g, x, y); got != bedrock {
		t.Fatalf("the bedrock (hardness 11) became %d under a power 10 tool", got)
	}
	if got := g.Inventory(bedrock); got != 0 {
		t.Fatalf("the inventory holds %d bedrock, want 0", got)
	}

	// Power 11 digs it in one tick: its hp is 1.
	g = newTestGame(t, Tool{Power: 11, Rate: 1})
	g.world.Set(x, y, bedrock)
	step(t, g, dig(x, y))
	if got := matAt(t, g, x, y); got != material.Air {
		t.Fatalf("a power 11 tool left %d where the bedrock was, want air", got)
	}
}

// Digging takes powders and statics only: liquids and empty cells stay.
func TestDigSkipsLiquids(t *testing.T) {
	g := newTestGame(t, Tool{Power: 255, Rate: 1 << 30, Radius: 0})
	water := g.mats.MustLookup("water")
	x, y := 20, 20
	g.world.Set(x, y, water)
	before := g.world.Count(water)
	// The brush acts before the world moves, so the water is under it.
	step(t, g, dig(x, y))
	if got := g.world.Count(water); got != before {
		t.Fatalf("digging water left %d water cells, want %d", got, before)
	}
	for id := range g.mats.Len() {
		if n := g.Inventory(material.ID(id)); n != 0 {
			t.Fatalf("digging water and air put %d %s in the inventory", n, g.mats.Def(material.ID(id)).Name)
		}
	}
}

// Placing fills the empty cells under the brush from the inventory, one
// cell for one unit, and places nothing once it is empty.
func TestPlaceTakesFromInventory(t *testing.T) {
	g := newTestGame(t, Tool{Power: 10, Rate: 5000, Radius: 1})
	stone := g.mats.MustLookup("stone")
	step(t, g, Input{Select: 3})
	// Nothing to place yet.
	before := g.world.Count(stone)
	step(t, g, Input{CursorX: 64, CursorY: 20, Place: true})
	if got := g.world.Count(stone); got != before {
		t.Fatalf("placing from an empty inventory changed the stone cells from %d to %d", before, got)
	}
	// Dig where the floor meets the wall, both 2 cells thick: the brush of
	// radius 1 covers 5 stone cells there.
	step(t, g, dig(1, g.world.Height()-2))
	held := g.Inventory(stone)
	if held != 5 {
		t.Fatalf("digging 5 stone cells put %d in the inventory", held)
	}
	// A brush of radius 1 in the air covers 5 empty cells.
	placeAt := func(x, y int) {
		t.Helper()
		before, inv := g.world.Count(stone), g.Inventory(stone)
		step(t, g, Input{CursorX: x, CursorY: y, Place: true})
		placed := g.world.Count(stone) - before
		if want := min(inv, 5); placed != want {
			t.Fatalf("with %d stone held, placing added %d cells, want %d", inv, placed, want)
		}
		if got := g.Inventory(stone); got != inv-placed {
			t.Fatalf("placing %d cells left %d in the inventory, want %d", placed, got, inv-placed)
		}
	}
	placeAt(64, 20)
	if got := g.Inventory(stone); got != 0 {
		t.Fatalf("the inventory holds %d stone after placing it all", got)
	}
	placeAt(80, 20)
}

// Creative mode places any slot's material, liquids too, without taking
// it from the inventory. Digging works as in play.
func TestCreativePlacesWithoutInventory(t *testing.T) {
	g, err := New(&Config{
		Width: 128, Height: 96, Seed: 1, Workers: 2, Materials: testTable(t),
		Slots: []string{"sand", "water", "stone"}, Tool: testTool, Creative: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	water, stone := g.mats.MustLookup("water"), g.mats.MustLookup("stone")
	step(t, g, Input{Select: 2})
	before := g.world.Count(water)
	step(t, g, Input{CursorX: 64, CursorY: 20, Place: true})
	if got := g.world.Count(water); got <= before {
		t.Fatalf("creative placing left %d water cells, want more than %d", got, before)
	}
	if got := g.Inventory(water); got != 0 {
		t.Fatalf("creative placing changed the water inventory to %d", got)
	}
	x, y := floor(g)
	for range 4 {
		step(t, g, dig(x, y))
	}
	if got := matAt(t, g, x, y); got != material.Air {
		t.Fatalf("4 ticks of creative digging left %d, want air", got)
	}
	held := g.Inventory(stone)
	if held == 0 {
		t.Fatal("creative digging put no stone in the inventory")
	}
	step(t, g, Input{CursorX: 40, CursorY: 20, Place: true, Select: 3})
	if got := g.Inventory(stone); got != held {
		t.Fatalf("creative placing took stone from the inventory: %d left of %d", got, held)
	}
}

// A short inventory fills the bottom of the brush first, row by row.
func TestPlaceFillsFromTheBottom(t *testing.T) {
	g := newTestGame(t, Tool{Power: 10, Rate: 1, Radius: 1})
	stone := g.mats.MustLookup("stone")
	step(t, g, Input{Select: 3})
	g.inventory[stone] = 2
	// The static stone stays where it is put, in the air.
	step(t, g, Input{CursorX: 64, CursorY: 20, Place: true})
	for _, p := range [][2]int{{64, 21}, {63, 20}} {
		if got := matAt(t, g, p[0], p[1]); got != stone {
			t.Errorf("(%d, %d) holds %d, want stone", p[0], p[1], got)
		}
	}
	for _, p := range [][2]int{{64, 20}, {65, 20}, {64, 19}} {
		if got := matAt(t, g, p[0], p[1]); got != material.Air {
			t.Errorf("(%d, %d) holds %d, want air", p[0], p[1], got)
		}
	}
}

// Damage stays in the cell: digging that stops, moves away or places
// in between carries on where it left off.
func TestDigDamageLastsWhenDiggingStops(t *testing.T) {
	g := newTestGame(t, testTool) // stone takes 4 ticks
	stone := g.mats.MustLookup("stone")
	x, y := floor(g)
	pauses := []Input{{CursorX: x, CursorY: y}, dig(x-20, y), {CursorX: x, CursorY: y, Place: true}}
	for _, pause := range pauses {
		step(t, g, dig(x, y))
		if got := matAt(t, g, x, y); got != stone {
			t.Fatalf("the stone is gone before 4 ticks of digging: the cell holds %d", got)
		}
		step(t, g, pause)
	}
	step(t, g, dig(x, y))
	if got := matAt(t, g, x, y); got != material.Air {
		t.Fatalf("4 interrupted ticks of digging left %d, want air", got)
	}
}

// Moving the brush keeps the damage of the cells it still covers.
func TestDigDamageFollowsTheCellsUnderTheBrush(t *testing.T) {
	g := newTestGame(t, testTool) // radius 3, stone takes 4 ticks
	x, y := floor(g)
	for i := range 4 {
		step(t, g, dig(x+i%2, y))
	}
	// (x, y) and (x+1, y) were under the brush for all 4 ticks.
	for _, cx := range []int{x, x + 1} {
		if got := matAt(t, g, cx, y); got != material.Air {
			t.Fatalf("(%d, %d) holds %d after 4 ticks under the moving brush, want air", cx, y, got)
		}
	}
}

// The damage belongs to the cell that took it: another cell in the same
// place starts from nothing.
func TestDigDamageDoesNotPassToAnotherCell(t *testing.T) {
	g := newTestGame(t, Tool{Power: 10, Rate: 100}) // stone takes 10 ticks
	stone := g.mats.MustLookup("stone")
	x, y := floor(g)
	for range 6 {
		step(t, g, dig(x, y))
	}
	g.world.Set(x, y, stone)
	// With the old cell's 600 damage, 5 more ticks would dig it out.
	for range 5 {
		step(t, g, dig(x, y))
	}
	if got := matAt(t, g, x, y); got != stone {
		t.Fatalf("a new stone cell inherited the old one's damage: the cell holds %d", got)
	}
	for range 5 {
		step(t, g, dig(x, y))
	}
	if got := matAt(t, g, x, y); got != material.Air {
		t.Fatalf("10 ticks of digging the new stone cell left %d, want air", got)
	}
}

// Every cell the tools take or give comes from or goes to the inventory, and
// the world's own tick neither makes nor destroys cells, so each
// material's cells plus its inventory stay the same.
func TestDigAndPlaceConserveCells(t *testing.T) {
	g := newTestGame(t, Tool{Power: 10, Rate: 400, Radius: 4})
	start := totals(g)
	s := rng.New(5)
	var in Input
	dug, placed := 0, 0
	for tick := range 1500 {
		if s.IntN(8) == 0 {
			in = Input{
				CursorX: s.IntN(140) - 6, CursorY: s.IntN(108) - 6,
				Place: s.IntN(3) == 0, Dig: s.IntN(2) == 0, Select: s.IntN(5),
			}
		}
		held := carried(g)
		step(t, g, in)
		if got := totals(g); !slices.Equal(got, start) {
			t.Fatalf("tick %d: cells plus inventory are %v, want %v", tick, got, start)
		}
		switch now := carried(g); {
		case now > held:
			dug++
		case now < held:
			placed++
		}
	}
	if dug == 0 || placed == 0 {
		t.Fatalf("the inputs dug in %d ticks and placed in %d; both must happen", dug, placed)
	}
}

// totals returns each material's cells plus its inventory.
func totals(g *Game) []int {
	n := make([]int, g.mats.Len())
	g.world.Counts(n)
	for id := 1; id < len(n); id++ {
		n[id] += g.Inventory(material.ID(id))
	}
	n[material.Air] = 0 // air is what digging leaves; it is not kept
	return n
}

// carried returns how many cells the inventory holds in all.
func carried(g *Game) int {
	n := 0
	for id := range g.mats.Len() {
		n += g.Inventory(material.ID(id))
	}
	return n
}

// Step runs every tick, so its tools must not allocate.
func TestStepDoesNotAllocate(t *testing.T) {
	g := newTestGame(t, Tool{Power: 10, Rate: 100, Radius: 4})
	x, y := floor(g)
	inputs := []Input{dig(x, y), dig(x+1, y), {CursorX: 40, CursorY: 20, Place: true, Select: 3}, {}}
	for _, in := range inputs {
		if n := testing.AllocsPerRun(20, func() {
			if err := g.Step(in); err != nil {
				t.Error(err)
			}
		}); n != 0 {
			t.Fatalf("Step(%+v) allocates %v times", in, n)
		}
	}
}

// The collapse scene is the room scene.Collapse lays out, all of it in
// memory, seen from the top left.
func TestCollapseScene(t *testing.T) {
	tab := testTable(t)
	g, err := New(&Config{
		Width: 480, Height: 270, Seed: 5, Workers: 2, Materials: tab,
		Slots: []string{"sand"}, Tool: testTool, Scene: SceneCollapse,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	w, err := world.New(world.Config{Width: 480, Height: 270, Seed: 5, Workers: 2, Materials: tab})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := scene.Collapse(w, tab, 5); err != nil {
		t.Fatal(err)
	}
	if got, want := g.World().Checksum(), w.Checksum(); got != want {
		t.Fatalf("the session's world has checksum %#x, scene.Collapse's %#x", got, want)
	}
	if x, y := g.Spawn(); x != 0 || y != 0 || g.Blocks() != 1 {
		t.Fatalf("spawn (%d, %d) with %d blocks, want (0, 0) with the one block of the room", x, y, g.Blocks())
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	tab := testTable(t)
	tests := []struct {
		name string
		cfg  Config
	}{
		{"unknown slot", Config{Slots: []string{"gold"}, Tool: testTool}},
		{"no slots", Config{Tool: testTool}},
		{"tool deals no damage", Config{Slots: []string{"sand"}, Tool: Tool{Power: 10}}},
		{"negative radius", Config{Slots: []string{"sand"}, Tool: Tool{Power: 10, Rate: 1, Radius: -1}}},
		{"unknown body", Config{Slots: []string{"sand"}, Tool: testTool, Body: BodyTool{Material: "oak", Width: 4, Height: 4}}},
		{"body of sand", Config{Slots: []string{"sand"}, Tool: testTool, Body: BodyTool{Material: "sand", Width: 4, Height: 4}}},
		{"body of no size", Config{Slots: []string{"sand"}, Tool: testTool, Body: BodyTool{Material: "wood", Height: 4}}},
	}
	for _, tt := range tests {
		tt.cfg.Width, tt.cfg.Height, tt.cfg.Materials = 64, 64, tab
		if g, err := New(&tt.cfg); err == nil {
			g.Close()
			t.Errorf("%s: New accepted %+v", tt.name, tt.cfg)
		}
	}
}

// The body tool drops a body centred on the cursor in the rooms. The body
// falls to the floor, and its pixels stay what the tool dropped.
func TestBodyToolDropsBodies(t *testing.T) {
	tab := testTable(t)
	g, err := New(&Config{
		Width: 128, Height: 96, Seed: 1, Workers: 2, Materials: tab,
		Slots: []string{"sand"}, Tool: testTool, Body: BodyTool{Material: "wood", Width: 8, Height: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	step(t, g, Input{CursorX: 64, CursorY: 10, Drop: true})
	step(t, g, Input{CursorX: 64, CursorY: 10, Drop: true}) // the first is still there
	if g.Bodies() != 1 {
		t.Fatalf("%d bodies, want 1", g.Bodies())
	}
	if c, _ := g.World().At(60, 7); c.Mat != tab.MustLookup("wood") {
		t.Fatalf("the body's top-left pixel is not at (60, 7): %+v", c)
	}
	for range 120 {
		step(t, g, Input{})
	}
	n := make([]int, tab.Len())
	g.BodyPixels(n)
	if n[tab.MustLookup("wood")] != 48 {
		t.Fatalf("the bodies hold %d pixels of wood, want 48", n[tab.MustLookup("wood")])
	}
	floorY := g.World().Height() - max(g.World().Width()/120, 2)
	if c, _ := g.World().At(64, floorY-1); c.Mat != tab.MustLookup("wood") {
		t.Fatalf("no body on the floor at (64, %d): %+v", floorY-1, c)
	}
}

// The terrain has no bodies yet: the body tool does nothing there.
func TestTerrainHasNoBodies(t *testing.T) {
	g, err := New(&Config{
		Width: 1024, Height: 2048, Seed: 1, Workers: 2, Materials: testTable(t), Scene: SceneTerrain,
		ViewWidth: 64, ViewHeight: 64, Stream: Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 12, Check: 60},
		Slots: []string{"sand"}, Tool: testTool, Body: BodyTool{Material: "wood", Width: 8, Height: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	x, y := g.Spawn()
	step(t, g, Input{CameraX: x, CameraY: y, CursorX: x + 32, CursorY: y + 2, Drop: true})
	if g.Bodies() != 0 {
		t.Fatalf("the terrain holds %d bodies", g.Bodies())
	}
}
