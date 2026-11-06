// Compiled without box2d_fp.h, to show whether this compiler fuses by
// default. If it does not, TestCompiledWithoutContraction proves nothing
// about the guard on this compiler and says so.
#include "fpprobe.h"

PHYSICS_PROBE_TARGET float physicsProbeFree( float a, float b, float c )
{
	return a * b + c;
}
