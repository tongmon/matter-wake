// Package app runs a session in a window: one game.Step per Ebitengine
// Update, at game.Hz.
package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/input"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/render"
	"github.com/tongmon/matter-wake/internal/replay"
)

// Config describes how to run a session.
type Config struct {
	// Scene is the world to play in. A replay sets it instead.
	Scene game.Scene
	// Width and Height are the world size in cells. The sandbox shows all
	// of it; the terrain shows a view of viewWidth × viewHeight.
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
	// ReplayPath plays back the replay file there instead of reading the
	// devices, and exits at its end. The replay sets the world size, the
	// seed, the tools and the mode; Width, Height, Seed and Creative are
	// ignored.
	ReplayPath string
	// RecordPath saves the session's inputs there as a replay on exit.
	RecordPath string
	// Creative places without taking from the inventory, for trying
	// materials out in development. A replay sets it instead.
	Creative bool
	// SavePath is the save folder of the terrain; empty for the default
	// in the user's config folder. A folder that holds a world goes on
	// with it. Replays, recordings and shots never touch it.
	SavePath string
	// FixedWorld is set when the command line chose the seed or the
	// world size, which a saved world refuses to change.
	FixedWorld bool
}

// Slots are the materials keys 1, 2, 3… select. 🧪 sandbox controls.
var slots = []string{"sand", "water", "stone"}

// tool is the player's dig and place tool. 🧪 It digs stone (hardness
// 10, hp 100000) in 50 ticks a cell and sand in one, with a brush of
// radius 4.
var tool = game.Tool{Power: 10, Rate: 2000, Radius: 4}

// stream keeps the blocks within 256 cells of the view and at most 12 in
// memory, as Noita does (🧪, design.md 7절).
var stream = game.Stream{Margin: 256, MaxBlocks: 12}

// The terrain's view in cells, the logical screen size (🧪).
const (
	viewWidth  = 480
	viewHeight = 270
)

// Run opens the window and plays until it closes, the replay ends or the
// last shot is taken.
func Run(cfg *Config) error {
	// Check settings before Ebitengine sees them: it panics on a
	// nonpositive window size, and these come from the command line.
	if cfg.Scale < 1 {
		return fmt.Errorf("app: window scale %d is not positive", cfg.Scale)
	}
	var playback *replay.Replay
	if cfg.ReplayPath != "" {
		var err error
		if playback, err = loadReplay(cfg.ReplayPath); err != nil {
			return err
		}
		if err := checkShots(cfg.Shots, len(playback.Inputs)); err != nil {
			return err
		}
	} else if cfg.Width < 1 || cfg.Height < 1 {
		return fmt.Errorf("app: world size %dx%d is not positive", cfg.Width, cfg.Height)
	}
	if cfg.RecordPath != "" {
		// Fail now rather than after the session, when the inputs would be lost.
		if err := prepareRecording(cfg.RecordPath); err != nil {
			return err
		}
	}
	mats, err := loadMaterials(cfg.MaterialsPath)
	if err != nil {
		return err
	}
	sess, saveDir, unlock, err := openSession(cfg, mats, playback)
	if err != nil {
		return err
	}
	// Deferred first, so it runs last: after the save is written.
	defer unlock()
	g, err := game.New(sess)
	if err != nil {
		return err
	}
	defer g.Close()

	a := &app{game: g, rend: render.New(mats), width: sess.ViewWidth, height: sess.ViewHeight}
	camX, camY := g.Spawn()
	// Until the first tick, draw what the session starts with.
	a.in = game.Input{CameraX: camX, CameraY: camY}
	a.camera = input.Camera{X: camX, Y: camY, MaxX: sess.Width - sess.ViewWidth, MaxY: sess.Height - sess.ViewHeight}
	if playback != nil {
		a.playback, a.replaying = playback.Inputs, true
	}
	if cfg.RecordPath != "" {
		a.record, a.recordLimit = replay.New(sess), replay.MaxTicks
	}
	if len(cfg.Shots) > 0 {
		a.shots = newShooter(cfg.Shots, cfg.ShotsDir, sess.ViewWidth, sess.ViewHeight)
	}
	ebiten.SetTPS(game.Hz)
	ebiten.SetWindowTitle("matter-wake")
	ebiten.SetWindowSize(sess.ViewWidth*cfg.Scale, sess.ViewHeight*cfg.Scale)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// RunGame returns nil when Update returns ebiten.Termination.
	runErr := ebiten.RunGame(a)
	if saveDir != "" {
		if err := writeSave(saveDir, g, a.in.CameraX, a.in.CameraY); err != nil {
			runErr = errors.Join(runErr, err)
		} else {
			slog.Info("saved", "save", saveDir, "tick", g.World().Tick())
		}
	}
	if a.record != nil {
		if err := saveReplay(cfg.RecordPath, a.record); err != nil {
			return errors.Join(runErr, err)
		}
		slog.Info("recorded", "path", cfg.RecordPath, "ticks", len(a.record.Inputs))
	}
	return runErr
}

// sessionConfig is the session to start: the replay's when there is one,
// else the one the command line asks for.
func sessionConfig(cfg *Config, mats *material.Table, playback *replay.Replay) *game.Config {
	if playback == nil {
		slog.Info("starting", "scene", cfg.Scene, "size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
			"seed", cfg.Seed, "workers", cfg.Workers, "creative", cfg.Creative)
		sess := &game.Config{
			Width: cfg.Width, Height: cfg.Height, Seed: cfg.Seed, Workers: cfg.Workers,
			Scene: cfg.Scene, ViewWidth: cfg.Width, ViewHeight: cfg.Height, Stream: stream,
			Materials: mats, Slots: slots, Tool: tool, Creative: cfg.Creative,
		}
		if cfg.Scene == game.SceneTerrain {
			sess.ViewWidth, sess.ViewHeight = min(viewWidth, cfg.Width), min(viewHeight, cfg.Height)
		}
		return sess
	}
	slog.Info("replaying", "path", cfg.ReplayPath, "seed", playback.Seed,
		"ticks", len(playback.Inputs), "workers", cfg.Workers)
	if now := mats.Fingerprint(); playback.Materials != now {
		slog.Warn("the replay was recorded with other material values; playback may diverge",
			"path", cfg.ReplayPath, "recorded", playback.Materials, "now", now, "materials", cfg.MaterialsPath)
	}
	return playback.Config(mats, cfg.Workers)
}

// openSession returns the session to start and the folder it saves in,
// if any: a saved world goes on from there. The folder stays locked to
// this process until unlock, which does nothing without a folder.
func openSession(cfg *Config, mats *material.Table, playback *replay.Replay) (
	sess *game.Config, dir string, unlock func(), err error,
) {
	sess = sessionConfig(cfg, mats, playback)
	if dir, err = saveFolder(cfg, playback); err != nil || dir == "" {
		return sess, "", func() {}, err
	}
	if unlock, err = lockSave(dir); err != nil {
		return nil, "", nil, err
	}
	if sess, err = openSave(dir, sess, cfg.FixedWorld); err != nil {
		unlock()
		return nil, "", nil, err
	}
	if sess.Resume != nil {
		slog.Info("resuming", "save", dir, "seed", sess.Seed, "tick", sess.Resume.Tick)
	} else {
		slog.Info("saving a new world", "save", dir)
	}
	return sess, dir, unlock, nil
}

// saveFolder returns the folder the session saves in: the terrain's, when
// the player plays, and none for replays, recordings and shots, which must
// start from a new world and never change a saved one.
func saveFolder(cfg *Config, playback *replay.Replay) (string, error) {
	if cfg.Scene != game.SceneTerrain || playback != nil || cfg.RecordPath != "" || len(cfg.Shots) > 0 {
		return "", nil
	}
	if cfg.SavePath != "" {
		return cfg.SavePath, nil
	}
	return defaultSave()
}

func loadMaterials(path string) (*material.Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	mats, err := material.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("app: %s: %w", path, err)
	}
	return mats, nil
}

type app struct {
	game  *game.Game
	rend  *render.Renderer
	shots *shooter
	// playback holds the inputs of the replay being played, if replaying.
	playback  []game.Input
	replaying bool
	// record collects the inputs to save, if recording, up to recordLimit
	// of them, the most a replay may hold.
	record      *replay.Replay
	recordLimit int
	recordFull  bool
	// in is the last tick's input; its camera is where the view is drawn.
	in game.Input
	// camera is where the devices move the view from.
	camera input.Camera
	// width and height are the view's size, the logical screen.
	width, height int
}

func (a *app) Update() error {
	if a.shots != nil {
		done, err := a.shots.capture(a.game, a.rend, a.in.CameraX, a.in.CameraY)
		if err != nil {
			return err
		}
		if done {
			return ebiten.Termination
		}
	}
	in, ok := a.nextInput()
	if !ok {
		return ebiten.Termination
	}
	if a.record != nil {
		a.recordInput(in)
	}
	a.in = in
	return a.game.Step(in)
}

// recordInput keeps in for the replay. At the limit it stops recording
// rather than make the whole replay too long to save.
func (a *app) recordInput(in game.Input) {
	if len(a.record.Inputs) < a.recordLimit {
		a.record.Inputs = append(a.record.Inputs, in)
		return
	}
	if !a.recordFull {
		a.recordFull = true
		slog.Warn("the recording is full; later inputs are not recorded", "ticks", a.recordLimit)
	}
}

// nextInput returns the input for the coming tick, or false when the
// replay has ended.
func (a *app) nextInput() (game.Input, bool) {
	switch {
	case a.replaying:
		tick := int(a.game.World().Tick())
		if tick >= len(a.playback) {
			return game.Input{}, false
		}
		return a.playback[tick], true
	case a.shots != nil:
		// Shots must be reproducible, so the devices are ignored and the
		// camera stays where the session started.
		return game.Input{CameraX: a.camera.X, CameraY: a.camera.Y}, true
	}
	in := input.Read(a.camera)
	a.camera.X, a.camera.Y = in.CameraX, in.CameraY
	return in, true
}

func (a *app) Draw(screen *ebiten.Image) {
	a.rend.DrawWorld(screen, a.game.World(), a.in.CameraX, a.in.CameraY)
	if a.shots == nil {
		a.rend.DrawOverlay(screen, a.game, a.in)
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
