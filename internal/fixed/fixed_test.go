package fixed

import (
	"encoding/json"
	"math"
	"testing"
)

func TestFromFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want Q8
	}{
		{0, 0},
		{1, One},
		{0.25, 64},
		{-0.5, -128},
		{0.9, 230}, // 230.4 rounds to 230
		{127.99, 32765},
	}
	for _, tt := range tests {
		got, err := FromFloat(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("FromFloat(%v) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []float64{128, -129, math.NaN(), math.Inf(1)} {
		if _, err := FromFloat(bad); err == nil {
			t.Errorf("FromFloat(%v) accepted", bad)
		}
	}
}

func TestUnmarshalJSON(t *testing.T) {
	var v struct {
		A Q8 `json:"a"`
	}
	if err := json.Unmarshal([]byte(`{"a": 0.5}`), &v); err != nil || v.A != 128 {
		t.Fatalf("got %v, %v", v.A, err)
	}
	if err := json.Unmarshal([]byte(`{"a": "x"}`), &v); err == nil {
		t.Fatal("accepted a string")
	}
	if err := json.Unmarshal([]byte(`{"a": 500}`), &v); err == nil {
		t.Fatal("accepted an out-of-range number")
	}
}

func TestArithmeticSaturates(t *testing.T) {
	if got := Max.Add(One); got != Max {
		t.Errorf("Max+1 = %v", got)
	}
	if got := Min.Sub(One); got != Min {
		t.Errorf("Min-1 = %v", got)
	}
	if got := Min.Abs(); got != Max {
		t.Errorf("|Min| = %v", got)
	}
	if got := FromInt(1000); got != Max {
		t.Errorf("FromInt(1000) = %v", got)
	}
	if got := FromInt(-3); got != -3*One {
		t.Errorf("FromInt(-3) = %v", got)
	}
}

// Damping must shrink negative values exactly as it shrinks positive ones;
// rounding toward minus infinity would leave a leftward bias.
func TestMulIsSymmetric(t *testing.T) {
	damping := Q8(230) // 0.9
	for v := Q8(-2000); v <= 2000; v += 7 {
		if v.Mul(damping) != -(-v).Mul(damping) {
			t.Fatalf("%v × 0.9 is not the mirror of %v × 0.9", v, -v)
		}
	}
	// Repeated damping reaches zero from both sides.
	pos, neg := One, -One
	for range 200 {
		pos, neg = pos.Mul(damping), neg.Mul(damping)
	}
	if pos != 0 || neg != 0 {
		t.Fatalf("damping left %v and %v", pos, neg)
	}
}

func TestSignAndClamp(t *testing.T) {
	if Q8(-3).Sign() != -1 || Q8(0).Sign() != 0 || Q8(9).Sign() != 1 {
		t.Fatal("Sign is wrong")
	}
	if Q8(500).Clamp(-One, One) != One || Q8(-500).Clamp(-One, One) != -One {
		t.Fatal("Clamp is wrong")
	}
}

func TestString(t *testing.T) {
	if s := Q8(64).String(); s != "0.25" {
		t.Fatalf("String = %q", s)
	}
}
