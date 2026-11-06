package scene

import (
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// Terrain is the test world of the big map: sky over hills of stone, caves
// in the stone, and pockets of sand and water in the caves. It is a
// placeholder until the game's world is designed (design.md 7절).
//
// Every block is made from the seed and its own coordinates alone, so any
// block can be made on its own, in any order, the first time the camera
// comes near it. All numbers are 🧪.
type Terrain struct {
	seed                uint64
	width, height       int
	stone, sand, water  material.ID
	surface, hillHeight int
}

// Terrain shape (🧪).
const (
	// surfaceDepth is how far below the top of the world the ground lies
	// on average, and hillHeight how far it rises and falls.
	surfaceDepth = 2 * world.BlockSize
	hillHeight   = 96
	hillSpan     = 512
	// Caves open where the cave noise, from 0 to noiseMax, passes caveOpen.
	caveSpanLarge = 128
	caveSpanSmall = 48
	caveOpen      = 38000
	// In a cave, the pocket noise fills sand above sandFrom and water
	// below waterTo.
	pocketSpan = 64
	sandFrom   = 50000
	waterTo    = 12000
)

// Salts keep the noises of one seed apart.
const (
	saltHills = iota + 1
	saltCaveLarge
	saltCaveSmall
	saltPockets
	saltTone
)

// NewTerrain returns the terrain of a world of the given size and seed. The
// material table must have stone, sand and water.
func NewTerrain(seed uint64, width, height int, mats *material.Table) (*Terrain, error) {
	stone, sand, water, err := lookup(mats)
	if err != nil {
		return nil, err
	}
	return &Terrain{
		seed: seed, width: width, height: height,
		stone: stone, sand: sand, water: water,
		surface: surfaceDepth, hillHeight: hillHeight,
	}, nil
}

// Surface returns the row of the ground's top cell in column x.
func (t *Terrain) Surface(x int) int {
	return t.surface + (noise1(t.seed, saltHills, x, hillSpan)-noiseHalf)*t.hillHeight/noiseHalf
}

// Block makes block (bx, by). Cells past the world's edge stay air.
//
// It hashes each lattice point the block's noises use once, not once a
// cell: that was half the time of making a block, which happens while the
// game waits (TestTerrainBlockMatchesTheNoise).
func (t *Terrain) Block(bx, by int) *world.Block {
	b := new(world.Block)
	x0, y0 := bx*world.BlockSize, by*world.BlockSize
	x1, y1 := min(x0+world.BlockSize, t.width), min(y0+world.BlockSize, t.height)
	if x0 >= x1 || y0 >= y1 {
		return b
	}
	large := newGrid(t.seed, saltCaveLarge, caveSpanLarge, x0, y0, x1, y1)
	small := newGrid(t.seed, saltCaveSmall, caveSpanSmall, x0, y0, x1, y1)
	pockets := newGrid(t.seed, saltPockets, pocketSpan, x0, y0, x1, y1)
	for x := x0; x < x1; x++ {
		surface := t.Surface(x)
		for y := max(y0, surface); y < y1; y++ {
			dx, dy := x-x0, y-y0
			m := t.stone
			if (2*large.at(dx, dy)+small.at(dx, dy))/3 >= caveOpen {
				switch pocket := pockets.at(dx, dy); {
				case pocket > sandFrom:
					m = t.sand
				case pocket < waterTo:
					m = t.water
				default:
					m = material.Air
				}
			}
			b[dy*world.BlockSize+dx] = world.Cell{Mat: m, Tone: t.tone(x, y)}
		}
	}
	return b
}

// grid is one noise over the cells [x0, x1) × [y0, y1): the values of the
// lattice points it uses, and for each column and row the lattice step it
// lies in and how far into it, eased. Its at gives what noise2 gives.
type grid struct {
	values []int
	// cols is how many lattice points a row of values has.
	cols   int
	ix, iy []int
	fx, fy []int
}

func newGrid(seed, salt uint64, span, x0, y0, x1, y1 int) *grid {
	i0, j0 := x0/span, y0/span
	g := &grid{cols: (x1-1)/span - i0 + 2}
	rows := (y1-1)/span - j0 + 2
	g.values = make([]int, g.cols*rows)
	for j := range rows {
		for i := range g.cols {
			g.values[j*g.cols+i] = lattice(seed, salt, i0+i, j0+j)
		}
	}
	steps := func(from, to, first int) (idx, f []int) {
		for v := from; v < to; v++ {
			i := v / span
			idx = append(idx, i-first)
			f = append(f, smooth((v-i*span)<<noiseBits/span))
		}
		return idx, f
	}
	g.ix, g.fx = steps(x0, x1, i0)
	g.iy, g.fy = steps(y0, y1, j0)
	return g
}

// at returns the noise at the cell dx, dy into the grid.
func (g *grid) at(dx, dy int) int {
	k := g.iy[dy]*g.cols + g.ix[dx]
	top := blend(g.values[k], g.values[k+1], g.fx[dx])
	bottom := blend(g.values[k+g.cols], g.values[k+g.cols+1], g.fx[dx])
	return blend(top, bottom, g.fy[dy])
}

func (t *Terrain) tone(x, y int) uint8 {
	return uint8(rng.Hash3(t.seed^saltTone, uint64(x), uint64(y), 0) >> 56)
}

// The noises return smooth values from 0 to noiseMax.
const (
	noiseBits = 16
	noiseMax  = 1<<noiseBits - 1
	noiseHalf = 1 << (noiseBits - 1)
)

// lattice returns the noise value at lattice point (i, j).
func lattice(seed, salt uint64, i, j int) int {
	return int(rng.Hash3(seed, salt, uint64(i), uint64(j)) >> (64 - noiseBits))
}

// noise1 is value noise along one row: random values at the points of a
// lattice span cells apart, smoothly blended between them. The ground's
// noises (grid) are the same in two dimensions. x is 0 or more.
func noise1(seed, salt uint64, x, span int) int {
	i := x / span
	return blend(lattice(seed, salt, i, 0), lattice(seed, salt, i+1, 0), smooth((x-i*span)<<noiseBits/span))
}

// blend moves from a to b by f, a fraction of 1<<noiseBits.
func blend(a, b, f int) int {
	return a + (b-a)*f>>noiseBits
}

// smooth eases f, a fraction of 1<<noiseBits, to 3f²−2f³, so the noise has
// no corners at the lattice points.
func smooth(f int) int {
	f2 := f * f >> noiseBits
	return (3*f2 - 2*(f2*f>>noiseBits))
}
