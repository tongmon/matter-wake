// Package game is one play session: the world and the player's tools,
// advanced by one Input per tick.
//
// Input is the only thing from outside that changes a session, so a
// recorded sequence of inputs replays it exactly.
package game

import (
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
	// Place puts the selected material under the cursor.
	Place bool
	// Dig clears the cells under the cursor.
	Dig bool
	// Select picks a material slot, 1-based; 0 keeps the current one.
	Select int
}

// Config describes a new session.
type Config struct {
	Width, Height int
	Seed          uint64
	Workers       int
	Materials     *material.Table
	// Slots name the materials the player can place, in key order.
	Slots []string
	// BrushRadius is the radius in cells of the dig and place brush.
	BrushRadius int
}

// Game is a play session. It is not safe for concurrent use.
type Game struct {
	world    *world.World
	mats     *material.Table
	slots    []material.ID
	selected int
	brush    int
}

// New starts a session in the sandbox scene.
func New(cfg Config) (*Game, error) {
	if len(cfg.Slots) == 0 {
		return nil, fmt.Errorf("game: no material slots")
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
	return &Game{world: w, mats: cfg.Materials, slots: slots, brush: max(cfg.BrushRadius, 0)}, nil
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

// BrushRadius returns the radius of the dig and place brush.
func (g *Game) BrushRadius() int {
	return g.brush
}

// Step applies one tick of input and advances the world.
func (g *Game) Step(in Input) {
	if in.Select >= 1 && in.Select <= len(g.slots) {
		g.selected = in.Select - 1
	}
	switch {
	case in.Dig:
		g.world.Paint(in.CursorX, in.CursorY, g.brush, material.Air, false)
	case in.Place:
		g.world.Paint(in.CursorX, in.CursorY, g.brush, g.slots[g.selected], true)
	}
	g.world.Step()
}
