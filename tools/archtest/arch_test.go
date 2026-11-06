package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// ebitenLayers are the packages under internal/ that may use Ebitengine,
// the operating system and the clock. Everything else under internal/ is
// game logic. A package nested below a layer belongs to that layer.
var ebitenLayers = []string{"app", "input", "render"}

// concurrent are the logic packages allowed to use sync and start
// goroutines. Each must keep its results independent of scheduling.
var concurrent = []string{"sched"}

// cgoPackages are the logic packages allowed to use cgo. physics compiles
// Box2D; nothing else may.
var cgoPackages = []string{"physics"}

// nondeterministic are standard packages game logic must not import: they
// read the clock, draw unseeded random numbers, or synchronize goroutines.
var nondeterministic = []string{"time", "math/rand", "math/rand/v2", "crypto/rand", "sync", "sync/atomic"}

// osAccess are standard packages that reach outside the process. Logic
// takes data as bytes or readers; the app layer opens files.
var osAccess = []string{"os", "os/exec", "net", "net/http", "syscall"}

type file struct {
	path string
	pkg  string // directory relative to internal/, slash-separated
	test bool
	ast  *ast.File
	fset *token.FileSet
}

func TestLogicStaysPortableAndDeterministic(t *testing.T) {
	module := modulePath(t)
	for _, f := range parseInternal(t) {
		if isLayer(f.pkg, ebitenLayers) {
			continue
		}
		for _, imp := range f.ast.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if err := checkLogicImport(module, f, path); err != "" {
				t.Errorf("%s imports %q: %s", f.path, path, err)
			}
		}
		if !f.test && !isLayer(f.pkg, concurrent) {
			ast.Inspect(f.ast, func(n ast.Node) bool {
				if g, ok := n.(*ast.GoStmt); ok {
					t.Errorf("%s:%d starts a goroutine; only %v may (use internal/sched)",
						f.path, f.fset.Position(g.Pos()).Line, concurrent)
				}
				return true
			})
		}
	}
}

// checkLogicImport returns why a logic file must not import path, or "".
func checkLogicImport(module string, f file, path string) string {
	top := strings.SplitN(path, "/", 2)[0]
	switch {
	case path == "C":
		if !isLayer(f.pkg, cgoPackages) {
			return "game logic must not use cgo"
		}
	case strings.HasPrefix(path, module+"/internal/"):
		if isLayer(strings.TrimPrefix(path, module+"/internal/"), ebitenLayers) {
			return "game logic must not depend on an Ebitengine layer"
		}
	case strings.Contains(top, "."):
		return "game logic imports only the standard library and other logic packages"
	case f.test:
		// Tests may use any standard package, e.g. os to read testdata.
	case isLayer(f.pkg, concurrent) && (path == "sync" || path == "sync/atomic"):
		// The scheduler synchronizes its workers.
	case slices.Contains(nondeterministic, path):
		return "game logic must stay deterministic"
	case slices.Contains(osAccess, path):
		return "game logic must not touch the OS; pass data in"
	}
	return ""
}

// The rules themselves must catch what they are meant to catch.
func TestCheckLogicImport(t *testing.T) {
	const module = "example.com/m"
	logic := file{pkg: "world"}
	tests := []struct {
		f      file
		path   string
		reject bool
	}{
		{logic, "fmt", false},
		{logic, module + "/internal/rng", false},
		{logic, module + "/internal/render", true},
		{logic, "github.com/hajimehoshi/ebiten/v2", true},
		{logic, "C", true},
		{logic, "time", true},
		{logic, "sync", true},
		{logic, "os", true},
		{file{pkg: "world", test: true}, "os", false},
		{file{pkg: "world", test: true}, "github.com/hajimehoshi/ebiten/v2", true},
		{file{pkg: "sched"}, "sync/atomic", false},
		{file{pkg: "sched"}, "time", true},
		{file{pkg: "physics"}, "C", false},
		{file{pkg: "physics"}, "sync", true},
		{file{pkg: "sched"}, "C", true},
	}
	for _, tt := range tests {
		if got := checkLogicImport(module, tt.f, tt.path) != ""; got != tt.reject {
			t.Errorf("package %s (test %v) importing %q: rejected = %v, want %v", tt.f.pkg, tt.f.test, tt.path, got, tt.reject)
		}
	}
}

// Every package a rule names must exist, so a rename cannot silently
// drop a rule.
func TestListsNameExistingPackages(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal")
	for _, p := range slices.Concat(ebitenLayers, concurrent, cgoPackages) {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil || !info.IsDir() {
			t.Errorf("rule lists internal/%s, which does not exist", p)
		}
	}
}

func isLayer(pkg string, layers []string) bool {
	for _, l := range layers {
		if pkg == l || strings.HasPrefix(pkg, l+"/") {
			return true
		}
	}
	return false
}

// parseInternal parses the imports and bodies of every Go file under
// internal/, whatever its build tags, so a rule holds on every platform.
func parseInternal(t *testing.T) []file {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal")
	fset := token.NewFileSet()
	var files []file
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		files = append(files, file{
			path: path, pkg: filepath.ToSlash(rel), test: strings.HasSuffix(name, "_test.go"), ast: parsed, fset: fset,
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("found no Go files under internal/")
	}
	return files
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func modulePath(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod has no module line")
	return ""
}
