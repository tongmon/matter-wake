//go:build !box2dscalar

package physics

// scalarBuild reports whether Box2D was compiled without SIMD (scalar.go).
const scalarBuild = false
