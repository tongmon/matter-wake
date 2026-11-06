package replay

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
)

var (
	none = game.Input{}
	put  = game.Input{CursorX: 10, CursorY: 5, Place: true}
)

func sample() *Replay {
	return &Replay{
		Seed: 7, Width: 64, Height: 32, Tool: game.Tool{Power: 10, Rate: 2000, Radius: 2},
		Slots: []string{"sand", "water"}, Materials: "0123456789abcdef",
	}
}

func TestWrite(t *testing.T) {
	r := sample()
	r.Inputs = []game.Input{
		none, none, put, put,
		{CursorX: 11, CursorY: 5, Place: true, Select: 2},
		{CursorX: 11, CursorY: 5, Dig: true},
		{CursorX: -3, CursorY: 40, Place: true, Dig: true},
		none,
	}
	var buf bytes.Buffer
	if err := Write(&buf, r); err != nil {
		t.Fatal(err)
	}
	want := `matter-wake-replay 2
seed 7
size 64 32
tool 10 2000 2
slots sand water
materials 0123456789abcdef
ticks 8
2 10 5 P 0
4 11 5 P 2
5 11 5 D 0
6 -3 40 PD 0
7 0 0 - 0
`
	if got := buf.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	s := rng.New(3)
	long := make([]game.Input, 2000)
	for i := range long {
		if i > 0 && s.IntN(4) != 0 {
			long[i] = long[i-1] // inputs mostly hold from tick to tick
			continue
		}
		long[i] = game.Input{
			CursorX: s.IntN(600) - 60, CursorY: s.IntN(400) - 60,
			Place: s.Bool(), Dig: s.IntN(4) == 0, Select: s.IntN(12),
		}
	}
	tests := [][]game.Input{
		nil,
		{none},
		{put},
		{put, none, put},
		long,
	}
	for i, inputs := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r := sample()
			r.Inputs = inputs
			var buf bytes.Buffer
			if err := Write(&buf, r); err != nil {
				t.Fatal(err)
			}
			got, err := Read(&buf)
			if err != nil {
				t.Fatal(err)
			}
			checkHeader(t, got, r)
			if len(got.Inputs) != len(inputs) {
				t.Fatalf("read %d inputs, want %d", len(got.Inputs), len(inputs))
			}
			for j := range inputs {
				if got.Inputs[j] != inputs[j] {
					t.Fatalf("input %d = %+v, want %+v", j, got.Inputs[j], inputs[j])
				}
			}
		})
	}
}

func checkHeader(t *testing.T, got, want *Replay) {
	t.Helper()
	if got.Seed != want.Seed || got.Width != want.Width || got.Height != want.Height ||
		got.Tool != want.Tool || got.Materials != want.Materials ||
		strings.Join(got.Slots, ",") != strings.Join(want.Slots, ",") {
		t.Fatalf("header = %+v, want %+v", got, want)
	}
}

// Replays are text so they can be written by hand: blank lines, comments,
// extra spaces and Windows line ends are all accepted.
func TestReadHandWritten(t *testing.T) {
	src := "matter-wake-replay 2\r\n" +
		"# A hand-written replay.\r\n" +
		"\r\n" +
		"seed 18446744073709551615\r\n" +
		"size  64   32 \r\n" +
		"tool  0 1  0\r\n" +
		"slots stone\r\n" +
		"materials unknown\r\n" +
		"ticks 6\r\n" +
		"  1 10 5 P 0\r\n" +
		"# hold, then stop\r\n" +
		"4 0 0 - 1\r\n" +
		"5 0 0 - 0"
	r, err := Read(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	checkHeader(t, r, &Replay{
		Seed: 1<<64 - 1, Width: 64, Height: 32, Tool: game.Tool{Rate: 1}, Slots: []string{"stone"}, Materials: "unknown",
	})
	want := []game.Input{none, put, put, put, {Select: 1}, none}
	if len(r.Inputs) != len(want) {
		t.Fatalf("read %d inputs, want %d", len(r.Inputs), len(want))
	}
	for i := range want {
		if r.Inputs[i] != want[i] {
			t.Fatalf("input %d = %+v, want %+v", i, r.Inputs[i], want[i])
		}
	}
}

func TestReadErrors(t *testing.T) {
	const header = "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 10 2000 4\nslots sand\nmaterials 00\n"
	tests := []struct {
		name, src, wantErr string
	}{
		{"empty", "", "not a matter-wake replay"},
		{"other file", "uplunge-replay 1\n", "not a matter-wake replay"},
		{"newer version", "matter-wake-replay 3\n", "version 3"},
		{"older version", "matter-wake-replay 1\nseed 1\n", "version 1"},
		{"bad version", "matter-wake-replay one\n", "version"},
		{"missing seed", "matter-wake-replay 2\nsize 64 32\n", `"seed`},
		{"negative seed", "matter-wake-replay 2\nseed -1\n", "seed"},
		{"one size", "matter-wake-replay 2\nseed 1\nsize 64\n", "size"},
		{"zero width", "matter-wake-replay 2\nseed 1\nsize 0 32\n", "size"},
		{"old brush line", "matter-wake-replay 2\nseed 1\nsize 64 32\nbrush 4\n", `"tool`},
		{"short tool", "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 10 2000\n", `"tool`},
		{"negative radius", "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 10 2000 -1\n", "tool"},
		{"power over 255", "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 256 2000 4\n", "tool"},
		{"zero rate", "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 10 0 4\n", "tool"},
		{"no slots", "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 10 2000 4\nslots\n", "slots"},
		{"no materials", "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 10 2000 4\nslots sand\nmaterials\n", "materials"},
		{"two fingerprints", "matter-wake-replay 2\nseed 1\nsize 64 32\ntool 10 2000 4\nslots sand\nmaterials a b\n", "materials"},
		{"no ticks", header, `"ticks`},
		{"negative ticks", header + "ticks -1\n", "ticks"},
		{"too many ticks", header + fmt.Sprintf("ticks %d\n", MaxTicks+1), "at most"},
		{"short line", header + "ticks 5\n1 2 3 P\n", "want"},
		{"long line", header + "ticks 5\n1 2 3 P 0 0\n", "want"},
		{"tick past the end", header + "ticks 5\n5 0 0 - 0\n", "below 5"},
		{"tick goes back", header + "ticks 5\n2 0 0 - 0\n1 0 0 - 0\n", "increase"},
		{"repeated tick", header + "ticks 5\n2 0 0 - 0\n2 0 0 P 0\n", "increase"},
		{"bad cursor", header + "ticks 5\n1 x 0 - 0\n", "cursor"},
		{"bad buttons", header + "ticks 5\n1 0 0 X 0\n", "buttons"},
		{"buttons out of order", header + "ticks 5\n1 0 0 DP 0\n", "buttons"},
		{"repeated button", header + "ticks 5\n1 0 0 PP 0\n", "buttons"},
		{"negative slot", header + "ticks 5\n1 0 0 - -1\n", "slot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read(strings.NewReader(tt.src))
			if err == nil {
				t.Fatalf("Read accepted\n%s", tt.src)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

// Write refuses what Read would refuse, so a saved replay always loads.
func TestWriteRejectsBadReplays(t *testing.T) {
	tests := []struct {
		name string
		edit func(r *Replay)
	}{
		{"no slots", func(r *Replay) { r.Slots = nil }},
		{"empty slot", func(r *Replay) { r.Slots = []string{"sand", ""} }},
		{"slot with a space", func(r *Replay) { r.Slots = []string{"wet sand"} }},
		{"slot with a newline", func(r *Replay) { r.Slots = []string{"sand\nticks"} }},
		{"zero height", func(r *Replay) { r.Height = 0 }},
		{"negative radius", func(r *Replay) { r.Tool.Radius = -1 }},
		{"tool deals no damage", func(r *Replay) { r.Tool.Rate = 0 }},
		{"no fingerprint", func(r *Replay) { r.Materials = "" }},
		{"fingerprint with a space", func(r *Replay) { r.Materials = "a b" }},
		{"negative slot", func(r *Replay) { r.Inputs = []game.Input{{Select: -1}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := sample()
			tt.edit(r)
			if err := Write(&bytes.Buffer{}, r); err == nil {
				t.Fatal("Write accepted the replay")
			}
		})
	}
}

// New records how a session starts, and Config starts the same session.
func TestNewAndConfig(t *testing.T) {
	tab := loadTable(t)
	cfg := &game.Config{
		Width: 96, Height: 64, Seed: 42, Workers: 3, Materials: tab,
		Slots: []string{"water", "stone"}, Tool: game.Tool{Power: 3, Rate: 77, Radius: 5},
	}
	r := New(cfg)
	want := &Replay{
		Seed: 42, Width: 96, Height: 64, Tool: game.Tool{Power: 3, Rate: 77, Radius: 5},
		Slots: []string{"water", "stone"}, Materials: tab.Fingerprint(),
	}
	checkHeader(t, r, want)
	if len(r.Inputs) != 0 {
		t.Fatalf("a new replay has %d inputs", len(r.Inputs))
	}
	cfg.Slots[0] = "sand"
	if r.Slots[0] != "water" {
		t.Fatal("the replay shares its slots with the config")
	}
	got := r.Config(tab, 2)
	if got.Width != 96 || got.Height != 64 || got.Seed != 42 || got.Workers != 2 || got.Materials != tab ||
		strings.Join(got.Slots, ",") != "water,stone" || got.Tool != cfg.Tool {
		t.Fatalf("Config = %+v", got)
	}
}

// The point of a replay: record a session's inputs, save and load them, and
// playing them back gives the same world, on any number of workers.
func TestReplayReproducesASession(t *testing.T) {
	tab := loadTable(t)
	cfg := &game.Config{
		Width: 160, Height: 96, Seed: 9, Workers: 1, Materials: tab,
		Slots: []string{"sand", "water", "stone"}, Tool: game.Tool{Power: 10, Rate: 400, Radius: 3},
	}
	rec := New(cfg)
	g := newGame(t, cfg)
	s := rng.New(11)
	var in game.Input
	for range 240 {
		if s.IntN(8) == 0 {
			in = game.Input{
				CursorX: s.IntN(cfg.Width), CursorY: s.IntN(cfg.Height),
				Place: s.Bool(), Dig: s.IntN(3) == 0, Select: s.IntN(4),
			}
		}
		rec.Inputs = append(rec.Inputs, in)
		g.Step(in)
	}
	var buf bytes.Buffer
	if err := Write(&buf, rec); err != nil {
		t.Fatal(err)
	}
	loaded, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 4} {
		p := newGame(t, loaded.Config(tab, workers))
		for _, in := range loaded.Inputs {
			p.Step(in)
		}
		if got, want := p.World().Checksum(), g.World().Checksum(); got != want {
			t.Fatalf("%d workers: playback checksum %016x, recording %016x", workers, got, want)
		}
		for id := range tab.Len() {
			if got, want := p.Inventory(material.ID(id)), g.Inventory(material.ID(id)); got != want {
				t.Fatalf("%d workers: playback holds %d %s, recording %d", workers, got, tab.Def(material.ID(id)).Name, want)
			}
		}
	}
}
