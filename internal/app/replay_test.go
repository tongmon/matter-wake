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
	if err := prepareRecording(path); err != nil {
		t.Fatal(err)
	}
	// It creates the folder and leaves nothing in it.
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 0 {
		t.Fatalf("after prepareRecording the folder holds %v (%v), want nothing", entries, err)
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

// A -record path that cannot be saved must stop Run before the session, or
// the whole session's inputs would be lost on exit (Codex review).
func TestRunRejectsAnUnsavableRecordPath(t *testing.T) {
	dir := t.TempDir()
	err := Run(&Config{Scale: 2, Width: 64, Height: 32, RecordPath: dir})
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("Run error = %v, want one saying the record path is a directory", err)
	}
}

// A recording that outgrows what a replay may hold keeps its first
// replay.MaxTicks inputs, so it can still be saved (Codex review).
func TestRecordingStopsAtTheLimit(t *testing.T) {
	a := &app{record: testReplay(), recordLimit: 4}
	a.record.Inputs = nil
	for i := range 6 {
		a.recordInput(game.Input{CursorX: i})
	}
	if n := len(a.record.Inputs); n != 4 {
		t.Fatalf("recorded %d inputs, want the limit of 4", n)
	}
	if a.record.Inputs[3].CursorX != 3 {
		t.Fatalf("the last recorded input is %+v, want the fourth", a.record.Inputs[3])
	}
}

// An existing replay that cannot be replaced, such as a read-only file on
// Windows, must stop Run before the session too (the second Codex review).
func TestPrepareRecordingRejectsAReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only file")
	}
	path := filepath.Join(t.TempDir(), "a.rpl")
	if err := os.WriteFile(path, []byte("old\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if err := prepareRecording(path); err == nil {
		t.Fatal("prepareRecording accepted a read-only file")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old\n" {
		t.Fatalf("the check changed the file to %q (%v)", data, err)
	}
}

// A replay written in full is kept when it cannot take its place, so a
// failure at exit never loses the session's inputs (the second Codex review).
func TestSaveReplayKeepsTheRecordingWhenTheRenameFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.rpl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err := saveReplay(path, testReplay())
	if err == nil {
		t.Fatal("saveReplay replaced a directory")
	}
	kept, globErr := filepath.Glob(path + ".*.tmp")
	if globErr != nil || len(kept) != 1 {
		t.Fatalf("found %v (%v), want the one kept recording", kept, globErr)
	}
	if !strings.Contains(err.Error(), kept[0]) {
		t.Fatalf("error %q does not say where the recording is", err)
	}
	r, err := loadReplay(kept[0])
	if err != nil || r.Seed != testReplay().Seed {
		t.Fatalf("the kept recording loads as %+v (%v)", r, err)
	}
}
