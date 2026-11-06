package scene

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

func newTerrain(t *testing.T, seed uint64) (*Terrain, *material.Table) {
	t.Helper()
	tab := loadTable(t)
	tr, err := NewTerrain(seed, 16384, 16384, tab)
	if err != nil {
		t.Fatal(err)
	}
	return tr, tab
}

// A block is made from the seed and its own place alone, so it comes out
// the same whenever and in whatever order it is made.
func TestTerrainBlockDependsOnlyOnSeedAndPlace(t *testing.T) {
	a, _ := newTerrain(t, 5)
	b, _ := newTerrain(t, 5)
	b.Block(9, 9)
	if *a.Block(16, 2) != *b.Block(16, 2) {
		t.Fatal("the same block of the same seed came out differently")
	}
	c, _ := newTerrain(t, 6)
	if *a.Block(16, 2) == *c.Block(16, 2) {
		t.Fatal("two seeds made the same block")
	}
}

// Below the sky there is stone, open caves, sand and water.
func TestTerrainHasEveryMaterial(t *testing.T) {
	tr, tab := newTerrain(t, 1)
	n := make([]int, tab.Len())
	for by := 2; by < 6; by++ {
		for bx := 14; bx < 18; bx++ {
			for _, c := range tr.Block(bx, by) {
				n[c.Mat]++
			}
		}
	}
	for _, name := range []string{"air", "stone", "sand", "water"} {
		if n[tab.MustLookup(name)] == 0 {
			t.Errorf("no %s in 16 blocks below the surface", name)
		}
	}
}

// Above the surface is sky, and the surface rises and falls smoothly.
func TestTerrainSurface(t *testing.T) {
	tr, _ := newTerrain(t, 1)
	for x := range 16384 {
		s := tr.Surface(x)
		if s < surfaceDepth-hillHeight || s > surfaceDepth+hillHeight {
			t.Fatalf("surface at column %d is row %d, outside %d±%d", x, s, surfaceDepth, hillHeight)
		}
		if x > 0 {
			if d := s - tr.Surface(x-1); d < -2 || d > 2 {
				t.Fatalf("surface jumps %d rows from column %d to %d", d, x-1, x)
			}
		}
	}
	b := tr.Block(16, 1)
	for y := range world.BlockSize {
		for x := range world.BlockSize {
			if c := b[y*world.BlockSize+x]; world.BlockSize+y < tr.Surface(16*world.BlockSize+x) && c.Mat != material.Air {
				t.Fatalf("cell (%d, %d) of block (16, 1) above the surface holds material %d", x, y, c.Mat)
			}
		}
	}
}

// TestTerrainGolden pins what the terrain makes. After an intended change
// to it, rerun with -update and review the diff of testdata/terrain.golden.
func TestTerrainGolden(t *testing.T) {
	tr, tab := newTerrain(t, 1)
	var b strings.Builder
	for _, at := range [][2]int{{16, 1}, {16, 2}, {15, 3}, {31, 31}} {
		blk := tr.Block(at[0], at[1])
		n := make([]int, tab.Len())
		h := fnv.New64a()
		for _, c := range blk {
			n[c.Mat]++
			_, _ = h.Write([]byte{byte(c.Mat), byte(c.Mat >> 8), c.Tone})
		}
		fmt.Fprintf(&b, "block %d %d", at[0], at[1])
		for id, k := range n {
			fmt.Fprintf(&b, " %s %d", tab.Def(material.ID(id)).Name, k)
		}
		fmt.Fprintf(&b, " hash %016x\n", h.Sum64())
	}
	compareGolden(t, filepath.Join("testdata", "terrain.golden"), b.String())
}

func BenchmarkTerrainBlock(b *testing.B) {
	tr, err := NewTerrain(1, 16384, 16384, loadTable(b))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := range b.N {
		tr.Block(i%32, 2)
	}
}
