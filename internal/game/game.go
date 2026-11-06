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

// Input is what the player does during one tick.
type Input struct {
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
	// wear holds the damage of the cells being dug.
	wear wearTable
}

// New starts a session in the sandbox scene with an empty inventory.
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
	w, err := world.New(world.Config{
		Width: cfg.Width, Height: cfg.Height, Seed: cfg.Seed, Workers: cfg.Workers, Materials: cfg.Materials,
	})
	if err != nil {
		return nil, err
	}
	if err := scene.Sandbox(w, cfg.Materials); err != nil {
		w.Close()
		return nil, err
	}
	return &Game{
		world: w, mats: cfg.Materials, slots: slots, tool: cfg.Tool, creative: cfg.Creative,
		inventory: make([]int, cfg.Materials.Len()),
		wear:      newWearTable(cfg.Tool.Radius),
	}, nil
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

// Step applies one tick of input and advances the world.
func (g *Game) Step(in Input) {
	if in.Select >= 1 && in.Select <= len(g.slots) {
		g.selected = in.Select - 1
	}
	switch {
	case in.Dig:
		g.dig(in.CursorX, in.CursorY)
	case in.Place:
		g.place(in.CursorX, in.CursorY)
	}
	if !in.Dig {
		g.wear.forget()
	}
	g.world.Step()
}

// dig deals the tool's damage to every cell under the brush centred on
// (cx, cy) that it can dig, and moves the cells dug out into the
// inventory. How long a cell keeps its damage is up to g.wear.
func (g *Game) dig(cx, cy int) {
	r, width := g.tool.Radius, g.world.Width()
	g.wear.begin()
	// Rows from the top, each from the left: increasing cell order.
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
			cell := y*width + x
			// The damage is below the hp: a cell is dug out when it reaches it.
			damage := g.wear.dealt(cell, c)
			if def.HP-damage <= g.tool.Rate {
				g.world.Set(x, y, material.Air)
				g.inventory[c.Mat]++
				continue
			}
			g.wear.keep(cell, c, damage+g.tool.Rate)
		}
	}
	g.wear.end()
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
