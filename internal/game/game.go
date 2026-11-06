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
// A session plays either the sandbox, a small room kept whole in memory,
// or the terrain, the big world of which only the blocks around the camera
// stay in memory (stream.go). The terrain makes a block the first time the
// camera comes near it; after that the block lives in the store.
package game

import (
	"errors"
	"fmt"

	"github.com/tongmon/matter-wake/internal/material"
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
	// Store keeps the blocks the terrain unloads. Nil keeps them in memory
	// for the session only, which replays and shots rely on.
	Store Store
	// Slots name the materials the player can place, in key order.
	Slots []string
	Tool  Tool
	// Creative places without taking from the inventory: any slot's
	// material, as much as the brush holds. It is for trying materials out
	// in development; play starts with an empty inventory instead.
	Creative bool
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
	// resident lists the blocks in memory.
	resident     [][2]int
	viewW, viewH int
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
		if !ok {
			return nil, fmt.Errorf("game: slot %d names unknown material %q", i+1, name)
		}
		slots[i] = id
	}
	switch cfg.Scene {
	case SceneSandbox, SceneTerrain:
	default:
		return nil, fmt.Errorf("game: unknown scene %q", cfg.Scene)
	}
	w, err := world.New(world.Config{
		Width: cfg.Width, Height: cfg.Height, Seed: cfg.Seed, Workers: cfg.Workers, Materials: cfg.Materials,
		Streamed: cfg.Scene == SceneTerrain,
	})
	if err != nil {
		return nil, err
	}
	g := &Game{
		world: w, mats: cfg.Materials, slots: slots, tool: cfg.Tool, creative: cfg.Creative,
		inventory: make([]int, cfg.Materials.Len()),
	}
	if err := g.setUp(cfg); err != nil {
		w.Close()
		return nil, err
	}
	return g, nil
}

// setUp lays out the session's scene.
func (g *Game) setUp(cfg *Config) error {
	if cfg.Scene == SceneSandbox {
		return scene.Sandbox(g.world, cfg.Materials)
	}
	switch {
	case cfg.ViewWidth < 1 || cfg.ViewHeight < 1:
		return fmt.Errorf("game: view %dx%d is not positive", cfg.ViewWidth, cfg.ViewHeight)
	case cfg.Stream.Margin < 0 || cfg.Stream.MaxBlocks < 1:
		return fmt.Errorf("game: stream %+v needs a margin of 0 or more and room for a block", cfg.Stream)
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
	x, y := g.Spawn()
	return g.stream(x, y)
}

// Spawn returns where the camera starts: in the terrain, over the middle
// of the world with the ground across the middle of the view; in the
// sandbox, at the top left.
func (g *Game) Spawn() (cameraX, cameraY int) {
	if g.terrain == nil {
		return 0, 0
	}
	x := g.world.Width() / 2
	return x - g.viewW/2, max(g.terrain.Surface(x)-g.viewH/2, 0)
}

// Close releases the session's resources.
func (g *Game) Close() {
	g.world.Close()
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
func (g *Game) Step(in Input) error {
	if g.terrain != nil {
		if err := g.stream(in.CameraX, in.CameraY); err != nil {
			return err
		}
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
	g.world.Step()
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
