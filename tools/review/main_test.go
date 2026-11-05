package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNextReportPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if got, err := nextReportPath(); err != nil || got != filepath.Join("out", "codex-review-1.txt") {
		t.Fatalf("empty out/: got %q, %v", got, err)
	}
	for _, name := range []string{"codex-review-1.txt", "codex-review-7.txt", "codex-review-x.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join("out", name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := nextReportPath(); err != nil || got != filepath.Join("out", "codex-review-8.txt") {
		t.Fatalf("after review 7: got %q, %v", got, err)
	}
}

func TestReportLeftovers(t *testing.T) {
	a, b := [32]byte{1}, [32]byte{2}
	tests := []struct {
		name          string
		before, after map[string][32]byte
		want          []string
	}{
		{"unchanged", map[string][32]byte{"x.go": a}, map[string][32]byte{"x.go": a}, nil},
		{"new file", map[string][32]byte{}, map[string][32]byte{"probe_test.go": a}, []string{"probe_test.go"}},
		{"dirty file edited", map[string][32]byte{"x.go": a}, map[string][32]byte{"x.go": b}, []string{"x.go"}},
		{"change reverted", map[string][32]byte{"x.go": a}, map[string][32]byte{}, []string{"x.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reportLeftovers(tt.before, tt.after)
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("reported %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("reported nothing")
			}
			for _, name := range tt.want {
				if !strings.Contains(err.Error(), name) {
					t.Fatalf("error %q does not name %s", err, name)
				}
			}
		})
	}
}

func TestCheckSandbox(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(good, []byte("model: x\nsandbox: workspace-write [workdir]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("model: x\nsandbox: read-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkSandbox(good); err != nil {
		t.Fatalf("workspace-write header rejected: %v", err)
	}
	if err := checkSandbox(bad); err == nil {
		t.Fatal("read-only header accepted")
	}
}
