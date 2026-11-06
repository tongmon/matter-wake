// Floating-point rules for every C file this package compiles.
//
// Box2D gives the same results on every platform only if the compiler never
// fuses a multiply and an add into one FMA instruction, which rounds once
// instead of twice. Box2D's CMake build passes -ffp-contract=off for that.
// Go rejects that flag in #cgo CFLAGS, so the same rule is set here with a
// pragma. Include this header first, before anything else: it must come
// before the first function. TestCompiledWithoutContraction checks it.
//
// Keep this header free of #include: timer.c defines feature macros that must
// come before any system header.

#ifndef PHYSICS_BOX2D_FP_H
#define PHYSICS_BOX2D_FP_H

#if defined( __clang__ )
#pragma STDC FP_CONTRACT OFF
#elif defined( __GNUC__ )
#pragma GCC optimize( "fp-contract=off" )
#else
#error "Box2D needs a compiler that can turn off floating-point contraction (gcc or clang)"
#endif

#endif
