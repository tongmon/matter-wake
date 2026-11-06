//go:build !cgo

package physics

// Box2D is C, so this package needs cgo and a C compiler: gcc or clang, on
// Windows a MinGW-family one on PATH. Without cgo, this undefined name is the
// build error that says so, instead of every name in the package missing.
var _ = needsCgoAndACCompilerSeeThirdPartyREADME
