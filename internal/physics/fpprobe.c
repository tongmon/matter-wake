// Compiled like the Box2D sources: box2d_fp.h first.
#include "box2d_fp.h"

#include "fpprobe.h"

PHYSICS_PROBE_TARGET float physicsProbeGuarded( float a, float b, float c )
{
	return a * b + c;
}

int physicsProbeCanFuse( void )
{
#if defined( __x86_64__ ) || defined( __i386__ )
	__builtin_cpu_init();
	return __builtin_cpu_supports( "fma" );
#elif defined( __aarch64__ )
	return 1;
#else
	return 0;
#endif
}
