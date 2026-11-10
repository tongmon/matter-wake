// Package material defines the kinds of matter a cell can hold and loads
// their definitions from data.
//
// Code never branches on a material's name: behavior comes from its Kind
// and its numeric properties, so a new material is a data change.
package material

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tongmon/matter-wake/internal/fixed"
)

// ID identifies a material inside one Table. ID 0 is always the empty
// material, so the zero value of a cell is empty space.
type ID uint16

// Air is the ID of the empty material every table starts with.
const Air ID = 0

// MaxMaterials is how many materials a table can hold.
const MaxMaterials = 1 << 16

// Kind is how a material moves.
type Kind uint8

// The kinds of material.
const (
	// Empty is space that anything may move into.
	Empty Kind = iota
	// Static never moves on its own, like rock.
	Static
	// Powder falls and piles up, like sand.
	Powder
	// Liquid falls and spreads sideways, like water.
	Liquid
	// Solid is the matter of rigid bodies, like wood. Its cells are the
	// pixels of bodies: the bodies move them, the cell rules never do.
	Solid
)

var kindNames = [...]string{
	Empty:  "empty",
	Static: "static",
	Powder: "powder",
	Liquid: "liquid",
	Solid:  "solid",
}

// Diggable reports whether a tool may dig materials of the kind: powders,
// statics and the solids of rigid bodies, whose pixels it digs out of
// their images. Liquids are gathered by another tool, and empty space
// holds nothing to dig.
func (k Kind) Diggable() bool {
	return k == Static || k == Powder || k == Solid
}

// String returns the name used for the kind in data files.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", k)
}

// UnmarshalText parses a kind name from a data file.
func (k *Kind) UnmarshalText(text []byte) error {
	for i, name := range kindNames {
		if string(text) == name {
			*k = Kind(i)
			return nil
		}
	}
	return fmt.Errorf("unknown kind %q (want one of %s)", text, strings.Join(kindNames[:], ", "))
}

// Anchor says which piece of a solid's static body stays static when the
// body breaks apart; the other pieces fall. A body made of terrain whose
// cells turn into the solid (Def.Body) is static only if the solid has an
// anchor. Noita's solid_static_type picks the same pieces (C136).
type Anchor uint8

// The anchors.
const (
	// NoAnchor: the solid's bodies are never static.
	NoAnchor Anchor = iota
	// AnchorTop keeps the piece that reaches highest, so what hangs below
	// a cut falls, as Noita's ice does.
	AnchorTop
	// AnchorBottom keeps the piece that reaches lowest, so what stands on
	// a cut falls over, as Noita's wood does.
	AnchorBottom
	// AnchorLargest keeps the piece with the most pixels.
	AnchorLargest
)

var anchorNames = [...]string{
	NoAnchor:      "",
	AnchorTop:     "top",
	AnchorBottom:  "bottom",
	AnchorLargest: "largest",
}

// String returns the name used for the anchor in data files.
func (a Anchor) String() string {
	if int(a) < len(anchorNames) {
		return anchorNames[a]
	}
	return fmt.Sprintf("Anchor(%d)", a)
}

// UnmarshalText parses an anchor name from a data file.
func (a *Anchor) UnmarshalText(text []byte) error {
	for i, name := range anchorNames {
		if i > 0 && string(text) == name {
			*a = Anchor(i)
			return nil
		}
	}
	return fmt.Errorf("unknown anchor %q (want one of %s)", text, strings.Join(anchorNames[1:], ", "))
}

// Color is an sRGB color with alpha.
type Color struct {
	R, G, B, A uint8
}

// UnmarshalText parses "#rrggbb" or "#rrggbbaa".
func (c *Color) UnmarshalText(text []byte) error {
	s := string(text)
	var n int
	var err error
	switch len(s) {
	case 7:
		n, err = fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B)
		c.A = 0xff
	case 9:
		n, err = fmt.Sscanf(s, "#%02x%02x%02x%02x", &c.R, &c.G, &c.B, &c.A)
	default:
		return fmt.Errorf("color %q is not #rrggbb or #rrggbbaa", s)
	}
	if err != nil || n < 3 {
		return fmt.Errorf("color %q is not #rrggbb or #rrggbbaa", s)
	}
	return nil
}

// Def is one material as written in the data file.
type Def struct {
	// Name is unique within a table.
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Density decides what sinks through what: a moving cell may sink into a
	// liquid of lower density. A solid's density also sets the mass of its
	// bodies (rigid package).
	Density uint8 `json:"density"`

	// FallAccel is how much a falling cell's speed grows each tick, in
	// cells per tick. Powders and liquids only.
	FallAccel fixed.Q8 `json:"fallAccel"`
	// SinkChance is the percent chance, each time it tries, that a moving
	// cell sinks into a lighter liquid. Powders and liquids only.
	SinkChance uint8 `json:"sinkChance"`
	// SlideChance is the percent chance each tick that a powder blocked
	// below slides down diagonally; it sets how steep a heap stands.
	// Powders only (liquids always slide).
	SlideChance uint8 `json:"slideChance"`
	// FlowSpeed is the sideways push a liquid blocked below gains each
	// tick. It keeps its direction from tick to tick. Liquids only.
	FlowSpeed fixed.Q8 `json:"flowSpeed"`
	// FlowDamping scales the push after each cell the liquid flows, 0..1.
	// Liquids only.
	FlowDamping fixed.Q8 `json:"flowDamping"`
	// Dispersion is the most cells a liquid flows sideways in one tick.
	// Liquids only.
	Dispersion uint8 `json:"dispersion"`

	// Hardness is the grade of tool it takes to dig the material: a tool
	// digs it only if the tool's power is at least this. Diggable kinds
	// only: powders, statics and solids.
	Hardness uint8 `json:"hardness"`
	// HP is how much digging one cell takes: the cell is dug out once the
	// damage dealt to it reaches HP, so 0 digs it at the first touch.
	// Diggable kinds only.
	HP uint32 `json:"hp"`

	// Friction is how much a solid's bodies grip what they touch, as
	// Box2D's Coulomb coefficient, usually 0 to 1. Solids only.
	Friction fixed.Q8 `json:"friction"`
	// Restitution is how much of its speed a solid's body keeps when it
	// bounces, 0..1. Solids only.
	Restitution fixed.Q8 `json:"restitution"`
	// Buoyancy is how strongly liquid under a solid's body holds it up:
	// 1 cancels gravity and the body's speed, less lets it sink, more
	// lifts it. 0 or more. Solids only.
	Buoyancy fixed.Q8 `json:"buoyancy"`

	// Body names the solid a static material's cells turn into, as one
	// body, once a cell of theirs is destroyed: the cells of the material
	// 4-connected to it (rigid package, C136). Empty for none. Statics
	// only.
	Body string `json:"body,omitempty"`
	// Anchor says which piece of a static body of the solid stays static
	// when it breaks apart. Solids only.
	Anchor Anchor `json:"anchor,omitempty"`
	// Shatter names the powder a body of the solid turns into, whole, once
	// it loses a pixel, as Noita's glass does (C136). Empty for none.
	// Solids only.
	Shatter string `json:"shatter,omitempty"`

	// Color is the base color; each cell varies it by its tone.
	Color Color `json:"color"`
	// ToneSpread is how much a cell's tone darkens the base color, 0..255.
	ToneSpread uint8 `json:"toneSpread"`
}

// Table is a validated, ordered set of materials. Index i holds ID(i).
type Table struct {
	defs   []Def
	byName map[string]ID
	// links holds, by material, the materials its Body and Shatter name,
	// Air for none.
	links []link
}

// link is what a material's names of other materials resolve to.
type link struct {
	body, shatter ID
}

type file struct {
	Materials []Def `json:"materials"`
}

// Parse reads a material table from JSON. Unknown fields and trailing data
// are errors, so a typo cannot silently zero a value.
func Parse(r io.Reader) (*Table, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("material: decode: %w", err)
	}
	// Decoder.More only looks for another element of an array or object, so
	// a stray "]" or "}" would pass it. Require the end of the input.
	var rest json.RawMessage
	if err := dec.Decode(&rest); !errors.Is(err, io.EOF) {
		return nil, errors.New("material: trailing data after the table")
	}
	return NewTable(f.Materials)
}

// ParseBytes is Parse on a byte slice.
func ParseBytes(data []byte) (*Table, error) {
	return Parse(bytes.NewReader(data))
}

// NewTable validates defs and builds a table from them.
func NewTable(defs []Def) (*Table, error) {
	if len(defs) == 0 {
		return nil, errors.New("material: the table is empty")
	}
	if len(defs) > MaxMaterials {
		return nil, fmt.Errorf("material: %d materials, at most %d", len(defs), MaxMaterials)
	}
	if defs[0].Kind != Empty {
		return nil, fmt.Errorf("material: the first material %q must be kind empty", defs[0].Name)
	}
	t := &Table{defs: make([]Def, len(defs)), byName: make(map[string]ID, len(defs))}
	copy(t.defs, defs)
	for i := range t.defs {
		d := &t.defs[i]
		if err := validate(d); err != nil {
			return nil, fmt.Errorf("material %d: %w", i, err)
		}
		if _, dup := t.byName[d.Name]; dup {
			return nil, fmt.Errorf("material %d: duplicate name %q", i, d.Name)
		}
		t.byName[d.Name] = ID(i)
	}
	t.links = make([]link, len(defs))
	for i := range t.defs {
		d := &t.defs[i]
		l, err := t.resolve(d)
		if err != nil {
			return nil, fmt.Errorf("material %d: %q: %w", i, d.Name, err)
		}
		t.links[i] = l
	}
	return t, nil
}

// resolve finds the materials d names, and checks their kinds: a static
// turns into a solid's body, and a solid's body shatters into a powder.
func (t *Table) resolve(d *Def) (link, error) {
	var l link
	if d.Body != "" {
		id, ok := t.byName[d.Body]
		if !ok || t.defs[id].Kind != Solid {
			return l, fmt.Errorf("body %q is not a solid of the table", d.Body)
		}
		l.body = id
	}
	if d.Shatter != "" {
		id, ok := t.byName[d.Shatter]
		if !ok || t.defs[id].Kind != Powder {
			return l, fmt.Errorf("shatter %q is not a powder of the table", d.Shatter)
		}
		l.shatter = id
	}
	return l, nil
}

func validate(d *Def) error {
	if d.Name == "" {
		return errors.New("missing name")
	}
	if int(d.Kind) >= len(kindNames) {
		return fmt.Errorf("%q: invalid kind %d", d.Name, d.Kind)
	}
	if err := validateMotion(d); err != nil {
		return fmt.Errorf("%q: %w", d.Name, err)
	}
	if !d.Kind.Diggable() && (d.Hardness != 0 || d.HP != 0) {
		return fmt.Errorf("%q: only powders, statics and solids may have hardness or hp", d.Name)
	}
	if err := validateSolid(d); err != nil {
		return fmt.Errorf("%q: %w", d.Name, err)
	}
	return nil
}

// validateSolid checks the values only solids have: a body needs mass, so
// a solid needs a density. It checks what bodies come of too: only a
// static turns into one, and only a solid's has an anchor or shatters.
// NewTable checks the materials they name.
func validateSolid(d *Def) error {
	switch {
	case d.Kind != Solid && (d.Friction != 0 || d.Restitution != 0 || d.Buoyancy != 0):
		return errors.New("only solids may have friction, restitution or buoyancy")
	case d.Kind == Solid && d.Density == 0:
		return errors.New("a solid needs a density: it sets its bodies' mass")
	case d.Friction < 0 || d.Restitution < 0 || d.Restitution > fixed.One:
		return fmt.Errorf("friction %v must not be negative and restitution %v must be in [0, 1]", d.Friction, d.Restitution)
	case d.Buoyancy < 0:
		return fmt.Errorf("buoyancy %v must not be negative", d.Buoyancy)
	case d.Kind != Static && d.Body != "":
		return errors.New("only statics may turn into a body")
	case d.Kind != Solid && (d.Anchor != NoAnchor || d.Shatter != ""):
		return errors.New("only solids may have an anchor or shatter")
	case int(d.Anchor) >= len(anchorNames):
		return fmt.Errorf("invalid anchor %d", d.Anchor)
	}
	return nil
}

// Limits on motion values. They keep one cell update within the reach the
// parallel tick allows (world.MaxReach).
var (
	maxFallAccel = fixed.FromInt(4)
	maxFlowSpeed = fixed.FromInt(8)
)

func validateMotion(d *Def) error {
	moving := d.Kind == Powder || d.Kind == Liquid
	switch {
	case !moving && (d.FallAccel != 0 || d.SinkChance != 0):
		return errors.New("only powders and liquids may have fallAccel or sinkChance")
	case d.Kind != Powder && d.SlideChance != 0:
		return errors.New("only powders may have slideChance")
	case d.Kind != Liquid && (d.FlowSpeed != 0 || d.FlowDamping != 0 || d.Dispersion != 0):
		return errors.New("only liquids may have flowSpeed, flowDamping or dispersion")
	case d.FallAccel < 0 || d.FallAccel > maxFallAccel:
		return fmt.Errorf("fallAccel %v is outside [0, %v]", d.FallAccel, maxFallAccel)
	case d.SinkChance > 100 || d.SlideChance > 100:
		return errors.New("sinkChance and slideChance are percents, 0..100")
	case d.FlowSpeed < 0 || d.FlowSpeed > maxFlowSpeed:
		return fmt.Errorf("flowSpeed %v is outside [0, %v]", d.FlowSpeed, maxFlowSpeed)
	case d.FlowDamping < 0 || d.FlowDamping > fixed.One:
		return fmt.Errorf("flowDamping %v is outside [0, 1]", d.FlowDamping)
	}
	return nil
}

// Len returns the number of materials.
func (t *Table) Len() int {
	return len(t.defs)
}

// Def returns the definition of id. It panics if id is not in the table.
func (t *Table) Def(id ID) Def {
	return t.defs[id]
}

// BodyOf returns the solid that cells of id turn into, as a body, once a
// cell of theirs is destroyed (Def.Body), or Air if they do not.
func (t *Table) BodyOf(id ID) ID {
	return t.links[id].body
}

// ShatterOf returns the powder a body of id turns into once it loses a
// pixel (Def.Shatter), or Air if it does not.
func (t *Table) ShatterOf(id ID) ID {
	return t.links[id].shatter
}

// Lookup returns the ID of the material called name.
func (t *Table) Lookup(name string) (ID, bool) {
	id, ok := t.byName[name]
	return id, ok
}

// Fingerprint returns 16 hex digits that identify the table's names and
// values, so a replay can tell whether it plays back with the materials it
// was recorded with. It covers every field, colors included.
func (t *Table) Fingerprint() string {
	data, err := json.Marshal(t.defs)
	if err != nil {
		panic(fmt.Sprintf("material: marshal: %v", err)) // a Def is plain data
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// MustLookup is Lookup for names the program itself relies on. It panics
// if the material is missing, which is a data error caught by tests.
func (t *Table) MustLookup(name string) ID {
	id, ok := t.byName[name]
	if !ok {
		panic(fmt.Sprintf("material: no material named %q", name))
	}
	return id
}
