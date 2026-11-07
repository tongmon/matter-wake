// Package game is one play session: the world and the player's tools,
// advanced by one Input per tick.
//
// Input is the only thing from outside that changes a session, so a
// recorded sequence of inputs replays it exactly.
//
// The tools move cells between the world and the inventory and never make
// or destroy one, and neither does the world's own tick. So for every
// material, its cells in the world plus its count in the inventory stay
// the same for the whole session. Creative mode, for development, places
// without taking from the inventory and so makes cells.
//
// The rooms also hold rigid bodies (internal/rigid), which the body tool,
// a development tool, drops from nothing. A body's pixels are cells of a
// solid material; it pushes the cells in its way into the world's queue of
// cells to put back, so for every other material the cells in the world
// plus those in the queue plus the inventory stay the same. The solids'
// pixels are counted in the bodies' images, since a turned body covers
// more or fewer cells than it has pixels (BodyPixels).
//
// A session plays the sandbox, a small room kept whole in memory; the
// collapse room, as small, for measuring; or the terrain, the big world of
// which only the blocks around the camera stay in memory (stream.go). The
// terrain makes a block the first time the camera comes near it; after
// that the block lives in the store. The terrain has no bodies yet: they
// are not saved, and would not leave memory with the blocks.
package game

import (
	"errors"
	"fmt"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rigid"
	"github.com/tongmon/matter-wake/internal/scene"
	"github.com/tongmon/matter-wake/internal/world"
)

// Hz is how many ticks run per second of play.
const Hz = 60

// Scene is the world a session plays in.
type Scene string

const (
	// SceneSandbox is a small test room, all of it in memory. It is the
	// zero value.
	SceneSandbox Scene = ""
	// SceneTerrain is the big seeded world, kept in memory around the
	// camera only (design.md 7절).
	SceneTerrain Scene = "terrain"
	// SceneCollapse is a room like the sandbox whose cells all fall at
	// once: the heaviest scene for a screen, to measure (design.md 2절).
	SceneCollapse Scene = "collapse"
)

// Input is what the player does during one tick.
type Input struct {
	// CameraX and CameraY are the top-left cell of what the camera shows.
	// The terrain keeps the blocks around it in memory; the sandbox
	// ignores it.
	CameraX, CameraY int
	// CursorX and CursorY are the cursor position in world cells.
	CursorX, CursorY int
	// Place fills the empty cells under the cursor with the selected
	// material, taken from the inventory.
	Place bool
	// Dig wears down the cells under the cursor and moves the ones it digs
	// out into the inventory. It wins over Place.
	Dig bool
	// Select picks a material slot, 1-based; 0 keeps the current one.
	Select int
	// Drop drops a body of the body tool centred on the cursor, in the
	// rooms. It does nothing where the body would not fit.
	Drop bool
}

// Tool is what the player digs and places with.
type Tool struct {
	// Power is the hardest material the tool digs: it digs a material whose
	// hardness is at most Power.
	Power uint8
	// Rate is the damage the tool deals in one tick to each cell under it.
	// A cell is dug out once the damage it took reaches its material's hp.
	Rate uint32
	// Radius is the radius in cells of the brush it digs and places with.
	Radius int
}

// BodyTool is what the body tool drops: rectangular bodies of one solid
// material. It is a development tool, for trying bodies out, until bodies
// come from destruction.
type BodyTool struct {
	// Material names the solid the bodies are made of; empty for no tool.
	Material string
	// Width and Height are the bodies' size in pixels.
	Width, Height int
}

// Config describes a new session.
type Config struct {
	Width, Height int
	Seed          uint64
	Workers       int
	Materials     *material.Table
	// Scene is the world to play in.
	Scene Scene
	// ViewWidth and ViewHeight are how many cells the camera shows. With
	// the stream they set which blocks of the terrain stay in memory.
	ViewWidth, ViewHeight int
	Stream                Stream
	// Budget is how many cells a tick may scan, 0 for no limit. Over it,
	// the chunks farthest from the middle of the view wait for a later
	// tick (world.Config.Budget).
	Budget int
	// Store keeps the blocks the terrain unloads. Nil keeps them in memory
	// for the session only, which replays and shots rely on.
	Store Store
	// Resume goes on with a saved terrain session instead of starting a
	// new one. Its blocks come from Store.
	Resume *Resume
	// Slots name the materials the player can place, in key order.
	Slots []string
	Tool  Tool
	// Body is what the body tool drops.
	Body BodyTool
	// Creative places without taking from the inventory: any slot's
	// material, as much as the brush holds. It is for trying materials out
	// in development; play starts with an empty inventory instead.
	Creative bool
}

// Resume is where a saved session left off.
type Resume struct {
	// Tick is how many ticks the world had run.
	Tick uint32
	// Slot is the selected slot, 1-based.
	Slot int
	// Inventory holds how many cells of each material the player carried,
	// by material ID.
	Inventory []int
	// CameraX and CameraY are where the camera was, if HasCamera is set;
	// else the camera starts where a new session's does.
	CameraX, CameraY int
	HasCamera        bool
	// Blocks lists the blocks that were in memory, as (bx, by). They come
	// back before those the camera needs, so the blocks the stream had
	// kept go on as they would have.
	Blocks [][2]int
	// Stream is where the stream's requests and checks stood. Nil starts
	// them anew: the camera requests its area with nothing ahead, and the
	// first check comes Check ticks after Tick.
	Stream *StreamState
	// Chunks lists where the chunks of those blocks stood, and Scheduled
	// is set when the save recorded it; then the chunks not listed had
	// nothing to scan. Which chunks wait for the budget depends on it.
	// Without it, the blocks come back awake whole.
	Chunks    []world.ChunkState
	Scheduled bool
}

// Game is a play session. It is not safe for concurrent use.
type Game struct {
	world    *world.World
	mats     *material.Table
	slots    []material.ID
	selected int
	tool     Tool
	creative bool
	// inventory holds how many cells of each material the player carries,
	// indexed by material.ID.
	inventory []int
	// terrain makes the blocks of the big world, nil in the sandbox. The
	// fields after it are for the terrain too.
	terrain *scene.Terrain
	store   Store
	limits  Stream
	// streaming is where the stream's requests and checks stand.
	streaming StreamState
	// resident lists the blocks in memory.
	resident     [][2]int
	viewW, viewH int
	// spawnX and spawnY are where the camera starts.
	spawnX, spawnY int
	// ahead and aheadList are Upcoming's buffers.
	ahead     []aheadBlock
	aheadList [][2]int
	// bodies are the rigid bodies of the rooms, nil in the terrain.
	// body is the body tool's material and size; bodyW is 0 without one.
	bodies       *rigid.System
	body         material.ID
	bodyW, bodyH int
}

// New starts a session in cfg.Scene with an empty inventory. In the
// terrain it loads the blocks around Spawn.
func New(cfg *Config) (*Game, error) {
	if len(cfg.Slots) == 0 {
		return nil, errors.New("game: no material slots")
	}
	switch {
	case cfg.Tool.Rate == 0:
		return nil, errors.New("game: the tool deals no damage")
	case cfg.Tool.Radius < 0:
		return nil, fmt.Errorf("game: tool radius %d is negative", cfg.Tool.Radius)
	}
	slots := make([]material.ID, len(cfg.Slots))
	for i, name := range cfg.Slots {
		id, ok := cfg.Materials.Lookup(name)
		switch {
		case !ok:
			return nil, fmt.Errorf("game: slot %d names unknown material %q", i+1, name)
		case cfg.Materials.Def(id).Kind == material.Solid:
			// A solid's cells are the pixels of bodies; the body tool
			// makes them.
			return nil, fmt.Errorf("game: slot %d names %q, a solid, which the brush cannot place", i+1, name)
		}
		slots[i] = id
	}
	switch cfg.Scene {
	case SceneSandbox, SceneTerrain, SceneCollapse:
	default:
		return nil, fmt.Errorf("game: unknown scene %q", cfg.Scene)
	}
	tick, err := resumeTick(cfg)
	if err != nil {
		return nil, err
	}
	w, err := world.New(world.Config{
		Width: cfg.Width, Height: cfg.Height, Seed: cfg.Seed, Workers: cfg.Workers, Materials: cfg.Materials,
		Streamed: cfg.Scene == SceneTerrain, Tick: tick, Budget: cfg.Budget,
	})
	if err != nil {
		return nil, err
	}
	g := &Game{
		world: w, mats: cfg.Materials, slots: slots, tool: cfg.Tool, creative: cfg.Creative,
		inventory: make([]int, cfg.Materials.Len()),
	}
	if err := g.setUp(cfg); err != nil {
		g.Close()
		return nil, err
	}
	return g, nil
}

// setUpBodies checks the body tool and, in the rooms, makes the System
// that holds the bodies.
func (g *Game) setUpBodies(cfg *Config) error {
	if t := cfg.Body; t.Material != "" {
		id, ok := cfg.Materials.Lookup(t.Material)
		switch {
		case !ok:
			return fmt.Errorf("game: the body tool names unknown material %q", t.Material)
		case cfg.Materials.Def(id).Kind != material.Solid:
			return fmt.Errorf("game: the body tool's material %q is not a solid", t.Material)
		case t.Width < 1 || t.Height < 1:
			return fmt.Errorf("game: the body tool's size %dx%d is not positive", t.Width, t.Height)
		}
		g.body, g.bodyW, g.bodyH = id, t.Width, t.Height
	}
	if cfg.Scene == SceneTerrain {
		return nil
	}
	s, err := rigid.New(g.world, cfg.Materials)
	if err != nil {
		return fmt.Errorf("game: %w", err)
	}
	g.bodies = s
	return nil
}

// resumeTick checks cfg.Resume and returns the tick the world starts at.
func resumeTick(cfg *Config) (uint32, error) {
	r := cfg.Resume
	switch {
	case r == nil:
		return 0, nil
	case cfg.Scene != SceneTerrain:
		return 0, errors.New("game: only the terrain resumes; the sandbox starts new")
	case r.Slot < 1 || r.Slot > len(cfg.Slots):
		return 0, fmt.Errorf("game: resumed slot %d is not one of the %d slots", r.Slot, len(cfg.Slots))
	case len(r.Inventory) != cfg.Materials.Len():
		return 0, fmt.Errorf("game: resumed inventory has %d materials, the table %d", len(r.Inventory), cfg.Materials.Len())
	}
	for id, n := range r.Inventory {
		if n < 0 {
			return 0, fmt.Errorf("game: resumed inventory holds %d of material %d", n, id)
		}
	}
	if s := r.Stream; s != nil {
		switch {
		case s.LastCheck > r.Tick:
			return 0, fmt.Errorf("game: resumed stream checked last at tick %d, after the saved tick %d", s.LastCheck, r.Tick)
		case s.Interval < 1:
			return 0, fmt.Errorf("game: resumed stream interval %d is not positive", s.Interval)
		}
	}
	return r.Tick, nil
}

// setUp lays out the session's scene.
func (g *Game) setUp(cfg *Config) error {
	if err := g.setUpBodies(cfg); err != nil {
		return err
	}
	switch cfg.Scene {
	case SceneSandbox:
		return scene.Sandbox(g.world, cfg.Materials)
	case SceneCollapse:
		return scene.Collapse(g.world, cfg.Materials, cfg.Seed)
	case SceneTerrain:
	}
	switch {
	case cfg.ViewWidth < 1 || cfg.ViewHeight < 1:
		return fmt.Errorf("game: view %dx%d is not positive", cfg.ViewWidth, cfg.ViewHeight)
	case !cfg.Stream.valid():
		return fmt.Errorf("game: stream %+v needs a grid and leads of 0 or more, a target of 1 block or more and checks 1 tick or more apart", cfg.Stream)
	}
	t, err := scene.NewTerrain(cfg.Seed, cfg.Width, cfg.Height, cfg.Materials)
	if err != nil {
		return err
	}
	g.terrain, g.store, g.limits = t, cfg.Store, cfg.Stream
	g.viewW, g.viewH = cfg.ViewWidth, cfg.ViewHeight
	if g.store == nil {
		g.store = newMemoryStore(cfg.Materials)
	}
	x := g.world.Width() / 2
	camX, camY := x-g.viewW/2, t.Surface(x)-g.viewH/2
	g.streaming = StreamState{LastCheck: g.world.Tick(), Interval: cfg.Stream.Check}
	if r := cfg.Resume; r != nil {
		if r.HasCamera {
			camX, camY = r.CameraX, r.CameraY
		}
		g.selected = r.Slot - 1
		copy(g.inventory, r.Inventory)
		if r.Stream != nil {
			g.streaming = *r.Stream
		}
		if err := g.load(r.Blocks); err != nil {
			return err
		}
	}
	// Keep the view in the world, moving it as little as it takes.
	g.spawnX = min(max(camX, 0), max(g.world.Width()-g.viewW, 0))
	g.spawnY = min(max(camY, 0), max(g.world.Height()-g.viewH, 0))
	// Only the request: the first check belongs to the first tick, which
	// sees that tick's camera, as the saved session's next tick would.
	if err := g.request(g.spawnX, g.spawnY); err != nil {
		return err
	}
	if r := cfg.Resume; r != nil && r.Scheduled {
		// Loading the blocks woke them whole; put back what the saved
		// session had left to scan.
		if err := g.world.SetChunkStates(r.Chunks); err != nil {
			return fmt.Errorf("game: %w", err)
		}
	}
	return nil
}

// Blocks returns how many blocks of the world are in memory.
func (g *Game) Blocks() int {
	if g.terrain == nil {
		cols, rows := g.world.Blocks()
		return cols * rows
	}
	return len(g.resident)
}

// Spawn returns where the camera starts: in a new terrain session, over
// the middle of the world with the ground across the middle of the view;
// in a resumed one, where it was; in the sandbox, at the top left. The
// terrain moves it as little as it takes to keep the view in the world.
func (g *Game) Spawn() (cameraX, cameraY int) {
	return g.spawnX, g.spawnY
}

// Materials returns the session's material table.
func (g *Game) Materials() *material.Table {
	return g.mats
}

// Slot returns the selected slot, 1-based.
func (g *Game) Slot() int {
	return g.selected + 1
}

// SaveBlocks puts every block of the terrain in memory into the store, so
// the store holds the whole world as it is, and keeps them in memory: the
// session goes on as if nothing happened (TestSavingBlocksChangesNothing).
// The sandbox has nothing to save.
func (g *Game) SaveBlocks() error {
	// Taking a block out and back wakes it whole. Which chunks wait for
	// the budget depends on what each has left to scan, so the chunks get
	// back where they stood and the session goes on as if unsaved.
	states := g.world.ChunkStates()
	for _, b := range g.resident {
		blk, err := g.world.UnloadBlock(b[0], b[1])
		if err != nil {
			return fmt.Errorf("game: %w", err)
		}
		saveErr := g.store.Save(b[0], b[1], blk)
		if err := g.world.LoadBlock(b[0], b[1], blk); err != nil {
			return fmt.Errorf("game: %w", err)
		}
		if saveErr != nil {
			return fmt.Errorf("game: block (%d, %d): %w", b[0], b[1], saveErr)
		}
	}
	if err := g.world.SetChunkStates(states); err != nil {
		return fmt.Errorf("game: %w", err)
	}
	return nil
}

// ResidentBlocks returns the blocks in memory, as (bx, by), in row order.
func (g *Game) ResidentBlocks() [][2]int {
	blocks := slices.Clone(g.resident)
	slices.SortFunc(blocks, func(a, b [2]int) int {
		if a[1] != b[1] {
			return a[1] - b[1]
		}
		return a[0] - b[0]
	})
	return blocks
}

// Close releases the session's resources.
func (g *Game) Close() {
	if g.bodies != nil {
		g.bodies.Close()
	}
	g.world.Close()
}

// Bodies returns how many rigid bodies the session holds.
func (g *Game) Bodies() int {
	if g.bodies == nil {
		return 0
	}
	return g.bodies.Bodies()
}

// BodyPixels adds to n[id], for every material id, how many pixels the
// bodies' images hold of it.
func (g *Game) BodyPixels(n []int) {
	if g.bodies != nil {
		g.bodies.Pixels(n)
	}
}

// World returns the session's world for drawing. Callers must not change it.
func (g *Game) World() *world.World {
	return g.world
}

// Selected returns the material the player places.
func (g *Game) Selected() material.Def {
	return g.mats.Def(g.slots[g.selected])
}

// Slots returns the materials the slots hold, in key order. Callers must
// not change it.
func (g *Game) Slots() []material.ID {
	return g.slots
}

// Tool returns the tool the player digs and places with.
func (g *Game) Tool() Tool {
	return g.tool
}

// Creative reports whether the session places without taking from the
// inventory.
func (g *Game) Creative() bool {
	return g.creative
}

// Inventory returns how many cells of id the player carries.
func (g *Game) Inventory(id material.ID) int {
	return g.inventory[id]
}

// Step applies one tick of input and advances the world. In the terrain it
// first loads and unloads blocks for the camera; an error there, from the
// store, ends the session.
//
// The tools act first. Then the bodies' pixels are written where Box2D's
// last Step left them and the terrain near them is made collision; the
// world's tick runs the cells and puts back the cells the bodies pushed
// out; and Box2D steps (internal/rigid).
func (g *Game) Step(in Input) error {
	if g.terrain != nil {
		if err := g.stream(in.CameraX, in.CameraY); err != nil {
			return err
		}
		// The rooms show all of the world, whose middle is the focus
		// already.
		g.world.SetFocus(in.CameraX+g.viewW/2, in.CameraY+g.viewH/2)
	}
	if in.Select >= 1 && in.Select <= len(g.slots) {
		g.selected = in.Select - 1
	}
	switch {
	case in.Dig:
		g.dig(in.CursorX, in.CursorY)
	case in.Place:
		g.place(in.CursorX, in.CursorY)
	}
	if g.bodies == nil {
		g.world.Step()
		return nil
	}
	if in.Drop && g.bodyW > 0 {
		if _, err := g.bodies.Drop(g.body, in.CursorX-g.bodyW/2, in.CursorY-g.bodyH/2, g.bodyW, g.bodyH); err != nil {
			return fmt.Errorf("game: %w", err)
		}
	}
	g.bodies.Write()
	if err := g.bodies.Collide(); err != nil {
		return fmt.Errorf("game: %w", err)
	}
	g.world.Step()
	g.bodies.Step()
	return nil
}

// dig deals the tool's damage to every cell under the brush centred on
// (cx, cy) that it can dig, and moves the cells it uses up into the
// inventory. The damage stays in the cells (world.World.Wear).
func (g *Game) dig(cx, cy int) {
	r := g.tool.Radius
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			c, ok := g.world.At(x, y)
			if !ok || dx*dx+dy*dy > r*r {
				continue
			}
			def := g.mats.Def(c.Mat)
			if !def.Kind.Diggable() || def.Hardness > g.tool.Power {
				continue
			}
			if g.world.Wear(x, y, g.tool.Rate) {
				g.world.Set(x, y, material.Air)
				g.inventory[c.Mat]++
			}
		}
	}
}

// place fills the empty cells under the brush centred on (cx, cy) with the
// selected material, one from the inventory for each, in rows from the
// bottom, so a short inventory fills a hole from its floor. It stops when
// the inventory runs out. In creative mode it takes nothing and fills the
// brush.
func (g *Game) place(cx, cy int) {
	r, sel := g.tool.Radius, g.slots[g.selected]
	if g.mats.Def(sel).Kind == material.Empty {
		return // placing air would fill nothing
	}
	for y := cy + r; y >= cy-r; y-- {
		for x := cx - r; x <= cx+r; x++ {
			if !g.creative && g.inventory[sel] == 0 {
				return
			}
			dx, dy := x-cx, y-cy
			c, ok := g.world.At(x, y)
			if !ok || dx*dx+dy*dy > r*r || g.mats.Def(c.Mat).Kind != material.Empty {
				continue
			}
			g.world.Set(x, y, sel)
			if !g.creative {
				g.inventory[sel]--
			}
		}
	}
}
