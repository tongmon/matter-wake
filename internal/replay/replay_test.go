package replay

import (
	"bytes"
	"fmt"
	"reflect"
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
		{CursorX: 30, CursorY: 8, Drop: true},
		{CursorX: 30, CursorY: 8, Place: true, Dig: true, Drop: true},
		{CursorX: 30, CursorY: 8, Place: true, Drop: true},
		{Change: game.Change{What: game.ChangeBrush, Radius: 9, Shape: game.ShapeSquare}},
		{Change: game.Change{What: game.ChangeMode, Creative: true}},
		{Change: game.Change{What: game.ChangeMode}},
		{CursorX: 30, CursorY: 8, Drop: true, Change: game.Change{
			What: game.ChangeBody, Body: game.BodyTool{Material: "ice", Width: 5, Height: 3, Angle: -45, VX: 50, VY: -100},
		}},
		{Change: game.Change{What: game.ChangeBrush | game.ChangeMode | game.ChangeBody, Radius: 0, Creative: true}},
	}
	r.Body = game.BodyTool{Material: "wood", Width: 16, Height: 12, Angle: 30, VX: -7}
	var buf bytes.Buffer
	if err := Write(&buf, r); err != nil {
		t.Fatal(err)
	}
	want := `matter-wake-replay 7
seed 7
size 64 32
scene sandbox
view 64 32
stream 64 4 512 12 60
budget 0
tool 10 2000 2 circle
body wood 16 12 30 -7 0
mode play
slots sand water
materials 0123456789abcdef
ticks 17
2 10 5 P 0 0 0
4 11 5 P 2 0 0
5 11 5 D 0 0 0
6 -3 40 PD 0 0 0
7 -3 40 PD 0 512 -1
8 0 0 - 0 0 0
9 30 8 B 0 0 0
10 30 8 PDB 0 0 0
11 30 8 PB 0 0 0
12 0 0 - 0 0 0 brush 9 square
13 0 0 - 0 0 0 mode creative
14 0 0 - 0 0 0 mode play
15 30 8 B 0 0 0 body ice 5 3 -45 50 -100
16 0 0 - 0 0 0 brush 0 circle mode creative body none
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
			Place: s.Bool(), Dig: s.IntN(4) == 0, Drop: s.IntN(8) == 0, Select: s.IntN(12),
			CameraX: s.IntN(20000) - 100, CameraY: s.IntN(20000) - 100,
			Change: randomChange(&s),
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
			r.Scene = []game.Scene{game.SceneTerrain, game.SceneSandbox, game.SceneCollapse}[i%3]
			r.Budget = i * 100_000
			if i%2 == 0 {
				r.Body = game.BodyTool{Material: "wood", Width: 3 + i, Height: 7}
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

// randomChange returns a change of a random set of settings to random
// values a replay can hold.
func randomChange(s *rng.Stream) game.Change {
	var c game.Change
	if s.IntN(4) == 0 {
		c.What |= game.ChangeBrush
		c.Radius, c.Shape = s.IntN(game.MaxRadius+1), game.Shape(s.IntN(2))
	}
	if s.IntN(4) == 0 {
		c.What |= game.ChangeMode
		c.Creative = s.Bool()
	}
	if s.IntN(4) == 0 {
		c.What |= game.ChangeBody
		if s.IntN(4) != 0 {
			c.Body = game.BodyTool{
				Material: []string{"wood", "ice"}[s.IntN(2)], Width: 1 + s.IntN(64), Height: 1 + s.IntN(64),
				Angle: s.IntN(2*game.MaxAngle+1) - game.MaxAngle,
				VX:    s.IntN(2*game.MaxSpeed+1) - game.MaxSpeed, VY: s.IntN(2*game.MaxSpeed+1) - game.MaxSpeed,
			}
		}
	}
	return c
}

func checkHeader(t *testing.T, got, want *Replay) {
	t.Helper()
	if got.Seed != want.Seed || got.Width != want.Width || got.Height != want.Height ||
		got.Scene != want.Scene || got.ViewWidth != want.ViewWidth || got.ViewHeight != want.ViewHeight ||
		got.Stream != want.Stream || got.Budget != want.Budget || got.Tool != want.Tool || got.Body != want.Body || got.Creative != want.Creative ||
		got.Materials != want.Materials || strings.Join(got.Slots, ",") != strings.Join(want.Slots, ",") {
		t.Fatalf("header = %+v, want %+v", got, want)
	}
}

// Replays are text so they can be written by hand: blank lines, comments,
// extra spaces and Windows line ends are all accepted. The file is of
// version 6, the one before, which reads as it always did: a round brush,
// a body tool at rest and unturned, and no changes.
func TestReadHandWritten(t *testing.T) {
	src := "matter-wake-replay 6\r\n" +
		"# A hand-written replay.\r\n" +
		"\r\n" +
		"seed 18446744073709551615\r\n" +
		"size  64   32 \r\n" +
		"scene terrain\r\n" +
		"view 64  32\r\n" +
		"stream 0 0  0 1 1\r\n" +
		"budget  70000\r\n" +
		"tool  0 1  0\r\n" +
		"body  wood 16  12\r\n" +
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
		Stream: game.Stream{Target: 1, Check: 1}, Budget: 70000, Tool: game.Tool{Rate: 1},
		Body: game.BodyTool{Material: "wood", Width: 16, Height: 12}, Creative: true,
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

// A version 7 file adds the brush's shape, the body tool's angle and
// velocity, and changes at the end of input lines, which hold until the
// next line.
func TestReadHandWrittenChanges(t *testing.T) {
	src := "matter-wake-replay 7\n" +
		"seed 3\nsize 64 32\nscene sandbox\nview 64 32\nstream 64 4 512 12 60\nbudget 0\n" +
		"tool 10 2000 4 square\n" +
		"body  wood 16 12 -30 50 -5\n" +
		"mode play\nslots sand\nmaterials 00\nticks 5\n" +
		"1 10 5 P 0 0 0 brush 7  circle   body none\n" +
		"3 0 0 B 0 0 0 mode creative body wood 2 3 4 5 6\n" +
		"4 0 0 - 0 0 0\n"
	r, err := Read(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	checkHeader(t, r, &Replay{
		Seed: 3, Width: 64, Height: 32, ViewWidth: 64, ViewHeight: 32,
		Stream: game.Stream{Grid: 64, Lead: 4, LeadMax: 512, Target: 12, Check: 60},
		Tool:   game.Tool{Power: 10, Rate: 2000, Radius: 4, Shape: game.ShapeSquare},
		Body:   game.BodyTool{Material: "wood", Width: 16, Height: 12, Angle: -30, VX: 50, VY: -5},
		Slots:  []string{"sand"}, Materials: "00",
	})
	brush := put
	brush.Change = game.Change{What: game.ChangeBrush | game.ChangeBody, Radius: 7}
	drop := game.Input{Drop: true, Change: game.Change{
		What: game.ChangeMode | game.ChangeBody, Creative: true,
		Body: game.BodyTool{Material: "wood", Width: 2, Height: 3, Angle: 4, VX: 5, VY: 6},
	}}
	want := []game.Input{none, brush, brush, drop, none}
	for i := range want {
		if r.Inputs[i] != want[i] {
			t.Fatalf("input %d = %+v, want %+v", i, r.Inputs[i], want[i])
		}
	}
}

func TestReadErrors(t *testing.T) {
	const (
		v        = "matter-wake-replay 7\n"
		sized    = v + "seed 1\nsize 64 32\n"
		streamed = sized + "scene sandbox\nview 64 32\nstream 64 4 512 12 60\n"
		viewed   = streamed + "budget 250000\n"
		tooled   = viewed + "tool 10 2000 4 circle\nbody none\n"
		header   = tooled + "mode play\nslots sand\nmaterials 00\n"
		// v6 is the older version's start, up to its tool line.
		v6 = "matter-wake-replay 6\nseed 1\nsize 64 32\nscene sandbox\nview 64 32\nstream 64 4 512 12 60\nbudget 0\n"
		h6 = v6 + "tool 10 2000 4\nbody wood 16 12\nmode play\nslots sand\nmaterials 00\n"
	)
	tests := []struct {
		name, src, wantErr string
	}{
		{"empty", "", "not a matter-wake replay"},
		{"newer version", "matter-wake-replay 8\n", "version 8"},
		{"older version", "matter-wake-replay 5\nseed 1\n", "version 5"},
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
		{"no budget", streamed + "tool 10 2000 4\n", `"budget`},
		{"negative budget", streamed + "budget -1\n", "budget"},
		{"bad budget", streamed + "budget lots\n", "budget"},
		{"old brush line", viewed + "brush 4\n", `"tool`},
		{"short tool", viewed + "tool 10 2000\n", `"tool`},
		{"tool without shape", viewed + "tool 10 2000 4\n", `"tool`},
		{"negative radius", viewed + "tool 10 2000 -1 circle\n", "tool"},
		{"radius over the most", viewed + fmt.Sprintf("tool 10 2000 %d circle\n", game.MaxRadius+1), "tool"},
		{"unknown shape", viewed + "tool 10 2000 4 star\n", "tool"},
		{"power over 255", viewed + "tool 256 2000 4 circle\n", "tool"},
		{"zero rate", viewed + "tool 10 0 4 circle\n", "tool"},
		{"no body", viewed + "tool 10 2000 4 circle\nmode play\n", `"body`},
		{"body without size", viewed + "tool 10 2000 4 circle\nbody wood\n", "body"},
		{"body without angle", viewed + "tool 10 2000 4 circle\nbody wood 16 12\n", "body"},
		{"body of no width", viewed + "tool 10 2000 4 circle\nbody wood 0 12 0 0 0\n", "body"},
		{"body of bad height", viewed + "tool 10 2000 4 circle\nbody wood 16 tall 0 0 0\n", "body"},
		{"body turned too far", viewed + "tool 10 2000 4 circle\nbody wood 16 12 361 0 0\n", "body"},
		{"body too fast", viewed + fmt.Sprintf("tool 10 2000 4 circle\nbody wood 16 12 0 0 %d\n", -game.MaxSpeed-1), "body"},
		{"version 6 tool with shape", v6 + "tool 10 2000 4 circle\n", `"tool`},
		{"version 6 body with angle", v6 + "tool 10 2000 4\nbody wood 16 12 0 0 0\n", "body"},
		{"version 6 change", h6 + "ticks 5\n1 2 3 P 0 0 0 mode creative\n", "want"},
		{"no mode", tooled + "slots sand\n", `"mode`},
		{"bad mode", tooled + "mode fast\n", "mode"},
		{"no slots", tooled + "mode play\nslots\n", "slots"},
		{"no materials", tooled + "mode play\nslots sand\nmaterials\n", "materials"},
		{"two fingerprints", tooled + "mode play\nslots sand\nmaterials a b\n", "materials"},
		{"no ticks", header, `"ticks`},
		{"negative ticks", header + "ticks -1\n", "ticks"},
		{"too many ticks", header + fmt.Sprintf("ticks %d\n", MaxTicks+1), "at most"},
		{"short line", header + "ticks 5\n1 2 3 P 0 0\n", "want"},
		{"long line", header + "ticks 5\n1 2 3 P 0 0 0 0\n", "changes"},
		{"unknown change", header + "ticks 5\n1 2 3 P 0 0 0 speed 2\n", "changes"},
		{"changes out of order", header + "ticks 5\n1 2 3 P 0 0 0 mode play brush 2 circle\n", "changes"},
		{"repeated change", header + "ticks 5\n1 2 3 P 0 0 0 mode play mode creative\n", "changes"},
		{"brush without shape", header + "ticks 5\n1 2 3 P 0 0 0 brush 2\n", "brush"},
		{"bad brush", header + "ticks 5\n1 2 3 P 0 0 0 brush big circle\n", "brush"},
		{"brush over the most", header + fmt.Sprintf("ticks 5\n1 2 3 P 0 0 0 brush %d square\n", game.MaxRadius+1), "brush"},
		{"bad mode change", header + "ticks 5\n1 2 3 P 0 0 0 mode fast\n", "mode"},
		{"mode without a word", header + "ticks 5\n1 2 3 P 0 0 0 mode\n", "mode"},
		{"short body change", header + "ticks 5\n1 2 3 P 0 0 0 body wood 1 2 3\n", "body"},
		{"body change then more", header + "ticks 5\n1 2 3 P 0 0 0 body none mode play\n", "body"},
		{"version 2 line", header + "ticks 5\n1 2 3 P 0\n", "want"},
		{"tick past the end", header + "ticks 5\n5 0 0 - 0 0 0\n", "below 5"},
		{"tick goes back", header + "ticks 5\n2 0 0 - 0 0 0\n1 0 0 - 0 0 0\n", "increase"},
		{"repeated tick", header + "ticks 5\n2 0 0 - 0 0 0\n2 0 0 P 0 0 0\n", "increase"},
		{"bad cursor", header + "ticks 5\n1 x 0 - 0 0 0\n", "cursor"},
		{"bad buttons", header + "ticks 5\n1 0 0 X 0 0 0\n", "buttons"},
		{"buttons out of order", header + "ticks 5\n1 0 0 DP 0 0 0\n", "buttons"},
		{"repeated button", header + "ticks 5\n1 0 0 PP 0 0 0\n", "buttons"},
		{"drop before dig", header + "ticks 5\n1 0 0 BD 0 0 0\n", "buttons"},
		{"repeated drop", header + "ticks 5\n1 0 0 PBB 0 0 0\n", "buttons"},
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
		{"negative budget", func(r *Replay) { r.Budget = -1 }},
		{"negative grid", func(r *Replay) { r.Stream.Grid = -1 }},
		{"negative radius", func(r *Replay) { r.Tool.Radius = -1 }},
		{"tool deals no damage", func(r *Replay) { r.Tool.Rate = 0 }},
		{"no fingerprint", func(r *Replay) { r.Materials = "" }},
		{"fingerprint with a space", func(r *Replay) { r.Materials = "a b" }},
		{"negative slot", func(r *Replay) { r.Inputs = []game.Input{{Select: -1}} }},
		{"body of no size", func(r *Replay) { r.Body = game.BodyTool{Material: "wood"} }},
		{"body named none", func(r *Replay) { r.Body = game.BodyTool{Material: "none", Width: 1, Height: 1} }},
		{"body with a space", func(r *Replay) { r.Body = game.BodyTool{Material: "dry wood", Width: 1, Height: 1} }},
		{"no body with a size", func(r *Replay) { r.Body = game.BodyTool{Width: 1, Height: 1} }},
		{"body turned too far", func(r *Replay) {
			r.Body = game.BodyTool{Material: "wood", Width: 1, Height: 1, Angle: -game.MaxAngle - 1}
		}},
		{"body too fast", func(r *Replay) { r.Body = game.BodyTool{Material: "wood", Width: 1, Height: 1, VX: game.MaxSpeed + 1} }},
		{"radius over the most", func(r *Replay) { r.Tool.Radius = game.MaxRadius + 1 }},
		{"unknown shape", func(r *Replay) { r.Tool.Shape = 7 }},
		{"unknown change", func(r *Replay) { r.Inputs = []game.Input{{Change: game.Change{What: 8}}} }},
		{"change of a bad brush", func(r *Replay) {
			r.Inputs = []game.Input{{Change: game.Change{What: game.ChangeBrush, Radius: -1}}}
		}},
		{"change of a bad body", func(r *Replay) {
			r.Inputs = []game.Input{{Change: game.Change{What: game.ChangeBody, Body: game.BodyTool{Material: "wood"}}}}
		}},
		{"value of no change", func(r *Replay) { r.Inputs = []game.Input{{Change: game.Change{Radius: 3}}} }},
		{"mode of a brush change", func(r *Replay) {
			r.Inputs = []game.Input{{Change: game.Change{What: game.ChangeBrush, Creative: true}}}
		}},
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
		Budget: 12345, Slots: []string{"water", "stone"}, Tool: game.Tool{Power: 3, Rate: 77, Radius: 5}, Creative: true,
		Body: game.BodyTool{Material: "wood", Width: 6, Height: 4},
	}
	r := New(cfg)
	want := &Replay{
		Seed: 42, Width: 96, Height: 64, Scene: game.SceneTerrain, ViewWidth: 40, ViewHeight: 30,
		Stream: game.Stream{Grid: 9, Lead: 2, LeadMax: 100, Target: 3, Check: 5}, Budget: 12345,
		Tool: game.Tool{Power: 3, Rate: 77, Radius: 5}, Body: cfg.Body, Creative: true, Slots: []string{"water", "stone"}, Materials: tab.Fingerprint(),
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
		got.Budget != 12345 || got.Store != nil || strings.Join(got.Slots, ",") != "water,stone" || got.Tool != cfg.Tool ||
		got.Body != cfg.Body || !got.Creative {
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
			cfg.Body = game.BodyTool{Material: "wood", Width: 8, Height: 6}
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
						Place: s.Bool(), Dig: s.IntN(3) == 0, Select: s.IntN(4), Drop: s.IntN(3) == 0,
						Change: tryChange(&s),
					}
				}
				in.CameraX, in.CameraY = camX, camY
				rec.Inputs = append(rec.Inputs, in)
				if err := g.Step(&in); err != nil {
					t.Fatal(err)
				}
			}
			if g.Dropped() == 0 {
				t.Fatal("the session dropped no bodies; the test needs some")
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
				for i := range loaded.Inputs {
					if err := p.Step(&loaded.Inputs[i]); err != nil {
						t.Fatal(err)
					}
				}
				if got, want := p.BodyRecords(nil), g.BodyRecords(nil); !reflect.DeepEqual(got, want) {
					t.Fatalf("%d workers: playback's bodies differ from the recording's", workers)
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

// tryChange returns, now and then, a change the dashboard could make in
// the replays' testdata table: the brush, the mode, or a wooden body tool
// turned and thrown.
func tryChange(s *rng.Stream) game.Change {
	var c game.Change
	switch s.IntN(6) {
	case 0:
		c = game.Change{What: game.ChangeBrush, Radius: s.IntN(8), Shape: game.Shape(s.IntN(2))}
	case 1:
		c = game.Change{What: game.ChangeMode, Creative: s.Bool()}
	case 2:
		c = game.Change{What: game.ChangeBody, Body: game.BodyTool{
			Material: "wood", Width: 2 + s.IntN(20), Height: 2 + s.IntN(20), Angle: s.IntN(361) - 180,
			VX: s.IntN(401) - 200, VY: s.IntN(401) - 200,
		}}
	}
	return c
}
