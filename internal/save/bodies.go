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
// go back (world.TakeQueued), and the rigid bodies it owns (Body). A block
// saved before version 3 holds cells alone.
type Block struct {
	Cells  *world.Block
	Queued []world.Queued
	Bodies []Body
}

// Body is a rigid body out of memory (internal/rigid): what a block keeps
// of each body it owns, enough to make the same body again. Lengths are
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
	// rectangle of its image; a body that lost pixels has shapes made from
	// them.
	Whole bool
	// X and Y are where the body's origin is, and C and S the cosine and
	// sine of its turn: Box2D's transform, to the bit.
	X, Y, C, S float32
	// VX and VY are the velocity of its center of mass, Spin its angular
	// velocity. Awake is set unless Box2D had put it to sleep.
	VX, VY, Spin float32
	Awake        bool
}

// Pixel is a pixel of a body's image: whether the body still has it, its
// tone, and the damage it took.
type Pixel struct {
	Damage uint16
	Tone   uint8
	OK     bool
}

// MaxBodyPixels is the most pixels a body's image may hold: what a cell's
// link to its pixel holds (world.BodyCell).
const MaxBodyPixels = 1 << 16

// maxBodies is the most bodies a block may hold: what a cell's link to its
// body holds.
const maxBodies = 1 << 16

// maxQueued caps the cells waiting to go back that a block may hold: many
// times its cells, so it only stops damage from claiming billions.
const maxQueued = 1 << 24

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
	return nil
}

// Flags of a body as written.
const (
	flagWhole = 1 << iota
	flagAwake
	flagAll = flagWhole | flagAwake
)

// queuedBytes is how many bytes a queued cell takes before its cell's
// fields: its x and y.
const queuedBytes = 8

// writeExtras writes the queued cells and the bodies of b, compressed
// together with DEFLATE, to w. The cells' and bodies' materials are IDs of
// mats, whose order the block's material list keeps.
func writeExtras(w io.Writer, b *Block, mats *material.Table) error {
	if len(b.Queued) > maxQueued || len(b.Bodies) > maxBodies {
		return fmt.Errorf("%d queued cells and %d bodies; a block holds at most %d and %d", len(b.Queued), len(b.Bodies), maxQueued, maxBodies)
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
		var err error
		if buf, err = writeBody(bw, &b.Bodies[i], mats, buf); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
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
	buf[24] = flags
	for i, f := range [...]float32{b.Left, b.Top, b.X, b.Y, b.C, b.S, b.VX, b.VY, b.Spin} {
		binary.LittleEndian.PutUint32(buf[25+4*i:], math.Float32bits(f))
	}
	for k, p := range b.Pixels {
		at := buf[bodyHead+4*k:]
		at[0], at[1] = boolByte(p.OK), p.Tone
		binary.LittleEndian.PutUint16(at[2:], p.Damage)
	}
	_, _ = w.Write(buf)
	return buf, nil
}

// readExtras reads what writeExtras wrote, from the size bytes of r,
// into b. ids maps the block's material list onto the table it is read
// against (-1 for a material it lacks) and names names them.
func (d *decoder) readExtras(b *Block, size uint64, ids []int, names []string) error {
	if size > uint64(d.r.Len()) {
		return io.ErrUnexpectedEOF
	}
	packed := io.NewSectionReader(d.r, d.r.Size()-int64(d.r.Len()), int64(size))
	zr := flate.NewReader(packed)
	e := &extras{r: bufio.NewReader(zr), ids: ids, names: names}
	err := e.read(b, d.fields)
	if err == nil {
		if n, end := e.r.Read(make([]byte, 1)); n != 0 || !errors.Is(end, io.EOF) {
			err = errors.New("more data than its queued cells and bodies")
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

// extras reads the queued cells and bodies of a block.
type extras struct {
	r     *bufio.Reader
	ids   []int
	names []string
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
	if flags&^flagAll != 0 {
		return Body{}, fmt.Errorf("flags %#x", flags)
	}
	b.Whole, b.Awake = flags&flagWhole != 0, flags&flagAwake != 0
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
		if p[0] > 1 {
			return Body{}, fmt.Errorf("pixel %d: has is %d, not 0 or 1", k, p[0])
		}
		b.Pixels[k] = Pixel{OK: p[0] == 1, Tone: p[1], Damage: binary.LittleEndian.Uint16(p[2:])}
	}
	return b, nil
}

// compressExtras returns the extras of b as writeExtras writes them.
func compressExtras(b *Block, mats *material.Table) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeExtras(&buf, b, mats); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
