package app

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// lockFile marks a save folder as open. It holds the id of the process
// that opened it.
const lockFile = "lock"

// lockSave makes this process the only one with the save in dir open,
// until it calls unlock. Two sessions on one save would each save their
// own world over the other's. A lock left by a process that is no longer
// running, after a crash, is taken over.
//
// Two processes that find the same stale lock at the same moment could
// both take it over; that needs a crash and two starts at once, and the
// lock is for the player starting the game twice by mistake.
func lockSave(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("app: -save: %w", err)
	}
	path := filepath.Join(dir, lockFile)
	for range 2 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, werr := fmt.Fprintf(f, "%d\n", os.Getpid())
			if err := errors.Join(werr, f.Close()); err != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("app: -save: %w", err)
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("app: -save: %w", err)
		}
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("app: -save: %w", err)
		}
		pid, perr := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && perr == nil && running(pid) {
			return nil, fmt.Errorf("app: %s is open in another matter-wake (process %d); close it, or remove %s if no such game runs",
				dir, pid, path)
		}
		slog.Warn("taking over the lock of a save whose game is gone", "path", path, "pid", strings.TrimSpace(string(data)))
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("app: -save: %w", err)
		}
	}
	return nil, fmt.Errorf("app: -save: could not lock %s", path)
}

// running reports whether a process with the given id runs. On Windows,
// finding a process opens it, which fails once it is gone. Elsewhere
// finding always succeeds, and signal 0 asks after the process without
// touching it; a process of another user refuses it but runs.
func running(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
