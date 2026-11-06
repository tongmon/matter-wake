package game

import (
	"slices"
	"testing"
)

// newViewGame starts a terrain session of 16×16 blocks with a view of
// w×h cells and the stream s.
func newViewGame(t *testing.T, w, h int, s Stream) *Game {
	t.Helper()
	g, err := New(&Config{
		Width: 8192, Height: 8192, Seed: 3, Workers: 1, Materials: testTable(t),
		Slots: []string{"sand", "water", "stone"}, Tool: testTool,
		Scene: SceneTerrain, ViewWidth: w, ViewHeight: h, Stream: s,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

// The camera's request area is the view with its corner moved toward zero
// onto the Grid, widened by Grid on each side. A move requests the area
// again and the area moved Lead times as far again, at most LeadMax on
// each axis. The numbers follow docs/briefs/streaming.md.
func TestRequestArea(t *testing.T) {
	g := newViewGame(t, 431, 242, testStream)
	tests := []struct {
		camX, camY int
		want       [4]int
	}{
		{1000, 500, [4]int{896, 384, 559, 370}},
		{900, 500, [4]int{832, 384, 559, 370}},
		{0, 0, [4]int{-64, -64, 559, 370}},
		{63, 64, [4]int{-64, 0, 559, 370}},
		// Toward zero, not down: -1 snaps to 0 before the border.
		{-1, -65, [4]int{-64, -128, 559, 370}},
	}
	for _, tt := range tests {
		if got := g.requestArea(tt.camX, tt.camY); got != tt.want {
			t.Errorf("requestArea(%d, %d) = %v, want %v", tt.camX, tt.camY, got, tt.want)
		}
	}
	aheads := []struct {
		prev, cur, want [4]int
	}{
		{[4]int{832, 384, 559, 370}, [4]int{896, 384, 559, 370}, [4]int{1152, 384, 559, 370}},
		{[4]int{896, 448, 559, 370}, [4]int{832, 384, 559, 370}, [4]int{576, 128, 559, 370}},
		{[4]int{0, 0, 559, 370}, [4]int{4096, 64, 559, 370}, [4]int{4608, 320, 559, 370}},
		{[4]int{4096, 64, 559, 370}, [4]int{0, 0, 559, 370}, [4]int{-512, -256, 559, 370}},
	}
	for _, tt := range aheads {
		if got := g.aheadOf(tt.prev, tt.cur); got != tt.want {
			t.Errorf("aheadOf(%v, %v) = %v, want %v", tt.prev, tt.cur, got, tt.want)
		}
	}
}

// An area takes the blocks it touches with both ends included, so one
// that ends exactly on a block edge takes the next block too.
func TestAreaBlocksIncludeTheFarEdge(t *testing.T) {
	g := newViewGame(t, 431, 242, testStream)
	tests := []struct {
		area [4]int
		want blockRange
	}{
		{[4]int{896, 384, 559, 370}, blockRange{1, 0, 3, 2}},
		{[4]int{0, 0, 512, 10}, blockRange{0, 0, 2, 1}},
		{[4]int{0, 0, 511, 511}, blockRange{0, 0, 1, 1}},
		// Clipped to the world: 16×16 blocks.
		{[4]int{-64, -64, 559, 370}, blockRange{0, 0, 1, 1}},
		{[4]int{7900, 7900, 559, 370}, blockRange{15, 15, 16, 16}},
		{[4]int{9000, 0, 559, 370}, blockRange{}},
	}
	for _, tt := range tests {
		if got := g.areaBlocks(tt.area); got != tt.want {
			t.Errorf("areaBlocks(%v) = %+v, want %+v", tt.area, got, tt.want)
		}
	}
}

// Moving the camera loads the blocks of the new area and of the area ahead
// in the direction it moved: the example of docs/briefs/streaming.md, in a
// world big enough that the spawn's blocks are elsewhere.
func TestStreamRequestsAhead(t *testing.T) {
	g := newViewGame(t, 431, 242, testStream)
	_, y := g.Spawn()
	step(t, g, Input{CameraX: 900, CameraY: y})
	area := g.areaBlocks(g.requestArea(900, y))
	for by := area.y0; by < area.y1; by++ {
		if g.world.Resident(3, by) {
			t.Fatalf("block (3, %d) is in memory before the camera moves toward it", by)
		}
	}
	step(t, g, Input{CameraX: 1000, CameraY: y})
	for by := area.y0; by < area.y1; by++ {
		for bx := 1; bx <= 3; bx++ {
			if !g.world.Resident(bx, by) {
				t.Fatalf("block (%d, %d) is not in memory after the camera moved right", bx, by)
			}
		}
	}
	// Standing still requests nothing more.
	before := len(g.resident)
	step(t, g, Input{CameraX: 1000, CameraY: y})
	if len(g.resident) != before {
		t.Fatalf("%d blocks in memory after standing still, %d before", len(g.resident), before)
	}
}

// residentAt loads blocks into g, besides those it holds.
func residentAt(t *testing.T, g *Game, blocks [][2]int) {
	t.Helper()
	if err := g.load(blocks); err != nil {
		t.Fatal(err)
	}
}

// An eviction check unloads nothing at or under Target and sets the
// interval back; one block over Target up to three times Target; past
// that, all the blocks over three times Target. At twice Target or more
// it halves the interval to the next check.
func TestEvictionCheckSchedule(t *testing.T) {
	tests := []struct {
		blocks, target     int
		interval           int
		wantGone, wantNext int
	}{
		{4, 4, 60, 0, 60},
		{4, 4, 15, 0, 60},
		{5, 4, 60, 1, 60},
		{7, 4, 60, 1, 60},
		{8, 4, 60, 1, 30},
		{12, 4, 60, 1, 30},
		{13, 4, 60, 1, 30},
		{16, 4, 60, 4, 30},
		{16, 4, 1, 4, 1},
		{9, 3, 60, 1, 30},
		{10, 3, 60, 1, 30},
	}
	for _, tt := range tests {
		s := testStream
		s.Target = tt.target
		g := newViewGame(t, 431, 242, s)
		x, y := g.Spawn()
		var extra [][2]int
		for b := 0; len(g.resident)+len(extra) < tt.blocks; b++ {
			at := [2]int{b % 16, 8 + b/16}
			if !g.world.Resident(at[0], at[1]) && !slices.Contains(extra, at) {
				extra = append(extra, at)
			}
		}
		residentAt(t, g, extra)
		if len(g.resident) != tt.blocks {
			t.Fatalf("set up %d blocks, want %d", len(g.resident), tt.blocks)
		}
		g.streaming.Interval = tt.interval
		g.streaming.LastCheck = 0
		if err := g.check(uint32(tt.interval-1), x, y); err != nil {
			t.Fatal(err)
		}
		if len(g.resident) != tt.blocks {
			t.Fatalf("%d blocks, interval %d: a check ran %d ticks after the last", tt.blocks, tt.interval, tt.interval-1)
		}
		if err := g.check(uint32(tt.interval), x, y); err != nil {
			t.Fatal(err)
		}
		if gone := tt.blocks - len(g.resident); gone != tt.wantGone || g.streaming.Interval != tt.wantNext {
			t.Errorf("%d blocks, target %d, interval %d: %d unloaded and next check in %d, want %d and %d",
				tt.blocks, tt.target, tt.interval, gone, g.streaming.Interval, tt.wantGone, tt.wantNext)
		}
		if g.streaming.LastCheck != uint32(tt.interval) {
			t.Errorf("last check at %d, want %d", g.streaming.LastCheck, tt.interval)
		}
	}
}

// A check unloads the block farthest from the block under the view's
// middle, in whole blocks; of equally far ones, the first in row order. It
// never unloads the camera's area.
func TestEvictionPicksFarthestFirst(t *testing.T) {
	s := testStream
	// The camera's area holds 2×2 blocks here; three more over Target 4
	// go one a check.
	s.Target = 4
	g := newViewGame(t, 431, 242, s)
	x, y := g.Spawn()
	area := g.areaBlocks(g.streaming.Area)
	cx, cy := (x+431/2)/512, (y+242/2)/512
	// Two blocks equally far left and right of the middle, and one nearer.
	far := [][2]int{{cx + 4, cy}, {cx - 4, cy}, {cx, cy + 3}}
	residentAt(t, g, far)
	var gone [][2]int
	for now := uint32(1); len(g.resident) > len(gone) && now < 100; now++ {
		g.streaming.Interval = 1
		before := slices.Clone(g.resident)
		if err := g.check(now, x, y); err != nil {
			t.Fatal(err)
		}
		for _, b := range before {
			if !g.world.Resident(b[0], b[1]) {
				gone = append(gone, b)
			}
		}
		if len(gone) == len(far) {
			break
		}
	}
	want := [][2]int{{cx - 4, cy}, {cx + 4, cy}, {cx, cy + 3}}
	if !slices.Equal(gone, want) {
		t.Fatalf("unloaded %v in that order, want %v", gone, want)
	}
	// Only the camera's area is left, and it stays, even far over Target.
	g.limits.Target = 1
	for range 5 {
		g.streaming.Interval = 1
		if err := g.check(g.streaming.LastCheck+1, x, y); err != nil {
			t.Fatal(err)
		}
	}
	for by := area.y0; by < area.y1; by++ {
		for bx := area.x0; bx < area.x1; bx++ {
			if !g.world.Resident(bx, by) {
				t.Fatalf("block (%d, %d) of the camera's area was unloaded", bx, by)
			}
		}
	}
}

// Standing still after a long walk brings the blocks in memory down to
// Target, one a check, never more than one at a check while under twice
// Target.
func TestStandingStillUnloadsDownToTarget(t *testing.T) {
	g := newViewGame(t, 480, 270, testStream)
	x, y := g.Spawn()
	for range 200 {
		x = min(x+16, 8192-480)
		step(t, g, Input{CameraX: x, CameraY: y})
	}
	if len(g.resident) <= testStream.Target {
		t.Fatalf("only %d blocks in memory after the walk; the test needs more than %d", len(g.resident), testStream.Target)
	}
	for range 3000 {
		before := len(g.resident)
		step(t, g, Input{CameraX: x, CameraY: y})
		if gone := before - len(g.resident); gone > 1 && before < 3*testStream.Target {
			t.Fatalf("%d blocks left at one check with %d in memory", gone, before)
		}
	}
	if len(g.resident) != testStream.Target {
		t.Fatalf("%d blocks in memory after standing still, want %d", len(g.resident), testStream.Target)
	}
}
