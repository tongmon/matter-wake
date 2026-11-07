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
// cell count, what the inventory holds, how many cells wait in the queue
// to be put back, how many bodies there are and the world checksum. CI runs it
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
	if all.placed == 0 || all.dug[material.Powder] == 0 || all.dug[material.Static] == 0 || all.selected == 0 || all.dropped == 0 || all.queued == 0 {
		t.Fatalf("the replays place %d cells, dig %d powder and %d static cells, select a slot %d times, drop %d bodies "+
			"and queue cells in %d ticks; each must happen",
			all.placed, all.dug[material.Powder], all.dug[material.Static], all.selected, all.dropped, all.queued)
	}
}

// coverage counts what a replay's inputs did.
type coverage struct {
	placed, selected, dropped int
	// dug counts the cells dug out by their material's kind.
	dug [material.Solid + 1]int
	// queued counts the ticks that ended with cells in the queue.
	queued int
}

func (c *coverage) add(o coverage) {
	c.placed += o.placed
	c.selected += o.selected
	c.dropped += o.dropped
	c.queued += o.queued
	for k, n := range o.dug {
		c.dug[k] += n
	}
}

// trace plays r with tab on the given number of workers. It writes a line
// with each material's cell count, the inventory, the cells in the queue,
// the bodies and the checksum at tick 0, every traceEvery ticks and at the
// end. In the rooms, after every tick it checks that nothing made or
// destroyed a cell: each material's cells in the world, in the queue and
// in the inventory are what they were at the start. The tools only move
// cells between the world and the inventory, the world's own tick neither
// makes nor destroys them, and a body pushes cells into the queue. Solids
// are counted in the bodies' images instead, since a turned body covers
// more or fewer cells than it has pixels; they grow only by what the body
// tool drops. In the terrain the counts cover the blocks in memory, which
// come and go.
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
		fmt.Fprintf(&b, " queued %d bodies %d checksum %016x\n", w.QueueLen(), g.Bodies(), w.Checksum())
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
		bodies := g.Bodies()
		if err := g.Step(in); err != nil {
			t.Fatal(err)
		}
		if n := g.Bodies() - bodies; n > 0 {
			cov.dropped += n
			m := tab.MustLookup(r.Body.Material)
			start[m] += n * r.Body.Width * r.Body.Height
		}
		if w.QueueLen() > 0 {
			cov.queued++
		}
		// In the terrain, cells come and go with the blocks in memory.
		if r.Scene != game.SceneTerrain {
			if got := totals(g, tab); !slices.Equal(got, start) {
				t.Fatalf("after tick %d each material's cells, queued cells, inventory and body pixels are %v, want %v",
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

// totals returns each material's cells in the world and in its queue plus
// what the inventory holds of it, and for a solid the pixels of the
// bodies' images instead of its cells. Air is left out: it is what digging
// and bodies leave.
func totals(g *game.Game, tab *material.Table) []int {
	n := countCells(g, tab)
	g.World().Queued(n)
	n[material.Air] = 0
	for id := 1; id < len(n); id++ {
		if tab.Def(material.ID(id)).Kind == material.Solid {
			n[id] = 0
		}
		n[id] += g.Inventory(material.ID(id))
	}
	g.BodyPixels(n)
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
