package physics

import "testing"

// pileFallSteps is how long the falling benchmark watches the pile: 5 s,
// from the drop until most boxes have landed.
const pileFallSteps = 300

// BenchmarkPile measures one tick of 300 boxes: a Step and reading every
// box's transform, as the game will. Box2D runs single-threaded.
//
//   - falling: the 300 steps after the boxes are dropped, with sleeping on.
//     The world starts again every 300 steps.
//   - awake: the pile after it has formed, with sleeping off so no box ever
//     rests: the worst case for one step.
//
// simd=8 is the AVX2 contact solver (the dev PC and the reference CPU both
// have AVX2), simd=4 the SSE2 fallback.
func BenchmarkPile(b *testing.B) {
	for _, sse2 := range []bool{false, true} {
		if !sse2 && !hasAVX2() {
			continue
		}
		width := "simd=8"
		if sse2 {
			width = "simd=4"
		}
		b.Run("falling/"+width, func(b *testing.B) {
			var w *World
			var bodies []Body
			var sink float32
			for i := range b.N {
				if i%pileFallSteps == 0 {
					b.StopTimer()
					if w != nil {
						w.Close()
					}
					w, bodies = newPile(b, sse2)
					b.StartTimer()
				}
				w.Step(dt, subSteps)
				sink += readAll(w, bodies)
			}
			_ = sink
		})
		b.Run("awake/"+width, func(b *testing.B) {
			w, bodies := newPile(b, sse2)
			w.setSleeping(false)
			for range pileFallSteps {
				w.Step(dt, subSteps)
			}
			var sink float32
			b.ResetTimer()
			for range b.N {
				w.Step(dt, subSteps)
				sink += readAll(w, bodies)
			}
			_ = sink
		})
	}
}

// BenchmarkRead measures reading the transforms of 300 boxes, the part of a
// tick that crosses from Go to C once per box.
func BenchmarkRead(b *testing.B) {
	w, bodies := newPile(b, false)
	var sink float32
	for range b.N {
		sink += readAll(w, bodies)
	}
	_ = sink
}
