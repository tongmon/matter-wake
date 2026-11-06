package main

import (
	"slices"
	"testing"
)

// TestSplitCC checks that CC is split the way the go command splits it:
// fields separated by spaces, and a field may be quoted to hold spaces, as
// a compiler under C:\Program Files needs (Codex review, 2026-11-07).
func TestSplitCC(t *testing.T) {
	for _, tt := range []struct {
		cc   string
		want []string // nil: an error
	}{
		{"gcc", []string{"gcc"}},
		{"  clang   -m64 ", []string{"clang", "-m64"}},
		{`"C:\Program Files\mingw\bin\gcc.exe" -m64`, []string{`C:\Program Files\mingw\bin\gcc.exe`, "-m64"}},
		{`'C:\Program Files\llvm mingw\bin\clang.exe'`, []string{`C:\Program Files\llvm mingw\bin\clang.exe`}},
		{`"unterminated`, nil},
		{"", nil},
		{"   ", nil},
	} {
		got, err := splitCC(tt.cc)
		if tt.want == nil {
			if err == nil {
				t.Errorf("splitCC(%q) = %q, want an error", tt.cc, got)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("splitCC(%q) = %q, %v, want %q", tt.cc, got, err, tt.want)
		}
	}
}
