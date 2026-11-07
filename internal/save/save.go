// Package save encodes a world for saving: one stream per block of cells
// (EncodeBlock) and a short header for the whole world (WriteMeta).
//
// A block is written field by field, never as the bytes of the cells in
// memory, so a change to world.Cell does not break old saves: a new layout
// raises the version, and a reader refuses versions it does not know
// rather than misread them. Each field of all the block's cells is one
// plane, compressed on its own, which compresses well because neighboring
// cells are alike. The stream is:
//
//	"mw-block"            8 bytes
//	version               uint16
//	materials size        uint64, the bytes of the material list below
//	materials             uint32 count, then each name: uint32 length, bytes
//	fields                uint8 count, then each field:
//	                        name: uint8 length, bytes
//	                        bytes per cell: uint8
//	                        compressed size: uint32
//	                        the field of every cell, row by row,
//	                        little-endian, compressed with DEFLATE
//	checksum              uint32, CRC-32 (IEEE) of everything before it
//
// Integers are little-endian. Cells name their material by its index in
// the block's own list of material names, so a block reads correctly
// against a table whose materials moved or grew.
//
// The world header is line-based text, like a replay:
//
//	matter-wake-world 1
//	seed 1
//	size 16384 16384
//	tick 3600
//
// The package reads and writes streams; the app layer opens the files.
package save

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/world"
)

// BlockVersion is the block layout this build writes. It also reads
// version 1, which had no rest field: its cells read back with Rest 0, so
// resting powders count their ticks at rest again.
const BlockVersion = 2

// MetaVersion is the world header layout this build reads and writes.
const MetaVersion = 1

const (
	blockMagic = "mw-block"
	metaMagic  = "matter-wake-world"
	// cells is how many cells a block holds.
	cells = world.BlockSize * world.BlockSize
)

// field is one field of world.Cell as a block stores it: its name, how
// many bytes it takes, and how to move it between a cell and those bytes.
type field struct {
	name  string
	width int
	put   func(b []byte, c *world.Cell)
	get   func(b []byte, c *world.Cell) error
}

// fields are the fields of a block of BlockVersion, in the order written;
// a block of version 1 has all but the last (fieldsOf). Mat is written as
// an index into the block's material list; put and get see that index in
// place of the material ID.
var fields = []field{
	{
		"mat", 2,
		func(b []byte, c *world.Cell) { binary.LittleEndian.PutUint16(b, uint16(c.Mat)) },
		func(b []byte, c *world.Cell) error { c.Mat = material.ID(binary.LittleEndian.Uint16(b)); return nil },
	},
	{
		"tone", 1,
		func(b []byte, c *world.Cell) { b[0] = c.Tone },
		func(b []byte, c *world.Cell) error { c.Tone = b[0]; return nil },
	},
	{
		"falling", 1,
		func(b []byte, c *world.Cell) { b[0] = boolByte(c.Falling) },
		func(b []byte, c *world.Cell) error {
			if b[0] > 1 {
				return fmt.Errorf("falling is %d, not 0 or 1", b[0])
			}
			c.Falling = b[0] == 1
			return nil
		},
	},
	{
		"last", 4,
		func(b []byte, c *world.Cell) { binary.LittleEndian.PutUint32(b, c.Last) },
		func(b []byte, c *world.Cell) error { c.Last = binary.LittleEndian.Uint32(b); return nil },
	},
	{
		"vy", 2,
		func(b []byte, c *world.Cell) { binary.LittleEndian.PutUint16(b, uint16(c.Vy)) },
		func(b []byte, c *world.Cell) error { c.Vy = fixed.Q8(int16(binary.LittleEndian.Uint16(b))); return nil },
	},
	{
		"ax", 2,
		func(b []byte, c *world.Cell) { binary.LittleEndian.PutUint16(b, uint16(c.Ax)) },
		func(b []byte, c *world.Cell) error { c.Ax = fixed.Q8(int16(binary.LittleEndian.Uint16(b))); return nil },
	},
	{
		"damage", 2,
		func(b []byte, c *world.Cell) { binary.LittleEndian.PutUint16(b, c.Damage) },
		func(b []byte, c *world.Cell) error { c.Damage = binary.LittleEndian.Uint16(b); return nil },
	},
	{
		"rest", 1,
		func(b []byte, c *world.Cell) { b[0] = c.Rest },
		func(b []byte, c *world.Cell) error {
			if b[0] > world.SettleTicks {
				return fmt.Errorf("rest is %d, above %d", b[0], world.SettleTicks)
			}
			c.Rest = b[0]
			return nil
		},
	},
}

// fieldsOf returns the fields a block of the given version holds.
func fieldsOf(version uint16) []field {
	if version == 1 {
		return fields[:len(fields)-1]
	}
	return fields
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// EncodeBlock writes the cells of b, whose materials come from mats.
func EncodeBlock(w io.Writer, b *world.Block, mats *material.Table) error {
	return encodeBlock(w, b, mats, BlockVersion)
}

// encodeBlock writes b in the layout of the given version; tests write
// older versions with it.
func encodeBlock(w io.Writer, b *world.Block, mats *material.Table, version uint16) error {
	fields := fieldsOf(version)
	sum := crc32.NewIEEE()
	bw := bufio.NewWriter(io.MultiWriter(w, sum))
	// A bufio.Writer keeps its first error and Flush returns it.
	_, _ = bw.WriteString(blockMagic)
	writeUint(bw, uint64(version), 2)
	// A table holds up to material.MaxMaterials materials, with names of
	// any length, so both take 32 bits. The list's size comes first, so a
	// reader knows how much to expect besides the cells.
	var list bytes.Buffer
	lw := bufio.NewWriter(&list)
	writeUint(lw, uint64(mats.Len()), 4)
	for i := range mats.Len() {
		name := mats.Def(material.ID(i)).Name
		writeUint(lw, uint64(len(name)), 4)
		_, _ = lw.WriteString(name)
	}
	_ = lw.Flush() // a bytes.Buffer does not fail
	writeUint(bw, uint64(list.Len()), 8)
	_, _ = bw.Write(list.Bytes())
	writeUint(bw, uint64(len(fields)), 1)
	raw := make([]byte, 4*cells)
	var packed bytes.Buffer
	for _, f := range fields {
		raw := raw[:f.width*cells]
		for i := range b {
			f.put(raw[i*f.width:], &b[i])
		}
		packed.Reset()
		zw, err := flate.NewWriter(&packed, flate.BestSpeed)
		if err != nil {
			return fmt.Errorf("save: %w", err)
		}
		if _, err := zw.Write(raw); err != nil {
			return fmt.Errorf("save: %w", err)
		}
		if err := zw.Close(); err != nil {
			return fmt.Errorf("save: %w", err)
		}
		writeName(bw, f.name)
		writeUint(bw, uint64(f.width), 1)
		writeUint(bw, uint64(packed.Len()), 4)
		_, _ = bw.Write(packed.Bytes())
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	var tail [4]byte
	binary.LittleEndian.PutUint32(tail[:], sum.Sum32())
	if _, err := w.Write(tail[:]); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

func writeUint(w *bufio.Writer, v uint64, size int) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	_, _ = w.Write(b[:size])
}

func writeName(w *bufio.Writer, name string) {
	writeUint(w, uint64(len(name)), 1)
	_, _ = w.WriteString(name)
}

// headBytes is how long a block's head is: its magic, version and the
// size of its material list.
const headBytes = len(blockMagic) + 2 + 8

// maxCellBytes caps how much DecodeBlock reads besides the head and the
// material list: well above the most the cells take, every field of every
// cell uncompressed.
const maxCellBytes = 8 << 20

// DecodeBlock reads a block that EncodeBlock wrote, and maps its materials
// by name onto mats. It fails on another format or version, on damaged
// data, and when a cell holds a material mats does not have.
func DecodeBlock(r io.Reader, mats *material.Table) (*world.Block, error) {
	data, err := readBlock(r)
	if err != nil {
		return nil, fmt.Errorf("save: block: %w", err)
	}
	body := data[:len(data)-4]
	if got, want := binary.LittleEndian.Uint32(data[len(body):]), crc32.ChecksumIEEE(body); got != want {
		return nil, fmt.Errorf("save: block: checksum %08x, the data hashes to %08x: the block is damaged or cut short", got, want)
	}
	// A bytes.Reader reads byte by byte, so DEFLATE never reads past its
	// field.
	d := &decoder{r: bytes.NewReader(body[headBytes:]), fields: fieldsOf(binary.LittleEndian.Uint16(data[len(blockMagic):]))}
	b, err := d.block(mats, binary.LittleEndian.Uint64(data[headBytes-8:]))
	if err != nil {
		return nil, fmt.Errorf("save: block: %w", err)
	}
	if d.r.Len() != 0 {
		return nil, errors.New("save: block: data after the last field")
	}
	return b, nil
}

// readBlock reads a whole block: its head, then as much as the head's
// material list size and maxCellBytes allow, which must end in a checksum.
func readBlock(r io.Reader) ([]byte, error) {
	head := make([]byte, headBytes)
	n, err := io.ReadFull(r, head)
	if !bytes.HasPrefix(head[:n], []byte(blockMagic)) || n < len(blockMagic) {
		return nil, errors.New("not a matter-wake block")
	}
	if n >= len(blockMagic)+2 {
		if v := binary.LittleEndian.Uint16(head[len(blockMagic):]); v < 1 || v > BlockVersion {
			return nil, fmt.Errorf("format version %d; this build reads versions 1 to %d", v, BlockVersion)
		}
	}
	if err != nil {
		return nil, noEOF(err)
	}
	limit := binary.LittleEndian.Uint64(head[headBytes-8:]) + maxCellBytes + 4
	rest, err := io.ReadAll(io.LimitReader(r, int64(min(limit, math.MaxInt64-1))+1))
	switch {
	case err != nil:
		return nil, err
	case uint64(len(rest)) > limit:
		return nil, fmt.Errorf("more than %d bytes after the head", limit)
	case len(rest) < 4:
		return nil, io.ErrUnexpectedEOF
	}
	return append(head, rest...), nil
}

// decoder reads the parts of a block after its version, which holds
// fields.
type decoder struct {
	r      *bytes.Reader
	fields []field
}

func (d *decoder) block(mats *material.Table, listBytes uint64) (*world.Block, error) {
	before := d.r.Len()
	ids, names, err := d.materials(mats)
	if err != nil {
		return nil, err
	}
	if read := uint64(before - d.r.Len()); read != listBytes {
		return nil, fmt.Errorf("the material list takes %d bytes, the head says %d", read, listBytes)
	}
	n, err := d.uint(1)
	if err != nil {
		return nil, err
	}
	if int(n) != len(d.fields) {
		return nil, fmt.Errorf("%d fields, want %d", n, len(d.fields))
	}
	b := new(world.Block)
	raw := make([]byte, 4*cells)
	for _, f := range d.fields {
		if err := d.field(f, raw[:f.width*cells]); err != nil {
			return nil, fmt.Errorf("field %q: %w", f.name, err)
		}
		for i := range b {
			if err := f.get(raw[i*f.width:], &b[i]); err != nil {
				return nil, fmt.Errorf("cell %d: %w", i, err)
			}
		}
	}
	for i := range b {
		k := int(b[i].Mat)
		if k >= len(ids) {
			return nil, fmt.Errorf("cell %d holds material %d of a list of %d", i, k, len(ids))
		}
		if ids[k] < 0 {
			return nil, fmt.Errorf("cell %d holds %q, which the material table does not have", i, names[k])
		}
		b[i].Mat = material.ID(ids[k])
	}
	return b, nil
}

// materials reads the block's material names and returns, for each, its
// ID in mats or -1.
func (d *decoder) materials(mats *material.Table) (ids []int, names []string, err error) {
	n, err := d.uint(4)
	if err != nil {
		return nil, nil, err
	}
	if n > material.MaxMaterials {
		return nil, nil, fmt.Errorf("%d materials, at most %d", n, material.MaxMaterials)
	}
	for range n {
		name, err := d.text(4)
		if err != nil {
			return nil, nil, err
		}
		id := -1
		if m, ok := mats.Lookup(name); ok {
			id = int(m)
		}
		ids, names = append(ids, id), append(names, name)
	}
	return ids, names, nil
}

// field reads the field f of every cell into raw.
func (d *decoder) field(f field, raw []byte) error {
	name, err := d.text(1)
	if err != nil {
		return err
	}
	if name != f.name {
		return fmt.Errorf("found field %q", name)
	}
	width, err := d.uint(1)
	if err != nil {
		return err
	}
	if int(width) != f.width {
		return fmt.Errorf("%d bytes per cell, want %d", width, f.width)
	}
	size, err := d.uint(4)
	if err != nil {
		return err
	}
	// DEFLATE never grows data by more than a few bytes per 64 KiB block,
	// so a bigger size is damage, not data.
	if size > uint64(len(raw)+len(raw)/64+64) {
		return fmt.Errorf("compressed size %d is more than its %d bytes can take", size, len(raw))
	}
	if size > uint64(d.r.Len()) {
		return io.ErrUnexpectedEOF
	}
	packed := io.NewSectionReader(d.r, d.r.Size()-int64(d.r.Len()), int64(size))
	zr := flate.NewReader(packed)
	_, err = io.ReadFull(zr, raw)
	if err != nil {
		err = noEOF(err)
	} else if n, end := zr.Read(make([]byte, 1)); n != 0 || !errors.Is(end, io.EOF) {
		err = errors.New("more data than its cells")
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

func (d *decoder) uint(size int) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(d.r, b[:size]); err != nil {
		return 0, noEOF(err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

// text reads a string after its length, which takes size bytes.
func (d *decoder) text(size int) (string, error) {
	n, err := d.uint(size)
	if err != nil {
		return "", err
	}
	if n > uint64(d.r.Len()) {
		return "", io.ErrUnexpectedEOF
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(d.r, b); err != nil {
		return "", noEOF(err)
	}
	return string(b), nil
}

// noEOF turns a plain end of stream into ErrUnexpectedEOF: in the middle of
// a block, it means the block was cut short.
func noEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// Meta is what a save holds about the whole world, besides its blocks.
type Meta struct {
	Seed          uint64
	Width, Height int
	// Tick is how many ticks the world had run, so a loaded world goes on
	// with the same random numbers (world.Config.Tick).
	Tick uint32
}

// WriteMeta writes m as the world header.
func WriteMeta(w io.Writer, m Meta) error {
	if m.Width <= 0 || m.Height <= 0 {
		return fmt.Errorf("save: world size %dx%d is not positive", m.Width, m.Height)
	}
	if _, err := fmt.Fprintf(w, "%s %d\nseed %d\nsize %d %d\ntick %d\n",
		metaMagic, MetaVersion, m.Seed, m.Width, m.Height, m.Tick); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// ReadMeta reads a world header that WriteMeta wrote. It refuses other
// versions and lines it does not expect.
func ReadMeta(r io.Reader) (Meta, error) {
	l := newLines(r)
	if v := l.uint("version", l.next(metaMagic, 1)[0], 16); l.err == nil && v != MetaVersion {
		l.err = fmt.Errorf("format version %d; this build reads version %d", v, MetaVersion)
	}
	m := Meta{Seed: l.uint("seed", l.next("seed", 1)[0], 64)}
	size := l.next("size", 2)
	width, height := l.uint("width", size[0], 31), l.uint("height", size[1], 31)
	if l.err == nil && (width == 0 || height == 0) {
		l.err = fmt.Errorf("world size %dx%d is not positive", width, height)
	}
	m.Width, m.Height = int(width), int(height)
	m.Tick = uint32(l.uint("tick", l.next("tick", 1)[0], 32))
	l.end()
	if l.err != nil {
		return Meta{}, fmt.Errorf("save: world header: %w", l.err)
	}
	return m, nil
}

// lines reads a header of "key values..." lines in a fixed order. It keeps
// the first error, and after one returns zero values. Lines may be of any
// length: a material name has no limit.
type lines struct {
	r    *bufio.Reader
	line string
	err  error
}

func newLines(r io.Reader) *lines {
	return &lines{r: bufio.NewReader(r)}
}

// scan reads the next line into l.line, without its line end, and reports
// whether there was one.
func (l *lines) scan() bool {
	s, err := l.r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		l.err = err
		return false
	}
	if s == "" {
		return false
	}
	l.line = strings.TrimRight(s, "\r\n")
	return true
}

// next returns the n values of the next line, which must start with key.
func (l *lines) next(key string, n int) []string {
	values := make([]string, n)
	if l.err != nil {
		return values
	}
	if !l.scan() {
		if l.err == nil {
			l.err = fmt.Errorf("missing %q line", key)
		}
		return values
	}
	f := strings.Fields(l.line)
	if len(f) != n+1 || f[0] != key {
		l.err = fmt.Errorf("line %q, want %q and %d values", l.line, key, n)
		return values
	}
	return f[1:]
}

// uint parses the value v of name as an unsigned number of the given bits.
func (l *lines) uint(name, v string, bits int) uint64 {
	if l.err != nil {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, bits)
	if err != nil {
		l.err = fmt.Errorf("%s %q is not a number of %d bits", name, v, bits)
	}
	return n
}

// int parses the value v of name as a signed number.
func (l *lines) int(name, v string) int {
	if l.err != nil {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.err = fmt.Errorf("%s %q is not a number", name, v)
	}
	return n
}

// end checks that no line follows.
func (l *lines) end() {
	if l.err == nil && l.scan() {
		l.err = fmt.Errorf("unexpected line %q", l.line)
	}
}

// SessionVersion is the session file layout this build writes. It reads
// version 1, which had no stream line, and version 2, which had no chunk
// lines.
const SessionVersion = 3

const sessionMagic = "matter-wake-session"

// Session is what a save holds about the session besides the world: where
// the camera was, the selected slot, what the inventory held by material
// name, and which blocks were in memory, so a reopened session goes on as
// the saved one would have.
//
// It is line-based text, like the world header:
//
//	matter-wake-session 3
//	camera 7952 955
//	slot 3
//	stream 7872 832 608 398 4200 60
//	held "sand" 120
//	held "stone" 15
//	block 15 1
//	block 16 1
//	chunk 130 9 8320 576 8384 600 0 0 0 0 3
//
// The stream line holds the camera's last area (x, y, width, height), the
// tick of the last check and the ticks to the next; a session that had
// none has no stream line. Only materials the inventory holds get a held
// line, in name order, the name quoted as in Go, since it may hold
// spaces; the blocks follow in row order. Last come the chunks of those
// blocks that had something to scan or had waited for the budget
// (world.ChunkState), in row order: the chunk's x and y, what it scans
// next and what it scans again (x0 y0 x1 y1 each, all 0 when nothing),
// and the ticks it has waited.
type Session struct {
	CameraX, CameraY int
	// Slot is the selected slot, 1-based as the keys are.
	Slot int
	// Stream is where the stream's requests and checks stood, or nil.
	Stream *SessionStream
	// Held is how many cells of each material the inventory holds.
	Held map[string]int
	// Blocks lists the blocks that were in memory, as (bx, by).
	Blocks [][2]int
	// Chunks lists the chunks of those blocks that had something to scan
	// or had waited, and Scheduled is set when the file records them
	// (version 3 on): then the chunks not listed had nothing to scan. An
	// older file says nothing about the chunks.
	Chunks    []SessionChunk
	Scheduled bool
}

// SessionChunk is where one chunk stood (world.ChunkState): what it had
// left to scan and scan again, as x0, y0, x1, y1, and how many ticks it had
// waited for the budget.
type SessionChunk struct {
	CX, CY      int
	Dirty, Keep [4]int
	Waited      int
}

// maxWaited is the longest wait a chunk records (world.ChunkState).
const maxWaited = 1<<32 - 1

// valid reports what is wrong with c, or nil.
func (c *SessionChunk) valid() error {
	if c.CX < 0 || c.CY < 0 {
		return fmt.Errorf("chunk (%d, %d) is negative", c.CX, c.CY)
	}
	for _, v := range append(c.Dirty[:], c.Keep[:]...) {
		if v < 0 {
			return fmt.Errorf("chunk (%d, %d) has a negative corner", c.CX, c.CY)
		}
	}
	if c.Waited < 0 || c.Waited > maxWaited {
		return fmt.Errorf("chunk (%d, %d) waited %d ticks, want 0 to %d", c.CX, c.CY, c.Waited, maxWaited)
	}
	return nil
}

// SessionStream is where a session's stream stood: the camera's last area
// as x, y, width and height in cells, the tick of its last check, and the
// ticks from that check to the next.
type SessionStream struct {
	Area      [4]int
	LastCheck uint32
	Interval  int
}

// valid reports what is wrong with s, or nil.
func (s *SessionStream) valid() error {
	switch {
	case s.Area[2] < 0 || s.Area[3] < 0:
		return fmt.Errorf("stream area width and height %d, %d are negative", s.Area[2], s.Area[3])
	case s.Interval < 1:
		return fmt.Errorf("stream interval %d is not 1 or more", s.Interval)
	}
	return nil
}

// WriteSession writes s.
func WriteSession(w io.Writer, s *Session) error {
	if s.Slot < 1 {
		return fmt.Errorf("save: slot %d is not 1 or more", s.Slot)
	}
	bw := bufio.NewWriter(w)
	// A bufio.Writer keeps its first error and Flush returns it.
	_, _ = fmt.Fprintf(bw, "%s %d\ncamera %d %d\nslot %d\n", sessionMagic, SessionVersion, s.CameraX, s.CameraY, s.Slot)
	if st := s.Stream; st != nil {
		if err := st.valid(); err != nil {
			return fmt.Errorf("save: %w", err)
		}
		_, _ = fmt.Fprintf(bw, "stream %d %d %d %d %d %d\n", st.Area[0], st.Area[1], st.Area[2], st.Area[3], st.LastCheck, st.Interval)
	}
	names := make([]string, 0, len(s.Held))
	for name, n := range s.Held {
		if n < 0 || name == "" {
			return fmt.Errorf("save: holding %d of %q", n, name)
		}
		if n > 0 {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		// Quoted, since a material name may hold spaces or anything else.
		_, _ = fmt.Fprintf(bw, "held %s %d\n", strconv.Quote(name), s.Held[name])
	}
	blocks := slices.Clone(s.Blocks)
	slices.SortFunc(blocks, func(a, b [2]int) int {
		if a[1] != b[1] {
			return a[1] - b[1]
		}
		return a[0] - b[0]
	})
	for i, b := range blocks {
		if b[0] < 0 || b[1] < 0 || i > 0 && b == blocks[i-1] {
			return fmt.Errorf("save: block %v", b)
		}
		_, _ = fmt.Fprintf(bw, "block %d %d\n", b[0], b[1])
	}
	if err := writeChunks(bw, s.Chunks); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// writeChunks writes a chunk line for each of chunks, in row order.
func writeChunks(w io.Writer, chunks []SessionChunk) error {
	chunks = slices.Clone(chunks)
	slices.SortFunc(chunks, func(a, b SessionChunk) int {
		if a.CY != b.CY {
			return a.CY - b.CY
		}
		return a.CX - b.CX
	})
	for i, c := range chunks {
		if err := c.valid(); err != nil {
			return fmt.Errorf("save: %w", err)
		}
		if i > 0 && c.CX == chunks[i-1].CX && c.CY == chunks[i-1].CY {
			return fmt.Errorf("save: chunk (%d, %d) is listed twice", c.CX, c.CY)
		}
		d, k := c.Dirty, c.Keep
		_, _ = fmt.Fprintf(w, "chunk %d %d %d %d %d %d %d %d %d %d %d\n",
			c.CX, c.CY, d[0], d[1], d[2], d[3], k[0], k[1], k[2], k[3], c.Waited)
	}
	return nil
}

// held reads the rest of a held line, a quoted material name and a count,
// into held.
func (l *lines) held(held map[string]int, rest string) {
	quoted, err := strconv.QuotedPrefix(rest)
	if err != nil {
		l.err = fmt.Errorf("line %q, want %q", l.line, `held "<material>" <count>`)
		return
	}
	name, err := strconv.Unquote(quoted)
	f := strings.Fields(rest[len(quoted):])
	if err != nil || len(f) != 1 {
		l.err = fmt.Errorf("line %q, want %q", l.line, `held "<material>" <count>`)
		return
	}
	if _, dup := held[name]; dup {
		l.err = fmt.Errorf("%q is held twice", name)
		return
	}
	held[name] = int(l.uint("count of "+quoted, f[0], 62))
}

// stream reads the fields of a stream line into s.
func (l *lines) stream(s *Session, f []string) {
	if len(f) != 6 {
		l.err = fmt.Errorf("line %q, want %q", l.line, "stream <x> <y> <width> <height> <last check> <interval>")
		return
	}
	if s.Stream != nil {
		l.err = errors.New("the stream line is there twice")
		return
	}
	st := &SessionStream{
		Area:      [4]int{l.int("stream x", f[0]), l.int("stream y", f[1]), l.int("stream width", f[2]), l.int("stream height", f[3])},
		LastCheck: uint32(l.uint("stream last check", f[4], 32)),
		Interval:  int(l.uint("stream interval", f[5], 31)),
	}
	if l.err == nil {
		l.err = st.valid()
	}
	s.Stream = st
}

// chunk reads the fields of a chunk line into s; seen holds the chunks
// read so far.
func (l *lines) chunk(s *Session, f []string, seen map[[2]int]bool) {
	if len(f) != 11 {
		l.err = fmt.Errorf("line %q, want %q", l.line, "chunk <x> <y> <x0> <y0> <x1> <y1> <keep x0> <keep y0> <keep x1> <keep y1> <waited>")
		return
	}
	c := SessionChunk{CX: int(l.uint("chunk x", f[0], 31)), CY: int(l.uint("chunk y", f[1], 31))}
	for i := range 4 {
		c.Dirty[i] = int(l.uint("chunk corner", f[2+i], 31))
		c.Keep[i] = int(l.uint("chunk corner", f[6+i], 31))
	}
	c.Waited = int(l.uint("chunk waited", f[10], 32))
	if l.err != nil {
		return
	}
	if l.err = c.valid(); l.err != nil {
		return
	}
	if seen[[2]int{c.CX, c.CY}] {
		l.err = fmt.Errorf("chunk (%d, %d) is listed twice", c.CX, c.CY)
		return
	}
	seen[[2]int{c.CX, c.CY}] = true
	s.Chunks = append(s.Chunks, c)
}

// ReadSession reads a session file that WriteSession wrote, or one of an
// older version.
func ReadSession(r io.Reader) (*Session, error) {
	l := newLines(r)
	v := l.uint("version", l.next(sessionMagic, 1)[0], 16)
	if l.err == nil && (v < 1 || v > SessionVersion) {
		l.err = fmt.Errorf("format version %d; this build reads versions 1 to %d", v, SessionVersion)
	}
	camera := l.next("camera", 2)
	s := &Session{Held: map[string]int{}, Scheduled: v >= 3}
	s.CameraX, s.CameraY = l.int("camera x", camera[0]), l.int("camera y", camera[1])
	if s.Slot = l.int("slot", l.next("slot", 1)[0]); l.err == nil && s.Slot < 1 {
		l.err = fmt.Errorf("slot %d is not 1 or more", s.Slot)
	}
	seen := map[[2]int]bool{}
	chunks := map[[2]int]bool{}
	for l.err == nil && l.scan() {
		f := strings.Fields(l.line)
		switch {
		case strings.HasPrefix(l.line, "held "):
			l.held(s.Held, strings.TrimPrefix(l.line, "held "))
		case len(f) > 0 && f[0] == "stream":
			if v < 2 {
				l.err = fmt.Errorf("line %q: a version 1 session has no stream line", l.line)
				break
			}
			l.stream(s, f[1:])
		case len(f) > 0 && f[0] == "chunk":
			if v < 3 {
				l.err = fmt.Errorf("line %q: a version %d session has no chunk lines", l.line, v)
				break
			}
			l.chunk(s, f[1:], chunks)
		case len(f) == 3 && f[0] == "block":
			b := [2]int{int(l.uint("block x", f[1], 31)), int(l.uint("block y", f[2], 31))}
			if seen[b] {
				l.err = fmt.Errorf("block %v is listed twice", b)
				break
			}
			seen[b] = true
			s.Blocks = append(s.Blocks, b)
		default:
			l.err = fmt.Errorf("line %q, want %q or %q", l.line, `held "<material>" <count>`, "block <x> <y>")
		}
	}
	if l.err != nil {
		return nil, fmt.Errorf("save: session: %w", l.err)
	}
	return s, nil
}
