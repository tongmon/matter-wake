// Package app runs a session in a window: one game.Step per Ebitengine
// Update, at game.Hz.
package app

import (
	"fmt"
	"os"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/input"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/render"
)

// Config describes how to run a session.
type Config struct {
	// Width and Height are the world size in cells, which is also the
	// logical screen size.
	Width, Height int
	// Scale multiplies the window size.
	Scale   int
	Seed    uint64
	Workers int
	// MaterialsPath is the material table to load.
	MaterialsPath string
	// Shots lists ticks to save as PNG files in ShotsDir. When set, the
	// devices are ignored and the program exits after the last shot.
	Shots    []uint32
	ShotsDir string
}

// Slots are the materials keys 1, 2, 3… select. 🧪 sandbox controls.
var slots = []string{"sand", "water", "stone"}

// brushRadius is the dig and place brush radius in cells. 🧪
const brushRadius = 4

// Run opens the window and plays until it closes, or until the last shot.
func Run(cfg *Config) error {
	// Check settings before Ebitengine sees them: it panics on a
	// nonpositive window size, and these come from the command line.
	if cfg.Scale < 1 {
		return fmt.Errorf("app: window scale %d is not positive", cfg.Scale)
	}
	if cfg.Width < 1 || cfg.Height < 1 {
		return fmt.Errorf("app: world size %dx%d is not positive", cfg.Width, cfg.Height)
	}
	data, err := os.ReadFile(cfg.MaterialsPath)
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	mats, err := material.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("app: %s: %w", cfg.MaterialsPath, err)
	}
	g, err := game.New(game.Config{
		Width: cfg.Width, Height: cfg.Height, Seed: cfg.Seed, Workers: cfg.Workers,
		Materials: mats, Slots: slots, BrushRadius: brushRadius,
	})
	if err != nil {
		return err
	}
	defer g.Close()

	a := &app{game: g, rend: render.New(mats, cfg.Width, cfg.Height), width: cfg.Width, height: cfg.Height}
	if len(cfg.Shots) > 0 {
		a.shots = newShooter(cfg.Shots, cfg.ShotsDir, cfg.Width, cfg.Height)
	}
	ebiten.SetTPS(game.Hz)
	ebiten.SetWindowTitle("matter-wake")
	ebiten.SetWindowSize(cfg.Width*cfg.Scale, cfg.Height*cfg.Scale)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// RunGame returns nil when Update returns ebiten.Termination.
	return ebiten.RunGame(a)
}

type app struct {
	game          *game.Game
	rend          *render.Renderer
	shots         *shooter
	in            game.Input
	width, height int
}

func (a *app) Update() error {
	if a.shots != nil {
		done, err := a.shots.capture(a.game, a.rend)
		if err != nil {
			return err
		}
		if done {
			return ebiten.Termination
		}
		// Shots must be reproducible, so the devices are ignored.
		a.game.Step(game.Input{})
		return nil
	}
	a.in = input.Read()
	a.game.Step(a.in)
	return nil
}

func (a *app) Draw(screen *ebiten.Image) {
	a.rend.DrawWorld(screen, a.game.World())
	if a.shots == nil {
		a.rend.DrawOverlay(screen, a.game, a.in.CursorX, a.in.CursorY)
	}
}

func (a *app) Layout(int, int) (int, int) {
	return a.width, a.height
}

// sortedTicks returns the ticks in increasing order without duplicates.
func sortedTicks(ticks []uint32) []uint32 {
	s := slices.Clone(ticks)
	slices.Sort(s)
	return slices.Compact(s)
}
