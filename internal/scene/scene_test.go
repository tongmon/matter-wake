package scene

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

var update = flag.Bool("update", false, "rewrite the .golden files")

// TestSandboxGolden pins how the sandbox evolves. After an intended change
// to the simulation rules, rerun with -update and review the diff of
// testdata/sandbox.golden in the PR.
func TestSandboxGolden(t *testing.T) {
	tab := loadTable(t)
	w, err := world.New(world.Config{Width: 480, Height: 270, Seed: 1, Workers: 4, Materials: tab})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := Sandbox(w, tab); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, tick := range []uint32{0, 1, 10, 60, 300} {
		for w.Tick() < tick {
			w.Step()
		}
		fmt.Fprintf(&b, "tick %d sand %d water %d checksum %016x\n",
			tick, w.Count(tab.MustLookup("sand")), w.Count(tab.MustLookup("water")), w.Checksum())
	}
	compareGolden(t, filepath.Join("testdata", "sandbox.golden"), b.String())
}

func TestSandboxNeedsItsMaterials(t *testing.T) {
	tab, err := material.NewTable([]material.Def{{Name: "air", Kind: material.Empty}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := world.New(world.Config{Width: 64, Height: 64, Materials: tab})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := Sandbox(w, tab); err == nil {
		t.Fatal("Sandbox accepted a table without stone, sand and water")
	}
}

func loadTable(t testing.TB) *material.Table {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := material.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func compareGolden(t *testing.T, path, got string) {
	t.Helper()
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
		t.Fatalf("%s differs (rerun with -update after an intended change)\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}
