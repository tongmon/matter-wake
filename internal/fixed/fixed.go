// Package fixed is signed fixed-point arithmetic for simulation values that
// need fractions, such as cell velocities.
//
// Simulation state never uses floats: Go may fuse a float multiply and add
// into one FMA instruction on some CPUs (arm64) and not on others, which
// changes the last bit of a result and so the whole simulation (see
// docs/pitfalls.md). Integer arithmetic gives the same bits everywhere.
package fixed

import (
	"fmt"
	"math"
	"strconv"
)

// Q8 is a fixed-point number with 8 fractional bits: Q8(256) is 1. It
// holds values from -128 to just under 128. Arithmetic saturates at the
// ends of the range instead of wrapping.
type Q8 int16

// FracBits is the number of fractional bits in a Q8.
const FracBits = 8

// Common values.
const (
	// One is 1.0.
	One Q8 = 1 << FracBits
	// Max is the largest Q8, just under 128.
	Max Q8 = math.MaxInt16
	// Min is the smallest Q8, -128.
	Min Q8 = math.MinInt16
)

// FromInt returns n as a Q8, saturated to the range.
func FromInt(n int) Q8 {
	return saturate(int64(n) << FracBits)
}

// FromFloat returns the Q8 nearest to f. It is for loading data, never for
// simulation steps. It fails if f is not finite or out of range.
func FromFloat(f float64) (Q8, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("fixed: %v is not a finite number", f)
	}
	scaled := math.Round(f * float64(One))
	if scaled < float64(Min) || scaled > float64(Max) {
		return 0, fmt.Errorf("fixed: %v is outside [%v, %v]", f, Min.Float(), Max.Float())
	}
	return Q8(scaled), nil
}

// UnmarshalJSON reads a JSON number such as 0.25.
func (q *Q8) UnmarshalJSON(data []byte) error {
	f, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return fmt.Errorf("fixed: %s is not a number", data)
	}
	v, err := FromFloat(f)
	if err != nil {
		return err
	}
	*q = v
	return nil
}

// Float returns q as a float64, for display and tests only.
func (q Q8) Float() float64 {
	return float64(q) / float64(One)
}

// String formats q as a decimal number.
func (q Q8) String() string {
	return strconv.FormatFloat(q.Float(), 'f', -1, 64)
}

// Add returns q + r.
func (q Q8) Add(r Q8) Q8 {
	return saturate(int64(q) + int64(r))
}

// Sub returns q - r.
func (q Q8) Sub(r Q8) Q8 {
	return saturate(int64(q) - int64(r))
}

// Mul returns q × r, rounded toward zero. Rounding toward zero keeps
// repeated damping symmetric: a negative value decays to zero exactly as
// its positive mirror does.
func (q Q8) Mul(r Q8) Q8 {
	return saturate(int64(q) * int64(r) / int64(One))
}

// Abs returns |q|.
func (q Q8) Abs() Q8 {
	if q < 0 {
		return saturate(-int64(q))
	}
	return q
}

// Sign returns -1, 0 or 1.
func (q Q8) Sign() int {
	switch {
	case q < 0:
		return -1
	case q > 0:
		return 1
	default:
		return 0
	}
}

// Clamp limits q to [lo, hi].
func (q Q8) Clamp(lo, hi Q8) Q8 {
	return min(max(q, lo), hi)
}

func saturate(v int64) Q8 {
	return Q8(min(max(v, int64(Min)), int64(Max)))
}
