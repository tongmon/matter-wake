//go:build !race

package replay

// raceDetector is set when the tests run with the race detector, which
// makes them about 16 times slower (docs/pitfalls.md).
const raceDetector = false
