package save

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// randomQueued returns n random cells of the first m materials waiting to
// go back, from anywhere in a big world.
func randomQueued(seed uint64, n, m int) []world.Queued {
	s := rng.New(seed)
	q := make([]world.Queued, n)
	for i := range q {
		q[i] = world.Queued{
			Cell: world.Cell{
				Mat: material.ID(s.IntN(m)), Tone: uint8(s.Uint32()), Falling: s.Bool(), Last: s.Uint32(),
				Vy: fixed.Q8(int16(s.Uint32())), Ax: fixed.Q8(int16(s.Uint32())), Damage: uint16(s.Uint32()),
				Rest: uint8(s.IntN(world.SettleTicks + 1)),
			},
			X: int32(s.IntN(16384)), Y: int32(s.IntN(16384)),
		}
	}
	return q
}

// randomBodies returns n bodies of material m with random images, some
// pixels gone, random transforms and velocities, every float any bits.
func randomBodies(seed uint64, n int, m material.ID) []Body {
	s := rng.New(seed)
	f := func() float32 { return math.Float32frombits(s.Uint32()) }
	bodies := make([]Body, n)
	for i := range bodies {
		w, h := 1+s.IntN(40), 1+s.IntN(30)
		b := Body{
			Key: s.Uint32(), Mat: m, W: w, H: h, Left: f(), Top: f(), CX: s.IntN(100), CY: s.IntN(100),
			Whole: s.Bool(), X: f(), Y: f(), C: f(), S: f(), VX: f(), VY: f(), Spin: f(), Awake: s.Bool(),
			Static: s.Bool(), Pixels: make([]Pixel, w*h),
		}
		for k := range b.Pixels {
			b.Pixels[k] = Pixel{Damage: uint16(s.Uint32()), Tone: uint8(s.Uint32()), OK: s.IntN(5) > 0}
		}
		bodies[i] = b
	}
	return bodies
}

// woodTable is table with a solid, wood, after its other materials.
func woodTable(tb testing.TB, names ...string) *material.Table {
	tb.Helper()
	defs := []material.Def{}
	base := table(tb, names...)
	for i := range base.Len() {
		defs = append(defs, base.Def(material.ID(i)))
	}
	defs = append(defs, material.Def{Name: "wood", Kind: material.Solid, Density: 6})
	t, err := material.NewTable(defs)
	if err != nil {
		tb.Fatal(err)
	}
	return t
}

// A block keeps the cells that wait to go back and the bodies it owns,
// every field to the bit, and maps their materials by name as it does its
// cells'.
func TestBlockKeepsQueuedCellsAndBodies(t *testing.T) {
	saved := woodTable(t, "stone", "sand", "water")
	now := woodTable(t, "water", "oil", "sand", "stone")
	want := &Block{
		Cells:  randomBlock(5, saved.Len()-1),
		Queued: randomQueued(6, 700, saved.Len()-1),
		Bodies: randomBodies(7, 50, saved.MustLookup("wood")),
	}
	got, err := DecodeBlock(bytes.NewReader(encodeAll(t, want, saved)), now)
	if err != nil {
		t.Fatal(err)
	}
	moved := func(id material.ID) material.ID { return now.MustLookup(saved.Def(id).Name) }
	for i := range want.Cells {
		c := want.Cells[i]
		c.Mat = moved(c.Mat)
		if got.Cells[i] != c {
			t.Fatalf("cell %d is %+v after the round trip, want %+v", i, got.Cells[i], c)
		}
	}
	if len(got.Queued) != len(want.Queued) {
		t.Fatalf("%d queued cells after the round trip, want %d", len(got.Queued), len(want.Queued))
	}
	for i, q := range want.Queued {
		q.Cell.Mat = moved(q.Cell.Mat)
		if got.Queued[i] != q {
			t.Fatalf("queued cell %d is %+v after the round trip, want %+v", i, got.Queued[i], q)
		}
	}
	if len(got.Bodies) != len(want.Bodies) {
		t.Fatalf("%d bodies after the round trip, want %d", len(got.Bodies), len(want.Bodies))
	}
	for i, b := range want.Bodies {
		b.Mat = moved(b.Mat)
		g := got.Bodies[i]
		// Compare floats by their bits: some are NaN.
		for _, p := range [][2]float32{
			{g.Left, b.Left},
			{g.Top, b.Top},
			{g.X, b.X},
			{g.Y, b.Y},
			{g.C, b.C},
			{g.S, b.S},
			{g.VX, b.VX},
			{g.VY, b.VY},
			{g.Spin, b.Spin},
		} {
			if math.Float32bits(p[0]) != math.Float32bits(p[1]) {
				t.Fatalf("body %d: a float came back as %v, want %v", i, p[0], p[1])
			}
		}
		g.Left, g.Top, g.X, g.Y, g.C, g.S, g.VX, g.VY, g.Spin = 0, 0, 0, 0, 0, 0, 0, 0, 0
		b.Left, b.Top, b.X, b.Y, b.C, b.S, b.VX, b.VY, b.Spin = 0, 0, 0, 0, 0, 0, 0, 0, 0
		if !reflect.DeepEqual(g, b) {
			t.Fatalf("body %d is %+v after the round trip, want %+v", i, g, b)
		}
	}
	// A body of a material the table lacks cannot be read.
	if _, err := DecodeBlock(bytes.NewReader(encodeAll(t, want, saved)), table(t, "stone", "sand", "water")); err == nil ||
		!strings.Contains(err.Error(), `"wood"`) {
		t.Fatalf("a block with a body of wood read against a table without wood: error %v", err)
	}
}

// A block of version 2, from before queued cells and bodies were saved,
// reads back with neither.
func TestBlockReadsVersion2(t *testing.T) {
	mats := table(t, "stone", "sand", "water")
	want := randomBlock(8, mats.Len())
	var buf bytes.Buffer
	if err := encodeBlock(&buf, &Block{Cells: want}, mats, 2); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeBlock(&buf, mats)
	if err != nil {
		t.Fatal(err)
	}
	if *got.Cells != *want || got.Queued != nil || got.Bodies != nil {
		t.Fatal("a version 2 block read back different")
	}
	// Only version 3 holds them.
	if err := encodeBlock(&bytes.Buffer{}, &Block{Cells: want, Queued: randomQueued(1, 1, 2)}, mats, 2); err == nil {
		t.Fatal("a version 2 block took queued cells")
	}
}

// A block of version 3, from before static bodies, reads back with
// dynamic bodies, and cannot hold a static one: a body flagged static in
// it is damage.
func TestBlockReadsVersion3(t *testing.T) {
	mats := woodTable(t, "stone", "sand", "water")
	bodies := randomBodies(10, 20, mats.MustLookup("wood"))
	for i := range bodies {
		bodies[i].Static = false
	}
	var buf bytes.Buffer
	if err := encodeBlock(&buf, &Block{Cells: new(world.Block), Bodies: bodies}, mats, 3); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeBlock(&buf, mats)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bodies) != len(bodies) {
		t.Fatalf("%d bodies read back, want %d", len(got.Bodies), len(bodies))
	}
	for i, b := range got.Bodies {
		if b.Static || b.Key != bodies[i].Key {
			t.Fatalf("body %d read back as %+v", i, b)
		}
	}
	bodies[0].Static = true
	if err := encodeBlock(&bytes.Buffer{}, &Block{Cells: new(world.Block), Bodies: bodies}, mats, 3); err == nil {
		t.Fatal("a version 3 block took a static body")
	}
	// The same block written as version 4 and called version 3.
	data := encodeAll(t, &Block{Cells: new(world.Block), Bodies: bodies}, mats)
	binary.LittleEndian.PutUint16(data[len(blockMagic):], 3)
	if _, err := DecodeBlock(bytes.NewReader(rechecksum(data)), mats); err == nil || !strings.Contains(err.Error(), "flags") {
		t.Fatalf("a version 3 block with a static body: error %v", err)
	}
}

// Damage inside the queued cells or the bodies that the checksum did not
// catch is refused, as are bodies no format can hold.
func TestBlockRefusesBadBodies(t *testing.T) {
	mats := woodTable(t, "stone", "sand", "water")
	wood := mats.MustLookup("wood")
	body := func() Body {
		return Body{Key: 1, Mat: wood, W: 2, H: 1, Pixels: []Pixel{{OK: true}, {OK: true}}, C: 1}
	}
	for name, b := range map[string]Body{
		"pixels short of the size": {Key: 1, Mat: wood, W: 2, H: 2, Pixels: []Pixel{{}}},
		"no width":                 {Key: 1, Mat: wood, W: 0, H: 1},
		"too many pixels":          {Key: 1, Mat: wood, W: 1 << 9, H: 1 << 8, Pixels: make([]Pixel, 1<<17)},
		"a material not in the table": func() Body {
			b := body()
			b.Mat = 99
			return b
		}(),
	} {
		if err := EncodeBlock(&bytes.Buffer{}, &Block{Cells: new(world.Block), Bodies: []Body{b}}, mats); err == nil {
			t.Errorf("%s: EncodeBlock took the body", name)
		}
	}
	data := encodeAll(t, &Block{Cells: new(world.Block), Bodies: []Body{body()}}, mats)
	// The extras length in the head must match what follows.
	bad := bytes.Clone(data)
	n := binary.LittleEndian.Uint64(bad[headBytes : headBytes+8])
	binary.LittleEndian.PutUint64(bad[headBytes:headBytes+8], n+1)
	if _, err := DecodeBlock(bytes.NewReader(rechecksum(bad)), mats); err == nil {
		t.Fatal("a block whose head misstates the extras' size read")
	}
}

// BenchmarkEncodeBlockWithBodies and BenchmarkDecodeBlockWithBodies time
// the typical block with 450 bodies of 16×12 pixels and 400 cells waiting
// to go back, the heavy scene's count in one block.
func BenchmarkEncodeBlockWithBodies(b *testing.B) {
	mats := woodTable(b, "stone", "sand", "water")
	blk := &Block{Cells: typicalBlock(mats), Queued: randomQueued(1, 400, 4), Bodies: bodiesOf(450, mats.MustLookup("wood"))}
	var buf bytes.Buffer
	b.ReportAllocs()
	for range b.N {
		buf.Reset()
		if err := EncodeBlock(&buf, blk, mats); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(buf.Len()), "bytes/block")
}

func BenchmarkDecodeBlockWithBodies(b *testing.B) {
	mats := woodTable(b, "stone", "sand", "water")
	data := encodeAll(b, &Block{Cells: typicalBlock(mats), Queued: randomQueued(1, 400, 4), Bodies: bodiesOf(450, mats.MustLookup("wood"))}, mats)
	b.ReportAllocs()
	for range b.N {
		if _, err := DecodeBlock(bytes.NewReader(data), mats); err != nil {
			b.Fatal(err)
		}
	}
}

// bodiesOf returns n whole boxes of 16×12 pixels of material m in a row,
// as the body tool drops them, with tones of their own.
func bodiesOf(n int, m material.ID) []Body {
	s := rng.New(9)
	bodies := make([]Body, n)
	for i := range bodies {
		px := make([]Pixel, 16*12)
		for k := range px {
			px[k] = Pixel{Tone: uint8(s.Uint32()), OK: true}
		}
		bodies[i] = Body{
			Key: uint32(i), Mat: m, W: 16, H: 12, Left: -8, Top: -6, Pixels: px, Whole: true,
			X: float32(i%30) * 1.6, Y: float32(i/30) * 1.2, C: 1, Awake: i%3 == 0,
		}
	}
	return bodies
}
