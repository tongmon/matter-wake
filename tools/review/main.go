// Command review asks Codex for a code review and saves it to
// out/codex-review-N.txt.
//
//	go run ./tools/review               the branch against origin/main
//	go run ./tools/review -base main    the branch against another base
//	go run ./tools/review -uncommitted  uncommitted changes (before the first commit)
//
// Codex may write only in the repository, the temp folder and the Go build
// cache, so it can run go test and temporary probe tests but nothing else
// on the machine. Afterwards the command lists every file Codex created,
// changed or removed in the repository and fails if there is one, so a
// leftover probe cannot slip into a commit. Run it from the repository root,
// preferably on a clean tree.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "review:", err)
		os.Exit(1)
	}
}

func run() error {
	base := flag.String("base", "origin/main", "branch to review against")
	uncommitted := flag.Bool("uncommitted", false, "review staged, unstaged and untracked changes instead")
	flag.Parse()
	if _, err := os.Stat("go.mod"); err != nil {
		return errors.New("run from the repository root")
	}

	gocache, err := output("go", "env", "GOCACHE")
	if err != nil {
		return err
	}
	args := []string{
		"review",
		"-c", `sandbox_mode="workspace-write"`,
		"-c", fmt.Sprintf("sandbox_workspace_write.writable_roots=['%s']", gocache),
	}
	if *uncommitted {
		args = append(args, "--uncommitted")
	} else {
		args = append(args, "--base", *base)
	}

	path, err := nextReportPath()
	if err != nil {
		return err
	}
	before, err := treeState()
	if err != nil {
		return err
	}
	fmt.Printf("$ codex %s > %s\n", strings.Join(args, " "), path)
	runErr := runCodex(args, path)

	after, err := treeState()
	if err != nil {
		return err
	}
	if err := printSummary(path); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("codex: %w (see %s)", runErr, path)
	}
	if err := checkSandbox(path); err != nil {
		return err
	}
	return reportLeftovers(before, after)
}

// checkSandbox fails if Codex did not run with the sandbox asked for, for
// example because a newer Codex renamed the setting. Without it the review
// could not run tests and would quietly fall back to reading code.
func checkSandbox(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	head := string(data[:min(len(data), 2048)])
	if !strings.Contains(head, "sandbox: workspace-write") {
		return fmt.Errorf("codex did not run in the workspace-write sandbox; see the header of %s", path)
	}
	return nil
}

func output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

var reportName = regexp.MustCompile(`^codex-review-(\d+)\.txt$`)

// nextReportPath returns out/codex-review-N.txt with N one past the
// highest report already there.
func nextReportPath() (string, error) {
	if err := os.MkdirAll("out", 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir("out")
	if err != nil {
		return "", err
	}
	next := 1
	for _, e := range entries {
		if m := reportName.FindStringSubmatch(e.Name()); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= next {
				next = n + 1
			}
		}
	}
	return filepath.Join("out", fmt.Sprintf("codex-review-%d.txt", next)), nil
}

func runCodex(args []string, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	cmd := exec.Command("codex", args...)
	// No stdin: codex review would otherwise wait for a prompt on it.
	cmd.Stdout, cmd.Stderr = f, f
	runErr := cmd.Run()
	if err := f.Close(); err != nil && runErr == nil {
		runErr = err
	}
	return runErr
}

// treeState hashes every file git reports as changed or untracked, so a
// file Codex touches shows up even if it was already dirty.
func treeState() (map[string][32]byte, error) {
	out, err := exec.Command("git", "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	state := map[string][32]byte{}
	for entry := range bytes.SplitSeq(out, []byte{0}) {
		if len(entry) < 4 {
			continue // the source of a rename, or the trailing empty entry
		}
		name := string(entry[3:])
		data, err := os.ReadFile(name)
		if err != nil {
			state[name] = [32]byte{} // deleted
			continue
		}
		state[name] = sha256.Sum256(data)
	}
	return state, nil
}

func reportLeftovers(before, after map[string][32]byte) error {
	var touched []string
	for name, sum := range after {
		if old, ok := before[name]; !ok || old != sum {
			touched = append(touched, name)
		}
	}
	for name := range maps.Keys(before) {
		if _, ok := after[name]; !ok {
			touched = append(touched, name)
		}
	}
	if len(touched) == 0 {
		fmt.Println("\nCodex left the working tree as it was.")
		return nil
	}
	slices.Sort(touched)
	return fmt.Errorf("codex changed files in the working tree; inspect and revert them:\n  %s",
		strings.Join(touched, "\n  "))
}

// printSummary prints the review's final message: everything after the
// last line that is exactly "codex".
func printSummary(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var lines []string
	start := -1
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		if sc.Text() == "codex" {
			start = len(lines) + 1
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Println()
	if start < 0 {
		fmt.Printf("No review summary found; read %s.\n", path)
		return nil
	}
	fmt.Println(strings.Join(lines[start:], "\n"))
	return nil
}
