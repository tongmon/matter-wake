package app

import (
	"testing"
	"time"
)

func TestCountsToDuration(t *testing.T) {
	const day = 24 * time.Hour
	tests := []struct {
		n, freq int64
		want    time.Duration
	}{
		{1, 10_000_000, 100 * time.Nanosecond},
		{12_345, 10_000_000, 1234500 * time.Nanosecond},
		{3, 3_000_000_000, time.Nanosecond},
		// A counter that has run for ten years would overflow if it were
		// multiplied before it was divided.
		{10_000_000 * 3650 * 86_400, 10_000_000, 3650 * day},
		{3_000_000_000*100*86_400 + 1_500_000_000, 3_000_000_000, 100*day + 500*ms},
	}
	for _, tt := range tests {
		if got := countsToDuration(tt.n, tt.freq); got != tt.want {
			t.Errorf("countsToDuration(%d, %d) = %v, want %v", tt.n, tt.freq, got, tt.want)
		}
	}
}
