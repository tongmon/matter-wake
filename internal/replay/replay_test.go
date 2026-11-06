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
		Seed: 7, Width: 64, Height: 32, ViewWidth: 64, ViewHeight: 32, Stream: game.Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 12, Check: 60},
		Tool: game.Tool{Power: 10, Rate: 2000, Radius: 2}, Slots: []string{"sand", "water"}, Materials: "0123456789abcdef",
	}
}

func TestWrite(t *testing.T) {
	r := sample()
	r.Inputs = []game.Input{
		none, none, put, put,
		{CursorX: 11, CursorY: 5, Place: true, Select: 2},
		{CursorX: 11, CursorY: 5, Dig: true},
		{CursorX: -3, CursorY: 40, Place: true, Dig: true},
		{CursorX: -3, CursorY: 40, Place: true, Dig: true, CameraX: 512, CameraY: -1},
		none,
	}
	var buf bytes.Buffer
	if err := Write(&buf, r); err != nil {
		t.Fatal(err)
	}
	want := `matter-wake-replay 4
seed 7
size 64 32
scene sandbox
view 64 32
stream 64 4 512 12 60
tool 10 2000 2
mode play
slots sand water
materials 0123456789abcdef
ticks 9
2 10 5 P 0 0 0
4 11 5 P 2 0 0
5 11 5 D 0 0 0
6 -3 40 PD 0 0 0
7 -3 40 PD 0 512 -1
8 0 0 - 0 0 0
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
			CameraX: s.IntN(20000) - 100, CameraY: s.IntN(20000) - 100,
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
			r.Creative = i%2 == 1
			if i%2 == 0 {
				r.Scene = game.SceneTerrain
			}
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
		got.Scene != want.Scene || got.ViewWidth != want.ViewWidth || got.ViewHeight != want.ViewHeight ||
		got.Stream != want.Stream || got.Tool != want.Tool || got.Creative != want.Creative ||
		got.Materials != want.Materials || strings.Join(got.Slots, ",") != strings.Join(want.Slots, ",") {
		t.Fatalf("header = %+v, want %+v", got, want)
	}
}

// Replays are text so they can be written by hand: blank lines, comments,
// extra spaces and Windows line ends are all accepted.
func TestReadHandWritten(t *testing.T) {
	src := "matter-wake-replay 4\r\n" +
		"# A hand-written replay.\r\n" +
		"\r\n" +
		"seed 18446744073709551615\r\n" +
		"size  64   32 \r\n" +
		"scene terrain\r\n" +
		"view 64  32\r\n" +
		"stream 0 0  0 1 1\r\n" +
		"tool  0 1  0\r\n" +
		"mode creative \r\n" +
		"slots stone\r\n" +
		"materials unknown\r\n" +
		"ticks 6\r\n" +
		"  1 10 5 P 0 0 0\r\n" +
		"# hold, then stop\r\n" +
		"4 0 0 - 1 0 0\r\n" +
		"5 0 0 - 0 0 0"
	r, err := Read(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	checkHeader(t, r, &Replay{
		Seed: 1<<64 - 1, Width: 64, Height: 32, Scene: game.SceneTerrain, ViewWidth: 64, ViewHeight: 32,
		Stream: game.Stream{Target: 1, Check: 1}, Tool: game.Tool{Rate: 1}, Creative: true,
		Slots: []string{"stone"}, Materials: "unknown",
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
	const (
		v      = "matter-wake-replay 4\n"
		sized  = v + "seed 1\nsize 64 32\n"
		viewed = sized + "scene sandbox\nview 64 32\nstream 64 4 512 12 60\n"
		header = viewed + "tool 10 2000 4\nmode play\nslots sand\nmaterials 00\n"
	)
	tests := []struct {
		name, src, wantErr string
	}{
		{"empty", "", "not a matter-wake replay"},
		{"newer version", "matter-wake-replay 5\n", "version 5"},
		{"older version", "matter-wake-replay 3\nseed 1\n", "version 3"},
		{"bad version", "matter-wake-replay one\n", "version"},
		{"missing seed", v + "size 64 32\n", `"seed`},
		{"negative seed", v + "seed -1\n", "seed"},
		{"one size", v + "seed 1\nsize 64\n", "size"},
		{"zero width", v + "seed 1\nsize 0 32\n", "size"},
		{"no scene", sized + "tool 10 2000 4\n", `"scene`},
		{"unknown scene", sized + "scene cave\n", "scene"},
		{"zero view", sized + "scene sandbox\nview 0 32\n", "view"},
		{"old stream line", sized + "scene sandbox\nview 64 32\nstream 256 12\n", "stream"},
		{"negative grid", sized + "scene sandbox\nview 64 32\nstream -1 4 512 12 60\n", "stream"},
		{"negative lead", sized + "scene sandbox\nview 64 32\nstream 64 -4 512 12 60\n", "stream"},
		{"negative lead cap", sized + "scene sandbox\nview 64 32\nstream 64 4 -512 12 60\n", "stream"},
		{"no target", sized + "scene terrain\nview 64 32\nstream 64 4 512 0 60\n", "stream"},
		{"no checks", sized + "scene terrain\nview 64 32\nstream 64 4 512 12 0\n", "stream"},
		{"old brush line", viewed + "brush 4\n", `"tool`},
		{"short tool", viewed + "tool 10 2000\n", `"tool`},
		{"negative radius", viewed + "tool 10 2000 -1\n", "tool"},
		{"power over 255", viewed + "tool 256 2000 4\n", "tool"},
		{"zero rate", viewed + "tool 10 0 4\n", "tool"},
		{"no mode", viewed + "tool 10 2000 4\nslots sand\n", `"mode`},
		{"bad mode", viewed + "tool 10 2000 4\nmode fast\n", "mode"},
		{"no slots", viewed + "tool 10 2000 4\nmode play\nslots\n", "slots"},
		{"no materials", viewed + "tool 10 2000 4\nmode play\nslots sand\nmaterials\n", "materials"},
		{"two fingerprints", viewed + "tool 10 2000 4\nmode play\nslots sand\nmaterials a b\n", "materials"},
		{"no ticks", header, `"ticks`},
		{"negative ticks", header + "ticks -1\n", "ticks"},
		{"too many ticks", header + fmt.Sprintf("ticks %d\n", MaxTicks+1), "at most"},
		{"short line", header + "ticks 5\n1 2 3 P 0 0\n", "want"},
		{"long line", header + "ticks 5\n1 2 3 P 0 0 0 0\n", "want"},
		{"version 2 line", header + "ticks 5\n1 2 3 P 0\n", "want"},
		{"tick past the end", header + "ticks 5\n5 0 0 - 0 0 0\n", "below 5"},
		{"tick goes back", header + "ticks 5\n2 0 0 - 0 0 0\n1 0 0 - 0 0 0\n", "increase"},
		{"repeated tick", header + "ticks 5\n2 0 0 - 0 0 0\n2 0 0 P 0 0 0\n", "increase"},
		{"bad cursor", header + "ticks 5\n1 x 0 - 0 0 0\n", "cursor"},
		{"bad buttons", header + "ticks 5\n1 0 0 X 0 0 0\n", "buttons"},
		{"buttons out of order", header + "ticks 5\n1 0 0 DP 0 0 0\n", "buttons"},
		{"repeated button", header + "ticks 5\n1 0 0 PP 0 0 0\n", "buttons"},
		{"negative slot", header + "ticks 5\n1 0 0 - -1 0 0\n", "slot"},
		{"bad camera", header + "ticks 5\n1 0 0 - 0 0 y\n", "camera"},
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
		{"unknown scene", func(r *Replay) { r.Scene = "cave" }},
		{"zero view", func(r *Replay) { r.ViewWidth = 0 }},
		{"no target", func(r *Replay) { r.Stream.Target = 0 }},
		{"no checks", func(r *Replay) { r.Stream.Check = 0 }},
		{"negative grid", func(r *Replay) { r.Stream.Grid = -1 }},
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
		Scene: game.SceneTerrain, ViewWidth: 40, ViewHeight: 30, Stream: game.Stream{Grid: 9, Lead: 2, LeadMax: 100, Target: 3, Check: 5},
		Slots: []string{"water", "stone"}, Tool: game.Tool{Power: 3, Rate: 77, Radius: 5}, Creative: true,
	}
	r := New(cfg)
	want := &Replay{
		Seed: 42, Width: 96, Height: 64, Scene: game.SceneTerrain, ViewWidth: 40, ViewHeight: 30,
		Stream: game.Stream{Grid: 9, Lead: 2, LeadMax: 100, Target: 3, Check: 5}, Tool: game.Tool{Power: 3, Rate: 77, Radius: 5}, Creative: true,
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
		got.Scene != cfg.Scene || got.ViewWidth != 40 || got.ViewHeight != 30 || got.Stream != cfg.Stream ||
		got.Store != nil || strings.Join(got.Slots, ",") != "water,stone" || got.Tool != cfg.Tool || !got.Creative {
		t.Fatalf("Config = %+v", got)
	}
}

// The point of a replay: record a session's inputs, save and load them, and
// playing them back gives the same world, on any number of workers. In the
// terrain the camera wanders, so blocks leave memory and come back.
func TestReplayReproducesASession(t *testing.T) {
	tab := loadTable(t)
	tests := []struct {
		name string
		cfg  game.Config
	}{
		{"sandbox", game.Config{Width: 160, Height: 96, ViewWidth: 160, ViewHeight: 96}},
		{"terrain", game.Config{
			Width: 3072, Height: 3072, Scene: game.SceneTerrain, ViewWidth: 480, ViewHeight: 270,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.Seed, cfg.Workers, cfg.Materials = 9, 1, tab
			cfg.Stream = game.Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 6, Check: 20}
			cfg.Slots, cfg.Tool = []string{"sand", "water", "stone"}, game.Tool{Power: 10, Rate: 400, Radius: 3}
			rec := New(&cfg)
			g := newGame(t, &cfg)
			camX, camY := g.Spawn()
			s := rng.New(11)
			var in game.Input
			for range 240 {
				camX = min(max(camX+s.IntN(129)-64, 0), cfg.Width-cfg.ViewWidth)
				camY = min(max(camY+s.IntN(65)-32, 0), cfg.Height-cfg.ViewHeight)
				if s.IntN(8) == 0 {
					in = game.Input{
						CursorX: camX + s.IntN(cfg.ViewWidth), CursorY: camY + s.IntN(cfg.ViewHeight),
						Place: s.Bool(), Dig: s.IntN(3) == 0, Select: s.IntN(4),
					}
				}
				in.CameraX, in.CameraY = camX, camY
				rec.Inputs = append(rec.Inputs, in)
				if err := g.Step(in); err != nil {
					t.Fatal(err)
				}
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
					if err := p.Step(in); err != nil {
						t.Fatal(err)
					}
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
		})
	}
}
