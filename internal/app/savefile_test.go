package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/replay"
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
	if err := s.Save(3, 4, b); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *b {
		t.Fatal("the block came back different")
	}
	if err := os.WriteFile(s.path(5, 5), []byte("not a block"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(5, 5); err == nil || !strings.Contains(err.Error(), "5_5.mwb") {
		t.Fatalf("Load of a damaged file: error %v, want one naming the file", err)
	}
}

// terrainConfig is a terrain session with the stream the game uses.
func terrainConfig(mats *material.Table) *game.Config {
	return &game.Config{
		Width: 4096, Height: 4096, Seed: 4, Workers: 2, Materials: mats,
		Scene: game.SceneTerrain, ViewWidth: 480, ViewHeight: 270, Stream: stream,
		Slots: []string{"sand", "water", "stone"}, Tool: game.Tool{Power: 10, Rate: 2000, Radius: 4},
	}
}

// A session saved and opened again goes on exactly as the session that
// was saved: the world, the tick, the inventory, the slot, the camera and
// the blocks the stream kept in memory past the view. The camera moves
// before the save, so blocks it left behind are still in memory and
// running. Keeping them was found missing by the Codex review of the save
// folder (2026-11-06).
func TestSavedSessionGoesOnTheSame(t *testing.T) {
	mats := testMaterials(t)
	dir := t.TempDir()
	sess, err := openSave(dir, terrainConfig(mats), false)
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
	inputs := func(from, to int) []game.Input {
		var in []game.Input
		for i := from; i < to; i++ {
			x := camX + 16*i
			in = append(in, game.Input{CameraX: x, CameraY: camY, CursorX: x + 240, CursorY: camY + 160, Dig: i%2 == 0, Select: 1 + i%3})
		}
		return in
	}
	for _, in := range inputs(0, 50) {
		if err := played.Step(in); err != nil {
			t.Fatal(err)
		}
	}
	last := inputs(49, 50)[0]
	if n := len(played.ResidentBlocks()); n <= 6 {
		t.Fatalf("only %d blocks are in memory at the save; the test needs some the view left behind", n)
	}
	if err := writeSave(dir, played, last.CameraX, last.CameraY); err != nil {
		t.Fatal(err)
	}
	// The session that was saved goes on in its folder; a copy of the save
	// is opened again elsewhere.
	copied := t.TempDir()
	copyDir(t, dir, copied)
	again, err := openSave(copied, terrainConfig(mats), false)
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
	for _, in := range inputs(50, 100) {
		for _, g := range []*game.Game{played, resumed} {
			if err := g.Step(in); err != nil {
				t.Fatal(err)
			}
		}
		if a, b := played.World().Checksum(), resumed.World().Checksum(); a != b {
			t.Fatalf("tick %d: the resumed session's checksum %#x, the saved one's %#x", played.World().Tick(), b, a)
		}
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
