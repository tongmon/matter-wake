package save

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

// Block is what a block of a big world holds while it is out of memory:
// its cells, the cells taken from them that wait in the world's queue to
// go back (world.TakeQueued), the rigid bodies that left with it (Body),
// and the particles flying in it (world.TakeParticles). A block saved
// before version 3 holds cells alone, and one before version 6 no
// particles.
type Block struct {
	Cells     *world.Block
	Queued    []world.Queued
	Bodies    []Body
	Particles []world.Particle
}

// Body is a rigid body out of memory (internal/rigid): what a block keeps
// of each body that left with it, enough to make the same body again. Lengths are
// in cells and pixels, positions and speeds in Box2D's meters, seconds and
// radians.
type Body struct {
	// Key orders the bodies: it is the order they were made in, and a body
	// with a higher key takes a cell another one wants.
	Key uint32
	Mat material.ID
	// W and H are the image's size in pixels. Left and Top are where, in
	// the body's frame, the image of the body it was first made as began,
	// in cells from the origin; CX and CY are where this image begins in
	// that one, in pixels. A piece broken off a body keeps its frame.
	W, H      int
	Left, Top float32
	CX, CY    int
	// Pixels holds the image, row by row.
	Pixels []Pixel
	// Whole is set while the body is the box it was made as, its shape the
	// rectangle of its image; a body that lost pixels, or that terrain
	// turned into, has shapes made from them.
	Whole bool
	// Static is set for a body that never moves: one terrain turned into,
	// or the piece of one that stayed (block version 4 on).
	Static bool
	// X and Y are where the body's origin is, and C and S the cosine and
	// sine of its turn: Box2D's transform, to the bit.
	X, Y, C, S float32
	// VX and VY are the velocity of its center of mass, Spin its angular
	// velocity. Awake is set unless Box2D had put it to sleep.
	VX, VY, Spin float32
	Awake        bool
}

// Pixel is a pixel of a body's image: whether the body still has it, its
// tone, the damage it took, and whether a crack marked it (block version 5
// on); a pixel the body no longer has has no mark.
type Pixel struct {
	Damage uint16
	Tone   uint8
	OK     bool
	Crack  bool
}

// What a pixel's first byte holds, as written: whether the body has it,
// and from version 5 whether a crack marked it.
const (
	pixelHas   = 1
	pixelCrack = 2
)

// MaxBodyPixels is the most pixels a body's image may hold: what a cell's
// link to its pixel holds (world.BodyCell).
const MaxBodyPixels = 1 << 16

// maxBodies is the most bodies a block may hold: what a cell's link to its
// body holds.
const maxBodies = 1 << 16

// maxQueued caps the cells waiting to go back that a block may hold: many
// times its cells, so it only stops damage from claiming billions.
const maxQueued = 1 << 24

// maxParticles caps the particles a block may hold, as maxQueued does the
// cells: more than the world lets fly at once.
const maxParticles = 1 << 20

// check reports what is wrong with b for a block of mats, or nil.
func (b *Body) check(mats *material.Table) error {
	switch {
	case int(b.Mat) >= mats.Len():
		return fmt.Errorf("body %d is of material %d, which the table does not have", b.Key, b.Mat)
	case b.W < 1 || b.H < 1 || b.W > MaxBodyPixels || b.H > MaxBodyPixels || b.W*b.H > MaxBodyPixels:
		return fmt.Errorf("body %d is %d×%d pixels; a body holds 1 to %d", b.Key, b.W, b.H, MaxBodyPixels)
	case len(b.Pixels) != b.W*b.H:
		return fmt.Errorf("body %d has %d pixels for a %d×%d image", b.Key, len(b.Pixels), b.W, b.H)
	case b.CX < 0 || b.CY < 0 || b.CX > math.MaxInt32 || b.CY > math.MaxInt32:
		return fmt.Errorf("body %d begins at (%d, %d) in its first image", b.Key, b.CX, b.CY)
	}
	for k, p := range b.Pixels {
		if p.Crack && !p.OK {
			return fmt.Errorf("body %d has a crack mark on pixel %d, which it does not have", b.Key, k)
		}
	}
	return nil
}

// marked reports whether a crack marked a pixel of b.
func (b *Body) marked() bool {
	for _, p := range b.Pixels {
		if p.Crack {
			return true
		}
	}
	return false
}

// Flags of a body as written. Version 3 has no flagStatic.
const (
	flagWhole = 1 << iota
	flagAwake
	flagStatic
	flagAll = flagWhole | flagAwake | flagStatic
)

// flagsOf returns the flags a body of a block of the given version may
// have.
func flagsOf(version uint16) uint64 {
	if version < 4 {
		return flagWhole | flagAwake
	}
	return flagAll
}

// queuedBytes is how many bytes a queued cell takes before its cell's
// fields: its x and y.
const queuedBytes = 8

// particleBytes is how many bytes a particle takes before its cell's
// fields: x, y, lx, ly, vx, vy, seed, life, density, pass and flags.
const particleBytes = 4*8 + 2*4 + 8 + 4 + 3

// writeExtras writes the queued cells, the bodies and, from version 6, the
// particles of b, compressed together with DEFLATE, to w, in the layout of
// the given version. The cells', bodies' and particles' materials are IDs
// of mats, whose order the block's material list keeps.
func writeExtras(w io.Writer, b *Block, mats *material.Table, version uint16) error {
	if len(b.Queued) > maxQueued || len(b.Bodies) > maxBodies {
		return fmt.Errorf("%d queued cells and %d bodies; a block holds at most %d and %d", len(b.Queued), len(b.Bodies), maxQueued, maxBodies)
	}
	if len(b.Particles) > 0 && version < 6 {
		return fmt.Errorf("%d particles, which a block of version %d cannot hold", len(b.Particles), version)
	}
	if len(b.Particles) > maxParticles {
		return fmt.Errorf("%d particles; a block holds at most %d", len(b.Particles), maxParticles)
	}
	zw, err := flate.NewWriter(w, flate.BestSpeed)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(zw)
	writeUint(bw, uint64(len(b.Queued)), 4)
	width := 0
	for _, f := range fields {
		width += f.width
	}
	buf := make([]byte, queuedBytes+width)
	for _, q := range b.Queued {
		if int(q.Cell.Mat) >= mats.Len() {
			return fmt.Errorf("a queued cell is of material %d, which the table does not have", q.Cell.Mat)
		}
		binary.LittleEndian.PutUint32(buf, uint32(q.X))
		binary.LittleEndian.PutUint32(buf[4:], uint32(q.Y))
		at := queuedBytes
		for _, f := range fields {
			f.put(buf[at:], &q.Cell)
			at += f.width
		}
		_, _ = bw.Write(buf)
	}
	writeUint(bw, uint64(len(b.Bodies)), 4)
	for i := range b.Bodies {
		if b.Bodies[i].Static && version < 4 {
			return fmt.Errorf("body %d is static, which a block of version %d cannot hold", b.Bodies[i].Key, version)
		}
		if version < 5 && b.Bodies[i].marked() {
			return fmt.Errorf("body %d has crack marks, which a block of version %d cannot hold", b.Bodies[i].Key, version)
		}
		var err error
		if buf, err = writeBody(bw, &b.Bodies[i], mats, buf); err != nil {
			return err
		}
	}
	if version >= 6 {
		if err := writeParticles(bw, b.Particles, mats, width); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
}

// writeParticles writes ps after their count, each its head and then its
// cell's fields, width bytes of them.
func writeParticles(w *bufio.Writer, ps []world.Particle, mats *material.Table, width int) error {
	writeUint(w, uint64(len(ps)), 4)
	buf := make([]byte, particleBytes+width)
	for i := range ps {
		p := &ps[i]
		switch {
		case int(p.Cell.Mat) >= mats.Len():
			return fmt.Errorf("a particle is of material %d, which the table does not have", p.Cell.Mat)
		case p.Flags&^world.ParticleFlagsAll != 0:
			return fmt.Errorf("a particle has flags %#x", p.Flags)
		}
		for k, v := range [...]int64{p.X, p.Y, p.LX, p.LY} {
			binary.LittleEndian.PutUint64(buf[8*k:], uint64(v))
		}
		binary.LittleEndian.PutUint32(buf[32:], uint32(p.VX))
		binary.LittleEndian.PutUint32(buf[36:], uint32(p.VY))
		binary.LittleEndian.PutUint64(buf[40:], p.Seed)
		binary.LittleEndian.PutUint32(buf[48:], uint32(p.Life))
		buf[52], buf[53], buf[54] = p.Density, byte(p.Pass), byte(p.Flags)
		at := particleBytes
		for _, f := range fields {
			f.put(buf[at:], &p.Cell)
			at += f.width
		}
		_, _ = w.Write(buf)
	}
	return nil
}

// bodyHead is how many bytes a body takes before its pixels.
const bodyHead = 6*4 + 1 + 9*4

// writeBody writes one body, packed in buf, which it grows as it needs and
// returns.
func writeBody(w *bufio.Writer, b *Body, mats *material.Table, buf []byte) ([]byte, error) {
	if err := b.check(mats); err != nil {
		return buf, err
	}
	n := bodyHead + 4*len(b.Pixels)
	buf = slices.Grow(buf[:0], n)[:n]
	for i, v := range [...]uint32{b.Key, uint32(b.Mat), uint32(b.W), uint32(b.H), uint32(b.CX), uint32(b.CY)} {
		binary.LittleEndian.PutUint32(buf[4*i:], v)
	}
	flags := byte(0)
	if b.Whole {
		flags |= flagWhole
	}
	if b.Awake {
		flags |= flagAwake
	}
	if b.Static {
		flags |= flagStatic
	}
	buf[24] = flags
	for i, f := range [...]float32{b.Left, b.Top, b.X, b.Y, b.C, b.S, b.VX, b.VY, b.Spin} {
		binary.LittleEndian.PutUint32(buf[25+4*i:], math.Float32bits(f))
	}
	for k, p := range b.Pixels {
		at := buf[bodyHead+4*k:]
		at[0], at[1] = boolByte(p.OK)*pixelHas|boolByte(p.Crack)*pixelCrack, p.Tone
		binary.LittleEndian.PutUint16(at[2:], p.Damage)
	}
	_, _ = w.Write(buf)
	return buf, nil
}

// readExtras reads what writeExtras wrote for a block of the given
// version, from the size bytes of r, into b. ids maps the block's material
// list onto the table it is read against (-1 for a material it lacks) and
// names names them.
func (d *decoder) readExtras(b *Block, size uint64, ids []int, names []string, version uint16) error {
	if size > uint64(d.r.Len()) {
		return io.ErrUnexpectedEOF
	}
	packed := io.NewSectionReader(d.r, d.r.Size()-int64(d.r.Len()), int64(size))
	zr := flate.NewReader(packed)
	e := &extras{
		r: bufio.NewReader(zr), ids: ids, names: names, flags: flagsOf(version), marks: version >= 5, particles: version >= 6,
	}
	err := e.read(b, d.fields)
	if err == nil {
		if n, end := e.r.Read(make([]byte, 1)); n != 0 || !errors.Is(end, io.EOF) {
			err = errors.New("more data than its queued cells, bodies and particles")
		}
	}
	if cerr := zr.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	_, err = d.r.Seek(int64(size), io.SeekCurrent)
	return err
}

// extras reads the queued cells, bodies and particles of a block; flags
// are those a body of its version may have, marks is set if its pixels may
// have crack marks, and particles if it holds particles.
type extras struct {
	r                *bufio.Reader
	ids              []int
	names            []string
	flags            uint64
	marks, particles bool
}

func (e *extras) uint(size int) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(e.r, b[:size]); err != nil {
		return 0, noEOF(err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

// material maps index k of the block's material list onto the table.
func (e *extras) material(k uint64) (material.ID, error) {
	switch {
	case k >= uint64(len(e.ids)):
		return 0, fmt.Errorf("material %d of a list of %d", k, len(e.ids))
	case e.ids[k] < 0:
		return 0, fmt.Errorf("%q, which the material table does not have", e.names[k])
	}
	return material.ID(e.ids[k]), nil
}

func (e *extras) read(b *Block, fields []field) error {
	n, err := e.uint(4)
	if err != nil {
		return err
	}
	if n > maxQueued {
		return fmt.Errorf("%d queued cells, at most %d", n, maxQueued)
	}
	width := 0
	for _, f := range fields {
		width += f.width
	}
	buf := make([]byte, queuedBytes+width)
	for i := range n {
		if _, err := io.ReadFull(e.r, buf); err != nil {
			return noEOF(err)
		}
		q := world.Queued{X: int32(binary.LittleEndian.Uint32(buf)), Y: int32(binary.LittleEndian.Uint32(buf[4:]))}
		at := queuedBytes
		for _, f := range fields {
			if err := f.get(buf[at:], &q.Cell); err != nil {
				return fmt.Errorf("queued cell %d: %w", i, err)
			}
			at += f.width
		}
		if q.Cell.Mat, err = e.material(uint64(q.Cell.Mat)); err != nil {
			return fmt.Errorf("queued cell %d holds %w", i, err)
		}
		b.Queued = append(b.Queued, q)
	}
	if n, err = e.uint(4); err != nil {
		return err
	}
	if n > maxBodies {
		return fmt.Errorf("%d bodies, at most %d", n, maxBodies)
	}
	for i := range n {
		body, err := e.body()
		if err != nil {
			return fmt.Errorf("body %d: %w", i, err)
		}
		b.Bodies = append(b.Bodies, body)
	}
	if !e.particles {
		return nil
	}
	return e.readParticles(b, fields, width)
}

// readParticles reads the particles of a block, whose cells have fields,
// width bytes of them.
func (e *extras) readParticles(b *Block, fields []field, width int) error {
	n, err := e.uint(4)
	if err != nil {
		return err
	}
	if n > maxParticles {
		return fmt.Errorf("%d particles, at most %d", n, maxParticles)
	}
	buf := make([]byte, particleBytes+width)
	for i := range n {
		if _, err := io.ReadFull(e.r, buf); err != nil {
			return noEOF(err)
		}
		i64 := func(at int) int64 { return int64(binary.LittleEndian.Uint64(buf[at:])) }
		i32 := func(at int) int32 { return int32(binary.LittleEndian.Uint32(buf[at:])) }
		p := world.Particle{
			X: i64(0), Y: i64(8), LX: i64(16), LY: i64(24), VX: i32(32), VY: i32(36),
			Seed: binary.LittleEndian.Uint64(buf[40:]), Life: i32(48),
			Density: buf[52], Pass: int8(buf[53]), Flags: world.ParticleFlags(buf[54]),
		}
		if p.Flags&^world.ParticleFlagsAll != 0 {
			return fmt.Errorf("particle %d: flags %#x", i, p.Flags)
		}
		at := particleBytes
		for _, f := range fields {
			if err := f.get(buf[at:], &p.Cell); err != nil {
				return fmt.Errorf("particle %d: %w", i, err)
			}
			at += f.width
		}
		if p.Cell.Mat, err = e.material(uint64(p.Cell.Mat)); err != nil {
			return fmt.Errorf("particle %d holds %w", i, err)
		}
		b.Particles = append(b.Particles, p)
	}
	return nil
}

func (e *extras) body() (Body, error) {
	var head [6 * 4]byte // the integers of bodyHead
	if _, err := io.ReadFull(e.r, head[:]); err != nil {
		return Body{}, noEOF(err)
	}
	u := func(i int) uint32 { return binary.LittleEndian.Uint32(head[4*i:]) }
	b := Body{Key: u(0), W: int(u(2)), H: int(u(3)), CX: int(u(4)), CY: int(u(5))}
	m, err := e.material(uint64(u(1)))
	if err != nil {
		return Body{}, fmt.Errorf("is of %w", err)
	}
	b.Mat = m
	if b.W < 1 || b.H < 1 || b.W > MaxBodyPixels || b.H > MaxBodyPixels || b.W*b.H > MaxBodyPixels {
		return Body{}, fmt.Errorf("%d×%d pixels; a body holds 1 to %d", b.W, b.H, MaxBodyPixels)
	}
	if b.CX > math.MaxInt32 || b.CY > math.MaxInt32 {
		return Body{}, fmt.Errorf("begins at (%d, %d) in its first image", b.CX, b.CY)
	}
	flags, err := e.uint(1)
	if err != nil {
		return Body{}, err
	}
	if flags&^e.flags != 0 {
		return Body{}, fmt.Errorf("flags %#x", flags)
	}
	b.Whole, b.Awake, b.Static = flags&flagWhole != 0, flags&flagAwake != 0, flags&flagStatic != 0
	for _, f := range [...]*float32{&b.Left, &b.Top, &b.X, &b.Y, &b.C, &b.S, &b.VX, &b.VY, &b.Spin} {
		v, err := e.uint(4)
		if err != nil {
			return Body{}, err
		}
		*f = math.Float32frombits(uint32(v))
	}
	raw := make([]byte, 4*b.W*b.H)
	if _, err := io.ReadFull(e.r, raw); err != nil {
		return Body{}, noEOF(err)
	}
	b.Pixels = make([]Pixel, b.W*b.H)
	for k := range b.Pixels {
		p := raw[4*k:]
		switch {
		case !e.marks && p[0] > pixelHas:
			return Body{}, fmt.Errorf("pixel %d: has is %d, not 0 or 1", k, p[0])
		case e.marks && p[0] != 0 && p[0] != pixelHas && p[0] != pixelHas|pixelCrack:
			return Body{}, fmt.Errorf("pixel %d: flags %d, not 0, %d or %d", k, p[0], pixelHas, pixelHas|pixelCrack)
		}
		b.Pixels[k] = Pixel{
			OK: p[0]&pixelHas != 0, Crack: p[0]&pixelCrack != 0, Tone: p[1], Damage: binary.LittleEndian.Uint16(p[2:]),
		}
	}
	return b, nil
}

// compressExtras returns the extras of b as writeExtras writes them for a
// block of the given version.
func compressExtras(b *Block, mats *material.Table, version uint16) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeExtras(&buf, b, mats, version); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
