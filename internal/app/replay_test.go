package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/replay"
)

func testReplay() *replay.Replay {
	return &replay.Replay{
		Seed: 3, Width: 64, Height: 32, BrushRadius: 4, Slots: []string{"sand"}, Materials: "00",
		Inputs: []game.Input{{}, {CursorX: 5, CursorY: 6, Place: true}, {Select: 1}},
	}
}

func TestSaveAndLoadReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replays", "a.rpl")
	if err := makeParentDir(path); err != nil {
		t.Fatal(err)
	}
	want := testReplay()
	if err := saveReplay(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadReplay(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Seed != want.Seed || len(got.Inputs) != len(want.Inputs) || got.Inputs[1] != want.Inputs[1] {
		t.Fatalf("loaded %+v, want %+v", got, want)
	}
}

// A failed save must keep the replay already there and leave no
// temporary file behind.
func TestSaveReplayKeepsOldFileOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.rpl")
	if err := saveReplay(path, testReplay()); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bad := testReplay()
	bad.Slots = nil
	if err := saveReplay(path, bad); err == nil {
		t.Fatal("saveReplay accepted a replay without slots")
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(now, old) {
		t.Fatal("a failed save changed the old replay")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the folder holds %d files after a failed save, want 1", len(entries))
	}
}

func TestLoadReplayNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.rpl")
	if err := os.WriteFile(path, []byte("not a replay\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadReplay(path)
	if err == nil || !strings.Contains(err.Error(), "bad.rpl") {
		t.Fatalf("loadReplay error = %v, want one naming the file", err)
	}
}

func TestCheckShots(t *testing.T) {
	tests := []struct {
		shots []uint32
		ok    bool
	}{
		{nil, true},
		{[]uint32{0, 5, 10}, true},
		{[]uint32{10, 2}, true},
		{[]uint32{3, 11}, false},
	}
	for _, tt := range tests {
		if err := checkShots(tt.shots, 10); (err == nil) != tt.ok {
			t.Errorf("checkShots(%v, 10) = %v, want ok %v", tt.shots, err, tt.ok)
		}
	}
}

// A replay brings its own world size and seed, and its problems must stop
// Run before Ebitengine starts.
func TestRunRejectsBadReplays(t *testing.T) {
	short := filepath.Join(t.TempDir(), "short.rpl")
	if err := saveReplay(short, testReplay()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"missing file", Config{Scale: 2, ReplayPath: filepath.Join(t.TempDir(), "none.rpl")}, "none.rpl"},
		{"shot past the end", Config{Scale: 2, ReplayPath: short, Shots: []uint32{1, 4}}, "past"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Run(&tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}
