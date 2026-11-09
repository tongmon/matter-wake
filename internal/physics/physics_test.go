package physics

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	box2dDir = "../../third_party/box2d"
	box2dSrc = box2dDir + "/src"
)

func wrapperFor(source string) string {
	return "// Compiles the vendored Box2D source of the same name (third_party/README.md).\n" +
		"#include \"box2d_fp.h\"\n" +
		"#include \"../../third_party/box2d/src/" + source + "\"\n"
}

// TestEveryBox2DSourceIsCompiled checks that each vendored Box2D source has
// its wrapper here, so cgo compiles it, and nothing else does. After an
// update of Box2D, add or remove wrappers until it passes.
func TestEveryBox2DSourceIsCompiled(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join(box2dSrc, "*.c"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatalf("no Box2D sources in %s", box2dSrc)
	}
	want := map[string]bool{}
	for _, src := range sources {
		name := "box2d_" + filepath.Base(src)
		want[name] = true
		got, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("%s has no wrapper: add %s containing\n%s", src, name, wrapperFor(filepath.Base(src)))
			continue
		}
		if strings.ReplaceAll(string(got), "\r", "") != wrapperFor(filepath.Base(src)) {
			t.Errorf("%s is not\n%s", name, wrapperFor(filepath.Base(src)))
		}
	}
	wrappers, err := filepath.Glob("box2d_*.c")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range wrappers {
		if !want[name] {
			t.Errorf("%s wraps a Box2D source that is gone; remove it", name)
		}
	}
}

// TestBox2DSumIsCurrent checks that box2dSum matches the vendored Box2D
// files. Go's build cache does not notice a change to a file that a C file
// includes from outside this folder, so after a Box2D update the package
// would keep the old compiled engine until box2dSum changes (Codex review,
// 2026-11-07).
func TestBox2DSumIsCurrent(t *testing.T) {
	got, err := vendoredSum(box2dDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != box2dSum {
		t.Fatalf("third_party/box2d changed: set box2dSum in box2d_sum.go to %q so Go rebuilds Box2D, then rerun", got)
	}
}

// vendoredSum hashes every file under dir: its slash path, then its content
// with carriage returns dropped, so a checkout's line endings do not count.
func vendoredSum(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data = bytes.ReplaceAll(data, []byte("\r"), nil)
		_, _ = fmt.Fprintf(h, "%s\n%d\n", filepath.ToSlash(rel), len(data)) // a hash never fails to write
		_, _ = h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

// TestCompiledWithoutContraction checks that box2d_fp.h stops the C compiler
// from fusing a multiply and an add, which would make Box2D's results differ
// between machines.
func TestCompiledWithoutContraction(t *testing.T) {
	if !probeCanFuse() {
		t.Skip("this CPU has no FMA instructions, so nothing can be fused")
	}
	// a*a is 1 + 2^-12 + 2^-26 exactly. Rounded to float32 it is 1 + 2^-12,
	// so a*a + c is 0, unless the two are fused into one rounding, which
	// keeps 2^-26.
	const a, c = float32(1 + 1.0/8192), -float32(1 + 1.0/4096)
	if got := probeGuarded(a, a, c); got != 0 {
		t.Fatalf("a*b + c under box2d_fp.h = %g, want 0: the compiler fused it", got)
	}
	if got := probeFree(a, a, c); got == 0 {
		t.Log("this compiler does not fuse even without box2d_fp.h, so the check above proves nothing here")
	} else {
		t.Logf("without box2d_fp.h this compiler fuses: a*b + c = %g", got)
	}
}

func newTestWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld(Config{Gravity: Vec2{0, -10}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w
}

// TestBoxComesToRestOnTheGround drops a box onto static ground and checks
// that it lands where its size says and stays upright.
func TestBoxComesToRestOnTheGround(t *testing.T) {
	w := newTestWorld(t)
	if _, err := w.AddStaticBox(Box{Center: Vec2{0, -1}, HalfW: 10, HalfH: 1, Friction: 0.6}); err != nil {
		t.Fatal(err)
	}
	box, err := w.AddDynamicBox(Box{Center: Vec2{2, 4}, HalfW: 0.5, HalfH: 0.25, Density: 1, Friction: 0.6})
	if err != nil {
		t.Fatal(err)
	}
	for range 180 {
		w.Step(dt, subSteps)
	}
	p := w.Position(box)
	// Box2D lets resting shapes overlap or part by up to a linear slop
	// (0.5 cm), and the box slides a few millimeters as it lands.
	if math.Abs(float64(p.X-2)) > 0.01 || math.Abs(float64(p.Y-0.25)) > 0.01 {
		t.Errorf("box rests at %v, want (2, 0.25)", p)
	}
	if a := w.Angle(box); math.Abs(float64(a)) > 1e-3 {
		t.Errorf("box angle %v, want 0", a)
	}
	if q := w.Rotation(box); math.Abs(float64(q.C-1)) > 1e-6 || math.Abs(float64(q.S)) > 1e-3 {
		t.Errorf("box rotation %v, want (1, 0)", q)
	}
	if n := w.awakeBodies(); n != 0 {
		t.Errorf("%d bodies awake after 3 s at rest, want 0", n)
	}
}

func TestStartingAngle(t *testing.T) {
	w := newTestWorld(t)
	box, err := w.AddDynamicBox(Box{Center: Vec2{0, 10}, Angle: 0.5, HalfW: 1, HalfH: 1, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a := w.Angle(box); math.Abs(float64(a-0.5)) > 1e-3 {
		t.Errorf("angle %v, want 0.5", a)
	}
	q := w.Rotation(box)
	if math.Abs(float64(q.C)-math.Cos(0.5)) > 1e-3 || math.Abs(float64(q.S)-math.Sin(0.5)) > 1e-3 {
		t.Errorf("rotation %v, want cos and sin of 0.5", q)
	}
}

func TestInvalidInputIsAnError(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	if _, err := NewWorld(Config{Gravity: Vec2{nan, 0}}); err == nil {
		t.Error("NewWorld accepted NaN gravity")
	}
	w := newTestWorld(t)
	good := Box{HalfW: 1, HalfH: 1, Density: 1}
	for _, tc := range []struct {
		name string
		edit func(*Box)
	}{
		{"NaN center", func(b *Box) { b.Center.X = nan }},
		{"infinite angle", func(b *Box) { b.Angle = inf }},
		{"zero width", func(b *Box) { b.HalfW = 0 }},
		{"negative height", func(b *Box) { b.HalfH = -1 }},
		{"negative friction", func(b *Box) { b.Friction = -0.5 }},
		{"zero density", func(b *Box) { b.Density = 0 }},
		{"NaN density", func(b *Box) { b.Density = nan }},
	} {
		b := good
		tc.edit(&b)
		if body, err := w.AddDynamicBox(b); err == nil {
			t.Errorf("%s: AddDynamicBox(%+v) = %#x, want an error", tc.name, b, uint64(body))
		}
	}
	if _, err := w.AddStaticBox(Box{HalfW: 1, HalfH: 1}); err != nil {
		t.Errorf("a static box needs no density: %v", err)
	}
}

func TestMisuseIsAProgrammerError(t *testing.T) {
	a, b := newTestWorld(t), newTestWorld(t)
	body, err := a.AddDynamicBox(Box{HalfW: 1, HalfH: 1, Density: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"zero body", func() { a.Position(0) }},
		{"body of another world", func() { b.Position(body) }},
		{"zero time step", func() { a.Step(0, subSteps) }},
		{"no sub-steps", func() { a.Step(dt, 0) }},
	} {
		if !panics(tc.call) {
			t.Errorf("%s: no panic", tc.name)
		}
	}
	c, err := NewWorld(Config{})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close() // closing twice is fine
	if !panics(func() { c.Step(dt, subSteps) }) {
		t.Error("Step on a closed world did not panic")
	}
}

func panics(f func()) (did bool) {
	defer func() { did = recover() != nil }()
	f()
	return false
}

func TestBodyRoundTrips(t *testing.T) {
	w := newTestWorld(t)
	for range 3 {
		body, err := w.AddDynamicBox(Box{HalfW: 1, HalfH: 1, Density: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got := bodyOf(body.id()); got != body {
			t.Errorf("bodyOf(%#x.id()) = %#x", uint64(body), uint64(got))
		}
	}
}

// TestStepAndReadDoNotAllocate checks the per-tick calls: Step, and reading
// every body's transform.
// Making terrain again, as a chunk's cells change, allocates nothing once
// the buffers have grown: in a big world a chunk near a body may be made
// again every tick. The body's definition used to move to the heap at
// every call, since C gets its address (found with the terrain's bodies,
// 2026-11-09).
func TestRebuildingChainsDoesNotAllocate(t *testing.T) {
	w := newTestWorld(t)
	chains := []Chain{{Points: []Vec2{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, Loop: true}}
	allocs := testing.AllocsPerRun(20, func() {
		b, err := w.AddStaticChains(chains, 0.6)
		if err != nil {
			t.Fatal(err)
		}
		w.DestroyBody(b)
	})
	if allocs != 0 {
		t.Errorf("making a chain body and removing it allocates %v times, want 0", allocs)
	}
}

func TestStepAndReadDoNotAllocate(t *testing.T) {
	w, bodies := newPile(t, false)
	var sink float32
	allocs := testing.AllocsPerRun(20, func() {
		w.Step(dt, subSteps)
		sink += readAll(w, bodies)
		for _, b := range bodies[:10] {
			sink += w.Angle(b)
		}
	})
	if allocs != 0 {
		t.Errorf("a step and its reads allocate %v times, want 0", allocs)
	}
	_ = sink
}
