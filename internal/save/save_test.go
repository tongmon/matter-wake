package save

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"strconv"
	"strings"
	"testing"

	"github.com/tongmon/matter-wake/internal/fixed"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/rng"
	"github.com/tongmon/matter-wake/internal/world"
)

// table builds a material table with the named materials in that order:
// air, and any of stone, sand, water and oil.
func table(tb testing.TB, names ...string) *material.Table {
	tb.Helper()
	q := func(f float64) fixed.Q8 {
		v, err := fixed.FromFloat(f)
		if err != nil {
			tb.Fatal(err)
		}
		return v
	}
	all := map[string]material.Def{
		"stone": {Name: "stone", Kind: material.Static, Density: 255, Hardness: 10, HP: 100000},
		"sand":  {Name: "sand", Kind: material.Powder, Density: 30, FallAccel: q(0.225), SinkChance: 25, SlideChance: 50, Hardness: 4, HP: 800},
		"water": {Name: "water", Kind: material.Liquid, Density: 20, FallAccel: q(0.375), SinkChance: 50, FlowSpeed: q(0.9), FlowDamping: q(0.8), Dispersion: 4},
		"oil":   {Name: "oil", Kind: material.Liquid, Density: 15, FallAccel: q(0.375), SinkChance: 50, FlowSpeed: q(0.9), FlowDamping: q(0.8), Dispersion: 4},
	}
	defs := []material.Def{{Name: "air", Kind: material.Empty}}
	for _, n := range names {
		defs = append(defs, all[n])
	}
	t, err := material.NewTable(defs)
	if err != nil {
		tb.Fatal(err)
	}
	return t
}

// randomBlock fills a block with random cells of the first n materials.
func randomBlock(seed uint64, n int) *world.Block {
	s := rng.New(seed)
	b := new(world.Block)
	for i := range b {
		b[i] = world.Cell{
			Mat: material.ID(s.IntN(n)), Tone: uint8(s.Uint32()), Falling: s.Bool(), Last: s.Uint32(),
			Vy: fixed.Q8(int16(s.Uint32())), Ax: fixed.Q8(int16(s.Uint32())), Damage: uint16(s.Uint32()),
		}
	}
	return b
}

func encode(tb testing.TB, b *world.Block, mats *material.Table) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if err := EncodeBlock(&buf, b, mats); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func TestBlockRoundTrip(t *testing.T) {
	mats := table(t, "stone", "sand", "water")
	want := randomBlock(1, mats.Len())
	got, err := DecodeBlock(bytes.NewReader(encode(t, want, mats)), mats)
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cell %d is %+v after the round trip, want %+v", i, got[i], want[i])
		}
	}
}

// A block names its materials, so it reads right against a table whose
// materials moved or grew.
func TestBlockMapsMaterialsByName(t *testing.T) {
	saved := table(t, "stone", "sand", "water")
	now := table(t, "water", "oil", "sand", "stone")
	b := randomBlock(2, saved.Len())
	got, err := DecodeBlock(bytes.NewReader(encode(t, b, saved)), now)
	if err != nil {
		t.Fatal(err)
	}
	for i := range b {
		want := now.MustLookup(saved.Def(b[i].Mat).Name)
		if got[i].Mat != want {
			t.Fatalf("cell %d holds %q, read as ID %d, want %d", i, saved.Def(b[i].Mat).Name, got[i].Mat, want)
		}
	}
}

// A material the table lacks is an error only when a cell holds it.
func TestBlockNeedsTheMaterialsItHolds(t *testing.T) {
	saved := table(t, "stone", "sand", "water")
	now := table(t, "stone", "sand")
	b := new(world.Block)
	b[7].Mat = saved.MustLookup("sand")
	if _, err := DecodeBlock(bytes.NewReader(encode(t, b, saved)), now); err != nil {
		t.Fatalf("a block without water failed to read against a table without water: %v", err)
	}
	b[9].Mat = saved.MustLookup("water")
	_, err := DecodeBlock(bytes.NewReader(encode(t, b, saved)), now)
	if err == nil || !strings.Contains(err.Error(), `"water"`) {
		t.Fatalf("a block holding water read against a table without it: error %v", err)
	}
}

func TestBlockRefusesBadData(t *testing.T) {
	mats := table(t, "stone", "sand", "water")
	data := encode(t, randomBlock(3, mats.Len()), mats)
	edit := func(f func(b []byte) []byte) []byte { return f(bytes.Clone(data)) }
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"empty", nil, "not a matter-wake block"},
		{"other format", []byte("matter-wake-replay 2\n"), "not a matter-wake block"},
		{"newer version", edit(func(b []byte) []byte { b[8] = BlockVersion + 1; return b }), "format version"},
		{"cut short", data[:len(data)/2], "damaged or cut short"},
		{"no checksum", data[:len(data)-4], "damaged or cut short"},
		{"only the header", data[:len(blockMagic)+2], "unexpected EOF"},
		{"flipped bit", edit(func(b []byte) []byte { b[len(b)/2] ^= 1; return b }), ""},
		{"wrong checksum", edit(func(b []byte) []byte { b[len(b)-1] ^= 1; return b }), "damaged"},
		{"trailing data", append(bytes.Clone(data), 0), "damaged or cut short"},
		{"data after the fields", rechecksum(append(bytes.Clone(data[:len(data)-4]), 0, 0, 0, 0, 0)), "after the last field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeBlock(bytes.NewReader(tt.data), mats)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("DecodeBlock error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

// rechecksum writes into the last four bytes of b the checksum of the
// bytes before them, as EncodeBlock does.
func rechecksum(b []byte) []byte {
	body := b[:len(b)-4]
	binary.LittleEndian.PutUint32(b[len(body):], crc32.ChecksumIEEE(body))
	return b
}

// A stored Falling must be 0 or 1; anything else is damage the checksum
// did not catch.
func TestFallingMustBeZeroOrOne(t *testing.T) {
	falling := fields[2]
	if falling.name != "falling" {
		t.Fatalf("fields[2] is %q", falling.name)
	}
	var c world.Cell
	for v, want := range []bool{false, true} {
		if err := falling.get([]byte{byte(v)}, &c); err != nil || c.Falling != want {
			t.Fatalf("falling %d read as %v, %v", v, c.Falling, err)
		}
	}
	if err := falling.get([]byte{2}, &c); err == nil {
		t.Fatal("falling 2 read without an error")
	}
}

// A world saved block by block and loaded again goes on exactly as the
// world that was never saved. It is 2×2 blocks, those at the right and
// bottom only partly in the world.
func TestSavedWorldGoesOnTheSame(t *testing.T) {
	mats := table(t, "stone", "sand", "water")
	cfg := world.Config{Width: 600, Height: 600, Seed: 9, Workers: 2, Materials: mats}
	kept, err := world.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer kept.Close()
	s := rng.New(4)
	for y := range 300 {
		for x := range 600 {
			if n := s.IntN(10); n < 3 {
				kept.Set(x, y, material.ID(2+n%2))
			}
		}
	}
	kept.Fill(50, 450, 550, 455, mats.MustLookup("stone"))
	for range 40 {
		kept.Step()
	}
	var meta bytes.Buffer
	if err := WriteMeta(&meta, Meta{Seed: cfg.Seed, Width: cfg.Width, Height: cfg.Height, Tick: kept.Tick()}); err != nil {
		t.Fatal(err)
	}
	cols, rows := kept.Blocks()
	files := map[[2]int][]byte{}
	for by := range rows {
		for bx := range cols {
			// Saving takes a block out of memory; the kept world puts it
			// straight back, which changes nothing
			// (TestUnloadingAndLoadingKeepsTheWorld).
			b, err := kept.UnloadBlock(bx, by)
			if err != nil {
				t.Fatal(err)
			}
			files[[2]int{bx, by}] = encode(t, b, mats)
			if err := kept.LoadBlock(bx, by, b); err != nil {
				t.Fatal(err)
			}
		}
	}
	m, err := ReadMeta(&meta)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := world.New(world.Config{
		Width: m.Width, Height: m.Height, Seed: m.Seed, Tick: m.Tick, Workers: 3, Materials: mats, Streamed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	for at, data := range files {
		b, err := DecodeBlock(bytes.NewReader(data), mats)
		if err != nil {
			t.Fatal(err)
		}
		if err := loaded.LoadBlock(at[0], at[1], b); err != nil {
			t.Fatal(err)
		}
	}
	if a, b := kept.Checksum(), loaded.Checksum(); a != b {
		t.Fatalf("the loaded world's checksum is %#x, the saved one's %#x", b, a)
	}
	for range 60 {
		kept.Step()
		loaded.Step()
		if a, b := kept.Checksum(), loaded.Checksum(); a != b {
			t.Fatalf("tick %d: the loaded world's checksum is %#x, the saved one's %#x", kept.Tick(), b, a)
		}
	}
}

func TestMetaRoundTrip(t *testing.T) {
	want := Meta{Seed: 1<<63 + 5, Width: 16384, Height: 8192, Tick: 4000000000}
	var buf bytes.Buffer
	if err := WriteMeta(&buf, want); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "matter-wake-world 1\nseed 9223372036854775813\nsize 16384 8192\ntick 4000000000\n" {
		t.Fatalf("WriteMeta wrote %q", got)
	}
	got, err := ReadMeta(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ReadMeta = %+v, want %+v", got, want)
	}
}

func TestMetaRefusesBadHeaders(t *testing.T) {
	tests := []struct {
		name, text, wantErr string
	}{
		{"empty", "", "missing"},
		{"newer version", "matter-wake-world 2\nseed 1\nsize 8 8\ntick 0\n", "format version 2"},
		{"bad seed", "matter-wake-world 1\nseed -1\nsize 8 8\ntick 0\n", "seed"},
		{"zero size", "matter-wake-world 1\nseed 1\nsize 0 8\ntick 0\n", "not positive"},
		{"size not a number", "matter-wake-world 1\nseed 1\nsize 8 x\ntick 0\n", "height"},
		{"missing tick", "matter-wake-world 1\nseed 1\nsize 8 8\n", `missing "tick"`},
		{"extra line", "matter-wake-world 1\nseed 1\nsize 8 8\ntick 0\nfoo 1\n", "unexpected line"},
		{"wrong order", "matter-wake-world 1\nsize 8 8\nseed 1\ntick 0\n", `want "seed"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadMeta(strings.NewReader(tt.text))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ReadMeta error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
	if err := WriteMeta(&bytes.Buffer{}, Meta{Width: 0, Height: 5}); err == nil {
		t.Fatal("WriteMeta accepted a world of width 0")
	}
}

// typicalBlock is a block after play: stone with a cave of air, a heap of
// sand and a pool of water, cells that moved carrying their last tick.
func typicalBlock(mats *material.Table) *world.Block {
	stone, sand, water := mats.MustLookup("stone"), mats.MustLookup("sand"), mats.MustLookup("water")
	s := rng.New(7)
	b := new(world.Block)
	for y := range world.BlockSize {
		for x := range world.BlockSize {
			c := &b[y*world.BlockSize+x]
			c.Tone = uint8(s.Uint32())
			dx, dy := x-256, y-220
			switch {
			case dx*dx/4+dy*dy > 150*150:
				c.Mat = stone
			case y > 330 && x < 256:
				c.Mat, c.Last = sand, 1000+uint32(s.IntN(500))
			case y > 300:
				c.Mat, c.Last = water, 1000+uint32(s.IntN(500))
			default:
				c.Tone = 0
			}
		}
	}
	return b
}

func BenchmarkEncodeBlock(b *testing.B) {
	mats := table(b, "stone", "sand", "water")
	blk := typicalBlock(mats)
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

func BenchmarkDecodeBlock(b *testing.B) {
	mats := table(b, "stone", "sand", "water")
	data := encode(b, typicalBlock(mats), mats)
	b.ReportAllocs()
	for range b.N {
		if _, err := DecodeBlock(bytes.NewReader(data), mats); err != nil {
			b.Fatal(err)
		}
	}
}

// Any table material.NewTable accepts must round-trip: the most materials
// a table may hold, and names of any length, even when the names alone
// take more than the cells. Found by two Codex reviews of the block format
// (2026-11-06).
func TestBlockTakesAnyMaterialTable(t *testing.T) {
	long := strings.Repeat("n", 300)
	defs := []material.Def{{Name: "air", Kind: material.Empty}, {Name: long, Kind: material.Static}}
	pad := strings.Repeat("m", 128)
	for i := len(defs); i < material.MaxMaterials; i++ {
		defs = append(defs, material.Def{Name: pad + strconv.Itoa(i), Kind: material.Static})
	}
	mats, err := material.NewTable(defs)
	if err != nil {
		t.Fatal(err)
	}
	b := new(world.Block)
	b[1].Mat = mats.MustLookup(long)
	b[2].Mat = material.ID(material.MaxMaterials - 1)
	got, err := DecodeBlock(bytes.NewReader(encode(t, b, mats)), mats)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Mat != b[1].Mat || got[2].Mat != b[2].Mat {
		t.Fatalf("cells hold %d and %d after the round trip, want %d and %d", got[1].Mat, got[2].Mat, b[1].Mat, b[2].Mat)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	in := &Session{
		CameraX: 7952, CameraY: -3, Slot: 3,
		Held:   map[string]int{"stone": 15, "water": 0, "sand": 120},
		Blocks: [][2]int{{16, 2}, {15, 1}, {16, 1}},
		Stream: &SessionStream{Area: [4]int{7872, -64, 608, 398}, LastCheck: 4200, Interval: 15},
		Chunks: []SessionChunk{
			{CX: 130, CY: 9, Dirty: [4]int{8320, 576, 8384, 600}, Waited: 3},
			{CX: 129, CY: 9, Keep: [4]int{8256, 590, 8260, 600}},
		},
	}
	var buf bytes.Buffer
	if err := WriteSession(&buf, in); err != nil {
		t.Fatal(err)
	}
	want := "matter-wake-session 3\ncamera 7952 -3\nslot 3\nstream 7872 -64 608 398 4200 15\n" +
		"held \"sand\" 120\nheld \"stone\" 15\nblock 15 1\nblock 16 1\nblock 16 2\n" +
		"chunk 129 9 0 0 0 0 8256 590 8260 600 0\nchunk 130 9 8320 576 8384 600 0 0 0 0 3\n"
	if got := buf.String(); got != want {
		t.Fatalf("WriteSession wrote %q, want %q", got, want)
	}
	got, err := ReadSession(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.CameraX != 7952 || got.CameraY != -3 || got.Slot != 3 || len(got.Held) != 2 ||
		got.Held["sand"] != 120 || got.Held["stone"] != 15 || len(got.Blocks) != 3 || got.Blocks[2] != [2]int{16, 2} ||
		got.Stream == nil || *got.Stream != *in.Stream || !got.Scheduled ||
		len(got.Chunks) != 2 || got.Chunks[0] != in.Chunks[1] || got.Chunks[1] != in.Chunks[0] {
		t.Fatalf("ReadSession = %+v", got)
	}
}

// Version 3 records the chunks' states: a file of version 3 says which
// chunks had something to scan, even when it lists none. Older files say
// nothing about them.
func TestSessionScheduled(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"matter-wake-session 3\ncamera 1 2\nslot 1\nblock 0 0\n", true},
		{"matter-wake-session 2\ncamera 1 2\nslot 1\nblock 0 0\n", false},
		{"matter-wake-session 1\ncamera 1 2\nslot 1\n", false},
	}
	for _, tt := range tests {
		got, err := ReadSession(strings.NewReader(tt.text))
		if err != nil {
			t.Fatalf("ReadSession(%q): %v", tt.text, err)
		}
		if got.Scheduled != tt.want || len(got.Chunks) != 0 {
			t.Errorf("ReadSession(%q): scheduled %v with %d chunks, want %v with none", tt.text, got.Scheduled, len(got.Chunks), tt.want)
		}
	}
}

// A session without a stream line writes none and reads back without
// one, as do version 1 files, which had none: the stream then starts anew.
func TestSessionWithoutStream(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSession(&buf, &Session{Slot: 1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "stream") {
		t.Fatalf("WriteSession wrote a stream line for a session without one: %q", buf.String())
	}
	for _, text := range []string{buf.String(), "matter-wake-session 1\ncamera 1 2\nslot 1\nblock 3 4\n"} {
		got, err := ReadSession(strings.NewReader(text))
		if err != nil {
			t.Fatalf("ReadSession(%q): %v", text, err)
		}
		if got.Stream != nil {
			t.Fatalf("ReadSession(%q) found a stream %+v", text, got.Stream)
		}
	}
}

// A material name may be anything a table takes: of any length, with
// spaces or quotes. A session holding one must read back. Found by two
// Codex reviews of the save folder (2026-11-06).
func TestSessionTakesAnyName(t *testing.T) {
	names := []string{strings.Repeat("n", 1<<16), "wet sand", "tab\tand \"quote\"", " "}
	held := map[string]int{}
	for i, name := range names {
		held[name] = i + 1
	}
	var buf bytes.Buffer
	if err := WriteSession(&buf, &Session{Slot: 1, Held: held}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSession(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for name, n := range held {
		if got.Held[name] != n {
			t.Errorf("%q reads back holding %d, want %d", name, got.Held[name], n)
		}
	}
}

func TestSessionRefusesBadFiles(t *testing.T) {
	const head = "matter-wake-session 3\ncamera 1 2\nslot 1\n"
	tests := []struct {
		name, text, wantErr string
	}{
		{"empty", "", "missing"},
		{"newer version", "matter-wake-session 4\n", "format version 4"},
		{"chunk in version 2", "matter-wake-session 2\ncamera 1 2\nslot 1\nchunk 0 0 0 0 1 1 0 0 0 0 0\n", "version 2"},
		{"short chunk line", head + "chunk 0 0 0 0 1 1 0 0 0 0\n", "chunk <x>"},
		{"negative chunk", head + "chunk -1 0 0 0 1 1 0 0 0 0 0\n", "chunk x"},
		{"chunk twice", head + "chunk 0 0 0 0 1 1 0 0 0 0 0\nchunk 0 0 0 0 1 1 0 0 0 0 0\n", "twice"},
		{"chunk waits too long", head + "chunk 0 0 0 0 1 1 0 0 0 0 4294967296\n", "waited"},
		{"version 0", "matter-wake-session 0\n", "format version 0"},
		{"stream in version 1", "matter-wake-session 1\ncamera 1 2\nslot 1\nstream 0 0 10 10 0 60\n", "version 1"},
		{"short stream line", head + "stream 0 0 10 10 0\n", "stream <x>"},
		{"stream twice", head + "stream 0 0 10 10 0 60\nstream 0 0 10 10 0 60\n", "twice"},
		{"stream without interval", head + "stream 0 0 10 10 0 0\n", "interval"},
		{"negative stream size", head + "stream 0 0 -10 10 0 60\n", "width"},
		{"negative last check", head + "stream 0 0 10 10 -1 60\n", "last check"},
		{"bad camera", "matter-wake-session 1\ncamera 1 x\n", "camera y"},
		{"slot 0", "matter-wake-session 1\ncamera 1 2\nslot 0\n", "slot 0"},
		{"bad held line", head + "held \"sand\"\n", "<count>"},
		{"unquoted name", head + "held sand 4\n", "<count>"},
		{"negative count", head + "held \"sand\" -4\n", "count of"},
		{"held twice", head + "held \"sand\" 1\nheld \"sand\" 2\n", "twice"},
		{"negative block", head + "block -1 2\n", "block x"},
		{"block twice", head + "block 1 2\nblock 1 2\n", "twice"},
		{"other line", head + "seed 4\n", "block <x> <y>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadSession(strings.NewReader(tt.text))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ReadSession error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
	bad := []*Session{
		{Slot: 0},
		{Slot: 1, Held: map[string]int{"sand": -1}},
		{Slot: 1, Blocks: [][2]int{{-1, 0}}},
		{Slot: 1, Blocks: [][2]int{{1, 0}, {1, 0}}},
		{Slot: 1, Stream: &SessionStream{Interval: 0}},
		{Slot: 1, Stream: &SessionStream{Area: [4]int{0, 0, -1, 0}, Interval: 1}},
		{Slot: 1, Chunks: []SessionChunk{{CX: -1}}},
		{Slot: 1, Chunks: []SessionChunk{{CX: 1}, {CX: 1}}},
		{Slot: 1, Chunks: []SessionChunk{{Waited: 1 << 32}}},
		{Slot: 1, Chunks: []SessionChunk{{Dirty: [4]int{-1, 0, 4, 4}}}},
	}
	for _, s := range bad {
		if err := WriteSession(&bytes.Buffer{}, s); err == nil {
			t.Errorf("WriteSession accepted %+v", s)
		}
	}
}
