// Package replay records the inputs of a session and plays them back.
//
// A replay is how a session started and the game.Input of every tick.
// Input is the only thing from outside that changes a session, so playing
// the inputs back from the same start gives the same world.
//
// The format is line-based text, so replays diff well and can be written
// by hand:
//
//	matter-wake-replay 1
//	seed 1
//	size 480 270
//	brush 4
//	slots sand water stone
//	materials 3f2a9c0d1e4b5a67
//	ticks 600
//	20 240 60 - 3
//	21 240 60 P 0
//	50 240 60 - 0
//
// The first line names the format and its version. The header follows in
// this order: the world seed, the world size in cells, the brush radius,
// the materials the slots hold in key order, the fingerprint of the
// material table (material.Table.Fingerprint) and the number of ticks.
//
// Each later line is a tick and the input from that tick on, written only
// where the input changes: the cursor's x and y in cells, the buttons (P
// to place, D to dig, PD for both, - for none) and the slot to select, 0
// to keep the current one. Ticks before the first line have no input.
// Blank lines and lines starting with # are ignored.
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

// Version is the format version this build reads and writes. A change to
// the format raises it, and older files are refused rather than misread.
const Version = 1

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
	BrushRadius   int
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
func New(cfg game.Config) *Replay {
	return &Replay{
		Seed: cfg.Seed, Width: cfg.Width, Height: cfg.Height, BrushRadius: cfg.BrushRadius,
		Slots: slices.Clone(cfg.Slots), Materials: cfg.Materials.Fingerprint(),
	}
}

// Config returns the config that starts the recorded session again, with
// mats and on the given number of workers.
func (r *Replay) Config(mats *material.Table, workers int) game.Config {
	return game.Config{
		Width: r.Width, Height: r.Height, Seed: r.Seed, Workers: workers, Materials: mats,
		Slots: slices.Clone(r.Slots), BrushRadius: r.BrushRadius,
	}
}

// Write encodes r.
func Write(w io.Writer, r *Replay) error {
	if err := r.validate(); err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	// A bufio.Writer keeps its first error and Flush returns it.
	_, _ = fmt.Fprintf(bw, "%s %d\nseed %d\nsize %d %d\nbrush %d\nslots %s\nmaterials %s\nticks %d\n",
		magic, Version, r.Seed, r.Width, r.Height, r.BrushRadius, strings.Join(r.Slots, " "), r.Materials, len(r.Inputs))
	var prev game.Input
	for i, in := range r.Inputs {
		if in != prev {
			_, _ = fmt.Fprintf(bw, "%d %d %d %s %d\n", i, in.CursorX, in.CursorY, buttons(in), in.Select)
			prev = in
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
	case r.BrushRadius < 0:
		return fmt.Errorf("replay: brush radius %d is negative", r.BrushRadius)
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
	for i, in := range r.Inputs {
		if in.Select < 0 {
			return fmt.Errorf("replay: tick %d selects slot %d", i, in.Select)
		}
	}
	return nil
}

// isWord reports whether s is a non-empty run of non-space characters, so
// it reads back as one field.
func isWord(s string) bool {
	return s != "" && !strings.ContainsFunc(s, unicode.IsSpace)
}

func buttons(in game.Input) string {
	switch {
	case in.Place && in.Dig:
		return "PD"
	case in.Place:
		return "P"
	case in.Dig:
		return "D"
	}
	return "-"
}

// Read decodes a replay written by Write or by hand.
func Read(rd io.Reader) (*Replay, error) {
	p := &parser{sc: bufio.NewScanner(rd)}
	if err := p.version(); err != nil {
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

func (p *parser) version() error {
	s, _ := p.next()
	f := strings.Fields(s)
	if len(f) != 2 || f[0] != magic {
		return p.errorf("not a matter-wake replay: want %q, got %q", fmt.Sprintf("%s %d", magic, Version), s)
	}
	v, err := strconv.Atoi(f[1])
	switch {
	case err != nil:
		return p.errorf("bad format version %q", f[1])
	case v != Version:
		return p.errorf("format version %d; this build reads version %d", v, Version)
	}
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
	if f, err = p.field("brush", "brush <radius>", 1); err != nil {
		return 0, err
	}
	if r.BrushRadius, err = strconv.Atoi(f[0]); err != nil || r.BrushRadius < 0 {
		return 0, p.errorf("brush radius %q is not a number of 0 or more", f[0])
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
		if len(f) != 5 {
			return p.errorf("want %q, got %q", "<tick> <x> <y> <buttons> <slot>", s)
		}
		tick, err := strconv.Atoi(f[0])
		if err != nil || tick <= last || tick >= len(dst) {
			return p.errorf("tick %q must increase from line to line and be below %d", f[0], len(dst))
		}
		in, err := parseInput(f[1:])
		if err != nil {
			return p.errorf("%v", err)
		}
		fill(dst[from:tick], cur)
		cur, from, last = in, tick, tick
	}
	fill(dst[from:], cur)
	return nil
}

// parseInput parses the x, y, buttons and slot fields of an input line.
func parseInput(f []string) (game.Input, error) {
	var in game.Input
	x, xerr := strconv.Atoi(f[0])
	y, yerr := strconv.Atoi(f[1])
	if xerr != nil || yerr != nil {
		return in, fmt.Errorf("cursor %s %s is not two numbers", f[0], f[1])
	}
	in.CursorX, in.CursorY = x, y
	switch f[2] {
	case "PD":
		in.Place, in.Dig = true, true
	case "P":
		in.Place = true
	case "D":
		in.Dig = true
	case "-":
	default:
		return in, fmt.Errorf("buttons %q: want P, D, PD or -", f[2])
	}
	slot, err := strconv.Atoi(f[3])
	if err != nil || slot < 0 {
		return in, fmt.Errorf("slot %q is not a number of 0 or more", f[3])
	}
	in.Select = slot
	return in, nil
}

func fill(dst []game.Input, in game.Input) {
	for i := range dst {
		dst[i] = in
	}
}
