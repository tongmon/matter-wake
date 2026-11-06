package physics

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the .golden files")

// TestDeterminismGolden pins how the 300-box pile falls, bit for bit. CI runs
// it on Windows, macOS and Linux, and here it runs every SIMD path this CPU
// has: AVX2 and SSE2 on x86, NEON on arm64, and the scalar path with
// -tags box2dscalar. They all must match the one golden.
//
// It changes only when Box2D, the compiler rules (box2d_fp.h, physics.go) or
// the scene change. Then rerun with -update and review the diff.
func TestDeterminismGolden(t *testing.T) {
	path := filepath.Join("testdata", "determinism.golden")
	if scalarBuild && hasAVX2() {
		t.Fatal("-tags box2dscalar did not reach the C code: Box2D still has its AVX2 solver")
	}
	got := pileTrace(t, false)
	t.Logf("%s/%s, %s path:\n%s", runtime.GOOS, runtime.GOARCH, simdPath(), got)
	if hasAVX2() {
		if sse2 := pileTrace(t, true); sse2 != got {
			t.Errorf("the SSE2 fallback differs from AVX2:\nSSE2:\n%sAVX2:\n%s", sse2, got)
		} else {
			t.Log("the SSE2 fallback matches AVX2")
		}
	}
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("the pile differs from %s:\ngot:\n%swant:\n%s", path, got, want)
	}
}

// TestDeterminismRepeats checks that running the same scene twice in one
// process gives the same result, so no state leaks between worlds.
func TestDeterminismRepeats(t *testing.T) {
	first := pileTrace(t, false)
	if again := pileTrace(t, false); again != first {
		t.Errorf("the second run differs:\nfirst:\n%ssecond:\n%s", first, again)
	}
}
