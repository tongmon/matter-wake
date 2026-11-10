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
			got, cov := trace(t, r, tab, 1)
			// The result must not depend on the worker count.
			if many, _ := trace(t, r, tab, 4); many != got {
				t.Fatalf("4 workers trace\n%s\ndiffers from 1 worker\n%s", many, got)
			}
			all.add(&cov)
			compareGolden(t, strings.TrimSuffix(path, ".rpl")+".golden", got)
		})
	}
	// The replays must keep exercising every tool, or a golden could pass
	// while pinning nothing about it.
	if all.placed == 0 || all.dug[material.Powder] == 0 || all.dug[material.Static] == 0 || all.dug[material.Solid] == 0 ||
		all.selected == 0 || all.dropped == 0 || all.broken == 0 || all.queued == 0 {
		t.Fatalf("the replays place %d cells, dig %d powder, %d static and %d solid cells, select a slot %d times, "+
			"drop %d bodies, break off %d and queue cells in %d ticks; each must happen",
			all.placed, all.dug[material.Powder], all.dug[material.Static], all.dug[material.Solid], all.selected,
			all.dropped, all.broken, all.queued)
	}
	if all.made == 0 || all.changed[0] == 0 || all.changed[1] == 0 || all.changed[2] == 0 || all.moving == 0 {
		t.Fatalf("the replays make %d cells in creative mode, change the brush %d times, the mode %d times and the body "+
			"tool %d times, and drop %d bodies turned or moving; each must happen",
			all.made, all.changed[0], all.changed[1], all.changed[2], all.moving)
	}
	if all.solidified == 0 || all.shattered == 0 || all.static == 0 {
		t.Fatalf("the replays turn %d cells of terrain into bodies, shatter %d pixels into powder and break %d static "+
			"bodies apart; each must happen", all.solidified, all.shattered, all.static)
	}
}

// coverage counts what a replay's inputs did: broken counts the bodies
// that breaking added, made the cells creative mode made and moving the
// bodies dropped turned or moving. solidified counts the cells of terrain
// that turned into bodies, shattered the pixels of bodies that turned into
// powder, and static the ticks in which a static body broke into pieces.
type coverage struct {
	placed, selected, dropped, broken, made, moving int
	solidified, shattered, static                   int
	// dug counts the cells dug out by their material's kind.
	dug [material.Solid + 1]int
	// queued counts the ticks that ended with cells in the queue.
	queued int
	// changed counts the ticks that changed the brush, the mode and the
	// body tool to something new.
	changed [3]int
}

func (c *coverage) add(o *coverage) {
	c.placed += o.placed
	c.selected += o.selected
	c.dropped += o.dropped
	c.broken += o.broken
	c.made += o.made
	c.moving += o.moving
	c.solidified += o.solidified
	c.shattered += o.shattered
	c.static += o.static
	c.queued += o.queued
	for k, n := range o.dug {
		c.dug[k] += n
	}
	for k, n := range o.changed {
		c.changed[k] += n
	}
}

// trace plays r with tab on the given number of workers. It writes a line
// with each material's cell count, the inventory, the cells in the queue,
// the bodies and the checksum at tick 0, every traceEvery ticks and at the
// end. In the rooms, after every tick it checks that nothing made or
// destroyed a cell: each material's cells in the world, in the queue and
// in the inventory are what they were at the start, plus what creative
// mode made (game.Game.Made), from whichever tick it was turned on, and
// less what turned into another material (game.Game.Turned): terrain dug
// into turning into a body, and a body shattering into powder. The
// tools only move cells between the world and the inventory, the world's
// own tick neither makes nor destroys them, and a body pushes cells into
// the queue. Solids are counted in the bodies' images instead, since a
// turned body covers more or fewer cells than it has pixels; they grow
// only by what the body tool drops, and what is dug or broken off a body
// goes into the inventory. In the terrain the counts cover the blocks in
// memory, which come and go.
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
	for i := range r.Inputs {
		in := &r.Inputs[i]
		if in.Select >= 1 && in.Select <= len(r.Slots) {
			cov.selected++
		}
		for id := range held {
			held[id] = g.Inventory(material.ID(id))
		}
		bodies, dropped, made := g.Bodies(), g.Dropped(), madeCells(g, tab)
		solid, powder := turnedInto(g, tab)
		statics, staticPx := staticBodies(g)
		was := settings{g.Tool(), g.Creative(), g.Body()}
		if err := g.Step(in); err != nil {
			t.Fatal(err)
		}
		cov.count(&was, &settings{g.Tool(), g.Creative(), g.Body()})
		cov.made += madeCells(g, tab) - made
		nowSolid, nowPowder := turnedInto(g, tab)
		cov.solidified += nowSolid - solid
		cov.shattered += nowPowder - powder
		// A static body that lost pixels while dynamic bodies came that
		// the body tool did not drop: pieces of it.
		nowStatics, nowStaticPx := staticBodies(g)
		if nowStaticPx < staticPx && g.Bodies()-nowStatics-(bodies-statics) > g.Dropped()-dropped {
			cov.static++
		}
		if n := g.Dropped() - dropped; n > 0 {
			cov.dropped += n
			body := g.Body()
			if body.Angle != 0 || body.VX != 0 || body.VY != 0 {
				cov.moving += n
			}
			start[tab.MustLookup(body.Material)] += n * body.Width * body.Height
		}
		if n := g.Bodies() - bodies - (g.Dropped() - dropped); n > 0 {
			cov.broken += n
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

// settings are the settings a game.Change sets.
type settings struct {
	tool     game.Tool
	creative bool
	body     game.BodyTool
}

// count counts the settings that changed from was to now.
func (c *coverage) count(was, now *settings) {
	for k, changed := range [...]bool{was.tool != now.tool, was.creative != now.creative, was.body != now.body} {
		if changed {
			c.changed[k]++
		}
	}
}

// madeCells returns how many cells creative mode has made in all.
func madeCells(g *game.Game, tab *material.Table) int {
	n := make([]int, tab.Len())
	g.Made(n)
	sum := 0
	for _, m := range n {
		sum += m
	}
	return sum
}

// turnedInto returns how many cells or pixels have turned into solids, the
// terrain that became bodies, and into powders, the bodies that shattered,
// in all (game.Game.Turned).
func turnedInto(g *game.Game, tab *material.Table) (solid, powder int) {
	n := make([]int, tab.Len())
	g.Turned(n)
	for id, k := range n {
		switch tab.Def(material.ID(id)).Kind {
		case material.Solid:
			solid += max(k, 0)
		case material.Powder:
			powder += k
		case material.Empty, material.Static, material.Liquid:
		}
	}
	return solid, powder
}

// staticBodies returns how many of g's bodies are static and how many
// pixels they hold.
func staticBodies(g *game.Game) (n, pixels int) {
	for _, b := range g.BodyRecords(nil) {
		if !b.Static {
			continue
		}
		n++
		for _, p := range b.Pixels {
			if p.OK {
				pixels++
			}
		}
	}
	return n, pixels
}

// totals returns each material's cells in the world and in its queue plus
// what the inventory holds of it, less what creative mode made and what
// turned into it from another material, and for a solid the pixels of the
// bodies' images instead of its cells. Air is left out: it is what digging
// and bodies leave.
func totals(g *game.Game, tab *material.Table) []int {
	n := countCells(g, tab)
	g.World().Queued(n)
	n[material.Air] = 0
	made, turned := make([]int, len(n)), make([]int, len(n))
	g.Made(made)
	g.Turned(turned)
	for id := 1; id < len(n); id++ {
		if tab.Def(material.ID(id)).Kind == material.Solid {
			n[id] = 0
		}
		n[id] += g.Inventory(material.ID(id)) - made[id] - turned[id]
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
