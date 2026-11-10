package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/replay"
	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

func testMaterials(t *testing.T) *material.Table {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "replay", "testdata", "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	mats, err := material.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return mats
}

func TestDiskStoreRoundTrip(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, blocksDir), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &diskStore{dir: dir, mats: mats}
	if b, err := s.Load(3, 4); b != nil || err != nil {
		t.Fatalf("Load of a block never saved = %v, %v; want nil, nil", b, err)
	}
	b := new(world.Block)
	b[100] = world.Cell{Mat: mats.MustLookup("sand"), Tone: 9, Falling: true, Last: 77, Damage: 5}
	if err := s.Save(3, 4, &save.Block{Cells: b}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if *got.Cells != *b {
		t.Fatal("the block came back different")
	}
	if err := os.WriteFile(s.path(5, 5), []byte("not a block"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(5, 5); err == nil || !strings.Contains(err.Error(), "5_5.mwb") {
		t.Fatalf("Load of a damaged file: error %v, want one naming the file", err)
	}
}

// terrainConfig is a terrain session with the stream and the budget the
// game uses.
func terrainConfig(mats *material.Table) *game.Config {
	return &game.Config{
		Width: 4096, Height: 4096, Seed: 4, Workers: 2, Materials: mats,
		Scene: game.SceneTerrain, ViewWidth: 480, ViewHeight: 270, Stream: stream, Budget: budget,
		Slots: []string{"sand", "water", "stone"}, Tool: game.Tool{Power: 10, Rate: 2000, Radius: 4},
		Body: game.BodyTool{Material: "wood", Width: 16, Height: 12},
	}
}

// A session saved and opened again goes on exactly as the session that
// was saved: the world, the tick, the inventory, the slot, the camera, the
// blocks the stream kept in memory past the view, and where the stream's
// requests and checks stood. The camera moves before the save, so blocks
// it left behind are still in memory and running. Keeping them was found
// missing by the Codex review of the save folder (2026-11-06). The second
// stream checks every 7 ticks and keeps 4 blocks, so checks unload blocks
// on both sides of the save, and the save lands between two checks. The
// player drops bodies as the camera goes, which leave with the blocks and
// are saved with them; the bodies must go on as they would have too, to
// the bit (design.md 8절).
func TestSavedSessionGoesOnTheSame(t *testing.T) {
	for _, s := range []game.Stream{stream, {Grid: 64, Lead: 4, LeadMax: 512, Target: 4, Check: 7}} {
		t.Run(fmt.Sprintf("target %d check %d", s.Target, s.Check), func(t *testing.T) {
			checkSavedSessionGoesOn(t, s)
		})
	}
}

func checkSavedSessionGoesOn(t *testing.T, s game.Stream) {
	mats := testMaterials(t)
	dir := t.TempDir()
	cfg := terrainConfig(mats)
	cfg.Stream = s
	sess, err := openSave(dir, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Resume != nil {
		t.Fatal("an empty folder resumed a world")
	}
	played, err := game.New(sess)
	if err != nil {
		t.Fatal(err)
	}
	defer played.Close()
	camX, camY := played.Spawn()
	// The input of tick i: the camera goes right, the tool digs, and every
	// fifth tick a body drops just above the ground, so bodies lie on the
	// ground and on each other at the save, where what Box2D carries from
	// tick to tick matters.
	input := func(i int) game.Input {
		x := camX + 16*i
		in := game.Input{CameraX: x, CameraY: camY, CursorX: x + 240, CursorY: camY + 160, Dig: i%2 == 0, Select: 1 + i%3}
		if i%5 != 1 {
			return in
		}
		dx := x + 200 + (i/5%3)*6
		for y := camY; y < camY+270; y++ {
			if c, ok := played.World().At(dx, y); ok && c.Mat != 0 {
				in.Drop, in.CursorX, in.CursorY = true, dx, y-16
				break
			}
		}
		return in
	}
	for i := range 50 {
		in := input(i)
		if err := played.Step(&in); err != nil {
			t.Fatal(err)
		}
	}
	last := input(49)
	if played.Bodies() == 0 || played.Dropped() == 0 {
		t.Fatalf("%d bodies in memory of %d dropped; the test needs bodies at the save", played.Bodies(), played.Dropped())
	}
	// Blocks the view left behind, and more than Target, so checks after
	// the save have blocks to unload.
	if n := len(played.ResidentBlocks()); n <= min(6, s.Target) {
		t.Fatalf("only %d blocks are in memory at the save; the test needs some the view left behind", n)
	}
	if err := writeSave(dir, played, last.CameraX, last.CameraY, nil); err != nil {
		t.Fatal(err)
	}
	// The session that was saved goes on in its folder; a copy of the save
	// is opened again elsewhere.
	copied := t.TempDir()
	copyDir(t, dir, copied)
	again, err := openSave(copied, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Resume == nil || again.Resume.Tick != played.World().Tick() {
		t.Fatalf("the save resumed %+v, want tick %d", again.Resume, played.World().Tick())
	}
	resumed, err := game.New(again)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if x, y := resumed.Spawn(); x != last.CameraX || y != last.CameraY {
		t.Fatalf("the resumed camera is at (%d, %d), want (%d, %d)", x, y, last.CameraX, last.CameraY)
	}
	// The camera went more than rigid.SettleMove right: far bodies of ice
	// and glass were checked there, and the next check counts from there.
	sx, sy := played.SettlePoint()
	if x, y := resumed.SettlePoint(); x != sx || y != sy || sx == camX+240 {
		t.Fatalf("the resumed session last settled at (%d, %d), the saved one at (%d, %d), from (%d, %d)",
			x, y, sx, sy, camX+240, camY+135)
	}
	for i := 50; i < 100; i++ {
		in := input(i)
		for _, g := range []*game.Game{played, resumed} {
			if err := g.Step(&in); err != nil {
				t.Fatal(err)
			}
		}
		if a, b := played.World().Checksum(), resumed.World().Checksum(); a != b {
			t.Fatalf("tick %d: the resumed session's checksum %#x, the saved one's %#x", played.World().Tick(), b, a)
		}
		if a, b := played.BodyRecords(nil), resumed.BodyRecords(nil); !reflect.DeepEqual(a, b) {
			t.Fatalf("tick %d: the resumed session's bodies\n%+v\ndiffer from the saved one's\n%+v", played.World().Tick(), b, a)
		}
	}
	if played.Bodies() == 0 {
		t.Fatal("no body is in memory at the end; the test needs some to go on with")
	}
	for id := range mats.Len() {
		if a, b := played.Inventory(material.ID(id)), resumed.Inventory(material.ID(id)); a != b {
			t.Fatalf("the resumed session holds %d %s, the saved one %d", b, mats.Def(material.ID(id)).Name, a)
		}
	}
	if played.Slot() != resumed.Slot() {
		t.Fatalf("the resumed session selects slot %d, the saved one %d", resumed.Slot(), played.Slot())
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A saved world sets its own seed and size; blocks without a world are
// someone else's.
func TestOpenSaveRefusesToMixWorlds(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	if _, err := openSave(dir, terrainConfig(mats), true); err != nil {
		t.Fatalf("a new world with chosen seed and size: %v", err)
	}
	if _, err := openSave(dir, terrainConfig(mats), true); err == nil || !strings.Contains(err.Error(), "holds a world") {
		t.Fatalf("opening a saved world with -seed: error %v", err)
	}
	if err := os.Remove(filepath.Join(dir, worldFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, blocksDir, "0_0.mwb"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openSave(dir, terrainConfig(mats), false); err == nil || !strings.Contains(err.Error(), "no world.txt") {
		t.Fatalf("blocks without a world header: error %v", err)
	}
}

// Only the player's terrain sessions touch the disk; replays, recordings
// and shots must start from a new world.
func TestSaveFolderIsOnlyForPlay(t *testing.T) {
	terrain := Config{Scene: game.SceneTerrain, SavePath: "x"}
	if dir, err := saveFolder(&terrain, nil); dir != "x" || err != nil {
		t.Fatalf("saveFolder for play = %q, %v", dir, err)
	}
	tests := []struct {
		name     string
		cfg      Config
		playback *replay.Replay
	}{
		{"sandbox", Config{SavePath: "x"}, nil},
		{"replay", terrain, &replay.Replay{}},
		{"record", Config{Scene: game.SceneTerrain, SavePath: "x", RecordPath: "a.rpl"}, nil},
		{"shots", Config{Scene: game.SceneTerrain, SavePath: "x", Shots: []uint32{1}}, nil},
	}
	for _, tt := range tests {
		if dir, err := saveFolder(&tt.cfg, tt.playback); dir != "" || err != nil {
			t.Errorf("%s: saveFolder = %q, %v; want none", tt.name, dir, err)
		}
	}
}

// A saved world smaller than the view reopens with the view fitted to it,
// though the command line's size was the default. Found by the Codex
// review of the save folder (2026-11-06).
func TestReopenedSmallWorldFitsTheView(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	small := terrainConfig(mats)
	small.Width, small.Height, small.ViewWidth, small.ViewHeight = 300, 200, 300, 200
	if _, err := openSave(dir, small, true); err != nil {
		t.Fatal(err)
	}
	sess, err := openSave(dir, terrainConfig(mats), false)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Width != 300 || sess.Height != 200 || sess.ViewWidth != 300 || sess.ViewHeight != 200 {
		t.Fatalf("reopened a %dx%d world with a %dx%d view, want 300x200 both",
			sess.Width, sess.Height, sess.ViewWidth, sess.ViewHeight)
	}
}

// A save that could not be written on exit is refused before play starts,
// or the session would be lost or saved half (pitfalls.md). Found by the
// Codex review of the save folder (2026-11-06).
func TestOpenSaveRefusesReadOnlyFiles(t *testing.T) {
	mats := testMaterials(t)
	files := map[string]string{
		sessionFile:                         "matter-wake-session 1\ncamera 0 0\nslot 1\n",
		filepath.Join(blocksDir, "3_2.mwb"): "x",
	}
	for _, name := range []string{worldFile, sessionFile, filepath.Join(blocksDir, "3_2.mwb")} {
		t.Run(filepath.Base(name), func(t *testing.T) {
			dir := t.TempDir()
			if _, err := openSave(dir, terrainConfig(mats), false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, name)
			if text, ok := files[name]; ok {
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := openSave(dir, terrainConfig(mats), false); err != nil {
				t.Fatalf("the save does not open before %s is read-only: %v", name, err)
			}
			if err := os.Chmod(path, 0o444); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
			_, err := openSave(dir, terrainConfig(mats), false)
			if err == nil || !strings.Contains(err.Error(), filepath.Base(name)) {
				t.Fatalf("opening a save with %s read-only: error %v, want one naming it", name, err)
			}
		})
	}
}

// Only one session at a time may have a save folder open, or the one that
// closes last would undo what the other saved. The operating system holds
// the lock and drops it when the process ends, crash or not, so a lock
// file left behind does not lock. Found by two Codex reviews of the save
// folder (2026-11-06).
func TestSaveFolderIsLocked(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockSave(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockSave(dir); err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("a second lock on the folder: error %v, want one saying it is open", err)
	}
	unlock()
	again, err := lockSave(dir)
	if err != nil {
		t.Fatalf("locking again after unlocking: %v", err)
	}
	again()
	// A lock file left by a game that crashed holds nothing.
	if err := os.WriteFile(filepath.Join(dir, lockFile), []byte("left behind"), 0o644); err != nil {
		t.Fatal(err)
	}
	left, err := lockSave(dir)
	if err != nil {
		t.Fatalf("locking a folder with a lock file left behind: %v", err)
	}
	left()
}

// Of games that start at the same moment, exactly one gets the save.
func TestSaveFolderLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	const tries = 16
	for round := range 200 {
		got := make(chan func(), tries)
		start := make(chan struct{})
		done := make(chan struct{}, tries)
		for range tries {
			go func() {
				<-start
				if unlock, err := lockSave(dir); err == nil {
					got <- unlock
				}
				done <- struct{}{}
			}()
		}
		close(start)
		for range tries {
			<-done
		}
		if n := len(got); n != 1 {
			t.Fatalf("round %d: %d of %d games that started together got the lock, want 1", round, n, tries)
		}
		(<-got)()
	}
}

// A session saved while chunks wait for the budget goes on the same when
// opened again: which chunks run depends on what each has left to scan
// and how long it has waited, so the save keeps both (Codex review).
func TestSavedSessionGoesOnOverTheBudget(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	cfg := terrainConfig(mats)
	cfg.Seed = 1
	sess, err := openSave(dir, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	played, err := game.New(sess)
	if err != nil {
		t.Fatal(err)
	}
	defer played.Close()
	x, y := played.Spawn()
	step := func(g *game.Game, in game.Input) {
		t.Helper()
		if err := g.Step(&in); err != nil {
			t.Fatal(err)
		}
	}
	for range 60 {
		step(played, game.Input{CameraX: x, CameraY: y})
	}
	// Into the caves: the blocks there come in at once, over the budget.
	caves := game.Input{CameraX: 1808, CameraY: 2600}
	for range 10 {
		step(played, caves)
	}
	if played.World().Waiting() == 0 {
		t.Fatal("no chunk waits at the save: the test needs a session over its budget")
	}
	if err := writeSave(dir, played, caves.CameraX, caves.CameraY, nil); err != nil {
		t.Fatal(err)
	}
	copied := t.TempDir()
	copyDir(t, dir, copied)
	again, err := openSave(copied, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := game.New(again)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	for range 120 {
		for _, g := range []*game.Game{played, resumed} {
			step(g, caves)
		}
		if a, b := played.World().Checksum(), resumed.World().Checksum(); a != b {
			t.Fatalf("tick %d: the resumed session's checksum %#x, the saved one's %#x", played.World().Tick(), b, a)
		}
		if a, b := played.BodyRecords(nil), resumed.BodyRecords(nil); !reflect.DeepEqual(a, b) {
			t.Fatalf("tick %d: the resumed session's bodies\n%+v\ndiffer from the saved one's\n%+v", played.World().Tick(), b, a)
		}
	}
}
