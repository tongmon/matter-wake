// Package replay records the inputs of a session and plays them back.
//
// A replay is how a session started and the game.Input of every tick.
// Input is the only thing from outside that changes a session, so playing
// the inputs back from the same start gives the same world.
//
// The format is line-based text, so replays diff well and can be written
// by hand:
//
//	matter-wake-replay 7
//	seed 1
//	size 480 270
//	scene sandbox
//	view 480 270
//	stream 64 4 512 12 60
//	budget 300000
//	tool 10 2000 4 circle
//	body wood 16 12 0 0 0
//	mode play
//	slots sand water stone
//	materials 3f2a9c0d1e4b5a67
//	ticks 600
//	20 240 60 - 3 0 0
//	21 240 60 P 0 0 0 brush 8 square
//	22 240 60 P 0 0 0
//	50 240 60 B 0 0 0 body wood 8 4 30 50 0
//	51 240 60 - 0 0 0
//
// The first line names the format and its version. The header follows in
// this order: the world seed, the world size in cells, the scene (sandbox,
// collapse or terrain, game.Scene), the size of the camera's view in cells, the
// stream (the grid, the lead and its cap, the target and the ticks between
// checks, as in game.Stream), the budget (how many cells a tick may scan, 0
// for no limit, as in game.Config.Budget), the tool (its power, its damage
// per tick, its brush radius and the brush's shape, circle or square, as
// in game.Tool), the body tool (the solid its bodies are made of, their
// width and height in pixels, their angle in degrees clockwise and the
// velocity they start with in cells per second, rightward and downward, or
// none, as in game.BodyTool), the mode (play, or creative for
// game.Config.Creative), the materials the slots hold in key order, the
// fingerprint of the material table (material.Table.Fingerprint) and the
// number of ticks.
//
// Each later line is a tick and the input from that tick on, written only
// where the input changes: the cursor's x and y in cells, the buttons (P
// to place, D to dig, B to drop a body, in that order, as many as are
// pressed, or - for none), the slot to select, 0 to keep the current one,
// and the camera's x and y: the top-left cell of the view. The settings
// the input changes (game.Change) may follow, each at most once and in
// this order: brush and its radius and shape, mode and the mode, body and
// the body tool as the header writes it. Setting a value again changes
// nothing, so a change in effect for several ticks does no harm. Ticks
// before the first line have no input. Blank lines and lines starting
// with # are ignored.
//
// Version 6 files are read too and mean the same: their tool line has no
// shape (the brush is round), their body line no angle or velocity (0),
// and their input lines no changes.
//
// The package reads and writes streams; the app layer opens the files.
package replay

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
)

// Version is the format version this build writes. A change to the format
// raises it. Files of this version and of oldVersion, the one before,
// which means the same in this one, are read; other versions are refused
// rather than misread.
const Version = 7

// oldVersion is the older format version this build still reads.
const oldVersion = 6

// magic is the first word of every replay.
const magic = "matter-wake-replay"

// MaxTicks caps the tick count a replay may hold, 24 hours of play, so a
// typo in a hand-written header cannot ask for gigabytes of memory.
const MaxTicks = 24 * 60 * 60 * game.Hz

// Replay is a recorded session: how it started and its input for every
// tick. The worker count is not part of it, because it does not change
// the result.
type Replay struct {
	Seed          uint64
	Width, Height int
	// Scene, the view's size and the stream decide which blocks of the
	// terrain are in memory, and so what the world does.
	Scene                 game.Scene
	ViewWidth, ViewHeight int
	Stream                game.Stream
	// Budget is how many cells a tick may scan; it decides which chunks
	// wait, and so what the world does.
	Budget int
	Tool   game.Tool
	// Body is what the body tool drops.
	Body game.BodyTool
	// Creative is game.Config.Creative: placing takes nothing from the
	// inventory.
	Creative bool
	// Slots name the materials the player can place, in key order.
	Slots []string
	// Materials is the fingerprint of the material table the session was
	// recorded with. Playback with another table may diverge.
	Materials string
	// Inputs holds the input of every tick: Inputs[i] advances tick i to
	// tick i+1.
	Inputs []game.Input
}

// New returns an empty recording of a session started with cfg.
func New(cfg *game.Config) *Replay {
	return &Replay{
		Seed: cfg.Seed, Width: cfg.Width, Height: cfg.Height, Tool: cfg.Tool, Body: cfg.Body, Creative: cfg.Creative,
		Scene: cfg.Scene, ViewWidth: cfg.ViewWidth, ViewHeight: cfg.ViewHeight, Stream: cfg.Stream,
		Budget: cfg.Budget, Slots: slices.Clone(cfg.Slots), Materials: cfg.Materials.Fingerprint(),
	}
}

// Config returns the config that starts the recorded session again, with
// mats and on the given number of workers. The terrain keeps the blocks it
// unloads in memory, so playback never touches a saved world.
func (r *Replay) Config(mats *material.Table, workers int) *game.Config {
	return &game.Config{
		Width: r.Width, Height: r.Height, Seed: r.Seed, Workers: workers, Materials: mats,
		Scene: r.Scene, ViewWidth: r.ViewWidth, ViewHeight: r.ViewHeight, Stream: r.Stream,
		Budget: r.Budget, Slots: slices.Clone(r.Slots), Tool: r.Tool, Body: r.Body, Creative: r.Creative,
	}
}

// Recordable reports why a session started with cfg could not be
// recorded: its header is one Write refuses, or the table has a solid
// whose name a body tool's change could not write, not one word or none
// (the word for no tool). A recording checks it before the session, since
// Write runs only at its end (two Codex reviews).
func Recordable(cfg *game.Config) error {
	if err := New(cfg).validate(); err != nil {
		return err
	}
	for id := range cfg.Materials.Len() {
		if d := cfg.Materials.Def(material.ID(id)); d.Kind == material.Solid && (!isWord(d.Name) || d.Name == bodyNone) {
			return fmt.Errorf("replay: the solid %q is not one word other than %q, as a replay names the body tool's material",
				d.Name, bodyNone)
		}
	}
	return nil
}

// Write encodes r in the current version.
func Write(w io.Writer, r *Replay) error {
	if err := r.validate(); err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	// A bufio.Writer keeps its first error and Flush returns it.
	s := r.Stream
	_, _ = fmt.Fprintf(bw, "%s %d\nseed %d\nsize %d %d\nscene %s\nview %d %d\nstream %d %d %d %d %d\nbudget %d\n",
		magic, Version, r.Seed, r.Width, r.Height, sceneWord(r.Scene), r.ViewWidth, r.ViewHeight,
		s.Grid, s.Lead, s.LeadMax, s.Target, s.Check, r.Budget)
	_, _ = fmt.Fprintf(bw, "tool %d %d %d %s\nbody %s\nmode %s\nslots %s\nmaterials %s\nticks %d\n",
		r.Tool.Power, r.Tool.Rate, r.Tool.Radius, r.Tool.Shape, bodyWords(&r.Body), mode(r.Creative),
		strings.Join(r.Slots, " "), r.Materials, len(r.Inputs))
	var prev game.Input
	for i := range r.Inputs {
		in := &r.Inputs[i]
		if *in != prev {
			_, _ = fmt.Fprintf(bw, "%d %d %d %s %d %d %d%s\n",
				i, in.CursorX, in.CursorY, buttons(in), in.Select, in.CameraX, in.CameraY, changeWords(&in.Change))
			prev = *in
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	return nil
}

func (r *Replay) validate() error {
	switch {
	case r.Width < 1 || r.Height < 1:
		return fmt.Errorf("replay: world size %dx%d is not positive", r.Width, r.Height)
	case r.Scene != game.SceneSandbox && r.Scene != game.SceneTerrain && r.Scene != game.SceneCollapse:
		return fmt.Errorf("replay: unknown scene %q", r.Scene)
	case r.ViewWidth < 1 || r.ViewHeight < 1:
		return fmt.Errorf("replay: view %dx%d is not positive", r.ViewWidth, r.ViewHeight)
	case !streamValid(r.Stream):
		return fmt.Errorf("replay: stream %+v needs a grid and leads of 0 or more, a target of 1 or more and checks 1 tick or more apart", r.Stream)
	case r.Budget < 0:
		return fmt.Errorf("replay: budget %d is negative", r.Budget)
	case r.Tool.Rate == 0 || !brushValid(r.Tool.Radius, r.Tool.Shape):
		return fmt.Errorf("replay: tool %+v needs a rate of 1 or more, a radius of 0 to %d and a known shape", r.Tool, game.MaxRadius)
	case !bodyValid(&r.Body):
		return fmt.Errorf("replay: body tool %+v: %s", r.Body, bodyWant)
	case len(r.Slots) == 0:
		return errors.New("replay: no material slots")
	case !isWord(r.Materials):
		return fmt.Errorf("replay: material fingerprint %q is not one word", r.Materials)
	case len(r.Inputs) > MaxTicks:
		return fmt.Errorf("replay: %d ticks, at most %d (24 hours)", len(r.Inputs), MaxTicks)
	}
	for i, name := range r.Slots {
		if !isWord(name) {
			return fmt.Errorf("replay: slot %d names %q, which is not one word", i+1, name)
		}
	}
	for i := range r.Inputs {
		in := &r.Inputs[i]
		if in.Select < 0 {
			return fmt.Errorf("replay: tick %d selects slot %d", i, in.Select)
		}
		if err := checkChange(&in.Change); err != nil {
			return fmt.Errorf("replay: tick %d: %w", i, err)
		}
	}
	return nil
}

// isWord reports whether s is a non-empty run of non-space characters, so
// it reads back as one field.
func isWord(s string) bool {
	return s != "" && !strings.ContainsFunc(s, unicode.IsSpace)
}

// brushValid reports whether a brush of radius r and shape s is one the
// game takes.
func brushValid(r int, s game.Shape) bool {
	return r >= 0 && r <= game.MaxRadius && (s == game.ShapeCircle || s == game.ShapeSquare)
}

// bodyWant says what a body tool needs, for errors.
var bodyWant = fmt.Sprintf("want a material of one word other than %q, a positive size, an angle of %d to %d "+
	"and a velocity of %d to %d either way, or %s", bodyNone, -game.MaxAngle, game.MaxAngle,
	-game.MaxSpeed, game.MaxSpeed, bodyNone)

// bodyValid reports whether t is a body tool a replay can hold: no tool,
// or one whose numbers the game takes. Whether its material is a solid
// depends on the material table, which the game checks.
func bodyValid(t *game.BodyTool) bool {
	if t.Material == "" {
		return *t == game.BodyTool{}
	}
	return isWord(t.Material) && t.Material != bodyNone && t.Width >= 1 && t.Height >= 1 &&
		t.Angle >= -game.MaxAngle && t.Angle <= game.MaxAngle &&
		t.VX >= -game.MaxSpeed && t.VX <= game.MaxSpeed && t.VY >= -game.MaxSpeed && t.VY <= game.MaxSpeed
}

// allChanges are the changes a replay holds.
const allChanges = game.ChangeBrush | game.ChangeMode | game.ChangeBody

// checkChange reports why c cannot be written.
func checkChange(c *game.Change) error {
	switch {
	case c.What&^allChanges != 0:
		return fmt.Errorf("unknown changes %#x", c.What&^allChanges)
	case c.What&game.ChangeBrush != 0 && !brushValid(c.Radius, c.Shape):
		return fmt.Errorf("brush %d %s: want a radius of 0 to %d and a known shape", c.Radius, c.Shape, game.MaxRadius)
	case c.What&game.ChangeBody != 0 && !bodyValid(&c.Body):
		return fmt.Errorf("body tool %+v: %s", c.Body, bodyWant)
	}
	// What a change does not set must be zero, or it would not read back
	// the same.
	unset := *c
	unset.What = 0
	if c.What&game.ChangeBrush != 0 {
		unset.Radius, unset.Shape = 0, 0
	}
	if c.What&game.ChangeMode != 0 {
		unset.Creative = false
	}
	if c.What&game.ChangeBody != 0 {
		unset.Body = game.BodyTool{}
	}
	if unset != (game.Change{}) {
		return fmt.Errorf("change %+v holds values for settings it does not change", *c)
	}
	return nil
}

// The words of the mode line.
const (
	modePlay     = "play"
	modeCreative = "creative"
)

// The words of the scene line.
const (
	sceneSandbox  = "sandbox"
	sceneTerrain  = "terrain"
	sceneCollapse = "collapse"
)

func sceneWord(s game.Scene) string {
	switch s {
	case game.SceneTerrain:
		return sceneTerrain
	case game.SceneCollapse:
		return sceneCollapse
	case game.SceneSandbox:
	}
	return sceneSandbox
}

func mode(creative bool) string {
	if creative {
		return modeCreative
	}
	return modePlay
}

// bodyNone is the body line's word for no body tool.
const bodyNone = "none"

func bodyWords(t *game.BodyTool) string {
	if t.Material == "" {
		return bodyNone
	}
	return fmt.Sprintf("%s %d %d %d %d %d", t.Material, t.Width, t.Height, t.Angle, t.VX, t.VY)
}

// The words that start the changes of an input line, in the order they
// are written.
var changeKeys = [...]string{"brush", "mode", "body"}

// changeWords returns the words of the changes c makes, each after a
// space, or "" for none.
func changeWords(c *game.Change) string {
	if c.What == 0 {
		return ""
	}
	var b strings.Builder
	if c.What&game.ChangeBrush != 0 {
		fmt.Fprintf(&b, " %s %d %s", changeKeys[0], c.Radius, c.Shape)
	}
	if c.What&game.ChangeMode != 0 {
		fmt.Fprintf(&b, " %s %s", changeKeys[1], mode(c.Creative))
	}
	if c.What&game.ChangeBody != 0 {
		fmt.Fprintf(&b, " %s %s", changeKeys[2], bodyWords(&c.Body))
	}
	return b.String()
}

// The buttons of an input line, in the order they are written.
var buttonLetters = [...]byte{'P', 'D', 'B'}

func buttons(in *game.Input) string {
	var b []byte
	for i, on := range [...]bool{in.Place, in.Dig, in.Drop} {
		if on {
			b = append(b, buttonLetters[i])
		}
	}
	if len(b) == 0 {
		return "-"
	}
	return string(b)
}

// parseButtons sets the buttons of in from s: letters of buttonLetters in
// their order, each at most once, or - for none.
func parseButtons(s string, in *game.Input) error {
	if s == "-" {
		return nil
	}
	pressed := [...]*bool{&in.Place, &in.Dig, &in.Drop}
	next := 0
	for k := range len(s) {
		for next < len(buttonLetters) && buttonLetters[next] != s[k] {
			next++
		}
		if next == len(buttonLetters) {
			return fmt.Errorf("buttons %q: want P, D and B in that order, each at most once, or -", s)
		}
		*pressed[next] = true
		next++
	}
	return nil
}

// Read decodes a replay written by Write or by hand.
func Read(rd io.Reader) (*Replay, error) {
	p := &parser{sc: bufio.NewScanner(rd)}
	if err := p.readVersion(); err != nil {
		return nil, err
	}
	r := &Replay{}
	ticks, err := p.header(r)
	if err != nil {
		return nil, err
	}
	r.Inputs = make([]game.Input, ticks)
	if err := p.inputs(r.Inputs); err != nil {
		return nil, err
	}
	if err := p.sc.Err(); err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	return r, nil
}

type parser struct {
	sc   *bufio.Scanner
	line int
	// version is the file's format version.
	version int
}

// next returns the next line that is neither blank nor a comment, without
// its surrounding spaces, or false at the end of the input.
func (p *parser) next() (string, bool) {
	for p.sc.Scan() {
		p.line++
		if s := strings.TrimSpace(p.sc.Text()); s != "" && !strings.HasPrefix(s, "#") {
			return s, true
		}
	}
	return "", false
}

func (p *parser) errorf(format string, args ...any) error {
	if err := p.sc.Err(); err != nil {
		return fmt.Errorf("replay: line %d: %w", p.line+1, err)
	}
	return fmt.Errorf("replay: line %d: %s", p.line, fmt.Sprintf(format, args...))
}

func (p *parser) readVersion() error {
	s, _ := p.next()
	f := strings.Fields(s)
	if len(f) != 2 || f[0] != magic {
		return p.errorf("not a matter-wake replay: want %q, got %q", fmt.Sprintf("%s %d", magic, Version), s)
	}
	v, err := strconv.Atoi(f[1])
	switch {
	case err != nil:
		return p.errorf("bad format version %q", f[1])
	case v != Version && v != oldVersion:
		return p.errorf("format version %d; this build reads versions %d and %d", v, oldVersion, Version)
	}
	p.version = v
	return nil
}

// field reads the header line for key, which want describes, and returns
// its values. A count of 0 accepts one or more.
func (p *parser) field(key, want string, count int) ([]string, error) {
	s, _ := p.next()
	f := strings.Fields(s)
	if len(f) < 2 || f[0] != key || (count > 0 && len(f) != count+1) {
		return nil, p.errorf("want %q, got %q", want, s)
	}
	return f[1:], nil
}

// header reads the lines after the version into r and returns the tick
// count.
func (p *parser) header(r *Replay) (int, error) {
	f, err := p.field("seed", "seed <number>", 1)
	if err != nil {
		return 0, err
	}
	if r.Seed, err = strconv.ParseUint(f[0], 10, 64); err != nil {
		return 0, p.errorf("seed %q is not a number from 0 to 2^64-1", f[0])
	}
	if f, err = p.field("size", "size <width> <height>", 2); err != nil {
		return 0, err
	}
	w, werr := strconv.Atoi(f[0])
	h, herr := strconv.Atoi(f[1])
	if werr != nil || herr != nil || w < 1 || h < 1 {
		return 0, p.errorf("size %s %s is not two positive numbers", f[0], f[1])
	}
	r.Width, r.Height = w, h
	if err := p.view(r); err != nil {
		return 0, err
	}
	if err := p.tools(r); err != nil {
		return 0, err
	}
	if f, err = p.field("mode", "mode play|creative", 1); err != nil {
		return 0, err
	}
	switch f[0] {
	case modePlay:
	case modeCreative:
		r.Creative = true
	default:
		return 0, p.errorf("mode %q: want %s or %s", f[0], modePlay, modeCreative)
	}
	if r.Slots, err = p.field("slots", "slots <material>...", 0); err != nil {
		return 0, err
	}
	if f, err = p.field("materials", "materials <fingerprint>", 1); err != nil {
		return 0, err
	}
	r.Materials = f[0]
	if f, err = p.field("ticks", "ticks <count>", 1); err != nil {
		return 0, err
	}
	ticks, err := strconv.Atoi(f[0])
	switch {
	case err != nil || ticks < 0:
		return 0, p.errorf("ticks %q is not a number of 0 or more", f[0])
	case ticks > MaxTicks:
		return 0, p.errorf("ticks %d: at most %d (24 hours)", ticks, MaxTicks)
	}
	return ticks, nil
}

// view reads the scene, view, stream and budget lines into r.
func (p *parser) view(r *Replay) error {
	f, err := p.field("scene", "scene sandbox|collapse|terrain", 1)
	if err != nil {
		return err
	}
	switch f[0] {
	case sceneSandbox:
		r.Scene = game.SceneSandbox
	case sceneTerrain:
		r.Scene = game.SceneTerrain
	case sceneCollapse:
		r.Scene = game.SceneCollapse
	default:
		return p.errorf("scene %q: want %s, %s or %s", f[0], sceneSandbox, sceneCollapse, sceneTerrain)
	}
	if f, err = p.field("view", "view <width> <height>", 2); err != nil {
		return err
	}
	w, werr := strconv.Atoi(f[0])
	h, herr := strconv.Atoi(f[1])
	if werr != nil || herr != nil || w < 1 || h < 1 {
		return p.errorf("view %s %s is not two positive numbers", f[0], f[1])
	}
	r.ViewWidth, r.ViewHeight = w, h
	if f, err = p.field("stream", "stream <grid> <lead> <lead cap> <target> <check>", 5); err != nil {
		return err
	}
	var v [5]int
	for i := range v {
		if v[i], err = strconv.Atoi(f[i]); err != nil {
			return p.errorf("stream %s: want five numbers", strings.Join(f, " "))
		}
	}
	r.Stream = game.Stream{Grid: v[0], Lead: v[1], LeadMax: v[2], Target: v[3], Check: v[4]}
	if !streamValid(r.Stream) {
		return p.errorf("stream %s: want a grid and leads of 0 or more, a target of 1 or more and checks 1 tick or more apart", strings.Join(f, " "))
	}
	if f, err = p.field("budget", "budget <cells>", 1); err != nil {
		return err
	}
	if r.Budget, err = strconv.Atoi(f[0]); err != nil || r.Budget < 0 {
		return p.errorf("budget %q is not a number of 0 or more", f[0])
	}
	return nil
}

// tools reads the tool and body lines into r.
func (p *parser) tools(r *Replay) error {
	toolWant, toolFields := "tool <power> <rate> <radius> <shape>", 4
	bodyWantLine := "body <material> <width> <height> <angle> <vx> <vy> | body none"
	if p.version == oldVersion {
		toolWant, toolFields = "tool <power> <rate> <radius>", 3
		bodyWantLine = "body <material> <width> <height> | body none"
	}
	f, err := p.field("tool", toolWant, toolFields)
	if err != nil {
		return err
	}
	if r.Tool, err = parseTool(f); err != nil {
		return p.errorf("%v", err)
	}
	if f, err = p.field("body", bodyWantLine, 0); err != nil {
		return err
	}
	if r.Body, err = p.parseBody(f); err != nil {
		return p.errorf("%v", err)
	}
	return nil
}

// streamValid reports whether s describes a stream the game takes.
func streamValid(s game.Stream) bool {
	return s.Grid >= 0 && s.Lead >= 0 && s.LeadMax >= 0 && s.Target >= 1 && s.Check >= 1
}

// parseTool parses the power, rate, radius and, in version 7, shape fields
// of the tool line.
func parseTool(f []string) (game.Tool, error) {
	power, perr := strconv.ParseUint(f[0], 10, 8)
	rate, rerr := strconv.ParseUint(f[1], 10, 32)
	radius, aerr := strconv.Atoi(f[2])
	shape, sok := game.ShapeCircle, true
	if len(f) > 3 {
		shape, sok = parseShape(f[3])
	}
	if perr != nil || rerr != nil || aerr != nil || !sok || rate == 0 || !brushValid(radius, shape) {
		return game.Tool{}, fmt.Errorf("tool %s: want a power of 0 to 255, a rate of 1 or more, a radius of 0 to %d and a shape, %s or %s",
			strings.Join(f, " "), game.MaxRadius, game.ShapeCircle, game.ShapeSquare)
	}
	return game.Tool{Power: uint8(power), Rate: uint32(rate), Radius: radius, Shape: shape}, nil
}

// parseShape parses a brush's shape.
func parseShape(s string) (game.Shape, bool) {
	for _, shape := range [...]game.Shape{game.ShapeCircle, game.ShapeSquare} {
		if s == shape.String() {
			return shape, true
		}
	}
	return 0, false
}

// parseBody parses the fields of a body tool: none, or its material, its
// size and, in version 7, its angle and velocity.
func (p *parser) parseBody(f []string) (game.BodyTool, error) {
	if len(f) == 1 && f[0] == bodyNone {
		return game.BodyTool{}, nil
	}
	numbers := 5
	if p.version == oldVersion {
		numbers = 2
	}
	if len(f) == numbers+1 {
		var v [5]int
		ok := true
		for i := range numbers {
			n, err := strconv.Atoi(f[i+1])
			v[i], ok = n, ok && err == nil
		}
		t := game.BodyTool{Material: f[0], Width: v[0], Height: v[1], Angle: v[2], VX: v[3], VY: v[4]}
		if ok && bodyValid(&t) {
			return t, nil
		}
	}
	return game.BodyTool{}, fmt.Errorf("body %s: %s", strings.Join(f, " "), bodyWant)
}

// inputs reads the input lines into dst, which holds one input per tick.
func (p *parser) inputs(dst []game.Input) error {
	// cur is the input in effect from tick from on; last is the last tick read.
	var cur game.Input
	from, last := 0, -1
	for {
		s, ok := p.next()
		if !ok {
			break
		}
		f := strings.Fields(s)
		if len(f) < 7 || len(f) > 7 && p.version == oldVersion {
			return p.errorf("want %q, got %q", "<tick> <x> <y> <buttons> <slot> <camera x> <camera y> [<changes>]", s)
		}
		tick, err := strconv.Atoi(f[0])
		if err != nil || tick <= last || tick >= len(dst) {
			return p.errorf("tick %q must increase from line to line and be below %d", f[0], len(dst))
		}
		in, err := parseInput(f[1:7])
		if err != nil {
			return p.errorf("%v", err)
		}
		if in.Change, err = p.parseChange(f[7:]); err != nil {
			return p.errorf("%v", err)
		}
		fill(dst[from:tick], &cur)
		cur, from, last = in, tick, tick
	}
	fill(dst[from:], &cur)
	return nil
}

// parseInput parses the x, y, buttons, slot and camera fields of an input
// line.
func parseInput(f []string) (game.Input, error) {
	var in game.Input
	x, xerr := strconv.Atoi(f[0])
	y, yerr := strconv.Atoi(f[1])
	if xerr != nil || yerr != nil {
		return in, fmt.Errorf("cursor %s %s is not two numbers", f[0], f[1])
	}
	in.CursorX, in.CursorY = x, y
	if err := parseButtons(f[2], &in); err != nil {
		return in, err
	}
	slot, err := strconv.Atoi(f[3])
	if err != nil || slot < 0 {
		return in, fmt.Errorf("slot %q is not a number of 0 or more", f[3])
	}
	in.Select = slot
	cx, cxerr := strconv.Atoi(f[4])
	cy, cyerr := strconv.Atoi(f[5])
	if cxerr != nil || cyerr != nil {
		return in, fmt.Errorf("camera %s %s is not two numbers", f[4], f[5])
	}
	in.CameraX, in.CameraY = cx, cy
	return in, nil
}

// parseChange parses the changes at the end of an input line: brush, mode
// and body, in that order, each at most once.
func (p *parser) parseChange(f []string) (game.Change, error) {
	var c game.Change
	next := 0
	for len(f) > 0 {
		k := slices.Index(changeKeys[:], f[0])
		if k < next {
			return c, fmt.Errorf("changes %q: want %s, %s and %s in that order, each at most once",
				strings.Join(f, " "), changeKeys[0], changeKeys[1], changeKeys[2])
		}
		next = k + 1
		var err error
		switch k {
		case 0:
			f, err = parseBrush(f[1:], &c)
		case 1:
			f, err = parseMode(f[1:], &c)
		default:
			// The body tool's words run to the end of the line.
			if c.Body, err = p.parseBody(f[1:]); err == nil {
				c.What |= game.ChangeBody
			}
			f = nil
		}
		if err != nil {
			return c, err
		}
	}
	return c, nil
}

// parseBrush parses the radius and shape of a brush change into c and
// returns the words after them.
func parseBrush(f []string, c *game.Change) ([]string, error) {
	if len(f) >= 2 {
		radius, err := strconv.Atoi(f[0])
		shape, ok := parseShape(f[1])
		if err == nil && ok && brushValid(radius, shape) {
			c.What |= game.ChangeBrush
			c.Radius, c.Shape = radius, shape
			return f[2:], nil
		}
	}
	return nil, fmt.Errorf("brush %s: want a radius of 0 to %d and a shape, %s or %s",
		strings.Join(f, " "), game.MaxRadius, game.ShapeCircle, game.ShapeSquare)
}

// parseMode parses the mode of a mode change into c and returns the words
// after it.
func parseMode(f []string, c *game.Change) ([]string, error) {
	if len(f) == 0 || f[0] != modePlay && f[0] != modeCreative {
		return nil, fmt.Errorf("mode %s: want %s or %s", strings.Join(f, " "), modePlay, modeCreative)
	}
	c.What |= game.ChangeMode
	c.Creative = f[0] == modeCreative
	return f[1:], nil
}

func fill(dst []game.Input, in *game.Input) {
	for i := range dst {
		dst[i] = *in
	}
}
