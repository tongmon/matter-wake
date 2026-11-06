package physics

// box2dSum is a SHA-256 of the vendored Box2D in third_party/box2d.
//
// Go's build cache tracks the files in this folder, but not the Box2D files
// that the box2d_*.c wrappers include from outside it. After a Box2D update
// the package would keep the old compiled engine, and the determinism golden
// would test it instead of the new one. Changing this constant changes the
// package, so Go rebuilds Box2D. TestBox2DSumIsCurrent fails, printing the
// new value, until it is updated.
const box2dSum = "6901ff3d9da7446ad36bddc20c0515104f3dabdf55afe7912602966a4381ae11"
