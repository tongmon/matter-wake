// Package app runs a session in a window: one game.Step per Ebitengine
// Update, at game.Hz.
package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/input"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/render"
	"github.com/tongmon/matter-wake/internal/replay"
	"github.com/tongmon/matter-wake/internal/scene"
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
	// Timing measures how long ticks, draws and frames take and logs it
	// on exit.
	Timing bool
	// Dash names the parts of the test dashboard to start with,
	// comma-separated: panel and the views (render.ViewNames). With Shots
	// they are drawn into the shots.
	Dash string
}

// slotsOf returns the materials the brush places, which keys 1, 2, 3…
// and the dashboard select: every powder, liquid and static material of
// mats, kind by kind in that order (🔶), each kind in the table's order.
// With the shipped table they are sand, water and stone.
func slotsOf(mats *material.Table) []string {
	var names []string
	for _, kind := range [...]material.Kind{material.Powder, material.Liquid, material.Static} {
		for id := range mats.Len() {
			if d := mats.Def(material.ID(id)); d.Kind == kind {
				names = append(names, d.Name)
			}
		}
	}
	return names
}

// tool is the player's dig and place tool. 🧪 It digs stone (hardness
// 10, hp 100000) in 50 ticks a cell and sand in one, with a brush of
// radius 4.
var tool = game.Tool{Power: 10, Rate: 2000, Radius: 4}

// body is what the body tool drops in the rooms: wooden boxes of 16 × 12
// pixels, 1.6 × 1.2 m (🧪, a development tool, design.md 2절).
var body = game.BodyTool{Material: "wood", Width: 16, Height: 12}

// stream requests and unloads blocks with Noita's numbers: a 64-cell
// grid, the area ahead four times as far as the camera's area moved, at
// most 512 cells, a target of 12 blocks, and a check every second at 60
// ticks a second (🧪, design.md 7절).
var stream = game.Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 12, Check: 60}

// budget is how many cells a tick may scan before the chunks farthest from
// the middle of the view wait (🧪, design.md 3절). It keeps the load scene
// within the cell budget on the reference CPU (design.md 2절).
const budget = 250_000

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
	playback, err := loadPlayback(cfg)
	if err != nil {
		return err
	}
	if cfg.RecordPath != "" {
		// Fail now rather than after the session, when the inputs would be lost.
		if err := prepareRecording(cfg.RecordPath); err != nil {
			return err
		}
	}
	open, views, err := parseDash(cfg.Dash)
	if err != nil {
		return err
	}
	var tm *timing
	if cfg.Timing {
		var err error
		if tm, err = newTiming(); err != nil {
			return err
		}
	}
	mats, err := loadMaterials(cfg.MaterialsPath)
	if err != nil {
		return err
	}
	sess, sv, err := openSession(cfg, mats, playback)
	if err != nil {
		return err
	}
	g, err := game.New(sess)
	if err != nil {
		return errors.Join(err, sv.close())
	}
	defer g.Close()

	a := newApp(cfg, g, sess, mats, playback)
	a.dash.open, a.dash.views = open, views
	a.timing = tm
	if sv != nil {
		a.prefetch = sv.store
	}
	ebiten.SetTPS(game.Hz)
	ebiten.SetWindowTitle("matter-wake")
	ebiten.SetWindowSize(sess.ViewWidth*cfg.Scale, sess.ViewHeight*cfg.Scale)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// RunGame returns nil when Update returns ebiten.Termination.
	runErr := ebiten.RunGame(a)
	if a.timing != nil {
		a.timing.report()
	}
	if sv != nil {
		err := writeSave(sv.dir, g, a.in.CameraX, a.in.CameraY, sv.store.Flush)
		if err == nil {
			slog.Info("saved", "save", sv.dir, "tick", g.World().Tick())
		}
		runErr = errors.Join(runErr, err, sv.close())
	}
	if a.record != nil {
		if err := saveReplay(cfg.RecordPath, a.record); err != nil {
			return errors.Join(runErr, err)
		}
		slog.Info("recorded", "path", cfg.RecordPath, "ticks", len(a.record.Inputs))
	}
	return runErr
}

// loadPlayback loads the replay cfg plays, nil for none, and checks that
// it has the ticks cfg shoots; without one, it checks the world size.
func loadPlayback(cfg *Config) (*replay.Replay, error) {
	if cfg.ReplayPath == "" {
		if cfg.Width < 1 || cfg.Height < 1 {
			return nil, fmt.Errorf("app: world size %dx%d is not positive", cfg.Width, cfg.Height)
		}
		return nil, nil
	}
	playback, err := loadReplay(cfg.ReplayPath)
	if err != nil {
		return nil, err
	}
	if err := checkShots(cfg.Shots, len(playback.Inputs)); err != nil {
		return nil, err
	}
	return playback, nil
}

// newApp returns the app that runs session g, started from sess, as cfg
// asks: playing the replay if there is one, recording and taking shots.
func newApp(cfg *Config, g *game.Game, sess *game.Config, mats *material.Table, playback *replay.Replay) *app {
	a := &app{
		game: g, rend: render.New(mats), width: sess.ViewWidth, height: sess.ViewHeight,
		dash: newDash(playback == nil), live: len(cfg.Shots) == 0,
	}
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
	return a
}

// sessionConfig is the session to start: the replay's when there is one,
// else the one the command line asks for.
func sessionConfig(cfg *Config, mats *material.Table, playback *replay.Replay) *game.Config {
	if playback == nil {
		slog.Info("starting", "scene", cfg.Scene, "size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
			"seed", cfg.Seed, "workers", cfg.Workers, "creative", cfg.Creative)
		sess := &game.Config{
			Width: cfg.Width, Height: cfg.Height, Seed: cfg.Seed, Workers: cfg.Workers,
			Scene: cfg.Scene, ViewWidth: cfg.Width, ViewHeight: cfg.Height, Stream: stream, Budget: budget,
			Materials: mats, Slots: slotsOf(mats), Tool: tool, Body: body, Creative: cfg.Creative,
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

// saving is where a session saves: its folder, locked to this process
// until close, and the store that reads and writes the folder's blocks in
// the background.
type saving struct {
	dir    string
	store  *prefetchStore
	unlock func()
}

// close stops the store, writing what is left, and unlocks the folder. A
// nil saving has nothing to close.
func (s *saving) close() error {
	if s == nil {
		return nil
	}
	err := s.store.Close()
	s.unlock()
	return err
}

// openSession returns the session to start and where it saves, nil for a
// session that does not: a saved world goes on from there.
func openSession(cfg *Config, mats *material.Table, playback *replay.Replay) (*game.Config, *saving, error) {
	sess := sessionConfig(cfg, mats, playback)
	dir, err := saveFolder(cfg, playback)
	if err != nil || dir == "" {
		return sess, nil, err
	}
	unlock, err := lockSave(dir)
	if err != nil {
		return nil, nil, err
	}
	if sess, err = openSave(dir, sess, cfg.FixedWorld); err != nil {
		unlock()
		return nil, nil, err
	}
	terrain, err := scene.NewTerrain(sess.Seed, sess.Width, sess.Height, mats)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	disk, ok := sess.Store.(*diskStore)
	if !ok {
		unlock()
		return nil, nil, errors.New("app: the save folder has no disk store")
	}
	sv := &saving{dir: dir, store: newPrefetchStore(disk, terrain), unlock: unlock}
	sess.Store = sv.store
	if sess.Resume != nil {
		slog.Info("resuming", "save", dir, "seed", sess.Seed, "tick", sess.Resume.Tick)
	} else {
		slog.Info("saving a new world", "save", dir)
	}
	return sess, sv, nil
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
	// prefetch gets ready the blocks the camera will need next, in a
	// session that saves on disk.
	prefetch *prefetchStore
	// timing, if set, measures the ticks and draws.
	timing *timing
	// dash is the test dashboard. live is set when the devices drive it,
	// which they do unless shots are taken; ui is what they did in the
	// last update.
	dash *dash
	live bool
	ui   input.UI
}

func (a *app) Update() error {
	if a.shots != nil {
		done, err := a.shots.capture(a.game.World().Tick(), a.drawShot)
		if err != nil {
			return err
		}
		if done {
			return ebiten.Termination
		}
	}
	if a.live {
		a.ui = input.ReadUI()
		a.dash.handle(&a.ui, a.game)
		if a.dash.open {
			a.dash.build(a.game, a.width)
		}
	}
	if !a.dash.ticks() {
		if a.live && !a.replaying {
			// Keep what was pressed for the tick that comes.
			in := input.Read(a.camera)
			a.dash.latch(&in)
		}
		return nil
	}
	in, ok := a.nextInput()
	if !ok {
		return ebiten.Termination
	}
	if a.record != nil {
		a.recordInput(&in)
	}
	a.in = in
	var start time.Duration
	if a.timing != nil {
		start = a.timing.clock.now()
	}
	if err := a.game.Step(&in); err != nil {
		return err
	}
	// Getting blocks ready is cheap to ask for but needs no asking every
	// tick: the camera crosses the extra block in no fewer than 32 ticks.
	if a.prefetch != nil && a.game.World().Tick()%4 == 0 {
		a.prefetch.Prefetch(a.game.Upcoming(in.CameraX, in.CameraY, prefetchMargin))
	}
	if a.timing != nil {
		a.timing.addTick(a.timing.clock.since(start))
	}
	return nil
}

// recordInput keeps in for the replay. At the limit it stops recording
// rather than make the whole replay too long to save.
func (a *app) recordInput(in *game.Input) {
	if len(a.record.Inputs) < a.recordLimit {
		a.record.Inputs = append(a.record.Inputs, *in)
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
	if a.dash.over {
		// The panel takes the mouse from the world.
		in.Place, in.Dig = false, false
	}
	a.dash.take(&in, a.game)
	a.camera.X, a.camera.Y = in.CameraX, in.CameraY
	return in, true
}

func (a *app) Draw(screen *ebiten.Image) {
	var start time.Duration
	if a.timing != nil {
		start = a.timing.clock.now()
	}
	a.rend.DrawWorld(screen, a.game.World(), a.in.CameraX, a.in.CameraY)
	if a.shots == nil {
		a.drawDash(screen)
	}
	if a.timing != nil {
		a.timing.addDraw(a.timing.clock.since(start))
	}
}

// drawDash draws, over the world, the views that are on, the status line
// with the brush at the cursor, the panel if it is open, and the cell
// under the cursor if that view is on. The cursor is the mouse, which
// moves while the ticks are paused, or in a replay, which ignores the
// mouse for the session, the replay's.
func (a *app) drawDash(screen *ebiten.Image) {
	camX, camY := a.in.CameraX, a.in.CameraY
	cx, cy := camX+a.ui.X, camY+a.ui.Y
	if a.replaying {
		cx, cy = a.in.CursorX, a.in.CursorY
	}
	if a.dash.views != 0 {
		a.rend.DrawViews(screen, a.game, a.dash.views, camX, camY)
	}
	r, shape := a.dash.brush(a.game)
	st := &render.Status{
		CursorX: cx, CursorY: cy, CameraX: camX, CameraY: camY, Radius: r, Shape: shape,
		Selected: a.game.Materials().Def(a.game.Slots()[a.dash.slot(a.game)-1]).Name, Time: a.dash.timeName(),
	}
	if a.dash.open {
		st.Cols = (a.width - a.dash.panel.Width()) / render.GlyphWidth
	}
	if a.dash.over {
		st.Radius = -1 // the mouse is on the panel, not the world
	}
	a.rend.DrawOverlay(screen, a.game, st)
	if a.dash.open {
		a.rend.DrawPanel(screen, &a.dash.panel, a.ui.X, a.ui.Y)
	}
	if a.dash.views&render.ViewCell != 0 && !a.dash.over {
		a.rend.DrawCell(screen, a.game, cx, cy, cx-camX, cy-camY)
	}
}

// drawShot draws a shot: the world and the parts of the dashboard -dash
// names, the cell view at the input's cursor. It draws no status line,
// whose rates would make shots differ from run to run.
func (a *app) drawShot(dst *ebiten.Image) {
	camX, camY := a.in.CameraX, a.in.CameraY
	a.rend.DrawWorld(dst, a.game.World(), camX, camY)
	if a.dash.views != 0 {
		a.rend.DrawViews(dst, a.game, a.dash.views, camX, camY)
	}
	if a.dash.open {
		a.dash.build(a.game, a.width)
		a.rend.DrawPanel(dst, &a.dash.panel, -1, -1)
	}
	if a.dash.views&render.ViewCell != 0 {
		a.rend.DrawCell(dst, a.game, a.in.CursorX, a.in.CursorY, a.in.CursorX-camX, a.in.CursorY-camY)
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
