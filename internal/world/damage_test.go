package world

import (
	"testing"

	"github.com/tongmon/matter-wake/internal/material"
)

// withHP gives stone and sand the hp a test needs.
func withHP(stone, sand uint32) func(defs []material.Def) {
	return func(defs []material.Def) {
		defs[defStone].HP = stone
		defs[defSand].HP = sand
	}
}

// wearsUntilUsedUp wears the cell at (x, y) by amount until it is used up
// and returns how many times that took, or -1 after limit times.
func wearsUntilUsedUp(w *World, x, y int, amount uint32, limit int) int {
	for n := 1; n <= limit; n++ {
		if w.Wear(x, y, amount) {
			return n
		}
	}
	return -1
}

// A cell is used up by the wear that brings its damage to its hp:
// ceil(hp / amount) wears. Damage is kept in steps of 1/DamageScale of the
// hp, each wear rounded up, so where hp / amount is just above a whole
// number the last, small wear can be skipped.
func TestWearTakesHPOverAmount(t *testing.T) {
	tests := []struct {
		hp, amount uint32
		wears      int
	}{
		{1000, 1000, 1},
		{1000, 5000, 1},
		{1000, 999, 2},
		{1000, 500, 2},
		{1000, 300, 4},
		{1000, 250, 4},
		{1000, 7, 143},
		{100000, 2000, 50},
		{800, 2000, 1},
		{0, 1, 1},
		{1 << 31, 1 << 31, 1},
		{4000000000, 1 << 26, 60},
		// 100001 / 2000 is 50.0005: rounding up 50 wears covers the rest.
		{100001, 2000, 50},
	}
	for _, tt := range tests {
		w, ids := newTestWorldWith(t, 8, 8, 1, withHP(tt.hp, 0))
		w.Set(3, 3, ids.stone)
		if got := wearsUntilUsedUp(w, 3, 3, tt.amount, 100000); got != tt.wears {
			t.Errorf("hp %d, %d a wear: used up after %d wears, want %d", tt.hp, tt.amount, got, tt.wears)
		}
		// Wear only reports a cell used up; removing it is the caller's.
		if c, _ := w.At(3, 3); c.Mat != ids.stone {
			t.Errorf("Wear changed the used-up cell to %d", c.Mat)
		}
	}
}

// The smallest damage still counts: it never rounds down to nothing.
func TestWearCountsTinyDamage(t *testing.T) {
	w, ids := newTestWorldWith(t, 8, 8, 1, withHP(4000000000, 0))
	w.Set(3, 3, ids.stone)
	w.Wear(3, 3, 1)
	if c, _ := w.At(3, 3); c.Damage != 1 {
		t.Fatalf("1 damage on hp 4000000000 left Damage %d, want 1 step", c.Damage)
	}
	if w.Wear(3, 3, 0) {
		t.Fatal("no damage used the cell up")
	}
	if c, _ := w.At(3, 3); c.Damage != 1 {
		t.Fatalf("no damage changed Damage to %d", c.Damage)
	}
}

func TestWearOutsideTheWorld(t *testing.T) {
	w, _ := newTestWorld(t, 8, 8, 1)
	if w.Wear(-1, 3, 1) || w.Wear(3, 8, 1) {
		t.Fatal("Wear outside the world used a cell up")
	}
}

// Damage belongs to the cell: it falls with a powder and stays when
// nothing wears it.
func TestDamageMovesWithTheCell(t *testing.T) {
	w, ids := newTestWorldWith(t, 16, 64, 1, withHP(0, 1000))
	w.Set(8, 2, ids.sand)
	w.Wear(8, 2, 300)
	before, _ := w.At(8, 2)
	if before.Damage == 0 {
		t.Fatal("wearing the sand left no damage")
	}
	for range 60 {
		w.Step()
	}
	x, y, ok := find(w, ids.sand)
	if !ok || y != w.Height()-1 {
		t.Fatalf("the sand is at (%d, %d), want it on the floor", x, y)
	}
	if c, _ := w.At(x, y); c.Damage != before.Damage {
		t.Fatalf("the sand fell with Damage %d, want %d", c.Damage, before.Damage)
	}
}

// A new cell, or a cell turned into another material, starts undamaged.
func TestNewCellsAreUndamaged(t *testing.T) {
	w, ids := newTestWorldWith(t, 16, 16, 1, withHP(1000, 1000))
	set := map[string]func(){
		"Set":   func() { w.Set(3, 3, ids.stone) },
		"Fill":  func() { w.Fill(2, 2, 5, 5, ids.stone) },
		"Paint": func() { w.Paint(3, 3, 1, ids.sand, false) },
	}
	for name, reset := range set {
		w.Set(3, 3, ids.stone)
		w.Wear(3, 3, 300)
		reset()
		if c, _ := w.At(3, 3); c.Damage != 0 {
			t.Errorf("%s left Damage %d, want 0", name, c.Damage)
		}
	}
}

func TestChecksumCoversDamage(t *testing.T) {
	w, ids := newTestWorldWith(t, 8, 8, 1, withHP(1000, 0))
	w.Set(3, 3, ids.stone)
	before := w.Checksum()
	w.Wear(3, 3, 1)
	if w.Checksum() == before {
		t.Fatal("the checksum did not change with the damage")
	}
}
