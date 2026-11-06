//go:build box2dscalar

package physics

// With -tags box2dscalar, Box2D is compiled without SIMD, so tests can check
// that the scalar path gives the same results as the SIMD ones. The game is
// never built this way.

// #cgo CFLAGS: -DBOX2D_DISABLE_SIMD
import "C"

// scalarBuild reports whether Box2D was compiled without SIMD.
const scalarBuild = true
