// Command fmthook formats a Go file right after Claude Code edits it.
//
// It is a PostToolUse hook (see .claude/settings.json). It reads the hook
// event from stdin and runs the project's formatters (gofumpt and
// goimports, through golangci-lint) on the edited file, so formatting
// never drifts while an agent works. It never blocks the edit: problems
// are reported on stderr and the hook exits 0.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type event struct {
	Cwd       string `json:"cwd"`
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fmthook:", err)
	}
}

func run() error {
	var ev event
	if err := json.NewDecoder(os.Stdin).Decode(&ev); err != nil {
		return fmt.Errorf("read hook event: %w", err)
	}
	path := ev.ToolInput.FilePath
	if !strings.HasSuffix(path, ".go") {
		return nil
	}
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		root = ev.Cwd
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("locate %s in %s: %w", path, root, err)
	}
	if strings.HasPrefix(rel, "..") {
		return nil // outside the project
	}
	cmd := exec.Command("go", "tool", "-modfile=tools.mod", "golangci-lint", "fmt", rel)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("format %s: %w\n%s", rel, err, out)
	}
	return nil
}
