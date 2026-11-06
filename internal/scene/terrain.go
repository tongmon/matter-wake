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
func (t *Terrain) Block(bx, by int) *world.Block {
	b := new(world.Block)
	x0, y0 := bx*world.BlockSize, by*world.BlockSize
	for x := x0; x < min(x0+world.BlockSize, t.width); x++ {
		surface := t.Surface(x)
		for y := max(y0, surface); y < min(y0+world.BlockSize, t.height); y++ {
			b[(y-y0)*world.BlockSize+x-x0] = world.Cell{Mat: t.at(x, y), Tone: t.tone(x, y)}
		}
	}
	return b
}

// at returns the material of the ground at (x, y), below the surface.
func (t *Terrain) at(x, y int) material.ID {
	cave := (2*noise2(t.seed, saltCaveLarge, x, y, caveSpanLarge) + noise2(t.seed, saltCaveSmall, x, y, caveSpanSmall)) / 3
	if cave < caveOpen {
		return t.stone
	}
	switch pocket := noise2(t.seed, saltPockets, x, y, pocketSpan); {
	case pocket > sandFrom:
		return t.sand
	case pocket < waterTo:
		return t.water
	}
	return material.Air
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

// noise2 is value noise: random values at the points of a lattice span
// cells apart, smoothly blended between them. x and y are 0 or more.
func noise2(seed, salt uint64, x, y, span int) int {
	i, j := x/span, y/span
	fx, fy := smooth((x-i*span)<<noiseBits/span), smooth((y-j*span)<<noiseBits/span)
	top := blend(lattice(seed, salt, i, j), lattice(seed, salt, i+1, j), fx)
	bottom := blend(lattice(seed, salt, i, j+1), lattice(seed, salt, i+1, j+1), fx)
	return blend(top, bottom, fy)
}

// noise1 is value noise along one row.
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
