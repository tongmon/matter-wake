// Probes for TestCompiledWithoutContraction: the same a*b + c, compiled once
// under box2d_fp.h (fpprobe.c) and once without it (fpprobe_free.c).

#ifndef PHYSICS_FPPROBE_H
#define PHYSICS_FPPROBE_H

// On x86 the FMA instructions are not enabled by default, so a compiler
// could never fuse there. The probes enable them, as Box2D's AVX2 functions
// could if a compiler ever implied FMA from AVX2. The test runs them only on
// a CPU that has FMA.
#if defined( __x86_64__ ) || defined( __i386__ )
#define PHYSICS_PROBE_TARGET __attribute__( ( target( "fma" ) ) )
#else
#define PHYSICS_PROBE_TARGET
#endif

float physicsProbeGuarded( float a, float b, float c );
float physicsProbeFree( float a, float b, float c );

// physicsProbeCanFuse reports whether this CPU has FMA instructions.
int physicsProbeCanFuse( void );

#endif
