package replay

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
)

var update = flag.Bool("update", false, "rewrite the .golden files")

// traceEvery is how often, in ticks, a trace writes a line.
const traceEvery = 30

// TestGoldenReplays plays every testdata/*.rpl with the testdata materials
// and compares its trace with NAME.golden. The trace pins each material's
// cell count, what the inventory holds and the world checksum. CI runs it
// on Windows, macOS and Linux, so a passing golden means the replay gives
// the same world on all three. After an intended change to the simulation
// rules, rerun go test ./internal/replay/ -update and review the golden
// diff in the PR.
func TestGoldenReplays(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.rpl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no testdata/*.rpl files")
	}
	tab := loadTable(t)
	var all coverage
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			r := load(t, path)
			if r.Creative {
				t.Fatal("a golden replay must be in play mode: creative mode makes cells, so nothing pins their conservation")
			}
			got, cov := trace(t, r, tab, 1)
			// The result must not depend on the worker count.
			if many, _ := trace(t, r, tab, 4); many != got {
				t.Fatalf("4 workers trace\n%s\ndiffers from 1 worker\n%s", many, got)
			}
			all.add(cov)
			compareGolden(t, strings.TrimSuffix(path, ".rpl")+".golden", got)
		})
	}
	// The replays must keep exercising every tool, or a golden could pass
	// while pinning nothing about it.
	if all.placed == 0 || all.dug[material.Powder] == 0 || all.dug[material.Static] == 0 || all.selected == 0 {
		t.Fatalf("the replays place %d cells, dig %d powder and %d static cells and select a slot %d times; each must happen",
			all.placed, all.dug[material.Powder], all.dug[material.Static], all.selected)
	}
}

// coverage counts what a replay's inputs did.
type coverage struct {
	placed, selected int
	// dug counts the cells dug out by their material's kind.
	dug [material.Liquid + 1]int
}

func (c *coverage) add(o coverage) {
	c.placed += o.placed
	c.selected += o.selected
	for k, n := range o.dug {
		c.dug[k] += n
	}
}

// trace plays r with tab on the given number of workers. It writes a line
// with each material's cell count, the inventory and the checksum at tick
// 0, every traceEvery ticks and at the end. In the sandbox, after every tick
// it checks that each material's cells plus its inventory are what they
// were at the start: the tools only move cells between the world and the
// inventory, and the world's own tick neither makes nor destroys them. In
// the terrain the counts cover the blocks in memory, which come and go.
func trace(t *testing.T, r *Replay, tab *material.Table, workers int) (string, coverage) {
	t.Helper()
	g := newGame(t, r.Config(tab, workers))
	w := g.World()
	start := totals(g, tab)
	var cov coverage
	var b strings.Builder
	line := func() {
		fmt.Fprintf(&b, "tick %d", w.Tick())
		for id, n := range countCells(g, tab) {
			fmt.Fprintf(&b, " %s %d", tab.Def(material.ID(id)).Name, n)
		}
		b.WriteString(" held")
		for id := 1; id < tab.Len(); id++ {
			fmt.Fprintf(&b, " %s %d", tab.Def(material.ID(id)).Name, g.Inventory(material.ID(id)))
		}
		fmt.Fprintf(&b, " checksum %016x\n", w.Checksum())
	}
	line()
	held := make([]int, tab.Len())
	for i, in := range r.Inputs {
		if in.Select >= 1 && in.Select <= len(r.Slots) {
			cov.selected++
		}
		for id := range held {
			held[id] = g.Inventory(material.ID(id))
		}
		if err := g.Step(in); err != nil {
			t.Fatal(err)
		}
		// In the terrain, cells come and go with the blocks in memory.
		if r.Scene == game.SceneSandbox {
			if got := totals(g, tab); !slices.Equal(got, start) {
				t.Fatalf("after tick %d each material's cells plus its inventory are %v, want %v as at the start",
					w.Tick(), got, start)
			}
		}
		for id, before := range held {
			switch now := g.Inventory(material.ID(id)); {
			case now > before:
				cov.dug[tab.Def(material.ID(id)).Kind] += now - before
			case now < before:
				cov.placed += before - now
			}
		}
		if tick := i + 1; tick%traceEvery == 0 || tick == len(r.Inputs) {
			line()
		}
	}
	return b.String(), cov
}

// totals returns each material's cells in the world plus what the
// inventory holds of it. Air is left out: it is what digging leaves.
func totals(g *game.Game, tab *material.Table) []int {
	n := countCells(g, tab)
	n[material.Air] = 0
	for id := 1; id < len(n); id++ {
		n[id] += g.Inventory(material.ID(id))
	}
	return n
}

func countCells(g *game.Game, tab *material.Table) []int {
	counts := make([]int, tab.Len())
	g.World().Counts(counts)
	return counts
}

func load(t *testing.T, path string) *Replay {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return r
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
		t.Fatalf("%v (run go test ./internal/replay/ -update to create it)", err)
	}
	if got != string(want) {
		t.Fatalf("%s differs (rerun with -update after an intended change)\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}
