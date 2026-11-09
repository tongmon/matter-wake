package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/input"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/render"
)

// dash is the test dashboard (design.md 5절, roadmap "개발 도구"): a panel
// over the world that Tab opens and closes, the time controls and the
// views. What it changes in the world goes into the tick's game.Input, so
// a replay records it; the rest changes only what is drawn and when the
// ticks run, never what they do.
type dash struct {
	open  bool
	views render.Views
	// paused stops the ticks; step runs one at the next update anyway.
	paused, step bool
	// slow indexes speeds, and wait counts the updates since the last tick.
	slow, wait int
	// pending holds what the player did on updates that ran no tick: the
	// slot selected, a body dropped and the settings changed. The next
	// tick takes it.
	pending game.Input
	// edits is set when the dashboard may change the session, which it
	// may not while a replay plays.
	edits bool
	// over is set while the cursor is on the open panel, which then takes
	// the mouse from the world.
	over  bool
	panel render.Panel
	// wheel adds up the wheel's turns until they make a notch, so a
	// touchpad's small turns do not step a value every update.
	wheel float64
}

// speeds are how many updates a tick takes at each speed (🔶): full, ½, ¼
// and ⅛.
var speeds = [...]int{1, 2, 4, 8}

// speedNames are the speeds' words.
var speedNames = [...]string{"x1", "x1/2", "x1/4", "x1/8"}

// The values the panel steps down and up, in the order of their acts.
const (
	fieldRadius = iota
	fieldWidth
	fieldHeight
	fieldAngle
	fieldVX
	fieldVY
	fields
)

// The panel's acts (render.Span.Act). Each value has two, down and up:
// downAct and downAct+1. The span that shows a value has its down act
// negated, so the wheel over it steps it but a click does nothing.
const (
	actShape = 2*fields + 1 + iota
	actCreative
	actPause
	actStep
	actSlow
	// actView is the first view's act; the others follow, one each.
	actView
	// actSlot is the first slot's act; the others follow, one each.
	actSlot = 64
	// actSolid plus a material's ID picks that solid for the body tool.
	actSolid = actSlot + 1<<16
)

// downAct returns the act that steps field f down.
func downAct(f int) int { return 2*f + 1 }

// fieldOf returns the field act a steps and whether it steps it up, or
// false if a steps no field.
func fieldOf(a int) (f int, up, ok bool) {
	if a < 1 || a > 2*fields {
		return 0, false, false
	}
	return (a - 1) / 2, a%2 == 0, true
}

// steps are what each field changes by in a step, and with Shift held
// (🔶). Speeds are in cells per second: 1 is 0.1 m/s.
var steps = [fields][2]int{
	fieldRadius: {1, 5},
	fieldWidth:  {1, 10},
	fieldHeight: {1, 10},
	fieldAngle:  {5, 45},
	fieldVX:     {1, 10},
	fieldVY:     {1, 10},
}

// The panel's width in characters, and the width of a line's label.
const (
	panelCols = 34
	labelCols = 8
)

// The largest side of a body the panel makes, 256 pixels, so a square body
// holds all a body may (rigid.MaxPixels); the angle it wraps at; and the
// size a body tool starts at when the panel makes one from none.
const (
	maxSide             = 256
	maxAngle            = 180
	newWidth, newHeight = 16, 12
)

// newDash returns a dashboard; edits says whether it may change the
// session.
func newDash(edits bool) *dash {
	return &dash{edits: edits}
}

// parseDash parses the -dash flag: the comma-separated parts of the
// dashboard to start with, panel and the views' names.
func parseDash(s string) (open bool, views render.Views, err error) {
	if s == "" {
		return false, 0, nil
	}
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "panel" {
			open = true
			continue
		}
		found := false
		for i, name := range render.ViewNames {
			if part == name {
				views |= 1 << i
				found = true
			}
		}
		if !found {
			return false, 0, fmt.Errorf("app: -dash %q: want panel or a view: %s", part, strings.Join(render.ViewNames[:], ", "))
		}
	}
	return open, views, nil
}

// handle takes what the devices did in an update: the dashboard's keys,
// and the mouse on the panel or, with the wheel, on the brush.
func (d *dash) handle(ui *input.UI, g *game.Game) {
	d.keys(ui.Keys)
	d.over = d.open && d.panel.Contains(ui.X, ui.Y)
	notch := d.notch(ui.Wheel)
	switch {
	case d.over && ui.Click:
		if a := d.panel.Hit(ui.X, ui.Y); a > 0 {
			d.do(a, ui.Shift, g)
		}
	case d.over && notch != 0:
		a := d.panel.Hit(ui.X, ui.Y)
		if f, _, ok := fieldOf(max(a, -a)); ok {
			d.stepField(f, notch > 0, ui.Shift, g)
		}
	case notch != 0:
		d.stepField(fieldRadius, notch > 0, ui.Shift, g)
	}
}

// notch adds the wheel's turn dy and returns 1 or -1 once the turns add up
// to a notch either way, else 0.
func (d *dash) notch(dy float64) int {
	if dy > 0 && d.wheel < 0 || dy < 0 && d.wheel > 0 {
		d.wheel = 0 // a turn the other way starts over
	}
	d.wheel += dy
	switch {
	case d.wheel >= 1:
		d.wheel = 0
		return 1
	case d.wheel <= -1:
		d.wheel = 0
		return -1
	}
	return 0
}

// keys toggles what the dashboard's keys just pressed toggle.
func (d *dash) keys(k input.Keys) {
	if k&input.KeyPanel != 0 {
		d.open = !d.open
	}
	if k&input.KeyPause != 0 {
		d.do(actPause, false, nil)
	}
	if k&input.KeyStep != 0 {
		d.do(actStep, false, nil)
	}
	if k&input.KeySlow != 0 {
		d.do(actSlow, false, nil)
	}
	for i := range render.ViewNames {
		if k&(input.KeyView<<i) != 0 {
			d.views ^= 1 << i
		}
	}
}

// ticks reports whether this update runs a tick.
func (d *dash) ticks() bool {
	switch {
	case d.step:
		d.step = false
		return true
	case d.paused:
		return false
	}
	d.wait++
	if d.wait < speeds[d.slow] {
		return false
	}
	d.wait = 0
	return true
}

// timeName names the time controls' state, empty at full speed.
func (d *dash) timeName() string {
	switch {
	case d.paused:
		return "paused (Space runs, F steps)"
	case d.slow > 0:
		return "speed " + speedNames[d.slow]
	}
	return ""
}

// latch keeps what the player did in in, the input of an update, for the
// next tick: the slot selected and a body dropped.
func (d *dash) latch(in *game.Input) {
	if in.Select != 0 {
		d.pending.Select = in.Select
	}
	d.pending.Drop = d.pending.Drop || in.Drop
}

// take gives in, the input of the tick about to run, what is pending, and
// clears it. Changes to what g has already are left out, so the replay
// records only real ones.
func (d *dash) take(in *game.Input, g *game.Game) {
	d.latch(in)
	p := &d.pending
	c := &p.Change
	if c.What&game.ChangeBrush != 0 && c.Radius == g.Tool().Radius && c.Shape == g.Tool().Shape {
		c.What &^= game.ChangeBrush
		c.Radius, c.Shape = 0, 0
	}
	if c.What&game.ChangeMode != 0 && c.Creative == g.Creative() {
		c.What &^= game.ChangeMode
		c.Creative = false
	}
	if c.What&game.ChangeBody != 0 && c.Body == g.Body() {
		c.What &^= game.ChangeBody
		c.Body = game.BodyTool{}
	}
	in.Select, in.Drop, in.Change = p.Select, p.Drop, *c
	d.pending = game.Input{}
}

// brush returns the brush's radius and shape as the next tick will have
// them.
func (d *dash) brush(g *game.Game) (int, game.Shape) {
	if c := &d.pending.Change; c.What&game.ChangeBrush != 0 {
		return c.Radius, c.Shape
	}
	return g.Tool().Radius, g.Tool().Shape
}

// creative returns the mode as the next tick will have it.
func (d *dash) creative(g *game.Game) bool {
	if c := &d.pending.Change; c.What&game.ChangeMode != 0 {
		return c.Creative
	}
	return g.Creative()
}

// body returns the body tool as the next tick will have it.
func (d *dash) body(g *game.Game) game.BodyTool {
	if c := &d.pending.Change; c.What&game.ChangeBody != 0 {
		return c.Body
	}
	return g.Body()
}

// slot returns the selected slot, 1-based, as the next tick will have it.
func (d *dash) slot(g *game.Game) int {
	if d.pending.Select >= 1 && d.pending.Select <= len(g.Slots()) {
		return d.pending.Select
	}
	return g.Slot()
}

// do does what act a says, with Shift held if shift. An act that changes
// the session does nothing unless the dashboard may change it; only those
// read g.
func (d *dash) do(a int, shift bool, g *game.Game) {
	switch {
	case a == actPause:
		d.paused, d.step = !d.paused, false
	case a == actStep:
		d.paused, d.step = true, true
	case a == actSlow:
		d.slow = (d.slow + 1) % len(speeds)
	case a >= actView && a < actView+len(render.ViewNames):
		d.views ^= 1 << (a - actView)
	case !d.edits:
	case a >= actSolid:
		t := d.body(g)
		t.Material = g.Materials().Def(material.ID(a - actSolid)).Name
		if t.Width == 0 {
			t.Width, t.Height = newWidth, newHeight
		}
		d.setBody(&t)
	case a >= actSlot:
		d.pending.Select = a - actSlot + 1
	case a == actShape:
		r, s := d.brush(g)
		d.setBrush(r, 1-s)
	case a == actCreative:
		// Read the mode before marking it pending, which changes what
		// creative reads (Codex review).
		on := !d.creative(g)
		c := &d.pending.Change
		c.What |= game.ChangeMode
		c.Creative = on
	default:
		if f, up, ok := fieldOf(a); ok {
			d.stepField(f, up, shift, g)
		}
	}
}

// stepField moves field f one step up or down, a big one with shift, if
// the dashboard may change the session.
func (d *dash) stepField(f int, up, shift bool, g *game.Game) {
	if !d.edits {
		return
	}
	by := steps[f][0]
	if shift {
		by = steps[f][1]
	}
	if !up {
		by = -by
	}
	if f == fieldRadius {
		r, s := d.brush(g)
		d.setBrush(clamp(r+by, 0, game.MaxRadius), s)
		return
	}
	t := d.body(g)
	if t.Material == "" {
		return // no body tool to change
	}
	switch f {
	case fieldWidth:
		t.Width = clamp(t.Width+by, 1, maxSide)
	case fieldHeight:
		t.Height = clamp(t.Height+by, 1, maxSide)
	case fieldAngle:
		t.Angle += by
		if t.Angle > maxAngle {
			t.Angle -= 2 * maxAngle
		} else if t.Angle <= -maxAngle {
			t.Angle += 2 * maxAngle
		}
	case fieldVX:
		t.VX = clamp(t.VX+by, -game.MaxSpeed, game.MaxSpeed)
	case fieldVY:
		t.VY = clamp(t.VY+by, -game.MaxSpeed, game.MaxSpeed)
	}
	d.setBody(&t)
}

func (d *dash) setBrush(r int, s game.Shape) {
	c := &d.pending.Change
	c.What |= game.ChangeBrush
	c.Radius, c.Shape = r, s
}

func (d *dash) setBody(t *game.BodyTool) {
	c := &d.pending.Change
	c.What |= game.ChangeBody
	c.Body = *t
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// build lays the panel out for g at the right edge of a screen width
// pixels wide.
func (d *dash) build(g *game.Game, width int) {
	p := &d.panel
	p.Cols = panelCols
	p.X, p.Y = width-p.Width(), 0
	p.Lines = p.Lines[:0]
	title := "TEST DASHBOARD"
	if !d.edits {
		title += " (replay)"
	}
	d.line(text(title), text(strings.Repeat(" ", max(panelCols-len(title)-10, 1))), text("Tab closes"))
	d.materials(g)
	r, s := d.brush(g)
	d.line(append(d.value([]render.Span{label("brush")}, fieldRadius, strconv.Itoa(r)), text("  "),
		d.toggle(s.String(), actShape, s == game.ShapeSquare), text(" "),
		d.toggle("creative", actCreative, d.creative(g)))...)
	if t := d.body(g); t.Material != "" {
		size := d.value([]render.Span{label("size")}, fieldWidth, strconv.Itoa(t.Width))
		d.line(append(d.value(append(size, text(" x ")), fieldHeight, strconv.Itoa(t.Height)), text(" px"))...)
		d.line(append(d.value([]render.Span{label("angle")}, fieldAngle, strconv.Itoa(t.Angle)), text(" deg cw"))...)
		d.line(append(d.value([]render.Span{label("vx")}, fieldVX, metersPerSecond(t.VX)), text(" m/s right"))...)
		d.line(append(d.value([]render.Span{label("vy")}, fieldVY, metersPerSecond(t.VY)), text(" m/s down"))...)
	} else {
		d.line(label("body"), text("none: pick a solid"))
	}
	d.line(label("time"), span("pause", actPause, d.paused), text(" "), span("step", actStep, false),
		text(" "), span(speedNames[d.slow], actSlow, d.slow > 0))
	views := []render.Span{label("view")}
	for i, name := range render.ViewNames {
		views = append(views, span(name, actView+i, d.views&(1<<i) != 0), text(" "))
	}
	d.wrapLine(views)
	d.line(text("Shift: big steps  wheel: on values"))
}

// materials lays out the materials by kind: the slots of each kind the
// brush places, which select the slot, and the solids, which the body
// tool drops.
func (d *dash) materials(g *game.Game) {
	mats := g.Materials()
	slot, body := d.slot(g), d.body(g).Material
	for _, kind := range [...]material.Kind{material.Powder, material.Liquid, material.Static, material.Solid} {
		items := []render.Span{label(kindLabel(kind))}
		for i, id := range g.Slots() {
			if mats.Def(id).Kind == kind {
				items = append(items, d.toggle(mats.Def(id).Name, actSlot+i, i+1 == slot), text(" "))
			}
		}
		if kind == material.Solid {
			for id := range mats.Len() {
				if def := mats.Def(material.ID(id)); def.Kind == material.Solid {
					items = append(items, d.toggle(def.Name, actSolid+id, def.Name == body), text(" "))
				}
			}
		}
		if len(items) == 1 {
			items = append(items, text("-"))
		}
		d.wrapLine(items)
	}
}

// kindLabel returns the panel's word for kind.
func kindLabel(kind material.Kind) string {
	switch kind {
	case material.Powder:
		return "powder"
	case material.Liquid:
		return "liquid"
	case material.Static:
		return "static"
	case material.Solid:
		return "solid"
	case material.Empty:
	}
	return "empty"
}

// line adds a line of spans to the panel.
func (d *dash) line(spans ...render.Span) {
	d.panel.Lines = append(d.panel.Lines, spans)
}

// wrapLine adds spans, which start with a label, as lines no longer than
// the panel: what does not fit goes on the next line, under the label.
func (d *dash) wrapLine(spans []render.Span) {
	line, n := []render.Span{spans[0]}, labelCols
	for _, s := range spans[1:] {
		if n+len(s.Text) > panelCols && n > labelCols {
			d.line(line...)
			line, n = []render.Span{label("")}, labelCols
			if s.Text == " " {
				continue
			}
		}
		line = append(line, s)
		n += len(s.Text)
	}
	d.line(line...)
}

// value appends to spans the spans of field f's value v: a - to step it
// down, the value, which the wheel steps, and a + to step it up. Without
// edits they are text.
func (d *dash) value(spans []render.Span, f int, v string) []render.Span {
	down, up, show := downAct(f), downAct(f)+1, -downAct(f)
	if !d.edits {
		down, up, show = 0, 0, 0
	}
	return append(spans, span("-", down, false), span(" "+v+" ", show, false), span("+", up, false))
}

// toggle returns a span that does act a when clicked, if the dashboard
// may change the session; on marks it chosen.
func (d *dash) toggle(s string, a int, on bool) render.Span {
	if !d.edits {
		a = 0
	}
	return render.Span{Text: s, Act: a, On: on}
}

func span(s string, a int, on bool) render.Span { return render.Span{Text: s, Act: a, On: on} }

func text(s string) render.Span { return render.Span{Text: s} }

// label returns a line's label, as wide as every label.
func label(s string) render.Span { return text(fmt.Sprintf("%-*s", labelCols, s)) }

// metersPerSecond writes a speed of v cells per second in m/s, one cell
// per second being 0.1 m/s.
func metersPerSecond(v int) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%d", sign, v/10, v%10)
}
