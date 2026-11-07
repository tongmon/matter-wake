// Package physics simulates rigid bodies with Box2D v3.2, which cgo compiles
// from third_party/box2d into the game. It is the only package that uses cgo
// (tools/archtest). It calls Box2D's C API directly; its only C of its own
// silences Box2D's log and fills in a chain's definition.
//
// Box2D works in meters, kilograms and seconds, with angles in radians
// counterclockwise. Which way is down comes from the gravity a World is given.
// How cells map to meters is decided where the grid meets the bodies, not
// here.
//
// A World runs on the goroutine that calls it: it is made with one worker, so
// Box2D starts no threads. A World must not be used by two goroutines at
// once. Box2D keeps its worlds in one global table without a lock, so create
// and close worlds from one goroutine at a time.
//
// Results are deterministic: the same calls in the same order give
// bit-identical bodies on every CPU and operating system. Box2D chooses its
// SIMD path at run time (AVX2 or SSE2 on x86, NEON on arm64) and every path
// gives the same results. That holds only while nothing fuses a multiply and
// an add into one rounding: box2d_fp.h sees to that for the C code, and Go
// code that computes values passed in must convert each product explicitly
// (docs/pitfalls.md).
package physics

/*
#cgo CFLAGS: -std=gnu17 -O2 -DNDEBUG -I${SRCDIR}/../../third_party/box2d/include
#cgo linux LDFLAGS: -lm

#include "box2d_fp.h"
#include "box2d/box2d.h"

static void physicsDropLog( const char* message )
{
	(void)message;
}

// Go cannot take the address of a static C function, so C installs it.
static void physicsSilenceBox2D( void )
{
	b2SetLogCallback( physicsDropLog );
}

// The definition points at the material on C's stack, and at the points
// only for the call: Box2D copies both.
static b2ChainId physicsCreateChain( b2BodyId body, const b2Vec2* points, int count, bool loop, b2Vec2 ghost1,
									 b2Vec2 ghost2, float friction )
{
	b2SurfaceMaterial material = b2DefaultSurfaceMaterial();
	material.friction = friction;
	b2ChainDef def = b2DefaultChainDef();
	def.points = points;
	def.pointCount = count;
	def.isLoop = loop;
	def.ghost1 = ghost1;
	def.ghost2 = ghost2;
	def.materials = &material;
	def.materialCount = 1;
	return b2CreateChain( body, &def );
}
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
)

func init() {
	// Box2D prints rejected input to stdout. Logic must not print, and the
	// checks here turn the same mistakes into errors first.
	C.physicsSilenceBox2D()
}

// Vec2 is a point or a vector in meters.
type Vec2 struct {
	X, Y float32
}

// Rot is a rotation: the cosine and sine of its angle, as Box2D keeps it.
// World.Angle computes the angle itself.
type Rot struct {
	C, S float32
}

// Config sets up a World.
type Config struct {
	// Gravity in meters per second squared, for example {0, -10} for down
	// along -Y.
	Gravity Vec2
}

// World is one Box2D world.
type World struct {
	id     C.b2WorldId
	closed bool
	// points is AddStaticChains's buffer: a chain's points as Box2D takes
	// them, in Go memory that holds no Go pointers.
	points []C.b2Vec2
}

// NewWorld makes a World. Close it when done: its memory is C memory.
func NewWorld(cfg Config) (*World, error) {
	if !finite(cfg.Gravity.X) || !finite(cfg.Gravity.Y) {
		return nil, fmt.Errorf("physics: gravity %v is not finite", cfg.Gravity)
	}
	def := C.b2DefaultWorldDef()
	def.gravity = vec(cfg.Gravity)
	def.workerCount = 1 // single-threaded: Box2D starts no threads
	// Pointers go to C through a variable, not as &def in the call, which
	// cgo turns into code that gocritic flags (docs/pitfalls.md, cgo).
	defp := &def
	id := C.b2CreateWorld(defp)
	if id.index1 == 0 {
		return nil, errors.New("physics: Box2D has no free world (it allows 128 at once)")
	}
	return &World{id: id}, nil
}

// Close frees the world and its bodies. Their Body handles become invalid.
// Closing twice does nothing.
func (w *World) Close() {
	if w.closed {
		return
	}
	C.b2DestroyWorld(w.id)
	w.closed = true
}

// Step advances the world by dt seconds, split into subSteps solver steps.
// Box2D recommends 4 sub-steps at 60 Hz.
func (w *World) Step(dt float32, subSteps int) {
	w.mustBeOpen()
	if !finite(dt) || dt <= 0 || subSteps < 1 {
		panic(fmt.Sprintf("physics: Step(%v, %d) needs a positive time and at least one sub-step", dt, subSteps))
	}
	C.b2World_Step(w.id, C.float(dt), C.int(subSteps))
}

// Box describes a rectangular body.
type Box struct {
	// Center is where the box's center starts, in meters.
	Center Vec2
	// Angle is the box's starting rotation in radians, counterclockwise.
	Angle float32
	// HalfW and HalfH are half the box's width and height, in meters. Both
	// must be positive.
	HalfW, HalfH float32
	// Density in kilograms per square meter. Dynamic boxes need a positive
	// density; static boxes ignore it.
	Density float32
	// Friction is the Coulomb friction coefficient, usually 0 to 1.
	Friction float32
	// Restitution is how much of its speed the box keeps when it bounces,
	// 0 to 1.
	Restitution float32
}

// Body is a handle to a body in a World. It is an integer, safe to copy and
// compare, and valid until its World closes or DestroyBody removes it. The
// zero Body is no body.
type Body uint64

// AddStaticBox adds a box that never moves, such as ground or a wall.
func (w *World) AddStaticBox(b Box) (Body, error) {
	return w.addBox(b, C.b2_staticBody)
}

// AddDynamicBox adds a box that gravity and contacts move.
func (w *World) AddDynamicBox(b Box) (Body, error) {
	if !(b.Density > 0) {
		return 0, fmt.Errorf("physics: dynamic box density %v is not positive", b.Density)
	}
	return w.addBox(b, C.b2_dynamicBody)
}

func (w *World) addBox(b Box, kind C.b2BodyType) (Body, error) {
	w.mustBeOpen()
	// Two statements, not if err := ...; err != nil: in a cgo file gocritic
	// misreads that form (docs/pitfalls.md, cgo).
	err := b.check()
	if err != nil {
		return 0, err
	}
	bodyDef := C.b2DefaultBodyDef()
	bodyDef._type = kind
	bodyDef.position = vec(b.Center)
	bodyDef.rotation = C.b2MakeRot(C.float(b.Angle))
	bodyp := &bodyDef
	id := C.b2CreateBody(w.id, bodyp)
	if id.index1 == 0 {
		return 0, fmt.Errorf("physics: Box2D rejected the body %+v", b)
	}
	shapeDef := C.b2DefaultShapeDef()
	shapeDef.density = C.float(b.Density)
	shapeDef.material.friction = C.float(b.Friction)
	shapeDef.material.restitution = C.float(b.Restitution)
	polygon := C.b2MakeBox(C.float(b.HalfW), C.float(b.HalfH))
	shapep, polygonp := &shapeDef, &polygon
	shape := C.b2CreatePolygonShape(id, shapep, polygonp)
	if shape.index1 == 0 {
		C.b2DestroyBody(id)
		return 0, fmt.Errorf("physics: Box2D rejected the box shape %+v", b)
	}
	return bodyOf(id), nil
}

func (b Box) check() error {
	for _, f := range [...]float32{b.Center.X, b.Center.Y, b.Angle, b.HalfW, b.HalfH, b.Density, b.Friction, b.Restitution} {
		if !finite(f) {
			return fmt.Errorf("physics: box %+v has a value that is not finite", b)
		}
	}
	if b.HalfW <= 0 || b.HalfH <= 0 {
		return fmt.Errorf("physics: box half size %vx%v is not positive", b.HalfW, b.HalfH)
	}
	if b.Density < 0 || b.Friction < 0 || b.Restitution < 0 {
		return fmt.Errorf("physics: box density %v, friction %v or restitution %v is negative", b.Density, b.Friction, b.Restitution)
	}
	return nil
}

// Chain is a line of segments that collide on one side only: the right of
// the way the line runs, in Box2D's frame. With y pointing down, as in the
// grid, that is the left on screen, so a line that runs clockwise on screen
// around a solid keeps bodies out of it.
type Chain struct {
	// Points are the line's corners in meters. A loop needs 3 or more, an
	// open line 2 or more; neighbors must be more than 5 mm apart.
	Points []Vec2
	// Loop joins the last point back to the first.
	Loop bool
	// Before and After are where an open line goes on past its first and
	// last points, in a neighboring chain. Box2D uses them so a body moving
	// from one chain onto the next does not catch on the joint. A loop
	// ignores them.
	Before, After Vec2
}

// AddStaticChains adds a body that never moves, made of chains, such as the
// outline of a piece of terrain. Every segment has the given friction.
func (w *World) AddStaticChains(chains []Chain, friction float32) (Body, error) {
	w.mustBeOpen()
	err := checkChains(chains, friction)
	if err != nil {
		return 0, err
	}
	bodyDef := C.b2DefaultBodyDef()
	bodyp := &bodyDef
	id := C.b2CreateBody(w.id, bodyp)
	if id.index1 == 0 {
		return 0, errors.New("physics: Box2D rejected a static body")
	}
	for i, c := range chains {
		// Box2D copies the points during the call and keeps no pointer to
		// them.
		w.points = w.points[:0]
		for _, p := range c.Points {
			w.points = append(w.points, vec(p))
		}
		points := &w.points[0]
		chain := C.physicsCreateChain(id, points, C.int(len(c.Points)), C.bool(c.Loop), vec(c.Before), vec(c.After), C.float(friction))
		if chain.index1 == 0 {
			C.b2DestroyBody(id)
			return 0, fmt.Errorf("physics: Box2D rejected chain %d of %d points", i, len(c.Points))
		}
	}
	return bodyOf(id), nil
}

func checkChains(chains []Chain, friction float32) error {
	if !finite(friction) || friction < 0 {
		return fmt.Errorf("physics: chain friction %v is negative or not finite", friction)
	}
	for i, c := range chains {
		least := 2
		if c.Loop {
			least = 3
		}
		if len(c.Points) < least {
			return fmt.Errorf("physics: chain %d has %d points, at least %d", i, len(c.Points), least)
		}
		ok := finiteVec(c.Before) && finiteVec(c.After)
		for _, p := range c.Points {
			ok = ok && finiteVec(p)
		}
		if !ok {
			return fmt.Errorf("physics: chain %d has a point that is not finite", i)
		}
	}
	return nil
}

// DestroyBody removes b and its shapes from the world. The handle becomes
// invalid: Box2D does not check it in a release build, so using it again
// is a programmer error that may crash.
func (w *World) DestroyBody(b Body) {
	C.b2DestroyBody(w.body(b))
}

// Position returns where the body's origin is, in meters. For a box that is
// its center.
func (w *World) Position(b Body) Vec2 {
	p := C.b2Body_GetPosition(w.body(b))
	return Vec2{float32(p.x), float32(p.y)}
}

// Rotation returns the body's rotation.
func (w *World) Rotation(b Body) Rot {
	q := C.b2Body_GetRotation(w.body(b))
	return Rot{float32(q.c), float32(q.s)}
}

// Transform returns where the body's origin is and its rotation, in one
// call.
func (w *World) Transform(b Body) (Vec2, Rot) {
	t := C.b2Body_GetTransform(w.body(b))
	return Vec2{float32(t.p.x), float32(t.p.y)}, Rot{float32(t.q.c), float32(t.q.s)}
}

// Velocity returns the velocity of the body's center of mass in meters per
// second and its angular velocity in radians per second.
func (w *World) Velocity(b Body) (linear Vec2, angular float32) {
	id := w.body(b)
	v := C.b2Body_GetLinearVelocity(id)
	return Vec2{float32(v.x), float32(v.y)}, float32(C.b2Body_GetAngularVelocity(id))
}

// Angle returns the body's angle in radians, in [-pi, pi]. It is computed
// from the rotation with Box2D's own arctangent, which gives the same result
// everywhere, unlike the C library's.
func (w *World) Angle(b Body) float32 {
	return float32(C.b2Rot_GetAngle(C.b2Body_GetRotation(w.body(b))))
}

// body returns the Box2D id of b, panicking if b cannot be one of w's.
func (w *World) body(b Body) C.b2BodyId {
	w.mustBeOpen()
	id := b.id()
	if id.index1 == 0 || uint16(id.world0)+1 != uint16(w.id.index1) {
		panic(fmt.Sprintf("physics: body %#x is not in this world", uint64(b)))
	}
	return id
}

func (w *World) mustBeOpen() {
	if w.closed {
		panic("physics: use of a closed World")
	}
}

// bodyOf packs a Box2D body id into a Body: index in the low 32 bits, then
// the world index, then the generation.
func bodyOf(id C.b2BodyId) Body {
	return Body(uint64(uint32(id.index1)) | uint64(id.world0)<<32 | uint64(id.generation)<<48)
}

func (b Body) id() C.b2BodyId {
	return C.b2BodyId{
		index1:     C.int32_t(int32(uint32(b))),
		world0:     C.uint16_t(uint16(b >> 32)),
		generation: C.uint16_t(uint16(b >> 48)),
	}
}

// useSSE2 makes w use the 4-wide SSE2 contact solver even where AVX2 is
// available, so tests can compare the two paths. It must be called before
// the first Step.
func (w *World) useSSE2(on bool) {
	w.mustBeOpen()
	C.b2World_EnableSSE2Fallback(w.id, C.bool(on))
}

// hasAVX2 reports whether Box2D uses its 8-wide AVX2 contact solver on this
// CPU when not told to fall back. Otherwise it solves 4 contacts at once
// (SSE2 or NEON), or one with SIMD compiled out.
func hasAVX2() bool {
	return bool(C.b2IsAVX2Available())
}

// setSleeping turns sleeping on or off for the whole world. With it off,
// every body stays awake, which is the worst case for a step's time.
func (w *World) setSleeping(on bool) {
	w.mustBeOpen()
	C.b2World_EnableSleeping(w.id, C.bool(on))
}

// Wake wakes the body if it sleeps, so the next Step moves it again. A
// sleeping body does not notice on its own that what held it up is gone.
func (w *World) Wake(b Body) {
	C.b2Body_SetAwake(w.body(b), true)
}

// Awake reports whether the body is awake.
func (w *World) Awake(b Body) bool {
	return bool(C.b2Body_IsAwake(w.body(b)))
}

// setVelocity sets the body's linear velocity, for tests.
func (w *World) setVelocity(b Body, v Vec2) {
	C.b2Body_SetLinearVelocity(w.body(b), vec(v))
}

// awakeBodies returns how many bodies are awake.
func (w *World) awakeBodies() int {
	w.mustBeOpen()
	return int(C.b2World_GetAwakeBodyCount(w.id))
}

func vec(v Vec2) C.b2Vec2 {
	return C.b2Vec2{x: C.float(v.X), y: C.float(v.Y)}
}

func finiteVec(v Vec2) bool {
	return finite(v.X) && finite(v.Y)
}

func finite(f float32) bool {
	return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}
