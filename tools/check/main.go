// Command check runs every check a change must pass before a PR. CI runs
// the same command on Windows, macOS and Linux.
//
//	go run ./tools/check        check everything
//	go run ./tools/check -fix   rewrite formatting first
//	go run ./tools/check -vuln  also scan dependencies for known
//	                            vulnerabilities (needs network)
//
// Run it from the repository root.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type result int

const (
	pass result = iota
	fail
	skip
)

type step struct {
	name string
	// run returns a reason when the step was skipped.
	run func() (skipped string, err error)
}

func main() {
	fix := flag.Bool("fix", false, "rewrite formatting before checking")
	vuln := flag.Bool("vuln", false, "scan dependencies with govulncheck (needs network)")
	flag.Parse()
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Fprintln(os.Stderr, "check: run from the repository root")
		os.Exit(2)
	}

	lint := []string{"tool", "-modfile=tools.mod", "golangci-lint"}
	steps := []step{
		{"format", func() (string, error) {
			if *fix {
				if err := gocmd(append(lint, "fmt", "./...")...); err != nil {
					return "", err
				}
			}
			return "", gocmd(append(lint, "fmt", "--diff", "./...")...)
		}},
		{"go mod tidy", func() (string, error) { return "", gocmd("mod", "tidy", "-diff") }},
		{"cgo", func() (string, error) { return "", cgoCompiler() }},
		{"lint", func() (string, error) { return "", gocmd(append(lint, "run", "./...")...) }},
		{"test", func() (string, error) { return "", gocmd("test", "-shuffle=on", "./...") }},
		// The determinism golden once more with Box2D's SIMD compiled out,
		// so every OS also checks the scalar path against the SIMD ones.
		{"box2d scalar", func() (string, error) {
			return "", gocmd("test", "-tags", "box2dscalar", "-run", "Determinism", "./internal/physics/")
		}},
		{"race", func() (string, error) { return "", gocmd("test", "-race", "./...") }},
		{"vuln", func() (string, error) {
			if !*vuln {
				return "not requested (-vuln)", nil
			}
			return "", gocmd("tool", "-modfile=tools.mod", "govulncheck", "./...")
		}},
	}

	results := make([]result, len(steps))
	notes := make([]string, len(steps))
	for i, s := range steps {
		fmt.Printf("\n== %s\n", s.name)
		skipped, err := s.run()
		switch {
		case err != nil:
			results[i], notes[i] = fail, err.Error()
		case skipped != "":
			results[i], notes[i] = skip, skipped
		default:
			results[i] = pass
		}
	}

	fmt.Println("\n== summary")
	failed := false
	for i, s := range steps {
		label := [...]string{pass: "PASS", fail: "FAIL", skip: "SKIP"}[results[i]]
		if notes[i] != "" {
			fmt.Printf("%s  %s  (%s)\n", label, s.name, notes[i])
		} else {
			fmt.Printf("%s  %s\n", label, s.name)
		}
		failed = failed || results[i] == fail
	}
	if failed {
		os.Exit(1)
	}
}

func gocmd(args ...string) error {
	fmt.Println("$ go", strings.Join(args, " "))
	cmd := exec.Command("go", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("exit status %d", exit.ExitCode())
		}
		return err
	}
	return nil
}

// cgoCompiler fails unless cgo is on and its C compiler runs, and prints
// which compiler it is, so a CI log shows what built Box2D. The game needs
// cgo for Box2D (internal/physics) and the race detector needs it too. On
// Windows that takes a MinGW-family gcc or clang.
func cgoCompiler() error {
	out, err := exec.Command("go", "env", "CGO_ENABLED", "CC").Output()
	if err != nil {
		return fmt.Errorf("cannot read go env: %w", err)
	}
	// One value per line: CC may hold spaces.
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	if len(lines) != 2 || strings.TrimSpace(lines[0]) != "1" {
		return errors.New("cgo is disabled; install a C compiler (MinGW-family gcc or clang on Windows)")
	}
	cc, err := splitCC(lines[1])
	if err != nil {
		return err
	}
	version, err := exec.Command(cc[0], "--version").Output()
	if err != nil {
		return fmt.Errorf("the C compiler %q does not run: %w", cc[0], err)
	}
	first, _, _ := strings.Cut(string(version), "\n")
	fmt.Printf("CC=%s: %s\n", strings.TrimSpace(lines[1]), strings.TrimSpace(first))
	return nil
}

// splitCC splits CC into the compiler and its arguments as the go command
// does: fields are separated by spaces, and a field in single or double
// quotes keeps its spaces. Nothing inside quotes is unescaped.
func splitCC(cc string) ([]string, error) {
	var fields []string
	for s := strings.TrimSpace(cc); s != ""; s = strings.TrimLeft(s, " \t\r\n") {
		if q := s[0]; q == '"' || q == '\'' {
			end := strings.IndexByte(s[1:], q)
			if end < 0 {
				return nil, fmt.Errorf("CC %q has an unterminated %c quote", cc, q)
			}
			fields = append(fields, s[1:1+end])
			s = s[2+end:]
			continue
		}
		end := strings.IndexAny(s, " \t\r\n")
		if end < 0 {
			end = len(s)
		}
		fields = append(fields, s[:end])
		s = s[end:]
	}
	if len(fields) == 0 {
		return nil, errors.New("CC is empty")
	}
	return fields, nil
}
