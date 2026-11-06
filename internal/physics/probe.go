package physics

// #include "fpprobe.h"
import "C"

// probeGuarded computes a*b + c in C compiled under box2d_fp.h, like Box2D.
func probeGuarded(a, b, c float32) float32 {
	return float32(C.physicsProbeGuarded(C.float(a), C.float(b), C.float(c)))
}

// probeFree computes a*b + c in C compiled without box2d_fp.h.
func probeFree(a, b, c float32) float32 {
	return float32(C.physicsProbeFree(C.float(a), C.float(b), C.float(c)))
}

// probeCanFuse reports whether this CPU has FMA instructions, so a compiler
// could fuse the probes.
func probeCanFuse() bool {
	return C.physicsProbeCanFuse() != 0
}
