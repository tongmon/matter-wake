package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/input"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/render"
	"github.com/tongmon/matter-wake/internal/replay"
)

// The screen the tests lay the panel out on.
const testWidth = 480

// newDashGame returns a sandbox of the testdata materials with the game's
// tool and body tool, and a dashboard that may change it.
func newDashGame(t *testing.T) (*game.Game, *game.Config, *dash) {
	t.Helper()
	mats := testMaterials(t)
	cfg := &game.Config{
		Width: 480, Height: 270, Seed: 1, Workers: 2, Materials: mats, ViewWidth: 480, ViewHeight: 270,
		Stream: stream, Budget: budget, Slots: slotsOf(mats), Tool: tool, Body: body,
	}
	g, err := game.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g, cfg, newDash(true)
}

// click clicks the span of d's panel that does act a, built for g, and
// fails the test if the panel has none.
func click(t *testing.T, d *dash, g *game.Game, a int, shift bool) {
	t.Helper()
	x, y, ok := spanAt(d, g, a)
	if !ok {
		t.Fatalf("the panel has no span for act %d", a)
	}
	d.handle(&input.UI{X: x, Y: y, Click: true, Shift: shift}, g)
}

// spanAt returns the middle of the span of d's panel, built for g, that
// does act a.
func spanAt(d *dash, g *game.Game, a int) (x, y int, ok bool) {
	d.open = true
	d.build(g, testWidth)
	p := &d.panel
	for i, line := range p.Lines {
		col := 0
		for _, s := range line {
			n := utf8.RuneCountInString(s.Text)
			if s.Act == a {
				return p.X + 4 + (col*2+n)*render.GlyphWidth/2, p.Y + 4 + i*render.LineHeight + render.LineHeight/2, true
			}
			col += n
		}
	}
	return 0, 0, false
}

// tick runs the tick of a live update: the devices' input dev, given what
// the dashboard holds, and returns the tick's input.
func tick(t *testing.T, d *dash, g *game.Game, dev *game.Input) game.Input {
	t.Helper()
	in := *dev
	d.take(&in, g)
	if err := g.Step(&in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestParseDash(t *testing.T) {
	open, views, err := parseDash("panel, cell,chunks")
	if err != nil || !open || views != render.ViewCell|render.ViewChunks {
		t.Fatalf("parseDash = %t, %b, %v", open, views, err)
	}
	if open, views, err := parseDash(""); open || views != 0 || err != nil {
		t.Fatalf("parseDash of nothing = %t, %b, %v", open, views, err)
	}
	if _, _, err := parseDash("cell,ghosts"); err == nil || !strings.Contains(err.Error(), "ghosts") {
		t.Fatalf("parseDash of an unknown part: %v", err)
	}
}

// The slots are the materials the brush places, kind by kind: with the
// shipped table, the three slots the game always had.
func TestSlotsComeFromTheTable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	mats, err := material.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := slotsOf(mats); !slices.Equal(got, []string{"sand", "water", "stone"}) {
		t.Fatalf("slots %v, want sand water stone", got)
	}
}

// Pausing stops the ticks, a step runs one, and slow speeds run a tick
// every few updates.
func TestTimeControls(t *testing.T) {
	d := newDash(true)
	count := func(updates int) int {
		n := 0
		for range updates {
			if d.ticks() {
				n++
			}
		}
		return n
	}
	if n := count(8); n != 8 {
		t.Fatalf("full speed ran %d ticks in 8 updates", n)
	}
	d.keys(input.KeyPause)
	if n := count(8); n != 0 || d.timeName() == "" {
		t.Fatalf("paused ran %d ticks, named %q", n, d.timeName())
	}
	d.keys(input.KeyStep)
	if n := count(8); n != 1 {
		t.Fatalf("a step ran %d ticks", n)
	}
	d.keys(input.KeyPause | input.KeySlow)
	d.keys(input.KeySlow)
	if n := count(16); n != 4 || d.timeName() != "speed x1/4" {
		t.Fatalf("a quarter speed ran %d ticks in 16 updates, named %q", n, d.timeName())
	}
	d.keys(input.KeySlow)
	d.keys(input.KeySlow)
	if n := count(8); n != 8 || d.timeName() != "" {
		t.Fatalf("back at full speed ran %d ticks in 8 updates, named %q", n, d.timeName())
	}
	d.keys(input.KeyView | input.KeyView<<3 | input.KeyPanel)
	if d.views != render.ViewCell|render.ViewChunks || !d.open {
		t.Fatalf("the keys turned on views %b, panel %t", d.views, d.open)
	}
}

// What the player does on updates that run no tick waits for the next
// tick; changes that change nothing are left out of it.
func TestPendingWaitsForATick(t *testing.T) {
	g, _, d := newDashGame(t)
	d.latch(&game.Input{Select: 2})
	d.latch(&game.Input{Drop: true})
	d.latch(&game.Input{})
	click(t, d, g, downAct(fieldRadius)+1, false)
	click(t, d, g, downAct(fieldRadius), false)
	if r, _ := d.brush(g); r != tool.Radius {
		t.Fatalf("the brush is %d after a step up and down, want %d", r, tool.Radius)
	}
	click(t, d, g, actCreative, false)
	in := tick(t, d, g, &game.Input{CursorX: 240, CursorY: 50})
	if in.Select != 2 || !in.Drop || in.Change != (game.Change{What: game.ChangeMode, Creative: true}) {
		t.Fatalf("the tick's input is %+v", in)
	}
	if !g.Creative() || g.Slot() != 2 || g.Dropped() != 1 {
		t.Fatalf("after the tick: creative %t, slot %d, %d dropped", g.Creative(), g.Slot(), g.Dropped())
	}
	if in := tick(t, d, g, &game.Input{}); in != (game.Input{}) {
		t.Fatalf("the next tick's input is %+v, want none", in)
	}
}

// The panel's values step by their steps, a big one with Shift or the
// wheel with Shift, within their bounds.
func TestPanelSteps(t *testing.T) {
	g, _, d := newDashGame(t)
	click(t, d, g, actSolid+int(g.Materials().MustLookup("wood")), false)
	for range 5 {
		click(t, d, g, downAct(fieldVX)+1, true)
	}
	click(t, d, g, downAct(fieldVX), false)
	x, y, _ := spanAt(d, g, -downAct(fieldVY))
	d.handle(&input.UI{X: x, Y: y, Wheel: 1, Shift: true}, g)
	d.handle(&input.UI{X: x, Y: y, Click: true}, g) // a click on the value does nothing
	for range 40 {
		click(t, d, g, downAct(fieldAngle), true)
	}
	click(t, d, g, downAct(fieldWidth), true)
	click(t, d, g, downAct(fieldWidth), true)
	want := game.BodyTool{Material: "wood", Width: 1, Height: body.Height, Angle: 0, VX: 49, VY: 10}
	if got := d.body(g); got != want {
		t.Fatalf("body tool %+v, want %+v", got, want)
	}
	click(t, d, g, actShape, false)
	d.handle(&input.UI{X: 10, Y: 10, Wheel: -1}, g) // on the world: the brush
	if r, s := d.brush(g); r != tool.Radius-1 || s != game.ShapeSquare {
		t.Fatalf("brush %d %s, want %d square", r, s, tool.Radius-1)
	}
}

// Playing a replay, the panel changes nothing that a replay records; the
// time controls and the views still work.
func TestReplayPanelChangesNothing(t *testing.T) {
	g, _, _ := newDashGame(t)
	d := newDash(false)
	d.open = true
	d.build(g, testWidth)
	if _, _, ok := spanAt(d, g, actCreative); ok {
		t.Fatal("the replay's panel has a creative switch to click")
	}
	d.do(actCreative, false, g)
	d.do(downAct(fieldRadius)+1, false, g)
	d.handle(&input.UI{X: 10, Y: 10, Wheel: 1}, g)
	if d.pending != (game.Input{}) {
		t.Fatalf("the replay's dashboard changed %+v", d.pending)
	}
	click(t, d, g, actPause, false)
	click(t, d, g, actView+2, false)
	if !d.paused || d.views != render.ViewChains {
		t.Fatalf("paused %t, views %b", d.paused, d.views)
	}
}

// A session played through the dashboard, paused and stepped, with bodies
// thrown at chosen speeds, is recorded and plays back the same, body for
// body, bit for bit.
func TestDashboardSessionReplays(t *testing.T) {
	g, cfg, d := newDashGame(t)
	rec := replay.New(cfg)
	wood := int(g.Materials().MustLookup("wood"))
	run := func(updates int, dev game.Input) {
		for range updates {
			if d.ticks() {
				in := tick(t, d, g, &dev)
				rec.Inputs = append(rec.Inputs, in)
			} else {
				d.latch(&dev)
			}
		}
	}
	run(30, game.Input{})
	// Paused, set a body thrown right at 5 m/s, turned 30°, then step.
	d.keys(input.KeyPause)
	click(t, d, g, actSolid+wood, false)
	for range 5 {
		click(t, d, g, downAct(fieldVX)+1, true)
	}
	for range 6 {
		click(t, d, g, downAct(fieldAngle)+1, false)
	}
	run(10, game.Input{CursorX: 240, CursorY: 60, Drop: true})
	d.keys(input.KeyStep)
	run(1, game.Input{CursorX: 240, CursorY: 60})
	if g.Dropped() != 1 || g.Body().VX != 50 || g.Body().Angle != 30 {
		t.Fatalf("after the step: %d dropped, body tool %+v", g.Dropped(), g.Body())
	}
	// At quarter speed, a bigger brush pours water in creative mode.
	d.keys(input.KeyPause | input.KeySlow)
	d.keys(input.KeySlow)
	click(t, d, g, actCreative, false)
	click(t, d, g, actSlot+1, false)
	click(t, d, g, downAct(fieldRadius)+1, true)
	run(40, game.Input{CursorX: 200, CursorY: 120, Place: true})
	// Full speed, a body thrown down at 10 m/s.
	d.keys(input.KeySlow)
	d.keys(input.KeySlow)
	for range 5 {
		click(t, d, g, downAct(fieldVX), true)
	}
	for range 10 {
		click(t, d, g, downAct(fieldVY)+1, true)
	}
	run(1, game.Input{CursorX: 300, CursorY: 120, Drop: true})
	run(120, game.Input{})
	if g.Dropped() != 2 {
		t.Fatalf("%d bodies dropped, want 2", g.Dropped())
	}

	var buf bytes.Buffer
	if err := replay.Write(&buf, rec); err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	for _, want := range []string{"body wood 16 12 30 50 0", "brush 9 circle mode creative", "body wood 16 12 30 0 100"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the replay has no %q:\n%s", want, text)
		}
	}
	loaded, err := replay.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	p, err := game.New(loaded.Config(cfg.Materials, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for i := range loaded.Inputs {
		if err := p.Step(&loaded.Inputs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if p.World().Checksum() != g.World().Checksum() || !reflect.DeepEqual(p.BodyRecords(nil), g.BodyRecords(nil)) {
		t.Fatal("the replay played back to another world")
	}
}

// A touchpad's small turns step a value once they add up to a notch; a
// turn the other way starts over.
func TestWheelNotches(t *testing.T) {
	d := newDash(true)
	got := []int{d.notch(0.4), d.notch(0), d.notch(0.4), d.notch(0.4), d.notch(-0.5), d.notch(-0.6), d.notch(1), d.notch(0)}
	if want := []int{0, 0, 0, 1, 0, -1, 1, 0}; !slices.Equal(got, want) {
		t.Fatalf("notches %v, want %v", got, want)
	}
}

// The creative switch turns creative mode off as well as on, also when
// the session started in it (Codex review).
func TestCreativeSwitchTurnsOff(t *testing.T) {
	g, _, d := newDashGame(t)
	click(t, d, g, actCreative, false)
	tick(t, d, g, &game.Input{})
	if !g.Creative() {
		t.Fatal("the switch did not turn creative mode on")
	}
	click(t, d, g, actCreative, false)
	if in := tick(t, d, g, &game.Input{}); in.Change != (game.Change{What: game.ChangeMode}) || g.Creative() {
		t.Fatalf("the switch sent %+v and left creative %t; want it off", in.Change, g.Creative())
	}
}
