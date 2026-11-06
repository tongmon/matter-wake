package physics

import (
	"fmt"
	"hash/fnv"
	"math"
	"runtime"
	"strings"
	"testing"
)

const (
	// dt is one game tick (game.Hz is 60).
	dt = float32(1.0 / 60)
	// subSteps is Box2D's recommendation at 60 Hz.
	subSteps = 4
	// pileBodies is the number of dynamic boxes in the pile scene: Noita's
	// heaviest load of rigid bodies (roadmap.md stage 4).
	pileBodies = 300
)

// newPile makes the scene the determinism golden and the benchmark use: a
// container 38 m wide and 300 boxes of three sizes and seven angles above it,
// in 20 columns and 15 rows, which fall and pile up. sse2 forces the 4-wide
// contact solver where AVX2 is available.
//
// Every number is an integer times a power of two, so it is exact in float32
// and no Go rounding can differ between machines.
func newPile(tb testing.TB, sse2 bool) (*World, []Body) {
	tb.Helper()
	w, err := NewWorld(Config{Gravity: Vec2{0, -10}})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(w.Close)
	w.useSSE2(sse2)
	walls := []Box{
		{Center: Vec2{0, -1}, HalfW: 20, HalfH: 1, Friction: 0.625},   // ground, top at y = 0
		{Center: Vec2{-20, 20}, HalfW: 1, HalfH: 20, Friction: 0.625}, // inner side at x = -19
		{Center: Vec2{20, 20}, HalfW: 1, HalfH: 20, Friction: 0.625},  // inner side at x = 19
	}
	for _, b := range walls {
		if _, err := w.AddStaticBox(b); err != nil {
			tb.Fatal(err)
		}
	}
	bodies := make([]Body, pileBodies)
	for i := range bodies {
		col, row := i%20, i/20
		half := float32(2+i%3) * 0.125 // 0.25, 0.375 or 0.5 m
		b, err := w.AddDynamicBox(Box{
			Center:   Vec2{float32(7*col-66) * 0.25, float32(4+3*row) * 0.5}, // x -16.5 to 16.75, y 2 to 23
			Angle:    float32(i%7) * 0.25,
			HalfW:    half,
			HalfH:    half,
			Density:  1,
			Friction: 0.625,
		})
		if err != nil {
			tb.Fatal(err)
		}
		bodies[i] = b
	}
	return w, bodies
}

// hashBodies hashes the position and rotation bits of every body, in order.
func hashBodies(w *World, bodies []Body) uint64 {
	h := fnv.New64a()
	var buf [16]byte
	for _, b := range bodies {
		p, q := w.Position(b), w.Rotation(b)
		for i, f := range [...]float32{p.X, p.Y, q.C, q.S} {
			bits := math.Float32bits(f)
			buf[4*i], buf[4*i+1], buf[4*i+2], buf[4*i+3] = byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24)
		}
		_, _ = h.Write(buf[:])
	}
	return h.Sum64()
}

// pileCheckpoints are the steps at which pileTrace records the pile.
var pileCheckpoints = []int{0, 1, 10, 60, 120, 300, 600}

// pileTrace runs the pile to the last checkpoint and records, at each one,
// how many bodies are awake and the hash of all of them.
func pileTrace(tb testing.TB, sse2 bool) string {
	tb.Helper()
	w, bodies := newPile(tb, sse2)
	var s strings.Builder
	step := 0
	for _, at := range pileCheckpoints {
		for ; step < at; step++ {
			w.Step(dt, subSteps)
		}
		fmt.Fprintf(&s, "step %d awake %d hash %016x\n", step, w.awakeBodies(), hashBodies(w, bodies))
	}
	return s.String()
}

// simdPath names the contact solver a world uses here unless told to fall
// back to SSE2.
func simdPath() string {
	switch {
	case scalarBuild:
		return "scalar"
	case hasAVX2():
		return "AVX2"
	case runtime.GOARCH == "arm64":
		return "NEON"
	default:
		return "SSE2"
	}
}

// readAll reads every body's transform, as the game will once per tick.
func readAll(w *World, bodies []Body) (sum float32) {
	for _, b := range bodies {
		p, q := w.Position(b), w.Rotation(b)
		sum += p.X + q.S
	}
	return sum
}
