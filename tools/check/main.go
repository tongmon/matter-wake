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
		{"lint", func() (string, error) { return "", gocmd(append(lint, "run", "./...")...) }},
		{"test", func() (string, error) { return "", gocmd("test", "-shuffle=on", "./...") }},
		{"race", func() (string, error) {
			if reason := cgoMissing(); reason != "" {
				return reason, nil
			}
			return "", gocmd("test", "-race", "./...")
		}},
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

// cgoMissing returns why the race detector cannot run here, or "". It
// needs cgo and a C compiler (on Windows, a MinGW-family gcc or clang).
func cgoMissing() string {
	out, err := exec.Command("go", "env", "CGO_ENABLED", "CC").Output()
	if err != nil {
		return "cannot read go env: " + err.Error()
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != "1" {
		return "cgo is disabled; install a C compiler (MinGW on Windows) for -race"
	}
	if _, err := exec.LookPath(fields[1]); err != nil {
		return fmt.Sprintf("C compiler %q not found", fields[1])
	}
	return ""
}
