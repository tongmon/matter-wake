package app

import (
	"strings"
	"testing"
)

// Run must reject bad settings with an error before it reaches Ebitengine,
// which panics on a nonpositive window size.
func TestRunRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"zero scale", Config{Width: 480, Height: 270, Scale: 0}, "scale"},
		{"negative scale", Config{Width: 480, Height: 270, Scale: -2}, "scale"},
		{"zero width", Config{Width: 0, Height: 270, Scale: 2}, "size"},
		{"negative height", Config{Width: 480, Height: -1, Scale: 2}, "size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Run(&tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}
