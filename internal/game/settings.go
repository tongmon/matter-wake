package game

import (
	"errors"
	"fmt"
	"math"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rigid"
)

// Shape is the shape of the tool's brush.
type Shape uint8

const (
	// ShapeCircle covers the cells no farther from the cursor than the
	// radius. It is the zero value.
	ShapeCircle Shape = iota
	// ShapeSquare covers the cells no farther from the cursor than the
	// radius along either axis.
	ShapeSquare
)

// String returns the shape's word in replays: circle or square.
func (s Shape) String() string {
	switch s {
	case ShapeSquare:
		return "square"
	case ShapeCircle:
	}
	return "circle"
}

// MaxRadius is the largest brush radius, so a typo in a hand-written
// replay cannot make every tick scan millions of cells. 🔶
const MaxRadius = 64

// MaxAngle bounds the body tool's angle in degrees either way. 🔶
const MaxAngle = 360

// MaxSpeed is the fastest the body tool may start a body along either
// axis, in cells per second: Box2D's maximum linear speed, 400 m/s.
const MaxSpeed = 4000

// Changes says which settings a Change sets.
type Changes uint8

const (
	// ChangeBrush sets the brush's radius and shape.
	ChangeBrush Changes = 1 << iota
	// ChangeMode sets creative mode on or off.
	ChangeMode
	// ChangeBody sets what the body tool drops.
	ChangeBody
)

// Change changes settings of the session that change what it does, from
// the tick it comes with on: the test dashboard's changes, which a replay
// records with the tick's input. What says which fields it sets; the zero
// Change sets nothing. Setting a value a setting has already changes
// nothing, so a replay may repeat a Change on later ticks.
type Change struct {
	// Body is what the body tool drops, with ChangeBody.
	Body BodyTool
	// Radius and Shape are the brush's, with ChangeBrush. The tool's
	// power and rate stay.
	Radius int
	What   Changes
	Shape  Shape
	// Creative is the mode, with ChangeMode.
	Creative bool
}

// checkTool reports why tool t cannot be the session's.
func checkTool(t Tool) error {
	switch {
	case t.Rate == 0:
		return errors.New("game: the tool deals no damage")
	case t.Radius < 0 || t.Radius > MaxRadius:
		return fmt.Errorf("game: tool radius %d is not 0 to %d", t.Radius, MaxRadius)
	case t.Shape != ShapeCircle && t.Shape != ShapeSquare:
		return fmt.Errorf("game: unknown brush shape %d", t.Shape)
	}
	return nil
}

// bodyMaterial returns the solid of body tool t, which mats holds, or why
// t cannot be the session's. The empty tool has none.
func bodyMaterial(t BodyTool, mats *material.Table) (material.ID, error) {
	if t.Material == "" {
		return material.Air, nil
	}
	id, ok := mats.Lookup(t.Material)
	switch {
	case !ok:
		return 0, fmt.Errorf("game: the body tool names unknown material %q", t.Material)
	case mats.Def(id).Kind != material.Solid:
		return 0, fmt.Errorf("game: the body tool's material %q is not a solid", t.Material)
	case t.Width < 1 || t.Height < 1 || t.Width > rigid.MaxPixels || t.Height > rigid.MaxPixels ||
		t.Width*t.Height > rigid.MaxPixels:
		return 0, fmt.Errorf("game: the body tool's size %dx%d is not positive or over %d pixels", t.Width, t.Height, rigid.MaxPixels)
	case t.Angle < -MaxAngle || t.Angle > MaxAngle:
		return 0, fmt.Errorf("game: the body tool's angle %d° is not %d° to %d°", t.Angle, -MaxAngle, MaxAngle)
	case t.VX < -MaxSpeed || t.VX > MaxSpeed || t.VY < -MaxSpeed || t.VY > MaxSpeed:
		return 0, fmt.Errorf("game: the body tool's velocity %d, %d cells/s is over %d either way", t.VX, t.VY, MaxSpeed)
	}
	return id, nil
}

// apply makes change c, checking all of it first, so a change that fails
// changes nothing.
func (g *Game) apply(c Change) error {
	if c.What == 0 {
		return nil
	}
	tool := g.tool
	if c.What&ChangeBrush != 0 {
		tool.Radius, tool.Shape = c.Radius, c.Shape
		if err := checkTool(tool); err != nil {
			return err
		}
	}
	body := g.bodyID
	if c.What&ChangeBody != 0 {
		id, err := bodyMaterial(c.Body, g.mats)
		if err != nil {
			return err
		}
		body = id
	}
	if c.What&^(ChangeBrush|ChangeMode|ChangeBody) != 0 {
		return fmt.Errorf("game: unknown changes %#x", c.What&^(ChangeBrush|ChangeMode|ChangeBody))
	}
	g.tool = tool
	if c.What&ChangeMode != 0 {
		g.creative = c.Creative
	}
	if c.What&ChangeBody != 0 {
		g.bodyTool, g.bodyID = c.Body, body
	}
	return nil
}

// covers reports whether the brush of radius r and shape s covers the cell
// (dx, dy) from its middle.
func covers(s Shape, r, dx, dy int) bool {
	return s == ShapeSquare || dx*dx+dy*dy <= r*r
}

// dropBody drops a body of the body tool centred on (cx, cy), turned and
// moving as the tool says. It reports whether the body fit.
func (g *Game) dropBody(cx, cy int) (bool, error) {
	t := g.bodyTool
	// One product, converted, so nothing fuses it (docs/pitfalls.md).
	angle := float32(float32(t.Angle) * float32(math.Pi/180))
	ok, err := g.bodies.Drop(g.bodyID, cx-t.Width/2, cy-t.Height/2, t.Width, t.Height, angle,
		float32(t.VX), float32(t.VY))
	if err != nil {
		return false, fmt.Errorf("game: %w", err)
	}
	return ok, nil
}

// Body returns what the body tool drops; its Material is empty for no
// tool.
func (g *Game) Body() BodyTool {
	return g.bodyTool
}

// Turned adds to n[id], for every material id, how many cells or pixels of
// it turned into another material, less how many turned into it: terrain
// turned into a body, and a body turned into powder (rigid.System.Turned).
// Its sum over the materials is zero. n must have an entry for every
// material.
func (g *Game) Turned(n []int) {
	g.bodies.Turned(n)
}

// Made adds to n[id], for every material id, how many cells of it
// creative mode placed: it makes them from nothing. So each material's
// cells, plus what the queue and the inventory hold, are what the session
// started with plus what creative mode made (and, for a solid, what the
// body tool dropped). n must have an entry for every material.
func (g *Game) Made(n []int) {
	for id, m := range g.made {
		n[id] += m
	}
}
