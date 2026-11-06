# Third-party code

Code written by others and kept in this repository unchanged. Do not edit
files under these folders; update them from upstream instead.

## box2d

[Box2D](https://box2d.org), the 2D rigid body engine, compiled into the game
through cgo by `internal/physics`.

- License: MIT, `box2d/LICENSE`. The license must ship with the game.
- Version: v3.2.0, released 2026-10-19, commit
  `178495ba35695aefe9c321e569d2277f06db3b90`.
- Source: `https://github.com/erincatto/box2d/archive/refs/tags/v3.2.0.tar.gz`,
  downloaded 2026-11-07, SHA-256
  `ffec747b27a2267016f8d0240d658c82d187fb2271fa0f69fcbe53fbc067180d`.
  `git get-tar-commit-id` on it prints the commit above.
- Kept: `LICENSE`, `include/box2d/*.h`, and `src/*.c`, `src/*.h`,
  `src/*.inl`. Everything else (samples, tests, benchmarks, docs, CMake and
  other build files) is left out.

### How it is built

There is no CMake step. Each `src/*.c` has a wrapper of the same name with a
`box2d_` prefix in `internal/physics`, which cgo compiles as part of
`go build`. The wrapper includes `internal/physics/box2d_fp.h` first and
then the source file. The header turns off floating-point contraction (fusing
`a*b + c` into one FMA instruction) with a pragma, because Go does not allow
`-ffp-contract=off` in `#cgo CFLAGS`. Box2D's own CMake build passes that flag
to keep results the same on every platform. The compiler flags are in
`internal/physics/physics.go`.

### Updating

1. Download the new release's source archive, check its commit with
   `git get-tar-commit-id`, and replace the kept files above.
2. Add a wrapper for every new `src/*.c` and remove the wrappers of deleted
   ones. `TestEveryBox2DSourceIsCompiled` lists what is missing.
3. Set `box2dSum` in `internal/physics/box2d_sum.go` to the value
   `TestBox2DSumIsCurrent` prints. Go's build cache does not see changes to
   files the wrappers include from here, so until that constant changes, Go
   keeps the old compiled Box2D.
4. Update the version, commit and hash here.
5. Run the determinism test and review the golden change, if any:
   `go test ./internal/physics/ -run Determinism -update`.
