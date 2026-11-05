// Package rng provides seeded random number streams that give the same
// sequence on every platform and Go version.
//
// The simulation must not use math/rand: its algorithms and seeding are
// allowed to change between Go releases, which would silently break
// replays and golden tests.
package rng

// Stream is a SplitMix64 generator. The zero value is a valid stream with
// seed 0.
type Stream struct {
	state uint64
}

// New returns a stream that starts from seed.
func New(seed uint64) Stream {
	return Stream{state: seed}
}

// Uint64 returns the next 64 random bits.
func (s *Stream) Uint64() uint64 {
	s.state += 0x9e3779b97f4a7c15
	return Mix(s.state)
}

// Uint32 returns the next 32 random bits.
func (s *Stream) Uint32() uint32 {
	return uint32(s.Uint64() >> 32)
}

// Bool returns a random bit.
func (s *Stream) Bool() bool {
	return s.Uint64()>>63 == 1
}

// IntN returns a uniform integer in [0, n). It panics if n <= 0.
func (s *Stream) IntN(n int) int {
	if n <= 0 {
		panic("rng: IntN called with n <= 0")
	}
	bound := uint64(n)
	// Reject the top slice of the range so every result is equally likely.
	limit := -bound % bound
	for {
		v := s.Uint64()
		if v >= limit {
			return int(v % bound)
		}
	}
}

// Mix is the SplitMix64 finalizer: a fast bijective hash of x.
func Mix(x uint64) uint64 {
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// Hash combines a seed with keys into one well-mixed value. The order of
// the keys matters.
func Hash(seed uint64, keys ...uint64) uint64 {
	h := Mix(seed)
	for _, k := range keys {
		h = Mix(h ^ k)
	}
	return h
}

// Hash3 is Hash with exactly three keys. It never allocates, so hot loops
// can use it.
func Hash3(seed, a, b, c uint64) uint64 {
	return Mix(Mix(Mix(Mix(seed)^a)^b) ^ c)
}
