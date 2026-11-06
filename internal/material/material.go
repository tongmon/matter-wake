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
)

var kindNames = [...]string{
	Empty:  "empty",
	Static: "static",
	Powder: "powder",
	Liquid: "liquid",
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
	// liquid of lower density.
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

	// Color is the base color; each cell varies it by its tone.
	Color Color `json:"color"`
	// ToneSpread is how much a cell's tone darkens the base color, 0..255.
	ToneSpread uint8 `json:"toneSpread"`
}

// Table is a validated, ordered set of materials. Index i holds ID(i).
type Table struct {
	defs   []Def
	byName map[string]ID
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
	for i, d := range defs {
		if err := validate(d); err != nil {
			return nil, fmt.Errorf("material %d: %w", i, err)
		}
		if _, dup := t.byName[d.Name]; dup {
			return nil, fmt.Errorf("material %d: duplicate name %q", i, d.Name)
		}
		t.byName[d.Name] = ID(i)
	}
	return t, nil
}

func validate(d Def) error {
	if d.Name == "" {
		return errors.New("missing name")
	}
	if int(d.Kind) >= len(kindNames) {
		return fmt.Errorf("%q: invalid kind %d", d.Name, d.Kind)
	}
	if err := validateMotion(d); err != nil {
		return fmt.Errorf("%q: %w", d.Name, err)
	}
	return nil
}

// Limits on motion values. They keep one cell update within the reach the
// parallel tick allows (world.MaxReach).
var (
	maxFallAccel = fixed.FromInt(4)
	maxFlowSpeed = fixed.FromInt(8)
)

func validateMotion(d Def) error {
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
