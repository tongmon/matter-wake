package rng

import "testing"

// The first outputs of SplitMix64 seeded with 0 are fixed by the
// algorithm; if they change, every replay and golden test changes too.
func TestKnownSequence(t *testing.T) {
	s := New(0)
	want := []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f}
	for i, w := range want {
		if got := s.Uint64(); got != w {
			t.Fatalf("output %d = %#x, want %#x", i, got, w)
		}
	}
}

func TestIntNStaysInRange(t *testing.T) {
	s := New(42)
	for _, n := range []int{1, 2, 3, 7, 64, 1000} {
		for range 1000 {
			if v := s.IntN(n); v < 0 || v >= n {
				t.Fatalf("IntN(%d) = %d", n, v)
			}
		}
	}
}

func TestIntNPanicsOnNonPositive(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("IntN(0) did not panic")
		}
	}()
	s := New(1)
	s.IntN(0)
}

func TestHash3MatchesHash(t *testing.T) {
	for _, keys := range [][3]uint64{{0, 0, 0}, {1, 2, 3}, {3, 2, 1}, {1 << 63, 5, 9}} {
		if Hash3(7, keys[0], keys[1], keys[2]) != Hash(7, keys[0], keys[1], keys[2]) {
			t.Fatalf("Hash3 and Hash disagree for %v", keys)
		}
	}
	if Hash3(7, 1, 2, 3) == Hash3(7, 3, 2, 1) {
		t.Fatal("key order does not change the hash")
	}
}

func TestHash3DoesNotAllocate(t *testing.T) {
	var sink uint64
	if n := testing.AllocsPerRun(100, func() { sink += Hash3(1, 2, 3, 4) }); n != 0 {
		t.Fatalf("Hash3 allocates %v times per call", n)
	}
	_ = sink
}
