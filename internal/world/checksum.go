package world

// Checksum hashes the cells in memory and the particles flying. Worlds
// with the same blocks in memory, equal cells and equal particles give
// equal checksums, so tests use it to compare runs. It hashes the blocks
// in order, each row by row, so a world of one block is hashed row by row,
// and then the particles in their order. A world without particles hashes
// as its cells alone.
//
// The hash is our own, built to be fast rather than standard. Each cell is
// packed into two 64-bit words, and each word is mixed in by an exclusive
// or, a multiply and a shift. Every step can be undone, so two worlds that
// differ in a single cell always hash differently. Within a row the cells
// go to four lanes in turn, so the multiplies of neighboring cells overlap,
// and the lanes are mixed into the hash at the end of the row.
func (w *World) Checksum() uint64 {
	h := uint64(checksumSeed)
	w.eachRow(func(row []Cell) {
		h = hashRow(h, row)
	})
	for i := range w.particles {
		h = hashParticle(h, &w.particles[i])
	}
	return h
}

// hashParticle mixes every field of p into h.
func hashParticle(h uint64, p *Particle) uint64 {
	h = hashCell(h, p.Cell)
	for _, v := range [...]uint64{uint64(p.X), uint64(p.Y), uint64(p.LX), uint64(p.LY), p.Seed} {
		h = mix(h, v)
	}
	h = mix(h, uint64(uint32(p.VX))|uint64(uint32(p.VY))<<32)
	return mix(h, uint64(uint32(p.Life))|uint64(p.Density)<<32|uint64(uint8(p.Pass))<<40|uint64(p.Flags)<<48)
}

const (
	// checksumSeed starts the hash. Any value would do.
	checksumSeed = 0x6a09e667f3bcc909
	// mixMul is odd, so multiplying by it can be undone.
	mixMul = 0x9e3779b97f4a7c15
)

// hashRow mixes a row of cells into h.
func hashRow(h uint64, row []Cell) uint64 {
	a, b, c, d := h, h^1, h^2, h^3
	for len(row) >= 4 {
		a = hashCell(a, row[0])
		b = hashCell(b, row[1])
		c = hashCell(c, row[2])
		d = hashCell(d, row[3])
		row = row[4:]
	}
	for _, cell := range row {
		a = hashCell(a, cell)
	}
	return mix(mix(mix(mix(h, a), b), c), d)
}

// hashCell mixes every field of c into h.
func hashCell(h uint64, c Cell) uint64 {
	falling := uint64(0)
	if c.Falling {
		falling = 1
	}
	h = mix(h, uint64(c.Mat)|uint64(c.Tone)<<16|falling<<24|uint64(uint16(c.Vy))<<32|uint64(uint16(c.Ax))<<48)
	return mix(h, uint64(c.Last)|uint64(c.Damage)<<32|uint64(c.Rest)<<48)
}

// mix mixes the word v into h. For any v it maps each h to a different
// result.
func mix(h, v uint64) uint64 {
	h = (h ^ v) * mixMul
	return h ^ h>>32
}
